package flatpak

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The names below deliberately avoid a second leading "I": the Tests
// workflow selects unit tests with `-run "^Test[^I]"`, so a test called
// TestIsInstalled… would be silently filtered out of CI.

func TestFlatpakInstallProbeAsksTheHostForItsVersion(t *testing.T) {
	capture := installCapturingFlatpak(t, "exit 0")

	if !IsInstalled() {
		t.Fatal("IsInstalled() = false for a flatpak that exits 0")
	}

	if got := capturedFlatpakArgs(t, capture); len(got) != 1 || got[0] != "--version" {
		t.Fatalf("probe argv = %q, want [--version]", got)
	}
}

func TestFlatpakInstallProbeReportsFalseWhenTheBinaryFails(t *testing.T) {
	installCapturingFlatpak(t, "exit 1")

	if IsInstalled() {
		t.Fatal("IsInstalled() = true for a flatpak that exits 1")
	}
}

func TestFlatpakInstallProbeReportsFalseWhenNothingIsOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if IsInstalled() {
		t.Fatal("IsInstalled() = true with an empty PATH")
	}
}

// A probe that never returns would block whatever called it. IsInstalled
// bounds itself with a context deadline; this asserts the bound is real by
// giving it a flatpak that never exits on its own.
func TestFlatpakInstallProbeBoundsAHangingBinary(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "flatpak")
	// PATH keeps the host's real directories so `sleep` resolves; the fake
	// shadows any real flatpak because dir comes first.
	body := "#!/bin/sh\nsleep 120\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write hanging flatpak: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	start := time.Now()
	installed := IsInstalled()
	elapsed := time.Since(start)

	if installed {
		t.Fatal("IsInstalled() = true for a flatpak that never exited")
	}
	if elapsed > 60*time.Second {
		t.Fatalf("IsInstalled() took %s; the probe is not bounded", elapsed)
	}
}

func TestFlatpakNotFoundErrorReportsItsMessage(t *testing.T) {
	err := &NotFoundError{Message: "Flatpak not found. Please install Flatpak first."}

	if got := err.Error(); got != "Flatpak not found. Please install Flatpak first." {
		t.Fatalf("NotFoundError.Error() = %q", got)
	}
}

// This is the only case that runs IsInstalledCached in the test binary's own
// process, which is what makes the function reachable at all: the memoising
// sync.Once fires here and nowhere else in this package. PATH is narrowed to
// an empty directory so the answer does not depend on whether the machine
// running the suite happens to have Flatpak.
func TestFlatpakCachedProbeAnswersTheSameWayEveryTime(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	first := IsInstalledCached()
	if first {
		t.Fatal("IsInstalledCached() = true with an empty PATH")
	}
	if IsInstalledCached() != first {
		t.Fatal("IsInstalledCached() changed its answer between calls")
	}
}

// IsInstalledCached memoises with a package-level sync.Once, so the fact that
// it probes only once can only be observed in a process that has not already
// run it. Each case re-executes this test binary and reads back what the
// child saw.
func TestFlatpakCachedProbeRunsTheBinaryAtMostOnce(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "available host", body: "exit 0", want: "true true true"},
		// A negative result is cached just as hard as a positive one. That
		// is the behaviour callers get; pin it so a change is deliberate.
		{name: "unavailable host", body: "exit 1", want: "false false false"},
	}

	for _, this := range cases {
		t.Run(this.name, func(t *testing.T) {
			dir := t.TempDir()
			counter := filepath.Join(dir, "calls")
			script := "#!/bin/sh\nprintf 'call\\n' >> \"$CHAIRLIFT_FLATPAK_CALLS\"\n" + this.body + "\n"
			if err := os.WriteFile(filepath.Join(dir, "flatpak"), []byte(script), 0o755); err != nil {
				t.Fatalf("write counting flatpak: %v", err)
			}

			cmd := exec.Command(os.Args[0], "-test.run=^TestFlatpakCachedProbeHelperProcess$")
			cmd.Env = append(os.Environ(),
				"CHAIRLIFT_FLATPAK_CACHE_HELPER=1",
				"CHAIRLIFT_FLATPAK_CALLS="+counter,
				"PATH="+dir,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("helper process: %v\n%s", err, out)
			}

			if got := cachedProbeResults(t, string(out)); got != this.want {
				t.Fatalf("IsInstalledCached() across three calls = %q, want %q\n%s", got, this.want, out)
			}
			if got := probeInvocations(t, counter); got != 1 {
				t.Fatalf("flatpak was executed %d times; IsInstalledCached must probe once", got)
			}
		})
	}
}

func TestFlatpakCachedProbeHelperProcess(t *testing.T) {
	if os.Getenv("CHAIRLIFT_FLATPAK_CACHE_HELPER") != "1" {
		return
	}

	results := make([]string, 0, 3)
	for range 3 {
		if IsInstalledCached() {
			results = append(results, "true")
			continue
		}
		results = append(results, "false")
	}
	_, _ = os.Stdout.WriteString("cached-probe: " + strings.Join(results, " ") + "\n")
}

// cachedProbeResults extracts the helper's marker line from `go test`
// output, which also carries PASS/ok lines the assertions must ignore.
func cachedProbeResults(t *testing.T, output string) string {
	t.Helper()
	const marker = "cached-probe: "
	for _, line := range strings.Split(output, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), marker); ok {
			return rest
		}
	}
	t.Fatalf("helper process printed no %q line:\n%s", marker, output)
	return ""
}

// probeInvocations counts how many times the fake flatpak ran. A missing
// file means it never ran at all, which is zero rather than an error.
func probeInvocations(t *testing.T, counter string) int {
	t.Helper()
	data, err := os.ReadFile(counter)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read probe counter: %v", err)
	}
	return len(strings.Fields(string(data)))
}
