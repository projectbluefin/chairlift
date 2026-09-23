package updateproviders

import (
	"context"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// HomebrewDeps contains the Homebrew operations used by the update provider.
type HomebrewDeps struct {
	Installed    func() bool
	ListOutdated func() ([]homebrew.Package, error)
	Update       func(ctx context.Context) error
	Upgrade      func(ctx context.Context, name string) error
	DryRun       func() bool
}
type homebrewProvider struct {
	deps HomebrewDeps
}

// NewHomebrew constructs the production Homebrew update provider.
func NewHomebrew() updateflow.Provider {
	return newHomebrew(HomebrewDeps{
		Installed:    homebrew.IsInstalledCached,
		ListOutdated: homebrew.ListOutdated,
		Update:       homebrew.Update,
		Upgrade:      homebrew.Upgrade,
		DryRun:       dryrun.Enabled,
	})
}

func newHomebrew(deps HomebrewDeps) updateflow.Provider {
	return &homebrewProvider{deps: deps}
}

func (p *homebrewProvider) ID() updateflow.SourceID {
	return updateflow.DeveloperTools
}

func (p *homebrewProvider) Available() bool {
	return p.deps.Installed != nil && p.deps.Installed()
}

func (p *homebrewProvider) Check(context.Context) (updateflow.CheckResult, error) {
	packages, err := p.deps.ListOutdated()
	var items []updateflow.Item
	if len(packages) > 0 {
		items = make([]updateflow.Item, 0, len(packages))
	}
	for _, pkg := range packages {
		items = append(items, updateflow.Item{
			Name:           pkg.Name,
			CurrentVersion: pkg.Version,
		})
	}
	return updateflow.CheckResult{Items: items}, err
}

func (p *homebrewProvider) Apply(ctx context.Context, _ []updateflow.Item, _ func(updateflow.Progress)) (updateflow.ApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if err := p.deps.Update(ctx); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if err := p.deps.Upgrade(ctx, ""); err != nil {
		return updateflow.ApplyResult{}, err
	}

	if p.isDryRun() {
		return updateflow.ApplyResult{Preview: true}, nil
	}
	return updateflow.ApplyResult{Changed: true}, nil
}

func (p *homebrewProvider) isDryRun() bool {
	return p.deps.DryRun != nil && p.deps.DryRun()
}
