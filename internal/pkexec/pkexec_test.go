package pkexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// exitError returns a real *exec.ExitError carrying status code, the shape
// helperexec and stageexec wrap.
func exitError(t *testing.T, code int) error {
	t.Helper()
	script := filepath.Join(t.TempDir(), "exit")
	if err := os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", code)), 0o755); err != nil {
		t.Fatal(err)
	}
	err := exec.CommandContext(context.Background(), script).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("running a script that exits %d returned %v, want *exec.ExitError", code, err)
	}
	return err
}

// A dismissed PolicyKit prompt is the user changing their mind, not a
// failure, so the window shows it briefly instead of pinning raw stderr
// (#492). Only exit 126 and pkexec's dismissal text qualify: 127 (not
// authorized, or no agent) is a real failure.
func TestAuthDismissedClassification(t *testing.T) {
	wrapped := func(err error) error { return fmt.Errorf("command failed (exit 126): %w", err) }
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"exit 126", exitError(t, DismissedExitCode), true},
		{"wrapped exit 126", wrapped(exitError(t, DismissedExitCode)), true},
		{"dismissal text", errors.New("command failed (exit 126): Error executing command as another user: Request dismissed"), true},
		{"not authorized", errors.New("command failed (exit 127): Error executing command as another user: Not authorized"), false},
		{"exit 127", exitError(t, 127), false},
		{"helper failure", exitError(t, 1), false},
		{"unrelated", errors.New("disk read error"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsAuthDismissed(test.err); got != test.want {
				t.Errorf("IsAuthDismissed(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}

	// The window only sees the caller's formatted message.
	if !MessageIsAuthDismissed("Could not change Developer Mode: Error executing command as another user: Request dismissed") {
		t.Error("a view's formatted dismissal message is not recognised")
	}
	if MessageIsAuthDismissed("Could not change Developer Mode: Error executing command as another user: Not authorized") {
		t.Error("a refused authorization must stay a persistent error")
	}
}
