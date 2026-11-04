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
	"time"

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
	// Args must record only the caller's real inputs, not the internal
	// --dry-run flag appended for WouldRun/logging.
	wantArgs := map[string]string{"args": "arg1"}
	if !reflect.DeepEqual(entry.Args, wantArgs) {
		t.Errorf("journalled Args = %v, want %v (must not include --dry-run)", entry.Args, wantArgs)
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

// TestRunCancellationBoundedWhenDescendantRetainsOutputPipe pins issue #82:
// Run cancels only the direct pkexec process, but a privileged descendant
// that inherited stdout/stderr can live on after that kill and hold the pipe
// open. Without a finite WaitDelay the output-copy goroutines would block
// forever waiting for EOF and the GUI action would stay stuck. The test
// spawns such a descendant and asserts Run returns promptly (bounded by
// WaitDelay) with a canceled classification instead of hanging. The fake
// pkexec runs unprivileged and is killable, which models the prompt stage;
// after real authentication the helper runs as root and Run waits for it
// (see the package's cancellation contract).
func TestRunCancellationBoundedWhenDescendantRetainsOutputPipe(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	marker := filepath.Join(t.TempDir(), "helper-started")
	helper := filepath.Join(t.TempDir(), "fake-helper")
	body := "#!/bin/sh\n" +
		"echo started\n" +
		"( exec sleep 100 ) &\n" +
		"echo ready >\"" + marker + "\"\n" +
		"wait\n"
	if err := os.WriteFile(helper, []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake helper: %v", err)
	}

	done := make(chan error, 1)
	go func() { _, _, e := Run(ctx, "/bin/sh", helper, "do-thing"); done <- e }()

	// Wait until the helper has spawned the descendant that owns the pipe.
	waited := 0
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		waited++
		if waited > 500 {
			t.Fatal("fake helper never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	// Run must return within WaitDelay plus a small margin rather than wait
	// for EOF on a pipe only the orphaned descendant still owns.
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Errorf("Run error = %v, want a canceled classification", err)
		}
		// The sentinel must stay reachable through errors.Is, the
		// convention internal/stageexec's errors follow.
		if !errors.Is(err, context.Canceled) {
			t.Errorf("errors.Is(%v, context.Canceled) = false, want true", err)
		}
		// Cancellation kills only the direct child, so the message must
		// not claim the privileged work itself stopped.
		if !strings.Contains(err.Error(), "may still be running") {
			t.Errorf("canceled message = %q, want it to warn the privileged work may continue", err.Error())
		}
	case <-time.After(WaitDelay + 2*time.Second):
		t.Fatal("Run waited for a descendant retaining the output pipe")
	}
}

// shortenWaitDelay narrows Run's pipe-drain bound for tests that deliberately
// strand a descendant on the output pipes, so they cost milliseconds instead
// of the production WaitDelay.
func shortenWaitDelay(t *testing.T, d time.Duration) {
	t.Helper()
	previous := waitDelay
	waitDelay = d
	t.Cleanup(func() { waitDelay = previous })
}

// writePipeHoldingHelper writes a helper that spawns a descendant inheriting
// stdout/stderr, touches marker, and exits with exitCode while that
// descendant keeps the pipes open — the shape that makes cmd.Run report
// exec.ErrWaitDelay even though the helper itself is done.
func writePipeHoldingHelper(t *testing.T, marker string, exitCode int) string {
	t.Helper()
	helper := filepath.Join(t.TempDir(), "fake-helper")
	body := "#!/bin/sh\n" +
		"echo helper-output\n" +
		"( exec sleep 100 ) &\n" +
		"echo ready >\"" + marker + "\"\n" +
		fmt.Sprintf("exit %d\n", exitCode)
	if err := os.WriteFile(helper, []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake helper: %v", err)
	}
	return helper
}

// TestRunReportsSuccessWhenDescendantHoldsPipesAfterCleanExit pins the
// consequence of bounding the wait: WaitDelay applies to every run, not only
// canceled ones, so a helper that succeeded while a descendant kept the
// inherited pipes open makes cmd.Run return exec.ErrWaitDelay. The helper's
// own exit status is authoritative there — reporting failure would tell the
// GUI a completed privileged action failed and invite a retry of work that
// already happened.
func TestRunReportsSuccessWhenDescendantHoldsPipesAfterCleanExit(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	shortenWaitDelay(t, 200*time.Millisecond)

	marker := filepath.Join(t.TempDir(), "helper-started")
	helper := writePipeHoldingHelper(t, marker, 0)

	stdout, _, err := Run(context.Background(), "/bin/sh", helper, "do-thing")
	if err != nil {
		t.Fatalf("Run = %v, want success for a helper that exited 0", err)
	}
	if !strings.Contains(stdout, "helper-output") {
		t.Errorf("stdout = %q, want the helper's output preserved", stdout)
	}
}

// TestRunReportsHelperExitCodeWhenDescendantHoldsPipes pins the other half:
// when the helper fails while a descendant holds the pipes, the failure must
// keep its exit code and stderr rather than degrade to the generic
// pipe-drain message.
func TestRunReportsHelperExitCodeWhenDescendantHoldsPipes(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	shortenWaitDelay(t, 200*time.Millisecond)

	marker := filepath.Join(t.TempDir(), "helper-started")
	helper := writePipeHoldingHelper(t, marker, 3)

	_, _, err := Run(context.Background(), "/bin/sh", helper, "do-thing")
	if err == nil || !strings.Contains(err.Error(), "exit 3") {
		t.Fatalf("Run error = %v, want the helper's exit code reported", err)
	}
}

// TestRunCancelDoesNotMaskHelperFailure pins that a cancel racing a genuine
// helper failure does not erase that failure's classification. The helper
// exits 3 but a descendant holds the pipes, so Run is still inside its
// bounded drain when ctx is canceled: ctx.Err() is non-nil, yet the helper
// terminated with its own status and that status is what the caller needs.
func TestRunCancelDoesNotMaskHelperFailure(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	shortenWaitDelay(t, 3*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	marker := filepath.Join(t.TempDir(), "helper-started")
	helper := writePipeHoldingHelper(t, marker, 3)

	done := make(chan error, 1)
	go func() { _, _, e := Run(ctx, "/bin/sh", helper, "do-thing"); done <- e }()

	waitForMarker(t, marker)
	// The marker is written immediately before exit; give the helper time to
	// actually exit so the cancel lands during the pipe drain, not before.
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "exit 3") {
			t.Fatalf("Run error = %v, want the helper's exit 3 preserved despite the cancel", err)
		}
		if errors.Is(err, context.Canceled) {
			t.Errorf("Run error = %v, want it not classified as cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run never returned")
	}
}

// TestRunCancelDoesNotMaskCleanExit pins the success side of the same race: a
// helper that already exited 0 did its privileged work, so a cancel landing
// while Run is still draining pipes a descendant holds must not turn that
// completed action into a failure the user is invited to retry.
func TestRunCancelDoesNotMaskCleanExit(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	shortenWaitDelay(t, 3*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	marker := filepath.Join(t.TempDir(), "helper-started")
	helper := writePipeHoldingHelper(t, marker, 0)

	type result struct {
		stdout string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		out, _, e := Run(ctx, "/bin/sh", helper, "do-thing")
		done <- result{stdout: out, err: e}
	}()

	waitForMarker(t, marker)
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Run = %v, want success for a helper that already exited 0", got.err)
		}
		if !strings.Contains(got.stdout, "helper-output") {
			t.Errorf("stdout = %q, want the helper's output preserved", got.stdout)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run never returned")
	}
}

// TestRunClassifiesDeadlineWithUnwrappableSentinel pins the timeout half of
// the context taxonomy, including the errors.Is reachability the cancel case
// gained.
func TestRunClassifiesDeadlineWithUnwrappableSentinel(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	t.Cleanup(cancel)
	<-ctx.Done()

	_, _, err := Run(ctx, "/bin/sh", "/usr/bin/chairlift-example-helper", "do-thing")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Run error = %v, want a timeout classification", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(%v, context.DeadlineExceeded) = false, want true", err)
	}
}

