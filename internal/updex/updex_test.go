package updex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/updexhelper"
)

// writeFakePkexec writes an executable shell script standing in for pkexec:
// it records its own argv (one element per line) to capturedArgsFile and
// exits 0. It never execs the real pkexec or requires root.
func writeFakePkexec(t *testing.T, capturedArgsFile string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-pkexec")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + capturedArgsFile + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake pkexec: %v", err)
	}
	return path
}

func TestRunHelperNonDryRunInvokesPkexecWithAbsoluteHelperPath(t *testing.T) {
	dryrun.Set(false)

	capturedArgsFile := filepath.Join(t.TempDir(), "captured-args")
	fakePkexec := writeFakePkexec(t, capturedArgsFile)

	ctx := context.Background()
	if _, _, err := runHelper(ctx, fakePkexec, "enable-feature", "demo"); err != nil {
		t.Fatalf("runHelper: %v", err)
	}

	data, err := os.ReadFile(capturedArgsFile)
	if err != nil {
		t.Fatalf("reading captured pkexec argv: %v", err)
	}
	got := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	want := []string{HelperPath, "enable-feature", "demo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pkexec argv = %v, want %v", got, want)
	}
	if got[0] != "/usr/bin/chairlift-updex-helper" {
		t.Fatalf("helper path passed to pkexec = %q, want the fixed absolute path matching data/io.projectbluefin.chairlift.updex.policy's exec.path annotation", got[0])
	}
}

func TestRunHelperDryRunNeverInvokesPkexec(t *testing.T) {
	dryrun.Set(true)
	defer dryrun.Set(false)

	// A path that does not exist: if runHelper failed to short-circuit and
	// tried to actually run it, cmd.Run() would return an error and this
	// test would fail loudly instead of silently passing.
	nonexistentPkexec := filepath.Join(t.TempDir(), "pkexec-should-never-run")

	ctx := context.Background()
	stdout, stderr, err := runHelper(ctx, nonexistentPkexec, "enable-feature", "demo")
	if err != nil {
		t.Fatalf("runHelper dry-run returned error, want short-circuit with nil error: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("runHelper dry-run returned stdout=%q stderr=%q, want both empty", stdout, stderr)
	}
}

func TestEnableDisableUpdateFeaturesDryRunNeverInvokePkexec(t *testing.T) {
	dryrun.Set(true)
	defer dryrun.Set(false)

	ctx := context.Background()
	if err := EnableFeature(ctx, "demo"); err != nil {
		t.Errorf("EnableFeature dry-run: %v", err)
	}
	if err := DisableFeature(ctx, "demo"); err != nil {
		t.Errorf("DisableFeature dry-run: %v", err)
	}
	if err := UpdateFeatures(ctx); err != nil {
		t.Errorf("UpdateFeatures dry-run: %v", err)
	}
}

func TestPrivilegedOperationsUseExactHelperArguments(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	dir := t.TempDir()
	capturedArgsFile := filepath.Join(dir, "captured-args")
	fakePkexec := filepath.Join(dir, pkexec.Command)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CHAIRLIFT_UPDEX_ARGS\"\n"
	if err := os.WriteFile(fakePkexec, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake pkexec: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CHAIRLIFT_UPDEX_ARGS", capturedArgsFile)

	tests := []struct {
		name string
		run  func(context.Context) error
		want []string
	}{
		{
			name: "enable feature",
			run:  func(ctx context.Context) error { return EnableFeature(ctx, "demo") },
			want: []string{HelperPath, "enable-feature", "demo"},
		},
		{
			name: "disable feature",
			run:  func(ctx context.Context) error { return DisableFeature(ctx, "demo") },
			want: []string{HelperPath, "disable-feature", "demo"},
		},
		{
			name: "update features",
			run:  UpdateFeatures,
			want: []string{HelperPath, "update"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(context.Background()); err != nil {
				t.Fatalf("privileged operation returned error: %v", err)
			}
			data, err := os.ReadFile(capturedArgsFile)
			if err != nil {
				t.Fatalf("reading captured pkexec argv: %v", err)
			}
			got := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("pkexec argv = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunHelperClassifiesFailures(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	t.Run("missing pkexec", func(t *testing.T) {
		_, _, err := runHelper(context.Background(), "chairlift-pkexec-that-does-not-exist", "update")
		var notFound *NotFoundError
		if !errors.As(err, &notFound) {
			t.Fatalf("error = %T (%v), want *NotFoundError", err, err)
		}
	})

	t.Run("non-zero exit retains stderr", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "failing-pkexec")
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'authorization denied' >&2\nexit 17\n"), 0o755); err != nil {
			t.Fatalf("writing failing pkexec: %v", err)
		}
		stdout, stderr, err := runHelper(context.Background(), path, "update")
		if stdout != "" || !strings.Contains(stderr, "authorization denied") {
			t.Fatalf("stdout/stderr = %q/%q, want retained stderr", stdout, stderr)
		}
		var updexErr *Error
		if !errors.As(err, &updexErr) || !strings.Contains(err.Error(), "exit 17") ||
			!strings.Contains(err.Error(), "authorization denied") {
			t.Fatalf("error = %T (%v), want classified exit error with stderr", err, err)
		}
	})

	t.Run("deadline", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "slow-pkexec")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o755); err != nil {
			t.Fatalf("writing slow pkexec: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, _, err := runHelper(ctx, path, "update")
		var updexErr *Error
		if !errors.As(err, &updexErr) || err.Error() != "command timed out" {
			t.Fatalf("error = %T (%v), want timed-out *Error", err, err)
		}
	})
}

