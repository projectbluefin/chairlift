package homebrew

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// withHostResolution pins both halves of executable resolution for one test:
// `brew` resolves to onPath, or resolves nowhere when onPath is empty, and the
// Linuxbrew fallback is pointed at fallbackPath. Both are restored when the
// test ends.
//
// Substituting the probes rather than the host is what lets this file assert
// the fallback on a machine that has no Linuxbrew, and assert absence on a
// machine that has one — the same reason internal/troubleshoot keeps a
// lookPath seam.
func withHostResolution(t *testing.T, onPath, fallbackPath string) {
	t.Helper()

	originalLookPath, originalFallback := lookPath, linuxbrewExecutable
	lookPath = func(string) (string, error) {
		if onPath == "" {
			return "", exec.ErrNotFound
		}
		return onPath, nil
	}
	linuxbrewExecutable = fallbackPath

	t.Cleanup(func() {
		lookPath, linuxbrewExecutable = originalLookPath, originalFallback
	})
}

// fakeBrewExecutable writes an executable shell script that appends its own
// argv to a log file, and returns both paths. It stands in for a brew binary
// at whichever location resolution chose, so a test can assert which
// executable a command actually ran and with what arguments.
func fakeBrewExecutable(t *testing.T, body string) (script, argvLog string) {
	t.Helper()

	dir := t.TempDir()
	argvLog = filepath.Join(dir, "argv.log")
	script = filepath.Join(dir, "brew")
	content := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + argvLog + "'\n" + body + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, argvLog
}

// absentPath returns a path inside a temporary directory that nothing created.
func absentPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "brew")
}

func TestExecutablePathPrefersBrewOnPath(t *testing.T) {
	onPath, _ := fakeBrewExecutable(t, "exit 0")
	fallback, _ := fakeBrewExecutable(t, "exit 0")
	withHostResolution(t, onPath, fallback)

	if got := ExecutablePath(); got != onPath {
		t.Errorf("ExecutablePath() = %q, want the $PATH brew %q ahead of the fallback %q", got, onPath, fallback)
	}
}

func TestExecutablePathFallsBackToLinuxbrew(t *testing.T) {
	fallback, _ := fakeBrewExecutable(t, "exit 0")
	withHostResolution(t, "", fallback)

	if got := ExecutablePath(); got != fallback {
		t.Errorf("ExecutablePath() = %q, want the Linuxbrew fallback %q when $PATH has no brew", got, fallback)
	}
}

func TestExecutablePathEmptyWhenNeitherResolves(t *testing.T) {
	withHostResolution(t, "", absentPath(t))

	if got := ExecutablePath(); got != "" {
		t.Errorf("ExecutablePath() = %q, want empty for a host with no Homebrew", got)
	}
}

// TestExecutablePathIgnoresADirectoryAtTheFallback holds the `-f` half of the
// fallback test: a directory named brew is not something the wrapper would
// have put on $PATH, and exec'ing it fails, so it is not an installation.
func TestExecutablePathIgnoresADirectoryAtTheFallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "brew")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	withHostResolution(t, "", dir)

	if got := ExecutablePath(); got != "" {
		t.Errorf("ExecutablePath() = %q, want empty when the fallback is a directory", got)
	}
}

func TestBrewExecutableKeepsTheBareNameWhenNothingResolves(t *testing.T) {
	withHostResolution(t, "", absentPath(t))

	if got := brewExecutable(); got != "brew" {
		t.Errorf("brewExecutable() = %q, want the bare name so the exec failure stays *NotFoundError", got)
	}
}

func TestBrewExecutableUsesTheResolvedFallback(t *testing.T) {
	fallback, _ := fakeBrewExecutable(t, "exit 0")
	withHostResolution(t, "", fallback)

	if got := brewExecutable(); got != fallback {
		t.Errorf("brewExecutable() = %q, want the resolved fallback %q", got, fallback)
	}
}

