package actionmsg

import (
	"fmt"
	"strings"
)

// Developer feed onboarding (issue #238).
//
// Enabling Developer Mode makes one privileged helper call and then, only in
// the branch that reached a confirmed live promotion, may run two optional
// user-scope steps: install Pulp, and stage the curated OPML catalog for the
// user to import. Both are opt-in through `dx_group`'s install_pulp and
// stage_feeds keys, both default to false, and neither is a prerequisite for
// developer access.
//
// The admission rule and the feedback wording live here, in a puregotk-free
// package, because internal/views itself cannot host a test binary: puregotk
// panics resolving GTK/graphene shared libraries at package init, before any
// test function runs (docs/skills/gtk-headless-testing/SKILL.md). The view
// keeps the goroutine, the Flatpak call, and the toast; every decision about
// whether that goroutine may start, and what it is allowed to claim
// afterwards, is decided and asserted here.

// DeveloperFeedSetup is the optional post-enable work a Developer Mode toggle
// should run. The two steps are independent: staging the catalog is useful
// without Pulp (a reader the user already uses can import the file), and
// installing Pulp is useful without the catalog.
type DeveloperFeedSetup struct {
	InstallPulp bool
	StageFeeds  bool
}

// Any reports whether the setup asks for at least one optional step. It is
// the only condition under which the view starts its worker goroutine.
func (s DeveloperFeedSetup) Any() bool {
	return s.InstallPulp || s.StageFeeds
}

// DeveloperFeedSetupPlan returns the optional work a Developer Mode toggle
// should attempt.
//
// The admission rule is deliberately narrow, and it is the same one
// DeveloperOnboardingTargets applies: every argument must line up before any
// optional step runs. A preview (dryRun) must mutate nothing, including the
// user's home directory; switching developer mode *off* is a clean no-op for
// Pulp, the staged catalog, and the subscriptions a user has already imported
// from it — uninstalling a reader or deleting a file the user owns because
// they left a group is a destructive side effect of an unrelated decision;
// and a failed group promotion leaves the account unchanged, so its
// follow-on steps must not run either.
//
// Each configured step is only ever *requested* here, never assumed to have
// happened: the caller runs the work and reports back through
// DeveloperFeedFeedback.
func DeveloperFeedSetupPlan(dryRun, enabled, succeeded, installPulp, stageFeeds bool) DeveloperFeedSetup {
	if dryRun || !enabled || !succeeded {
		return DeveloperFeedSetup{}
	}
	return DeveloperFeedSetup{InstallPulp: installPulp, StageFeeds: stageFeeds}
}

// DeveloperFeedOutcome is what the optional work actually did. The view fills
// it in from the two return values, and it is the only thing
// DeveloperFeedFeedback is allowed to describe: an outcome word is never set
// by inference from configuration.
type DeveloperFeedOutcome struct {
	// PulpReady reports that Pulp is present in the user scope at the end of
	// the run, whether this run installed it or found it already there.
	PulpReady bool
	// FeedsStaged reports that the catalog was written to StagedPath.
	FeedsStaged bool
	// StagedPath is the absolute path of the staged catalog, when the view
	// could resolve one. It is empty when staging failed, and may be empty on
	// a successful stage whose home directory could not be resolved, which is
	// why the feedback has a sentence for both cases.
	StagedPath string
}

// DeveloperFeedResult is the single decision for the optional setup's
// feedback: the banner text, and whether it reports a failure. The view
// branches on Failed alone for both the toast kind and nothing else, so the
// classification and the wording cannot disagree (ADR-0009 rule 3).
type DeveloperFeedResult struct {
	Failed bool
	// Message is empty only for an empty setup; DeveloperFeedFeedback never
	// returns an empty message for a setup that requested work.
	Message string
}

// DeveloperFeedFeedback returns the in-app banner for an optional setup that
// was admitted by DeveloperFeedSetupPlan.
//
// Three things about the wording are requirements, not style:
//
//   - It never claims a subscription was imported. ChairLift writes the OPML
//     file and stops; importing it is the user's own action in Pulp, and
//     Pulp's sandboxed database is never touched. The staged sentence says
//     "open Pulp to import them", not "imported".
//   - A failed optional step is reported separately from developer access.
//     The permission change already succeeded by the time this runs, so a
//     failed Flatpak install says so without implying the groups were not
//     granted, and without a rollback the user did not ask for.
//   - The staged file is named by its real path when one is known, because
//     the user has to find it to import it; the fallback names the home
//     folder rather than inventing a path.
//
// There is no `[DRY-RUN] Preview:` case here, and that is deliberate: a
// preview is rejected by DeveloperFeedSetupPlan before the worker starts, so
// this function is never reached with dry-run work to describe. The toggle's
// own preview toast already says nothing was changed (ADR-0009 rule 2).
func DeveloperFeedFeedback(setup DeveloperFeedSetup, outcome DeveloperFeedOutcome) DeveloperFeedResult {
	if !setup.Any() {
		return DeveloperFeedResult{}
	}

	sentences := make([]string, 0, 3)
	failed := false

	if setup.InstallPulp {
		if outcome.PulpReady {
			sentences = append(sentences, "Pulp is ready.")
		} else {
			failed = true
			sentences = append(sentences, "Pulp could not be installed.")
		}
	}

	if setup.StageFeeds {
		switch {
		case !outcome.FeedsStaged:
			failed = true
			sentences = append(sentences, "The developer feed list could not be staged.")
		case outcome.StagedPath != "":
			sentences = append(sentences, fmt.Sprintf(
				"Developer feeds staged at %s — open Pulp to import them when you are ready.", outcome.StagedPath))
		default:
			sentences = append(sentences, "Developer feeds staged in your home folder — open Pulp to import them when you are ready.")
		}
	}

	if failed {
		// The enable this ran behind has already succeeded. Saying so keeps
		// a failed optional install from reading as a failed permission
		// change, which is the misreport the two outcomes are kept apart to
		// prevent.
		sentences = append(sentences, "Developer access is on; only the optional setup failed.")
	}

	return DeveloperFeedResult{Failed: failed, Message: strings.Join(sentences, " ")}
}
