package troubleshoot

import (
	"errors"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"strings"
	"testing"
)

// The configuration goose-mcp-setup writes on a fresh system.
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
`

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name         string
		data         string
		wantWired    bool
		wantProvider string
	}{
		{
			name:         "what the setup script writes",
			data:         freshConfig,
			wantWired:    true,
			wantProvider: "gemini-cli",
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
			name:      "extension added by hand under another key order",
			data:      "extensions:\n  something:\n    name: linux-tools\n",
			wantWired: true,
		},
		{
			name: "empty file",
			data: "",
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
			// The state goose-mcp-setup leaves behind when a config already
			// existed: packages installed, nothing connected.
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
func stubEnvironment(t *testing.T, config string, present map[string]bool) *[]string {
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

	lookPath = func(name string) bool { return present[name] }
	readConfig = func() ([]byte, error) { return []byte(config), nil }

	ran := []string{}
	runSetup = func() error {
		ran = append(ran, setupCommand)
		present["linux-mcp-server"] = true
		return nil
	}
	return &ran
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

func TestSetupSkipsStepsThatAreAlreadyDone(t *testing.T) {
	// A host where everything is installed but the extension was never
	// wired in — the case that matters, since the packages give no clue.
	present := map[string]bool{
		"linux-mcp-server": true,
		"goose":            true,
		"goose-desktop":    true,
	}
	ran := stubEnvironment(t, "GOOSE_PROVIDER: anthropic\n", present)

	var steps []string
	state := State{ServerInstalled: true, AgentInstalled: true, DesktopInstalled: true}
	if _, err := Setup(state, func(name string) { steps = append(steps, name) }); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// The tap step always runs; the three installs are skipped; the wiring
	// step runs because the configuration does not reference linux-tools.
	if len(steps) != 2 {
		t.Fatalf("ran %v, want the tap and the wiring step only", steps)
	}
	if !strings.Contains(steps[1], "Connecting") {
		t.Errorf("second step = %q, want the wiring step", steps[1])
	}
	if len(*ran) != 1 {
		t.Errorf("setup script ran %d times, want 1", len(*ran))
	}
}

func TestSetupNamesTheStepThatFailed(t *testing.T) {
	stubEnvironment(t, "", map[string]bool{"linux-mcp-server": true, "goose": true, "goose-desktop": true})
	previousSetup := runSetup
	t.Cleanup(func() { runSetup = previousSetup })
	runSetup = func() error { return errors.New("permission denied") }

	state := State{ServerInstalled: true, AgentInstalled: true, DesktopInstalled: true}
	_, err := Setup(state, nil)
	if err == nil {
		t.Fatal("Setup succeeded with a failing setup script")
	}
	if !strings.Contains(err.Error(), "connecting it to this system") {
		t.Errorf("error does not name the failing step: %v", err)
	}
}

// Setup must report the state it actually left behind, not the one it was
// aiming for: goose-mcp-setup exits 0 without writing anything when a
// configuration already exists.
func TestSetupReturnsTheStateItActuallyLeft(t *testing.T) {
	present := map[string]bool{"linux-mcp-server": true, "goose": true, "goose-desktop": true}
	stubEnvironment(t, "GOOSE_PROVIDER: anthropic\n", present)
	runSetup = func() error { return nil } // exits 0, changes nothing

	after, err := Setup(State{ServerInstalled: true, AgentInstalled: true, DesktopInstalled: true}, nil)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if after.Ready() {
		t.Error("Setup reported the feature ready after a no-op setup script")
	}
}

func TestDryRunRunsNothing(t *testing.T) {
	previousSetup := runSetup
	t.Cleanup(func() { runSetup = previousSetup; dryrun.Set(false) })
	runSetup = defaultRunSetup
	dryrun.Set(true)

	if err := runSetup(); err != nil {
		t.Fatalf("dry-run setup: %v", err)
	}
}