// TestBrewCommandsRunTheResolvedFallbackExecutable is the execution half of the
// issue: a direct binary launch has no brew on $PATH, so the command must run
// the Linuxbrew fallback the visibility probe reported.
func TestBrewCommandsRunTheResolvedFallbackExecutable(t *testing.T) {
	t.Run("IsInstalled", func(t *testing.T) {
		fallback, argvLog := fakeBrewExecutable(t, "exit 0")
		withHostResolution(t, "", fallback)

		if !IsInstalled() {
			t.Fatal("IsInstalled() = false, want true from the fallback executable")
		}
		assertArgv(t, argvLog, []string{"--version"})
	})

	t.Run("read-only command", func(t *testing.T) {
		fallback, argvLog := fakeBrewExecutable(t, emit("{}"))
		withHostResolution(t, "", fallback)

		if _, err := ListInstalledFormulae(); err != nil {
			t.Fatalf("ListInstalledFormulae() error = %v", err)
		}
		assertArgv(t, argvLog, []string{"info --installed --json=v2 --formula"})
	})

	t.Run("state-changing command", func(t *testing.T) {
		fallback, argvLog := fakeBrewExecutable(t, "exit 0")
		withHostResolution(t, "", fallback)

		if err := Install("demo", false); err != nil {
			t.Fatalf("Install() error = %v", err)
		}
		assertArgv(t, argvLog, []string{"install demo"})
	})
}

// TestBrewCommandsPreferThePathBrewOverTheFallback is the discriminating half
// of the test above: the fallback exists too, and must stay untouched.
func TestBrewCommandsPreferThePathBrewOverTheFallback(t *testing.T) {
	onPath, argvLog := fakeBrewExecutable(t, "exit 0")
	fallback, fallbackLog := fakeBrewExecutable(t, "exit 0")
	withHostResolution(t, onPath, fallback)

	if !IsInstalled() {
		t.Fatal("IsInstalled() = false, want true from the $PATH brew")
	}
	assertArgv(t, argvLog, []string{"--version"})

	if got := recordedArgv(t, fallbackLog); got != nil {
		t.Errorf("fallback executable ran %#v, want it untouched while brew is on $PATH", got)
	}
}

// TestAvailabilityFalseWhenNothingResolves pins the short-circuit: with no
// Homebrew anywhere, IsInstalled answers false without exec'ing a command that
// cannot resolve. The name avoids the reserved `TestI` prefix CI's filter
// excludes — see internal/installcheck's TestNoInternalTestNameIsExcludedByTheCIFilter.
func TestAvailabilityFalseWhenNothingResolves(t *testing.T) {
	withHostResolution(t, "", absentPath(t))

	if IsInstalled() {
		t.Error("IsInstalled() = true, want false for a host with no Homebrew")
	}
}

// TestBrewPrefixWithoutResolutionReportsNotFound keeps the not-found contract
// reachable through the new resolution: the bare name is still what reaches
// exec, so the error a caller sees is unchanged.
func TestBrewPrefixWithoutResolutionReportsNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	withHostResolution(t, "", absentPath(t))

	prefix, err := brewPrefix()
	if err == nil {
		t.Fatalf("brewPrefix() = %q, want an error with no Homebrew to resolve", prefix)
	}
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("error %v (%T), want *homebrew.NotFoundError", err, err)
	}
}

// TestHostResolutionIsRestored keeps the seam honest for the rest of the
// package: a test that substitutes resolution must leave the real probes
// behind, or every test after it would resolve against a deleted temp
// directory. The substitution happens in a subtest because a subtest's
// cleanups run when the subtest ends.
func TestHostResolutionIsRestored(t *testing.T) {
	originalLookPath, originalFallback := lookPath, linuxbrewExecutable

	t.Run("substituted", func(t *testing.T) {
		onPath, _ := fakeBrewExecutable(t, "exit 0")
		withHostResolution(t, onPath, absentPath(t))

		if reflect.ValueOf(lookPath).Pointer() == reflect.ValueOf(originalLookPath).Pointer() {
			t.Fatal("withHostResolution did not substitute the probe")
		}
		if linuxbrewExecutable == originalFallback {
			t.Fatal("withHostResolution did not substitute the fallback")
		}
	})

	if got := reflect.ValueOf(lookPath).Pointer(); got != reflect.ValueOf(originalLookPath).Pointer() {
		t.Error("lookPath was not restored after the substituting subtest ended")
	}
	if linuxbrewExecutable != originalFallback {
		t.Errorf("linuxbrewExecutable = %q after the substituting subtest ended, want %q", linuxbrewExecutable, originalFallback)
	}
}
