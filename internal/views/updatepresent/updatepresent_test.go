package updatepresent

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/updateflow"
)

func TestSnapshotMapsAggregateStates(t *testing.T) {
	tests := []struct {
		name  string
		state updateflow.Snapshot
		want  Presentation
	}{
		{
			name: "idle",
			state: updateflow.Snapshot{
				Phase: updateflow.PhaseIdle,
			},
			want: Presentation{Title: "Checking for updates",
				Description: "Preparing to check for updates…"},
		},
		{
			name: "checking",
			state: updateflow.Snapshot{
				Phase:    updateflow.PhaseChecking,
				Current:  updateflow.Applications,
				Progress: "Loading application updates…",
			},
			want: Presentation{Title: "Checking for updates",
				Description: "Applications: Loading application updates…"},
		},
		{
			name: "up to date",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhaseReady,
				Action: updateflow.ActionCheck,
			},
			want: Presentation{Title: "System is up to date",
				Description: "No updates are available.",
				ActionLabel: "Check again",
				ShowAction:  true,
				ActionStyle: "suggested-action"},
		},
		{
			name: "no configured sources",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhaseReady,
				Action: updateflow.ActionNone,
			},
			want: Presentation{Title: "System is up to date",
				Description: "No update sources are available."},
		},
		{
			name: "updates available",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhaseReady,
				Action: updateflow.ActionUpdateAll,
				Sources: []updateflow.SourceState{
					{
						ID:         updateflow.Applications,
						Configured: true,
						Available:  true,
						Enabled:    true,
						Items: []updateflow.Item{
							{Name: "org.example.App"},
							{Name: "org.example.Other"},
						},
					},
				},
				TotalUpdates: 2,
			},
			want: Presentation{Title: "Updates available",
				Description: "2 updates are available.",
				ActionLabel: "Update all",
				ShowAction:  true,
				ActionStyle: "suggested-action"},
		},
		{
			name: "check failed",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhaseCheckFailed,
				Action: updateflow.ActionCheck,
				Sources: []updateflow.SourceState{
					{
						ID:       updateflow.DeveloperTools,
						CheckErr: errors.New("network unavailable"),
					},
				},
			},
			want: Presentation{Title: "Unable to check for updates",
				Description: "Developer tools: network unavailable",
				ActionLabel: "Try again",
				ShowAction:  true,
				ActionStyle: "suggested-action",
				Banner:      "Unable to check for updates"},
		},
		{
			name: "updating",
			state: updateflow.Snapshot{
				Phase:    updateflow.PhaseUpdating,
				Current:  updateflow.DeveloperTools,
				Progress: "Installing packages…",
			},
			want: Presentation{Title: "Installing updates",
				Description: "Developer tools: Installing packages…"},
		},
		{
			name: "partial failure",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhasePartialFailure,
				Action: updateflow.ActionRetryFailed,
				CompletedSources: []updateflow.SourceID{
					updateflow.Applications,
				},
				FailedSources: []updateflow.SourceID{
					updateflow.DeveloperTools,
					updateflow.OperatingSystem,
				},
				Sources: []updateflow.SourceState{
					{ID: updateflow.Applications, Completed: true},
					{ID: updateflow.DeveloperTools, ApplyErr: errors.New("permission denied")},
					{ID: updateflow.OperatingSystem, ApplyErr: errors.New("staging failed")},
				},
			},
			want: Presentation{Title: "Some updates could not be installed",
				Description: "1 source completed; 2 sources failed.",
				ActionLabel: "Retry failed",
				ShowAction:  true,
				ActionStyle: "suggested-action",
				Banner:      "Some updates could not be installed"},
		},
		{
			name: "maintenance failure",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhasePartialFailure,
				Action: updateflow.ActionRetryFailed,
				CompletedSources: []updateflow.SourceID{
					updateflow.Applications,
				},
				MaintenanceErr: errors.New("cleanup failed"),
			},
			want: Presentation{Title: "Some updates could not be installed",
				Description: "Updates completed, but maintenance failed: cleanup failed",
				ActionLabel: "Retry failed",
				ShowAction:  true,
				ActionStyle: "suggested-action",
				Banner:      "Maintenance failed"},
		},
		{
			name: "restart required",
			state: updateflow.Snapshot{
				Phase: updateflow.PhaseRestartRequired,
				Sources: []updateflow.SourceState{
					{
						ID:              updateflow.OperatingSystem,
						RestartRequired: true,
					},
				},
			},
			// The restart state lives on the Operating system row, not on
			// the page-level status panel; the panel clears so the wordmark
			// leads straight into the "System updates" group. Only the
			// announcement survives, so a screen reader still reports the
			// pending reboot.
			want: Presentation{Announcement: "Deployment staged"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Snapshot(tt.state); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Snapshot() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSourceMapsSourceState(t *testing.T) {
	tests := []struct {
		name  string
		state updateflow.SourceState
		title string
		sub   string
	}{
		{
			name: "applications",
			state: updateflow.SourceState{
				ID:         updateflow.Applications,
				Configured: true,
				Available:  true,
				Enabled:    true,
			},
			title: "Applications",
			sub:   "Up to date",
		},
		{
			name: "administrator disabled",
			state: updateflow.SourceState{
				ID:         updateflow.DeveloperTools,
				Configured: false,
			},
			title: "Developer tools",
			sub:   "Disabled by administrator",
		},
		{
			name: "runtime unavailable",
			state: updateflow.SourceState{
				ID:         updateflow.SystemComponents,
				Configured: true,
				Available:  false,
			},
			title: "System components",
			sub:   "Not available on this system",
		},
		{
			name: "preferences disabled",
			state: updateflow.SourceState{
				ID:         updateflow.Applications,
				Configured: true,
				Available:  true,
				Enabled:    false,
			},
			title: "Applications",
			sub:   "Disabled in preferences",
		},
		{
			name: "checking",
			state: updateflow.SourceState{
				ID:         updateflow.OperatingSystem,
				Configured: true,
				Available:  true,
				Enabled:    true,
				Checking:   true,
			},
			title: "Operating system",
			sub:   "Checking for updates…",
		},
		{
			name: "updates",
			state: updateflow.SourceState{
				ID:         updateflow.Applications,
				Configured: true,
				Available:  true,
				Enabled:    true,
				Items:      []updateflow.Item{{Name: "one"}, {Name: "two"}},
			},
			title: "Applications",
			sub:   "2 updates available",
		},
		{
			name: "restart",
			state: updateflow.SourceState{
				ID:              updateflow.OperatingSystem,
				Configured:      true,
				Available:       true,
				Enabled:         true,
				RestartRequired: true,
			},
			title: "Operating system",
			sub:   "Deployment staged",
		},
		{
			name: "check error",
			state: updateflow.SourceState{
				ID:         updateflow.DeveloperTools,
				Configured: true,
				Available:  true,
				Enabled:    true,
				CheckErr:   errors.New("offline"),
			},
			title: "Developer tools",
			sub:   "Check failed: offline",
		},
		{
			name: "apply error",
			state: updateflow.SourceState{
				ID:         updateflow.OperatingSystem,
				Configured: true,
				Available:  true,
				Enabled:    true,
				ApplyErr:   errors.New("staging failed"),
			},
			title: "Operating system",
			sub:   "Update failed: staging failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, subtitle := Source(tt.state)
			if title != tt.title || subtitle != tt.sub {
				t.Fatalf("Source() = %q, %q; want %q, %q", title, subtitle, tt.title, tt.sub)
			}
		})
	}
}

// The Operating system source's one pending item is the deployment itself:
// its versions go on the source row and no child row repeats the source's
// name. VM finding: "Operating system — 1 update available" followed by an
// "Operating System" child row with "20261006 → 20261007". The fold is keyed
// on the source ID, never the item's name: a lone app or formula that happens
// to share its source's title keeps its row, which carries its only Update
// button (Hive review on #539).
func TestSourceFoldsSoleOperatingSystemItem(t *testing.T) {
	osItem := updateflow.Item{Name: "Operating system", CurrentVersion: "20261006", AvailableVersion: "20261007", Scope: "bootc"}
	apps := []updateflow.Item{{Name: "Firefox"}, {Name: "GIMP"}}
	tests := []struct {
		name     string
		id       updateflow.SourceID
		items    []updateflow.Item
		sub      string
		itemRows []updateflow.Item
	}{
		{"os version transition", updateflow.OperatingSystem, []updateflow.Item{osItem}, "Update available: 20261006 → 20261007", nil},
		{"os legacy capitalization", updateflow.OperatingSystem, []updateflow.Item{{Name: "Operating System", CurrentVersion: "1", AvailableVersion: "2"}}, "Update available: 1 → 2", nil},
		{"os available version only", updateflow.OperatingSystem, []updateflow.Item{{Name: "Operating system", AvailableVersion: "20261007"}}, "Update available: 20261007", nil},
		{"os without versions", updateflow.OperatingSystem, []updateflow.Item{{Name: "Operating system"}}, "1 update available", nil},
		{"single differently named app", updateflow.Applications, []updateflow.Item{{Name: "Firefox", CurrentVersion: "1", AvailableVersion: "2"}}, "1 update available", []updateflow.Item{{Name: "Firefox", CurrentVersion: "1", AvailableVersion: "2"}}},
		{"single app named like its source", updateflow.Applications, []updateflow.Item{{Name: "Applications", CurrentVersion: "1", AvailableVersion: "2"}}, "1 update available", []updateflow.Item{{Name: "Applications", CurrentVersion: "1", AvailableVersion: "2"}}},
		{"single tool named like its source", updateflow.DeveloperTools, []updateflow.Item{{Name: "Developer tools"}}, "1 update available", []updateflow.Item{{Name: "Developer tools"}}},
		{"single component named like its source", updateflow.SystemComponents, []updateflow.Item{{Name: "System components"}}, "1 update available", []updateflow.Item{{Name: "System components"}}},
		{"multiple apps", updateflow.Applications, apps, "2 updates available", apps},
		{"multiple developer tools", updateflow.DeveloperTools, apps, "2 updates available", apps},
		{"two operating system items", updateflow.OperatingSystem, []updateflow.Item{osItem, osItem}, "2 updates available", []updateflow.Item{osItem, osItem}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := updateflow.SourceState{ID: tt.id, Configured: true, Available: true, Enabled: true, Items: tt.items}
			if _, sub := Source(state); sub != tt.sub {
				t.Fatalf("Source() subtitle = %q, want %q", sub, tt.sub)
			}
			if got := ItemRows(state); !slices.Equal(got, tt.itemRows) {
				t.Fatalf("ItemRows() = %#v, want %#v", got, tt.itemRows)
			}
		})
	}
}

