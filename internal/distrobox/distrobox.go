// Package distrobox is ChairLift's minimal client for Distrobox, the
// container-based development environment tool. ChairLift does not manage
// individual containers — that already has a purpose-built tool in
// Distrobox itself — it only needs to detect Distrobox and remove every
// container as one step of Powerwash (internal/powerwash).
package distrobox

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

const commandTimeout = 2 * time.Minute

// IsInstalled reports whether the distrobox command is on $PATH.
func IsInstalled() bool {
	_, err := exec.LookPath("distrobox")
	return err == nil
}

// RemoveAll deletes every Distrobox container for the invoking user. It runs
// entirely unprivileged, the same as Podman/Distrobox containers themselves.
func RemoveAll(ctx context.Context) error {
	args := []string{"rm", "--all", "--force"}

	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would execute: distrobox %s", strings.Join(args, " "))
		return nil
	}

	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "distrobox", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &Error{Message: strings.TrimSpace(string(output)), Err: err}
	}
	return nil
}

// Error wraps a failed distrobox invocation, carrying its combined output.
type Error struct {
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}
