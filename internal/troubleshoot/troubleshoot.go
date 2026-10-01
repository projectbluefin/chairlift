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
// install and the agent runs as the invoking user. The shipped configuration
// explicitly selects fixed Linux diagnostic tools without SSH-key discovery.
package troubleshoot

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"gopkg.in/yaml.v3"
)

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

// defaultConfigPath is the premade configuration shipped by Common.
var defaultConfigPath = "/usr/share/ublue-os/goose/config.yaml"

var diagnosticExtensionKeys = [...]string{"linux-mcp-server", "linux-tools"}

// State is what ChairLift knows about the feature on this host.
type State struct {
	// ServerInstalled reports whether linux-mcp-server is on $PATH.
	ServerInstalled bool
	// AgentInstalled reports whether the goose CLI is on $PATH.
	AgentInstalled bool
	// DesktopInstalled reports whether the Goose desktop app is available.
	DesktopInstalled bool
	// Wired reports whether an enabled Linux diagnostic extension uses the
	// explicit fixed-tool policy. Detect also checks its command availability.
	Wired    bool
	commands [len(diagnosticExtensionKeys)]string
	// Provider is the LLM provider Goose is configured to use, empty when
	// none is set. ChairLift does not select or replace the user's provider.
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

// ParseConfig recognizes the shipped and legacy diagnostic extension keys.
// Malformed YAML and extensions without explicit fixed tools are not wired.
func ParseConfig(data []byte) State {
	var cfg gooseConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return State{}
	}

	state := State{Provider: cfg.Provider}
	for i, key := range diagnosticExtensionKeys {
		ext, ok := cfg.Extensions[key]
		if !ok || !ext.enabled() {
			continue
		}
		if !ext.valid() {
			state.Wired = false
			return state
		}
		state.Wired, state.commands[i] = true, ext.Cmd
	}
	return state
}

// gooseConfig reads only the provider and diagnostic extension definitions.
type gooseConfig struct {
	Provider   string              `yaml:"GOOSE_PROVIDER"`
	Extensions map[string]gooseExt `yaml:"extensions"`
}

// gooseExt contains the fields needed to verify the diagnostic tool policy.
type gooseExt struct {
	Enabled *bool             `yaml:"enabled"`
	Type    string            `yaml:"type"`
	Cmd     string            `yaml:"cmd"`
	Args    []string          `yaml:"args"`
	Envs    map[string]string `yaml:"envs"`
}

// enabled reports whether the extension is active. Goose enables an extension
// unless `enabled` is explicitly false, so an absent flag still counts as on.
func (e gooseExt) enabled() bool {
	return e.Enabled == nil || *e.Enabled
}

// valid requires the fixed diagnostic tools and explicit SSH-key opt-out.
func (e gooseExt) valid() bool {
	if e.Type != "stdio" || filepath.Base(e.Cmd) != "linux-mcp-server" || e.Envs["LINUX_MCP_SSH_KEY_PATH"] != "" {
		return false
	}
	fixed, noSearch := false, false
	for i := 0; i < len(e.Args); i++ {
		switch e.Args[i] {
		case "--toolset":
			if fixed || i+1 == len(e.Args) || e.Args[i+1] != "FIXED" {
				return false
			}
			fixed = true
			i++
		case "--no-search-for-ssh-key":
			noSearch = true
		case "--search-for-ssh-key", "--no-verify-host-keys", "--ssh-key-path":
			return false
		default:
			if strings.HasPrefix(e.Args[i], "--toolset=") || strings.HasPrefix(e.Args[i], "--ssh-key-path=") {
				return false
			}
		}
	}
	return fixed && noSearch
}

// lookPath is the binary-detection seam. Homebrew's existing resolution
// supplies its bin directory when a desktop launch has no brew on PATH.
var lookPath = defaultLookPath

func defaultLookPath(name string) bool {
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	if filepath.IsAbs(name) {
		return false
	}
	brew := homebrew.ExecutablePath()
	if brew == "" {
		return false
	}
	_, err := exec.LookPath(filepath.Join(filepath.Dir(brew), name))
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
	if state.Wired {
		for _, command := range state.commands {
			if command != "" && !lookPath(command) {
				state.Wired = false
				break
			}
		}
	}
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

// runSetup is an injection seam for installing the premade configuration.
var runSetup = defaultRunSetup

func defaultRunSetup() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		if dryrun.Enabled() {
			log.Print("[DRY-RUN] would keep existing Goose configuration")
			return nil
		}
		if data, err := os.ReadFile(path); err == nil && ParseConfig(data).Wired {
			return nil
		}
		return &Error{Message: fmt.Sprintf("Goose configuration was kept unchanged; review Linux diagnostics in %s", path), Err: os.ErrExist}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would copy Goose configuration from %s", defaultConfigPath)
		return nil
	}
	data, err := os.ReadFile(defaultConfigPath)
	if err != nil {
		return fmt.Errorf("reading the shipped Goose configuration: %w", err)
	}
	data, err = prepareNewConfig(data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return errors.Join(writeErr, closeErr)
	}
	return nil
}

// prepareNewConfig makes the shipped preset's implicit server defaults explicit
// in a new user configuration. Existing user files never pass through here.
func prepareNewConfig(data []byte) ([]byte, error) {
	if ParseConfig(data).Wired {
		return data, nil
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	extensions, _ := document["extensions"].(map[string]any)
	for _, key := range diagnosticExtensionKeys {
		extension, _ := extensions[key].(map[string]any)
		command, _ := extension["cmd"].(string)
		if extension["enabled"] == false || extension["type"] != "stdio" || filepath.Base(command) != "linux-mcp-server" {
			continue
		}
		args, empty := extension["args"].([]any)
		if extension["args"] == nil || (empty && len(args) == 0) {
			extension["args"] = []string{"--toolset", "FIXED", "--no-search-for-ssh-key"}
		}
	}
	prepared, err := yaml.Marshal(document)
	if err != nil {
		return nil, err
	}
	if !ParseConfig(prepared).Wired {
		return nil, fmt.Errorf("the shipped Goose configuration does not enable fixed Linux diagnostics")
	}
	return prepared, nil
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
