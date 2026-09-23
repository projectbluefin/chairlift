// Package updateall sequences ChairLift's "Update All" action: bringing the
// OS image, Flatpak applications, and Homebrew packages up to date from one
// user action, and deciding what to tell the user afterwards.
//
// It is the ChairLift equivalent of bluefinctl's `bctl update` and
// finupdate's hero "Check for Updates" button. Both of those present a
// multi-phase update as a single operation, and both draw the same
// distinction this package does: the OS phase only *stages* a new image, so
// the machine is not actually updated until it restarts, whereas the Flatpak
// and Homebrew phases take effect immediately.
//
// The package is pure. It executes nothing itself: every phase runs through a
// function seam on Runner, whose production values are the existing
// internal/bootc, internal/flatpak, and internal/homebrew entry points. That
// keeps the whole sequencing, failure, and restart-decision matrix testable
// on a host with no bootc, no Flatpak, and no Homebrew — and it is why this
// package adds no privileged surface of its own. In particular the OS phase
// goes through internal/bootc's existing staging path, which is the single
// owner of privileged OS staging; adding a second privileged route to the
// same operation would break that ownership.
package updateall

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// cancelledDetail is the detail shown beside every phase that did not complete
// because the user stopped the run, whether it was still running at that
// moment or had not started yet.
const cancelledDetail = "Cancelled"

// PhaseID identifies one update phase.
type PhaseID string

// The update phases, in the order Run executes them. The OS image goes first
// because it is the slowest and the most likely to fail, and because a user
// who abandons the operation partway is better served having the system
// image underway than having had their Flatpaks updated first.
const (
	PhaseOS      PhaseID = "os"
	PhaseFlatpak PhaseID = "flatpak"
	PhaseBrew    PhaseID = "brew"
)

// Phase is one unit of work in an Update All run.
type Phase struct {
	ID    PhaseID
	Title string
}

// phases is the canonical ordered phase inventory.
var phases = []Phase{
	{ID: PhaseOS, Title: "System Image"},
	{ID: PhaseFlatpak, Title: "Applications"},
	{ID: PhaseBrew, Title: "Homebrew Packages"},
}

// Phases returns the canonical phase inventory in execution order. The
// returned slice is freshly allocated on every call.
func Phases() []Phase {
	result := make([]Phase, len(phases))
	copy(result, phases)
	return result
}

// Availability reports which providers exist on this host. A provider that is
// absent is not a failure — it is simply not part of the run, the same way
// ChairLift hides a group whose backing tool is not installed.
type Availability struct {
	OS      bool
	Flatpak bool
	Brew    bool
}

// Available reports whether the phase with the given ID can run.
func (a Availability) Available(id PhaseID) bool {
	switch id {
	case PhaseOS:
		return a.OS
	case PhaseFlatpak:
		return a.Flatpak
	case PhaseBrew:
		return a.Brew
	default:
		return false
	}
}

// Plan returns the phases that will actually run on this host, in execution
// order. An empty plan means Update All has nothing to do and the action must
// not be offered.
func Plan(availability Availability) []Phase {
	plan := make([]Phase, 0, len(phases))
	for _, phase := range phases {
		if availability.Available(phase.ID) {
			plan = append(plan, phase)
		}
	}
	return plan
}

// Outcome is the result of one phase.
type Outcome string

// The phase outcomes. OutcomeSkipped covers a phase that was in the plan but
// did not complete on its own terms: one the run abandoned after the user
// cancelled, including the phase that was in flight when they did. A provider
// that is absent never enters the plan at all.
const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeSkipped   Outcome = "skipped"
)

// Result is one phase's outcome plus the detail shown beside it.
type Result struct {
	Phase   Phase
	Outcome Outcome
	Detail  string
	// StagedVersion is the version the OS phase staged, when it staged one.
	// It is carried as data rather than recovered from Detail: Detail is
	// presentation, and a caller parsing it back would silently lose the
	// version the moment the wording changed. Empty for every other phase,
	// and for an OS phase that staged nothing.
	StagedVersion string
}

// EventType classifies a progress event emitted during a run.
type EventType string

