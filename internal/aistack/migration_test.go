package aistack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestExistingServiceMigrationStopsHiddenLegacyOffload(t *testing.T) {
	h := newHost(t)
	current, err := RenderUnit(h.exe)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.ReplaceAll(current, "Environment=LLMMAN_PEERS=\n", "")
	path, _ := UnitPath()
	if err := writeAtomic(path, legacy); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(h.config, "llmman.conf")
	unrelated := "user-managed configuration"
	if err := os.WriteFile(configPath, []byte(unrelated), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileService(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != current+serviceInvocationMark+strings.Repeat("a", 32)+"\n" {
		t.Fatalf("local-only unit lacks its applied invocation: %q, %v", got, err)
	}
	got, err = os.ReadFile(configPath)
	if err != nil || string(got) != unrelated {
		t.Fatal("service migration changed unrelated llmman configuration")
	}
	h.calls = nil
	if err := ReconcileService(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestServiceMigrationFailurePreservesTheManagementHandle(t *testing.T) {
	h := newHost(t)
	current, _ := RenderUnit(h.exe)
	legacy := strings.ReplaceAll(current, "Environment=LLMMAN_PEERS=\n", "")
	path, _ := UnitPath()
	if err := writeAtomic(path, legacy); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("manager rejected reload")
	h.fail["systemctl --user daemon-reload"] = failure
	if err := ReconcileService(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("migration error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != current {
		t.Fatal("failed migration did not preserve the unstamped management unit")
	}
}

func TestServiceMigrationPreviewAndUnconfiguredHostMutateNothing(t *testing.T) {
	h := newHost(t)
	if err := ReconcileService(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, _ := UnitPath()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("migration configured a disabled host")
	}
	if err := writeAtomic(path, "legacy unit"); err != nil {
		t.Fatal(err)
	}
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	if err := ReconcileService(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "legacy unit" || len(h.calls) != 0 {
		t.Fatal("preview migrated the service")
	}
}

func TestServiceMigrationRecoversAfterTheUnitWasWrittenButNotApplied(t *testing.T) {
	h := newHost(t)
	current, _ := RenderUnit(h.exe)
	path, _ := UnitPath()
	if err := writeAtomic(path, current); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(h.calls, "\n"), "systemctl --user restart "+ServiceName) {
		t.Fatal("the already-written unit left its legacy daemon running")
	}
}

func TestServiceMigrationRetriesAfterTheRunningInvocationChanges(t *testing.T) {
	h := newHost(t)
	current, _ := RenderUnit(h.exe)
	path, _ := UnitPath()
	if err := writeAtomic(path, current+serviceInvocationMark+strings.Repeat("b", 32)+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileService(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != current+serviceInvocationMark+strings.Repeat("a", 32)+"\n" {
		t.Fatal("a previous process's stamp was treated as current policy adoption")
	}
}

func TestServiceMigrationFailureNeverStampsUnverifiedPolicy(t *testing.T) {
	for _, stage := range []string{"reload", "restart", "empty invocation", "invalid invocation"} {
		t.Run(stage, func(t *testing.T) {
			h := newHost(t)
			current, _ := RenderUnit(h.exe)
			path, _ := UnitPath()
			if err := writeAtomic(path, current); err != nil {
				t.Fatal(err)
			}
			switch stage {
			case "reload":
				h.fail["systemctl --user daemon-reload"] = errors.New("reload failed")
			case "restart":
				h.fail["systemctl --user restart "+ServiceName] = errors.New("restart failed")
			case "empty invocation":
				h.outputs["systemctl --user show "+ServiceName+" --property=InvocationID --value"] = ""
			case "invalid invocation":
				h.outputs["systemctl --user show "+ServiceName+" --property=InvocationID --value"] = strings.Repeat("a", 31) + "\n[Service]\n"
			}
			if err := ReconcileService(context.Background()); err == nil {
				t.Fatal("unapplied policy was accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != current {
				t.Fatal("failure lost the management unit or stamped unverified policy")
			}
		})
	}
}

func TestEnablePolicyObservationFailurePreservesRunningServiceManagement(t *testing.T) {
	for _, state := range []string{"active", "inactive"} {
		t.Run(state, func(t *testing.T) {
			h := newHost(t)
			observationErr := errors.New("invocation query failed after successful restart")
			h.fail["systemctl --user show "+ServiceName+" --property=InvocationID --value"] = observationErr
			h.fail["systemctl --user disable --now "+ServiceName] = errors.New("stop failed")
			h.outputs["systemctl --user is-active "+ServiceName] = state
			if err := Enable(context.Background()); !errors.Is(err, observationErr) {
				t.Fatalf("Enable error = %v", err)
			}
			wantPresent := state == "active"
			if h.exists(t, unitRel) != wantPresent || h.exists(t, envRel) != wantPresent {
				t.Fatalf("post-start failure lost management of a %s service", state)
			}
		})
	}
}
