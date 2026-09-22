package homebrew

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func writeBundleFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write bundle %s: %v", path, err)
	}
	return path
}

func TestAvailableBundlesDiscoversEveryConfiguredDirectory(t *testing.T) {
	root := t.TempDir()
	firstDir := filepath.Join(root, "a")
	secondDir := filepath.Join(root, "b")
	if err := os.Mkdir(firstDir, 0o755); err != nil {
		t.Fatalf("create first bundle directory: %v", err)
	}
	if err := os.Mkdir(secondDir, 0o755); err != nil {
		t.Fatalf("create second bundle directory: %v", err)
	}
	firstCLI := writeBundleFile(t, firstDir, "cli.Brewfile", "# Command-line tools\nbrew \"bat\"\n")
	secondCLI := writeBundleFile(t, secondDir, "cli.Brewfile", "# Alternate CLI set\nbrew \"fd\"\n")
	fonts := writeBundleFile(t, secondDir, "fonts.Brewfile", "cask \"font-test\"\n")
	writeBundleFile(t, firstDir, "ignored.txt", "# not a Brewfile\n")
	if err := os.Mkdir(filepath.Join(firstDir, "directory.Brewfile"), 0o700); err != nil {
		t.Fatalf("create Brewfile-shaped directory: %v", err)
	}

	missingDir := filepath.Join(t.TempDir(), "not-installed")
	bundles, err := AvailableBundles([]string{
		firstDir,
		missingDir,
		secondDir,
		filepath.Join(firstDir, "."),
	})
	if err != nil {
		t.Fatalf("AvailableBundles() error = %v, want nil", err)
	}

	want := []Bundle{
		{Name: "cli", Description: "Command-line tools", Path: firstCLI},
		{Name: "cli", Description: "Alternate CLI set", Path: secondCLI},
		{Name: "fonts", Description: "Fonts", Path: fonts},
	}
	if !reflect.DeepEqual(bundles, want) {
		t.Fatalf("AvailableBundles() = %#v, want %#v", bundles, want)
	}
	for _, bundle := range bundles {
		if !filepath.IsAbs(bundle.Path) {
			t.Errorf("bundle path %q is not absolute", bundle.Path)
		}
	}
}

func TestAvailableBundlesMissingPathsAreNotErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	bundles, err := AvailableBundles([]string{missing})
	if err != nil {
		t.Fatalf("AvailableBundles(%q) error = %v, want nil", missing, err)
	}
	if len(bundles) != 0 {
		t.Fatalf("AvailableBundles(%q) = %#v, want no bundles", missing, bundles)
	}
}

func TestAvailableBundlesReturnsPartialResultsWithPathErrors(t *testing.T) {
	validDir := t.TempDir()
	wantPath := writeBundleFile(t, validDir, "working.Brewfile", "# Working set\n")
	notDirectory := writeBundleFile(t, t.TempDir(), "not-a-directory", "contents\n")

	bundles, err := AvailableBundles([]string{notDirectory, "", validDir})
	if err == nil {
		t.Fatal("AvailableBundles() error = nil, want invalid-path diagnostics")
	}
	for _, want := range []string{notDirectory, "bundle directory path is empty"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("AvailableBundles() error %q does not contain %q", err, want)
		}
	}

	want := []Bundle{{Name: "working", Description: "Working set", Path: wantPath}}
	if !reflect.DeepEqual(bundles, want) {
		t.Fatalf("AvailableBundles() partial result = %#v, want %#v", bundles, want)
	}
}

func TestAvailableBundlesRejectsUnreadableEntriesWithoutLosingOthers(t *testing.T) {
	dir := t.TempDir()
	goodPath := writeBundleFile(t, dir, "good.Brewfile", "# Good bundle\n")
	brokenPath := filepath.Join(dir, "broken.Brewfile")
	if err := os.Symlink(filepath.Join(dir, "missing-target"), brokenPath); err != nil {
		t.Fatalf("create broken Brewfile symlink: %v", err)
	}

	bundles, err := AvailableBundles([]string{dir})
	if err == nil || !strings.Contains(err.Error(), brokenPath) {
		t.Fatalf("AvailableBundles() error = %v, want broken entry path", err)
	}
	want := []Bundle{{Name: "good", Description: "Good bundle", Path: goodPath}}
	if !reflect.DeepEqual(bundles, want) {
		t.Fatalf("AvailableBundles() partial result = %#v, want %#v", bundles, want)
	}
}

func TestAvailableBundlesBoundsDescriptionRead(t *testing.T) {
	dir := t.TempDir()
	path := writeBundleFile(
		t,
		dir,
		"oversized.Brewfile",
		"#"+strings.Repeat("x", maxBundleDescriptionBytes)+"\nbrew \"bat\"\n",
	)

	bundles, err := AvailableBundles([]string{dir})
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("AvailableBundles() error = %v, want bounded-read error naming %q", err, path)
	}
	if len(bundles) != 0 {
		t.Fatalf("AvailableBundles() = %#v, want oversized description omitted", bundles)
	}
}

