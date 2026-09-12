package homebrew

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// captureBrewArgv runs fn with dry-run mode enabled and returns every brew
// argv that runBrewCommand short-circuited, in call order.
//
// runBrewCommand discards nothing in dry-run mode: it logs
// "[DRY-RUN] Would execute: brew <args>" and returns that same string. Every
// mutation wrapper throws the string away and returns only an error, so the
// log line is the only place the assembled argv is observable without
// executing brew. These wrappers are otherwise unreachable in a unit test —
// they call runBrewCommand directly, with no injectable runner.
func captureBrewArgv(t *testing.T, fn func()) []string {
	t.Helper()

	originalDryRun := dryrun.Enabled()
	originalFlags := log.Flags()
	var buf bytes.Buffer

	log.SetOutput(&buf)
	log.SetFlags(0)
	dryrun.Set(true)
	t.Cleanup(func() {
		dryrun.Set(originalDryRun)
		log.SetFlags(originalFlags)
		log.SetOutput(os.Stderr)
	})

	fn()

	log.SetOutput(os.Stderr)

	const prefix = "[DRY-RUN] Would execute: brew "
	var argv []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if after, ok := strings.CutPrefix(line, prefix); ok {
			argv = append(argv, after)
		}
	}
	return argv
}

func captureSingleBrewArgv(t *testing.T, fn func()) string {
	t.Helper()

	argv := captureBrewArgv(t, fn)
	if len(argv) != 1 {
		t.Fatalf("got %d brew invocations %q, want exactly 1", len(argv), argv)
	}
	return argv[0]
}

func TestMutationWrappersBuildExpectedArgv(t *testing.T) {
	cases := []struct {
		name string
		run  func() error
		want string
	}{
		{"Tap", func() error { return Tap("multica-ai/tap") }, "tap multica-ai/tap"},
		{"InstallFormula", func() error { return Install("gh", false) }, "install gh"},
		{"InstallCask", func() error { return Install("firefox", true) }, "install --cask firefox"},
		{"UninstallFormula", func() error { return Uninstall("gh", false) }, "uninstall gh"},
		{"UninstallCask", func() error { return Uninstall("firefox", true) }, "uninstall --cask firefox"},
		{"UpgradeNamed", func() error { return Upgrade("gh") }, "upgrade gh"},
		{"UpgradeAll", func() error { return Upgrade("") }, "upgrade"},
		{"Update", Update, "update"},
		{"Pin", func() error { return Pin("gh") }, "pin gh"},
		{"Unpin", func() error { return Unpin("gh") }, "unpin gh"},
		{"BundleDumpPlain", func() error { return BundleDump("", false) }, "bundle dump"},
		{"BundleDumpToPath", func() error { return BundleDump("/tmp/Brewfile", false) }, "bundle dump --file=/tmp/Brewfile"},
		{"BundleDumpForced", func() error { return BundleDump("/tmp/Brewfile", true) }, "bundle dump --file=/tmp/Brewfile --force"},
		{"BundleDumpForcedNoPath", func() error { return BundleDump("", true) }, "bundle dump --force"},
		{"BundleInstallFromPath", func() error { return BundleInstall("/tmp/Brewfile") }, "bundle install --file=/tmp/Brewfile"},
		{"BundleInstallDefault", func() error { return BundleInstall("") }, "bundle install"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			got := captureSingleBrewArgv(t, func() { err = tc.run() })
			if err != nil {
				t.Fatalf("%s returned error %v, want nil in dry-run mode", tc.name, err)
			}
			if got != tc.want {
				t.Errorf("brew argv = %q, want %q", got, tc.want)
			}
		})
	}
}

// Upgrade("") must not append an empty argument: "brew upgrade ”" is not the
// same command as "brew upgrade".
func TestUpgradeAllPassesNoPackageArgument(t *testing.T) {
	got := captureSingleBrewArgv(t, func() {
		if err := Upgrade(""); err != nil {
			t.Fatalf("Upgrade(\"\"): %v", err)
		}
	})
	if strings.Fields(got)[0] != "upgrade" || len(strings.Fields(got)) != 1 {
		t.Errorf("brew argv = %q, want a bare \"upgrade\"", got)
	}
}

func TestCleanupReturnsDryRunMessage(t *testing.T) {
	var out string
	var err error

	argv := captureSingleBrewArgv(t, func() { out, err = Cleanup() })

	if err != nil {
		t.Fatalf("Cleanup() error = %v, want nil in dry-run mode", err)
	}
	if argv != "cleanup" {
		t.Errorf("brew argv = %q, want \"cleanup\"", argv)
	}
	if out != "[DRY-RUN] Would execute: brew cleanup" {
		t.Errorf("Cleanup() output = %q, want the dry-run preview line", out)
	}
}

