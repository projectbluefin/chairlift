package homebrew

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

// Docker's daemon resolves runc from Homebrew's sbin; cask preflights also
// need bin siblings. Exercise actual child lookup, including a direct launch.
func TestBrewChildrenFindBinAndSbinTools(t *testing.T) {
	prefix := t.TempDir()
	bin, sbin := filepath.Join(prefix, "bin"), filepath.Join(prefix, "sbin")
	for _, dir := range []string{bin, sbin} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(bin, "brew")
	for path, body := range map[string]string{
		script:                            "bin-child && sbin-child",
		filepath.Join(bin, "bin-child"):   "exit 0",
		filepath.Join(sbin, "sbin-child"): "exit 0",
	} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/usr/bin:/bin", bin + ":/usr/bin", bin + ":" + sbin + ":/usr/bin", ""} {
		t.Run(path, func(t *testing.T) {
			t.Setenv("PATH", path)
			if _, err := runBrewCommandAt(context.Background(), script, "install", "docker-engine"); err != nil {
				t.Fatalf("Homebrew child could not resolve bin/sbin tools: %v", err)
			}
		})
	}
	cmd := exec.Command(script)
	cmd.Env = WithBrewPath([]string{"HOME=" + prefix}, script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("children could not resolve Homebrew tools without an inherited PATH: %v\n%s", err, output)
	}
}

func TestWithBrewPathLeavesUnresolvedExecutablesAlone(t *testing.T) {
	for _, brew := range []string{"", "brew"} {
		env := []string{"HOME=/h", "PATH=/usr/bin"}
		if got := WithBrewPath(env, brew); !reflect.DeepEqual(got, env) {
			t.Fatalf("relative executable changed environment: %v", got)
		}
	}
}

func TestWithBrewPathIsIdempotentAndDoesNotDuplicateDirectories(t *testing.T) {
	for name, env := range map[string][]string{
		"missing PATH": {"HOME=/h"},
		"empty PATH":   {"PATH="},
		"bin listed":   {"PATH=/lb/bin:/usr/bin"},
		"sbin listed":  {"PATH=/lb/sbin:/usr/bin"},
		"both listed":  {"PATH=/lb/bin:/lb/sbin:/usr/bin"},
	} {
		t.Run(name, func(t *testing.T) {
			first := WithBrewPath(env, "/lb/bin/brew")
			if next := WithBrewPath(first, "/lb/bin/brew"); !reflect.DeepEqual(first, next) {
				t.Fatalf("repeated application grew or reordered PATH: %v -> %v", first, next)
			}
			var path string
			for _, entry := range first {
				if value, ok := strings.CutPrefix(entry, "PATH="); ok {
					path = value
				}
			}
			for _, directory := range []string{"/lb/bin", "/lb/sbin"} {
				count := 0
				for entry := range strings.SplitSeq(path, string(filepath.ListSeparator)) {
					if entry == directory {
						count++
					}
				}
				if count != 1 {
					t.Fatalf("%s appears %d times in %s", directory, count, path)
				}
			}
		})
	}
}

func TestHomebrewBinAndSbinTakePrecedenceWhenAdded(t *testing.T) {
	prefix := t.TempDir()
	bin, sbin, inherited := filepath.Join(prefix, "bin"), filepath.Join(prefix, "sbin"), filepath.Join(prefix, "inherited")
	for _, directory := range []string{bin, sbin, inherited} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	brew := filepath.Join(bin, "brew")
	for path, body := range map[string]string{
		brew:                                       "ordered-tool && engine-runtime",
		filepath.Join(bin, "ordered-tool"):         "exit 0",
		filepath.Join(sbin, "ordered-tool"):        "exit 21",
		filepath.Join(inherited, "ordered-tool"):   "exit 22",
		filepath.Join(sbin, "engine-runtime"):      "exit 0",
		filepath.Join(inherited, "engine-runtime"): "exit 23",
	} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(brew)
	cmd.Env = WithBrewPath([]string{"PATH=" + inherited}, brew)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Homebrew children resolved an inherited or sbin-shadowed executable: %v\n%s", err, output)
	}
}
