package troubleshoot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
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
	previousBrewPath := brewPath
	brewPath = func() string { return "" }
	t.Cleanup(func() { brewPath = previousBrewPath })

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

func TestHomebrewToolsResolveOutsidePath(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	previousBrewPath := brewPath
	brewPath = func() string { return filepath.Join(bin, "brew") }
	t.Cleanup(func() { brewPath = previousBrewPath })
	t.Setenv("PATH", t.TempDir())

	for _, name := range []string{"linux-mcp-server", "goose", "goose-desktop", setupCommand} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(bin, name)
			if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if !defaultLookPath(name) {
				t.Errorf("%s is installed beside the resolved Homebrew but was reported absent", name)
			}
			if name == setupCommand {
				if err := defaultRunSetup(); err != nil {
					t.Fatalf("installed setup script failed outside PATH: %v", err)
				}
			}
		})
	}
}

func TestHomebrewToolResolutionKeepsPathPrecedence(t *testing.T) {
	fakeSetupScript(t, "exit 0")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, setupCommand), []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousBrewPath := brewPath
	brewPath = func() string { return filepath.Join(bin, "brew") }
	t.Cleanup(func() { brewPath = previousBrewPath })

	if err := defaultRunSetup(); err != nil {
		t.Fatalf("setup did not prefer the working PATH executable: %v", err)
	}
}

func TestHomebrewToolResolutionRejectsNonExecutables(t *testing.T) {
	bin := t.TempDir()
	previousBrewPath := brewPath
	brewPath = func() string { return filepath.Join(bin, "brew") }
	t.Cleanup(func() { brewPath = previousBrewPath })
	t.Setenv("PATH", t.TempDir())

	for _, name := range []string{"linux-mcp-server", "goose", "goose-desktop", setupCommand} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(bin, name)
			if err := os.WriteFile(path, []byte("not executable"), 0o644); err != nil {
				t.Fatal(err)
			}
			if defaultLookPath(name) {
				t.Errorf("non-executable %s was reported installed", name)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if defaultLookPath(name) {
				t.Errorf("directory %s was reported installed", name)
			}
		})
	}
}

func TestSetupFindsNewlyInstalledHomebrewToolsOutsidePath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("shell stub requires a POSIX shell")
	}
	bin := t.TempDir()
	config := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", config)
	dryrun.Set(false)

	previousBrewPath, previousLook, previousRead := brewPath, lookPath, readConfig
	previousTap, previousInstall, previousSetup := tapPackage, installPackage, runSetup
	t.Cleanup(func() {
		brewPath, lookPath, readConfig = previousBrewPath, previousLook, previousRead
		tapPackage, installPackage, runSetup = previousTap, previousInstall, previousSetup
	})
	brewPath = func() string { return filepath.Join(bin, "brew") }
	lookPath, readConfig, runSetup = defaultLookPath, defaultReadConfig, defaultRunSetup
	tapPackage = func(string) error { return nil }
	installPackage = func(name string, _ bool) error {
		tools := []string{"goose-desktop"}
		if name == ServerFormula {
			tools = []string{"linux-mcp-server", "goose", setupCommand}
		}
		for _, tool := range tools {
			body := "#!/bin/sh\nexit 0\n"
			if tool == setupCommand {
				body = "#!/bin/sh\n/bin/mkdir -p \"$XDG_CONFIG_HOME/goose\"\nprintf '%s' '" + freshConfig + "' > \"$XDG_CONFIG_HOME/goose/config.yaml\"\n"
			}
			if err := os.WriteFile(filepath.Join(bin, tool), []byte(body), 0o755); err != nil {
				return err
			}
		}
		return nil
	}

	after, err := Setup(State{}, nil)
	if err != nil {
		t.Fatalf("fresh setup failed outside the shell's PATH: %v", err)
	}
	if !after.Ready() || !after.DesktopInstalled || after.Provider != "gemini-cli" {
		t.Fatalf("setup left %+v, want a configured Goose desktop session", after)
	}
}

func TestSetupPropagatesUntrustedTapError(t *testing.T) {
	previousTap := tapPackage
	t.Cleanup(func() { tapPackage = previousTap })

	wantTap := "ublue-os/tap"
	tapPackage = func(string) error {
		return &homebrew.UntrustedTapError{
			Message: "tap is untrusted",
			Tap:     wantTap,
		}
	}

	_, err := Setup(State{}, nil)
	if err == nil {
		t.Fatal("Setup succeeded when tapPackage returned an error")
	}
	var trustErr *homebrew.UntrustedTapError
	if !errors.As(err, &trustErr) {
		t.Fatalf("Setup err = %v, want errors.As to unwrap to *homebrew.UntrustedTapError", err)
	}
	if trustErr.Tap != wantTap {
		t.Errorf("unwrapped Tap = %q, want %q", trustErr.Tap, wantTap)
	}
}