func TestTrustPackagesArgvSplitsFormulaeAndCasks(t *testing.T) {
	tap := UntrustedTap{
		Name:     "multica-ai/tap",
		Formulae: []string{"multica-ai/tap/multica", "multica-ai/tap/other"},
		Casks:    []string{"multica-ai/tap/multica-app"},
	}

	var err error
	argv := captureBrewArgv(t, func() { err = TrustPackages(tap) })

	if err != nil {
		t.Fatalf("TrustPackages() error = %v, want nil in dry-run mode", err)
	}
	want := []string{
		"trust --formula multica-ai/tap/multica multica-ai/tap/other",
		"trust --cask multica-ai/tap/multica-app",
	}
	if len(argv) != len(want) {
		t.Fatalf("got %d brew invocations %q, want %q", len(argv), argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Errorf("invocation %d = %q, want %q", i, argv[i], want[i])
		}
	}
}

func TestTrustPackagesSkipsEmptyNamespaces(t *testing.T) {
	t.Run("formulae only", func(t *testing.T) {
		argv := captureBrewArgv(t, func() {
			if err := TrustPackages(UntrustedTap{
				Name:     "multica-ai/tap",
				Formulae: []string{"multica-ai/tap/multica"},
			}); err != nil {
				t.Fatalf("TrustPackages: %v", err)
			}
		})
		if len(argv) != 1 || !strings.Contains(argv[0], "--formula") {
			t.Errorf("brew invocations = %q, want a single --formula trust", argv)
		}
	})

	t.Run("nothing installed", func(t *testing.T) {
		argv := captureBrewArgv(t, func() {
			if err := TrustPackages(UntrustedTap{Name: "multica-ai/tap"}); err != nil {
				t.Fatalf("TrustPackages: %v", err)
			}
		})
		if len(argv) != 0 {
			t.Errorf("brew invocations = %q, want none for a tap with no packages", argv)
		}
	})
}

// Every command these wrappers emit must be classified as state-changing, or
// dry-run mode would execute it for real and the 30-second read timeout would
// bound a download or build.
func TestMutationCommandsAreClassifiedStateChanging(t *testing.T) {
	for _, verb := range []string{
		"tap", "install", "uninstall", "upgrade", "update",
		"pin", "unpin", "bundle", "cleanup", "trust",
	} {
		if !stateChangingCommands[verb] {
			t.Errorf("stateChangingCommands[%q] = false, want true", verb)
		}
		if got := commandTimeout([]string{verb}); got != mutationTimeout {
			t.Errorf("commandTimeout(%q) = %v, want %v", verb, got, mutationTimeout)
		}
	}
}

func TestErrorUnwrapsUnderlyingCause(t *testing.T) {
	err := &Error{Message: "brew timed out", Err: context.DeadlineExceeded}

	if err.Error() != "brew timed out" {
		t.Errorf("Error() = %q, want %q", err.Error(), "brew timed out")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("errors.Is(err, context.DeadlineExceeded) = false, want true")
	}
	if errors.Is(err, context.Canceled) {
		t.Error("errors.Is(err, context.Canceled) = true, want false")
	}
}

func TestErrorWithoutCauseUnwrapsToNil(t *testing.T) {
	err := &Error{Message: "Failed to parse JSON"}

	if errors.Unwrap(err) != nil {
		t.Errorf("Unwrap() = %v, want nil", errors.Unwrap(err))
	}
	if err.Error() != "Failed to parse JSON" {
		t.Errorf("Error() = %q, want %q", err.Error(), "Failed to parse JSON")
	}
}

func TestNotFoundErrorMessage(t *testing.T) {
	err := &NotFoundError{Message: "Homebrew is not installed"}

	if err.Error() != "Homebrew is not installed" {
		t.Errorf("Error() = %q, want %q", err.Error(), "Homebrew is not installed")
	}

	var target *NotFoundError
	if !errors.As(error(err), &target) {
		t.Error("errors.As did not match *NotFoundError")
	}
}

func TestUntrustedTapErrorMessage(t *testing.T) {
	err := &UntrustedTapError{Message: "taps are not trusted"}

	if err.Error() != "taps are not trusted" {
		t.Errorf("Error() = %q, want %q", err.Error(), "taps are not trusted")
	}
}
