package aistack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// writeUnitAt writes the unit and pins its mtime, so tests can order the
// service's start against the write.
func writeUnitAt(t *testing.T, contents string, written time.Time) string {
	t.Helper()
	path, _ := UnitPath()
	if err := writeAtomic(path, contents); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, written, written); err != nil {
		t.Fatal(err)
	}
	return path
}

func restarted(h *host) bool {
	return strings.Contains(strings.Join(h.calls, "\n"), "systemctl --user restart "+ServiceName)
}

// TestServiceStartedFromTheCurrentUnitIsAdoptedWithoutARestart is the #694
// regression: after a reboot the stamped InvocationID is stale, but systemd
// started the daemon from the current file, so it is re-stamped, not
// restarted (a restart drops loaded models and in-flight requests).
func TestServiceStartedFromTheCurrentUnitIsAdoptedWithoutARestart(t *testing.T) {
	for _, stamped := range []bool{true, false} {
		name := "stale stamp"
		if !stamped {
			name = "never stamped"
		}
		t.Run(name, func(t *testing.T) {
			h := newHost(t)
			current, _ := RenderUnit(h.exe)
			contents := current
			if stamped {
				contents += serviceInvocationMark + strings.Repeat("b", 32) + "\n"
			}
			written := time.Now().Add(-time.Hour)
			path := writeUnitAt(t, contents, written)
			booted := strings.Repeat("c", 32)
			h.setServiceState("active", "no", written.Add(time.Minute), booted)

			if err := ReconcileService(context.Background()); err != nil {
				t.Fatal(err)
			}
			if restarted(h) {
				t.Fatal("a daemon already running the current unit was restarted")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != current+serviceInvocationMark+booted+"\n" {
				t.Fatalf("adopted invocation was not stamped: %q, %v", got, err)
			}

			// A later launch in the same boot takes the stamp path.
			h.calls = nil
			if err := ReconcileService(context.Background()); err != nil || restarted(h) {
				t.Fatalf("second launch restarted the daemon or failed: %v", err)
			}
		})
	}
}

// The start-time proof must not accept a daemon that only might run the
// current file: crashes between the write and the restart still migrate.
func TestServiceMigrationStillRestartsWhenAdoptionIsUnproven(t *testing.T) {
	for _, tc := range []struct {
		name       string
		needReload string
		startDelta time.Duration
		active     string
	}{
		// Written and reloaded, crashed before the restart: the loaded unit
		// matches, but the process is older than the file.
		{"started before the write", "no", -time.Minute, "active"},
		// Written, crashed before daemon-reload.
		{"manager not reloaded", "yes", time.Minute, "active"},
		// Same second as the write: not trusted.
		{"started in the write's second", "no", 0, "active"},
		{"activating", "no", time.Minute, "activating"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			current, _ := RenderUnit(h.exe)
			written := time.Now().Add(-time.Hour).Truncate(time.Second)
			writeUnitAt(t, current+serviceInvocationMark+strings.Repeat("b", 32)+"\n", written)
			h.setServiceState(tc.active, tc.needReload, written.Add(tc.startDelta), strings.Repeat("c", 32))
			if err := ReconcileService(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !restarted(h) {
				t.Fatal("unproven policy adoption skipped the restart")
			}
		})
	}
}

// A stopped or failed service runs no older policy; reconciling it must not
// report "needs an update" or stop it just because no invocation exists.
func TestStoppedCurrentServiceNeedsNoMigration(t *testing.T) {
	for _, active := range []string{"inactive", "failed"} {
		t.Run(active, func(t *testing.T) {
			h := newHost(t)
			current, _ := RenderUnit(h.exe)
			contents := current + serviceInvocationMark + strings.Repeat("b", 32) + "\n"
			path := writeUnitAt(t, contents, time.Now().Add(-time.Hour))
			h.setServiceState(active, "no", time.Unix(0, 0), "")
			if err := ReconcileService(context.Background()); err != nil {
				t.Fatalf("a stopped current service was reported as needing an update: %v", err)
			}
			for _, call := range h.calls {
				if strings.Contains(call, " restart ") || strings.Contains(call, " stop ") || strings.Contains(call, "daemon-reload") {
					t.Fatalf("reconcile mutated a stopped current service: %q", call)
				}
			}
			got, _ := os.ReadFile(path)
			if string(got) != contents {
				t.Fatal("reconcile rewrote a stopped current service")
			}
		})
	}
}

// TestObservationFailureFallsBackToRestart verifies that when systemctl fails on
// serviceStateArgs (e.g. older systemd rejecting --timestamp=unix), ReconcileService
// does not return an error and stop the service; instead, if the stamp is stale or
// missing, it falls back to the standard restart path. If the stamp matches the
// running invocation, it requires no restart.
func TestObservationFailureFallsBackToRestart(t *testing.T) {
	t.Run("stale stamp restarts and stamps", func(t *testing.T) {
		h := newHost(t)
		current, _ := RenderUnit(h.exe)
		contents := current + serviceInvocationMark + strings.Repeat("b", 32) + "\n"
		path := writeUnitAt(t, contents, time.Now().Add(-time.Hour))
		h.fail["systemctl --user "+strings.Join(serviceStateArgs, " ")] = errors.New("unknown option: --timestamp=unix")
		liveInvocation := strings.Repeat("a", 32)

		if err := ReconcileService(context.Background()); err != nil {
			t.Fatalf("observation failure caused reconcile error: %v", err)
		}
		if !restarted(h) {
			t.Fatal("observation failure with stale stamp did not restart service")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != current+serviceInvocationMark+liveInvocation+"\n" {
			t.Fatalf("unit was not stamped with live invocation: %q, %v", got, err)
		}
		for _, call := range h.calls {
			if strings.Contains(call, " stop ") {
				t.Fatalf("reconcile stopped the service: %q", call)
			}
		}
	})

	t.Run("matching stamp needs no restart", func(t *testing.T) {
		h := newHost(t)
		current, _ := RenderUnit(h.exe)
		liveInvocation := strings.Repeat("a", 32)
		contents := current + serviceInvocationMark + liveInvocation + "\n"
		path := writeUnitAt(t, contents, time.Now().Add(-time.Hour))
		h.fail["systemctl --user "+strings.Join(serviceStateArgs, " ")] = errors.New("unknown option: --timestamp=unix")

		if err := ReconcileService(context.Background()); err != nil {
			t.Fatalf("observation failure with matching stamp caused reconcile error: %v", err)
		}
		if restarted(h) {
			t.Fatal("observation failure with matching stamp unexpectedly restarted service")
		}
		got, _ := os.ReadFile(path)
		if string(got) != contents {
			t.Fatal("unit was rewritten unexpectedly")
		}
	})
}
