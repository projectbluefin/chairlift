package agentmode

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/launcher"
	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

var (
	profileFor = troubleshoot.DefaultProfile
	runCmd     = launcher.Run
	// sessionStartWait bounds how long a fresh launch waits for Goose to
	// take its profile's single-instance lock; sessionPoll is how often it
	// looks.
	sessionStartWait = 15 * time.Second
	sessionPoll      = 250 * time.Millisecond
)

// LaunchResult is what a successful Launch did.
type LaunchResult int

const (
	// LaunchStarted started a fresh session, which now holds the profile's
	// single-instance lock.
	LaunchStarted LaunchResult = iota
	// LaunchStarting started a fresh session that had not taken the lock
	// when the wait ended; it may still be starting.
	LaunchStarting
	// LaunchHandedOff found a session already holding the profile and handed
	// the request to it. That session may be one that never drew a window,
	// so the caller has to say so rather than report a fresh start.
	LaunchHandedOff
)

// Launch opens Goose Desktop inside ChairLift's own Goose profile, the way
// clicking Goose would: when no session is running, through llmman's
// invocation-scoped integration on Agent Mode's model alias; when one is,
// by starting goose-desktop again, which Goose's single-instance lock hands
// to the running session, so no second Goose starts (the compositor decides
// whether to raise its window; no activation token is passed). The profile is
// rewritten before a fresh session, so a moved Homebrew prefix or a hand
// edit cannot leave a session with stale tools; the user's own
// ~/.config/goose is never read or written.
//
// A fresh launch returns once Goose holds the profile's lock, the process
// exits, sessionStartWait passes, or ctx ends, so a caller's busy state
// covers Goose's startup: a second click before the lock exists would
// otherwise start a second llmman launch, which llmman refuses with an error
// while the first Goose opens normally. A non-zero exit during that wait is
// Launch's error.
//
// The process is started without a context so the long-lived GUI is not tied
// to a caller's timeout or cancellation. reportFailure, when non-nil, gets a
// non-zero exit after Launch returned; it runs on the wait goroutine.
func Launch(ctx context.Context, facts ReadinessFacts, reportFailure func(error)) (LaunchResult, error) {
	if err := ctx.Err(); err != nil {
		return LaunchStarted, err
	}
	if state := Evaluate(facts); !state.Ready() {
		return LaunchStarted, errors.New(state.MissingPrerequisite())
	}
	profile, err := profileFor()
	if err != nil {
		return LaunchStarted, err
	}

	if profile.Running() {
		cmd, err := troubleshoot.ReopenCommand(facts.Tools, profile)
		if err != nil {
			return LaunchHandedOff, err
		}
		if dryrun.Enabled() {
			log.Printf("[DRY-RUN] would execute: %s", cmd.String())
			return LaunchHandedOff, nil
		}
		return LaunchHandedOff, runCmd(cmd, exitReporter(reportFailure))
	}

	if err := profile.Write(facts.Tools.ServerPath); err != nil {
		return LaunchStarted, err
	}
	// The alias, not the reference it resolves to: llmman resolves it when
	// the session launches, so a session always starts on whatever the
	// Agents page last selected.
	cmd, err := troubleshoot.Command(facts.Tools, profile, aistack.ActiveModelAlias)
	if err != nil {
		return LaunchStarted, err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would launch Goose Desktop with model %s via llmman", facts.ActiveModel)
		log.Printf("[DRY-RUN] would execute: %s", cmd.String())
		return LaunchStarted, nil
	}
	exit := &startupExit{waiting: true, exited: make(chan error, 1), report: exitReporter(reportFailure)}
	if err := runCmd(cmd, exit.onExit); err != nil {
		return LaunchStarted, err
	}
	defer exit.stopWaiting()
	return awaitSession(ctx, profile, exit.exited)
}

// awaitSession waits for a fresh session to take the profile's lock. A clean
// exit does not end the wait — the launcher may hand Goose off and return —
// but a failed one does.
func awaitSession(ctx context.Context, profile troubleshoot.Profile, exited <-chan error) (LaunchResult, error) {
	deadline := time.NewTimer(sessionStartWait)
	defer deadline.Stop()
	poll := time.NewTicker(sessionPoll)
	defer poll.Stop()
	for {
		if profile.Running() {
			return LaunchStarted, nil
		}
		select {
		case err := <-exited:
			if err != nil {
				return LaunchStarted, fmt.Errorf("goose desktop exited while starting: %w", err)
			}
			exited = nil
		case <-poll.C:
		case <-deadline.C:
			return LaunchStarting, nil
		case <-ctx.Done():
			return LaunchStarting, nil
		}
	}
}

// startupExit routes the launch's exit: to Launch while it waits for the
// session to start, and to the caller's reporter after Launch returned.
type startupExit struct {
	mu      sync.Mutex
	waiting bool
	exited  chan error
	report  func(error)
}

func (s *startupExit) onExit(err error) {
	s.mu.Lock()
	if s.waiting {
		s.exited <- err
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.report(err)
}

// stopWaiting hands later exits to the reporter, including one that arrived
// after the wait's last look.
func (s *startupExit) stopWaiting() {
	s.mu.Lock()
	s.waiting = false
	var pending error
	select {
	case pending = <-s.exited:
	default:
	}
	s.mu.Unlock()
	s.report(pending)
}

func exitReporter(reportFailure func(error)) func(error) {
	return func(err error) {
		if err != nil && reportFailure != nil {
			reportFailure(err)
		}
	}
}
