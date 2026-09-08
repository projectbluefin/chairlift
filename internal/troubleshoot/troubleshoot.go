// Package troubleshoot implements ChairLift's Enhanced Troubleshooting: an
// AI agent that can read this machine's live state — logs, services,
// processes, network — and answer questions about it.
//
// It is a port of Bluefin's `ujust probe` recipe
// (projectbluefin/dakota, files/just-overrides/default.just) into one row.
// The pieces are all Homebrew packages: `linux-mcp-server` from ublue-os/tap
// exposes the system as MCP tools, it depends on `block-goose-cli` for the
// agent itself, and the `goose-linux` cask from the same tap provides the
// desktop app ChairLift launches.
//
// Nothing here is privileged. Every package is a user-scope Homebrew
// install, the agent runs as the invoking user, and linux-mcp-server's
// access is read-only — the same reasoning that keeps Homebrew tap trust and
// gaming mode off the pkexec path.
package troubleshoot

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/homebrew"
)

// setupTimeout bounds goose-mcp-setup, which only writes a file.
const setupTimeout = 30 * time.Second

// The packages the feature is assembled from. Both live in ublue-os/tap,
// which brew requires be tapped explicitly before either can be installed
// by its qualified name.
const (
	Tap = "ublue-os/tap"
	// ServerFormula also pulls block-goose-cli, so installing it installs
	// the agent as well.
	ServerFormula = "ublue-os/tap/linux-mcp-server"
	// DesktopCask is the Goose desktop app. Its binary is `goose-desktop`,
	// distinct from the formula's `goose`, so the two coexist.
	DesktopCask = "ublue-os/tap/goose-linux"
	// DesktopFile is where the cask installs its launcher.
	DesktopFile = "Goose.desktop"
)

// setupCommand writes the linux-tools extension into Goose's configuration.
// It ships with linux-mcp-server rather than being ChairLift's own script.
const setupCommand = "goose-mcp-setup"

// State is what ChairLift knows about the feature on this host.
type State struct {
	// ServerInstalled reports whether linux-mcp-server is on $PATH.
	ServerInstalled bool
	// AgentInstalled reports whether the goose CLI is on $PATH.
	AgentInstalled bool
	// DesktopInstalled reports whether the Goose desktop app is available.
	DesktopInstalled bool
	// Wired reports whether Goose's configuration actually references the
	// linux-tools extension. This is the load-bearing check: goose-mcp-setup
	// exits 0 without changing anything when a configuration already
	// exists, so treating its success as "configured" would report a
	// feature that was never wired up.
	Wired bool
	// Provider is the LLM provider Goose is configured to use, empty when
	// none is set. ChairLift reads it and does not write it — the setup
	// script owns that file, and refuses to touch an existing one.
	Provider string
}

// Ready reports whether a session can be started.
func (s State) Ready() bool {
	return s.ServerInstalled && s.AgentInstalled && s.Wired
}

// ConfigPath returns Goose's configuration file.
func ConfigPath() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "goose", "config.yaml"), nil
}

// ParseConfig reads Goose's configuration for the two facts ChairLift needs.
//
// It deliberately does not decode the file as YAML. ChairLift neither owns
// nor rewrites this file; it only needs to know whether the linux-tools
// extension is present and which provider is set, and a line scan cannot
// corrupt a document written by another tool.
func ParseConfig(data []byte) State {
	var state State

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "linux-tools:"), trimmed == "name: linux-tools":
			state.Wired = true
		case strings.HasPrefix(trimmed, "GOOSE_PROVIDER:"):
			state.Provider = strings.TrimSpace(strings.TrimPrefix(trimmed, "GOOSE_PROVIDER:"))
		}
	}
	return state
}

var dryRun = false

// SetDryRun enables/disables dry-run mode.
func SetDryRun(mode bool) {
	dryRun = mode
	log.Printf("troubleshoot dry-run mode: %v", mode)
}

// IsDryRun reports whether dry-run mode is active.
func IsDryRun() bool {
	return dryRun
}

// lookPath is an injection seam for binary detection, so Detect is testable
// without installing anything. Bare names are resolved on $PATH, the same
// way internal/homebrew finds brew itself.
var lookPath = defaultLookPath

func defaultLookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// readConfig is an injection seam for the configuration read.
var readConfig = defaultReadConfig

func defaultReadConfig() ([]byte, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// Detect reports the feature's state on this host.
func Detect() State {
	var state State
	if data, err := readConfig(); err == nil {
		state = ParseConfig(data)
	}

	state.ServerInstalled = lookPath("linux-mcp-server")
	state.AgentInstalled = lookPath("goose")
	state.DesktopInstalled = lookPath("goose-desktop")
	return state
}

// tapPackage and installPackage are injection seams for the Homebrew
// operations, so the setup sequence is testable without shelling out. A test
// that reached real brew would tap a repository on the machine running it.
var (
	tapPackage     = homebrew.Tap
	installPackage = homebrew.Install
)

// Step is one action in the setup sequence.
type Step struct {
	// Name identifies the step in progress reporting.
	Name string
	// Run performs it.
	Run func() error
	// Needed reports whether the step has anything to do, so an install
	// that is already half-done resumes rather than repeating.
	Needed func(State) bool
}

// Steps returns the setup sequence, in order. It mirrors `ujust probe`:
// tap, install, wire up. The desktop app is included because ChairLift is a
// GUI and launching a terminal agent from one means guessing at a terminal
// emulator.
func Steps() []Step {
	return []Step{
		{
			Name:   "Adding the ublue-os tap",
			Needed: func(State) bool { return true },
			Run:    func() error { return tapPackage(Tap) },
		},
		{
			Name:   "Installing linux-mcp-server",
			Needed: func(s State) bool { return !s.ServerInstalled || !s.AgentInstalled },
			Run:    func() error { return installPackage(ServerFormula, false) },
		},
		{
			Name:   "Installing the Goose app",
			Needed: func(s State) bool { return !s.DesktopInstalled },
			Run:    func() error { return installPackage(DesktopCask, true) },
		},
		{
			Name:   "Connecting it to this system",
			Needed: func(s State) bool { return !s.Wired },
			Run:    runSetupScript,
		},
	}
}

// runSetup is an injection seam for the configuration script.
var runSetup = defaultRunSetup

func defaultRunSetup() error {
	if dryRun {
		log.Printf("[DRY-RUN] would execute: %s", setupCommand)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, setupCommand).CombinedOutput()
	if err != nil {
		return &Error{Message: strings.TrimSpace(string(output)), Err: err}
	}
	return nil
}

func runSetupScript() error { return runSetup() }

// Setup runs every step that still has work to do, reporting progress as it
// goes. It returns the state afterwards so a caller can tell whether the run
// actually left the feature usable.
func Setup(state State, progress func(string)) (State, error) {
	for _, step := range Steps() {
		if !step.Needed(state) {
			continue
		}
		if progress != nil {
			progress(step.Name)
		}
		if err := step.Run(); err != nil {
			return Detect(), fmt.Errorf("%s: %w", strings.ToLower(step.Name), err)
		}
	}
	return Detect(), nil
}

// Error wraps a failed setup command, carrying its output.
type Error struct {
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }
