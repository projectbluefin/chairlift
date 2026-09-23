package updateproviders

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/sysupdate"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/userprefs"
)

func TestOperatingSystemSelectionMatrix(t *testing.T) {
	tests := []struct {
		name        string
		booted      bool
		bootcStage  bool
		native      bool
		sysStage    bool
		bootcCheck  bootc.AvailableUpdate
		sysCheck    sysupdate.AvailableUpdate
		wantScope   string
		wantVersion string
		wantErr     string
		wantAvail   bool
	}{
		{
			name:        "bootc host",
			booted:      true,
			bootcStage:  true,
			bootcCheck:  bootc.AvailableUpdate{Available: true, Version: "20260907", Digest: "sha256:abc"},
			wantScope:   "bootc",
			wantVersion: "20260907",
			wantAvail:   true,
		},
		{
			name:        "native A/B host",
			native:      true,
			sysStage:    true,
			sysCheck:    sysupdate.AvailableUpdate{Available: true, Version: "20260907134040"},
			wantScope:   "sysupdate",
			wantVersion: "20260907134040",
			wantAvail:   true,
		},
		{
			name:      "no runtime",
			wantErr:   "runtime-unavailable",
			wantAvail: false,
		},
		{
			name:       "ambiguous runtimes",
			booted:     true,
			bootcStage: true,
			native:     true,
			sysStage:   true,
			wantErr:    "ambiguous",
			wantAvail:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newOperatingSystem(OSDeps{
				BootcBooted:         func() bool { return test.booted },
				BootcStageAvailable: func() bool { return test.bootcStage },
				BootcCheck: func(context.Context) (bootc.AvailableUpdate, error) {
					return test.bootcCheck, nil
				},
				BootcStatus: func(context.Context) (*bootc.Status, error) {
					return &bootc.Status{}, nil
				},
				NativeAB:                func() bool { return test.native },
				SysupdateStageAvailable: func() bool { return test.sysStage },
				SysupdateCheck: func(context.Context) (sysupdate.AvailableUpdate, error) {
					return test.sysCheck, nil
				},
				SysupdateStatus: func() (sysupdate.Status, error) {
					return sysupdate.Status{}, nil
				},
			})

			if got := provider.Available(); got != test.wantAvail {
				t.Fatalf("Available() = %v, want %v", got, test.wantAvail)
			}

			result, err := provider.Check(context.Background())
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Check() error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if len(result.Items) != 1 {
				t.Fatalf("Check() items = %#v, want one item", result.Items)
			}
			if got := result.Items[0].Scope; got != test.wantScope {
				t.Errorf("item scope = %q, want %q", got, test.wantScope)
			}
			if got := result.Items[0].AvailableVersion; got != test.wantVersion {
				t.Errorf("item version = %q, want %q", got, test.wantVersion)
			}
		})
	}
}

func TestOperatingSystemCheckMapsCurrentVersionAndRestartState(t *testing.T) {
	bootcProvider := newOperatingSystem(OSDeps{
		BootcBooted:         func() bool { return true },
		BootcStageAvailable: func() bool { return true },
		BootcCheck: func(context.Context) (bootc.AvailableUpdate, error) {
			return bootc.AvailableUpdate{
				Available: true,
				Version:   "20260907",
				Digest:    "sha256:abc",
			}, nil
		},
		BootcStatus: func(context.Context) (*bootc.Status, error) {
			return &bootc.Status{
				Status: bootc.StatusInfo{
					Booted: &bootc.Deployment{
						Image: &bootc.ImageStatus{Version: "20260901"},
					},
					Staged: &bootc.Deployment{
						Image: &bootc.ImageStatus{Version: "20260906"},
					},
				},
			}, nil
		},
	})

	got, err := bootcProvider.Check(context.Background())
	if err != nil {
		t.Fatalf("bootc Check() error = %v", err)
	}
	want := updateflow.CheckResult{
		Items: []updateflow.Item{{
			Name:             "Operating System",
			CurrentVersion:   "20260901",
			AvailableVersion: "20260907",
			Scope:            "bootc",
		}},
		RestartRequired: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bootc Check() = %#v, want %#v", got, want)
	}

	sysProvider := newOperatingSystem(OSDeps{
		NativeAB:                func() bool { return true },
		SysupdateStageAvailable: func() bool { return true },
		SysupdateCheck: func(context.Context) (sysupdate.AvailableUpdate, error) {
			return sysupdate.AvailableUpdate{Available: true, Version: "20260907134040"}, nil
		},
		SysupdateStatus: func() (sysupdate.Status, error) {
			return sysupdate.Status{
				Check:  &sysupdate.UpdateCheck{RunningVersion: "20260901"},
				Staged: &sysupdate.StagedUpdate{Version: "20260906120000"},
			}, nil
		},
	})

	got, err = sysProvider.Check(context.Background())
	if err != nil {
		t.Fatalf("sysupdate Check() error = %v", err)
	}
	want = updateflow.CheckResult{
		Items: []updateflow.Item{{
			Name:             "Operating System",
			CurrentVersion:   "20260901",
			AvailableVersion: "20260907134040",
			Scope:            "sysupdate",
		}},
		RestartRequired: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sysupdate Check() = %#v, want %#v", got, want)
	}
}

