// Package powerwash sequences ChairLift's Powerwash action: removing every
// user-scope Flatpak application and every Distrobox container, so a
// machine's applications return to what the base image ships.
//
// It is unprivileged. Both steps run in the user's own account — the same
// reasoning as gaming mode and Homebrew tap trust — so Powerwash needs no
// pkexec prompt and touches nothing the administrator owns. That is also
// what distinguishes it from Factory Reset (internal/ublue.FactoryReset):
// Powerwash clears applications; Factory Reset replaces the OS image itself
// and is the one that needs privilege and an --experimental warning.
//
// The package is pure in the same shape as internal/updateall: every step
// runs through a function seam on Runner, so the whole
// succeeded/failed/tool-absent matrix is testable without a real Flatpak or
// Distrobox installation.
package powerwash

import "context"

// StepID identifies one step of a Powerwash run.
type StepID string

const (
	StepFlatpak   StepID = "flatpak"
	StepDistrobox StepID = "distrobox"
)

// Step is one unit of work.
type Step struct {
	ID    StepID
	Title string
}

// steps is the canonical, ordered step inventory.
var steps = []Step{
	{ID: StepFlatpak, Title: "Flatpak Applications"},
	{ID: StepDistrobox, Title: "Distrobox Containers"},
}

// Outcome is one step's result.
type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	// OutcomeSkipped means the tool the step needs is not installed. That is
	// not a failure: a machine without Distrobox has nothing for the
	// Distrobox step to remove.
	OutcomeSkipped Outcome = "skipped"
)

// Result is one step's outcome plus the detail shown beside it.
type Result struct {
	Step    Step
	Outcome Outcome
	Detail  string
}

// Runner executes a Powerwash run. Every field is a seam; a nil seam for a
// step's action is treated as OutcomeFailed, never success, matching
// internal/updateall.Runner's rule that a wiring mistake cannot report an
// operation as having happened.
type Runner struct {
	FlatpakInstalled   func() bool
	RemoveFlatpaks     func(ctx context.Context) error
	DistroboxInstalled func() bool
	RemoveDistroboxes  func(ctx context.Context) error
}

// Run executes every step in order and returns one Result each. A failed
// step does not abort the run: the two steps are independent, so a
// Distrobox failure must not leave Flatpaks unremoved, and vice versa.
func (r Runner) Run(ctx context.Context) []Result {
	results := make([]Result, 0, len(steps))
	for _, step := range steps {
		results = append(results, r.runStep(ctx, step))
	}
	return results
}

func (r Runner) runStep(ctx context.Context, step Step) Result {
	switch step.ID {
	case StepFlatpak:
		installed := r.FlatpakInstalled != nil && r.FlatpakInstalled()
		if !installed {
			return Result{Step: step, Outcome: OutcomeSkipped, Detail: "Flatpak is not installed"}
		}
		if r.RemoveFlatpaks == nil {
			return Result{Step: step, Outcome: OutcomeFailed, Detail: "Flatpak removal is unavailable"}
		}
		if err := r.RemoveFlatpaks(ctx); err != nil {
			return Result{Step: step, Outcome: OutcomeFailed, Detail: err.Error()}
		}
		return Result{Step: step, Outcome: OutcomeSucceeded, Detail: "Removed"}

	case StepDistrobox:
		installed := r.DistroboxInstalled != nil && r.DistroboxInstalled()
		if !installed {
			return Result{Step: step, Outcome: OutcomeSkipped, Detail: "Distrobox is not installed"}
		}
		if r.RemoveDistroboxes == nil {
			return Result{Step: step, Outcome: OutcomeFailed, Detail: "Distrobox removal is unavailable"}
		}
		if err := r.RemoveDistroboxes(ctx); err != nil {
			return Result{Step: step, Outcome: OutcomeFailed, Detail: err.Error()}
		}
		return Result{Step: step, Outcome: OutcomeSucceeded, Detail: "Removed"}

	default:
		return Result{Step: step, Outcome: OutcomeFailed, Detail: "unknown step"}
	}
}

// Summary aggregates a run's results.
type Summary struct {
	Succeeded int
	Failed    int
	Skipped   int
	Headline  string
}

// Summarize computes the aggregate outcome. The distinct headlines: every
// step succeeded, every step was skipped (neither tool installed — nothing
// to do), some failed, and everything else succeeded or was skipped.
func Summarize(results []Result) Summary {
	summary := Summary{}
	for _, result := range results {
		switch result.Outcome {
		case OutcomeSucceeded:
			summary.Succeeded++
		case OutcomeFailed:
			summary.Failed++
		case OutcomeSkipped:
			summary.Skipped++
		}
	}

	total := len(results)
	switch {
	case total == 0:
		summary.Headline = "Nothing to remove"
	case summary.Skipped == total:
		summary.Headline = "Nothing was installed to remove"
	case summary.Failed > 0:
		summary.Headline = "Powerwash finished with problems"
	default:
		summary.Headline = "Powerwash complete"
	}
	return summary
}
