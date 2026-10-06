// Package helperexec runs ChairLift's fixed-path privileged helper binaries
// via pkexec. It owns the invocation contract shared by internal/ublue and
// internal/updex — previously copied verbatim into both packages, where it
// had already drifted (one classified wrapped exec errors with errors.As,
// the other with comma-ok type assertions that miss wrapped errors).
//
// The contract: the helper path is a fixed absolute path chosen by the
// caller (it must match the polkit policy's exec.path annotation and is
// never overridable here); every invocation — dry-run or live — is recorded
// in internal/journal; dry-run appends --dry-run and never spawns pkexec;
// failure is classified as *NotFoundError (pkexec or helper absent) or
// *Error (timeout, non-zero exit, or any other start/wait failure, with the
// underlying message preserved).
//
// internal/ublue and internal/updex alias both error types, so the taxonomy
// is deliberately shared across helpers: a failure's type never identifies
// which helper failed. A caller that needs that attribution must carry the
// invocation context itself rather than discriminate on the error type.
//
// pkexecPath is a parameter so tests can substitute a fake pkexec stand-in
// without invoking the real pkexec/polkit stack or requiring root. In
// production every caller passes internal/pkexec.Command, which owns that
// name for the whole application.
//
// # Cancellation contract
//
// Canceling ctx (or letting its deadline pass) kills only the direct pkexec
// child. The privileged helper and anything it spawned run as root, so an
// unprivileged ChairLift cannot signal them as a process group the way
// internal/maintenanceexec does for unprivileged maintenance scripts. Run
// therefore bounds its wait with WaitDelay instead of stopping the work:
// it returns promptly, but a root descendant may still be mutating the
// system afterwards, which is why the cancellation message does not claim
// the action stopped. Surfaces rendering it must not imply otherwise.
//
// Because the wait is bounded, cmd.Run can report exec.ErrWaitDelay even
// though the helper itself finished: a descendant that inherited stdout and
// stderr kept the pipes open past the delay. Run classifies from the helper's
// own exit status in that case, so a completed privileged action is reported
// as success (with possibly truncated output) rather than as a failure the
// user is invited to retry. For the same reason a cancel or deadline racing a
// genuine helper failure does not mask that failure's exit code and stderr:
// the context classification applies only when the helper was killed rather
// than exiting on its own.
package helperexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/journal"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
)

// WaitDelay bounds how long Run waits for the pkexec command's output pipes
// to drain after the process is gone. pkexec hands stdout/stderr on to its
// privileged helper, which may itself spawn descendants that inherit them; a
// straggler could otherwise hold cmd.Run open forever waiting for EOF on a
// pipe only a now-orphaned descendant still owns, exactly as maintenanceexec
// guards against for configured maintenance scripts.
//
// Unlike maintenanceexec, Run cannot kill the whole process group: the helper
// runs as root and an unprivileged ChairLift cannot signal it. WaitDelay
// therefore bounds the wait, it does not stop the work — see the package
// doc's cancellation contract.
const WaitDelay = 5 * time.Second

// waitDelay is the delay Run actually applies. It is a seam so tests can
// shorten the pipe-holding-descendant cases instead of paying WaitDelay for
// each; production never reassigns it.
var waitDelay = WaitDelay

// canceledMessage deliberately does not claim the privileged work stopped:
// Run kills only the direct pkexec child, so a root descendant that inherited
// the work can keep mutating the system after Run returns.
const canceledMessage = "command canceled (privileged work already started may still be running)"

// Error represents a privileged helper invocation failure.
type Error struct {
	Message string
	// Err is the underlying process or context error, so callers can match
	// context.Canceled / context.DeadlineExceeded with errors.Is the same
	// way internal/stageexec's errors allow.
	Err error
}

func (e *Error) Error() string {
	return e.Message
}

// Unwrap exposes the underlying process or context error.
func (e *Error) Unwrap() error {
	return e.Err
}

