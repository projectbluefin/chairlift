package pageview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/projectbluefin/chairlift/internal/helperexec"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/stageexec"
)

// exitStatus returns a real *exec.ExitError carrying code, the error a
// pkexec invocation returns.
func exitStatus(t *testing.T, code int) error {
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

// The Updates page's Download, early-updates, and graphics-driver actions
// report fixed failure text without the helper's output, so the window's
// message-based dismissal check never fired and cancelling the password
// prompt left a persistent error toast. Each error shape those actions
// return must be classified from the error itself.
func TestPrivilegedFailureToastTreatsDismissalAsCancellation(t *testing.T) {
	const failure = "Could not switch the graphics driver"
	dismissed := exitStatus(t, pkexec.DismissedExitCode)
	tests := []struct {
		name        string
		err         error
		wantToast   string
		wantIsError bool
	}{
		{
			name:      "stage helper dismissed (stageexec)",
			err:       &stageexec.Error{Message: "update staging failed (exit 126)", Err: dismissed},
			wantToast: pkexec.CancelledMessage,
		},
		{
			name:      "ublue helper dismissed (helperexec)",
			err:       &helperexec.Error{Message: "command failed (exit 126): ", Err: dismissed},
			wantToast: pkexec.CancelledMessage,
		},
		{
			name:        "not authorized is a real failure",
			err:         &helperexec.Error{Message: "command failed (exit 127): Not authorized", Err: exitStatus(t, 127)},
			wantToast:   failure,
			wantIsError: true,
		},
		{
			name:        "helper failure",
			err:         &stageexec.Error{Message: "update staging failed (exit 1): no space left", Err: exitStatus(t, 1)},
			wantToast:   failure,
			wantIsError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			toast, isError := PrivilegedFailureToast(test.err, failure)
			if toast != test.wantToast || isError != test.wantIsError {
				t.Fatalf("PrivilegedFailureToast() = (%q, %v), want (%q, %v)", toast, isError, test.wantToast, test.wantIsError)
			}
		})
	}
}
