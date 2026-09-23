package actionmsg

import (
	"strings"
	"testing"
)

// setUp builds the plan a live enable produces, so the outcome tables below
// vary only the part they are about.
func feedSetup(installPulp, stageFeeds bool) DeveloperFeedSetup {
	return DeveloperFeedSetupPlan(false, true, true, installPulp, stageFeeds)
}

// Only a confirmed live enable may start the optional worker. Every other
// combination — the preview the screenshot run uses, a disable, and an enable
// whose privileged promotion failed — must produce an empty plan, which is
// what stops the view from spawning the goroutine at all.
func TestDeveloperFeedSetupPlanNeedsAConfirmedLiveEnable(t *testing.T) {
	tests := []struct {
		name      string
		dryRun    bool
		enabled   bool
		succeeded bool
	}{
		{name: "confirmed live enable", enabled: true, succeeded: true},
		{name: "dry-run preview", dryRun: true, enabled: true, succeeded: true},
		{name: "dry-run disable", dryRun: true, enabled: false, succeeded: true},
		{name: "live disable", enabled: false, succeeded: true},
		{name: "failed enable", enabled: true, succeeded: false},
		{name: "failed disable", enabled: false, succeeded: false},
		{name: "dry-run failed enable", dryRun: true, enabled: true, succeeded: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Both optional steps are configured, so an empty result can
			// only come from the admission rule.
			got := DeveloperFeedSetupPlan(tt.dryRun, tt.enabled, tt.succeeded, true, true)

			if tt.enabled && tt.succeeded && !tt.dryRun {
				if !got.InstallPulp || !got.StageFeeds {
					t.Fatalf("DeveloperFeedSetupPlan(%v, %v, %v, true, true) = %+v, want both steps",
						tt.dryRun, tt.enabled, tt.succeeded, got)
				}
				return
			}
			if got.Any() {
				t.Errorf("DeveloperFeedSetupPlan(%v, %v, %v, true, true) = %+v, want no optional work",
					tt.dryRun, tt.enabled, tt.succeeded, got)
			}
		})
	}
}

// The plan mirrors the configuration rather than defaulting either step on:
// each key is independent, and an administrator who set neither gets neither.
func TestDeveloperFeedSetupPlanCarriesEachConfiguredStep(t *testing.T) {
	tests := []struct {
		name        string
		installPulp bool
		stageFeeds  bool
		wantAny     bool
	}{
		{name: "neither configured", wantAny: false},
		{name: "pulp only", installPulp: true, wantAny: true},
		{name: "feeds only", stageFeeds: true, wantAny: true},
		{name: "both configured", installPulp: true, stageFeeds: true, wantAny: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeveloperFeedSetupPlan(false, true, true, tt.installPulp, tt.stageFeeds)

			if got.InstallPulp != tt.installPulp || got.StageFeeds != tt.stageFeeds {
				t.Errorf("DeveloperFeedSetupPlan(_, _, _, %v, %v) = %+v, want %+v",
					tt.installPulp, tt.stageFeeds, got, DeveloperFeedSetup{InstallPulp: tt.installPulp, StageFeeds: tt.stageFeeds})
			}
			if got.Any() != tt.wantAny {
				t.Errorf("Any() = %v, want %v", got.Any(), tt.wantAny)
			}
		})
	}
}

// An unconfigured setup says nothing at all: the ordinary enable that
// installs nothing and stages nothing must not raise a banner about it.
func TestDeveloperFeedFeedbackIsSilentWithoutASetup(t *testing.T) {
	result := DeveloperFeedFeedback(DeveloperFeedSetup{}, DeveloperFeedOutcome{})

	if result.Failed {
		t.Error("an empty setup reported a failure")
	}
	if result.Message != "" {
		t.Errorf("an empty setup produced the message %q, want none", result.Message)
	}
}

