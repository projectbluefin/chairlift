package devtools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func fakeVSCode(t *testing.T, body string) (home, calls string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SETUP_CHECKER_FILE", "")
	previous := dryrun.Enabled()
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(previous) })
	calls = fakeProgram(t, "code", body)
	// Resolve Homebrew from this private PATH too, never the host fallback.
	if err := os.WriteFile(filepath.Join(filepath.Dir(calls), "brew"), []byte("#!/bin/sh\nexit 97\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, calls
}

func writeVSCodeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertVSCodeUnmarked(t *testing.T, home string) {
	t.Helper()
	path := filepath.Join(home, ".local", "share", "ublue", "setup_versioning.json")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed/preview setup left completion marker: %v", err)
	}
}

func TestSetupVSCodeRequiresNativeExecutableBeforeWriting(t *testing.T) {
	for _, preview := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "preview"}[preview], func(t *testing.T) {
			home, calls := fakeVSCode(t, "exit 99\n")
			// Isolate the entire resolution, including executable's Homebrew
			// fallback; overriding lookPath alone still reaches the host's code.
			original := vsCodeExecutable
			vsCodeExecutable = func() string { return "" }
			t.Cleanup(func() { vsCodeExecutable = original })
			dryrun.Set(preview)
			if err := SetupVSCode(context.Background(), nil); !errors.Is(err, ErrVSCodeNotInstalled) {
				t.Fatalf("missing native code error = %v, want ErrVSCodeNotInstalled", err)
			}
			if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
				t.Fatalf("missing code seeded user state: %v, %v", entries, err)
			}
			assertCalls(t, calls)
		})
	}
}

