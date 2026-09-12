package bootc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/stageexec"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-script")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStageUpdateDryRunUsesFixedPath(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	ch := make(chan ProgressEvent)
	done := make(chan error, 1)
	go func() { done <- StageUpdate(context.Background(), ch) }()

	var events []ProgressEvent
	for event := range ch {
		events = append(events, event)
	}
	if err := <-done; err != nil {
		t.Fatalf("StageUpdate: %v", err)
	}
	if StageScriptPath != "/usr/libexec/bootc-update-stage" {
		t.Errorf("StageScriptPath = %q", StageScriptPath)
	}
	if len(events) != 2 ||
		events[0] != (ProgressEvent{Type: EventMessage, Message: "[DRY-RUN] would run " + StageScriptPath}) ||
		events[1].Type != EventComplete {
		t.Errorf("events = %+v, want fixed-path preview and completion", events)
	}
}

// The stage-update error contract is stageexec's: the provider aliases the
// shared types instead of re-mapping them, so a staging failure is
// interchangeable with a status-read failure for errors.Is/errors.As.
func TestStageErrorContractIsStageexec(t *testing.T) {
	var deadline error = &Error{Message: "Update staging timed out", Err: context.DeadlineExceeded}
	var stageErr *stageexec.Error
	if !errors.As(deadline, &stageErr) || !errors.Is(deadline, context.DeadlineExceeded) {
		t.Errorf("deadline error = %T %v", deadline, deadline)
	}

	var missing error = &NotFoundError{Message: "pkexec not found"}
	var notFound *stageexec.NotFoundError
	if !errors.As(missing, &notFound) {
		t.Errorf("missing error = %T %v", missing, missing)
	}
}
