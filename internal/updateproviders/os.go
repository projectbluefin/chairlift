package updateproviders

import (
	"context"
	"errors"
	"fmt"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/sysupdate"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

const (
	bootcScope     = "bootc"
	sysupdateScope = "sysupdate"
)

var (
	errOperatingSystemRuntimeUnavailable = errors.New("operating system update transport is runtime-unavailable")
	errOperatingSystemAmbiguous          = errors.New("ambiguous operating system update transports: bootc and sysupdate are both available")
)

// OSDeps contains the read, stage, and status operations used by the
// operating-system provider.
type OSDeps struct {
	BootcBooted             func() bool
	BootcStageAvailable     func() bool
	BootcCheck              func(context.Context) (bootc.AvailableUpdate, error)
	BootcStage              func(context.Context, chan<- bootc.ProgressEvent) error
	BootcDryRun             func() bool
	BootcStatus             func(context.Context) (*bootc.Status, error)
	NativeAB                func() bool
	SysupdateStageAvailable func() bool
	SysupdateCheck          func(context.Context) (sysupdate.AvailableUpdate, error)
	SysupdateStage          func(context.Context, chan<- sysupdate.ProgressEvent) error
	SysupdateDryRun         func() bool
	SysupdateStatus         func() (sysupdate.Status, error)
}

type operatingSystemProvider struct {
	deps OSDeps
}

// NewOperatingSystem constructs the production operating-system provider.
func NewOperatingSystem() updateflow.Provider {
	return newOperatingSystem(OSDeps{
		BootcBooted:             bootc.IsBootcBootedCached,
		BootcStageAvailable:     bootc.StageScriptAvailable,
		BootcCheck:              bootc.CheckUpdate,
		BootcStage:              bootc.StageUpdate,
		BootcDryRun:             dryrun.Enabled,
		BootcStatus:             bootc.GetStatus,
		NativeAB:                sysupdate.IsNativeABCached,
		SysupdateStageAvailable: sysupdate.StageScriptAvailable,
		SysupdateCheck:          sysupdate.CheckUpdate,
		SysupdateStage:          sysupdate.StageUpdate,
		SysupdateDryRun:         dryrun.Enabled,
		SysupdateStatus:         sysupdate.GetStatus,
	})
}

func newOperatingSystem(deps OSDeps) updateflow.Provider {
	return &operatingSystemProvider{deps: deps}
}

func (p *operatingSystemProvider) ID() updateflow.SourceID {
	return updateflow.OperatingSystem
}

// Available only inspects the cheap, local transport markers. The bootc host
// probe is deliberately deferred to Check, which runs off the GTK thread.
func (p *operatingSystemProvider) Available() bool {
	return p.bootcStageAvailable() || p.nativeAB() && p.sysupdateStageAvailable()
}

func (p *operatingSystemProvider) Check(ctx context.Context) (updateflow.CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return updateflow.CheckResult{}, err
	}

	bootcReady := p.bootcBooted() && p.bootcStageAvailable()
	sysupdateReady := p.nativeAB() && p.sysupdateStageAvailable()

	switch {
	case bootcReady && sysupdateReady:
		return updateflow.CheckResult{}, errOperatingSystemAmbiguous
	case bootcReady:
		return p.checkBootc(ctx)
	case sysupdateReady:
		return p.checkSysupdate(ctx)
	default:
		return updateflow.CheckResult{}, errOperatingSystemRuntimeUnavailable
	}
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

func (p *operatingSystemProvider) checkSysupdate(ctx context.Context) (updateflow.CheckResult, error) {
	if p.deps.SysupdateCheck == nil {
		return updateflow.CheckResult{}, errors.New("systemd-sysupdate check is unavailable")
	}
	update, err := p.deps.SysupdateCheck(ctx)
	if err != nil {
		return updateflow.CheckResult{}, err
	}

	var status sysupdate.Status
	if p.deps.SysupdateStatus != nil {
		st, stErr := p.deps.SysupdateStatus()
		if stErr != nil {
			return updateflow.CheckResult{}, stErr
		}
		status = st
	}

	result := updateflow.CheckResult{
		RestartRequired: status.IsStaged(),
	}
	if !update.Available {
		return result, nil
	}

	item := updateflow.Item{
		Name:             "Operating System",
		AvailableVersion: update.Version,
		Scope:            sysupdateScope,
	}
	if status.Check != nil {
		item.CurrentVersion = status.Check.RunningVersion
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

	bootcSelected, sysupdateSelected := false, false
	for _, item := range items {
		switch item.Scope {
		case bootcScope:
			bootcSelected = true
		case sysupdateScope:
			sysupdateSelected = true
		default:
			return updateflow.ApplyResult{}, fmt.Errorf("unknown operating system update scope %q", item.Scope)
		}
	}
	if bootcSelected && sysupdateSelected {
		return updateflow.ApplyResult{}, errOperatingSystemAmbiguous
	}

	if bootcSelected {
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

	if p.deps.SysupdateStage == nil {
		return updateflow.ApplyResult{}, errors.New("systemd-sysupdate staging is unavailable")
	}
	if err := p.stageSysupdate(ctx, progress); err != nil {
		return updateflow.ApplyResult{}, err
	}
	if p.deps.SysupdateDryRun != nil && p.deps.SysupdateDryRun() {
		return updateflow.ApplyResult{Preview: true, Changed: false}, nil
	}
	if p.deps.SysupdateStatus == nil {
		return updateflow.ApplyResult{Changed: true}, nil
	}
	st, stErr := p.deps.SysupdateStatus()
	if stErr != nil {
		return updateflow.ApplyResult{}, stErr
	}
	return updateflow.ApplyResult{
		Changed:         true,
		RestartRequired: st.IsStaged(),
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

func (p *operatingSystemProvider) stageSysupdate(
	ctx context.Context,
	progress func(updateflow.Progress),
) error {
	events := make(chan sysupdate.ProgressEvent)
	done := make(chan error, 1)
	go func() {
		done <- p.deps.SysupdateStage(ctx, events)
	}()

	for {
		select {
		case event, ok := <-events:
			if !ok {
				return <-done
			}
			if event.Type == sysupdate.EventMessage && progress != nil {
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

func (p *operatingSystemProvider) nativeAB() bool {
	return p.deps.NativeAB != nil && p.deps.NativeAB()
}

func (p *operatingSystemProvider) sysupdateStageAvailable() bool {
	return p.deps.SysupdateStageAvailable != nil && p.deps.SysupdateStageAvailable()
}
