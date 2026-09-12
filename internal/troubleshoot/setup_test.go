package troubleshoot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// fakeSetupScript puts an executable named after setupCommand on $PATH, so
// defaultRunSetup can be exercised without goose-mcp-setup being installed.
func fakeSetupScript(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("shell stub requires a POSIX shell")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, setupCommand)
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	t.Setenv("PATH", dir)
}

func TestConfigPathUsesUserConfigDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	got, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	want := filepath.Join(base, "goose", "config.yaml")
	if got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
}

func TestConfigPathErrorsWithoutAHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("UserConfigDir only fails this way on unix")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if _, err := ConfigPath(); err == nil {
		t.Fatal("ConfigPath succeeded with neither XDG_CONFIG_HOME nor HOME set")
	}
}

func TestDefaultReadConfigReadsGooseConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := os.MkdirAll(filepath.Join(base, "goose"), 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "goose", "config.yaml"), []byte(freshConfig), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	data, err := defaultReadConfig()
	if err != nil {
		t.Fatalf("defaultReadConfig: %v", err)
	}
	// The read is what Detect feeds to ParseConfig, so assert on the fact
	// that survives the round trip rather than on the bytes.
	if state := ParseConfig(data); !state.Wired || state.Provider != "gemini-cli" {
		t.Errorf("round trip = %+v, want Wired with provider gemini-cli", state)
	}
}

func TestDefaultReadConfigErrorsWhenAbsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, err := defaultReadConfig(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("defaultReadConfig error = %v, want os.ErrNotExist", err)
	}
}

func TestDefaultReadConfigPropagatesConfigPathError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("UserConfigDir only fails this way on unix")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if _, err := defaultReadConfig(); err == nil {
		t.Fatal("defaultReadConfig succeeded with no resolvable config dir")
	}
}

func TestDefaultRunSetupDryRunSkipsTheCommand(t *testing.T) {
	// No stub on $PATH: if dry-run did not short-circuit, the exec would fail.
	t.Setenv("PATH", t.TempDir())
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	if err := defaultRunSetup(); err != nil {
		t.Errorf("defaultRunSetup in dry-run = %v, want nil", err)
	}
}

func TestDefaultRunSetupSucceeds(t *testing.T) {
	dryrun.Set(false)
	fakeSetupScript(t, "exit 0")

	if err := defaultRunSetup(); err != nil {
		t.Errorf("defaultRunSetup = %v, want nil", err)
	}
}

func TestDefaultRunSetupWrapsFailureOutput(t *testing.T) {
	dryrun.Set(false)
	fakeSetupScript(t, "echo 'no provider configured' >&2\nexit 3")

	err := defaultRunSetup()
	if err == nil {
		t.Fatal("defaultRunSetup succeeded on a failing command")
	}

	var setupErr *Error
	if !errors.As(err, &setupErr) {
		t.Fatalf("error type = %T, want *troubleshoot.Error", err)
	}
	if setupErr.Message != "no provider configured" {
		t.Errorf("Message = %q, want the command's combined output", setupErr.Message)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("wrapped error = %v, want an *exec.ExitError to remain unwrappable", err)
	}
}

func TestDefaultRunSetupErrorsWhenCommandMissing(t *testing.T) {
	dryrun.Set(false)
	t.Setenv("PATH", t.TempDir())

	if err := defaultRunSetup(); err == nil {
		t.Fatal("defaultRunSetup succeeded with goose-mcp-setup absent")
	}
}

// TestStepsDispatchTheRightPackages pins the arguments each step hands to
// Homebrew. Detect cannot catch a wrong name here: a step that tapped or
// installed the wrong thing would simply leave the feature undetected.
func TestStepsDispatchTheRightPackages(t *testing.T) {
	previousTap, previousInstall, previousSetup := tapPackage, installPackage, runSetup
	t.Cleanup(func() {
		tapPackage, installPackage, runSetup = previousTap, previousInstall, previousSetup
	})

	var taps []string
	type install struct {
		name string
		cask bool
	}
	var installs []install
	setupRuns := 0

	tapPackage = func(name string) error { taps = append(taps, name); return nil }
	installPackage = func(name string, cask bool) error {
		installs = append(installs, install{name, cask})
		return nil
	}
	runSetup = func() error { setupRuns++; return nil }

	steps := Steps()
	if len(steps) != 4 {
		t.Fatalf("Steps() returned %d steps, want 4", len(steps))
	}
	for _, step := range steps {
		if err := step.Run(); err != nil {
			t.Fatalf("%s: %v", step.Name, err)
		}
	}

	if len(taps) != 1 || taps[0] != Tap {
		t.Errorf("taps = %v, want [%s]", taps, Tap)
	}
	want := []install{{ServerFormula, false}, {DesktopCask, true}}
	if len(installs) != len(want) {
		t.Fatalf("installs = %v, want %v", installs, want)
	}
	for i, w := range want {
		if installs[i] != w {
			t.Errorf("install %d = %v, want %v", i, installs[i], w)
		}
	}
	if setupRuns != 1 {
		t.Errorf("setup script ran %d times, want 1", setupRuns)
	}
}

// TestStepsAlwaysTap guards the one step with no Needed shortcut: brew
// requires the tap before either qualified name resolves, and re-tapping is
// cheap, so it must run on every attempt.
func TestStepsAlwaysTap(t *testing.T) {
	fullyInstalled := State{
		ServerInstalled:  true,
		AgentInstalled:   true,
		DesktopInstalled: true,
		Wired:            true,
	}

	steps := Steps()
	if !steps[0].Needed(fullyInstalled) {
		t.Error("tap step reported not needed on a fully installed host")
	}
	for _, step := range steps[1:] {
		if step.Needed(fullyInstalled) {
			t.Errorf("%s reported needed on a fully installed host", step.Name)
		}
	}
}

func TestErrorPrefersCommandOutput(t *testing.T) {
	err := &Error{Message: "no provider configured", Err: errors.New("exit status 3")}

	if got := err.Error(); got != "no provider configured" {
		t.Errorf("Error() = %q, want the command output", got)
	}
}

func TestErrorFallsBackToWrappedError(t *testing.T) {
	// A command that fails silently leaves Message empty; the user must still
	// see something.
	err := &Error{Err: errors.New("exit status 3")}

	if got := err.Error(); got != "exit status 3" {
		t.Errorf("Error() = %q, want the wrapped error", got)
	}
}

func TestErrorUnwrap(t *testing.T) {
	wrapped := errors.New("exit status 3")
	err := &Error{Message: "output", Err: wrapped}

	if !errors.Is(err, wrapped) {
		t.Errorf("errors.Is did not find the wrapped error through Unwrap")
	}
}
