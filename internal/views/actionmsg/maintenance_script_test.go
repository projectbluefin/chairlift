package actionmsg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/projectbluefin/chairlift/internal/pkexec"
)

// scriptExit returns a real *exec.ExitError carrying status code, the shape
// maintenanceexec.Run returns for a configured script or its pkexec wrapper.
func scriptExit(t *testing.T, code int) error {
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

// Cancelling the PolicyKit prompt for a sudo maintenance script pinned
// "Clean Up Boot Old Entries failed: exit status 126": the script runner
// captures no stderr, so the window's text-based dismissal check never fired.
func TestMaintenanceScriptFailureTreatsDismissalAsCancelled(t *testing.T) {
	const title = "Clean Up Boot Old Entries"
	cases := []struct {
		name      string
		sudo      bool
		code      int
		wantText  string
		wantError bool
	}{
		{"sudo dismissed", true, pkexec.DismissedExitCode, pkexec.CancelledMessage, false},
		{"sudo not authorized", true, 127, title + " failed: exit status 127", true},
		{"sudo script failed", true, 1, title + " failed: exit status 1", true},
		{"unprivileged script's own 126", false, pkexec.DismissedExitCode, title + " failed: exit status 126", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, isError := MaintenanceScriptFailure(title, tc.sudo, scriptExit(t, tc.code))
			if text != tc.wantText || isError != tc.wantError {
				t.Errorf("MaintenanceScriptFailure = (%q, %v), want (%q, %v)", text, isError, tc.wantText, tc.wantError)
			}
		})
	}
}
