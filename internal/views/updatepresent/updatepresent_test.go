package updatepresent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strings"
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
			want: Presentation{Status: "Checking for updates…"},
		},
		{
			name: "checking",
			state: updateflow.Snapshot{
				Phase: updateflow.PhaseChecking,
			},
			want: Presentation{Status: "Checking for updates…"},
		},
		{
			name: "up to date",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhaseReady,
				Action: updateflow.ActionCheck,
			},
			want: Presentation{Status: "Up to date",
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
			want: Presentation{Status: "Nothing to update on this computer"},
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
			want: Presentation{Status: "2 updates available",
				ActionLabel: "Update all",
				ShowAction:  true,
				ActionStyle: "suggested-action"},
		},
		{
			name: "updates available with a staged deployment",
			state: updateflow.Snapshot{
				Phase:  updateflow.PhaseReady,
				Action: updateflow.ActionUpdateAll,
				Sources: []updateflow.SourceState{
					{ID: updateflow.OperatingSystem, RestartRequired: true},
				},
				TotalUpdates: 1,
			},
			want: Presentation{Status: "1 update available · restart needed",
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
						CheckErr: errors.New("curl: (6) Could not resolve host: formulae.brew.sh"),
					},
				},
			},
			want: Presentation{Status: "Couldn't check for updates",
				Detail:      "Not checked: Developer tools. Check your internet connection.",
				ActionLabel: "Try again",
				ShowAction:  true,
				ActionStyle: "suggested-action"},
		},
		{
			name: "updating with provider progress",
			state: updateflow.Snapshot{
				Phase:    updateflow.PhaseUpdating,
				Current:  updateflow.DeveloperTools,
				Progress: "Installing packages…",
			},
			want: Presentation{Status: "Developer tools: Installing packages…"},
		},
		{
			name: "updating without provider progress",
			state: updateflow.Snapshot{
				Phase:   updateflow.PhaseUpdating,
				Current: updateflow.Applications,
			},
			want: Presentation{Status: "Applications: installing updates…"},
		},
		{
			// Post-update maintenance: no source is updating, but the run
			// is still cleaning up, so the panel keeps reporting it.
			name: "maintenance before its first step",
			state: updateflow.Snapshot{
				Phase:       updateflow.PhaseUpdating,
				Maintaining: true,
			},
			want: Presentation{Status: "Cleaning up after updates…"},
		},
		{
			name: "maintenance step",
			state: updateflow.Snapshot{
				Phase:       updateflow.PhaseUpdating,
				Maintaining: true,
				Current:     updateflow.DeveloperTools,
				Progress:    "Removing old downloads",
			},
			want: Presentation{Status: "Developer tools: Removing old downloads"},
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
			want: Presentation{Status: "Couldn't install some updates",
				Detail:      "Not updated: Developer tools, Operating system",
				ActionLabel: "Retry failed",
				ShowAction:  true,
				ActionStyle: "suggested-action"},
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
			want: Presentation{Status: "Updates installed, but couldn't clean up",
				Detail:      "Old files are still on this computer. Details are in the log.",
				ActionLabel: "Retry failed",
				ShowAction:  true,
				ActionStyle: "suggested-action"},
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
			// The restart action stays on the Operating system row (#439):
			// the header offers no primary action, only the line saying
			// what is left to do.
			want: Presentation{Status: "Restart to finish updating"},
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
			sub:   "Not available on this computer",
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
			name: "check error from the network",
			state: updateflow.SourceState{
				ID:         updateflow.DeveloperTools,
				Configured: true,
				Available:  true,
				Enabled:    true,
				CheckErr:   &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("network is unreachable")},
			},
			title: "Developer tools",
			sub:   "Couldn't check for updates. Check your internet connection.",
		},
		{
			name: "check error that is not the network",
			state: updateflow.SourceState{
				ID:         updateflow.DeveloperTools,
				Configured: true,
				Available:  true,
				Enabled:    true,
				CheckErr:   errors.New("permission denied @ rb_sysopen - /home/linuxbrew/.linuxbrew/var"),
			},
			title: "Developer tools",
			sub:   "Couldn't check for updates. Details are in the log.",
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
			sub:   "Couldn't update. Details are in the log.",
		},
		{
			name: "apply error that timed out",
			state: updateflow.SourceState{
				ID:         updateflow.Applications,
				Configured: true,
				Available:  true,
				Enabled:    true,
				ApplyErr:   fmt.Errorf("flatpak update timed out: %w", context.DeadlineExceeded),
			},
			title: "Applications",
			sub:   "Couldn't update. It took too long. Try again later.",
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
	if got.Status != "1 update available" {
		t.Fatalf("Snapshot() status = %q, want %q", got.Status, "1 update available")
	}
}

