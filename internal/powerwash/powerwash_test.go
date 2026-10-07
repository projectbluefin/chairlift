package powerwash

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStepsAreOrderedAndTitled(t *testing.T) {
	list := steps
	want := []StepID{StepFlatpak, StepDistrobox}
	if len(list) != len(want) {
		t.Fatalf("step inventory has %d entries, want %d", len(list), len(want))
	}
	for i, step := range list {
		if step.ID != want[i] {
			t.Errorf("steps[%d].ID = %q, want %q", i, step.ID, want[i])
		}
		if step.Title == "" {
			t.Errorf("steps[%d] has no title", i)
		}
	}
}

func okRunner() Runner {
	return Runner{
		FlatpakInstalled:   func() bool { return true },
		FlatpaksPresent:    func(context.Context) (bool, error) { return true, nil },
		RemoveFlatpaks:     func(context.Context) error { return nil },
		DistroboxInstalled: func() bool { return true },
		DistroboxesPresent: func(context.Context) (bool, error) { return true, nil },
		RemoveDistroboxes:  func(context.Context) error { return nil },
	}
}

func TestRunExecutesBothStepsIndependently(t *testing.T) {
	results := okRunner().Run(context.Background())
	if len(results) != 2 {
		t.Fatalf("Run() returned %d results, want 2", len(results))
	}
	for _, result := range results {
		if result.Outcome != OutcomeSucceeded {
			t.Errorf("%s outcome = %q, want %q", result.Step.ID, result.Outcome, OutcomeSucceeded)
		}
	}
}

// A failed step must not abort the run: the two steps are independent.
func TestRunContinuesAfterAStepFails(t *testing.T) {
	distroboxRan := false
	runner := okRunner()
	runner.RemoveFlatpaks = func(context.Context) error { return errors.New("flatpak exploded") }
	runner.RemoveDistroboxes = func(context.Context) error { distroboxRan = true; return nil }

	results := runner.Run(context.Background())
	if !distroboxRan {
		t.Error("a failed flatpak step aborted the independent distrobox step")
	}
	if results[0].Outcome != OutcomeFailed {
		t.Errorf("flatpak outcome = %q, want %q", results[0].Outcome, OutcomeFailed)
	}
	if results[1].Outcome != OutcomeSucceeded {
		t.Errorf("distrobox outcome = %q, want %q", results[1].Outcome, OutcomeSucceeded)
	}
}

// A tool that is not installed is a skip, not a failure — there is nothing
// for that step to remove.
func TestRunSkipsAnUninstalledTool(t *testing.T) {
	runner := okRunner()
	runner.DistroboxInstalled = func() bool { return false }

	results := runner.Run(context.Background())
	if results[1].Outcome != OutcomeSkipped {
		t.Errorf("distrobox outcome = %q, want %q", results[1].Outcome, OutcomeSkipped)
	}
}

// A nil seam is a wiring mistake and must not report success.
func TestRunTreatsAMissingSeamAsFailureNotSuccess(t *testing.T) {
	runner := Runner{
		FlatpakInstalled:   func() bool { return true },
		DistroboxInstalled: func() bool { return true },
	}
	results := runner.Run(context.Background())
	for _, result := range results {
		if result.Outcome != OutcomeFailed {
			t.Errorf("%s outcome = %q with no seam wired, want %q", result.Step.ID, result.Outcome, OutcomeFailed)
		}
	}

	// An inventory seam alone, without the removal, is the same mistake.
	noRemoval := okRunner()
	noRemoval.RemoveFlatpaks = nil
	if got := noRemoval.Run(context.Background())[0].Outcome; got != OutcomeFailed {
		t.Errorf("flatpak outcome with no removal seam = %q, want %q", got, OutcomeFailed)
	}
}

// Both removal commands exit 0 when the account holds nothing — observed in
// ghcr.io/projectbluefin/dakota:testing, where every application is a
// system-scope Flatpak — so a removal that had nothing to remove must be a
// skip that reaches the "nothing was installed" copy, and must not run.
func TestRunSkipsAnAccountWithNothingToRemove(t *testing.T) {
	removed := false
	runner := okRunner()
	runner.FlatpaksPresent = func(context.Context) (bool, error) { return false, nil }
	runner.DistroboxesPresent = func(context.Context) (bool, error) { return false, nil }
	runner.RemoveFlatpaks = func(context.Context) error { removed = true; return nil }
	runner.RemoveDistroboxes = func(context.Context) error { removed = true; return nil }

	results := runner.Run(context.Background())
	if removed {
		t.Error("a removal ran for an account that holds nothing")
	}
	for _, result := range results {
		if result.Outcome != OutcomeSkipped {
			t.Errorf("%s outcome = %q, want %q", result.Step.ID, result.Outcome, OutcomeSkipped)
		}
	}
	if got := Summarize(results).Headline; got != "Nothing was installed to remove" {
		t.Errorf("Headline = %q, want %q", got, "Nothing was installed to remove")
	}
}

// An inventory that cannot be read is a failure, not a skip: the run cannot
// say whether anything was left behind.
func TestRunFailsWhenTheInventoryCannotBeRead(t *testing.T) {
	removed := false
	runner := okRunner()
	runner.DistroboxesPresent = func(context.Context) (bool, error) { return false, errors.New("podman is not running") }
	runner.RemoveDistroboxes = func(context.Context) error { removed = true; return nil }

	results := runner.Run(context.Background())
	if removed {
		t.Error("distrobox removal ran after its inventory failed")
	}
	if results[1].Outcome != OutcomeFailed || results[1].Detail != "podman is not running" {
		t.Errorf("distrobox result = %+v, want a failure carrying the inventory error", results[1])
	}
	if results[0].Outcome != OutcomeSucceeded {
		t.Errorf("flatpak outcome = %q, want %q", results[0].Outcome, OutcomeSucceeded)
	}
}

func TestSummarizeCoversEveryOutcome(t *testing.T) {
	flatpak := Step{ID: StepFlatpak, Title: "Flatpak Applications"}
	distrobox := Step{ID: StepDistrobox, Title: "Distrobox Containers"}

	tests := []struct {
		name         string
		results      []Result
		wantHeadline string
	}{
		{name: "nothing planned", results: nil, wantHeadline: "Nothing to remove"},
		{
			name: "both succeeded",
			results: []Result{
				{Step: flatpak, Outcome: OutcomeSucceeded},
				{Step: distrobox, Outcome: OutcomeSucceeded},
			},
			wantHeadline: "Powerwash complete",
		},
		{
			name: "neither tool installed",
			results: []Result{
				{Step: flatpak, Outcome: OutcomeSkipped},
				{Step: distrobox, Outcome: OutcomeSkipped},
			},
			wantHeadline: "Nothing was installed to remove",
		},
		{
			name: "one failed",
			results: []Result{
				{Step: flatpak, Outcome: OutcomeFailed},
				{Step: distrobox, Outcome: OutcomeSucceeded},
			},
			wantHeadline: "problems",
		},
		{
			name: "one skipped, one succeeded",
			results: []Result{
				{Step: flatpak, Outcome: OutcomeSucceeded},
				{Step: distrobox, Outcome: OutcomeSkipped},
			},
			wantHeadline: "Powerwash complete",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary := Summarize(test.results)
			if !strings.Contains(summary.Headline, test.wantHeadline) {
				t.Errorf("Headline = %q, want it to contain %q", summary.Headline, test.wantHeadline)
			}
			if total := summary.Succeeded + summary.Failed + summary.Skipped; total != len(test.results) {
				t.Errorf("counts sum to %d, want %d", total, len(test.results))
			}
		})
	}
}
