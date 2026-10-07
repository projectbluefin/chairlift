package pageview

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/views/updatepresent"
)

func TestUpdateSourcePreferenceRowsExplainWhyASwitchIsLocked(t *testing.T) {
	id := updateflow.Applications
	cases := []struct {
		name          string
		states        []updateflow.SourceState
		ready         bool
		wantSubtitle  string
		wantSensitive bool
		wantLocked    bool
	}{
		{"before the first check", nil, false, "Checking availability…", false, false},
		{"a source the shell has not reported", []updateflow.SourceState{{ID: updateflow.OperatingSystem, Configured: true, Available: true}}, true, "Checking availability…", false, false},
		// W3-05: the switch showed ON beside this subtitle, and the words
		// differed from the Updates page's row for the same source.
		{"disabled by the administrator", []updateflow.SourceState{{ID: id, Configured: false, Available: true}}, true, "Disabled by administrator", false, true},
		{"not backed by this host", []updateflow.SourceState{{ID: id, Configured: true, Available: false}}, true, "Not available on this computer", false, true},
		{"operable", []updateflow.SourceState{{ID: id, Configured: true, Available: true}}, true, "", true, false},
		{"operable but turned off", []updateflow.SourceState{{ID: id, Configured: true, Available: true, Enabled: false}}, true, "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UpdateSourcePreferenceSubtitle(tc.states, tc.ready, id); got != tc.wantSubtitle {
				t.Errorf("subtitle = %q, want %q", got, tc.wantSubtitle)
			}
			if got := UpdateSourcePreferenceSensitive(tc.states, tc.ready, id); got != tc.wantSensitive {
				t.Errorf("sensitive = %v, want %v", got, tc.wantSensitive)
			}
			if got := UpdateSourcePreferenceLocked(tc.states, tc.ready, id); got != tc.wantLocked {
				t.Errorf("locked = %v, want %v", got, tc.wantLocked)
			}
		})
	}
}

// Preferences and the Updates page must describe a locked source in the same
// words, for every source.
func TestUpdateSourcePreferenceSubtitleMatchesTheUpdatesRow(t *testing.T) {
	for _, preference := range UpdateSourcePreferences {
		for _, state := range []updateflow.SourceState{
			{ID: preference.ID, Configured: false, Available: true},
			{ID: preference.ID, Configured: false, Available: false},
			{ID: preference.ID, Configured: true, Available: false},
		} {
			_, row := updatepresent.Source(state)
			got := UpdateSourcePreferenceSubtitle([]updateflow.SourceState{state}, true, preference.ID)
			if got != row {
				t.Errorf("%s %+v: preferences say %q, Updates row says %q", preference.ID, state, got, row)
			}
		}
	}
}

// The "Run maintenance after updates" switch was always bound and sensitive,
// so with maintenance_freespace_group disabled it offered an ON switch for a
// post-update phase that skips every step. It is locked with the same words
// as an administrator-disabled source.
func TestMaintenancePreferenceLockedWhenCleanupIsDisabled(t *testing.T) {
	if got := MaintenancePreferenceLockReason(true); got != "" {
		t.Errorf("configured cleanup is locked: %q", got)
	}
	want := UpdateSourcePreferenceSubtitle(
		[]updateflow.SourceState{{ID: updateflow.Applications, Configured: false, Available: true}},
		true, updateflow.Applications,
	)
	if got := MaintenancePreferenceLockReason(false); got == "" || got != want {
		t.Errorf("disabled cleanup reason = %q, want %q", got, want)
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate update_preferences_test.go")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", "window", "preferences.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	lock := strings.Index(text, "pageview.MaintenancePreferenceLockReason(updateproviders.CleanupConfigured(w.config))")
	bind := strings.Index(text, `store.BindBoolean("maintenance-after-updates"`)
	if lock < 0 || bind < 0 || bind < lock {
		t.Error("buildPreferences binds the maintenance preference without first checking that cleanup is configured")
	}
}