const (
	// EventPhaseStarted is emitted once per phase, before its work begins.
	EventPhaseStarted EventType = "phase-started"
	// EventMessage carries one line of a phase's streamed output.
	EventMessage EventType = "message"
	// EventPhaseFinished is emitted once per phase, carrying its Result.
	EventPhaseFinished EventType = "phase-finished"
)

// Event is a single progress notification from a run.
type Event struct {
	Type    EventType
	Phase   Phase
	Message string
	Result  Result
}

// Runner executes an Update All run. Every field is a seam: production values
// wrap internal/bootc, internal/flatpak, and internal/homebrew, and tests
// substitute their own. A nil seam for a phase in the plan is treated as that
// phase failing, not as success, so a wiring mistake cannot report a system
// as updated when nothing ran.
type Runner struct {
	// StageOS stages the next OS image. It streams its output through the
	// supplied callback. It must return nil when the system is already
	// current — the stage script is idempotent — which is why StagedAfter,
	// not this error, decides whether a restart is required.
	StageOS func(ctx context.Context, emit func(string)) error
	// StagedAfter reports whether an OS image is staged and pending a
	// restart. It is consulted after StageOS, because a successful stage run
	// on an already-current system stages nothing. A failed status read is an
	// error, not evidence that the system is current.
	StagedAfter func(ctx context.Context) (staged bool, version string, err error)
	// UpdateFlatpak updates all Flatpak applications.
	UpdateFlatpak func(ctx context.Context) error
	// UpdateBrew updates Homebrew and upgrades outdated packages.
	UpdateBrew func(ctx context.Context) error
}

// Run executes plan in order, emitting events as it goes, and returns one
// Result per planned phase. It closes events before returning.
//
// Failure of one phase does not abort the run: Flatpak and Homebrew are
// independent of the OS image and of each other, so a failed OS stage must
// not leave a user's applications un-updated too. The one exception is
// context cancellation, which skips every remaining phase — the user asked
// to stop.
func (r Runner) Run(ctx context.Context, plan []Phase, events chan<- Event) []Result {
	defer close(events)

	results := make([]Result, 0, len(plan))
	cancelled := false

	for _, phase := range plan {
		if cancelled || ctx.Err() != nil {
			cancelled = true
			result := Result{Phase: phase, Outcome: OutcomeSkipped, Detail: cancelledDetail}
			results = append(results, result)
			emit(events, Event{Type: EventPhaseFinished, Phase: phase, Result: result})
			continue
		}

		emit(events, Event{Type: EventPhaseStarted, Phase: phase})
		result := r.runPhase(ctx, phase, events)
		results = append(results, result)
		emit(events, Event{Type: EventPhaseFinished, Phase: phase, Result: result})
	}

	return results
}

// runPhase executes one phase and classifies its outcome.
func (r Runner) runPhase(ctx context.Context, phase Phase, events chan<- Event) Result {
	switch phase.ID {
	case PhaseOS:
		if r.StageOS == nil {
			return failed(phase, "system update is unavailable")
		}
		err := r.StageOS(ctx, func(line string) {
			emit(events, Event{Type: EventMessage, Phase: phase, Message: line})
		})
		if err != nil {
			return classify(phase, err)
		}
		staged, version := false, ""
		if r.StagedAfter != nil {
			var err error
			staged, version, err = r.StagedAfter(ctx)
			if err != nil {
				return classify(phase, err)
			}
		}
		result := Result{Phase: phase, Outcome: OutcomeSucceeded, Detail: stagedDetail(staged, version)}
		if staged {
			result.StagedVersion = version
		}
		return result

	case PhaseFlatpak:
		if r.UpdateFlatpak == nil {
			return failed(phase, "Flatpak is unavailable")
		}
		if err := r.UpdateFlatpak(ctx); err != nil {
			return classify(phase, err)
		}
		return Result{Phase: phase, Outcome: OutcomeSucceeded, Detail: "Up to date"}

	case PhaseBrew:
		if r.UpdateBrew == nil {
			return failed(phase, "Homebrew is unavailable")
		}
		if err := r.UpdateBrew(ctx); err != nil {
			return classify(phase, err)
		}
		return Result{Phase: phase, Outcome: OutcomeSucceeded, Detail: "Up to date"}

	default:
		return failed(phase, fmt.Sprintf("unknown phase %q", phase.ID))
	}
}

