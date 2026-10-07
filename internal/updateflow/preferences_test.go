package updateflow

import (
	"context"
	"testing"

	"github.com/projectbluefin/chairlift/internal/userprefs"
)

// A preference toggled while the Updates page shows a snapshot left the
// source's row reading "Up to date" until a manual Refresh (W3-15). StaleFor
// is how the shell notices that the snapshot no longer matches.
func TestSnapshotStaleForPreferenceChanges(t *testing.T) {
	all := userprefs.Values{OperatingSystem: true, Applications: true, DeveloperTools: true, SystemComponents: true}
	withoutApps := all
	withoutApps.Applications = false

	tests := []struct {
		name        string
		sources     []SourceState
		preferences userprefs.Values
		want        bool
	}{
		{"no sources", nil, all, false},
		{"enabled source still preferred", []SourceState{{ID: Applications, Configured: true, Available: true, Enabled: true}}, all, false},
		{"enabled source turned off", []SourceState{{ID: Applications, Configured: true, Available: true, Enabled: true}}, withoutApps, true},
		{"disabled source turned on", []SourceState{{ID: Applications, Configured: true, Available: true}}, all, true},
		{"disabled source still off", []SourceState{{ID: Applications, Configured: true, Available: true}}, withoutApps, false},
		{"administrator-disabled source ignores preference", []SourceState{{ID: Applications, Available: true}}, all, false},
		{"unavailable source ignores preference", []SourceState{{ID: Applications, Configured: true}}, all, false},
		{"another source changed", []SourceState{
			{ID: OperatingSystem, Configured: true, Available: true, Enabled: true},
			{ID: DeveloperTools, Configured: true, Available: true, Enabled: true},
			{ID: SystemComponents, Configured: true, Available: true},
		}, all, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Snapshot{Sources: tc.sources}).StaleFor(tc.preferences); got != tc.want {
				t.Errorf("StaleFor = %v, want %v", got, tc.want)
			}
		})
	}
}

// Every source's preference must round-trip: a Check run with some
// preferences yields a snapshot that is not stale for those preferences, and
// is stale once any one source's preference flips. A source preferenceEnabled
// forgot would never trigger the re-check.
func TestCheckedSnapshotIsStaleOnlyAfterAPreferenceFlips(t *testing.T) {
	ids := []SourceID{Applications, DeveloperTools, SystemComponents, OperatingSystem}
	providers := make([]*testProvider, 0, len(ids))
	policy := make(map[SourceID]Policy, len(ids))
	for _, id := range ids {
		providers = append(providers, &testProvider{id: id, available: true})
		policy[id] = Policy{Configured: true, Supported: true}
	}
	preferences := userprefs.Values{OperatingSystem: true, Applications: true, DeveloperTools: true, SystemComponents: true}
	got := New(providerInterfaces(providers), nil).Check(context.Background(), preferences, policy, nil)
	if got.StaleFor(preferences) {
		t.Fatalf("snapshot checked with %+v is stale for it", preferences)
	}

	flips := map[SourceID]func(*userprefs.Values){
		Applications:     func(v *userprefs.Values) { v.Applications = false },
		DeveloperTools:   func(v *userprefs.Values) { v.DeveloperTools = false },
		SystemComponents: func(v *userprefs.Values) { v.SystemComponents = false },
		OperatingSystem:  func(v *userprefs.Values) { v.OperatingSystem = false },
	}
	for _, id := range ids {
		changed := preferences
		flips[id](&changed)
		if !got.StaleFor(changed) {
			t.Errorf("turning %s off did not make the snapshot stale", id)
		}
	}
}
