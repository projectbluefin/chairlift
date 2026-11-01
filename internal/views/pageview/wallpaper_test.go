package pageview

import (
	"strings"
	"testing"
)

func TestDamaskOfferNeverTreatsAFailedProbeAsAbsent(t *testing.T) {
	tests := []struct {
		name                                   string
		installed, probeFailed, flatpakPresent bool
		want                                   DamaskState
	}{
		{name: "installed opens", installed: true, flatpakPresent: true, want: DamaskInstalled},
		{name: "absent with Flatpak installs", flatpakPresent: true, want: DamaskInstallable},
		{name: "absent without Flatpak is hidden", want: DamaskHidden},
		{name: "a failed probe is unknown", probeFailed: true, flatpakPresent: true, want: DamaskUnknown},
		{name: "a failed probe is unknown even when Flatpak looked missing", probeFailed: true, want: DamaskUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DamaskOffer(tt.installed, tt.probeFailed, tt.flatpakPresent); got != tt.want {
				t.Fatalf("DamaskOffer(%v, %v, %v) = %v, want %v", tt.installed, tt.probeFailed, tt.flatpakPresent, got, tt.want)
			}
		})
	}
}

// TestDamaskControlIsOneStatefulAction holds every visible state to one
// distinct button label and a subtitle that agrees with it, so install, open
// and retry can never be offered side by side or mislabelled.
func TestDamaskControlIsOneStatefulAction(t *testing.T) {
	tests := []struct {
		state        DamaskState
		action       string
		subtitleHas  string
		failureHas   string
		subtitleLack string
	}{
		{state: DamaskInstallable, action: "Install", subtitleHas: "Install", failureHas: "install", subtitleLack: "Couldn't"},
		{state: DamaskInstalled, action: "Open", subtitleHas: "feeds", failureHas: "open", subtitleLack: "Install"},
		{state: DamaskUnknown, action: "Try Again", subtitleHas: "Couldn't check", failureHas: "check", subtitleLack: "Install a"},
	}
	seen := map[string]bool{}
	for _, tt := range tests {
		row := DamaskRow(tt.state)
		if row.Title != "Damask" {
			t.Errorf("state %v: title %q, want Damask", tt.state, row.Title)
		}
		if !strings.Contains(row.Subtitle, tt.subtitleHas) || strings.Contains(row.Subtitle, tt.subtitleLack) {
			t.Errorf("state %v: subtitle %q should contain %q and not %q", tt.state, row.Subtitle, tt.subtitleHas, tt.subtitleLack)
		}
		action := DamaskActionName(tt.state)
		if action != tt.action {
			t.Errorf("state %v: action %q, want %q", tt.state, action, tt.action)
		}
		if seen[action] {
			t.Errorf("state %v reuses action label %q", tt.state, action)
		}
		seen[action] = true
		failure := DamaskFailure(tt.state)
		if !strings.Contains(failure, tt.failureHas) || !strings.Contains(failure, "Try again") {
			t.Errorf("state %v: failure %q should name %q and offer a retry", tt.state, failure, tt.failureHas)
		}
	}
}

func TestWallpaperGroupFragmentNamesBothSources(t *testing.T) {
	for _, want := range []string{"collections", "apps"} {
		if !strings.Contains(WallpaperGroupFragment, want) {
			t.Errorf("WallpaperGroupFragment %q does not mention %q", WallpaperGroupFragment, want)
		}
	}
}