func TestOperatingSystemApplyForwardsProgressAndRefreshesStatus(t *testing.T) {
	var stageCalls, statusCalls int
	var progress []updateflow.Progress
	provider := newOperatingSystem(OSDeps{
		BootcStage: func(_ context.Context, events chan<- bootc.ProgressEvent) error {
			stageCalls++
			events <- bootc.ProgressEvent{Type: bootc.EventMessage, Message: "Downloading image"}
			events <- bootc.ProgressEvent{Type: bootc.EventComplete}
			close(events)
			return nil
		},
		BootcStatus: func(context.Context) (*bootc.Status, error) {
			statusCalls++
			return &bootc.Status{
				Status: bootc.StatusInfo{
					Staged: &bootc.Deployment{Image: &bootc.ImageStatus{Version: "20260907"}},
				},
			}, nil
		},
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{{
		Name:  "Operating System",
		Scope: "bootc",
	}}, func(update updateflow.Progress) {
		progress = append(progress, update)
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if stageCalls != 1 {
		t.Fatalf("BootcStage calls = %d, want 1", stageCalls)
	}
	if statusCalls != 1 {
		t.Fatalf("BootcStatus calls = %d, want one post-stage refresh", statusCalls)
	}
	if result != (updateflow.ApplyResult{Changed: true, RestartRequired: true}) {
		t.Fatalf("Apply() result = %#v, want changed + restart required", result)
	}
	wantProgress := []updateflow.Progress{{
		Source:  updateflow.OperatingSystem,
		Message: "Downloading image",
	}}
	if len(progress) != len(wantProgress) || progress[0] != wantProgress[0] {
		t.Fatalf("progress = %#v, want %#v", progress, wantProgress)
	}
}

func TestOperatingSystemApplyBootcDryRunReportsPreview(t *testing.T) {
	var statusCalls int
	provider := newOperatingSystem(OSDeps{
		BootcDryRun: func() bool { return true },
		BootcStage: func(_ context.Context, events chan<- bootc.ProgressEvent) error {
			close(events)
			return nil
		},
		BootcStatus: func(context.Context) (*bootc.Status, error) {
			statusCalls++
			return &bootc.Status{}, nil
		},
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{{
		Name:  "Operating System",
		Scope: bootcScope,
	}}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result != (updateflow.ApplyResult{Preview: true}) {
		t.Fatalf("Apply() result = %#v, want preview without change", result)
	}
	if statusCalls != 0 {
		t.Fatalf("BootcStatus calls = %d, want 0 for preview", statusCalls)
	}
}

func TestOperatingSystemApplySysupdateDryRunReportsPreview(t *testing.T) {
	var statusCalls int
	provider := newOperatingSystem(OSDeps{
		SysupdateDryRun: func() bool { return true },
		SysupdateStage: func(_ context.Context, events chan<- sysupdate.ProgressEvent) error {
			close(events)
			return nil
		},
		SysupdateStatus: func() (sysupdate.Status, error) {
			statusCalls++
			return sysupdate.Status{}, nil
		},
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{{
		Name:  "Operating System",
		Scope: sysupdateScope,
	}}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result != (updateflow.ApplyResult{Preview: true}) {
		t.Fatalf("Apply() result = %#v, want preview without change", result)
	}
	if statusCalls != 0 {
		t.Fatalf("SysupdateStatus calls = %d, want 0 for preview", statusCalls)
	}
}

func TestOperatingSystemDryRunRemainsPendingAndSkipsMaintenance(t *testing.T) {
	var maintenanceCalls int
	provider := newOperatingSystem(OSDeps{
		BootcDryRun: func() bool { return true },
		BootcStage: func(_ context.Context, events chan<- bootc.ProgressEvent) error {
			close(events)
			return nil
		},
	})
	maintenance := &osTestMaintenance{
		run: func(context.Context, func(updateflow.Progress)) error {
			maintenanceCalls++
			return nil
		},
	}
	current := updateflow.Snapshot{Sources: []updateflow.SourceState{{
		ID:         updateflow.OperatingSystem,
		Configured: true,
		Available:  true,
		Enabled:    true,
		Items: []updateflow.Item{{
			Name:  "Operating System",
			Scope: bootcScope,
		}},
	}}}

	got := updateflow.New([]updateflow.Provider{provider}, maintenance).UpdateAll(
		context.Background(),
		current,
		userprefs.Values{
			OperatingSystem:         true,
			MaintenanceAfterUpdates: true,
		},
		nil,
	)

	if maintenanceCalls != 0 {
		t.Fatalf("maintenance calls = %d, want 0 during preview", maintenanceCalls)
	}
	source := got.Sources[0]
	if !got.Preview || source.Completed || len(source.Items) != 1 || got.TotalUpdates != 1 {
		t.Fatalf("preview snapshot = %#v, want pending actionable OS item", got)
	}
	if len(got.CompletedSources) != 0 {
		t.Fatalf("completed sources = %#v, want none during preview", got.CompletedSources)
	}
}

func TestOperatingSystemApplyPropagatesStageErrorWithoutRefreshingStatus(t *testing.T) {
	wantErr := errors.New("stage failed")
	statusCalls := 0
	provider := newOperatingSystem(OSDeps{
		SysupdateStage: func(_ context.Context, events chan<- sysupdate.ProgressEvent) error {
			close(events)
			return wantErr
		},
		SysupdateStatus: func() (sysupdate.Status, error) {
			statusCalls++
			return sysupdate.Status{}, nil
		},
	})

	_, err := provider.Apply(context.Background(), []updateflow.Item{{
		Name:  "Operating System",
		Scope: "sysupdate",
	}}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Apply() error = %v, want %v", err, wantErr)
	}
	if statusCalls != 0 {
		t.Fatalf("SysupdateStatus calls = %d, want 0 after stage failure", statusCalls)
	}
}

type osTestMaintenance struct {
	run func(context.Context, func(updateflow.Progress)) error
}

func (m *osTestMaintenance) Run(
	ctx context.Context,
	progress func(updateflow.Progress),
) error {
	return m.run(ctx, progress)
}

func TestOperatingSystemApplyUsesItemScope(t *testing.T) {
	var bootcCalls, sysupdateCalls int
	provider := newOperatingSystem(OSDeps{
		BootcStage: func(_ context.Context, events chan<- bootc.ProgressEvent) error {
			bootcCalls++
			close(events)
			return nil
		},
		BootcStatus: func(context.Context) (*bootc.Status, error) {
			return &bootc.Status{}, nil
		},
		SysupdateStage: func(_ context.Context, events chan<- sysupdate.ProgressEvent) error {
			sysupdateCalls++
			close(events)
			return nil
		},
		SysupdateStatus: func() (sysupdate.Status, error) {
			return sysupdate.Status{}, nil
		},
	})

	if _, err := provider.Apply(context.Background(), []updateflow.Item{{
		Name:  "Operating System",
		Scope: "sysupdate",
	}}, nil); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if bootcCalls != 0 || sysupdateCalls != 1 {
		t.Fatalf("stage calls = bootc %d, sysupdate %d; want 0, 1", bootcCalls, sysupdateCalls)
	}
}
