package agentmode

import (
	"context"
	"errors"
	"log"

	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/launcher"
	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

var (
	profileFor = troubleshoot.DefaultProfile
	runCmd     = launcher.Run
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
	profile, err := profileFor()
	if err != nil {
		return err
	}

	if profile.Running() {
		cmd, err := troubleshoot.ReopenCommand(facts.Tools, profile)
		if err != nil {
			return err
		}
		if dryrun.Enabled() {
			log.Printf("[DRY-RUN] would execute: %s", cmd.String())
			return nil
		}
		return runCmd(cmd, exitReporter(reportFailure))
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
	return runCmd(cmd, exitReporter(reportFailure))
}

func exitReporter(reportFailure func(error)) func(error) {
	return func(err error) {
		if err != nil && reportFailure != nil {
			reportFailure(err)
		}
	}
}
