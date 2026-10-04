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
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
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
	// ModelSelected reports a model name, not credentials or service readiness.
	ModelSelected bool
}

// Ready reports whether the installed diagnostic tools are connected. It does
// not verify a model provider, credentials, or the provider's reachability.
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

// BackupPath returns Goose's backup configuration file path.
func BackupPath() (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}
	return path + BackupSuffix, nil
}

// ParseConfig recognizes the shipped and legacy diagnostic extension keys.
// Malformed YAML and extensions without explicit fixed tools are not wired.
func ParseConfig(data []byte) State {
	document, err := decodeConfig(data)
	if err != nil {
		return State{}
	}
	var cfg gooseConfig
	if err := document.Decode(&cfg); err != nil {
		return State{}
	}

	state := State{Provider: cfg.Provider, ModelSelected: strings.TrimSpace(cfg.Model) != ""}
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
	Model      string              `yaml:"GOOSE_MODEL"`
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
	return resolveTool(name) != ""
}

func resolveTool(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	if filepath.IsAbs(name) {
		return ""
	}
	brew := homebrew.ExecutablePath()
	if brew == "" {
		return ""
	}
	path, err := exec.LookPath(filepath.Join(filepath.Dir(brew), name))
	if err != nil {
		return ""
	}
	return path
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
			Run:    func() error { return runSetup() },
		},
	}
}

// ExtensionStatus represents the classification of the Linux diagnostic extension
// in Goose's configuration.
type ExtensionStatus int

const (
	// ExtensionStatusMissing: No Goose config or extension is not defined.
	ExtensionStatusMissing ExtensionStatus = iota
	// ExtensionStatusMalformed: Config exists but contains invalid YAML or invalid mapping.
	ExtensionStatusMalformed
	// ExtensionStatusUnsafe: Extension exists but violates security policies (SSH key, not stdio, missing FIXED toolset).
	ExtensionStatusUnsafe
	// ExtensionStatusValid: Extension exists, is enabled, stdio, points to linux-mcp-server with --toolset FIXED and no SSH.
	ExtensionStatusValid
)

// ClassifyConfig evaluates raw Goose configuration YAML bytes and returns its ExtensionStatus.
func ClassifyConfig(data []byte, fileExists bool) ExtensionStatus {
	if !fileExists || len(bytes.TrimSpace(data)) == 0 {
		return ExtensionStatusMissing
	}
	document, err := decodeConfig(data)
	if err != nil {
		return ExtensionStatusMalformed
	}
	var cfg gooseConfig
	if err := document.Decode(&cfg); err != nil {
		return ExtensionStatusMalformed
	}
	for _, key := range diagnosticExtensionKeys {
		ext, ok := cfg.Extensions[key]
		if !ok {
			continue
		}
		if !ext.enabled() || !ext.valid() {
			return ExtensionStatusUnsafe
		}
		return ExtensionStatusValid
	}
	return ExtensionStatusMissing
}

// VerifyExtensionOnDisk checks the current user's Goose configuration file and
// verifies that the Linux diagnostic extension is configured and its command resolves.
func VerifyExtensionOnDisk() ExtensionStatus {
	data, err := readConfig()
	if err != nil {
		if os.IsNotExist(err) {
			return ExtensionStatusMissing
		}
		return ExtensionStatusMalformed
	}
	status := ClassifyConfig(data, true)
	if status != ExtensionStatusValid {
		return status
	}
	parsed := ParseConfig(data)
	for _, cmd := range parsed.commands {
		if cmd != "" && !lookPath(cmd) {
			return ExtensionStatusUnsafe
		}
	}
	return ExtensionStatusValid
}

// EnsureDiagnosticsConfigured writes the hardened Linux diagnostic extension into
// Goose's configuration, preserving existing user settings, models, providers, and
// other extensions. Before replacing an existing configuration, it keeps an atomic
// backup of the exact prior bytes at config.yaml.chairlift-backup. It verifies the
// written configuration before returning.
func EnsureDiagnosticsConfigured() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, info, err := readUserConfig(path)
	if err != nil {
		return fmt.Errorf("reading Goose configuration: %w", err)
	}
	if dryrun.Enabled() && info == nil {
		log.Printf("[DRY-RUN] would copy Goose configuration from %s", defaultConfigPath)
		return nil
	}
	if info == nil {
		data, err = os.ReadFile(defaultConfigPath)
		if err != nil {
			return fmt.Errorf("reading the shipped Goose configuration: %w", err)
		}
	}
	prepared, err := prepareDiagnostics(data, info != nil)
	if err != nil {
		return fmt.Errorf("the Goose configuration was kept unchanged: %w", err)
	}
	if info != nil && bytes.Equal(data, prepared) {
		return nil
	}
	if dryrun.Enabled() {
		log.Print("[DRY-RUN] would connect read-only Linux tools in existing Goose configuration")
		return nil
	}
	if err := writeUserConfig(path, data, info, prepared); err != nil {
		return fmt.Errorf("saving Goose configuration: %w", err)
	}
	observed, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !ParseConfig(observed).Wired {
		return fmt.Errorf("linux tools could not be verified in Goose; check its configuration")
	}
	return nil
}

// runSetup is an injection seam for connecting the diagnostic extension.
var runSetup = EnsureDiagnosticsConfigured

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
	after := Detect()
	if !dryrun.Enabled() && (!after.Ready() || !after.DesktopInstalled) {
		return after, fmt.Errorf("setup finished, but Goose or its Linux tools are still unavailable")
	}
	return after, nil
}
