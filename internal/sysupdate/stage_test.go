package sysupdate

import (
	"context"
	"errors"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/stageexec"
)

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
	if StageScriptPath != "/usr/libexec/snosi-sysupdate-stage" {
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
	var canceled error = &Error{Message: "Update staging was canceled", Err: context.Canceled}
	var stageErr *stageexec.Error
	if !errors.As(canceled, &stageErr) || !errors.Is(canceled, context.Canceled) {
		t.Errorf("cancellation error = %T %v", canceled, canceled)
	}

	var missing error = &NotFoundError{Message: "pkexec not found"}
	var notFound *stageexec.NotFoundError
	if !errors.As(missing, &notFound) {
		t.Errorf("missing error = %T %v", missing, missing)
	}
}

func TestNativeABDetectionUsesMarkerConstant(t *testing.T) {
	if MarkerPath != "/usr/lib/snosi/native-ab" {
		t.Errorf("MarkerPath = %q, want the snosi native A/B marker", MarkerPath)
	}
}
