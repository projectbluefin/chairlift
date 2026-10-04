package troubleshoot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"gopkg.in/yaml.v3"
)

func TestConfigPathUsesUserConfigDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	got, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	want := filepath.Join(base, "goose", "config.yaml")
	if got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
}

func TestConfigPathErrorsWithoutAHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("UserConfigDir only fails this way on unix")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if _, err := ConfigPath(); err == nil {
		t.Fatal("ConfigPath succeeded with neither XDG_CONFIG_HOME nor HOME set")
	}
}

func TestDefaultReadConfigReadsGooseConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := os.MkdirAll(filepath.Join(base, "goose"), 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "goose", "config.yaml"), []byte(freshConfig), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	data, err := defaultReadConfig()
	if err != nil {
		t.Fatalf("defaultReadConfig: %v", err)
	}
	// The read is what Detect feeds to ParseConfig, so assert on the fact
	// that survives the round trip rather than on the bytes.
	if state := ParseConfig(data); !state.Wired || state.Provider != "gemini-cli" {
		t.Errorf("round trip = %+v, want Wired with provider gemini-cli", state)
	}
}

func TestDefaultReadConfigErrorsWhenAbsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, err := defaultReadConfig(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("defaultReadConfig error = %v, want os.ErrNotExist", err)
	}
}

func TestDefaultReadConfigPropagatesConfigPathError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("UserConfigDir only fails this way on unix")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if _, err := defaultReadConfig(); err == nil {
		t.Fatal("defaultReadConfig succeeded with no resolvable config dir")
	}
}

func TestSetupConnectsExistingConfigurationWithoutChangingUserSettings(t *testing.T) {
	t.Cleanup(func() { dryrun.Set(false) })
	for _, key := range []string{"", "linux-tools", "linux-mcp-server"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path, _ := ConfigPath()
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			original := "# User preferences\nGOOSE_PROVIDER: anthropic\nGOOSE_MODEL: existing\nCUSTOM_SETTING: keep-me\nextensions:\n  other:\n    type: builtin\n    enabled: true\n    name: developer\n"
			if key != "" {
				original += fmt.Sprintf("  %s:\n    type: stdio\n    cmd: linux-mcp-server\n    enabled: false\n    args: []\n    timeout: 123\n", key)
			}
			if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			dryrun.Set(true)
			if err := EnsureDiagnosticsConfigured(); err != nil {
				t.Fatalf("preview: %v", err)
			}
			preview, _ := os.ReadFile(path)
			if string(preview) != original {
				t.Fatal("preview changed user settings")
			}
			dryrun.Set(false)
			if err := EnsureDiagnosticsConfigured(); err != nil {
				t.Fatalf("connect existing config: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !ParseConfig(got).Wired {
				t.Fatalf("Linux tools still disconnected: %v", err)
			}
			var before, after map[string]any
			if err := yaml.Unmarshal([]byte(original), &before); err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(got, &after); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"GOOSE_PROVIDER", "GOOSE_MODEL", "CUSTOM_SETTING"} {
				if after[field] != before[field] {
					t.Fatalf("setup replaced %s", field)
				}
			}
			if !reflect.DeepEqual(after["extensions"].(map[string]any)["other"], before["extensions"].(map[string]any)["other"]) {
				t.Fatal("setup changed another extension")
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("configuration permissions = %o", info.Mode().Perm())
			}
			if err := EnsureDiagnosticsConfigured(); err != nil {
				t.Fatalf("repeat connection: %v", err)
			}
			repeated, _ := os.ReadFile(path)
			if string(repeated) != string(got) {
				t.Fatal("repeat setup rewrote the connected file")
			}
		})
	}
}