// Every requested step and every outcome has a sentence, and the failure flag
// tracks whether any of them did not complete.
func TestDeveloperFeedFeedbackCoversEveryOutcome(t *testing.T) {
	const staged = "/home/dev/.local/share/chairlift/developer-feeds.opml"

	tests := []struct {
		name         string
		setup        DeveloperFeedSetup
		outcome      DeveloperFeedOutcome
		wantFailed   bool
		wantContains []string
		wantMissing  []string
	}{
		{
			name:         "pulp installed",
			setup:        feedSetup(true, false),
			outcome:      DeveloperFeedOutcome{PulpReady: true},
			wantContains: []string{"Pulp is ready."},
			wantMissing:  []string{"could not", "feeds staged", "only the optional setup failed"},
		},
		{
			name:         "pulp install failed",
			setup:        feedSetup(true, false),
			outcome:      DeveloperFeedOutcome{},
			wantFailed:   true,
			wantContains: []string{"Pulp could not be installed.", "Developer access is on; only the optional setup failed."},
			wantMissing:  []string{"Pulp is ready."},
		},
		{
			name:         "feeds staged with a known path",
			setup:        feedSetup(false, true),
			outcome:      DeveloperFeedOutcome{FeedsStaged: true, StagedPath: staged},
			wantContains: []string{staged, "open Pulp to import them"},
			wantMissing:  []string{"could not", "home folder", "only the optional setup failed"},
		},
		{
			name:         "feeds staged without a resolved path",
			setup:        feedSetup(false, true),
			outcome:      DeveloperFeedOutcome{FeedsStaged: true},
			wantContains: []string{"Developer feeds staged in your home folder", "open Pulp to import them"},
			wantMissing:  []string{"could not", "only the optional setup failed"},
		},
		{
			name:         "staging failed",
			setup:        feedSetup(false, true),
			outcome:      DeveloperFeedOutcome{},
			wantFailed:   true,
			wantContains: []string{"The developer feed list could not be staged.", "only the optional setup failed"},
			wantMissing:  []string{"staged at", "staged in your home folder"},
		},
		{
			name:         "both steps succeeded",
			setup:        feedSetup(true, true),
			outcome:      DeveloperFeedOutcome{PulpReady: true, FeedsStaged: true, StagedPath: staged},
			wantContains: []string{"Pulp is ready.", staged},
			wantMissing:  []string{"could not", "only the optional setup failed"},
		},
		{
			name:         "pulp succeeded and staging failed",
			setup:        feedSetup(true, true),
			outcome:      DeveloperFeedOutcome{PulpReady: true},
			wantFailed:   true,
			wantContains: []string{"Pulp is ready.", "The developer feed list could not be staged."},
			wantMissing:  []string{"Pulp could not be installed."},
		},
		{
			name:         "pulp failed and staging succeeded",
			setup:        feedSetup(true, true),
			outcome:      DeveloperFeedOutcome{FeedsStaged: true, StagedPath: staged},
			wantFailed:   true,
			wantContains: []string{"Pulp could not be installed.", staged},
			wantMissing:  []string{"The developer feed list could not be staged."},
		},
		{
			name:         "both steps failed",
			setup:        feedSetup(true, true),
			outcome:      DeveloperFeedOutcome{},
			wantFailed:   true,
			wantContains: []string{"Pulp could not be installed.", "The developer feed list could not be staged."},
			wantMissing:  []string{"Pulp is ready."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DeveloperFeedFeedback(tt.setup, tt.outcome)

			if result.Failed != tt.wantFailed {
				t.Errorf("DeveloperFeedFeedback(%+v, %+v).Failed = %v, want %v", tt.setup, tt.outcome, result.Failed, tt.wantFailed)
			}
			if result.Message == "" {
				t.Fatal("a setup that requested work produced no message")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(result.Message, want) {
					t.Errorf("message = %q, want it to contain %q", result.Message, want)
				}
			}
			for _, unwanted := range tt.wantMissing {
				if strings.Contains(result.Message, unwanted) {
					t.Errorf("message = %q, want it not to contain %q", result.Message, unwanted)
				}
			}
		})
	}
}

// The one claim the issue forbids outright: ChairLift stages a file and stops.
// Importing it is the user's action inside Pulp, whose sandboxed database
// ChairLift never touches, so no admitted outcome may read as if the
// subscriptions were already there.
func TestDeveloperFeedFeedbackNeverClaimsAnImport(t *testing.T) {
	setups := []DeveloperFeedSetup{
		feedSetup(true, false),
		feedSetup(false, true),
		feedSetup(true, true),
	}
	outcomes := []DeveloperFeedOutcome{
		{},
		{PulpReady: true},
		{FeedsStaged: true},
		{FeedsStaged: true, StagedPath: "/home/dev/.local/share/chairlift/developer-feeds.opml"},
		{PulpReady: true, FeedsStaged: true, StagedPath: "/home/dev/.local/share/chairlift/developer-feeds.opml"},
	}

	for _, setup := range setups {
		for _, outcome := range outcomes {
			message := DeveloperFeedFeedback(setup, outcome).Message
			for _, forbidden := range []string{"imported", "subscribed", "subscriptions were added"} {
				if strings.Contains(message, forbidden) {
					t.Errorf("DeveloperFeedFeedback(%+v, %+v) = %q, want it not to claim %q",
						setup, outcome, message, forbidden)
				}
			}
			if !setup.StageFeeds || !outcome.FeedsStaged {
				continue
			}
			// A staged file the user cannot act on is the other half of the
			// same requirement: the banner has to say what to do with it.
			if !strings.Contains(message, "Pulp") {
				t.Errorf("DeveloperFeedFeedback(%+v, %+v) = %q, want it to name the reader to open",
					setup, outcome, message)
			}
		}
	}
}
