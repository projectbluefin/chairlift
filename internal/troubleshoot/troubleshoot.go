// Package troubleshoot is the engine behind Troubleshooting, the
// Agents page's Goose row; the Ask Bluefin menu entry is one path into it.
// It is the Goose desktop app, running on the Agent Mode model, with
// read-only tools for this machine's live state and the Project Bluefin
// knowledge base. Knowledge searches go online, so nothing here claims a
// session's questions stay on this computer.
//
// Everything is installed with Homebrew and everything Goose reads is
// ChairLift's own. The session runs in a dedicated Goose profile under
// $XDG_DATA_HOME/chairlift/troubleshooting: GOOSE_PATH_ROOT points Goose's
// config, data, and state there, and XDG_CONFIG_HOME points the desktop
// app's Electron profile (and with it the single-instance lock) at a
// sibling directory. A user's own ~/.config/goose and a Goose window they
// already have open are never read, written, or handed this launch.
//
// llmman wraps the launch: `llmman launch goose-desktop --model <alias>`
// hands Goose its provider through the environment, so no provider, model,
// or key is written to disk by anyone.
//
// Nothing here is privileged. Every package is a user-scope Homebrew
// install, the agent runs as the invoking user, and linux-mcp-server runs
// with its fixed, read-only, local-only toolset. internal/agentmode decides
// readiness and owns the launch; this package supplies its pieces.
package troubleshoot

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"gopkg.in/yaml.v3"
)

// The packages the feature is assembled from.
const (
	// Tap holds linux-mcp-server and the Goose desktop cask. brew requires
	// it be tapped before either qualified name resolves.
	Tap = "ublue-os/tap"
	// ServerFormula is the MCP server that exposes this system's state.
	ServerFormula = "ublue-os/tap/linux-mcp-server"
	// ExtractorFormula supplies the `cpio` the desktop cask's preflight
	// pipes the upstream RPM into. The cask declares only rpm2cpio, and
	// Bluefin images ship no cpio, so without this the cask install fails
	// partway through unpacking.
	ExtractorFormula = "cpio"
	// DesktopCask is the Goose desktop app. Its binary is `goose-desktop`.
	// Upstream publishes it for x86_64 only.
	DesktopCask = "ublue-os/tap/goose-linux"
)

// The executables Detect looks for.
const (
	serverBinary  = "linux-mcp-server"
	desktopBinary = "goose-desktop"
	llmmanBinary  = "llmman"
)

// KnowledgeURI is the Project Bluefin knowledge MCP server: a public,
// read-only index of the project's Hive knowledge base.
const KnowledgeURI = "https://mcp.projectbluefin.io/mcp"

// knowledgeTool is the one knowledge-server tool a troubleshooting session
// needs. The server's factory-status and work-queue tools are for
// contributors and would only distract a small model.
const knowledgeTool = "search_knowledge"

// ServerArgs confines linux-mcp-server to its fixed read-only toolset on
// this machine only: no script execution, no remote hosts, and no SSH key
// discovery.
var ServerArgs = []string{"--toolset", "FIXED", "--host-mode", "LOCAL_ONLY", "--no-search-for-ssh-key"}

//go:embed hints.md
var hints string

// Hints returns the instructions every session starts with.
func Hints() string { return hints }

// State is what ChairLift knows about the feature on this host.
type State struct {
	// Supported reports whether the desktop app exists for this
	// architecture.
	Supported bool
	// ServerPath is linux-mcp-server's absolute path, empty when absent.
	ServerPath string
	// DesktopPath is goose-desktop's absolute path, empty when absent.
	DesktopPath string
	// LLMManPath is llmman's absolute path, empty when absent. Agent Mode
	// installs it; this feature never does.
	LLMManPath string
}

// Installed reports whether every package this feature installs is present.
func (s State) Installed() bool {
	return s.ServerPath != "" && s.DesktopPath != ""
}

// Ready reports whether a session can be launched, model aside: the model
// is Agent Mode's, and the caller checks it there.
func (s State) Ready() bool {
	return s.Supported && s.Installed() && s.LLMManPath != ""
}

// lookPath is an injection seam for binary detection.
var lookPath = exec.LookPath

// brewExecutable is an injection seam for the Homebrew resolution.
var brewExecutable = homebrew.ExecutablePath

// goarch is an injection seam for the architecture check.
var goarch = runtime.GOARCH