func TestDryRunStateAndDefaultContext(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	if !dryrun.Enabled() {
		t.Fatal("dryrun.Enabled() = false after dryrun.Set(true)")
	}

	ctx, cancel := DefaultContext()
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("DefaultContext has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= DefaultTimeout-time.Second || remaining > DefaultTimeout {
		t.Fatalf("DefaultContext deadline remaining = %v, want approximately %v", remaining, DefaultTimeout)
	}
}

func TestUpdexAvailabilityRequiresNonEmptyFeatures(t *testing.T) {
	origLister := featuresLister
	t.Cleanup(func() { featuresLister = origLister })

	t.Run("returns false when feature list is empty", func(t *testing.T) {
		featuresLister = func(ctx context.Context) ([]Feature, error) {
			return []Feature{}, nil
		}
		if IsInstalled() {
			t.Fatal("IsInstalled() = true with empty feature list, want false")
		}
	})

	t.Run("returns false when features listing returns error", func(t *testing.T) {
		featuresLister = func(ctx context.Context) ([]Feature, error) {
			return nil, errors.New("failed to read features")
		}
		if IsInstalled() {
			t.Fatal("IsInstalled() = true with listing error, want false")
		}
	})

	t.Run("returns true when features list is non-empty and error is nil", func(t *testing.T) {
		featuresLister = func(ctx context.Context) ([]Feature, error) {
			return []Feature{{Name: "test-feature", Enabled: true}}, nil
		}
		if !IsInstalled() {
			t.Fatal("IsInstalled() = false with non-empty features, want true")
		}
	})
}

func TestCheckFeaturesReturnsResultsAndRetainsWarnings(t *testing.T) {
	origChecker := featuresChecker
	t.Cleanup(func() { featuresChecker = origChecker })

	expectedChecks := []FeatureCheck{
		{
			Feature: "demo",
			Results: []CheckResult{
				{
					Component:       "comp1",
					CurrentVersion:  "1.0",
					NewestVersion:   "2.0",
					UpdateAvailable: true,
				},
			},
		},
	}
	expectedWarnings := []string{
		"failed to get available versions for comp2: 404 Not Found",
	}

	featuresChecker = func(ctx context.Context) ([]FeatureCheck, []string, error) {
		return expectedChecks, expectedWarnings, nil
	}

	ctx := context.Background()
	checks, warnings, err := CheckFeatures(ctx)
	if err != nil {
		t.Fatalf("CheckFeatures() returned unexpected error: %v", err)
	}
	if !reflect.DeepEqual(checks, expectedChecks) {
		t.Fatalf("CheckFeatures() checks = %#v, want %#v", checks, expectedChecks)
	}
	if !reflect.DeepEqual(warnings, expectedWarnings) {
		t.Fatalf("CheckFeatures() warnings = %#v, want %#v", warnings, expectedWarnings)
	}
}

func TestCheckFeaturesPropagatesError(t *testing.T) {
	origChecker := featuresChecker
	t.Cleanup(func() { featuresChecker = origChecker })

	expectedErr := errors.New("check failed")
	expectedWarnings := []string{"manifest error"}

	featuresChecker = func(ctx context.Context) ([]FeatureCheck, []string, error) {
		return nil, expectedWarnings, expectedErr
	}

	ctx := context.Background()
	checks, warnings, err := CheckFeatures(ctx)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("CheckFeatures() error = %v, want %v", err, expectedErr)
	}
	if checks != nil {
		t.Fatalf("CheckFeatures() checks = %#v, want nil", checks)
	}
	if !reflect.DeepEqual(warnings, expectedWarnings) {
		t.Fatalf("CheckFeatures() warnings = %#v, want %#v", warnings, expectedWarnings)
	}
}

func TestWarningCapturingReporterCapturesFormattedWarnings(t *testing.T) {
	rep := &warningCapturingReporter{}
	rep.Warning("warning %d: %s", 1, "test")
	rep.Warning("second warning")

	got := rep.Warnings()
	want := []string{"warning 1: test", "second warning"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Warnings() = %#v, want %#v", got, want)
	}
}

// The GUI's clock runs while the PolicyKit prompt is open and the helper's
// starts after authentication, so every mutation context must outlast the
// helper's own budget by the authentication allowance (#695).
func TestMutationTimeoutOutlastsTheHelperBudget(t *testing.T) {
	for _, command := range updexhelper.SupportedCommands() {
		if got, want := MutationTimeout(command), updexhelper.Timeout(command)+AuthenticationAllowance; got != want {
			t.Errorf("MutationTimeout(%s) = %v, want %v", command, got, want)
		}
	}
	if AuthenticationAllowance <= 0 {
		t.Fatal("AuthenticationAllowance must be positive")
	}
}
