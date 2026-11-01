package updateproviders

import (
	"context"
	"errors"
	"log"
	"os"
	"slices"
	"time"

	"github.com/projectbluefin/chairlift/internal/dockerbrew"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// DockerRuntimeItemID is the execution identity of the root-owned Docker
// runtime ChairLift installs from verified Homebrew bottles. It is updated by
// the fixed helper's docker-update command, never by brew.
const DockerRuntimeItemID = "docker-runtime"

// HomebrewDeps contains the Homebrew operations used by the update provider.
type HomebrewDeps struct {
	Installed    func() bool
	ListOutdated func() ([]homebrew.Package, error)
	Update       func(ctx context.Context) error
	Upgrade      func(ctx context.Context, name string) error
	DryRun       func() bool
	// DockerManifest reads the installed Homebrew Docker runtime's manifest;
	// os.ErrNotExist means none is installed.
	DockerManifest func() (dockerbrew.Manifest, error)
	// DockerImageUnits reports whether the OS image ships Docker's units, in
	// which case the helper's docker-update has nothing to update.
	DockerImageUnits func() bool
	// DockerUpdateSupported reports whether the helper and its PolicyKit
	// action for docker-update are installed.
	DockerUpdateSupported func() bool
	// DockerLatest reads the latest bottles from Homebrew's unsigned API; the
	// helper re-verifies against the signed index before installing.
	DockerLatest func(ctx context.Context) ([]dockerbrew.Bottle, error)
	// UpdateDocker runs the helper's docker-update command.
	UpdateDocker func(ctx context.Context) error
}

type homebrewProvider struct {
	deps HomebrewDeps
}

// NewHomebrew constructs the production Homebrew update provider.
func NewHomebrew() updateflow.Provider {
	return newHomebrew(productionHomebrewDeps())
}

func productionHomebrewDeps() HomebrewDeps {
	return HomebrewDeps{
		Installed:      homebrew.IsInstalledCached,
		ListOutdated:   homebrew.ListOutdated,
		Update:         homebrew.Update,
		Upgrade:        homebrew.Upgrade,
		DryRun:         dryrun.Enabled,
		DockerManifest: func() (dockerbrew.Manifest, error) { return dockerbrew.ReadManifest(dockerbrew.Root) },
		DockerImageUnits: func() bool {
			_, err := os.Stat(dockerbrew.ImageUnitPath)
			return err == nil
		},
		DockerUpdateSupported: func() bool { return ublue.StatusCached().Supports(ubluehelper.CommandDockerUpdate) },
		DockerLatest:          dockerbrew.Client{}.Latest,
		UpdateDocker:          ublue.UpdateDocker,
	}
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

func (p *homebrewProvider) Check(ctx context.Context) (updateflow.CheckResult, error) {
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
	if item, ok := dockerRuntimeUpdate(ctx, p.deps); ok {
		items = append(items, item)
	}
	return updateflow.CheckResult{Items: items}, err
}

// dockerLatestTimeout bounds the advisory Homebrew API read so a stalled
// connection cannot hold the whole update check; a variable for tests.
var dockerLatestTimeout = 30 * time.Second

// dockerRuntimeUpdate offers the Homebrew Docker runtime when ChairLift
// installed it, the image does not ship Docker, the helper can update it,
// and Homebrew publishes different bottles. The read is advisory: an
// unreachable API omits the item rather than failing the tools check, and the
// helper verifies the signed index before it installs anything.
func dockerRuntimeUpdate(ctx context.Context, deps HomebrewDeps) (updateflow.Item, bool) {
	if deps.DockerManifest == nil || deps.DockerImageUnits == nil || deps.DockerUpdateSupported == nil || deps.DockerLatest == nil || deps.UpdateDocker == nil {
		return updateflow.Item{}, false
	}
	installed, err := deps.DockerManifest()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("updateproviders: reading Docker runtime manifest: %v", err)
		}
		return updateflow.Item{}, false
	}
	if deps.DockerImageUnits() || !deps.DockerUpdateSupported() {
		return updateflow.Item{}, false
	}
	latestCtx, cancel := context.WithTimeout(ctx, dockerLatestTimeout)
	defer cancel()
	latest, err := deps.DockerLatest(latestCtx)
	if err != nil {
		log.Printf("updateproviders: checking Docker runtime bottles: %v", err)
		return updateflow.Item{}, false
	}
	if !dockerbrew.Outdated(installed, latest) {
		return updateflow.Item{}, false
	}
	return updateflow.Item{
		ID:               DockerRuntimeItemID,
		Name:             "Docker engine",
		CurrentVersion:   engineVersion(installed.Bottles),
		AvailableVersion: engineVersion(latest),
	}, true
}

func engineVersion(bottles []dockerbrew.Bottle) string {
	for _, bottle := range bottles {
		if bottle.Name == "docker-engine" {
			return bottle.Dir()
		}
	}
	return ""
}

func (p *homebrewProvider) Apply(ctx context.Context, items []updateflow.Item, progress func(updateflow.Progress)) (updateflow.ApplyResult, error) {
	if progress != nil {
		progress(updateflow.Progress{
			Source:  updateflow.DeveloperTools,
			Message: "Updating software list…",
		})
	}
	if err := ctx.Err(); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if err := p.deps.Update(ctx); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if progress != nil {
		progress(updateflow.Progress{
			Source:  updateflow.DeveloperTools,
			Message: "Upgrading tools…",
		})
	}
	if err := ctx.Err(); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if err := p.deps.Upgrade(ctx, ""); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if slices.ContainsFunc(items, func(item updateflow.Item) bool { return item.ID == DockerRuntimeItemID }) && p.deps.UpdateDocker != nil {
		if progress != nil {
			progress(updateflow.Progress{
				Source:  updateflow.DeveloperTools,
				Message: "Updating Docker engine…",
			})
		}
		if err := ctx.Err(); err != nil {
			return updateflow.ApplyResult{}, err
		}
		if err := p.deps.UpdateDocker(ctx); err != nil {
			return updateflow.ApplyResult{}, err
		}
	}

	if p.isDryRun() {
		return updateflow.ApplyResult{Preview: true}, nil
	}
	return updateflow.ApplyResult{Changed: true}, nil
}

// updateDockerRuntime is the Docker engine row's individual update: the
// helper replaces the runtime, then a fresh check confirms it is current.
func updateDockerRuntime(ctx context.Context, deps HomebrewDeps) (updateflow.ApplyResult, error) {
	if deps.UpdateDocker == nil {
		return updateflow.ApplyResult{}, errors.New("docker engine update is not available")
	}
	if err := deps.UpdateDocker(ctx); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if deps.DryRun != nil && deps.DryRun() {
		return updateflow.ApplyResult{Preview: true}, nil
	}
	if _, pending := dockerRuntimeUpdate(ctx, deps); pending {
		return updateflow.ApplyResult{}, nil
	}
	return updateflow.ApplyResult{Changed: true}, nil
}

func (p *homebrewProvider) isDryRun() bool {
	return p.deps.DryRun != nil && p.deps.DryRun()
}
