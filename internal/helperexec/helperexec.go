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
// pkexecPath is a parameter, always "pkexec" in production, so tests can
// substitute a fake pkexec stand-in without invoking the real pkexec/polkit
// stack or requiring root.
package helperexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"path"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/journal"
)

// Error represents a privileged helper invocation failure.
type Error struct {
	Message string
}

func (e *Error) Error() string {
	return e.Message
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
		args = append(args, "--dry-run")
		wouldRun := append([]string{pkexecPath, helperPath}, args...)
		journal.Record(action, journalArgs(args), wouldRun, journal.SuppressedDryRun)
		log.Printf("[DRY-RUN] would execute: %s %s %v", pkexecPath, helperPath, args)
		return "", "", nil
	}

	fullArgs := append([]string{helperPath}, args...)
	journal.Record(action, journalArgs(args), append([]string{pkexecPath}, fullArgs...), journal.SuppressedNone)
	cmd := exec.CommandContext(ctx, pkexecPath, fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if stderr.Len() > 0 {
		log.Printf("%s stderr: %s", path.Base(helperPath), stderr.String())
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", stderr.String(), &Error{Message: "command timed out"}
		}
		return "", stderr.String(), classifyFailure(err, helperPath, stderr.String())
	}

	return stdout.String(), stderr.String(), nil
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
		}
	}
	return &Error{Message: err.Error()}
}