// NotFoundError is returned when pkexec or the privileged helper is absent.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

// journalArgs turns a privileged helper's argv (minus the leading command
// word, which becomes journal.Entry.Action) into the journal's args map.
func journalArgs(args []string) map[string]string {
	if len(args) <= 1 {
		return nil
	}
	return map[string]string{"args": strings.Join(args[1:], " ")}
}

// Run executes helperPath via pkexecPath with args, honoring the global
// dry-run switch and journaling every invocation. It returns the helper's
// stdout and stderr alongside any classified error.
func Run(ctx context.Context, pkexecPath, helperPath string, args ...string) (string, string, error) {
	action := ""
	if len(args) > 0 {
		action = args[0]
	}

	if dryrun.Enabled() {
		// Journal Args must reflect the caller's actual inputs, so build the
		// --dry-run-appended argv in a separate slice rather than mutating
		// args (which journalArgs(args) below still reads unmodified).
		dryRunArgs := append(append([]string{}, args...), "--dry-run")
		wouldRun := append([]string{pkexecPath, helperPath}, dryRunArgs...)
		journal.Record(action, journalArgs(args), wouldRun, journal.SuppressedDryRun)
		log.Printf("[DRY-RUN] would execute: %s %s %v", pkexecPath, helperPath, dryRunArgs)
		return "", "", nil
	}

	fullArgs := append([]string{helperPath}, args...)
	journal.Record(action, journalArgs(args), append([]string{pkexecPath}, fullArgs...), journal.SuppressedNone)
	cmd := exec.CommandContext(ctx, pkexecPath, fullArgs...)
	// A finite WaitDelay guarantees cancellation returns even when a
	// privileged descendant of the helper retains the inherited output pipes.
	cmd.WaitDelay = waitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	cleanStdout, execOut := parseAndCleanExec(stdout.String(), helperPath)
	cleanStderr, execErr := parseAndCleanExec(stderr.String(), helperPath)
	var executed [][]string
	if len(execOut) > 0 {
		executed = append(executed, execOut...)
	}
	if len(execErr) > 0 {
		executed = append(executed, execErr...)
	}

	if len(cleanStderr) > 0 {
		log.Printf("%s stderr: %s", path.Base(helperPath), cleanStderr)
	}

	if err == nil {
		journal.RecordOutcome(action, journal.OutcomeSucceeded, nil, executed)
		return cleanStdout, cleanStderr, nil
	}

	// The helper's own exit status is authoritative whenever it exited on its
	// own, whatever error cmd.Run surfaced. Two races make that distinction
	// matter: a descendant that inherited the output pipes can keep them open
	// past WaitDelay (cmd.Run then reports exec.ErrWaitDelay for a helper that
	// succeeded), and a cancel or deadline can land while the helper is
	// already finishing (cmd.Run then reports the context error). Reporting
	// either as a failure would tell the GUI a completed privileged action
	// failed and invite a retry of work that already happened; reporting a
	// real non-zero exit as "canceled" would drop the exit code and stderr the
	// caller needs.
	if exitedOnOwn(cmd.ProcessState) {
		if cmd.ProcessState.Success() {
			if errors.Is(err, exec.ErrWaitDelay) {
				// The pipes were force-closed at WaitDelay, so the captured
				// output may be truncated mid-stream.
				log.Printf("%s exited 0 but a descendant still held its output pipes; captured output may be truncated",
					path.Base(helperPath))
			}
			journal.RecordOutcome(action, journal.OutcomeSucceeded, nil, executed)
			return cleanStdout, cleanStderr, nil
		}
		exitCode := cmd.ProcessState.ExitCode()
		// pkexec exits 127 both for a refused authorization and for a helper
		// it cannot execute ("Error accessing <path>"); only the former is a
		// refusal.
		if exitCode == pkexec.DismissedExitCode || (exitCode == 127 && !strings.Contains(cleanStderr, "Error accessing")) {
			journal.RecordOutcome(action, journal.OutcomeRefused, &exitCode, executed)
		} else {
			journal.RecordOutcome(action, journal.OutcomeFailed, &exitCode, executed)
		}
		return "", cleanStderr, exitFailure(exitCode, cleanStderr, err)
	}

	// The helper was killed rather than exiting, so a non-nil ctx error is the
	// reason it died and names the outcome better than "signal: killed".
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			journal.RecordOutcome(action, journal.OutcomeTimedOut, nil, executed)
		} else {
			journal.RecordOutcome(action, journal.OutcomeCancelled, nil, executed)
		}
		return "", cleanStderr, contextError(ctxErr)
	}

	journal.RecordOutcome(action, journal.OutcomeFailed, nil, executed)
	return "", cleanStderr, classifyFailure(err, helperPath, cleanStderr)
}