// resolve finds an executable on $PATH, or beside brew when $PATH lacks
// Homebrew's bin directory — a direct launch that bypassed the wrapper.
func resolve(name string) string {
	if path, err := lookPath(name); err == nil && filepath.IsAbs(path) {
		return path
	}
	if brew := brewExecutable(); brew != "" {
		candidate := filepath.Join(filepath.Dir(brew), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// Detect reports the feature's state on this host. It only resolves paths,
// so it is cheap, but callers still run it off the GTK main thread.
func Detect() State {
	return State{
		Supported:   goarch == "amd64",
		ServerPath:  resolve(serverBinary),
		DesktopPath: resolve(desktopBinary),
		LLMManPath:  resolve(llmmanBinary),
	}
}

// tapPackage and installPackage are injection seams for the Homebrew
// operations, so the setup sequence is testable without shelling out.
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

// Steps returns the setup sequence, in order.
func Steps() []Step {
	return []Step{
		{
			Name:   "Adding the ublue-os tap",
			Needed: func(s State) bool { return !s.Installed() },
			Run:    func() error { return tapPackage(Tap) },
		},
		{
			Name:   "Installing the system inspection tools",
			Needed: func(s State) bool { return s.ServerPath == "" },
			Run:    func() error { return installPackage(ServerFormula, false) },
		},
		{
			Name:   "Installing the Goose app",
			Needed: func(s State) bool { return s.DesktopPath == "" },
			Run: func() error {
				if err := installPackage(ExtractorFormula, false); err != nil {
					return err
				}
				return installPackage(DesktopCask, true)
			},
		},
	}
}

// ErrUnsupported is returned when the desktop app does not exist for this
// architecture.
var ErrUnsupported = errors.New("the Goose app is only published for x86_64")

// Setup runs every step that still has work to do, reporting progress as it
// goes. It returns the state afterwards so a caller can tell whether the run
// actually left the feature usable.
func Setup(state State, progress func(string)) (State, error) {
	if !state.Supported {
		return state, ErrUnsupported
	}
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

// Profile is the dedicated Goose profile a session runs in.
type Profile struct {
	// Root is the profile's own directory.
	Root string
}

// ProfileAt returns the profile rooted in dataHome ($XDG_DATA_HOME).
func ProfileAt(dataHome string) Profile {
	return Profile{Root: filepath.Join(dataHome, "chairlift", "troubleshooting")}
}

// DefaultProfile returns the profile under the user's $XDG_DATA_HOME.
func DefaultProfile() (Profile, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return ProfileAt(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Profile{}, err
	}
	return ProfileAt(filepath.Join(home, ".local", "share")), nil
}

// GooseRoot is the GOOSE_PATH_ROOT: Goose keeps config/, data/, and
// state/ beneath it.
func (p Profile) GooseRoot() string { return filepath.Join(p.Root, "goose") }

// DesktopConfigHome is the XDG_CONFIG_HOME the desktop app runs with, so
// its Electron profile and single-instance lock are this profile's own.
func (p Profile) DesktopConfigHome() string { return filepath.Join(p.Root, "desktop") }

// lockOwner is an injection seam: whether the process a Chromium
// single-instance lock names is alive. The lock is a symlink to
// "<host>-<pid>"; a lock from another host or a dead process is stale.
var lockOwner = func(target string) bool {
	i := strings.LastIndex(target, "-")
	if i <= 0 {
		return false
	}
	if name, err := os.Hostname(); err != nil || name != target[:i] {
		return false
	}
	pid, err := strconv.Atoi(target[i+1:])
	if err != nil || pid <= 0 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// bootTime is an injection seam: when this host booted, from /proc/stat's
// btime. The zero time, when it cannot be read, disables the boot check.
var bootTime = func() time.Time {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if s, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return time.Unix(s, 0)
			}
		}
	}
	return time.Time{}
}

// Running reports whether a Goose desktop session holds this profile's
// single-instance lock, so a new launch would be handed to it. The profile
// persists across reboots and so can a lock Goose never removed; a lock
// written before this boot is stale whatever its pid now names, or a reused
// pid would hand the launch to an unrelated process and skip the profile
// write and llmman's environment.
func (p Profile) Running() bool {
	lock := filepath.Join(p.DesktopConfigHome(), "Goose", "SingletonLock")
	target, err := os.Readlink(lock)
	if err != nil {
		return false
	}
	info, err := os.Lstat(lock)
	if err != nil || info.ModTime().Before(bootTime()) {
		return false
	}
	return lockOwner(target)
}

// ConfigPath is the profile's Goose configuration file.
func (p Profile) ConfigPath() string {
	return filepath.Join(p.GooseRoot(), "config", "config.yaml")
}

// HintsPath is the profile's global hints file, which Goose adds to every
// session's instructions.
func (p Profile) HintsPath() string {
	return filepath.Join(p.GooseRoot(), "config", ".goosehints")
}

// profileConfig is the whole of the profile's config.yaml. The provider is
// absent on purpose: llmman supplies it through the environment.
type profileConfig struct {
	Telemetry  bool           `yaml:"GOOSE_TELEMETRY_ENABLED"`
	NoKeyring  bool           `yaml:"GOOSE_DISABLE_KEYRING"`
	Extensions map[string]any `yaml:"extensions"`
}

// disabledPlatformExtensions are Goose's own built-in extensions, each
// written disabled. Goose adds any platform extension missing from the
// file at startup with its own default — `developer`, which runs shell
// commands, and `extensionmanager`, which can turn other extensions on,
// both default to on — but keeps an `enabled` value already present. This
// list covers Goose 1.53 on a first launch; on every later launch
// RenderConfig also disables whatever extension Goose wrote into the
// profile since, so one a later Goose adds is on for one session at most.
var disabledPlatformExtensions = []string{
	"analyze", "apps", "chatrecall", "code_execution", "developer",
	"extensionmanager", "orchestrator", "scheduler", "skills", "summarize",
	"summon", "todo", "tom",
}

// disabledPlatform is the smallest entry Goose's migration accepts; it
// fills in the description and display name itself.
type disabledPlatform struct {
	Enabled bool   `yaml:"enabled"`
	Type    string `yaml:"type"`
	Name    string `yaml:"name"`
}

// profileExtension is one Goose extension entry.
type profileExtension struct {
	Enabled        bool     `yaml:"enabled"`
	Type           string   `yaml:"type"`
	Name           string   `yaml:"name"`
	Description    string   `yaml:"description"`
	Cmd            string   `yaml:"cmd,omitempty"`
	Args           []string `yaml:"args,omitempty"`
	URI            string   `yaml:"uri,omitempty"`
	AvailableTools []string `yaml:"available_tools,omitempty"`
	Timeout        int      `yaml:"timeout"`
	Bundled        bool     `yaml:"bundled"`
}

// RenderConfig returns the profile's config.yaml for a linux-mcp-server at
// serverPath. It is the complete extension set: a session can inspect this
// machine and search the Bluefin knowledge base, and nothing else. current
// is the profile's config as it stands, or nil: every extension in it other
// than the two enabled here is carried over disabled.
func RenderConfig(serverPath string, current []byte) ([]byte, error) {
	if !filepath.IsAbs(serverPath) {
		return nil, fmt.Errorf("linux-mcp-server path %q is not absolute", serverPath)
	}
	extensions := map[string]any{}
	var previous struct {
		Extensions map[string]map[string]any `yaml:"extensions"`
	}
	// The file is ChairLift's own; an unreadable one is simply replaced.
	if yaml.Unmarshal(current, &previous) == nil {
		for name, entry := range previous.Extensions {
			if entry == nil {
				continue
			}
			entry["enabled"] = false
			extensions[name] = entry
		}
	}
	allowed := map[string]any{
		"linux-tools": profileExtension{
			Enabled:     true,
			Type:        "stdio",
			Name:        "linux-tools",
			Description: "Read-only inspection of this computer: logs, services, processes, storage, and network",
			Cmd:         serverPath,
			Args:        ServerArgs,
			Timeout:     300,
		},
		"bluefin-knowledge": profileExtension{
			Enabled:        true,
			Type:           "streamable_http",
			Name:           "bluefin-knowledge",
			Description:    "Search the Project Bluefin knowledge base for known issues and fixes",
			URI:            KnowledgeURI,
			AvailableTools: []string{knowledgeTool},
			Timeout:        60,
		},
	}
	extensions["linux-tools"] = allowed["linux-tools"]
	extensions["bluefin-knowledge"] = allowed["bluefin-knowledge"]
	for _, name := range disabledPlatformExtensions {
		if _, seen := extensions[name]; !seen {
			extensions[name] = disabledPlatform{Type: "platform", Name: name}
		}
	}
	cfg := profileConfig{NoKeyring: true, Extensions: extensions}
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return append([]byte("# Written by "+branding.AppName+" for Troubleshooting; rewritten on every launch.\n"), body...), nil
}

// writeFile is an injection seam for the profile writes.
var writeFile = writeAtomic

// Write lays the profile down for a linux-mcp-server at serverPath. The
// profile is ChairLift's alone, so it is rewritten whole on every launch:
// a moved Homebrew prefix or a hand edit cannot leave a session with stale
// tools.
func (p Profile) Write(serverPath string) error {
	current, err := os.ReadFile(p.ConfigPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	config, err := RenderConfig(serverPath, current)
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would write %s and %s", p.ConfigPath(), p.HintsPath())
		return nil
	}
	if err := writeFile(p.ConfigPath(), config); err != nil {
		return err
	}
	if err := writeFile(p.HintsPath(), []byte(Hints())); err != nil {
		return err
	}
	if user, err := userConfigDir(); err == nil {
		return p.linkDesktopSettings(user)
	}
	return nil
}

// userConfigDir is an injection seam for the user's own XDG_CONFIG_HOME.
var userConfigDir = os.UserConfigDir

// sharedDesktopSettings are the user's settings the desktop app still has
// to see from inside its own XDG_CONFIG_HOME: the default-application
// choices `xdg-open` reads, so a link opened from a session goes to the
// user's browser, and the dconf database GTK and GSettings read.
var sharedDesktopSettings = []string{"mimeapps.list", "dconf"}

// linkDesktopSettings symlinks each shared setting from the user's config
// directory into the desktop config home. Moving XDG_CONFIG_HOME is what
// isolates Electron's profile and lock, but it moves these with it. A
// name already present in the desktop config home is left as it is.
func (p Profile) linkDesktopSettings(userConfig string) error {
	if err := os.MkdirAll(p.DesktopConfigHome(), 0o700); err != nil {
		return err
	}
	for _, name := range sharedDesktopSettings {
		target := filepath.Join(userConfig, name)
		if _, err := os.Stat(target); err != nil {
			continue
		}
		link := filepath.Join(p.DesktopConfigHome(), name)
		if _, err := os.Lstat(link); err == nil {
			continue
		}
		if err := os.Symlink(target, link); err != nil {
			return err
		}
	}
	return nil
}

// writeAtomic replaces dest with data through a temporary file in the same
// directory, so a reader never sees a partial file.
func writeAtomic(dest string, data []byte) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".troubleshoot-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// Command returns the launch: llmman starts Goose's desktop app pointed at
// model, inside the profile. The environment is the caller's plus the two
// profile roots.
func Command(state State, profile Profile, model string) (*exec.Cmd, error) {
	if !state.Ready() {
		return nil, errors.New("goose and its tools are not set up")
	}
	if model == "" {
		return nil, errors.New("no Agent Mode model is selected")
	}
	cmd := exec.Command(state.LLMManPath, "launch", "goose-desktop", "--model", model)
	cmd.Env = sessionEnv(profile)
	return cmd, nil
}

// ReopenCommand starts Goose's desktop app the ordinary way inside a
// profile whose session is already running. Goose's single-instance lock
// hands the request to that session, which keeps the provider llmman gave
// it; the new process then exits. Raising the window is left to the
// compositor: no activation token is passed, and GNOME may show a "Goose is
// ready" notification instead. llmman is not involved: it refuses a launch
// while the lock is held.
func ReopenCommand(state State, profile Profile) (*exec.Cmd, error) {
	if state.DesktopPath == "" {
		return nil, errors.New("goose desktop is not installed")
	}
	cmd := exec.Command(state.DesktopPath)
	cmd.Env = sessionEnv(profile)
	return cmd, nil
}

// sessionEnv is the caller's environment plus the two profile roots, with
// Homebrew on PATH (llmman finds goose-desktop there, and a direct launch
// of ChairLift does not provide it).
func sessionEnv(profile Profile) []string {
	env := homebrew.WithBrewPath(os.Environ(), brewExecutable())
	return append(env,
		"GOOSE_PATH_ROOT="+profile.GooseRoot(),
		"XDG_CONFIG_HOME="+profile.DesktopConfigHome(),
	)
}
