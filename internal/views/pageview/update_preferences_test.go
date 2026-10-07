package pageview

import (
	"testing"

	"github.com/projectbluefin/chairlift/internal/updateflow"
)

func TestUpdateSourcePreferenceRowsExplainWhyASwitchIsLocked(t *testing.T) {
	id := updateflow.Applications
	cases := []struct {
		name          string
		states        []updateflow.SourceState
		ready         bool
		wantSubtitle  string
		wantSensitive bool
	}{
		{"before the first check", nil, false, "Checking availability…", false},
		{"a source the shell has not reported", []updateflow.SourceState{{ID: updateflow.OperatingSystem, Configured: true, Available: true}}, true, "Checking availability…", false},
		{"disabled by the administrator", []updateflow.SourceState{{ID: id, Configured: false, Available: true}}, true, "Disabled by your administrator", false},
		{"not backed by this host", []updateflow.SourceState{{ID: id, Configured: true, Available: false}}, true, "Not available on this computer", false},
		{"operable", []updateflow.SourceState{{ID: id, Configured: true, Available: true}}, true, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UpdateSourcePreferenceSubtitle(tc.states, tc.ready, id); got != tc.wantSubtitle {
				t.Errorf("subtitle = %q, want %q", got, tc.wantSubtitle)
			}
			if got := UpdateSourcePreferenceSensitive(tc.states, tc.ready, id); got != tc.wantSensitive {
				t.Errorf("sensitive = %v, want %v", got, tc.wantSensitive)
			}
		})
	}
}
