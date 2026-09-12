package helperexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/journal"
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

func TestRunClassifiesNonExecutablePkexecAsError(t *testing.T) {
	dryrun.Set(false)

	// A non-executable file: os/exec reports EACCES, which is not
	// exec.ErrNotFound. The classification must fall through the ladder to
	// *Error carrying the OS message — a present-but-unexecutable pkexec is
	// not "pkexec not found".
	notExecutable := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("writing non-executable file: %v", err)
	}

	_, _, err := Run(context.Background(), notExecutable, "/usr/bin/chairlift-example-helper", "do-thing")
	var notFound *NotFoundError
	if errors.As(err, &notFound) {
		t.Fatalf("Run error = %T (%v), want EACCES not to classify as *NotFoundError", err, err)
	}
	var helperErr *Error
	if !errors.As(err, &helperErr) {
		t.Fatalf("Run error = %T (%v), want *Error", err, err)
	}
}

// TestClassifyFailureSeesThroughWrappedErrors pins the guarantee that no
// input to Run can provide: os/exec returns *exec.Error and *exec.ExitError
// unwrapped, so the errors.As ladder and the pre-extraction comma-ok ladder
// internal/updex carried agree on every error Run can observe. The ladders
// diverge only on a wrapped error — and the comma-ok form silently degrades
// a wrapped exec.ErrNotFound to a generic *Error. That drift is the one this
// package exists to make impossible, so it is pinned here at the seam where
// it can actually be exercised.
func TestClassifyFailureSeesThroughWrappedErrors(t *testing.T) {
	wrappedNotFound := fmt.Errorf("spawning pkexec: %w", &exec.Error{Name: "pkexec", Err: exec.ErrNotFound})
	err := classifyFailure(wrappedNotFound, "/usr/bin/chairlift-example-helper", "")
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("classifyFailure(wrapped exec.ErrNotFound) = %T (%v), want *NotFoundError", err, err)
	}
	if !strings.Contains(err.Error(), "chairlift-example-helper") {
		t.Fatalf("NotFoundError message = %q, want it to name the helper basename", err.Error())
	}
}

// TestRunJournalsDryRunFlagAsSuppressed pins the safety-critical half of
// dry-run in the package that owns it. The short-circuit stops pkexec
// spawning (TestRunDryRunNeverInvokesPkexec); the appended --dry-run is what
// tells the helper to preview rather than act on every invocation that does
// reach it, and the journal is the machine-readable record of both.
func TestRunJournalsDryRunFlagAsSuppressed(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, journalPath)
	journal.Reset()
	t.Cleanup(journal.Reset)

	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	pkexecPath := "pkexec-should-never-run"
	helperPath := "/usr/bin/chairlift-example-helper"
	if _, _, err := Run(context.Background(), pkexecPath, helperPath, "do-thing", "arg1"); err != nil {
		t.Fatalf("Run dry-run error = %v, want nil", err)
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Action != "do-thing" {
		t.Errorf("journalled action = %q, want %q", entry.Action, "do-thing")
	}
	if entry.Suppressed != journal.SuppressedDryRun {
		t.Errorf("journalled suppressed = %q, want %q", entry.Suppressed, journal.SuppressedDryRun)
	}
	wantArgv := []string{pkexecPath, helperPath, "do-thing", "arg1", "--dry-run"}
	if !reflect.DeepEqual(entry.WouldRun, wantArgv) {
		t.Errorf("journalled WouldRun = %v, want %v (--dry-run appended last)", entry.WouldRun, wantArgv)
	}
}

func readJournal(t *testing.T, path string) []journal.Entry {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading journal %s: %v", path, err)
	}

	var entries []journal.Entry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry journal.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decoding journal line %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}
