package sysupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckUpdateCommandOutcomes(t *testing.T) {
	t.Run("available and passes fixed args", func(t *testing.T) {
		argsFile := filepath.Join(t.TempDir(), "args")
		script := writeCheckScript(t, "printf '%s\\n' \"$@\" > "+shellQuote(argsFile)+"\n"+
			"printf '20260906010101\\n'\n")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)

		got, err := checkUpdateFrom(ctx, script)
		if err != nil {
			t.Fatalf("checkUpdateFrom() error = %v", err)
		}
		want := AvailableUpdate{Available: true, Version: "20260906010101"}
		if got != want {
			t.Fatalf("checkUpdateFrom() = %+v, want %+v", got, want)
		}

		args, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("read args file: %v", err)
		}
		if string(args) != "--no-pager\ncheck-new\n" {
			t.Fatalf("args = %q, want %q", string(args), "--no-pager\ncheck-new\n")
		}
	})

	t.Run("current exit code returns no update", func(t *testing.T) {
		script := writeCheckScript(t, "exit 1\n")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)

		got, err := checkUpdateFrom(ctx, script)
		if err != nil {
			t.Fatalf("checkUpdateFrom() error = %v", err)
		}
		if got != (AvailableUpdate{}) {
			t.Fatalf("checkUpdateFrom() = %+v, want zero AvailableUpdate", got)
		}
	})

	t.Run("non-zero exit with stderr is provider error", func(t *testing.T) {
		script := writeCheckScript(t, "echo 'failed to query' >&2\nexit 2\n")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)

		update, err := checkUpdateFrom(ctx, script)
		if err == nil {
			t.Fatalf("checkUpdateFrom() = %+v, nil error; want failure", update)
		}
		var updateErr *Error
		if !errors.As(err, &updateErr) {
			t.Fatalf("errors.As(*Error) = false; err = %T %v", err, err)
		}
		if !strings.Contains(updateErr.Message, "exit 2") {
			t.Fatalf("message %q missing exit code", updateErr.Message)
		}
		if !strings.Contains(updateErr.Message, "failed to query") {
			t.Fatalf("message %q missing stderr", updateErr.Message)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			t.Fatalf("plain exit failure matches context sentinel: %v", err)
		}
	})

	t.Run("missing executable is NotFoundError", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "no-such-systemd-sysupdate")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)

		_, err := checkUpdateFrom(ctx, missing)
		var notFound *NotFoundError
		if !errors.As(err, &notFound) {
			t.Fatalf("errors.As(*NotFoundError) = false; err = %T %v", err, err)
		}
	})

	t.Run("deadline unwraps to DeadlineExceeded", func(t *testing.T) {
		script := writeCheckScript(t, "exec sleep 30\n")

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		t.Cleanup(cancel)

		_, err := checkUpdateFrom(ctx, script)
		if err == nil {
			t.Fatal("checkUpdateFrom() = nil error, want deadline failure")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("errors.Is(err, DeadlineExceeded) = false; err = %v", err)
		}
		if errors.Is(err, context.Canceled) {
			t.Fatalf("deadline error also matches context.Canceled: %v", err)
		}
		if strings.Contains(err.Error(), "signal: killed") {
			t.Fatalf("message leaks raw kill signal: %q", err.Error())
		}
	})

	t.Run("cancellation unwraps to Canceled", func(t *testing.T) {
		script := writeCheckScript(t, "exec sleep 30\n")

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		done := make(chan error, 1)
		go func() {
			_, err := checkUpdateFrom(ctx, script)
			done <- err
		}()
		time.Sleep(50 * time.Millisecond)
		cancel()

		var err error
		select {
		case err = <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("checkUpdateFrom() did not return")
		}
		if err == nil {
			t.Fatal("checkUpdateFrom() = nil error, want cancellation failure")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("errors.Is(err, Canceled) = false; err = %v", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation error also matches context.DeadlineExceeded: %v", err)
		}
		if strings.Contains(err.Error(), "signal: killed") {
			t.Fatalf("message leaks raw kill signal: %q", err.Error())
		}
	})
}

func TestCheckUpdateRejectsMalformedSuccessOutput(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{
			name:    "empty",
			body:    "printf '\\n'\n",
			wantMsg: "unexpected systemd-sysupdate check-new output: empty",
		},
		{
			name:    "nondigit version",
			body:    "printf 'stable\\n'\n",
			wantMsg: "unexpected systemd-sysupdate check-new output",
		},
		{
			name:    "multiline output",
			body:    "printf '20260906010101\\nextra\\n'\n",
			wantMsg: "unexpected systemd-sysupdate check-new output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := writeCheckScript(t, tt.body)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			t.Cleanup(cancel)

			got, err := checkUpdateFrom(ctx, script)
			if err == nil {
				t.Fatalf("checkUpdateFrom() = %+v, nil error; want malformed-output failure", got)
			}
			var updateErr *Error
			if !errors.As(err, &updateErr) {
				t.Fatalf("errors.As(*Error) = false; err = %T %v", err, err)
			}
			if !strings.Contains(updateErr.Message, tt.wantMsg) {
				t.Fatalf("error %q missing %q", updateErr.Message, tt.wantMsg)
			}
		})
	}
}

func writeCheckScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-systemd-sysupdate")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}
