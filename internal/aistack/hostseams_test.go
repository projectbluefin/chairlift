package aistack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/gpu"
)

// fakeBin writes an executable named name into a fresh directory and points
// PATH at that directory alone, so a lookup either finds this stand-in or
// finds nothing at all. The host's real systemctl and podman are therefore
// never consulted, which is what makes these tests deterministic on a
// container runner that has neither. Because PATH is narrowed to that one
// directory, the script may use shell builtins only; @DIR@ expands to the
// directory so a script can capture its argv without calling dirname.
func fakeBin(t *testing.T, name, script string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, name)
	script = strings.ReplaceAll(script, "@DIR@", dir)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir)
	return dir
}

func TestExecSystemctlPassesUserScopeAndArguments(t *testing.T) {
	dir := fakeBin(t, "systemctl", "#!/bin/sh\nprintf '%s\\n' \"$@\" > @DIR@/argv\nexit 0\n")

	if err := execSystemctl(context.Background(), "start", ServiceName); err != nil {
		t.Fatalf("execSystemctl: %v", err)
	}

	recorded, err := os.ReadFile(filepath.Join(dir, "argv"))
	if err != nil {
		t.Fatalf("reading captured argv: %v", err)
	}
	got := strings.Fields(string(recorded))
	want := []string{"--user", "start", ServiceName}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("systemctl argv = %v, want %v", got, want)
	}
}

func TestExecSystemctlReportsTheCommandAndItsOutput(t *testing.T) {
	fakeBin(t, "systemctl", "#!/bin/sh\necho 'Unit chairlift-ai.service not found.' >&2\nexit 5\n")

	err := execSystemctl(context.Background(), "start", ServiceName)
	if err == nil {
		t.Fatal("execSystemctl returned no error when systemctl failed")
	}
	for _, want := range []string{"systemctl --user start " + ServiceName, "Unit chairlift-ai.service not found."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestExecSystemctlFailsWhenSystemctlIsAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if err := execSystemctl(context.Background(), "daemon-reload"); err == nil {
		t.Fatal("execSystemctl returned no error on a host without systemctl")
	}
}

func TestAvailabilityFollowsPodmanOnPath(t *testing.T) {
	fakeBin(t, "podman", "#!/bin/sh\nexit 0\n")
	if !IsAvailable() {
		t.Error("IsAvailable reported false with podman on PATH")
	}

	t.Setenv("PATH", t.TempDir())
	if IsAvailable() {
		t.Error("IsAvailable reported true with no podman on PATH")
	}
}

func TestDefaultUnitDirIsTheQuadletDirectoryUnderTheUserConfigDir(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)

	dir, err := defaultUnitDir()
	if err != nil {
		t.Fatalf("defaultUnitDir: %v", err)
	}
	want := filepath.Join(config, "containers", "systemd")
	if dir != want {
		t.Errorf("defaultUnitDir = %q, want %q", dir, want)
	}
}

func TestDefaultUnitDirFailsWithoutAUserConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if _, err := defaultUnitDir(); err == nil {
		t.Fatal("defaultUnitDir returned no error with neither XDG_CONFIG_HOME nor HOME set")
	}
}

func TestUnitPathUsesTheQuadletDirectory(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)

	path, err := UnitPath()
	if err != nil {
		t.Fatalf("UnitPath: %v", err)
	}
	want := filepath.Join(config, "containers", "systemd", UnitName)
	if path != want {
		t.Errorf("UnitPath = %q, want %q", path, want)
	}
}

// stubBrokenUnitDir makes the quadlet directory unresolvable, the state a
// host without a usable config directory is in.
func stubBrokenUnitDir(t *testing.T) error {
	t.Helper()

	failure := errors.New("no config directory")
	previous := unitDir
	t.Cleanup(func() { unitDir = previous })
	unitDir = func() (string, error) { return "", failure }
	return failure
}

func TestUnitPathPropagatesAnUnresolvableDirectory(t *testing.T) {
	failure := stubBrokenUnitDir(t)

	path, err := UnitPath()
	if !errors.Is(err, failure) {
		t.Fatalf("UnitPath error = %v, want %v", err, failure)
	}
	if path != "" {
		t.Errorf("UnitPath = %q, want an empty path alongside the error", path)
	}
}

func TestEnabledStateIsFalseWhenTheDirectoryCannotBeResolved(t *testing.T) {
	_ = stubBrokenUnitDir(t)

	if IsEnabled() {
		t.Error("IsEnabled reported true when the quadlet directory is unresolvable")
	}
}

func TestEnableAndDisablePropagateAnUnresolvableDirectory(t *testing.T) {
	failure := stubBrokenUnitDir(t)

	if err := Enable(context.Background(), Select(gpu.Set{})); !errors.Is(err, failure) {
		t.Errorf("Enable error = %v, want %v", err, failure)
	}
	if err := Disable(context.Background()); !errors.Is(err, failure) {
		t.Errorf("Disable error = %v, want %v", err, failure)
	}
}

func TestDetectSelectsTheStackForTheDetectedHardware(t *testing.T) {
	got := Detect()
	want := Select(gpu.Detect())

	if got.Vendor != want.Vendor || got.Image != want.Image || got.Accelerator != want.Accelerator {
		t.Fatalf("Detect = %+v, want %+v", got, want)
	}
	if got.Image == "" {
		t.Error("Detect returned a stack with no image; every vendor must map to one")
	}
}

func TestDefaultContextCarriesTheCommandTimeout(t *testing.T) {
	ctx, cancel := DefaultContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("DefaultContext returned a context with no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > commandTimeout {
		t.Errorf("DefaultContext deadline is %v away, want (0, %v]", remaining, commandTimeout)
	}

	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("ctx.Err() = %v after cancel, want %v", ctx.Err(), context.Canceled)
	}
}

func TestEnableFailsWhenTheQuadletDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "containers")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("writing blocking file: %v", err)
	}

	previous := unitDir
	t.Cleanup(func() { unitDir = previous })
	unitDir = func() (string, error) { return filepath.Join(blocker, "systemd"), nil }

	if err := Enable(context.Background(), Select(gpu.Set{})); err == nil {
		t.Fatal("Enable returned no error when its directory could not be created")
	}
}

func TestDisablePropagatesAnUnremovableUnit(t *testing.T) {
	dir, calls := stubUnitDir(t)

	// A directory in the unit's place fails os.Remove with something other
	// than "not exist", which is the one removal error Disable must report.
	occupied := filepath.Join(dir, UnitName)
	if err := os.MkdirAll(filepath.Join(occupied, "child"), 0o755); err != nil {
		t.Fatalf("occupying the unit path: %v", err)
	}

	if err := Disable(context.Background()); err == nil {
		t.Fatal("Disable returned no error when the unit could not be removed")
	}
	if last := *calls; len(last) == 0 || last[len(last)-1] != "stop "+ServiceName {
		t.Errorf("systemctl calls = %v, want the stop attempt and no daemon-reload after the failure", *calls)
	}
}

func TestEnableRemovesTheUnitWhenSystemdWillNotReload(t *testing.T) {
	dir, _ := stubUnitDir(t)

	runSystemctl = func(_ context.Context, args ...string) error {
		if args[0] == "daemon-reload" {
			return errors.New("failed to reload daemon")
		}
		return nil
	}

	if err := Enable(context.Background(), Select(gpu.Set{Intel: true})); err == nil {
		t.Fatal("Enable returned no error when daemon-reload failed")
	}
	if _, err := os.Stat(filepath.Join(dir, UnitName)); !os.IsNotExist(err) {
		t.Error("a unit systemd never loaded was left on disk")
	}
}
