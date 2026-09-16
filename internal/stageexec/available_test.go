package stageexec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScriptAvailableReportsFilePresence(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "stage-script")
	if err := os.WriteFile(present, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing stage script: %v", err)
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"installed script", present, true},
		{"absent script", filepath.Join(dir, "no-such-script"), false},
		{"empty path", "", false},
		{"directory", dir, true},
		{"path under a non-directory", filepath.Join(present, "child"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScriptAvailable(tc.path); got != tc.want {
				t.Errorf("ScriptAvailable(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// ScriptAvailable gates the UI row, so a non-executable but present file
// must still report available: executability is pkexec/polkit's call, not
// the availability probe's.
func TestScriptAvailableIgnoresExecutableBit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("writing stage script: %v", err)
	}

	if !ScriptAvailable(path) {
		t.Error("ScriptAvailable() = false for a present non-executable file, want true")
	}
}

func TestNotFoundErrorMessageAndUnwrap(t *testing.T) {
	cause := os.ErrNotExist
	err := &NotFoundError{Message: "pkexec not found", Err: cause}

	if got := err.Error(); got != "pkexec not found" {
		t.Errorf("Error() = %q, want %q", got, "pkexec not found")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("errors.Is(err, os.ErrNotExist) = false; Unwrap() = %v", err.Unwrap())
	}

	bare := &NotFoundError{Message: "script missing"}
	if bare.Unwrap() != nil {
		t.Errorf("Unwrap() on a causeless NotFoundError = %v, want nil", bare.Unwrap())
	}
	if errors.Is(bare, os.ErrNotExist) {
		t.Error("a causeless NotFoundError matched os.ErrNotExist")
	}
}

func TestErrorUnwrapDistinguishesNotFoundFromExecutionFailure(t *testing.T) {
	execFailure := &Error{Message: "stage failed", Err: os.ErrPermission}

	var notFound *NotFoundError
	if errors.As(execFailure, &notFound) {
		t.Error("an *Error matched *NotFoundError; the two failure kinds must stay distinct")
	}
	if !errors.Is(execFailure, os.ErrPermission) {
		t.Error("errors.Is(*Error, os.ErrPermission) = false; Unwrap() lost the cause")
	}
}
