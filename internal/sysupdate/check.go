package sysupdate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
)

const checkCommand = "systemd-sysupdate"

// AvailableUpdate is the result of a read-only `systemd-sysupdate check-new`.
type AvailableUpdate struct {
	Available bool
	Version   string
}

// CheckUpdate reports whether `systemd-sysupdate check-new` found a newer version.
func CheckUpdate(ctx context.Context) (AvailableUpdate, error) {
	return checkUpdateFrom(ctx, checkCommand)
}

func checkUpdateFrom(ctx context.Context, executable string) (AvailableUpdate, error) {
	cmd := exec.CommandContext(ctx, executable, "--no-pager", "check-new")
	output, err := cmd.Output()
	if err == nil {
		version := strings.TrimSpace(string(output))
		if version == "" {
			return AvailableUpdate{}, &Error{Message: "unexpected systemd-sysupdate check-new output: empty"}
		}
		if !ValidVersion(version) {
			return AvailableUpdate{}, &Error{
				Message: fmt.Sprintf("unexpected systemd-sysupdate check-new output: %q", version),
			}
		}
		return AvailableUpdate{Available: true, Version: version}, nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return AvailableUpdate{}, &Error{Message: "systemd-sysupdate check timed out", Err: context.DeadlineExceeded}
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return AvailableUpdate{}, &Error{Message: "systemd-sysupdate check was canceled", Err: context.Canceled}
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return AvailableUpdate{}, &NotFoundError{Message: "systemd-sysupdate not found"}
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stdout := strings.TrimSpace(string(output))
		stderr := strings.TrimSpace(string(exitErr.Stderr))
		if exitErr.ExitCode() == 1 && stdout == "" && stderr == "" {
			return AvailableUpdate{}, nil
		}
		return AvailableUpdate{}, &Error{
			Message: fmt.Sprintf("systemd-sysupdate check failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr)),
			Err:     err,
		}
	}

	return AvailableUpdate{}, &Error{Message: err.Error(), Err: err}
}
