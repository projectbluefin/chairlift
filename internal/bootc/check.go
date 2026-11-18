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

// CheckUpdate reports whether a newer image is published for the booted
// stream. On a composefs host it compares the registry's digest for the
// booted tag with the booted and staged deployments, without root. An ostree
// host answers the same way from the digest its repository records; a host
// whose digest cannot be read falls back to `bootc upgrade --check`, as does
// any other host.
func CheckUpdate(ctx context.Context) (AvailableUpdate, error) {
	status, err := readComposefsStatus(hostRoot)
	if errors.Is(err, errNotComposefs) {
		status, err = readOstreeStatus(ctx, hostRoot)
		if errors.Is(err, errNotOstree) {
			return checkUpdateFrom(ctx, bootcCommand)
		}
	}
	if err != nil {
		return AvailableUpdate{}, err
	}
	// A staged deployment without a digest cannot be compared either: the
	// registry check would only match the booted digest and report an image
	// that is already staged as a new update, so that answer needs root.
	if status.Status.Booted == nil || status.Status.Booted.Digest() == "" ||
		(status.Status.Staged != nil && status.Status.Staged.Digest() == "") {
		return checkUpdateFrom(ctx, bootcCommand)
	}
	return checkFromRegistry(ctx, status, registryTag)
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

// rootRefusalMarker is bootc's message when it refuses a read to a caller that
// is not root. bootc 1.16+ answers `bootc status` and `bootc upgrade --check`
// with it even though both only read.
const rootRefusalMarker = "must be executed as the root user"

// IsRootRefusal reports whether err is bootc refusing to run a read because
// the caller is not root. A host whose only remaining update check is that
// command cannot answer it without authentication, so callers report the
// source as unavailable instead of as a failed check.
func IsRootRefusal(err error) bool {
	var bootcErr *Error
	return errors.As(err, &bootcErr) && strings.Contains(bootcErr.Message, rootRefusalMarker)
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
