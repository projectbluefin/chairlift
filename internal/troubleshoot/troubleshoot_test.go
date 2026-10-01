package troubleshoot

import (
	"errors"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"path/filepath"
	"strings"
	"testing"
)

// A configured user may retain their own provider alongside fixed diagnostics.
const freshConfig = `GEMINI_CLI_COMMAND: gemini
GOOSE_PROVIDER: gemini-cli
GOOSE_MODEL: gemini-3-flash-preview
extensions:
  linux-tools:
    enabled: true
    type: stdio
    name: linux-tools
    description: Linux system administration and diagnostics
    cmd: /home/linuxbrew/.linuxbrew/bin/linux-mcp-server
    args: [--toolset, FIXED, --no-search-for-ssh-key, --verify-host-keys]
`

const premadeConfig = `# Bluefin default Goose configuration; select a model provider in Goose.
# Keep the prefix/bin symlink; a resolved Cellar path expires after an upgrade.
extensions:
  linux-mcp-server:
    args:
      - --toolset
      - FIXED
      - --no-search-for-ssh-key
      - --verify-host-keys
    bundled: false
    cmd: /home/linuxbrew/.linuxbrew/bin/linux-mcp-server
    enabled: true
    envs: {}
    name: linux-mcp-server
    timeout: 300
    type: stdio
`

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name         string
		data         string
		wantWired    bool
		wantProvider string
	}{
		{
			name:         "configured user with legacy extension key",
			data:         freshConfig,
			wantWired:    true,
			wantProvider: "gemini-cli",
		},
		{
			name:      "Common premade configuration",
			data:      premadeConfig,
			wantWired: true,
		},
		{
			name:         "unrestricted extension is not ready",
			data:         strings.Replace(freshConfig, "    args: [--toolset, FIXED, --no-search-for-ssh-key, --verify-host-keys]\n", "", 1),
			wantWired:    false,
			wantProvider: "gemini-cli",
		},
		{
			name:         "script toolset is not ready",
			data:         strings.Replace(freshConfig, "FIXED", "BOTH", 1),
			wantWired:    false,
			wantProvider: "gemini-cli",
		},
		{
			name:         "wrong transport is not ready",
			data:         strings.Replace(freshConfig, "type: stdio", "type: http", 1),
			wantWired:    false,
			wantProvider: "gemini-cli",
		},
		{
			name:      "partial YAML type failure is not ready",
			data:      strings.Replace(freshConfig, "GOOSE_PROVIDER: gemini-cli", "GOOSE_PROVIDER: [invalid]", 1),
			wantWired: false,
		},
		{
			// The common case the setup script refuses to touch: a user who
			// already ran `goose configure`. The extension is absent, so the
			// feature is not wired up however many packages are installed.
			name:         "configured by hand, no linux-tools",
			data:         "GOOSE_PROVIDER: anthropic\nGOOSE_MODEL: claude-sonnet-4\n",
			wantWired:    false,
			wantProvider: "anthropic",
		},
		{
			// The bug this fixes: the old line scan matched a `name: linux-tools`
			// field anywhere, so an extension under a different key counted as
			// wired. Structurally it is not the linux-tools extension, so it is
			// not wired.
			name:      "linux-tools name under a different extension key",
			data:      "extensions:\n  something:\n    name: linux-tools\n",
			wantWired: false,
		},
		{
			name: "empty file",
			data: "",
		},
		{
			// An explicitly disabled extension is present but not usable.
			name:      "linux-tools present but disabled",
			data:      "extensions:\n  linux-tools:\n    enabled: false\n    type: stdio\n    cmd: /usr/bin/linux-mcp-server\n",
			wantWired: false,
		},
		{
			// Present and enabled but missing the command that makes it run is
			// not a usable extension.
			name:      "linux-tools enabled but no command",
			data:      "extensions:\n  linux-tools:\n    enabled: true\n    type: stdio\n",
			wantWired: false,
		},
		{
			// Issue #57's literal repro: a stray top-level key that merely
			// shares the extension's name wires up nothing.
			name:         "stray top-level linux-tools key",
			data:         "GOOSE_PROVIDER: anthropic\nlinux-tools: disabled\n",
			wantWired:    false,
			wantProvider: "anthropic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := ParseConfig([]byte(tt.data))

			if state.Wired != tt.wantWired {
				t.Errorf("Wired = %v, want %v", state.Wired, tt.wantWired)
			}
			if state.Provider != tt.wantProvider {
				t.Errorf("Provider = %q, want %q", state.Provider, tt.wantProvider)
			}
		})
	}
}

