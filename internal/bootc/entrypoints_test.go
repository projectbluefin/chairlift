package bootc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// installFakeBootc writes an executable named exactly `bootc` into a fresh
// temp dir, puts that dir alone on $PATH, and returns the file the fake
// records its argv into. It exercises the production name-resolution path:
// GetStatus passes the fixed bootcCommand constant, so the only way to
// reach it from a test is through $PATH.
func installFakeBootc(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\n" + body
	if err := os.WriteFile(filepath.Join(dir, bootcCommand), []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake bootc: %v", err)
	}
	// Prepend rather than replace: the fake is a /bin/sh script and still
	// needs the system utilities it calls, but it shadows any real bootc.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvFile
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestGetStatusResolvesBootcByNameWithJSONFormatArgs(t *testing.T) {
	argvFile := installFakeBootc(t, "cat <<'JSON'\n"+bootedStagedJSON+"\nJSON\n")

	s, err := GetStatus(testContext(t))
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !s.Booted() {
		t.Errorf("Booted() = false, want true")
	}
	if got := s.Status.Booted.Version(); got != "20260701.0" {
		t.Errorf("booted version = %q, want 20260701.0", got)
	}

	data, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("reading captured argv: %v", err)
	}
	got := strings.TrimRight(string(data), "\n")
	const want = "status\n--format\njson"
	if got != want {
		t.Errorf("bootc argv = %q, want %q", got, want)
	}
}

func TestGetStatusMissingBootcIsNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := GetStatus(testContext(t))
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("GetStatus with no bootc on PATH = %v, want *NotFoundError", err)
	}
}

func TestIsBootcBootedTrueOnBootedHost(t *testing.T) {
	installFakeBootc(t, "cat <<'JSON'\n"+bootedStagedJSON+"\nJSON\n")

	if !IsBootcBooted(testContext(t)) {
		t.Error("IsBootcBooted() = false on a booted host, want true")
	}
}

func TestIsBootcBootedFalseOnNonBootcHostThatExitsZero(t *testing.T) {
	// The gate is the booted field, not the exit code: `bootc status`
	// exits 0 with "booted": null on a non-bootc host.
	installFakeBootc(t, "cat <<'JSON'\n"+nonBootcJSON+"\nJSON\n")

	if IsBootcBooted(testContext(t)) {
		t.Error("IsBootcBooted() = true for a null booted entry, want false")
	}
}

func TestIsBootcBootedFalseWhenStatusFails(t *testing.T) {
	installFakeBootc(t, "echo 'boom' >&2\nexit 1\n")

	if IsBootcBooted(testContext(t)) {
		t.Error("IsBootcBooted() = true after a failed status read, want false")
	}
}

func TestIsBootcBootedFalseOnMalformedJSON(t *testing.T) {
	installFakeBootc(t, "echo 'not json'\n")

	if IsBootcBooted(testContext(t)) {
		t.Error("IsBootcBooted() = true for unparseable status output, want false")
	}
}

// TestIsBootcBootedCachedRunsTheCheckAtMostOnce consumes the package-level
// sync.Once, so it is the only test that may call IsBootcBootedCached.
func TestIsBootcBootedCachedRunsTheCheckAtMostOnce(t *testing.T) {
	installFakeBootc(t, "cat <<'JSON'\n"+bootedStagedJSON+"\nJSON\n")

	if !IsBootcBootedCached() {
		t.Fatal("IsBootcBootedCached() = false on a booted host, want true")
	}

	// Remove every bootc from PATH: an uncached second call would now
	// resolve nothing and report false.
	t.Setenv("PATH", t.TempDir())
	if !IsBootcBootedCached() {
		t.Error("IsBootcBootedCached() re-ran the check instead of returning the cached result")
	}
}

func TestStageScriptAvailableTracksTheFixedScriptPath(t *testing.T) {
	_, statErr := os.Stat(StageScriptPath)
	want := statErr == nil

	if got := StageScriptAvailable(); got != want {
		t.Errorf("StageScriptAvailable() = %v, want %v (presence of %s)", got, want, StageScriptPath)
	}
	if StageScriptPath != "/usr/libexec/bootc-update-stage" {
		t.Errorf("StageScriptPath = %q, want the snow-shipped /usr/libexec/bootc-update-stage", StageScriptPath)
	}
}

func TestDefaultContextCarriesTheDefaultTimeout(t *testing.T) {
	ctx, cancel := DefaultContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("DefaultContext() returned a context without a deadline")
	}
	if remaining := time.Until(deadline); remaining <= DefaultTimeout-time.Minute || remaining > DefaultTimeout {
		t.Errorf("DefaultContext() deadline in %v, want ~%v", remaining, DefaultTimeout)
	}

	cancel()
	if ctx.Err() == nil {
		t.Error("cancel() from DefaultContext() did not cancel the context")
	}
}
