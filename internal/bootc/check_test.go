package bootc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseUpgradeCheck(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    AvailableUpdate
		wantErr string
	}{
		{
			name: "current",
			out:  "No changes in: registry.example/os:stable\n",
			want: AvailableUpdate{},
		},
		{
			name: "available with version",
			out: "Update available for: registry.example/os:stable\n" +
				" Version: 20260906\n" +
				" Digest: sha256:abc123\n",
			want: AvailableUpdate{
				Available: true,
				Version:   "20260906",
				Digest:    "sha256:abc123",
			},
		},
		{
			name: "malformed successful output",
			out: "Update available for: registry.example/os:stable\n" +
				" Channel: stable\n",
			wantErr: "unexpected bootc upgrade --check output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUpgradeCheck(tt.out)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseUpgradeCheck() = %+v, nil error; want %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseUpgradeCheck() error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseUpgradeCheck() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseUpgradeCheck() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseUpgradeCheckRejectsMissingMetadata(t *testing.T) {
	tests := []struct {
		name string
		out  string
	}{
		{
			name: "missing version",
			out: "Update available for: registry.example/os:stable\n" +
				" Digest: sha256:abc123\n",
		},
		{
			name: "missing digest",
			out: "Update available for: registry.example/os:stable\n" +
				" Version: 20260906\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUpgradeCheck(tt.out)
			if err == nil {
				t.Fatalf("parseUpgradeCheck() = %+v, nil error; want missing metadata failure", got)
			}
			var bootcErr *Error
			if !errors.As(err, &bootcErr) {
				t.Fatalf("errors.As(*Error) = false; err = %T %v", err, err)
			}
			if !strings.Contains(bootcErr.Message, "unexpected bootc upgrade --check output") {
				t.Fatalf("error %q missing malformed-output prefix", bootcErr.Message)
			}
		})
	}
}

func TestCheckUpdateFromSuccessAndArgs(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	script := writeScript(t, "printf '%s\\n' \"$@\" > "+shellQuote(argsFile)+"\n"+
		"cat <<'EOF'\n"+
		"Update available for: registry.example/os:stable\n"+
		" Version: 20260906\n"+
		" Digest: sha256:abc123\n"+
		"EOF\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	got, err := checkUpdateFrom(ctx, script)
	if err != nil {
		t.Fatalf("checkUpdateFrom() error = %v", err)
	}
	want := AvailableUpdate{Available: true, Version: "20260906", Digest: "sha256:abc123"}
	if got != want {
		t.Fatalf("checkUpdateFrom() = %+v, want %+v", got, want)
	}

	args, err := osReadFileString(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if args != "upgrade\n--check\n" {
		t.Fatalf("args = %q, want %q", args, "upgrade\n--check\n")
	}
}

func TestCheckUpdateFromCommandFailure(t *testing.T) {
	script := writeScript(t, "echo 'cannot check updates' >&2\nexit 4\n")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	update, err := checkUpdateFrom(ctx, script)
	if err == nil {
		t.Fatalf("checkUpdateFrom() = %+v, nil error; want failure", update)
	}
	var bootcErr *Error
	if !errors.As(err, &bootcErr) {
		t.Fatalf("errors.As(*Error) = false; err = %T %v", err, err)
	}
	if !strings.Contains(bootcErr.Message, "exit 4") {
		t.Fatalf("message %q missing exit code", bootcErr.Message)
	}
	if !strings.Contains(bootcErr.Message, "cannot check updates") {
		t.Fatalf("message %q missing stderr", bootcErr.Message)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("plain exit failure matches context sentinel: %v", err)
	}
}

func TestCheckUpdateFromMissingExecutable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-bootc")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	_, err := checkUpdateFrom(ctx, missing)
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("errors.As(*NotFoundError) = false; err = %T %v", err, err)
	}
}

func TestCheckUpdateFromDeadline(t *testing.T) {
	script := writeScript(t, "exec sleep 30\n")

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
}

func TestCheckUpdateFromCanceled(t *testing.T) {
	script := writeScript(t, "exec sleep 30\n")

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
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func osReadFileString(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
