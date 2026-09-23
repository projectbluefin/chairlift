package trustmsg

import (
	"strings"
	"testing"
)

// TestUpgradeMessage covers both trustGroupAvailable states for the toast text
// shown when a Homebrew upgrade fails with an *homebrew.UntrustedTapError. When
// the Untrusted Homebrew Taps group isn't available (brew_trust_group disabled,
// or its group not yet built) the message must not point the user at a section
// that doesn't exist: no "below" and no "Untrusted Homebrew Taps" reference.
// When the group is available, the existing pointer-to-the-section wording must
// be preserved so current behavior is unchanged. This is the group-combination
// regression for issue #57 (brew_trust_group enabled while brew_updates_group
// disabled). The function is pure and puregotk-free, so this runs headlessly.
func TestUpgradeMessage(t *testing.T) {
	tests := []struct {
		name                string
		pkgName             string
		trustGroupAvailable bool
		wantContains        []string
		forbidContains      []string
	}{
		{
			name:                "trust group available points to section",
			pkgName:             "example-pkg",
			trustGroupAvailable: true,
			wantContains:        []string{"example-pkg", "untrusted tap", "Untrusted Homebrew Taps", "below"},
		},
		{
			name:                "trust group unavailable is self-contained",
			pkgName:             "example-pkg",
			trustGroupAvailable: false,
			wantContains:        []string{"example-pkg", "untrusted tap", "trusted"},
			forbidContains:      []string{"below", "Untrusted Homebrew Taps"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpgradeMessage(tt.pkgName, tt.trustGroupAvailable)

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("UpgradeMessage(%q, %v) = %q, want it to contain %q", tt.pkgName, tt.trustGroupAvailable, got, want)
				}
			}
			for _, forbid := range tt.forbidContains {
				if strings.Contains(got, forbid) {
					t.Errorf("UpgradeMessage(%q, %v) = %q, must not contain %q", tt.pkgName, tt.trustGroupAvailable, got, forbid)
				}
			}
		})
	}
}

func TestBundleMessage(t *testing.T) {
	t.Run("with specific tap", func(t *testing.T) {
		got := BundleMessage("cli", "foo/bar")
		for _, want := range []string{"Brew bundle cli", "trusting third-party taps", "brew trust foo/bar"} {
			if !strings.Contains(got, want) {
				t.Errorf("BundleMessage(\"cli\", \"foo/bar\") = %q, want it to contain %q", got, want)
			}
		}
		for _, forbid := range []string{"Untrusted Homebrew Taps", "below", "Updates"} {
			if strings.Contains(got, forbid) {
				t.Errorf("BundleMessage(\"cli\", \"foo/bar\") = %q, must not contain %q", got, forbid)
			}
		}
	})

	t.Run("without specific tap", func(t *testing.T) {
		got := BundleMessage("cli", "")
		for _, want := range []string{"Brew bundle cli", "trusting third-party taps"} {
			if !strings.Contains(got, want) {
				t.Errorf("BundleMessage(\"cli\", \"\") = %q, want it to contain %q", got, want)
			}
		}
		for _, forbid := range []string{"Untrusted Homebrew Taps", "below", "Updates", "brew trust"} {
			if strings.Contains(got, forbid) {
				t.Errorf("BundleMessage(\"cli\", \"\") = %q, must not contain %q", got, forbid)
			}
		}
	})
}
