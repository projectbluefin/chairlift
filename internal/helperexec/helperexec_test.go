package helperexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// writeFakePkexec writes an executable shell script standing in for pkexec:
// it records its own argv (one element per line) to capturedArgsFile and
// exits 0. It never execs the real pkexec or requires root.
func writeFakePkexec(t *testing.T, capturedArgsFile string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-pkexec")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + capturedArgsFile + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake pkexec: %v", err)
	}
	return path
}

func TestRunInvokesPkexecWithFixedHelperPathAndArgs(t *testing.T) {
	dryrun.Set(false)

	capturedArgsFile := filepath.Join(t.TempDir(), "captured-args")
	fakePkexec := writeFakePkexec(t, capturedArgsFile)

	helperPath := "/usr/bin/chairlift-example-helper"
	if _, _, err := Run(context.Background(), fakePkexec, helperPath, "do-thing", "arg1"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(capturedArgsFile)
	if err != nil {
		t.Fatalf("reading captured pkexec argv: %v", err)
	}
	got := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	want := []string{helperPath, "do-thing", "arg1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pkexec argv = %v, want %v", got, want)
	}
}

func TestRunDryRunNeverInvokesPkexec(t *testing.T) {
	dryrun.Set(true)
	defer dryrun.Set(false)

	// A path that does not exist: if Run failed to short-circuit and tried
	// to actually run it, cmd.Run() would return an error and this test
	// would fail loudly instead of silently passing.
	nonexistentPkexec := filepath.Join(t.TempDir(), "pkexec-should-never-run")

	stdout, stderr, err := Run(context.Background(), nonexistentPkexec, "/usr/bin/chairlift-example-helper", "do-thing")
	if err != nil {
		t.Fatalf("Run dry-run returned error, want short-circuit with nil error: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("Run dry-run returned stdout=%q stderr=%q, want both empty", stdout, stderr)
	}
}

func TestRunClassifiesMissingPkexecAsNotFound(t *testing.T) {
	dryrun.Set(false)

	// A bare name that $PATH lookup cannot resolve: exec reports
	// exec.ErrNotFound, the shape Run classifies as *NotFoundError.
	_, _, err := Run(context.Background(), "chairlift-pkexec-that-does-not-exist", "/usr/bin/chairlift-example-helper", "do-thing")
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Run error = %T (%v), want *NotFoundError", err, err)
	}
	if !strings.Contains(err.Error(), "chairlift-example-helper") {
		t.Fatalf("NotFoundError message = %q, want it to name the helper basename", err.Error())
	}
}

func TestRunClassifiesWrappedExecErrors(t *testing.T) {
	dryrun.Set(false)

	// A non-executable file: exec returns a wrapped *exec.Error (EACCES,
	// not ErrNotFound). The errors.As classification must see through the
	// wrap — the pre-extraction comma-ok assertion in internal/updex did
	// not, which is the drift this package eliminates.
	notExecutable := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("writing non-executable file: %v", err)
	}

	_, _, err := Run(context.Background(), notExecutable, "/usr/bin/chairlift-example-helper", "do-thing")
	var helperErr *Error
	if !errors.As(err, &helperErr) {
		t.Fatalf("Run error = %T (%v), want *Error", err, err)
	}
}
