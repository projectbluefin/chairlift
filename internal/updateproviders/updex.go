package updateproviders

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/updex"
)

// UpdexDeps contains the updex operations used by the system-components
// provider.
type UpdexDeps struct {
	Installed func() bool
	Check     func(context.Context) ([]updex.FeatureCheck, []string, error)
	Update    func(context.Context) error
	DryRun    func() bool
}
type systemComponentsProvider struct {
	deps UpdexDeps
}

// NewSystemComponents constructs the production updex provider.
func NewSystemComponents() updateflow.Provider {
	return newSystemComponents(UpdexDeps{
		Installed: updex.IsInstalledCached,
		Check:     updex.CheckFeatures,
		Update:    updex.UpdateFeatures,
		DryRun:    dryrun.Enabled,
	})
}

func newSystemComponents(deps UpdexDeps) updateflow.Provider {
	return &systemComponentsProvider{deps: deps}
}

func (p *systemComponentsProvider) ID() updateflow.SourceID {
	return updateflow.SystemComponents
}

func (p *systemComponentsProvider) Available() bool {
	return p.deps.Installed != nil && p.deps.Installed()
}

func (p *systemComponentsProvider) Check(ctx context.Context) (updateflow.CheckResult, error) {
	if p.deps.Check == nil {
		return updateflow.CheckResult{}, errors.New("updex feature check is unavailable")
	}
	checks, warnings, err := p.deps.Check(ctx)
	if err != nil {
		return updateflow.CheckResult{}, err
	}
	if len(warnings) > 0 {
		return updateflow.CheckResult{}, fmt.Errorf("updex feature check incomplete: %s", strings.Join(warnings, "; "))
	}

	var items []updateflow.Item
	seen := make(map[string]bool)
	for _, feature := range checks {
		affected := false
		for _, result := range feature.Results {
			if result.UpdateAvailable {
				affected = true
				break
			}
		}
		if affected && !seen[feature.Feature] {
			seen[feature.Feature] = true
			items = append(items, updateflow.Item{Name: feature.Feature})
		}
	}
	return updateflow.CheckResult{Items: items}, nil
}

func (p *systemComponentsProvider) Apply(
	ctx context.Context,
	_ []updateflow.Item,
	_ func(updateflow.Progress),
) (updateflow.ApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if p.deps.Update == nil {
		return updateflow.ApplyResult{}, errors.New("updex feature update is unavailable")
	}
	if err := p.deps.Update(ctx); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if p.deps.DryRun != nil && p.deps.DryRun() {
		return updateflow.ApplyResult{Preview: true}, nil
	}
	return updateflow.ApplyResult{Changed: true}, nil
}