// parseAndCleanExec extracts any machine-readable command emitted by the helper
// and strips those marker lines from the human-readable output.
func parseAndCleanExec(output, helperPath string) (string, [][]string) {
	if !strings.Contains(output, ": exec ") {
		return output, nil
	}
	prefix := path.Base(helperPath) + ": exec "
	var executed [][]string
	var cleanLines []string
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if strings.HasPrefix(line, prefix) || strings.HasPrefix(line, ubluehelper.HelperExecPrefix) {
			idx := strings.Index(line, ": exec ")
			if idx != -1 {
				jsonPart := line[idx+len(": exec "):]
				var cmd []string
				if err := json.Unmarshal([]byte(jsonPart), &cmd); err == nil && len(cmd) > 0 {
					executed = append(executed, cmd)
				}
			}
			continue
		}
		cleanLines = append(cleanLines, line)
	}
	cleanOutput := strings.Join(cleanLines, "\n")
	if len(cleanLines) > 0 && strings.HasSuffix(output, "\n") {
		cleanOutput += "\n"
	} else if len(cleanLines) == 0 && !strings.HasSuffix(output, "\n") {
		cleanOutput = ""
	}
	return cleanOutput, executed
}

// exitFailure reports a helper that ran and exited non-zero, keeping the exit
// code and stderr even when cmd.Run surfaced something other than an
// *exec.ExitError (a bounded-wait or cancellation artifact racing the exit).
func exitFailure(exitCode int, stderr string, err error) error {
	return &Error{
		Message: fmt.Sprintf("command failed (exit %d): %s", exitCode, stderr),
		Err:     err,
	}
}

// exitedOnOwn reports whether the helper process terminated with an exit
// status of its own rather than being killed — by Run's cancellation, by a
// deadline, or by any other signal.
func exitedOnOwn(state *os.ProcessState) bool {
	return state != nil && state.Exited()
}

// contextError maps a context error onto the shared taxonomy, keeping the
// sentinel reachable through errors.Is.
func contextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Message: "command timed out", Err: context.DeadlineExceeded}
	}
	return &Error{Message: canceledMessage, Err: context.Canceled}
}

// classifyFailure maps a failed invocation's error onto the shared taxonomy:
// *NotFoundError when the process never started because pkexec (or the
// helper) is absent, *Error otherwise. It uses errors.As rather than the
// pre-extraction comma-ok assertions internal/updex carried, so a wrapped
// *exec.Error or *exec.ExitError still classifies instead of falling through
// to the generic *Error. os/exec returns these types unwrapped today, so the
// two forms agree on every error Run can currently observe; the
// wrap-transparency is the drift this package exists to prevent, and
// TestClassifyFailureSeesThroughWrappedErrors pins it.
func classifyFailure(err error, helperPath, stderr string) error {
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return &NotFoundError{Message: fmt.Sprintf("pkexec or %s not found", path.Base(helperPath))}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &Error{
			Message: fmt.Sprintf("command failed (exit %d): %s", exitErr.ExitCode(), stderr),
			Err:     err,
		}
	}
	return &Error{Message: err.Error(), Err: err}
}
