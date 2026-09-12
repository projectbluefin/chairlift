package sysupdate

import (
	"context"

	"github.com/projectbluefin/chairlift/internal/stageexec"
)

// StageScriptPath is the snosi-shipped native A/B stager. It checks the
// image's sysupdate target for a newer version and downloads it into the
// inactive root/verity slots via systemd-sysupdate; it never reboots, and
// it is idempotent (exits 0 without staging when already current or when
// the candidate is already in the inactive slot).
const StageScriptPath = "/usr/libexec/snosi-sysupdate-stage"

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
// The execution, dry-run, and error contract lives in stageexec.Stage; the
// returned error is a *stageexec.Error (aliased here as *Error) or
// *stageexec.NotFoundError (aliased as *NotFoundError).
func StageUpdate(ctx context.Context, progressCh chan<- ProgressEvent) error {
	return stageexec.Stage(ctx, progressCh, pkexecCommand, StageScriptPath)
}
