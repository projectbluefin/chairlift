package sysupdate

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestDefaultContextCarriesDefaultTimeout(t *testing.T) {
	ctx, cancel := DefaultContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("DefaultContext() returned a context with no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > DefaultTimeout {
		t.Errorf("deadline in %v, want (0, %v]", remaining, DefaultTimeout)
	}
	if err := ctx.Err(); err != nil {
		t.Errorf("ctx.Err() = %v, want nil before cancel", err)
	}
}

func TestDefaultContextCancelStopsWork(t *testing.T) {
	ctx, cancel := DefaultContext()
	cancel()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not done after cancel")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("ctx.Err() = %v, want context.Canceled", ctx.Err())
	}
}

func TestErrorMessageAndUnwrap(t *testing.T) {
	tests := map[string]struct {
		cause   error
		wantIs  error
		wantNil bool
	}{
		"deadline cause": {cause: context.DeadlineExceeded, wantIs: context.DeadlineExceeded},
		"canceled cause": {cause: context.Canceled, wantIs: context.Canceled},
		"no cause":       {cause: nil, wantNil: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := &Error{Message: "update failed", Err: tc.cause}

			if got := err.Error(); got != "update failed" {
				t.Errorf("Error() = %q, want %q", got, "update failed")
			}
			if got := err.Unwrap(); got != tc.cause {
				t.Errorf("Unwrap() = %v, want %v", got, tc.cause)
			}
			if tc.wantNil {
				return
			}
			if !errors.Is(err, tc.wantIs) {
				t.Errorf("errors.Is(err, %v) = false, want true", tc.wantIs)
			}
		})
	}
}

func TestErrorIsDiscoverableWithErrorsAs(t *testing.T) {
	var wrapped error = &Error{Message: "staging failed", Err: context.Canceled}

	var updateErr *Error
	if !errors.As(wrapped, &updateErr) {
		t.Fatalf("errors.As did not match *Error for %T", wrapped)
	}
	if updateErr.Message != "staging failed" {
		t.Errorf("Message = %q, want %q", updateErr.Message, "staging failed")
	}
}

func TestNotFoundErrorMessage(t *testing.T) {
	err := &NotFoundError{Message: "systemd-sysupdate is not installed"}

	if got := err.Error(); got != "systemd-sysupdate is not installed" {
		t.Errorf("Error() = %q, want %q", got, "systemd-sysupdate is not installed")
	}

	var notFound *NotFoundError
	if !errors.As(error(err), &notFound) {
		t.Error("errors.As did not match *NotFoundError")
	}
	var updateErr *Error
	if errors.As(error(err), &updateErr) {
		t.Error("*NotFoundError must not satisfy *Error; callers classify on the distinct type")
	}
}

func TestIsNativeABFollowsMarkerFile(t *testing.T) {
	_, statErr := os.Stat(MarkerPath)
	want := statErr == nil

	if got := IsNativeAB(); got != want {
		t.Errorf("IsNativeAB() = %v, want %v (stat %s: %v)", got, want, MarkerPath, statErr)
	}
}

func TestMarkerPathIsSnosiContract(t *testing.T) {
	// snosi units gate on ConditionPathExists=/usr/lib/snosi/native-ab; the
	// host-type probe must stay on that exact path, not a bootc probe.
	if MarkerPath != "/usr/lib/snosi/native-ab" {
		t.Errorf("MarkerPath = %q, want %q", MarkerPath, "/usr/lib/snosi/native-ab")
	}
}

func TestIsNativeABCachedMatchesAndIsStable(t *testing.T) {
	first := IsNativeABCached()

	if first != IsNativeAB() {
		t.Errorf("IsNativeABCached() = %v, want the uncached result %v", first, IsNativeAB())
	}
	for i := 0; i < 3; i++ {
		if got := IsNativeABCached(); got != first {
			t.Fatalf("IsNativeABCached() call %d = %v, want stable %v", i+2, got, first)
		}
	}
}

func TestIsNativeABCachedIsRaceFree(t *testing.T) {
	done := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		go func() { done <- IsNativeABCached() }()
	}

	want := <-done
	for i := 0; i < 7; i++ {
		if got := <-done; got != want {
			t.Fatalf("concurrent IsNativeABCached() = %v, want %v", got, want)
		}
	}
}

func TestDefaultTimeoutIsThirtyMinutes(t *testing.T) {
	if DefaultTimeout != 30*time.Minute {
		t.Errorf("DefaultTimeout = %v, want 30m", DefaultTimeout)
	}
}
