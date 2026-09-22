package updatepresent

import (
	"errors"
	"reflect"
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
			want: Presentation{
				Icon:        "view-refresh-symbolic",
				Title:       "Checking for updates",
				Description: "Preparing to check for updates…",
			},
		},
		{
			name: "checking",
			state: updateflow.Snapshot{
				Phase:    updateflow.PhaseChecking,
				Current:  updateflow.Applications,
				Progress: "Loading application updates…",
			},
			want: Presentation{
				Icon:        "view-refresh-symbolic",
				Title:       "Checking for updates",
				Description: "Applications: Loading application updates…",
			},
		},
		{
			name: "up to date",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhaseReady,
				Action: updateflow.ActionCheck,
			},
			want: Presentation{
				Icon:        "emblem-system-symbolic",
				Title:       "System is up to date",
				Description: "No updates are available.",
				ActionLabel: "Check again",
				ShowAction:  true,
				ActionStyle: "suggested-action",
			},
		},
		{
			name: "no configured sources",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhaseReady,
				Action: updateflow.ActionNone,
			},
			want: Presentation{
				Icon:        "emblem-system-symbolic",
				Title:       "System is up to date",
				Description: "No update sources are available.",
			},
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
			want: Presentation{
				Icon:        "software-update-available-symbolic",
				Title:       "Updates available",
				Description: "2 updates are available.",
				ActionLabel: "Update all",
				ShowAction:  true,
				ActionStyle: "suggested-action",
			},
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
			want: Presentation{
				Icon:        "network-error-symbolic",
				Title:       "Unable to check for updates",
				Description: "Developer tools: network unavailable",
				ActionLabel: "Try again",
				ShowAction:  true,
				ActionStyle: "suggested-action",
				Banner:      "Unable to check for updates",
			},
		},
		{
			name: "updating",
			state: updateflow.Snapshot{
				Phase:    updateflow.PhaseUpdating,
				Current:  updateflow.DeveloperTools,
				Progress: "Installing packages…",
			},
			want: Presentation{
				Icon:        "content-loading-symbolic",
				Title:       "Installing updates",
				Description: "Developer tools: Installing packages…",
			},
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
			want: Presentation{
				Icon:        "dialog-warning-symbolic",
				Title:       "Some updates could not be installed",
				Description: "1 source completed; 2 sources failed.",
				ActionLabel: "Retry failed",
				ShowAction:  true,
				ActionStyle: "suggested-action",
				Banner:      "Some updates could not be installed",
			},
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
			want: Presentation{
				Icon:        "dialog-warning-symbolic",
				Title:       "Some updates could not be installed",
				Description: "Updates completed, but maintenance failed: cleanup failed",
				ActionLabel: "Retry failed",
				ShowAction:  true,
				ActionStyle: "suggested-action",
				Banner:      "Maintenance failed",
			},
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
			want: Presentation{
				Icon:        "system-reboot-symbolic",
				Title:       "Restart required",
				Description: "Restart to finish installing updates.",
				Banner:      "Restart required",
			},
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
			sub:   "Restart required",
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

func TestSourceHasDetailsFollowsPendingItems(t *testing.T) {
	if SourceHasDetails(updateflow.SourceState{}) {
		t.Fatal("SourceHasDetails(empty) = true, want false")
	}
	if !SourceHasDetails(updateflow.SourceState{Items: []updateflow.Item{{Name: "update"}}}) {
		t.Fatal("SourceHasDetails(with item) = false, want true")
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
