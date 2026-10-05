package agentmode

import (
	"context"
	"errors"
	"log"
	"sync/atomic"

	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/launcher"
	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

// ErrSessionOpen is returned while a session Launch started is still running.
var ErrSessionOpen = errors.New("a Goose session is already open")

var (
	profileFor = troubleshoot.DefaultProfile
	runCmd     = launcher.Run
	// sessionOpen is true from a started launch until its process exits.
	// A second launch would reach the first session's single-instance lock,
	// which llmman refuses without a terminal to ask on, so it is refused
	// here instead — for the Launch button and --ask-bluefin alike.
	sessionOpen atomic.Bool
)

// SessionOpen reports whether a session Launch started is still running.
func SessionOpen() bool { return sessionOpen.Load() }

// Launch starts Goose Desktop through llmman's invocation-scoped desktop
// integration, on Agent Mode's model alias, inside ChairLift's own Goose
// profile. The profile is rewritten immediately before every launch, so a
// moved Homebrew prefix or a hand edit cannot leave a session with stale
// tools; the user's own ~/.config/goose is never read or written.
//
// The process is started without a context so the long-lived GUI is not tied
// to a caller's timeout or cancellation. reportFailure, when non-nil, gets a
// non-zero exit; it runs on the wait goroutine.
func Launch(ctx context.Context, facts ReadinessFacts, reportFailure func(error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if state := Evaluate(facts); !state.Ready() {
		return errors.New(state.MissingPrerequisite())
	}
	if !sessionOpen.CompareAndSwap(false, true) {
		return ErrSessionOpen
	}
	started := false
	defer func() {
		if !started {
			sessionOpen.Store(false)
		}
	}()

	profile, err := profileFor()
	if err != nil {
		return err
	}
	if err := profile.Write(facts.Tools.ServerPath); err != nil {
		return err
	}
	// The alias, not the reference it resolves to: llmman resolves it when
	// the session launches, so a session always starts on whatever the
	// Agents page last selected.
	cmd, err := troubleshoot.Command(facts.Tools, profile, aistack.ActiveModelAlias)
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would launch Goose Desktop with model %s via llmman", facts.ActiveModel)
		log.Printf("[DRY-RUN] would execute: %s", cmd.String())
		return nil
	}
	if err := runCmd(cmd, func(err error) {
		sessionOpen.Store(false)
		if err != nil && reportFailure != nil {
			reportFailure(err)
		}
	}); err != nil {
		return err
	}
	started = true
	return nil
}
