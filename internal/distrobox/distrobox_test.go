package distrobox

import (
	"context"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withFakeDistrobox puts an executable script named "distrobox" first on
// $PATH, so IsInstalled and RemoveAll exercise a real subprocess without
// depending on the test host actually having Distrobox.
func withFakeDistrobox(t *testing.T, script string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "distrobox")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake distrobox: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestIsInstalledReflectsPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if IsInstalled() {
		t.Error("IsInstalled() = true with an empty PATH")
	}

	withFakeDistrobox(t, "#!/bin/sh\nexit 0\n")
	if !IsInstalled() {
		t.Error("IsInstalled() = false with distrobox on PATH")
	}
}

func TestRemoveAllRunsTheExpectedArgv(t *testing.T) {
	captured := filepath.Join(t.TempDir(), "captured-args")
	withFakeDistrobox(t, "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+captured+"\nexit 0\n")

	if err := RemoveAll(context.Background()); err != nil {
		t.Fatalf("RemoveAll() error = %v, want nil", err)
	}

	data, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("reading captured argv: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	want := []string{"rm", "--all", "--force"}
	if len(got) != len(want) {
		t.Fatalf("distrobox argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRemoveAllSurfacesFailureOutput(t *testing.T) {
	withFakeDistrobox(t, "#!/bin/sh\necho 'container busy' >&2\nexit 1\n")

	err := RemoveAll(context.Background())
	if err == nil {
		t.Fatal("RemoveAll() error = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "container busy") {
		t.Errorf("RemoveAll() error = %q, want it to contain the command's own output", err.Error())
	}
}

func TestDryRunNeverExecutesDistrobox(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	t.Setenv("PATH", t.TempDir()) // no distrobox binary at all
	if err := RemoveAll(context.Background()); err != nil {
		t.Fatalf("dry-run RemoveAll() error = %v, want nil", err)
	}
}

func TestRemoveAllReportsAMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := RemoveAll(context.Background()); err == nil {
		t.Fatal("RemoveAll() error = nil, want a failure when distrobox is not on PATH")
	}
}