func TestBundleInstallHonorsDryRun(t *testing.T) {
	original := dryrun.Enabled()
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(original) })

	if err := BundleInstall("/definitely/not/a/real/Brewfile"); err != nil {
		t.Fatalf("BundleInstall() dry-run error = %v, want nil without invoking brew", err)
	}
}

func TestBundleDescriptionScanningAndHumanizedFallback(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name     string
		fileName string
		content  string
		wantDesc string
	}{
		{
			name:     "single-line comment",
			fileName: "single.Brewfile",
			content:  "# Single line description\nbrew \"bat\"\n",
			wantDesc: "Single line description",
		},
		{
			name:     "multi-line comment block",
			fileName: "multi.Brewfile",
			content:  "# First line of description.\n# Second line of description.\nbrew \"bat\"\n",
			wantDesc: "First line of description. Second line of description.",
		},
		{
			name:     "comment block with empty hash line",
			fileName: "empty_hash.Brewfile",
			content:  "# Leading part.\n#\n# Trailing part.\nbrew \"bat\"\n",
			wantDesc: "Leading part. Trailing part.",
		},
		{
			name:     "leading blank lines before comment block",
			fileName: "blanks.Brewfile",
			content:  "\n\n# After blank lines\nbrew \"bat\"\n",
			wantDesc: "After blank lines",
		},
		{
			name:     "no comment - k8s-tools fallback",
			fileName: "k8s-tools.Brewfile",
			content:  "brew \"kubectl\"\n",
			wantDesc: "K8s Tools",
		},
		{
			name:     "no comment - ai-tools fallback",
			fileName: "ai-tools.Brewfile",
			content:  "brew \"ollama\"\n",
			wantDesc: "AI Tools",
		},
		{
			name:     "no comment - system-dx-flatpaks fallback",
			fileName: "system-dx-flatpaks.Brewfile",
			content:  "cask \"podman-desktop\"\n",
			wantDesc: "System DX Flatpaks",
		},
		{
			name:     "no comment - cli fallback",
			fileName: "cli.Brewfile",
			content:  "brew \"ripgrep\"\n",
			wantDesc: "CLI",
		},
		{
			name:     "no comment - ide fallback",
			fileName: "ide.Brewfile",
			content:  "cask \"visual-studio-code\"\n",
			wantDesc: "IDE",
		},
		{
			name:     "no comment - artwork fallback",
			fileName: "artwork.Brewfile",
			content:  "cask \"bluefin-wallpapers\"\n",
			wantDesc: "Artwork",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeBundleFile(t, dir, tc.fileName, tc.content)
			bundles, err := AvailableBundles([]string{dir})
			if err != nil {
				t.Fatalf("AvailableBundles() error = %v, want nil", err)
			}
			found := false
			for _, b := range bundles {
				if b.Path == path {
					found = true
					if b.Description != tc.wantDesc {
						t.Errorf("Bundle description = %q, want %q", b.Description, tc.wantDesc)
					}
				}
			}
			if !found {
				t.Fatalf("bundle %q not found in AvailableBundles result", path)
			}
		})
	}
}

type mockExitErr struct {
	code int
	msg  string
}

func (e *mockExitErr) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return "exit status"
}

func (e *mockExitErr) ExitCode() int {
	return e.code
}