func TestSnapshotUsesSingularUpdateText(t *testing.T) {
	got := Snapshot(updateflow.Snapshot{
		Phase:        updateflow.PhaseReady,
		Action:       updateflow.ActionUpdateAll,
		TotalUpdates: 1,
	})
	if got.Description != "1 update is available." {
		t.Fatalf("Snapshot() description = %q, want %q", got.Description, "1 update is available.")
	}
}

func TestSnapshotShowsLastCheckedTimeWhenCurrent(t *testing.T) {
	got := Snapshot(updateflow.Snapshot{
		Phase:       updateflow.PhaseReady,
		Action:      updateflow.ActionCheck,
		LastChecked: time.Date(2026, 9, 7, 14, 5, 0, 0, time.UTC),
	})
	if got.Description != "No updates are available. Last checked at 14:05." {
		t.Fatalf("Snapshot() description = %q, want %q", got.Description, "No updates are available. Last checked at 14:05.")
	}
}

func TestSourceIconMapsProviderKinds(t *testing.T) {
	tests := []struct {
		id   updateflow.SourceID
		want string
	}{
		{updateflow.Applications, "applications-system-symbolic"},
		{updateflow.DeveloperTools, "package-x-generic-symbolic"},
		{updateflow.SystemComponents, "applications-system-symbolic"},
		{updateflow.OperatingSystem, "drive-harddisk-system-symbolic"},
		{"unknown", "software-update-available-symbolic"},
	}
	for _, tt := range tests {
		if got := SourceIcon(tt.id); got != tt.want {
			t.Fatalf("SourceIcon(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestVersionDetailsMappedByItemSubtitle(t *testing.T) {
	tests := []struct {
		item updateflow.Item
		want string
	}{
		{updateflow.Item{CurrentVersion: "1", AvailableVersion: "2"}, "1 → 2"},
		{updateflow.Item{AvailableVersion: "2"}, "Available: 2"},
		{updateflow.Item{CurrentVersion: "1"}, "Installed: 1"},
		{updateflow.Item{}, "Update available"},
	}
	for _, tt := range tests {
		if got := ItemSubtitle(tt.item); got != tt.want {
			t.Fatalf("ItemSubtitle(%#v) = %q, want %q", tt.item, got, tt.want)
		}
	}
}

func TestShowProgressOnlyDuringOperations(t *testing.T) {
	for _, phase := range []updateflow.Phase{
		updateflow.PhaseIdle,
		updateflow.PhaseReady,
		updateflow.PhaseCheckFailed,
		updateflow.PhasePartialFailure,
		updateflow.PhaseRestartRequired,
	} {
		if ShowProgress(phase) {
			t.Fatalf("ShowProgress(%v) = true, want false", phase)
		}
	}

	for _, phase := range []updateflow.Phase{
		updateflow.PhaseChecking,
		updateflow.PhaseUpdating,
	} {
		if !ShowProgress(phase) {
			t.Fatalf("ShowProgress(%v) = false, want true", phase)
		}
	}
}

func TestCanStartOperationRejectsBusyOrClosedShell(t *testing.T) {
	tests := []struct {
		name   string
		busy   bool
		closed bool
		want   bool
	}{
		{name: "ready", want: true},
		{name: "busy", busy: true},
		{name: "closed", closed: true},
		{name: "busy and closed", busy: true, closed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanStartOperation(tt.busy, tt.closed); got != tt.want {
				t.Fatalf("CanStartOperation(%t, %t) = %t, want %t", tt.busy, tt.closed, got, tt.want)
			}
		})
	}
}

func TestPrimaryActionEnabled(t *testing.T) {
	tests := []struct {
		name            string
		showAction      bool
		busy            bool
		closed          bool
		restartInFlight bool
		want            bool
	}{
		{name: "ready to restart", showAction: true, want: true},
		{name: "no action", showAction: false, want: false},
		{name: "busy", busy: true, showAction: true, want: false},
		{name: "closed", closed: true, showAction: true, want: false},
		{name: "restart in flight", restartInFlight: true, showAction: true, want: false},
		{name: "busy and restart in flight", busy: true, restartInFlight: true, showAction: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrimaryActionEnabled(tt.showAction, tt.busy, tt.closed, tt.restartInFlight); got != tt.want {
				t.Fatalf("PrimaryActionEnabled(%t, %t, %t, %t) = %t, want %t",
					tt.showAction, tt.busy, tt.closed, tt.restartInFlight, got, tt.want)
			}
		})
	}
}

func TestShouldPublishRejectsClosedShell(t *testing.T) {
	if !ShouldPublish(false) {
		t.Fatal("ShouldPublish(false) = false, want true")
	}
	if ShouldPublish(true) {
		t.Fatal("ShouldPublish(true) = true, want false")
	}
}

func TestSourceSubtitleLinesTracksCompactMode(t *testing.T) {
	if got := SourceSubtitleLines(false); got != 2 {
		t.Fatalf("SourceSubtitleLines(false) = %d, want 2", got)
	}
	if got := SourceSubtitleLines(true); got != 1 {
		t.Fatalf("SourceSubtitleLines(true) = %d, want 1", got)
	}
}

func TestSourceItemTitleUsesIdentityOnlyWhenNameIsMissing(t *testing.T) {
	if got := ItemTitle(updateflow.Item{ID: "org.mozilla.firefox", Name: "Firefox"}); got != "Firefox" {
		t.Fatalf("named app title = %q, want its display name", got)
	}
	if got := ItemTitle(updateflow.Item{ID: "org.mozilla.firefox"}); got != "org.mozilla.firefox" {
		t.Fatalf("nameless app title = %q, want its identity", got)
	}
}

// A phase change announces to screen readers, so no phase may map to empty
// announcement text. PhaseRestartRequired clears the status panel's title,
// which would otherwise announce "" and leave a pending reboot silent.
func TestEveryPhaseAnnouncesSomething(t *testing.T) {
	for _, phase := range []updateflow.Phase{
		updateflow.PhaseIdle,
		updateflow.PhaseChecking,
		updateflow.PhaseReady,
		updateflow.PhaseCheckFailed,
		updateflow.PhaseUpdating,
		updateflow.PhasePartialFailure,
		updateflow.PhaseRestartRequired,
	} {
		if got := Snapshot(updateflow.Snapshot{Phase: phase}).Announce(); got == "" {
			t.Errorf("Snapshot(%v).Announce() is empty; a phase change would announce nothing", phase)
		}
	}
}

func TestAnnouncePrefersTitleWhenThePanelHasOne(t *testing.T) {
	if got := (Presentation{Title: "Updates available"}).Announce(); got != "Updates available" {
		t.Fatalf("Announce() = %q, want the title", got)
	}
	if got := (Presentation{Title: "Updates available", Announcement: "Deployment staged"}).Announce(); got != "Deployment staged" {
		t.Fatalf("Announce() = %q, want the explicit announcement", got)
	}
}

func TestStatusPanelKeepsVisibleContent(t *testing.T) {
	tests := []struct {
		name         string
		presentation Presentation
		phase        updateflow.Phase
		want         bool
	}{
		{name: "empty panel", phase: updateflow.PhaseReady},
		{name: "announcement only", presentation: Presentation{Announcement: "Deployment staged"}, phase: updateflow.PhaseRestartRequired},
		{name: "banner outside panel", presentation: Presentation{Banner: "Check failed"}, phase: updateflow.PhaseCheckFailed},
		{name: "title", presentation: Presentation{Title: "Updates available"}, phase: updateflow.PhaseReady, want: true},
		{name: "description", presentation: Presentation{Description: "Permission denied"}, phase: updateflow.PhaseCheckFailed, want: true},
		{name: "action", presentation: Presentation{ShowAction: true}, phase: updateflow.PhaseReady, want: true},
		{name: "checking progress", phase: updateflow.PhaseChecking, want: true},
		{name: "installing progress", phase: updateflow.PhaseUpdating, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.presentation.ShowStatus(tt.phase); got != tt.want {
				t.Fatalf("ShowStatus(%v) = %t, want %t", tt.phase, got, tt.want)
			}
		})
	}
}

func TestStatusPanelReturnsAfterStagedDeployment(t *testing.T) {
	for _, phase := range []updateflow.Phase{
		updateflow.PhaseIdle,
		updateflow.PhaseChecking,
		updateflow.PhaseReady,
		updateflow.PhaseCheckFailed,
		updateflow.PhaseUpdating,
		updateflow.PhasePartialFailure,
	} {
		staged := Snapshot(updateflow.Snapshot{Phase: updateflow.PhaseRestartRequired})
		if staged.ShowStatus(updateflow.PhaseRestartRequired) {
			t.Fatal("staged deployment leaves an empty status panel visible")
		}
		if staged.Announce() != "Deployment staged" {
			t.Fatal("collapsing the staged panel drops the restart announcement")
		}
		if !Snapshot(updateflow.Snapshot{Phase: phase}).ShowStatus(phase) {
			t.Fatalf("phase %v stays hidden after leaving the staged state", phase)
		}
	}
}