// Staged reports whether this result represents a genuinely staged OS image.
// A successful OS phase is not sufficient on its own — the stage script is
// idempotent and exits 0 on an already-current system.
func (r Result) Staged() bool {
	return r.Phase.ID == PhaseOS &&
		r.Outcome == OutcomeSucceeded &&
		strings.Contains(r.Detail, "staged")
}

func failed(phase Phase, detail string) Result {
	return Result{Phase: phase, Outcome: OutcomeFailed, Detail: detail}
}

// classify turns a phase's error into its Result. A cancellation is the user
// asking to stop, not the provider failing: the phase that was in flight when
// they pressed Stop must read the same way as the phases the run never
// reached, or a user-requested stop is reported as a broken update. Every
// other error is a failure.
func classify(phase Phase, err error) Result {
	if errors.Is(err, context.Canceled) {
		return Result{Phase: phase, Outcome: OutcomeSkipped, Detail: cancelledDetail}
	}
	return failed(phase, err.Error())
}

// stagedDetail describes the OS phase's outcome. A successful run that staged
// nothing means the system image was already current, which must not be
// reported as an update.
func stagedDetail(staged bool, version string) string {
	if !staged {
		return "Already up to date"
	}
	if version == "" {
		return "Update staged — restart to apply"
	}
	return fmt.Sprintf("Update %s staged — restart to apply", version)
}

// emit sends an event, dropping it if no receiver is listening. A blocked or
// abandoned UI must not wedge the update.
func emit(events chan<- Event, event Event) {
	if events == nil {
		return
	}
	select {
	case events <- event:
	default:
	}
}

// Summary is the aggregate outcome of a run, and what the UI reports.
type Summary struct {
	// Succeeded, Failed, and Skipped count phases by outcome.
	Succeeded int
	Failed    int
	Skipped   int
	// RestartRequired is true only when an OS image was actually staged. A
	// run in which everything succeeded but the system image was already
	// current must not prompt for a restart.
	RestartRequired bool
	// Headline is the one-line result shown to the user.
	Headline string
	// FailedPhases names the phases that failed, in execution order, so the
	// UI can point at them without re-deriving the order.
	FailedPhases []string
	// StagedVersion is the version the OS phase staged, when RestartRequired
	// is true. It may be empty even then: bootc does not always report a
	// version for a staged deployment.
	StagedVersion string
}

// Summarize aggregates a run's results. It is separate from Run so that the
// reporting decisions — in particular "did anything actually change" and "is
// a restart genuinely needed" — are testable without executing anything.
func Summarize(results []Result) Summary {
	summary := Summary{}

	for _, result := range results {
		switch result.Outcome {
		case OutcomeSucceeded:
			summary.Succeeded++
		case OutcomeFailed:
			summary.Failed++
			summary.FailedPhases = append(summary.FailedPhases, result.Phase.Title)
		case OutcomeSkipped:
			summary.Skipped++
		}

		// Only a genuinely staged OS image justifies asking for a restart.
		if result.Phase.ID == PhaseOS && result.Outcome == OutcomeSucceeded && result.Staged() {
			summary.RestartRequired = true
			summary.StagedVersion = result.StagedVersion
		}
	}

	summary.Headline = headline(summary, len(results))
	return summary
}

// headline selects the one-line result. The distinct outcomes are: nothing
// ran, everything failed, some failed, a restart is pending, and everything
// was already current.
func headline(summary Summary, total int) string {
	switch {
	case total == 0:
		return "Nothing to update"
	case summary.Failed == total:
		return "Update failed"
	case summary.Failed > 0 && summary.RestartRequired:
		return fmt.Sprintf("Updated with %d problem(s) — restart to apply the system image", summary.Failed)
	case summary.Failed > 0:
		return fmt.Sprintf("Updated with %d problem(s)", summary.Failed)
	case summary.Skipped > 0:
		return "Update cancelled"
	case summary.RestartRequired:
		return "Update complete — restart to apply"
	default:
		return "Everything is up to date"
	}
}

// PhaseTitles returns the titles of the supplied phases, sorted, for
// diagnostics and test assertions.
func PhaseTitles(list []Phase) []string {
	titles := make([]string, 0, len(list))
	for _, phase := range list {
		titles = append(titles, phase.Title)
	}
	sort.Strings(titles)
	return titles
}