func TestSetupRefusesConflictingConfigurationWithoutDataLoss(t *testing.T) {
	for _, config := range []string{
		"extensions: [broken",
		"extensions: {}\nextensions: {}\n",
		"extensions: {}\n---\nGOOSE_PROVIDER: other\n",
		"extensions: [linux-tools]\n",
		"defaults: &defaults\n  extensions:\n    developer:\n      type: builtin\n<<: *defaults\nGOOSE_PROVIDER: existing\n",
		"defaults: &defaults\n  developer:\n    type: builtin\nextensions:\n  <<: *defaults\n",
		"extensions:\n  linux-tools:\n    type: http\n    uri: https://example.test\n",
		"extensions:\n  linux-tools:\n    type: stdio\n    cmd: another-server\n",
		"extensions:\n  linux-tools:\n    type: stdio\n    cmd: linux-mcp-server\n    args: [--toolset, BOTH]\n",
		"extensions:\n  linux-tools:\n    type: stdio\n    cmd: linux-mcp-server\n    envs: {LINUX_MCP_SSH_KEY_PATH: /private/key}\n",
	} {
		t.Run(config, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path, _ := ConfigPath()
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := EnsureDiagnosticsConfigured(); err == nil {
				t.Fatal("conflicting configuration was replaced")
			}
			got, _ := os.ReadFile(path)
			if string(got) != config {
				t.Fatal("refused setup changed user data")
			}
		})
	}
}

func TestSetupRefusesConfigurationSymlinks(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, _ := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "config.yaml")
	original := "GOOSE_PROVIDER: existing\n"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDiagnosticsConfigured(); err == nil {
		t.Fatal("setup followed a configuration symlink")
	}
	got, _ := os.ReadFile(target)
	if string(got) != original {
		t.Fatal("setup changed the link target")
	}
}

func TestSetupUsesExistingConfigAfterInstallingMissingTools(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, _ := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(freshConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{"goose-desktop": true}
	stubEnvironment(t, freshConfig, present)
	runSetup = EnsureDiagnosticsConfigured
	installPackage = func(string, bool) error {
		present["linux-mcp-server"], present["goose"] = true, true
		return nil
	}
	after, err := Setup(State{DesktopInstalled: true}, nil)
	if err != nil || !after.Ready() {
		t.Fatalf("restored configuration: %+v, %v", after, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != freshConfig {
		t.Fatal("existing configuration was modified")
	}
}

func TestSetupCopiesTheShippedConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previous := defaultConfigPath
	t.Cleanup(func() { defaultConfigPath = previous; dryrun.Set(false) })
	bin := t.TempDir()
	exe := filepath.Join(bin, "linux-mcp-server")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	defaultConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	want := []byte(strings.Replace(freshConfig, "/home/linuxbrew/.linuxbrew/bin/linux-mcp-server", exe, 1))
	if err := os.WriteFile(defaultConfigPath, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDiagnosticsConfigured(); err != nil {
		t.Fatal(err)
	}
	path, _ := ConfigPath()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatalf("premade configuration: %q, %v", got, err)
	}
}

func TestSetupPinsDiagnosticPolicyInNewPremadeConfig(t *testing.T) {
	previous := defaultConfigPath
	t.Cleanup(func() { defaultConfigPath = previous })
	for _, key := range []string{"linux-mcp-server", "linux-tools"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			defaultConfigPath = filepath.Join(t.TempDir(), "config.yaml")
			data := []byte(fmt.Sprintf("GOOSE_PROVIDER: existing-provider\nGOOSE_MODEL: existing-model\nextensions:\n  %s:\n    cmd: linux-mcp-server\n    type: stdio\n    enabled: true\n    args: []\n    envs: {}\n  other:\n    cmd: other-server\n    type: stdio\n    enabled: false\n", key))
			if err := os.WriteFile(defaultConfigPath, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := EnsureDiagnosticsConfigured(); err != nil {
				t.Fatalf("shipped default preset could not be set up: %v", err)
			}
			path, _ := ConfigPath()
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if state := ParseConfig(got); !state.Wired || state.Provider != "existing-provider" {
				t.Fatalf("new preset = %+v, want fixed diagnostics without changing provider", state)
			}
			var document map[string]any
			if err := yaml.Unmarshal(got, &document); err != nil {
				t.Fatal(err)
			}
			if document["GOOSE_MODEL"] != "existing-model" || document["extensions"].(map[string]any)["other"] == nil {
				t.Fatal("setup dropped the model or an unrelated extension")
			}
		})
	}
}

func TestSetupDoesNotReplaceExplicitUnsafePremadePolicy(t *testing.T) {
	previous := defaultConfigPath
	t.Cleanup(func() { defaultConfigPath = previous })
	for _, key := range []string{"linux-mcp-server", "linux-tools"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			defaultConfigPath = filepath.Join(t.TempDir(), "config.yaml")
			data := []byte(fmt.Sprintf("extensions:\n  %s:\n    cmd: linux-mcp-server\n    type: stdio\n    args: [--toolset, BOTH]\n", key))
			if err := os.WriteFile(defaultConfigPath, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := EnsureDiagnosticsConfigured(); err == nil {
				t.Fatal("unsafe explicit policy was silently replaced")
			}
			path, _ := ConfigPath()
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe preset was written: %v", err)
			}
		})
	}
}

func TestSetupPreviewDoesNotWriteConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	if err := EnsureDiagnosticsConfigured(); err != nil {
		t.Fatal(err)
	}
	path, _ := ConfigPath()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote configuration: %v", err)
	}
}

func TestSetupRejectsInvalidPremadeConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previous := defaultConfigPath
	t.Cleanup(func() { defaultConfigPath = previous })
	defaultConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(defaultConfigPath, []byte("extensions: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDiagnosticsConfigured(); err == nil {
		t.Fatal("invalid shipped configuration was accepted")
	}
	path, _ := ConfigPath()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid configuration was installed: %v", err)
	}
}

// setupPipeline uses actual process execution and filesystem effects. Its brew
// fixture cannot install anything until the required tap has been added.
func setupPipeline(t *testing.T) string {
	t.Helper()
	prefix := t.TempDir()
	bin := filepath.Join(prefix, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(prefix, "config"))
	t.Setenv("HOME", prefix)
	t.Setenv("GOOSE_TEST_PREFIX", prefix)
	stub := filepath.Join(prefix, "tool")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	brew := `#!/bin/sh
case "$*" in
  "tap ublue-os/tap") /usr/bin/touch "$GOOSE_TEST_PREFIX/tapped" ;;
  "install ublue-os/tap/linux-mcp-server")
    test -f "$GOOSE_TEST_PREFIX/tapped" || exit 2
    /usr/bin/cp "$GOOSE_TEST_PREFIX/tool" "$GOOSE_TEST_PREFIX/bin/linux-mcp-server"
    /usr/bin/cp "$GOOSE_TEST_PREFIX/tool" "$GOOSE_TEST_PREFIX/bin/goose" ;;
  "install --cask ublue-os/tap/goose-linux")
    test -f "$GOOSE_TEST_PREFIX/tapped" || exit 2
    /usr/bin/cp "$GOOSE_TEST_PREFIX/tool" "$GOOSE_TEST_PREFIX/bin/goose-desktop" ;;
  *) exit 3 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "brew"), []byte(brew), 0o755); err != nil {
		t.Fatal(err)
	}
	oldLook, oldRead, oldTap, oldInstall, oldSetup, oldDefault := lookPath, readConfig, tapPackage, installPackage, runSetup, defaultConfigPath
	t.Cleanup(func() {
		lookPath, readConfig, tapPackage, installPackage, runSetup, defaultConfigPath = oldLook, oldRead, oldTap, oldInstall, oldSetup, oldDefault
		dryrun.Set(false)
	})
	lookPath, readConfig, tapPackage, installPackage, runSetup = defaultLookPath, defaultReadConfig, homebrew.Tap, homebrew.Install, EnsureDiagnosticsConfigured
	dryrun.Set(false)
	defaultConfigPath = filepath.Join(prefix, "preset.yaml")
	preset := fmt.Sprintf("extensions:\n  linux-mcp-server:\n    type: stdio\n    cmd: %q\n    args: [--toolset, FIXED, --no-search-for-ssh-key, --verify-host-keys]\n", filepath.Join(bin, "linux-mcp-server"))
	if err := os.WriteFile(defaultConfigPath, []byte(preset), 0o600); err != nil {
		t.Fatal(err)
	}
	return prefix
}

func TestStepsDispatchTheRightPackages(t *testing.T) {
	setupPipeline(t)
	after, err := Setup(State{}, nil)
	if err != nil || !after.Ready() || !after.DesktopInstalled {
		t.Fatalf("fresh package setup: %+v, %v", after, err)
	}
}

func TestStepsAlwaysTap(t *testing.T) {
	prefix := setupPipeline(t)
	after, err := Setup(State{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(prefix, "tapped")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(after, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("repeat setup did not restore the tap: %v", err)
	}
}

func TestSetupSkipsStepsThatAreAlreadyDone(t *testing.T) {
	stubEnvironment(t, "", map[string]bool{"linux-mcp-server": true, "goose": true, "goose-desktop": true})
	installs, connections := 0, 0
	installPackage = func(string, bool) error {
		installs++
		return nil
	}
	runSetup = func() error {
		connections++
		readConfig = func() ([]byte, error) { return []byte(freshConfig), nil }
		return nil
	}
	state := State{ServerInstalled: true, AgentInstalled: true, DesktopInstalled: true}
	after, err := Setup(state, nil)
	if err != nil || !after.Ready() {
		t.Fatalf("connecting installed tools: %+v, %v", after, err)
	}
	if installs != 0 || connections != 1 {
		t.Fatalf("setup performed %d installs and %d connections; want no installs and one connection", installs, connections)
	}
	if _, err := Setup(after, nil); err != nil {
		t.Fatal(err)
	}
	if installs != 0 || connections != 1 {
		t.Fatalf("ready tools were reinstalled or reconnected: installs=%d connections=%d", installs, connections)
	}
}

func TestConfigUpdateRefusesConcurrentUserEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("GOOSE_PROVIDER: existing\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	_, snapshot, err := readUserConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("GOOSE_PROVIDER: changed-by-goose\n")
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeUserConfig(path, original, snapshot, []byte(premadeConfig)); err == nil {
		t.Fatal("setup replaced a concurrent user edit")
	}
	got, _ := os.ReadFile(path)
	if !reflect.DeepEqual(got, changed) {
		t.Fatal("setup lost the new user settings")
	}
}

func TestFreshConfigWriteDoesNotReplaceANewUserFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("GOOSE_PROVIDER: created-by-goose\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeUserConfig(path, nil, nil, []byte(premadeConfig)); !errors.Is(err, os.ErrExist) {
		t.Fatalf("concurrent creation error = %v", err)
	}
	got, _ := os.ReadFile(path)
	if !reflect.DeepEqual(got, original) {
		t.Fatal("setup replaced the newly created file")
	}
}

func TestExistingSetupPipelineActuallyConnectsLinuxTools(t *testing.T) {
	setupPipeline(t)
	installed, err := Setup(State{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := ConfigPath()
	user := []byte("GOOSE_PROVIDER: existing-provider\nGOOSE_MODEL: existing-model\nextensions:\n  developer:\n    type: builtin\n    enabled: true\n")
	if err := os.WriteFile(path, user, 0o600); err != nil {
		t.Fatal(err)
	}
	installed.Wired = false
	after, err := Setup(installed, nil)
	if err != nil || !after.Ready() || after.Provider != "existing-provider" || !after.ModelSelected {
		t.Fatalf("installed-but-unwired pipeline: %+v, %v", after, err)
	}
	got, _ := os.ReadFile(path)
	var document map[string]any
	if err := yaml.Unmarshal(got, &document); err != nil {
		t.Fatal(err)
	}
	if document["GOOSE_MODEL"] != "existing-model" || document["extensions"].(map[string]any)["developer"] == nil {
		t.Fatal("connecting diagnostics lost the user's Goose settings")
	}
}

func TestSetupRefusesSharedDiagnosticMappings(t *testing.T) {
	for _, key := range diagnosticExtensionKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path, _ := ConfigPath()
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			original := []byte(fmt.Sprintf("extensions:\n  %s: &diagnostics\n    cmd: linux-mcp-server\n    type: stdio\n    enabled: false\n    args: []\n  other: *diagnostics\n", key))
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := EnsureDiagnosticsConfigured(); err == nil {
				t.Fatal("repair changed an extension shared with unrelated settings")
			}
			got, _ := os.ReadFile(path)
			if !reflect.DeepEqual(got, original) {
				t.Fatal("refused shared mapping changed the user's file")
			}
		})
	}
}

func TestSetupRepairsStaleDiagnosticExecutable(t *testing.T) {
	setupPipeline(t)
	if _, err := Setup(State{}, nil); err != nil {
		t.Fatal(err)
	}
	path, _ := ConfigPath()
	data := []byte("GOOSE_PROVIDER: existing-provider\nextensions:\n  linux-tools:\n    type: stdio\n    cmd: /missing/old-cellar/bin/linux-mcp-server\n    args: [--toolset, FIXED, --no-search-for-ssh-key, --verify-host-keys]\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if Detect().Wired {
		t.Fatal("missing executable was reported connected")
	}
	if err := EnsureDiagnosticsConfigured(); err != nil {
		t.Fatal(err)
	}
	after := Detect()
	if !after.Wired || after.Provider != "existing-provider" {
		t.Fatalf("stale diagnostic repair: %+v", after)
	}
}

func TestBackupPathAppendsSuffix(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	got, err := BackupPath()
	if err != nil {
		t.Fatalf("BackupPath: %v", err)
	}
	want := filepath.Join(base, "goose", "config.yaml.chairlift-backup")
	if got != want {
		t.Errorf("BackupPath = %q, want %q", got, want)
	}
}

func TestBackupCreatedOnRepairOfExistingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "# Custom user configuration\nGOOSE_PROVIDER: anthropic\nGOOSE_MODEL: claude-3-opus\nCUSTOM_KEY: custom-val\nextensions:\n  other:\n    type: builtin\n    enabled: true\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := BackupPath()
	if err != nil {
		t.Fatal(err)
	}

	if err := EnsureDiagnosticsConfigured(); err != nil {
		t.Fatalf("repairing config: %v", err)
	}

	bInfo, err := os.Stat(backup)
	if err != nil {
		t.Fatalf("backup file was not created: %v", err)
	}
	if bInfo.Mode().Perm() != 0o600 {
		t.Errorf("backup permissions = %o, want 0600", bInfo.Mode().Perm())
	}
	bContent, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(bContent) != original {
		t.Errorf("backup content = %q, want %q", string(bContent), original)
	}

	repaired, err := os.ReadFile(path)
	if err != nil || !ParseConfig(repaired).Wired {
		t.Fatalf("repaired config is not wired: %v", err)
	}
}

func TestBackupOverwritesPreviousBackupOnSubsequentRepair(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, _ := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	backup, _ := BackupPath()
	if err := os.WriteFile(backup, []byte("stale-backup-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	original := "GOOSE_PROVIDER: custom\nextensions:\n  linux-tools:\n    type: stdio\n    cmd: linux-mcp-server\n    enabled: false\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDiagnosticsConfigured(); err != nil {
		t.Fatalf("repairing config: %v", err)
	}

	bContent, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(bContent) != original {
		t.Errorf("overwritten backup content = %q, want %q", string(bContent), original)
	}
}

func TestNoBackupWhenNothingChanges(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	oldLook := lookPath
	t.Cleanup(func() { lookPath = oldLook })
	lookPath = func(string) bool { return true }
	path, _ := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(freshConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, _ := BackupPath()

	if err := EnsureDiagnosticsConfigured(); err != nil {
		t.Fatalf("EnsureDiagnosticsConfigured: %v", err)
	}

	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("backup exists when nothing changed: %v", err)
	}
}

func TestNoBackupOnFreshCreate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previous := defaultConfigPath
	t.Cleanup(func() { defaultConfigPath = previous })

	bin := t.TempDir()
	exe := filepath.Join(bin, "linux-mcp-server")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	defaultConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	premade := strings.Replace(freshConfig, "/home/linuxbrew/.linuxbrew/bin/linux-mcp-server", exe, 1)
	if err := os.WriteFile(defaultConfigPath, []byte(premade), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDiagnosticsConfigured(); err != nil {
		t.Fatalf("EnsureDiagnosticsConfigured: %v", err)
	}

	backup, _ := BackupPath()
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("backup exists on fresh creation: %v", err)
	}

	path, _ := ConfigPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fresh config was not created: %v", err)
	}
}

func TestNoBackupUnderDryRun(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, _ := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "GOOSE_PROVIDER: custom\nextensions:\n  other:\n    type: builtin\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	if err := EnsureDiagnosticsConfigured(); err != nil {
		t.Fatalf("dry run EnsureDiagnosticsConfigured: %v", err)
	}

	backup, _ := BackupPath()
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("backup exists under dry-run: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("config modified under dry-run: got %q, want %q", string(got), original)
	}
}

func TestFailureBeforeReplaceLeavesOriginalIntact(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, _ := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "# Original content\nGOOSE_PROVIDER: preserved\nextensions:\n  other:\n    type: builtin\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, _ := BackupPath()
	if err := os.MkdirAll(backup, 0o700); err != nil {
		t.Fatal(err)
	}

	err := EnsureDiagnosticsConfigured()
	if err == nil {
		t.Fatal("EnsureDiagnosticsConfigured succeeded when backup path was a directory")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading original config: %v", readErr)
	}
	if string(got) != original {
		t.Fatalf("original config changed after failure: got %q, want %q", string(got), original)
	}
}
