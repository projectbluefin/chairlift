package pageview

import (
	"errors"
	"testing"

	"github.com/projectbluefin/chairlift/internal/shellextensions"
)

// TestDesktopExtensionRowNeverShowsAnUnobservedEnable holds every catalog
// entry, including Tailscale whose catalog default is on, to GNOME's observed
// state: a missing extension or unreadable extension list shows off.
func TestDesktopExtensionRowNeverShowsAnUnobservedEnable(t *testing.T) {
	for _, extension := range shellextensions.Catalog() {
		cases := []struct {
			name   string
			states map[string]shellextensions.State
			err    error
			want   DesktopIntegrationRow
		}{
			{
				name: "load error",
				err:  errors.New("gnome-extensions: not found"),
				want: DesktopIntegrationRow{Subtitle: "Only available on the GNOME desktop."},
			},
			{
				name:   "not installed",
				states: map[string]shellextensions.State{},
				want:   DesktopIntegrationRow{Subtitle: "Not installed on this computer."},
			},
			{
				name:   "installed and enabled",
				states: map[string]shellextensions.State{extension.UUID: {Installed: true, Enabled: true}},
				want:   DesktopIntegrationRow{Active: true, Sensitive: true, Subtitle: extension.Description},
			},
			{
				name:   "installed and disabled",
				states: map[string]shellextensions.State{extension.UUID: {Installed: true}},
				want:   DesktopIntegrationRow{Sensitive: true, Subtitle: extension.Description},
			},
		}
		for _, tc := range cases {
			if got := DesktopIntegration(extension, tc.states, tc.err); got != tc.want {
				t.Errorf("%s %s: DesktopIntegration() = %+v, want %+v", extension.UUID, tc.name, got, tc.want)
			}
		}
	}
}
