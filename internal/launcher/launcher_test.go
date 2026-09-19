package launcher

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStartReportsNonzeroExit(t *testing.T) {
	cmd := launcherTestCommand("7")
	reported := make(chan error, 1)

	if err := Start(cmd, func(err error) { reported <- err }); err != nil {
		t.Fatalf("Start() returned start error: %v", err)
	}

	select {
	case err := <-reported:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("reported error type = %T, want *exec.ExitError", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not report the asynchronous launcher failure")
	}
}

func TestStartReturnsSynchronousStartError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-launcher")
	reported := make(chan error, 1)

	err := Start(exec.Command(missing), func(err error) { reported <- err })
	if err == nil {
		t.Fatal("Start() returned nil for a missing launcher")
	}

	select {
	case err := <-reported:
		t.Fatalf("reportFailure called for a process that never started: %v", err)
	default:
	}
}

func TestLauncherHelperProcess(t *testing.T) {
	if os.Getenv("CHAIRLIFT_LAUNCHER_TEST_HELPER") != "1" {
		return
	}
	if len(os.Args) < 1 || os.Args[len(os.Args)-1] != "7" {
		os.Exit(2)
	}
	os.Exit(7)
}

func launcherTestCommand(exitCode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLauncherHelperProcess$", "--", exitCode)
	cmd.Env = append(os.Environ(), "CHAIRLIFT_LAUNCHER_TEST_HELPER=1")
	return cmd
}
