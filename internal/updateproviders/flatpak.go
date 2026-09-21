package updateproviders

import (
	"context"
	"errors"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// FlatpakDeps contains the Flatpak operations used by the update provider.
type FlatpakDeps struct {
	Installed   func() bool
	ListUpdates func(user bool) ([]flatpak.UpdateInfo, error)
	Update      func(ctx context.Context, appID string, user bool) error
	DryRun      func() bool
}
type flatpakProvider struct {
	deps FlatpakDeps
}

// NewFlatpak constructs the production Flatpak update provider.
func NewFlatpak() updateflow.Provider {
	return newFlatpak(FlatpakDeps{
		Installed:   flatpak.IsInstalledCached,
		ListUpdates: flatpak.ListUpdates,
		Update:      flatpak.Update,
		DryRun:      dryrun.Enabled,
	})
}

func newFlatpak(deps FlatpakDeps) updateflow.Provider {
	return &flatpakProvider{deps: deps}
}

func (p *flatpakProvider) ID() updateflow.SourceID {
	return updateflow.Applications
}

func (p *flatpakProvider) Available() bool {
	return p.deps.Installed != nil && p.deps.Installed()
}

func (p *flatpakProvider) Check(context.Context) (updateflow.CheckResult, error) {
	userUpdates, userErr := p.deps.ListUpdates(true)
	systemUpdates, systemErr := p.deps.ListUpdates(false)

	var items []updateflow.Item
	items = append(items, flatpakItems(userUpdates)...)
	items = append(items, flatpakItems(systemUpdates)...)

	return updateflow.CheckResult{Items: items}, joinErrors(userErr, systemErr)
}

func (p *flatpakProvider) Apply(ctx context.Context, items []updateflow.Item, _ func(updateflow.Progress)) (updateflow.ApplyResult, error) {
	scopes := scopesFromItems(items)
	changed := false

	for _, user := range []bool{true, false} {
		scope := "system"
		if user {
			scope = "user"
		}
		if !scopes[scope] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return updateflow.ApplyResult{Changed: changed}, err
		}
		if err := p.deps.Update(ctx, "", user); err != nil {
			return updateflow.ApplyResult{Changed: changed}, err
		}
		changed = true
	}

	preview := changed && p.isDryRun()
	return updateflow.ApplyResult{Changed: changed && !preview, Preview: preview}, nil
}

func (p *flatpakProvider) isDryRun() bool {
	return p.deps.DryRun != nil && p.deps.DryRun()
}

func flatpakItems(updates []flatpak.UpdateInfo) []updateflow.Item {
	if len(updates) == 0 {
		return nil
	}
	items := make([]updateflow.Item, 0, len(updates))
	for _, update := range updates {
		items = append(items, updateflow.Item{
			Name:             update.Name,
			AvailableVersion: update.NewVersion,
			Scope:            update.Installation,
		})
	}
	return items
}

func scopesFromItems(items []updateflow.Item) map[string]bool {
	scopes := make(map[string]bool)
	for _, item := range items {
		switch item.Scope {
		case "user", "system":
			scopes[item.Scope] = true
		}
	}
	return scopes
}

func joinErrors(first, second error) error {
	switch {
	case first == nil:
		return second
	case second == nil:
		return first
	default:
		return errors.Join(first, second)
	}
}
