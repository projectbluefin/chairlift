package updateproviders

import (
	"context"
	"fmt"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// UpdateItem preserves the individual update actions on the unified surface.
// The shell admits it through the same mutation owner as Update All.
func UpdateItem(ctx context.Context, source updateflow.SourceID, item updateflow.Item) (updateflow.ApplyResult, error) {
	switch source {
	case updateflow.Applications:
		if item.ID == "" || (item.Scope != "user" && item.Scope != "system") {
			return updateflow.ApplyResult{}, fmt.Errorf("application update has no execution identity or installation")
		}
		if err := flatpak.Update(ctx, item.ID, item.Scope == "user"); err != nil {
			return updateflow.ApplyResult{}, err
		}
		if dryrun.Enabled() {
			return updateflow.ApplyResult{Preview: true}, nil
		}
		updates, err := flatpak.ListUpdates(ctx, item.Scope == "user")
		if err != nil {
			return updateflow.ApplyResult{}, fmt.Errorf("verify application update: %w", err)
		}
		for _, update := range updates {
			if update.ApplicationID == item.ID {
				return updateflow.ApplyResult{}, nil
			}
		}
	case updateflow.DeveloperTools:
		if item.Name == "" {
			return updateflow.ApplyResult{}, fmt.Errorf("tool update has no package name")
		}
		if err := homebrew.Upgrade(ctx, item.Name); err != nil {
			return updateflow.ApplyResult{}, err
		}
		if dryrun.Enabled() {
			return updateflow.ApplyResult{Preview: true}, nil
		}
		packages, err := homebrew.ListOutdated()
		if err != nil {
			return updateflow.ApplyResult{}, fmt.Errorf("verify tool update: %w", err)
		}
		for _, pkg := range packages {
			if pkg.Name == item.Name {
				return updateflow.ApplyResult{}, nil
			}
		}
	default:
		return updateflow.ApplyResult{}, fmt.Errorf("source %q does not offer individual updates", source)
	}
	return updateflow.ApplyResult{Changed: true}, nil
}

// RefreshDeveloperTools refreshes metadata without upgrading installed tools.
func RefreshDeveloperTools(ctx context.Context) (updateflow.ApplyResult, error) {
	if err := homebrew.Update(ctx); err != nil {
		return updateflow.ApplyResult{}, err
	}
	return updateflow.ApplyResult{Changed: !dryrun.Enabled(), Preview: dryrun.Enabled()}, nil
}
