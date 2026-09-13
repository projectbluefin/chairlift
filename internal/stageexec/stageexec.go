// Package stageexec runs the fixed privileged staging commands used by OS
// update providers. It owns their widget-free progress and process contract,
// including the dry-run gate, the script-availability probe, and the
// internal/journal record every privileged escalation owes, so each provider
// package only names its script path.
//
// The journal record is not incidental. internal/journal documents its
// contract in universal terms — "records every privileged action ChairLift
// takes or would take" — but for a long time the only package honoring it was
// internal/helperexec, so the two staging actions
// (io.projectbluefin.chairlift.bootc.stage and
// io.projectbluefin.chairlift.sysupdate.stage) escalated with no audit entry.
// Staging writes the inactive slot or a bootc switch; it is the least
// undoable thing ChairLift does, which is exactly where the audit trail
// mattered most. Stage now records both branches, and
// internal/installcheck's journal-contract gate keeps a future privileged
// executor from reopening the hole.
package stageexec

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/journal"
)

// EventType classifies a ProgressEvent.
type EventType string

const (
	EventMessage  EventType = "message"
	EventComplete EventType = "complete"
)

// ProgressEvent is one line of staging progress or the final completion event.
type ProgressEvent struct {
	Type    EventType
	Message string
}

// Error describes a staging execution failure.
type Error struct {
	Message string
	Err     error
}

func (e *Error) Error() string {
	return e.Message
}

// Unwrap exposes the underlying process or context error.
func (e *Error) Unwrap() error {
	return e.Err
}

// NotFoundError reports that the requested executable does not exist.
type NotFoundError struct {
	Message string
	Err     error
}

func (e *NotFoundError) Error() string {
	return e.Message
}

// Unwrap exposes the executable lookup error.
func (e *NotFoundError) Unwrap() error {
	return e.Err
}

// ScriptAvailable reports whether the stage script is installed.
func ScriptAvailable(scriptPath string) bool {
	_, err := os.Stat(scriptPath)
	return err == nil
}

// Stage is the full stage-update contract shared by every OS update
// provider: it journals the escalation, then, when dry-run mode is active,
// logs and emits the preview events without invoking pkexec; otherwise it
// runs `pkexec scriptPath`, streaming progress. It always closes progressCh.
//
// The journal entry is written before dispatch so the argv ChairLift
// assembled is recorded whether or not the process is allowed to start,
// matching helperexec.Run. Action is the script's base name — the staging
// operation's stable identity, independent of the /usr/libexec prefix — and
// WouldRun is the argv a real run executes, so a dry-run entry and a live
// entry differ only in Suppressed.
func Stage(ctx context.Context, progressCh chan<- ProgressEvent, pkexec, scriptPath string) error {
	// The stage scripts take no arguments, so Entry.Args stays nil and the
	// whole invocation is carried by WouldRun.
	action, wouldRun := path.Base(scriptPath), []string{pkexec, scriptPath}

	if dryrun.Enabled() {
		journal.Record(action, nil, wouldRun, journal.SuppressedDryRun)
		log.Printf("[DRY-RUN] would execute: %s %s", pkexec, scriptPath)
		return DryRun(ctx, progressCh, scriptPath)
	}
	journal.Record(action, nil, wouldRun, journal.SuppressedNone)
	return Run(ctx, progressCh, pkexec, scriptPath)
}

// DryRun emits the standard preview and completion events without starting a
// process. It always closes progressCh.
func DryRun(ctx context.Context, progressCh chan<- ProgressEvent, scriptPath string) error {
	defer close(progressCh)

	if err := send(ctx, progressCh, ProgressEvent{
		Type:    EventMessage,
		Message: "[DRY-RUN] would run " + scriptPath,
	}); err != nil {
		return contextError(err)
	}
	if err := send(ctx, progressCh, ProgressEvent{
		Type:    EventComplete,
		Message: "Dry run complete",
	}); err != nil {
		return contextError(err)
	}
	return nil
}

// Run executes name with args, streams merged stdout and stderr as trimmed,
// non-empty EventMessage values, emits one EventComplete after a successful
// exit, and always closes progressCh. Cancellation kills and reaps only the
// direct child: current callers execute through pkexec, whose privileged child
// cannot be signaled as an unprivileged process group.
func Run(ctx context.Context, progressCh chan<- ProgressEvent, name string, args ...string) error {
	defer close(progressCh)

	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &Error{Message: fmt.Sprintf("failed to create stdout pipe: %v", err), Err: err}
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return contextError(ctxErr)
		}
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return &NotFoundError{Message: name + " not found", Err: err}
		}
		return &Error{Message: fmt.Sprintf("failed to start %s: %v", name, err), Err: err}
	}

	lines := scanLines(ctx, stdout)
	var lastLine string
	for {
		select {
		case <-ctx.Done():
			stop(cmd, stdout)
			return contextError(ctx.Err())
		case result, ok := <-lines:
			if !ok {
				goto scanned
			}
			if result.err != nil {
				stop(cmd, stdout)
				if ctxErr := ctx.Err(); ctxErr != nil {
					return contextError(ctxErr)
				}
				return &Error{
					Message: fmt.Sprintf("failed to read staging output: %v", result.err),
					Err:     result.err,
				}
			}
			lastLine = result.line
			if err := send(ctx, progressCh, ProgressEvent{Type: EventMessage, Message: result.line}); err != nil {
				stop(cmd, stdout)
				return contextError(err)
			}
		}
	}

scanned:
	if ctxErr := ctx.Err(); ctxErr != nil {
		stop(cmd, stdout)
		return contextError(ctxErr)
	}
	if err := cmd.Wait(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return contextError(ctxErr)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := fmt.Sprintf("update staging failed (exit %d)", exitErr.ExitCode())
			if lastLine != "" {
				msg += ": " + lastLine
			}
			return &Error{Message: msg, Err: err}
		}
		return &Error{Message: err.Error(), Err: err}
	}

	if err := send(ctx, progressCh, ProgressEvent{
		Type:    EventComplete,
		Message: "Staging complete",
	}); err != nil {
		return contextError(err)
	}
	return nil
}

type scanResult struct {
	line string
	err  error
}

func scanLines(ctx context.Context, output io.Reader) <-chan scanResult {
	results := make(chan scanResult)
	go func() {
		defer close(results)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			select {
			case results <- scanResult{line: line}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case results <- scanResult{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return results
}

func stop(cmd *exec.Cmd, output io.Closer) {
	_ = cmd.Process.Kill()
	_ = output.Close()
	_ = cmd.Wait()
}

func send(ctx context.Context, progressCh chan<- ProgressEvent, event ProgressEvent) error {
	select {
	case progressCh <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func contextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Message: "Update staging timed out", Err: context.DeadlineExceeded}
	}
	return &Error{Message: "Update staging was canceled", Err: context.Canceled}
}
