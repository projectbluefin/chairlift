package pageview

import (
	"reflect"
	"testing"
)

// The three onboarding URLs and their order are a product decision recorded
// in issue #240, not an implementation detail. Asserting them verbatim is
// the point: a reordered or dropped link changes what a user sees the moment
// they switch Developer Mode on, and no other gate would notice.
func TestDeveloperOnboardingTargetsConfirmedLiveEnable(t *testing.T) {
	got := DeveloperOnboardingTargets(false, true, true)

	want := []DeveloperOnboardingLink{
		{
			Title: "Bluefin Developer Documentation",
			URL:   "https://docs.projectbluefin.io/bluefin-dx/",
		},
		{
			Title: "Project Bluefin Training Catalog",
			URL:   "https://training.projectbluefin.io",
		},
		{
			Title: "GNOME Developer Center",
			URL:   "https://developer.gnome.org/",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeveloperOnboardingTargets(false, true, true) = %+v, want %+v", got, want)
	}
}

// Every remaining combination must open nothing. The table is exhaustive
// over the three admission inputs so that a future relaxation of the rule
// has to delete a named case rather than quietly widen the condition.
func TestDeveloperOnboardingTargetsOpensNothingOtherwise(t *testing.T) {
	tests := []struct {
		name      string
		dryRun    bool
		enabled   bool
		succeeded bool
	}{
		{name: "dry-run preview of an enable", dryRun: true, enabled: true, succeeded: true},
		{name: "disabling", enabled: false, succeeded: true},
		{name: "failed enable", enabled: true, succeeded: false},
		{name: "failed disable", enabled: false, succeeded: false},
		{name: "dry-run disable", dryRun: true, enabled: false, succeeded: true},
		{name: "dry-run failure", dryRun: true, enabled: true, succeeded: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeveloperOnboardingTargets(tt.dryRun, tt.enabled, tt.succeeded); len(got) != 0 {
				t.Errorf("DeveloperOnboardingTargets(%v, %v, %v) = %+v, want no targets",
					tt.dryRun, tt.enabled, tt.succeeded, got)
			}
		})
	}
}

// The view iterates the returned slice, so a caller that writes to it must
// not be able to corrupt the package-level table for the next enable.
func TestDeveloperOnboardingTargetsReturnsACopy(t *testing.T) {
	targets := DeveloperOnboardingTargets(false, true, true)
	if len(targets) == 0 {
		t.Fatal("a confirmed live enable returned no targets; the mutation check is not holding anything")
	}

	targets[0] = DeveloperOnboardingLink{Title: "clobbered", URL: "https://example.invalid/"}

	again := DeveloperOnboardingTargets(false, true, true)
	if again[0].URL == "https://example.invalid/" {
		t.Error("mutating a returned target changed the package-level onboarding table")
	}
}