// waitForMarker blocks until the fake helper reports it reached the point the
// test cares about.
func waitForMarker(t *testing.T, marker string) {
	t.Helper()
	for waited := 0; waited <= 500; waited++ {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fake helper never started")
}

func TestRunCapturesDerivedCommandAndJournalsOutcome(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, journalPath)
	journal.Reset()
	t.Cleanup(journal.Reset)

	fakeHelper := filepath.Join(t.TempDir(), "fake-helper")
	body := "#!/bin/sh\n" +
		"echo 'chairlift-helper: exec [\"usermod\", \"-aG\", \"docker\", \"testuser\"]'\n" +
		"echo 'chairlift-helper: exec [\"systemctl\", \"enable\", \"--now\", \"docker.socket\", \"docker.service\"]'\n" +
		"echo 'normal output'\n" +
		"exit 0\n"
	if err := os.WriteFile(fakeHelper, []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake helper: %v", err)
	}

	stdout, stderr, err := Run(context.Background(), "/bin/sh", fakeHelper, "docker-enable")
	if err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if strings.Contains(stdout, "chairlift-helper: exec") {
		t.Errorf("stdout = %q, want exec marker stripped", stdout)
	}
	if !strings.Contains(stdout, "normal output") {
		t.Errorf("stdout = %q, want normal output preserved", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal entries = %d, want 2 (dispatch + outcome)", len(entries))
	}
	if entries[0].Action != "docker-enable" || entries[0].Suppressed != journal.SuppressedNone {
		t.Errorf("entry[0] = %+v, want action docker-enable suppressed no", entries[0])
	}
	if entries[1].Action != "docker-enable" || entries[1].Outcome != journal.OutcomeSucceeded {
		t.Errorf("entry[1] = %+v, want action docker-enable outcome succeeded", entries[1])
	}
	wantExecuted := [][]string{
		{"usermod", "-aG", "docker", "testuser"},
		{"systemctl", "enable", "--now", "docker.socket", "docker.service"},
	}
	if !reflect.DeepEqual(entries[1].Executed, wantExecuted) {
		t.Errorf("entry[1].Executed = %v, want %v", entries[1].Executed, wantExecuted)
	}
}

func TestRunJournalsOutcomeRefusedOnExit126(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, journalPath)
	journal.Reset()
	t.Cleanup(journal.Reset)

	fakeHelper := filepath.Join(t.TempDir(), "fake-helper")
	body := "#!/bin/sh\nexit 126\n"
	if err := os.WriteFile(fakeHelper, []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake helper: %v", err)
	}

	_, _, err := Run(context.Background(), "/bin/sh", fakeHelper, "restart")
	if err == nil {
		t.Fatal("Run = nil, want error on exit 126")
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal entries = %d, want 2", len(entries))
	}
	if entries[1].Outcome != journal.OutcomeRefused {
		t.Errorf("entry[1].Outcome = %q, want refused", entries[1].Outcome)
	}
	if entries[1].ExitCode == nil || *entries[1].ExitCode != 126 {
		t.Errorf("entry[1].ExitCode = %v, want 126", entries[1].ExitCode)
	}
}

