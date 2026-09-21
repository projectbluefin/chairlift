package updateproviders

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/config"
)

func maintenanceConfig(brew, flatpak, scripts, optimization bool) *config.Config {
	return &config.Config{
		MaintenancePage: config.PageConfig{
			"maintenance_brew_group": {
				Enabled: brew,
			},
			"maintenance_flatpak_group": {
				Enabled: flatpak,
			},
			"maintenance_cleanup_group": {
				Enabled: scripts,
				Actions: []config.ActionConfig{{
					Title:  "Configured script",
					Script: "/bin/false",
					Sudo:   true,
				}},
			},
			"maintenance_optimization_group": {
				Enabled: optimization,
			},
		},
	}
}

func TestMaintenanceRunsEnabledCleanupInOrder(t *testing.T) {
	var calls []string
	maintenance := newMaintenance(maintenanceConfig(true, true, true, true), MaintenanceDeps{
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
	maintenance := newMaintenance(maintenanceConfig(true, true, false, false), MaintenanceDeps{
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
	maintenance := newMaintenance(maintenanceConfig(true, true, false, false), MaintenanceDeps{
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

func TestMaintenanceIgnoresConfiguredScriptAndOptimizationGroups(t *testing.T) {
	var calls []string
	maintenance := newMaintenance(maintenanceConfig(false, false, true, true), MaintenanceDeps{
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
