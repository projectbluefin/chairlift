package updateproviders

import (
	"context"
	"errors"

	"github.com/projectbluefin/chairlift/internal/config"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// MaintenanceDeps contains the typed cleanup operations used by automatic
// post-update maintenance.
type MaintenanceDeps struct {
	HomebrewInstalled func() bool
	HomebrewCleanup   func() (string, error)
	FlatpakInstalled  func() bool
	FlatpakCleanup    func() error
}

type maintenance struct {
	cfg  *config.Config
	deps MaintenanceDeps
}

// NewMaintenance constructs the production typed maintenance runner.
func NewMaintenance(cfg *config.Config) updateflow.Maintenance {
	return newMaintenance(cfg, MaintenanceDeps{
		HomebrewInstalled: homebrew.IsInstalledCached,
		HomebrewCleanup:   homebrew.Cleanup,
		FlatpakInstalled:  flatpak.IsInstalledCached,
		FlatpakCleanup: func() error {
			_, err := flatpak.UninstallUnused()
			return err
		},
	})
}

func newMaintenance(cfg *config.Config, deps MaintenanceDeps) updateflow.Maintenance {
	return &maintenance{cfg: cfg, deps: deps}
}

func (m *maintenance) Run(ctx context.Context, progress func(updateflow.Progress)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if m.enabled("maintenance_brew_group") &&
		m.deps.HomebrewInstalled != nil &&
		m.deps.HomebrewInstalled() {
		if progress != nil {
			progress(updateflow.Progress{
				Source:  updateflow.DeveloperTools,
				Message: "Cleaning up Homebrew",
			})
		}
		if m.deps.HomebrewCleanup == nil {
			return errors.New("homebrew cleanup is unavailable")
		}
		if _, err := m.deps.HomebrewCleanup(); err != nil {
			return err
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if m.enabled("maintenance_flatpak_group") &&
		m.deps.FlatpakInstalled != nil &&
		m.deps.FlatpakInstalled() {
		if progress != nil {
			progress(updateflow.Progress{
				Source:  updateflow.Applications,
				Message: "Cleaning up Flatpak",
			})
		}
		if m.deps.FlatpakCleanup == nil {
			return errors.New("flatpak cleanup is unavailable")
		}
		if err := m.deps.FlatpakCleanup(); err != nil {
			return err
		}
	}

	return nil
}

func (m *maintenance) enabled(group string) bool {
	if m.cfg == nil {
		return false
	}
	settings, ok := m.cfg.MaintenancePage[group]
	return ok && settings.Enabled
}
