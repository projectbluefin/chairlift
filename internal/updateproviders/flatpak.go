package updateproviders

import (
	"context"
	"errors"
	"fmt"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// FlatpakDeps contains the Flatpak operations used by the update provider.
type FlatpakDeps struct {
	Installed   func() bool
	ListUpdates func(ctx context.Context, user bool) ([]flatpak.UpdateInfo, error)
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

func (p *flatpakProvider) Check(ctx context.Context) (updateflow.CheckResult, error) {
	userUpdates, userErr := p.deps.ListUpdates(ctx, true)
	systemUpdates, systemErr := p.deps.ListUpdates(ctx, false)

	var items []updateflow.Item
	items = append(items, flatpakItems(userUpdates)...)
	items = append(items, flatpakItems(systemUpdates)...)

	return updateflow.CheckResult{Items: items}, joinErrors(userErr, systemErr)
}

func (p *flatpakProvider) Apply(ctx context.Context, items []updateflow.Item, progress func(updateflow.Progress)) (updateflow.ApplyResult, error) {
	scopes := scopesFromItems(items)
	ran := false

	for _, user := range []bool{true, false} {
		scope := "system"
		if user {
			scope = "user"
		}
		if !scopes[scope] {
			continue
		}
		if progress != nil {
			label := "user applications"
			if !user {
				label = "system applications"
			}
			progress(updateflow.Progress{
				Source:  updateflow.Applications,
				Message: "Updating " + label + "…",
			})
		}
		if err := ctx.Err(); err != nil {
			return updateflow.ApplyResult{}, err
		}
		if err := p.deps.Update(ctx, "", user); err != nil {
			return updateflow.ApplyResult{}, err
		}
		ran = true
	}
	if ran && p.isDryRun() {
		return updateflow.ApplyResult{Preview: true}, nil
	}
	if !ran {
		return updateflow.ApplyResult{}, nil
	}

	// `flatpak update` exits 0 on paths that pull nothing — it prints
	// "Nothing to update." and returns success when it has no work. A zero
	// exit is therefore not evidence that the pending inventory was applied,
	// and reporting a completed update here would be a claim the command
	// cannot support. Re-list the scopes the run was asked to apply and claim
	// the mutation only when those applied entries are gone; otherwise the
	// source stays pending and the coordinator reports it as unchanged.
	// Newly appeared updates (published between check and apply) were not
	// part of the applied set and do not mark the run unchanged.
	changed, err := p.updatesCleared(ctx, items, scopes)
	if err != nil {
		return updateflow.ApplyResult{}, err
	}
	return updateflow.ApplyResult{Changed: changed}, nil
}

// updatesCleared reports whether all applied updates in the scopes that ran an
// update are no longer listed in available updates. It is the post-hoc
// reconciliation flatpak requires: the tool has no dry-run, so the only proof
// that a zero exit applied the inventory is that the inventory is gone.
// Newly appeared updates (updates published after the check) were not part of
// the applied set and do not mark the run unchanged.
func (p *flatpakProvider) updatesCleared(ctx context.Context, items []updateflow.Item, scopes map[string]bool) (bool, error) {
	for _, user := range []bool{true, false} {
		scope := "system"
		if user {
			scope = "user"
		}
		if !scopes[scope] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		updates, err := p.deps.ListUpdates(ctx, user)
		if err != nil {
			return false, fmt.Errorf("verify Flatpak updates after apply: %w", err)
		}
		applied := appliedRefsForScope(items, scope)
		if len(applied) == 0 {
			return false, nil
		}
		for _, u := range updates {
			if updateMatchesApplied(u, applied) {
				return false, nil
			}
		}
	}
	return true, nil
}

func appliedRefsForScope(items []updateflow.Item, scope string) map[string]bool {
	refs := make(map[string]bool)
	for _, item := range items {
		if item.Scope != scope {
			continue
		}
		if item.ID != "" {
			refs[item.ID] = true
		}
		if item.Name != "" {
			refs[item.Name] = true
		}
	}
	return refs
}

func updateMatchesApplied(u flatpak.UpdateInfo, appliedRefs map[string]bool) bool {
	if u.ApplicationID != "" && appliedRefs[u.ApplicationID] {
		return true
	}
	if u.Name != "" && appliedRefs[u.Name] {
		return true
	}
	return false
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
			ID:               update.ApplicationID,
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