func TestReadyNeedsEveryPiece(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  bool
	}{
		{
			name:  "everything present",
			state: State{ServerInstalled: true, AgentInstalled: true, Wired: true},
			want:  true,
		},
		{
			// Installed tools are not usable without a diagnostic extension.
			name:  "installed but not connected",
			state: State{ServerInstalled: true, AgentInstalled: true},
		},
		{
			name:  "connected but the server is gone",
			state: State{AgentInstalled: true, Wired: true},
		},
		{
			// The desktop app is a convenience for launching, not a
			// requirement for the feature to work.
			name:  "no desktop app",
			state: State{ServerInstalled: true, AgentInstalled: true, Wired: true},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.Ready(); got != tt.want {
				t.Errorf("Ready() = %v, want %v", got, tt.want)
			}
		})
	}
}

// stubEnvironment points the package at a fake host.
func stubEnvironment(t *testing.T, config string, present map[string]bool) {
	t.Helper()

	previousLook, previousRead, previousSetup := lookPath, readConfig, runSetup
	previousTap, previousInstall := tapPackage, installPackage
	t.Cleanup(func() {
		lookPath, readConfig, runSetup = previousLook, previousRead, previousSetup
		tapPackage, installPackage = previousTap, previousInstall
		dryrun.Set(false)
	})

	// Never reach real brew: a test that did would tap a repository on the
	// machine running it.
	tapPackage = func(string) error { return nil }
	installPackage = func(string, bool) error { return nil }

	lookPath = func(name string) bool { return present[filepath.Base(name)] }
	readConfig = func() ([]byte, error) { return []byte(config), nil }

	runSetup = func() error { return nil }
}

func TestDetectCombinesConfigAndBinaries(t *testing.T) {
	stubEnvironment(t, freshConfig, map[string]bool{
		"linux-mcp-server": true,
		"goose":            true,
	})

	state := Detect()

	if !state.Ready() {
		t.Errorf("Ready() = false for a fully set-up host: %+v", state)
	}
	if state.DesktopInstalled {
		t.Error("DesktopInstalled = true with no goose-desktop on PATH")
	}
	if state.Provider != "gemini-cli" {
		t.Errorf("Provider = %q", state.Provider)
	}
}

func TestDetectTreatsAMissingConfigAsNotWired(t *testing.T) {
	previousRead := readConfig
	t.Cleanup(func() { readConfig = previousRead })
	readConfig = func() ([]byte, error) { return nil, errors.New("no such file") }

	if Detect().Wired {
		t.Error("Wired = true with no configuration file")
	}
}

func TestSetupPreservesTheOriginalFailure(t *testing.T) {
	stubEnvironment(t, "", map[string]bool{"linux-mcp-server": true, "goose": true, "goose-desktop": true})
	failure := errors.New("permission denied")
	runSetup = func() error { return failure }

	state := State{ServerInstalled: true, AgentInstalled: true, DesktopInstalled: true}
	_, err := Setup(state, nil)
	if err == nil {
		t.Fatal("Setup succeeded with a failing setup script")
	}
	if !errors.Is(err, failure) {
		t.Errorf("setup lost the original failure: %v", err)
	}
	if !strings.Contains(err.Error(), "connecting it to this system") {
		t.Errorf("setup failure does not identify the failed step: %v", err)
	}
}

// Setup reports the observed result, not success inferred from a setup callback.
func TestSetupReturnsTheStateItActuallyLeft(t *testing.T) {
	present := map[string]bool{"linux-mcp-server": true, "goose": true, "goose-desktop": true}
	stubEnvironment(t, "GOOSE_PROVIDER: anthropic\n", present)
	runSetup = func() error { return nil } // exits 0, changes nothing

	after, err := Setup(State{ServerInstalled: true, AgentInstalled: true, DesktopInstalled: true}, nil)
	if err == nil {
		t.Fatal("Setup did not report the incomplete connection")
	}
	if after.Ready() {
		t.Error("Setup reported the feature ready after a no-op setup script")
	}
}

func TestDetectRejectsAnUnavailableConfiguredCommand(t *testing.T) {
	config := freshConfig + `
  linux-mcp-server:
    enabled: true
    type: stdio
    cmd: /missing/linux-mcp-server
    args: [--toolset, FIXED, --no-search-for-ssh-key, --verify-host-keys]
`
	stubEnvironment(t, config, map[string]bool{"linux-mcp-server": true, "goose": true})
	lookPath = func(name string) bool { return name != "/missing/linux-mcp-server" }
	if state := Detect(); state.Wired || state.Ready() {
		t.Fatalf("missing enabled extension was ignored: %+v", state)
	}
}

func TestModelSelectionDoesNotEstablishDiagnosticReadiness(t *testing.T) {
	selected := ParseConfig([]byte("GOOSE_PROVIDER: existing\nGOOSE_MODEL: existing-model\n"))
	if !selected.ModelSelected || selected.Wired || selected.Ready() {
		t.Fatal("a provider and model selection was mistaken for connected tools")
	}
	tools := ParseConfig([]byte(premadeConfig))
	if !tools.Wired || tools.Provider != "" || tools.ModelSelected {
		t.Fatal("diagnostic wiring invented a provider or model selection")
	}
}
