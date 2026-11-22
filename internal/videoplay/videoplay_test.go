package videoplay

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestResolvePrefersGio(t *testing.T) {
	original := lookPath
	t.Cleanup(func() { lookPath = original })
	lookPath = func(name string) (string, error) {
		if name != "gio" {
			t.Fatalf("unexpected lookPath(%q)", name)
		}
		return "/usr/bin/gio", nil
	}

	name, args := resolve("https://example.invalid/intro")
	if name != "gio" || len(args) != 2 || args[0] != "open" || args[1] != "https://example.invalid/intro" {
		t.Fatalf("resolve = %s %v, want gio [open https://example.invalid/intro]", name, args)
	}
}

func TestResolveFallsBackToXdgOpen(t *testing.T) {
	original := lookPath
	t.Cleanup(func() { lookPath = original })
	lookPath = func(name string) (string, error) {
		if name == "xdg-open" {
			return "/usr/bin/xdg-open", nil
		}
		return "", exec.ErrNotFound
	}

	name, args := resolve("https://example.invalid/intro")
	if name != "xdg-open" || len(args) != 1 || args[0] != "https://example.invalid/intro" {
		t.Fatalf("resolve = %s %v, want xdg-open [https://example.invalid/intro]", name, args)
	}
}

func TestResolveNoHandler(t *testing.T) {
	original := lookPath
	t.Cleanup(func() { lookPath = original })
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	if name, _ := resolve("https://example.invalid/intro"); name != "" {
		t.Fatalf("resolve = %q, want empty", name)
	}
}

// TestLaunchStartsGioAndReportsExitFailure exercises the real start path: the
// command the seams hand back is a live subprocess that exits non-zero, so
// launcher.Start's asynchronous Wait is what reports the error. Stubbing out
// the whole spawn would not catch a reaping regression (a released, never
// waited child) or a silently swallowed exit status.
func TestLaunchStartsGioAndReportsExitFailure(t *testing.T) {
	originalLook := lookPath
	origCmd := newCmd
	t.Cleanup(func() { lookPath = originalLook; newCmd = origCmd })

	lookPath = func(name string) (string, error) {
		if name == "gio" {
			return "/usr/bin/gio", nil
		}
		return "", exec.ErrNotFound
	}
	var gotArgs []string
	newCmd = func(_ string, args ...string) *exec.Cmd {
		gotArgs = args
		return helperProcessCommand()
	}

	reported := make(chan error, 1)
	if err := Launch("https://example.invalid/intro", func(err error) { reported <- err }); err != nil {
		t.Fatalf("Launch returned a start error: %v", err)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "open" || gotArgs[1] != "https://example.invalid/intro" {
		t.Fatalf("command args = %v, want [open https://example.invalid/intro]", gotArgs)
	}

	select {
	case err := <-reported:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("reported error type = %T, want *exec.ExitError", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Launch did not report the player's non-zero exit")
	}
}

func TestLaunchReturnsStartErrorSynchronously(t *testing.T) {
	originalLook := lookPath
	origCmd := newCmd
	t.Cleanup(func() { lookPath = originalLook; newCmd = origCmd })

	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	newCmd = func(string, ...string) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "missing-handler"))
	}

	reported := make(chan error, 1)
	if err := Launch("https://example.invalid/intro", func(err error) { reported <- err }); err == nil {
		t.Fatal("Launch returned nil for a handler that cannot start")
	}
	select {
	case err := <-reported:
		t.Fatalf("onExit called for a process that never started: %v", err)
	default:
	}
}

// TestVideoplayHelperProcess is re-executed by helperProcessCommand as a
// short-lived subprocess that exits non-zero, standing in for a player that
// fails after a successful start.
func TestVideoplayHelperProcess(t *testing.T) {
	if os.Getenv("CHAIRLIFT_VIDEOPLAY_TEST_HELPER") != "1" {
		return
	}
	os.Exit(7)
}

func helperProcessCommand() *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestVideoplayHelperProcess$")
	cmd.Env = append(os.Environ(), "CHAIRLIFT_VIDEOPLAY_TEST_HELPER=1")
	return cmd
}