func TestSetupVSCodeSeedsBluefinDefaultsAndInstallsExactExtensions(t *testing.T) {
	home, calls := fakeVSCode(t, "exit 0\n")
	if err := SetupVSCode(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "Code", "User", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]string
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"window.titleBarStyle": "custom",
		"editor.fontFamily":    "'Cascadia Code', 'Droid Sans Mono', 'monospace', monospace, 'Symbols Nerd Font Mono'",
		"update.mode":          "none",
	}
	if !reflect.DeepEqual(settings, want) {
		t.Fatalf("native VS Code defaults = %v, want %v", settings, want)
	}
	assertCalls(t, calls,
		"--install-extension ms-vscode-remote.remote-containers",
		"--install-extension ms-vscode-remote.remote-ssh",
		"--install-extension ms-azuretools.vscode-containers")
	marker, err := os.ReadFile(filepath.Join(home, ".local", "share", "ublue", "setup_versioning.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]map[string]map[string]string
	if err := json.Unmarshal(marker, &state); err != nil || state["version"]["user"]["vscode"] != "1" {
		t.Fatalf("upstream completion version = %s, %v", marker, err)
	}
}

func TestSetupVSCodePreservesExistingJSONCAndSymlinks(t *testing.T) {
	const custom = "// My settings\n{\n    \"editor.fontSize\": 14,\n}\n"
	for _, kind := range []string{"file", "symlink", "dangling-symlink"} {
		t.Run(kind, func(t *testing.T) {
			home, _ := fakeVSCode(t, "exit 0\n")
			path := filepath.Join(home, ".config", "Code", "User", "settings.json")
			target := filepath.Join(home, "user-settings.jsonc")
			if kind == "file" {
				writeVSCodeFixture(t, path, custom)
			} else {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					writeVSCodeFixture(t, target, custom)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if err := SetupVSCode(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			if kind != "file" {
				if link, err := os.Readlink(path); err != nil || link != target {
					t.Fatalf("settings symlink changed: %q, %v", link, err)
				}
			}
			if kind == "dangling-symlink" {
				if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("dangling settings target was created: %v", err)
				}
			} else if got, err := os.ReadFile(path); err != nil || string(got) != custom {
				t.Fatalf("JSONC changed: %q, %v", got, err)
			}
		})
	}
}

func TestSetupVSCodeRepeatDoesNotReinstallExtensions(t *testing.T) {
	_, calls := fakeVSCode(t, `if [ "$1" = --list-extensions ]; then
printf '%s\n' ms-vscode-remote.remote-containers ms-vscode-remote.remote-ssh ms-azuretools.vscode-containers
fi
`)
	for range 2 {
		if err := SetupVSCode(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	assertCalls(t, calls,
		"--install-extension ms-vscode-remote.remote-containers",
		"--install-extension ms-vscode-remote.remote-ssh",
		"--install-extension ms-azuretools.vscode-containers",
		"--list-extensions")
}

func TestSetupVSCodeHonorsVerifiedDistroVersionAndOverride(t *testing.T) {
	home, calls := fakeVSCode(t, "printf '%s\\n' ms-vscode-remote.remote-containers ms-vscode-remote.remote-ssh ms-azuretools.vscode-containers\n")
	marker := filepath.Join(home, "custom-checker.json")
	t.Setenv("SETUP_CHECKER_FILE", marker)
	const state = `{"other":9007199254740993,"version":{"user":{"vscode":"1","theming":"7"},"system":{"tailscale":"2"}}}`
	writeVSCodeFixture(t, marker, state)
	if err := SetupVSCode(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, calls, "--list-extensions")
	if got, err := os.ReadFile(marker); err != nil || string(got) != state {
		t.Fatalf("verified distro marker changed: %s, %v", got, err)
	}
	assertVSCodeUnmarked(t, home) // The override, not a second default marker, owns completion.
}

func TestSetupVSCodeRepairsDistroPremarkedIncompleteExtensions(t *testing.T) {
	home, calls := fakeVSCode(t, "if [ \"$1\" = --list-extensions ]; then printf '%s\\n' ms-vscode-remote.remote-containers; fi\n")
	marker := filepath.Join(home, ".local", "share", "ublue", "setup_versioning.json")
	writeVSCodeFixture(t, marker, `{"version":{"user":{"vscode":"1"}}}`)
	if err := SetupVSCode(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, calls, "--list-extensions",
		"--install-extension ms-vscode-remote.remote-ssh",
		"--install-extension ms-azuretools.vscode-containers")
}

func TestSetupVSCodeDoesNotTrustAnUnreadableExtensionInventory(t *testing.T) {
	home, calls := fakeVSCode(t, "echo 'extension inventory unavailable' >&2\nexit 2\n")
	marker := filepath.Join(home, ".local", "share", "ublue", "setup_versioning.json")
	const state = `{"version":{"user":{"vscode":"1"}}}`
	writeVSCodeFixture(t, marker, state)
	if err := SetupVSCode(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "extension inventory unavailable") {
		t.Fatalf("unverified distro marker reported success: %v", err)
	}
	assertCalls(t, calls, "--list-extensions")
	if got, err := os.ReadFile(marker); err != nil || string(got) != state {
		t.Fatalf("failed observation rewrote distro state: %s, %v", got, err)
	}
}

func TestSetupVSCodePartialFailureRetriesWithoutFalseCompletion(t *testing.T) {
	extensions := []string{
		"ms-vscode-remote.remote-containers",
		"ms-vscode-remote.remote-ssh",
		"ms-azuretools.vscode-containers",
	}
	for failed, extension := range extensions {
		t.Run(extension, func(t *testing.T) {
			home, calls := fakeVSCode(t, `if [ "$2" = `+extension+` ] && [ ! -e "$HOME/retry" ]; then
echo 'already installed: authentication failed' >&2
exit 3
fi
`)
			if err := SetupVSCode(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "authentication failed") {
				t.Fatalf("extension error was suppressed: %v", err)
			}
			assertVSCodeUnmarked(t, home)
			settings := filepath.Join(home, ".config", "Code", "User", "settings.json")
			writeVSCodeFixture(t, settings, "// Keep edits between attempts\n{}\n")
			writeVSCodeFixture(t, filepath.Join(home, "retry"), "")
			if err := SetupVSCode(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, extension := range extensions[:failed+1] {
				want = append(want, "--install-extension "+extension)
			}
			for _, extension := range extensions {
				want = append(want, "--install-extension "+extension)
			}
			assertCalls(t, calls, want...)
			if got, err := os.ReadFile(settings); err != nil || string(got) != "// Keep edits between attempts\n{}\n" {
				t.Fatalf("retry overwrote settings: %q, %v", got, err)
			}
		})
	}
}

func TestSetupVSCodePropagatesSettingsStatFailure(t *testing.T) {
	home, calls := fakeVSCode(t, "exit 0\n")
	path := filepath.Join(home, ".config")
	if err := os.Symlink(path, path); err != nil {
		t.Fatal(err)
	}
	if err := SetupVSCode(context.Background(), nil); err == nil {
		t.Fatal("settings symlink-loop stat failure reported success")
	}
	assertCalls(t, calls)
	assertVSCodeUnmarked(t, home)
}

func TestSetupVSCodePropagatesSettingsPermissionFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory access permissions")
	}
	home, calls := fakeVSCode(t, "exit 0\n")
	dir := filepath.Join(home, ".config")
	if err := os.Mkdir(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := SetupVSCode(context.Background(), nil); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("settings access error = %v, want permission denied", err)
	}
	assertCalls(t, calls)
	assertVSCodeUnmarked(t, home)
}

func TestSetupVSCodePropagatesMarkerReadAndShapeFailures(t *testing.T) {
	for _, content := range []string{"directory", "{", "{} {}", "null", `{"version":false}`, `{"version":{"user":[]}}`} {
		t.Run(content, func(t *testing.T) {
			home, calls := fakeVSCode(t, "exit 0\n")
			marker := filepath.Join(home, ".local", "share", "ublue", "setup_versioning.json")
			if content == "directory" {
				if err := os.MkdirAll(marker, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				writeVSCodeFixture(t, marker, content)
			}
			if err := SetupVSCode(context.Background(), nil); err == nil {
				t.Fatal("unreadable/invalid version state reported success")
			}
			assertCalls(t, calls)
			if _, err := os.Stat(filepath.Join(home, ".config")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed marker read seeded settings: %v", err)
			}
		})
	}
}

func TestSetupVSCodePreservesOtherVersionEntriesWhenCompleting(t *testing.T) {
	home, _ := fakeVSCode(t, "exit 0\n")
	marker := filepath.Join(home, ".local", "share", "ublue", "setup_versioning.json")
	writeVSCodeFixture(t, marker, `{"other":9007199254740993,"version":{"user":{"vscode":"0","theming":"7"},"system":{"tailscale":"2"}}}`)
	if err := SetupVSCode(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil || string(got["other"]) != "9007199254740993" {
		t.Fatalf("unrelated JSON number changed: %s, %v", data, err)
	}
	var versions map[string]map[string]string
	if err := json.Unmarshal(got["version"], &versions); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]string{"user": {"vscode": "1", "theming": "7"}, "system": {"tailscale": "2"}}
	if !reflect.DeepEqual(versions, want) {
		t.Fatalf("version state = %v, want %v", versions, want)
	}
}

func TestSetupVSCodeCancellationLeavesNoCompletionMarker(t *testing.T) {
	for _, cancelAfterStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-setup", true: "before-extension"}[cancelAfterStart], func(t *testing.T) {
			home, calls := fakeVSCode(t, "exit 0\n")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !cancelAfterStart {
				cancel()
			}
			if err := SetupVSCode(ctx, func(string) { cancel() }); err == nil {
				t.Fatal("canceled setup reported completion")
			}
			assertVSCodeUnmarked(t, home)
			assertCalls(t, calls)
		})
	}
}

func TestSetupVSCodeDryRunCreatesNoArtifactsOrExtensionProcesses(t *testing.T) {
	home, calls := fakeVSCode(t, "exit 99\n")
	dryrun.Set(true)
	if err := SetupVSCode(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("dry-run created user artifacts: %v, %v", entries, err)
	}
	assertCalls(t, calls)
}
