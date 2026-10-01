package troubleshoot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func TestSetupPreservesExistingConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, _ := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("GOOSE_PROVIDER: anthropic\nGOOSE_MODEL: existing\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dryrun.Set(false) })
	for _, preview := range []bool{false, true} {
		dryrun.Set(preview)
		err := defaultRunSetup()
		if preview && err != nil {
			t.Fatalf("preservation preview failed: %v", err)
		}
		if !preview && !errors.Is(err, os.ErrExist) {
			t.Fatalf("existing config: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Fatalf("existing configuration changed: %q, %v", got, err)
		}
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
	runSetup = defaultRunSetup
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
	defaultConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	want := []byte(freshConfig)
	if err := os.WriteFile(defaultConfigPath, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := defaultRunSetup(); err != nil {
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
			if err := defaultRunSetup(); err != nil {
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
			if err := defaultRunSetup(); err == nil {
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
	if err := defaultRunSetup(); err != nil {
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
	if err := defaultRunSetup(); err == nil {
		t.Fatal("invalid shipped configuration was accepted")
	}
	path, _ := ConfigPath()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid configuration was installed: %v", err)
	}
}

func TestErrorPrefersCommandOutput(t *testing.T) {
	err := &Error{Message: "no provider configured", Err: errors.New("exit status 3")}

	if got := err.Error(); got != "no provider configured" {
		t.Errorf("Error() = %q, want the command output", got)
	}
}

func TestErrorFallsBackToWrappedError(t *testing.T) {
	// A command that fails silently leaves Message empty; the user must still
	// see something.
	err := &Error{Err: errors.New("exit status 3")}

	if got := err.Error(); got != "exit status 3" {
		t.Errorf("Error() = %q, want the wrapped error", got)
	}
}

func TestErrorUnwrap(t *testing.T) {
	wrapped := errors.New("exit status 3")
	err := &Error{Message: "output", Err: wrapped}

	if !errors.Is(err, wrapped) {
		t.Errorf("errors.Is did not find the wrapped error through Unwrap")
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
	lookPath, readConfig, tapPackage, installPackage, runSetup = defaultLookPath, defaultReadConfig, homebrew.Tap, homebrew.Install, defaultRunSetup
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
