package updatepresent

import (
	"testing"

	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/userprefs"
)

// W3-15: turning Applications off in Preferences left its Updates row saying
// "Up to date" until a manual Refresh.
func TestRecheckForPreferences(t *testing.T) {
	on := userprefs.Values{Applications: true}
	off := userprefs.Values{}
	apps := func(enabled bool) []updateflow.SourceState {
		return []updateflow.SourceState{{ID: updateflow.Applications, Configured: true, Available: true, Enabled: enabled}}
	}
	tests := []struct {
		name        string
		snapshot    updateflow.Snapshot
		preferences userprefs.Values
		want        bool
	}{
		{"shown snapshot matches", updateflow.Snapshot{Phase: updateflow.PhaseReady, Sources: apps(true)}, on, false},
		{"source turned off", updateflow.Snapshot{Phase: updateflow.PhaseReady, Sources: apps(true)}, off, true},
		{"source turned on", updateflow.Snapshot{Phase: updateflow.PhaseReady, Sources: apps(false)}, on, true},
		{"check in flight before sources are known", updateflow.Snapshot{Phase: updateflow.PhaseChecking}, on, true},
		{"administrator-disabled source", updateflow.Snapshot{Phase: updateflow.PhaseReady, Sources: []updateflow.SourceState{{ID: updateflow.Applications, Available: true}}}, on, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RecheckForPreferences(tc.snapshot, tc.preferences); got != tc.want {
				t.Errorf("RecheckForPreferences = %v, want %v", got, tc.want)
			}
		})
	}
}

// A preference changed while a check ran must be picked up when the check
// returns, but a newer check still in flight must not be superseded.
func TestRecheckAfterCheck(t *testing.T) {
	on := userprefs.Values{Applications: true}
	off := userprefs.Values{}
	apps := func(enabled bool) []updateflow.SourceState {
		return []updateflow.SourceState{{ID: updateflow.Applications, Configured: true, Available: true, Enabled: enabled}}
	}
	tests := []struct {
		name        string
		snapshot    updateflow.Snapshot
		preferences userprefs.Values
		want        bool
	}{
		{"finished check matches", updateflow.Snapshot{Phase: updateflow.PhaseReady, Sources: apps(true)}, on, false},
		{"turned off during check", updateflow.Snapshot{Phase: updateflow.PhaseReady, Sources: apps(true)}, off, true},
		{"turned on during check", updateflow.Snapshot{Phase: updateflow.PhaseReady, Sources: apps(false)}, on, true},
		{"newer check in flight", updateflow.Snapshot{Phase: updateflow.PhaseChecking, Sources: apps(true)}, off, false},
		{"newer check before sources are known", updateflow.Snapshot{Phase: updateflow.PhaseChecking}, on, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RecheckAfterCheck(tc.snapshot, tc.preferences); got != tc.want {
				t.Errorf("RecheckAfterCheck = %v, want %v", got, tc.want)
			}
		})
	}
}

// The shared lock reason is the subtitle the Updates row shows, and an
// operable source has none whatever its preference.
func TestSourceLockReason(t *testing.T) {
	tests := []struct {
		state updateflow.SourceState
		want  string
	}{
		{updateflow.SourceState{ID: updateflow.Applications, Available: true}, "Disabled by administrator"},
		{updateflow.SourceState{ID: updateflow.Applications}, "Disabled by administrator"},
		{updateflow.SourceState{ID: updateflow.Applications, Configured: true}, "Not available on this computer"},
		{updateflow.SourceState{ID: updateflow.Applications, Configured: true, Available: true}, ""},
		{updateflow.SourceState{ID: updateflow.Applications, Configured: true, Available: true, Enabled: true}, ""},
	}
	for _, tc := range tests {
		if got := SourceLockReason(tc.state); got != tc.want {
			t.Errorf("SourceLockReason(%+v) = %q, want %q", tc.state, got, tc.want)
		}
		if tc.want == "" {
			continue
		}
		if _, subtitle := Source(tc.state); subtitle != tc.want {
			t.Errorf("Source(%+v) subtitle = %q, want the lock reason %q", tc.state, subtitle, tc.want)
		}
	}
}