func TestSnapshotShowsLastCheckedTimeWhenCurrent(t *testing.T) {
	got := Snapshot(updateflow.Snapshot{
		Phase:       updateflow.PhaseReady,
		Action:      updateflow.ActionCheck,
		LastChecked: time.Date(2026, 9, 7, 14, 5, 0, 0, time.UTC),
	})
	if !strings.HasPrefix(got.Status, "Up to date") || !strings.Contains(got.Status, "14:05") {
		t.Fatalf("Snapshot() status = %q, want it to say up to date and name 14:05", got.Status)
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

var everyPhase = []updateflow.Phase{
	updateflow.PhaseIdle,
	updateflow.PhaseChecking,
	updateflow.PhaseReady,
	updateflow.PhaseCheckFailed,
	updateflow.PhaseUpdating,
	updateflow.PhasePartialFailure,
	updateflow.PhaseRestartRequired,
}

// The header is wordmark → action → one line. Every phase announces its
// status line to screen readers on a change, so no phase may leave it
// empty, and it must stay one line: a newline would turn the compact
// header back into a title and a description.
func TestEveryPhaseHasOneStatusLine(t *testing.T) {
	for _, phase := range everyPhase {
		got := Snapshot(updateflow.Snapshot{Phase: phase})
		if got.Status == "" {
			t.Errorf("Snapshot(%v).Status is empty; the phase change would announce nothing", phase)
		}
		if strings.Contains(got.Status, "\n") {
			t.Errorf("Snapshot(%v).Status = %q spans more than one line", phase, got.Status)
		}
	}
}

// Only failures may add a second line; the normal states are one line.
func TestOnlyFailuresAddADetailLine(t *testing.T) {
	failure := errors.New("boom")
	sources := []updateflow.SourceState{{ID: updateflow.Applications, CheckErr: failure, ApplyErr: failure}}
	for _, phase := range everyPhase {
		got := Snapshot(updateflow.Snapshot{
			Phase:         phase,
			Sources:       sources,
			FailedSources: []updateflow.SourceID{updateflow.Applications},
		})
		isFailure := phase == updateflow.PhaseCheckFailed || phase == updateflow.PhasePartialFailure
		if isFailure && got.Detail == "" {
			t.Errorf("Snapshot(%v) names no failure in its detail line", phase)
		}
		if !isFailure && got.Detail != "" {
			t.Errorf("Snapshot(%v).Detail = %q, want a single status line", phase, got.Detail)
		}
	}
}

// A failure is told in plain words: the raw error text a tool produced never
// reaches the header or a source row (it is logged by the coordinator).
func TestRawErrorTextNeverReachesTheScreen(t *testing.T) {
	const raw = "error: Unable to load summary from remote flathub: exit status 1"
	failure := errors.New(raw)
	for _, phase := range everyPhase {
		got := Snapshot(updateflow.Snapshot{
			Phase:          phase,
			Sources:        []updateflow.SourceState{{ID: updateflow.Applications, CheckErr: failure, ApplyErr: failure}},
			FailedSources:  []updateflow.SourceID{updateflow.Applications},
			MaintenanceErr: failure,
		})
		if strings.Contains(got.Status, raw) || strings.Contains(got.Detail, raw) {
			t.Errorf("Snapshot(%v) shows the raw error: %#v", phase, got)
		}
	}
	// Maintenance failing alone takes its own branch.
	got := Snapshot(updateflow.Snapshot{Phase: updateflow.PhasePartialFailure, MaintenanceErr: failure})
	if strings.Contains(got.Status, raw) || strings.Contains(got.Detail, raw) {
		t.Errorf("cleanup failure shows the raw error: %#v", got)
	}
	for _, state := range []updateflow.SourceState{
		{ID: updateflow.Applications, Configured: true, Available: true, Enabled: true, CheckErr: failure},
		{ID: updateflow.Applications, Configured: true, Available: true, Enabled: true, ApplyErr: failure},
	} {
		if _, subtitle := Source(state); strings.Contains(subtitle, raw) || subtitle == "" {
			t.Errorf("Source() subtitle = %q, want plain words without the raw error", subtitle)
		}
	}
}

// A failure is told to check the internet connection only when its error
// shows the network was the cause; a local tool failure is not given a
// remedy it may not need (it is pointed at the log, where the raw error is).
func TestFailureHintNamesTheNetworkOnlyWhenTheErrorDoes(t *testing.T) {
	const (
		network = "Check your internet connection."
		timeout = "It took too long. Try again later."
		logged  = "Details are in the log."
	)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"Go dial error", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: network is unreachable")}, network},
		{"wrapped Go DNS error", fmt.Errorf("resolving 43: %w", &net.DNSError{Err: "no such host", Name: "ghcr.io"}), network},
		{"Flatpak cannot resolve the remote", errors.New("Flatpak command failed: error: Unable to load summary from remote flathub: While fetching https://dl.flathub.org/repo/summary.idx: [6] Could not resolve hostname"), network},
		{"Homebrew's curl cannot connect", errors.New("curl: (7) Failed to connect to ghcr.io port 443 after 3 ms: Couldn't connect to server"), network},
		{"bootc cannot reach the registry", errors.New("error: Fetching: error sending request for url (https://ghcr.io/v2/): dns error: failed to lookup address information"), network},
		{"glibc resolver", errors.New("Temporary failure in name resolution"), network},
		{"Flatpak remote without a cause", errors.New("Flatpak command failed: error: Unable to load summary from remote flathub"), logged},
		{"Homebrew local permission error", errors.New("permission denied @ rb_sysopen - /home/linuxbrew/.linuxbrew/var"), logged},
		{"updex incomplete check", errors.New("updex feature check incomplete: feature store unreadable"), logged},
		{"deadline", fmt.Errorf("flatpak remote-ls timed out: %w", context.DeadlineExceeded), timeout},
		{"cancellation", fmt.Errorf("flatpak remote-ls: %w", context.Canceled), logged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FailureHint(tt.err); got != tt.want {
				t.Fatalf("FailureHint(%q) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// The check-failure detail line carries one hint for every failed source; it
// names the network only when every failure was the network.
func TestCheckFailedDetailNamesTheNetworkOnlyWhenEveryFailureWasIt(t *testing.T) {
	offline := errors.New("curl: (6) Could not resolve host: formulae.brew.sh")
	local := errors.New("permission denied")
	tests := []struct {
		name    string
		sources []updateflow.SourceState
		want    string
	}{
		{
			name: "every source offline",
			sources: []updateflow.SourceState{
				{ID: updateflow.Applications, CheckErr: offline},
				{ID: updateflow.DeveloperTools, CheckErr: offline},
			},
			want: "Not checked: Applications, Developer tools. Check your internet connection.",
		},
		{
			name:    "one local failure",
			sources: []updateflow.SourceState{{ID: updateflow.DeveloperTools, CheckErr: local}},
			want:    "Not checked: Developer tools. Details are in the log.",
		},
		{
			name: "offline and local mixed",
			sources: []updateflow.SourceState{
				{ID: updateflow.Applications, CheckErr: offline},
				{ID: updateflow.DeveloperTools, CheckErr: local},
			},
			want: "Not checked: Applications, Developer tools. Details are in the log.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Snapshot(updateflow.Snapshot{Phase: updateflow.PhaseCheckFailed, Sources: tt.sources})
			if got.Detail != tt.want {
				t.Fatalf("Detail = %q, want %q", got.Detail, tt.want)
			}
		})
	}
}

// The restart lives on the Operating system row (#439); the header must not
// grow a restart, destructive, or any other primary action for it.
func TestRestartRequiredLeavesTheActionOnTheRow(t *testing.T) {
	got := Snapshot(updateflow.Snapshot{
		Phase:   updateflow.PhaseRestartRequired,
		Action:  updateflow.ActionNone,
		Sources: []updateflow.SourceState{{ID: updateflow.OperatingSystem, RestartRequired: true}},
	})
	if got.ShowAction || got.ActionLabel != "" || got.ActionStyle != "" {
		t.Fatalf("restart phase offers a primary action: %#v", got)
	}
	if !strings.Contains(strings.ToLower(got.Status), "restart") {
		t.Fatalf("restart phase status = %q, want it to say a restart is needed", got.Status)
	}
}

func TestStatusHeaderKeepsVisibleContent(t *testing.T) {
	tests := []struct {
		name         string
		presentation Presentation
		phase        updateflow.Phase
		want         bool
	}{
		{name: "empty header", phase: updateflow.PhaseReady},
		{name: "status", presentation: Presentation{Status: "Up to date"}, phase: updateflow.PhaseReady, want: true},
		{name: "detail", presentation: Presentation{Detail: "Permission denied"}, phase: updateflow.PhaseCheckFailed, want: true},
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

func TestEveryPhaseShowsTheHeader(t *testing.T) {
	for _, phase := range everyPhase {
		if !Snapshot(updateflow.Snapshot{Phase: phase}).ShowStatus(phase) {
			t.Errorf("phase %v hides the status header", phase)
		}
	}
}
