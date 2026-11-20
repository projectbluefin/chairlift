package updateproviders

import (
	"context"
	"errors"
	"fmt"
	"log"

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

// Check lists both installations. flatpak.ListUpdates queries each remote
// separately and returns what the healthy remotes reported together with a
// *flatpak.RemoteError for every in-use remote it could not read. That is a
// partial check when updates were found: those updates are offered and the
// unreadable remotes are named in Warnings, so one unreachable remote no
// longer hides every other application's update behind "Couldn't check".
// With no updates found, preserve the errors and their actionable failure hints.
// Any other failure — the remote listing itself, the origin listing that
// decides whether a broken remote is in use, cancellation — means the scope
// was not checked, and stays an error.
func (p *flatpakProvider) Check(ctx context.Context) (updateflow.CheckResult, error) {
	userUpdates, userErr := p.deps.ListUpdates(ctx, true)
	systemUpdates, systemErr := p.deps.ListUpdates(ctx, false)

	var items []updateflow.Item
	items = append(items, flatpakItems(userUpdates)...)
	items = append(items, flatpakItems(systemUpdates)...)
	if len(items) == 0 {
		return updateflow.CheckResult{}, errors.Join(userErr, systemErr)
	}

	var warnings []string
	seen := make(map[string]bool)
	var errs []error
	for _, err := range []error{userErr, systemErr} {
		if err == nil {
			continue
		}
		remotes, partial := remoteFailures(err)
		if !partial {
			errs = append(errs, err)
			continue
		}
		for _, remote := range remotes {
			log.Printf("updates: checking %s: %v", updateflow.Applications, remote)
			if seen[remote.Remote] {
				continue
			}
			seen[remote.Remote] = true
			warnings = append(warnings, flatpakRemoteWarning(remote.Remote))
		}
	}
	if len(errs) > 0 {
		return updateflow.CheckResult{Items: items}, errors.Join(errs...)
	}
	return updateflow.CheckResult{Items: items, Warnings: warnings}, nil
}

// remoteFailures reports whether err consists only of per-remote failures,
// returning them. A joined error with any other member is not a partial
// check: something other than a single remote went wrong.
func remoteFailures(err error) ([]*flatpak.RemoteError, bool) {
	if remote, ok := err.(*flatpak.RemoteError); ok {
		return []*flatpak.RemoteError{remote}, true
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return nil, false
	}
	var remotes []*flatpak.RemoteError
	for _, member := range joined.Unwrap() {
		found, partial := remoteFailures(member)
		if !partial {
			return nil, false
		}
		remotes = append(remotes, found...)
	}
	return remotes, len(remotes) > 0
}

// flatpakRemoteWarning names one remote a partial check could not read. It
// is the noun phrase updatepresent places after "couldn't check"; the
// underlying error is logged, never shown.
func flatpakRemoteWarning(remote string) string {
	return fmt.Sprintf("the %s remote", remote)
}

func (p *flatpakProvider) Apply(ctx context.Context, items []updateflow.Item, progress func(updateflow.Progress)) (updateflow.ApplyResult, error) {
	scopes := scopesFromItems(items)
	pending := append([]updateflow.Item{}, items...)
	ran := false
	preview := p.isDryRun()
	for _, user := range []bool{true, false} {
		scope := "system"
		if user {
			scope = "user"
		}
		if !scopes[scope] {
			continue
		}
		if progress != nil {
			progress(updateflow.Progress{Source: updateflow.Applications, Message: "Updating " + scope + " applications…"})
		}
		if err := ctx.Err(); err != nil {
			return updateflow.ApplyResult{PendingItems: pending, Preview: ran && preview}, err
		}
		if err := p.deps.Update(ctx, "", user); err != nil {
			return updateflow.ApplyResult{PendingItems: pending, Preview: ran && preview}, err
		}
		ran = true
		if preview {
			continue
		}

		// Reconcile each successful scope before starting the next one, so a
		// later failure cannot put already-verified work back in the retry.
		remaining, err := p.pendingAfterScope(ctx, pending, scope)
		if err != nil {
			return updateflow.ApplyResult{PendingItems: pending}, err
		}
		pending = remaining
	}
	if ran && preview {
		return updateflow.ApplyResult{Preview: true}, nil
	}
	if !ran {
		return updateflow.ApplyResult{}, nil
	}
	if len(pending) > 0 {
		if len(pending) < len(items) {
			return updateflow.ApplyResult{PendingItems: pending}, errors.New("some Flatpak updates are still available")
		}
		// Preserve the unchanged outcome when no selected item was applied.
		return updateflow.ApplyResult{}, nil
	}
	return updateflow.ApplyResult{Changed: true}, nil
}

// pendingAfterScope removes only selected items proven absent after a successful
// update in scope. Failed observations retain the entire unverified inventory;
// newly published updates do not prevent completion of the selected items.
func (p *flatpakProvider) pendingAfterScope(ctx context.Context, items []updateflow.Item, scope string) ([]updateflow.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	updates, err := p.deps.ListUpdates(ctx, scope == "user")
	if err != nil {
		return nil, fmt.Errorf("verify Flatpak updates after apply: %w", err)
	}
	pending := make([]updateflow.Item, 0, len(items))
	for _, item := range items {
		if item.Scope != scope {
			pending = append(pending, item)
			continue
		}
		refs := appliedRefsForScope([]updateflow.Item{item}, scope)
		stillPending := len(refs) == 0
		for _, update := range updates {
			if updateMatchesApplied(update, refs) {
				stillPending = true
				break
			}
		}
		if stillPending {
			pending = append(pending, item)
		}
	}
	return pending, nil
}

func appliedRefsForScope(items []updateflow.Item, scope string) map[string]bool {
	refs := make(map[string]bool)
	for _, item := range items {
		if item.Scope != scope {
			continue
		}
		if item.ID != "" {
			refs[item.ID] = true
		} else if item.Name != "" {
			refs[item.Name] = true
		}
	}
	return refs
}

func updateMatchesApplied(u flatpak.UpdateInfo, appliedRefs map[string]bool) bool {
	if u.ApplicationID != "" && (appliedRefs[u.UpdateRef()] || appliedRefs[u.ApplicationID]) {
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
			ID:               update.UpdateRef(),
			Name:             update.Name,
			CurrentVersion:   update.CurrentVersion,
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