func TestBundleCheckTwoPassStatusDiscrimination(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		runner     func(...string) (string, error)
		wantStatus BundleStatus
		wantErr    bool
		wantCalls  [][]string
	}{
		{
			name: "Pass 1 exit 0 -> Installed",
			path: "/usr/share/bundles/cli.Brewfile",
			runner: func(args ...string) (string, error) {
				return "The Brewfile's dependencies are satisfied.", nil
			},
			wantStatus: BundleInstalled,
			wantErr:    false,
			wantCalls: [][]string{
				{"bundle", "check", "--file=/usr/share/bundles/cli.Brewfile"},
			},
		},
		{
			name: "Pass 1 exit 1, Pass 2 exit 0 -> Update Available",
			path: "/usr/share/bundles/cli.Brewfile",
			runner: func(args ...string) (string, error) {
				if len(args) >= 3 && args[2] == "--no-upgrade" {
					return "The Brewfile's dependencies are satisfied.", nil
				}
				return "", &mockExitErr{code: 1, msg: "Satisfying dependencies would require upgrades"}
			},
			wantStatus: BundleUpdateAvailable,
			wantErr:    false,
			wantCalls: [][]string{
				{"bundle", "check", "--file=/usr/share/bundles/cli.Brewfile"},
				{"bundle", "check", "--no-upgrade", "--file=/usr/share/bundles/cli.Brewfile"},
			},
		},
		{
			name: "Pass 1 exit 1, Pass 2 exit 1 -> Not Installed",
			path: "/usr/share/bundles/cli.Brewfile",
			runner: func(args ...string) (string, error) {
				return "", &mockExitErr{code: 1, msg: "brew bundle can't satisfy your Brewfile's dependencies."}
			},
			wantStatus: BundleNotInstalled,
			wantErr:    false,
			wantCalls: [][]string{
				{"bundle", "check", "--file=/usr/share/bundles/cli.Brewfile"},
				{"bundle", "check", "--no-upgrade", "--file=/usr/share/bundles/cli.Brewfile"},
			},
		},
		{
			name: "Pass 1 timeout -> Indeterminate",
			path: "/usr/share/bundles/cli.Brewfile",
			runner: func(args ...string) (string, error) {
				return "", &Error{Message: "Command 'brew bundle check' timed out", Err: context.DeadlineExceeded}
			},
			wantStatus: BundleIndeterminate,
			wantErr:    true,
			wantCalls: [][]string{
				{"bundle", "check", "--file=/usr/share/bundles/cli.Brewfile"},
			},
		},
		{
			name: "Pass 1 malformed manifest -> Indeterminate",
			path: "/usr/share/bundles/malformed.Brewfile",
			runner: func(args ...string) (string, error) {
				return "", &mockExitErr{code: 1, msg: "syntax error, unexpected end-of-input"}
			},
			wantStatus: BundleIndeterminate,
			wantErr:    true,
			wantCalls: [][]string{
				{"bundle", "check", "--file=/usr/share/bundles/malformed.Brewfile"},
			},
		},
		{
			name: "Pass 1 exit code 2 (usage/exec error) -> Indeterminate",
			path: "/usr/share/bundles/cli.Brewfile",
			runner: func(args ...string) (string, error) {
				return "", &mockExitErr{code: 2, msg: "invalid option: --unknown"}
			},
			wantStatus: BundleIndeterminate,
			wantErr:    true,
			wantCalls: [][]string{
				{"bundle", "check", "--file=/usr/share/bundles/cli.Brewfile"},
			},
		},
		{
			name: "Pass 2 timeout -> Indeterminate",
			path: "/usr/share/bundles/cli.Brewfile",
			runner: func(args ...string) (string, error) {
				if len(args) >= 3 && args[2] == "--no-upgrade" {
					return "", &Error{Message: "Command 'brew bundle check' timed out", Err: context.DeadlineExceeded}
				}
				return "", &mockExitErr{code: 1, msg: "brew bundle check exit 1"}
			},
			wantStatus: BundleIndeterminate,
			wantErr:    true,
			wantCalls: [][]string{
				{"bundle", "check", "--file=/usr/share/bundles/cli.Brewfile"},
				{"bundle", "check", "--no-upgrade", "--file=/usr/share/bundles/cli.Brewfile"},
			},
		},
		{
			name: "Pass 2 malformed manifest -> Indeterminate",
			path: "/usr/share/bundles/cli.Brewfile",
			runner: func(args ...string) (string, error) {
				if len(args) >= 3 && args[2] == "--no-upgrade" {
					return "", &mockExitErr{code: 1, msg: "undefined method 'invalid_directive'"}
				}
				return "", &mockExitErr{code: 1, msg: "brew bundle check exit 1"}
			},
			wantStatus: BundleIndeterminate,
			wantErr:    true,
			wantCalls: [][]string{
				{"bundle", "check", "--file=/usr/share/bundles/cli.Brewfile"},
				{"bundle", "check", "--no-upgrade", "--file=/usr/share/bundles/cli.Brewfile"},
			},
		},
		{
			name: "Pass 2 exit code 2 -> Indeterminate",
			path: "/usr/share/bundles/cli.Brewfile",
			runner: func(args ...string) (string, error) {
				if len(args) >= 3 && args[2] == "--no-upgrade" {
					return "", &mockExitErr{code: 2, msg: "fatal error"}
				}
				return "", &mockExitErr{code: 1, msg: "brew bundle check exit 1"}
			},
			wantStatus: BundleIndeterminate,
			wantErr:    true,
			wantCalls: [][]string{
				{"bundle", "check", "--file=/usr/share/bundles/cli.Brewfile"},
				{"bundle", "check", "--no-upgrade", "--file=/usr/share/bundles/cli.Brewfile"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var recordedCalls [][]string
			recordRunner := func(args ...string) (string, error) {
				recordedCalls = append(recordedCalls, append([]string(nil), args...))
				return tc.runner(args...)
			}

			status, err := bundleCheckWith(recordRunner, tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("bundleCheckWith() error = %v, wantErr %v", err, tc.wantErr)
			}
			if status != tc.wantStatus {
				t.Errorf("bundleCheckWith() status = %v, want %v", status, tc.wantStatus)
			}
			if !reflect.DeepEqual(recordedCalls, tc.wantCalls) {
				t.Errorf("recorded calls = %#v, want %#v", recordedCalls, tc.wantCalls)
			}
		})
	}
}
