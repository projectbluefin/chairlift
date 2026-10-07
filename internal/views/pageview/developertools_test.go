package pageview

import (
	"strings"
	"testing"
)

func TestDeveloperToolEnumeratesEveryObservation(t *testing.T) {
	const name, description = "Micro", "Terminal text editor."
	tests := []struct {
		label       string
		observation DeveloperToolObservation
		subtitle    string
		button      string
		accessible  string
		allowed     bool
	}{
		{"unsupported", DeveloperToolObservation{Homebrew: true}, "Not available for this architecture.", "Install", "Install Micro", false},
		{"no homebrew", DeveloperToolObservation{Supported: true}, "Needs Homebrew.", "Install", "Install Micro", false},
		{"read failed", DeveloperToolObservation{Supported: true, Homebrew: true, ReadFailed: true, Installed: true}, "Could not check installed state", "Install", "Install Micro", false},
		{"installed", DeveloperToolObservation{Supported: true, Homebrew: true, Installed: true}, "Installed through Homebrew.", "Installed", "Micro installed", false},
		// The stale-row defect: the same row observed absent after an
		// uninstall elsewhere must offer the install again.
		{"absent", DeveloperToolObservation{Supported: true, Homebrew: true}, "Installs through Homebrew when you choose it.", "Install", "Install Micro", true},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got := DeveloperTool(name, description, tt.observation)
			if !strings.HasPrefix(got.Subtitle, description+" ") || !strings.Contains(got.Subtitle, tt.subtitle) {
				t.Errorf("subtitle %q, want description then %q", got.Subtitle, tt.subtitle)
			}
			if got.ButtonLabel != tt.button || got.AccessibleLabel != tt.accessible || got.Allowed != tt.allowed {
				t.Errorf("got %+v, want button %q accessible %q allowed %v", got, tt.button, tt.accessible, tt.allowed)
			}
		})
	}
}

func TestDeveloperToolTransientStatesNameTheTool(t *testing.T) {
	const name, description = "Helix", "Modal editor."
	for label, view := range map[string]DeveloperToolView{
		"checking":   DeveloperToolChecking(name, description),
		"installing": DeveloperToolInstalling(name),
		"unverified": DeveloperToolUnverified(name, description),
	} {
		if !strings.Contains(view.AccessibleLabel, name) {
			t.Errorf("%s accessible label %q does not name the tool", label, view.AccessibleLabel)
		}
		if view.ButtonLabel == "" || view.Subtitle == "" {
			t.Errorf("%s view is incomplete: %+v", label, view)
		}
	}
	if DeveloperToolChecking(name, description).Allowed || DeveloperToolInstalling(name).Allowed {
		t.Error("an unknown or in-flight tool offers an install")
	}
	if !DeveloperToolUnverified(name, description).Allowed {
		t.Error("an unverified install no longer offers a retry")
	}
}
