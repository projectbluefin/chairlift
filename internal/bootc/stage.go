package bootc

import (
	"context"

	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/stageexec"
)

// StageScriptPath is the snow-shipped workaround script that pulls the OS
// image via podman and stages it with `bootc switch --transport
// containers-storage`. See the design spec for why plain `bootc upgrade`
// is not used.
const StageScriptPath = "/usr/libexec/bootc-update-stage"

// EventType classifies a ProgressEvent.
type EventType = stageexec.EventType

const (
	EventMessage  = stageexec.EventMessage
	EventComplete = stageexec.EventComplete
)

// ProgressEvent is a single line of progress from the stage script.
type ProgressEvent = stageexec.ProgressEvent

// StageScriptAvailable reports whether the stage script is installed.
func StageScriptAvailable() bool {
	return stageexec.ScriptAvailable(StageScriptPath)
}

// StageUpdate checks for and stages a system update by running the stage
// script via pkexec. Output lines stream to progressCh as EventMessage
// events; EventComplete is sent on success. progressCh is closed when done.
//
// On a composefs host the registry is asked first, and an already-current
// system completes without running the script: composefs `bootc upgrade`
// fails ("Target image has the same fs-verity digest as the existing
// Booted deployment") instead of exiting 0, and an administrator password
// prompt for nothing to do is no better. A check that cannot answer falls
// through to the script. Elsewhere the script is idempotent itself.
//
// The execution, dry-run, and error contract lives in stageexec.Stage; the
// returned error is a *stageexec.Error (aliased here as *Error) or
// *stageexec.NotFoundError (aliased as *NotFoundError).
func StageUpdate(ctx context.Context, progressCh chan<- ProgressEvent) error {
	return stageUpdate(ctx, progressCh, func(ctx context.Context, progressCh chan<- ProgressEvent) error {
		return stageexec.Stage(ctx, progressCh, pkexec.Command, StageScriptPath)
	})
}

func stageUpdate(ctx context.Context, progressCh chan<- ProgressEvent, stage func(context.Context, chan<- ProgressEvent) error) error {
	if status, err := readComposefsStatus(hostRoot); err == nil {
		if update, err := checkFromRegistry(ctx, status, registryTag); err == nil && !update.Available {
			defer close(progressCh)
			select {
			case progressCh <- ProgressEvent{Type: EventComplete, Message: "Already up to date"}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return stage(ctx, progressCh)
}
