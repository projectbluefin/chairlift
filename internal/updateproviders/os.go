package updateproviders

import (
	"context"
	"errors"
	"fmt"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

const bootcScope = "bootc"

var errOperatingSystemRuntimeUnavailable = errors.New("operating system update transport is runtime-unavailable")

// OSDeps contains the read, stage, and status operations used by the
// operating-system provider.
type OSDeps struct {
	BootcBooted         func() bool
	BootcStageAvailable func() bool
	BootcCheck          func(context.Context) (bootc.AvailableUpdate, error)
	BootcStage          func(context.Context, chan<- bootc.ProgressEvent) error
	BootcDryRun         func() bool
	BootcStatus         func(context.Context) (*bootc.Status, error)
}

type operatingSystemProvider struct {
	deps OSDeps
}

// NewOperatingSystem constructs the production operating-system provider.
func NewOperatingSystem() updateflow.Provider {
	return newOperatingSystem(OSDeps{
		BootcBooted:         bootc.IsBootcBootedCached,
		BootcStageAvailable: bootc.StageScriptAvailable,
		BootcCheck:          bootc.CheckUpdate,
		BootcStage:          bootc.StageUpdate,
		BootcDryRun:         dryrun.Enabled,
		BootcStatus:         bootc.GetStatus,
	})
}

func newOperatingSystem(deps OSDeps) updateflow.Provider {
	return &operatingSystemProvider{deps: deps}
}

func (p *operatingSystemProvider) ID() updateflow.SourceID {
	return updateflow.OperatingSystem
}

// Available only inspects the cheap, local stage-helper marker. The bootc host
// probe is deliberately deferred to Check, which runs off the GTK thread.
func (p *operatingSystemProvider) Available() bool {
	return p.bootcStageAvailable()
}

func (p *operatingSystemProvider) Check(ctx context.Context) (updateflow.CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return updateflow.CheckResult{}, err
	}
	if !p.bootcBooted() || !p.bootcStageAvailable() {
		return updateflow.CheckResult{}, errOperatingSystemRuntimeUnavailable
	}
	return p.checkBootc(ctx)
}

func (p *operatingSystemProvider) checkBootc(ctx context.Context) (updateflow.CheckResult, error) {
	if p.deps.BootcCheck == nil {
		return updateflow.CheckResult{}, errors.New("bootc update check is unavailable")
	}
	update, err := p.deps.BootcCheck(ctx)
	if err != nil {
		return updateflow.CheckResult{}, err
	}

	var status *bootc.Status
	if p.deps.BootcStatus != nil {
		status, err = p.deps.BootcStatus(ctx)
		if err != nil {
			return updateflow.CheckResult{}, err
		}
	}

	result := updateflow.CheckResult{
		RestartRequired: status != nil && status.Status.Staged != nil,
	}
	if !update.Available {
		return result, nil
	}

	item := updateflow.Item{
		Name:             "Operating System",
		AvailableVersion: update.Version,
		Scope:            bootcScope,
	}
	if status != nil && status.Status.Booted != nil {
		item.CurrentVersion = status.Status.Booted.Version()
	}
	result.Items = []updateflow.Item{item}
	return result, nil
}

func (p *operatingSystemProvider) Apply(
	ctx context.Context,
	items []updateflow.Item,
	progress func(updateflow.Progress),
) (updateflow.ApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if len(items) == 0 {
		return updateflow.ApplyResult{}, nil
	}

	for _, item := range items {
		if item.Scope != bootcScope {
			return updateflow.ApplyResult{}, fmt.Errorf("unknown operating system update scope %q", item.Scope)
		}
	}

	if p.deps.BootcStage == nil {
		return updateflow.ApplyResult{}, errors.New("bootc update staging is unavailable")
	}
	if err := p.stageBootc(ctx, progress); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if p.deps.BootcDryRun != nil && p.deps.BootcDryRun() {
		return updateflow.ApplyResult{Preview: true, Changed: false}, nil
	}
	if p.deps.BootcStatus == nil {
		return updateflow.ApplyResult{Changed: true}, nil
	}
	status, err := p.deps.BootcStatus(ctx)
	if err != nil {
		return updateflow.ApplyResult{}, err
	}
	return updateflow.ApplyResult{
		Changed:         true,
		RestartRequired: status != nil && status.Status.Staged != nil,
	}, nil
}

func (p *operatingSystemProvider) stageBootc(
	ctx context.Context,
	progress func(updateflow.Progress),
) error {
	events := make(chan bootc.ProgressEvent)
	done := make(chan error, 1)
	go func() {
		done <- p.deps.BootcStage(ctx, events)
	}()

	for {
		select {
		case event, ok := <-events:
			if !ok {
				return <-done
			}
			if event.Type == bootc.EventMessage && progress != nil {
				progress(updateflow.Progress{
					Source:  updateflow.OperatingSystem,
					Message: event.Message,
				})
			}
		case err := <-done:
			return err
		}
	}
}

func (p *operatingSystemProvider) bootcBooted() bool {
	return p.deps.BootcBooted != nil && p.deps.BootcBooted()
}

func (p *operatingSystemProvider) bootcStageAvailable() bool {
	return p.deps.BootcStageAvailable != nil && p.deps.BootcStageAvailable()
}
