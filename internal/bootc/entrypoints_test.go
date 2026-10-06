package bootc

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/projectbluefin/chairlift/internal/registrytags"
)

// installFakeBootc writes an executable named exactly `bootc` into a fresh
// temp dir, prepends that dir to $PATH, and returns the file the fake
// records its argv into. It exercises the production name-resolution path:
// GetStatus passes the fixed bootcCommand constant, so the only way to
// reach it from a test is through $PATH.
func installFakeBootc(t *testing.T, body string) string {
	t.Helper()
	// These tests cover the bootc fallback, so the host looks like one that
	// did not boot from composefs whatever the developer's machine is.
	withHostRoot(t, fstest.MapFS{})
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
	withHostRoot(t, fstest.MapFS{})

	_, err := GetStatus(testContext(t))
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("GetStatus with no bootc on PATH = %v, want *NotFoundError", err)
	}
}

func TestBootedGateTrueOnBootedHost(t *testing.T) {
	installFakeBootc(t, "cat <<'JSON'\n"+bootedStagedJSON+"\nJSON\n")

	if !IsBootcBooted(testContext(t)) {
		t.Error("IsBootcBooted() = false on a booted host, want true")
	}
}

func TestBootedGateFalseOnNonBootcHostThatExitsZero(t *testing.T) {
	// The gate is the booted field, not the exit code: `bootc status`
	// exits 0 with "booted": null on a non-bootc host.
	installFakeBootc(t, "cat <<'JSON'\n"+nonBootcJSON+"\nJSON\n")

	if IsBootcBooted(testContext(t)) {
		t.Error("IsBootcBooted() = true for a null booted entry, want false")
	}
}

func TestBootedGateFalseWhenStatusFails(t *testing.T) {
	installFakeBootc(t, "echo 'boom' >&2\nexit 1\n")

	if IsBootcBooted(testContext(t)) {
		t.Error("IsBootcBooted() = true after a failed status read, want false")
	}
}

func TestBootedGateFalseOnMalformedJSON(t *testing.T) {
	installFakeBootc(t, "echo 'not json'\n")

	if IsBootcBooted(testContext(t)) {
		t.Error("IsBootcBooted() = true for unparseable status output, want false")
	}
}

func withHostRoot(t *testing.T, root fs.FS) {
	t.Helper()
	previous := hostRoot
	hostRoot = root
	t.Cleanup(func() { hostRoot = previous })
}

func withRegistryTag(t *testing.T, resolve tagResolver) {
	t.Helper()
	previous := registryTag
	registryTag = resolve
	t.Cleanup(func() { registryTag = previous })
}

// On a composefs host the entry points must not run bootc at all: bootc
// 1.16 refuses both reads to an unprivileged caller.
func TestEntryPointsOnComposefsNeverRunBootc(t *testing.T) {
	argv := installFakeBootc(t, "echo 'error: must be executed as the root user' >&2; exit 1\n")
	withHostRoot(t, dakotaHost())
	withRegistryTag(t, func(context.Context, string, string) (registrytags.Tag, error) {
		return registrytags.Tag{Digest: "sha256:newer", Created: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)}, nil
	})

	status, err := GetStatus(testContext(t))
	if err != nil || !status.Booted() {
		t.Fatalf("GetStatus = %+v, %v; want the composefs deployment", status, err)
	}
	if !IsBootcBooted(testContext(t)) {
		t.Fatal("IsBootcBooted = false on a composefs host where bootc status is denied")
	}
	update, err := CheckUpdate(testContext(t))
	if err != nil || !update.Available || update.Version != "20260926" {
		t.Fatalf("CheckUpdate = %+v, %v; want an available update from the registry", update, err)
	}
	if _, err := os.Stat(argv); err == nil {
		t.Fatal("bootc was executed on a composefs host")
	}
}

// A composefs `bootc upgrade` on a current system fails instead of exiting 0,
// so staging must not run the script when the registry reports nothing newer
// than the booted (or already staged) deployment. Every other answer, and
// every other host, still runs it.
func TestStageUpdateSkipsTheScriptOnlyWhenComposefsIsCurrent(t *testing.T) {
	created := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		host      fs.FS
		digest    string
		lookupErr error
		wantStage bool
	}{
		{"composefs host on the published image", dakotaHost(), bootedDigest, nil, false},
		{"composefs host with the published image already staged", dakotaHost(), stagedDigest, nil, false},
		{"composefs host behind the registry", dakotaHost(), "sha256:newer", nil, true},
		{"composefs host whose registry check fails", dakotaHost(), "", errors.New("registry down"), true},
		{"host that is not composefs", fstest.MapFS{"proc/cmdline": {Data: []byte("root=UUID=1 ostree=/ostree/boot.1/x\n")}}, bootedDigest, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withHostRoot(t, tc.host)
			withRegistryTag(t, func(context.Context, string, string) (registrytags.Tag, error) {
				return registrytags.Tag{Digest: tc.digest, Created: created}, tc.lookupErr
			})
			staged := false
			ch := make(chan ProgressEvent, 4)
			err := stageUpdate(testContext(t), ch, func(_ context.Context, ch chan<- ProgressEvent) error {
				staged = true
				close(ch)
				return nil
			})
			if err != nil {
				t.Fatalf("stageUpdate: %v", err)
			}
			if staged != tc.wantStage {
				t.Fatalf("stage script ran = %v, want %v", staged, tc.wantStage)
			}
			var events []ProgressEvent
			for ev := range ch {
				events = append(events, ev)
			}
			if !tc.wantStage && (len(events) != 1 || events[0].Type != EventComplete) {
				t.Errorf("events = %+v, want one completion", events)
			}
		})
	}
}