func TestParseAndCleanExecKeepsLinesAfterAnOverlongLine(t *testing.T) {
	long := strings.Repeat("x", 100*1024)
	output := long + "\nchairlift-helper: exec [\"bootc\",\"status\"]\nafter\n"
	clean, executed := parseAndCleanExec(output, "/usr/bin/chairlift-helper")
	if clean != long+"\nafter\n" {
		t.Fatalf("clean output lost lines after an overlong line (len %d)", len(clean))
	}
	if len(executed) != 1 || executed[0][0] != "bootc" {
		t.Fatalf("executed = %v, want one bootc command", executed)
	}
}

func TestRunJournalsUnexecutableHelperAsFailedNotRefused(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, journalPath)
	journal.Reset()
	t.Cleanup(journal.Reset)

	fakeHelper := filepath.Join(t.TempDir(), "fake-helper")
	body := "#!/bin/sh\necho 'Error accessing /usr/bin/chairlift-helper: No such file or directory' >&2\nexit 127\n"
	if err := os.WriteFile(fakeHelper, []byte(body), 0o755); err != nil {
		t.Fatalf("writing fake helper: %v", err)
	}

	if _, _, err := Run(context.Background(), "/bin/sh", fakeHelper, "restart"); err == nil {
		t.Fatal("Run = nil, want error on exit 127")
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 || entries[1].Outcome != journal.OutcomeFailed {
		t.Fatalf("entries = %+v, want a second entry with outcome failed", entries)
	}
}
