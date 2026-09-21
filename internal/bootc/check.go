package bootc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
)

const (
	upgradeCurrentPrefix   = "No changes in:"
	upgradeAvailablePrefix = "Update available for:"
	upgradeVersionPrefix   = " Version:"
	upgradeDigestPrefix    = " Digest:"
)

// AvailableUpdate is the result of a read-only `bootc upgrade --check`.
type AvailableUpdate struct {
	Available bool
	Version   string
	Digest    string
}

// CheckUpdate reports whether `bootc upgrade --check` found a newer image.
func CheckUpdate(ctx context.Context) (AvailableUpdate, error) {
	return checkUpdateFrom(ctx, bootcCommand)
}

func checkUpdateFrom(ctx context.Context, executable string) (AvailableUpdate, error) {
	cmd := exec.CommandContext(ctx, executable, "upgrade", "--check")
	output, err := cmd.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return AvailableUpdate{}, &Error{Message: "bootc update check timed out", Err: context.DeadlineExceeded}
		}
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			return AvailableUpdate{}, &Error{Message: "bootc update check was canceled", Err: context.Canceled}
		}
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return AvailableUpdate{}, &NotFoundError{Message: "bootc not found"}
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return AvailableUpdate{}, &Error{
				Message: fmt.Sprintf("bootc update check failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr)),
				Err:     err,
			}
		}
		return AvailableUpdate{}, &Error{Message: err.Error(), Err: err}
	}
	return parseUpgradeCheck(string(output))
}

func parseUpgradeCheck(output string) (AvailableUpdate, error) {
	var update AvailableUpdate
	headerSeen := false

	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, upgradeCurrentPrefix):
			if headerSeen {
				return AvailableUpdate{}, &Error{Message: fmt.Sprintf("unexpected bootc upgrade --check output: %q", line)}
			}
			headerSeen = true
		case strings.HasPrefix(line, upgradeAvailablePrefix):
			if headerSeen {
				return AvailableUpdate{}, &Error{Message: fmt.Sprintf("unexpected bootc upgrade --check output: %q", line)}
			}
			headerSeen = true
			update.Available = true
		case strings.HasPrefix(line, upgradeVersionPrefix):
			if !update.Available || update.Version != "" {
				return AvailableUpdate{}, &Error{Message: fmt.Sprintf("unexpected bootc upgrade --check output: %q", line)}
			}
			update.Version = strings.TrimSpace(strings.TrimPrefix(line, upgradeVersionPrefix))
			if update.Version == "" {
				return AvailableUpdate{}, &Error{Message: fmt.Sprintf("unexpected bootc upgrade --check output: %q", line)}
			}
		case strings.HasPrefix(line, upgradeDigestPrefix):
			if !update.Available || update.Digest != "" {
				return AvailableUpdate{}, &Error{Message: fmt.Sprintf("unexpected bootc upgrade --check output: %q", line)}
			}
			update.Digest = strings.TrimSpace(strings.TrimPrefix(line, upgradeDigestPrefix))
			if update.Digest == "" {
				return AvailableUpdate{}, &Error{Message: fmt.Sprintf("unexpected bootc upgrade --check output: %q", line)}
			}
		default:
			return AvailableUpdate{}, &Error{Message: fmt.Sprintf("unexpected bootc upgrade --check output: %q", line)}
		}
	}

	if !headerSeen {
		return AvailableUpdate{}, &Error{Message: "unexpected bootc upgrade --check output: empty"}
	}
	if update.Available && (update.Version == "" || update.Digest == "") {
		return AvailableUpdate{}, &Error{Message: "unexpected bootc upgrade --check output: incomplete metadata"}
	}

	return update, nil
}
