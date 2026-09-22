package updateproviders

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/config"
)

func maintenanceConfig(freespace, scripts bool) *config.Config {
	return &config.Config{
		MaintenancePage: config.PageConfig{
			CleanupGroup: {
				Enabled: freespace,
			},
			"maintenance_cleanup_group": {
				Enabled: scripts,
				Actions: []config.ActionConfig{{
					Title:  "Configured script",
					Script: "/bin/false",
					Sudo:   true,
				}},
			},
		},
	}
}

func TestMaintenanceRunsEnabledCleanupInOrder(t *testing.T) {
	var calls []string
	maintenance := newMaintenance(maintenanceConfig(true, true), MaintenanceDeps{
		HomebrewInstalled: func() bool {
			calls = append(calls, "brew-available")
			return true
		},
		HomebrewCleanup: func() (string, error) {
			calls = append(calls, "brew-cleanup")
			return "cleaned", nil
		},
		FlatpakInstalled: func() bool {
			calls = append(calls, "flatpak-available")
			return true
		},
		FlatpakCleanup: func() error {
			calls = append(calls, "flatpak-cleanup")
			return nil
		},
	})

	if err := maintenance.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{
		"brew-available",
		"brew-cleanup",
		"flatpak-available",
		"flatpak-cleanup",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("maintenance calls = %v, want %v", calls, want)
	}
}

func TestMaintenanceSkipsUnavailableTools(t *testing.T) {
	var calls []string
	maintenance := newMaintenance(maintenanceConfig(true, false), MaintenanceDeps{
		HomebrewInstalled: func() bool {
			calls = append(calls, "brew-available")
			return false
		},
		HomebrewCleanup: func() (string, error) {
			calls = append(calls, "brew-cleanup")
			return "", nil
		},
		FlatpakInstalled: func() bool {
			calls = append(calls, "flatpak-available")
			return false
		},
		FlatpakCleanup: func() error {
			calls = append(calls, "flatpak-cleanup")
			return nil
		},
	})

	if err := maintenance.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"brew-available", "flatpak-available"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("maintenance calls = %v, want %v", calls, want)
	}
}

func TestMaintenanceFailureStopsLaterCleanup(t *testing.T) {
	wantErr := errors.New("brew cleanup failed")
	var flatpakCalls int
	maintenance := newMaintenance(maintenanceConfig(true, false), MaintenanceDeps{
		HomebrewInstalled: func() bool { return true },
		HomebrewCleanup:   func() (string, error) { return "", wantErr },
		FlatpakInstalled:  func() bool { flatpakCalls++; return true },
		FlatpakCleanup:    func() error { return nil },
	})

	if err := maintenance.Run(context.Background(), nil); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if flatpakCalls != 0 {
		t.Fatalf("Flatpak availability calls = %d, want 0 after Homebrew failure", flatpakCalls)
	}
}

// The administrator-configured script group is a separate feature with its
// own opt-in default. Enabling it must never make the typed cleanup run, or
// an administrator who configured one script would silently get two.
func TestMaintenanceIgnoresTheConfiguredScriptGroup(t *testing.T) {
	var calls []string
	maintenance := newMaintenance(maintenanceConfig(false, true), MaintenanceDeps{
		HomebrewInstalled: func() bool { calls = append(calls, "brew"); return true },
		HomebrewCleanup:   func() (string, error) { calls = append(calls, "brew-cleanup"); return "", nil },
		FlatpakInstalled:  func() bool { calls = append(calls, "flatpak"); return true },
		FlatpakCleanup:    func() error { calls = append(calls, "flatpak-cleanup"); return nil },
	})

	if err := maintenance.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("typed maintenance calls = %v, want none", calls)
	}
}

// RunSteps is the manual button's path. A failing step must not cancel the
// other one, and both outcomes must come back so the page can say which
// half happened.
func TestCleanupStepsReportEachProviderIndependently(t *testing.T) {
	var flatpakRan bool
	cleanup := newMaintenance(maintenanceConfig(true, false), MaintenanceDeps{
		HomebrewInstalled: func() bool { return true },
		HomebrewCleanup:   func() (string, error) { return "", errors.New("disk read error") },
		FlatpakInstalled:  func() bool { return true },
		FlatpakCleanup:    func() error { flatpakRan = true; return nil },
	})

	results := cleanup.RunSteps(context.Background())
	if !flatpakRan {
		t.Fatal("unused-support step did not run after old-downloads failed")
	}
	want := []StepResult{
		{ID: StepOldDownloads, Outcome: OutcomeFailed},
		{ID: StepUnusedSupport, Outcome: OutcomeCleaned},
	}
	for i, result := range results {
		if result.ID != want[i].ID || result.Outcome != want[i].Outcome {
			t.Errorf("results[%d] = %s/%s, want %s/%s",
				i, result.ID, result.Outcome, want[i].ID, want[i].Outcome)
		}
	}
}

// A dismissed authentication prompt is neither a success nor a bug. It has
// to be distinguishable, because the page must not tell a user space was
// freed when they cancelled the prompt that would have freed it.
func TestCleanupClassifiesAbandonedStepsApartFromFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want StepOutcome
	}{
		{"dismissed prompt", errors.New("Error: Request dismissed"), OutcomeCancelled},
		{"polkit refusal", errors.New("not authorized to perform operation"), OutcomeCancelled},
		{"cancelled context", context.Canceled, OutcomeCancelled},
		{"deadline", context.DeadlineExceeded, OutcomeCancelled},
		{"real failure", errors.New("disk read error"), OutcomeFailed},
		{"no error", nil, OutcomeCleaned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(tt.err); got != tt.want {
				t.Errorf("classify(%v) = %s, want %s", tt.err, got, tt.want)
			}
		})
	}
}

// Disabled cleanup must report every step as skipped rather than returning
// an empty slice: the page distinguishes "nothing to do" from "nothing ran".
func TestCleanupStepsAreSkippedWhenCleanupIsDisabled(t *testing.T) {
	var calls int
	cleanup := newMaintenance(maintenanceConfig(false, false), MaintenanceDeps{
		HomebrewInstalled: func() bool { calls++; return true },
		HomebrewCleanup:   func() (string, error) { calls++; return "", nil },
		FlatpakInstalled:  func() bool { calls++; return true },
		FlatpakCleanup:    func() error { calls++; return nil },
	})

	results := cleanup.RunSteps(context.Background())
	if len(results) != len(cleanupSteps) {
		t.Fatalf("len(results) = %d, want %d", len(results), len(cleanupSteps))
	}
	for _, result := range results {
		if result.Outcome != OutcomeSkipped {
			t.Errorf("%s outcome = %s, want %s", result.ID, result.Outcome, OutcomeSkipped)
		}
	}
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0 when cleanup is disabled", calls)
	}
}
