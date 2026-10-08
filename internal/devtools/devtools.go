// Package devtools implements the user-scope parts of Bluefin's documented
// developer workstation: optional editors, Lima Ubuntu, and Docker CLI tools.
// Only fixed ublue helper commands grant host access or control Docker.
package devtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/outputtail"
	"github.com/projectbluefin/chairlift/internal/ublue"
)

const (
	// BackendNSL is the default backend using nsl.
	BackendNSL = "nsl"
	// BackendLima is the alternative backend using Lima.
	BackendLima = "lima"
)

type Tool struct {
	Name string
	// Description is the one-line summary the tool's row shows, so the
	// choices are distinguishable without already knowing each tool.
	Description string
	Package     string
	Cask        bool
	AMD64Only   bool
}

// ErrNewLogin means access was granted but this session cannot use it yet.
var ErrNewLogin = errors.New("hardware virtualization access granted; log out and back in, then enable WSL Mode again")

// PrerequisiteError means the host lacks something WSL Mode needs, so trying
// again will not help. Message is a plain sentence for the user; Err keeps the
// raw command output for the log.
type PrerequisiteError struct {
	Message string
	Err     error
}

func (e *PrerequisiteError) Error() string { return e.Err.Error() }

func (e *PrerequisiteError) Unwrap() error { return e.Err }

// Plain prerequisite sentences shown on the WSL Mode row. ParseNSLDoctor and
// the view share them so the same missing piece always reads the same way.
const (
	// NeedsVirtualization means the processor's virtualization is off.
	NeedsVirtualization = "Turn on virtualization in your computer's firmware settings."
	// NeedsKVMAccess means this account cannot use virtualization yet.
	NeedsKVMAccess = "Turning this on asks for your administrator password. Then log out and back in."
	// NeedsPrerequisite means nsl doctor reported something else missing.
	NeedsPrerequisite = "This computer is missing a part WSL Mode needs."
)

// nslNewMachine and nslNewDistro name the machine WSL Mode creates with the
// built-in engine when none exists.
const (
	nslNewMachine = "ubuntu"
	nslNewDistro  = "ubuntu:26.04"
)

// nslManagedMachines lists, in order of preference, the nsl machine names
// ChairLift has created: ubuntu now, debian before the built-in engine moved
// to Ubuntu. WSL Mode adopts the first one that exists, so a host that already
// has the older debian machine keeps using it rather than gaining a second
// machine. Machines with other names belong to the user and are not WSL Mode's.
var nslManagedMachines = []string{nslNewMachine, "debian"}

// EngineSubtitle describes what the engines run, given the nsl machine WSL
// Mode manages (WSLState.Machine; empty when none exists or it was not read).
func EngineSubtitle(nslMachine string) string {
	if nslMachine == "debian" {
		return "Your built-in engine runs Debian. Lima runs Ubuntu."
	}
	return "Both engines run Ubuntu."
}

// Tools follows Common's ide.Brewfile and devmode terminal-editor choices.
// The current Toolbox cask contains only the x86_64 Linux archive.
func Tools() []Tool {
	return []Tool{
		{Name: "Dev Container CLI", Description: "Builds and runs development containers from a devcontainer.json.", Package: "devcontainer"},
		{Name: "VSCode Stable", Description: "Microsoft's Visual Studio Code editor, monthly stable releases.", Package: "ublue-os/tap/visual-studio-code-linux", Cask: true},
		{Name: "VSCode Insiders", Description: "Daily Visual Studio Code builds with features before they reach stable.", Package: "ublue-os/tap/visual-studio-code-linux@insiders", Cask: true},
		{Name: "VSCodium", Description: "Visual Studio Code built from its open-source code, without Microsoft branding or telemetry.", Package: "ublue-os/tap/vscodium-linux", Cask: true},
		{Name: "Antigravity", Description: "Google's agent-first code editor.", Package: "ublue-os/tap/antigravity-linux", Cask: true},
		{Name: "JetBrains Toolbox", Description: "Installs and updates JetBrains IDEs such as IntelliJ IDEA and PyCharm.", Package: "ublue-os/tap/jetbrains-toolbox-linux", Cask: true, AMD64Only: true},
		{Name: "Neovim", Description: "Extensible, Vim-based terminal text editor.", Package: "neovim"},
		{Name: "Helix", Description: "Modal terminal editor with built-in language support.", Package: "helix"},
		{Name: "Vim", Description: "The classic modal terminal text editor.", Package: "vim"},
		{Name: "Micro", Description: "Terminal text editor with familiar keyboard shortcuts and mouse support.", Package: "micro"},
	}
}

func HostSupported() bool {
	return runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
}

// NSLHostSupported reports whether this host architecture supports nsl.
// nsl machines run in an x86-64 VM and require an x86-64 Linux host.
func NSLHostSupported() bool {
	return runtime.GOOS == "linux" && runtime.GOARCH == "amd64"
}

// NSLInstalled reports whether the nsl binary is found on PATH or Homebrew bin.
func NSLInstalled() bool {
	return executable("nsl") != ""
}

// LimaInstalled reports whether the limactl binary is found on PATH or Homebrew bin.
func LimaInstalled() bool {
	return executable("limactl") != ""
}

func (t Tool) Supported() bool {
	return HostSupported() && (!t.AMD64Only || runtime.GOARCH == "amd64")
}

func Install(t Tool) error {
	if !t.Supported() {
		return fmt.Errorf("%s is not available for this architecture", t.Name)
	}
	if t.Cask {
		if err := homebrew.Tap("ublue-os/tap"); err != nil {
			return err
		}
		// Trust only the package the user explicitly chose, not the whole tap.
		if err := homebrew.TrustPackages(homebrew.UntrustedTap{Name: "ublue-os/tap", Casks: []string{t.Package}}); err != nil {
			return err
		}
	}
	return homebrew.Install(t.Package, t.Cask)
}

func executable(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	if brew := homebrew.ExecutablePath(); brew != "" {
		path := filepath.Join(filepath.Dir(brew), name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return path
		}
	}
	return ""
}

func command(ctx context.Context, mutate bool, program string, args ...string) (string, error) {
	if mutate && dryrun.Enabled() {
		log.Printf("[DRY-RUN] would execute: %s %v", program, args)
		return "", nil
	}
	path := executable(program)
	if path == "" {
		return "", fmt.Errorf("%s is not installed", program)
	}
	timeout := 30 * time.Second
	if mutate {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = 5 * time.Second
	var capture interface {
		io.Writer
		String() string
	}
	var stderr *outputtail.Writer
	if mutate {
		capture = outputtail.New(64 * 1024)
	} else {
		capture = &bytes.Buffer{}
		stderr = outputtail.New(64 * 1024)
	}
	cmd.Stdout, cmd.Stderr = capture, capture
	if !mutate {
		cmd.Stderr = stderr
	}
	err := cmd.Run()
	if mutate && err == nil {
		return "", nil
	}
	output := capture.String()
	if err != nil {
		if stderr != nil {
			if diagnostic := stderr.String(); diagnostic != "" {
				output += "\n" + diagnostic
			}
		}
		return output, fmt.Errorf("%s %v: %w: %s", program, args, err, strings.TrimSpace(output))
	}
	return output, nil
}

type WSLState struct {
	Exists  bool
	Running bool
	Ready   bool
	// Machine is the nsl machine WSL Mode manages when it exists (see
	// nslManagedMachines); empty for Lima or when none exists.
	Machine string
}

// NSLDoctor runs "nsl doctor" to check host prerequisites.
func NSLDoctor(ctx context.Context) (string, error) {
	if !NSLInstalled() {
		return "", errors.New("nsl is not installed")
	}
	return command(ctx, false, "nsl", "doctor")
}

// ParseNSLDoctor parses the output of "nsl doctor". It returns a plain
// sentence if prerequisites are missing, and reports whether the failure was
// due to KVM device access or group membership. The missing items themselves
// name programs and devices, so callers log the raw output instead.
func ParseNSLDoctor(output string) (actionable string, isKVM bool) {
	missing := false
	hasKVM := false
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "MISSING") {
			item := strings.TrimSpace(strings.TrimPrefix(line, "MISSING"))
			if strings.Contains(strings.ToLower(item), "kvm") {
				hasKVM = true
			} else {
				missing = true
			}
		} else if strings.HasPrefix(line, "KVM/vsock group access:") || strings.HasPrefix(line, "SESSION /dev/kvm:") {
			hasKVM = true
		} else if strings.HasPrefix(line, "User namespaces:") || strings.HasPrefix(line, "User systemd:") {
			missing = true
		}
	}
	if missing {
		return NeedsPrerequisite, hasKVM
	}
	if hasKVM {
		return NeedsKVMAccess, true
	}
	return "", false
}

// ParseNSLList parses the output of "nsl list" into the WSLState of the
// machine WSL Mode manages: the first of nslManagedMachines that exists.
// The shared VM's own state and other machines do not count.
func ParseNSLList(output string) WSLState {
	running := map[string]bool{}
	inMachines := false
	for _, rawLine := range strings.Split(output, "\n") {
		fields := strings.Fields(rawLine)
		if len(fields) == 0 || fields[0] == "No" || fields[0] == "Pending" {
			continue
		}
		switch fields[0] {
		case "MACHINE":
			inMachines = true
			continue
		case "VM":
			inMachines = false
			continue
		}
		if inMachines {
			running[fields[0]] = len(fields) >= 2 && fields[1] == "running"
		}
	}
	for _, name := range nslManagedMachines {
		if isRunning, ok := running[name]; ok {
			return WSLState{Exists: true, Running: isRunning, Machine: name}
		}
	}
	return WSLState{}
}

// statusTimeout bounds one whole status probe — every command it runs and the
// Docker socket ping together — so a stalled read cannot hold a developer
// option's controls insensitive (#487). It bounds reads only; mutations keep
// their own per-command class timeout and privileged helper calls are never
// cut mid-flight.
var statusTimeout = 45 * time.Second

// NSLStatus reads the state of nsl machines and VM.
func NSLStatus(ctx context.Context) (WSLState, error) {
	if !NSLInstalled() {
		return WSLState{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	output, err := command(ctx, false, "nsl", "list")
	if err != nil {
		return WSLState{}, err
	}
	state := ParseNSLList(output)
	if state.Running {
		name, args := nslProbe(state.Machine)
		_, err := command(ctx, false, name, args...)
		state.Ready = err == nil
	}
	return state, nil
}

// LimaStatus reads the state of Lima instances.
func LimaStatus(ctx context.Context) (WSLState, error) {
	if !LimaInstalled() {
		return WSLState{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	output, err := command(ctx, false, "limactl", "list", "--json")
	if err != nil {
		return WSLState{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	state := WSLState{}
	for {
		var vm struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		if err := decoder.Decode(&vm); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return WSLState{}, fmt.Errorf("reading Lima instances: %w", err)
		}
		if vm.Name == "ubuntu" {
			state.Exists = true
			state.Running = vm.Status == "Running"
		}
	}
	if state.Running {
		_, err := command(ctx, false, "limactl", "shell", "ubuntu", "true")
		state.Ready = err == nil
	}
	return state, nil
}

// ResolveBackend returns the backend whose machine the user already has when
// the configured default is nsl: an existing Lima machine and no nsl machine
// means Lima. An administrator's explicit Lima is kept as is.
func ResolveBackend(configured string, nslExists, limaExists bool) string {
	if configured == BackendNSL && limaExists && !nslExists {
		return BackendLima
	}
	return configured
}

// WSLStatus reads the status for the chosen backend (defaults to nsl).
func WSLStatus(ctx context.Context, backend ...string) (WSLState, error) {
	b := BackendNSL
	if len(backend) > 0 && backend[0] != "" {
		b = backend[0]
	}
	if b == BackendLima {
		return LimaStatus(ctx)
	}
	return NSLStatus(ctx)
}

// KVMAvailable checks actual invoking-session read/write access, not account
// membership that only reaches the next login. This is a read-only probe.
func KVMAvailable() (exists, accessible bool) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return false, false
	}
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return true, false
	}
	_ = f.Close()
	return true, true
}

// SetWSL enables or disables WSL Mode for the specified backend.
func SetWSL(ctx context.Context, backend string, enabled bool, progress func(string)) error {
	if backend == BackendLima {
		return setLima(ctx, enabled, progress)
	}
	return setNSL(ctx, enabled, progress)
}

func setNSL(ctx context.Context, enabled bool, progress func(string)) error {
	stage := func(text string) {
		if progress != nil {
			progress(text)
		}
	}
	if dryrun.Enabled() {
		if enabled {
			log.Print("[DRY-RUN] would install nsl, check host prerequisites, start the existing ubuntu or debian machine (creating ubuntu only when neither exists), and probe its shell")
		} else {
			log.Print("[DRY-RUN] would stop the ubuntu or debian nsl machine WSL Mode manages, leaving other machines and all data alone")
		}
		return nil
	}
	if enabled {
		if !NSLHostSupported() {
			return errors.New("nsl requires a Linux x86_64 host")
		}
		exists, accessible := KVMAvailable()
		if !exists {
			return errors.New("hardware virtualization is unavailable: /dev/kvm is missing; enable it in firmware")
		}
		if !accessible {
			stage("Asking for virtual machine access…")
			if err := ublue.EnableKVMAccess(ctx); err != nil {
				return err
			}
			return ErrNewLogin
		}
		if !NSLInstalled() {
			stage("Installing…")
			if err := homebrew.Tap("frostyard/tap"); err != nil {
				return err
			}
			if err := homebrew.TrustPackages(homebrew.UntrustedTap{Name: "frostyard/tap", Casks: []string{"frostyard/tap/nsl"}}); err != nil {
				return err
			}
			if err := homebrew.Install("frostyard/tap/nsl", true); err != nil {
				return err
			}
		}
		stage("Checking this computer…")
		// command's error already carries the doctor output for the log.
		if output, err := command(ctx, false, "nsl", "doctor"); err != nil {
			if actionable, _ := ParseNSLDoctor(output); actionable != "" {
				return &PrerequisiteError{Message: actionable, Err: err}
			}
			return err
		}
		return startNSL(ctx, stage)
	}
	state, err := NSLStatus(ctx)
	if err != nil || !state.Running {
		return err
	}
	stage("Stopping…")
	// Stop only the managed machine: `nsl shutdown` stops every machine and
	// every nsl VM, including the user's own (#546). The shared VM powers
	// itself off once its last machine stops.
	_, err = command(ctx, true, "nsl", "stop", state.Machine)
	return err
}

// startNSL brings up the machine WSL Mode manages: the existing ubuntu or
// debian machine, or a new ubuntu machine only when neither exists. Every
// command names that machine, since nsl's default machine may be another one.
func startNSL(ctx context.Context, stage func(string)) error {
	state, err := NSLStatus(ctx)
	if err != nil {
		return err
	}
	machine := state.Machine
	if !state.Exists {
		machine = nslNewMachine
		stage("Creating your virtual machine…")
		if _, err := command(ctx, true, "nsl", "create", machine, "--distro", nslNewDistro); err != nil {
			return err
		}
	}
	if !state.Running {
		stage("Starting…")
		if _, err := command(ctx, true, "nsl", "start", machine); err != nil {
			name, args := nslProbe(machine)
			if _, runErr := command(ctx, true, name, args...); runErr != nil {
				return err
			}
		}
	}
	stage("Checking that it works…")
	name, args := nslProbe(machine)
	_, err = command(ctx, false, name, args...)
	return err
}

// nslProbe is the shell-readiness probe for machine. `--cd /` keeps it
// independent of ChairLift's working directory: nsl run translates the host
// directory into the guest and fails outright when it cannot (/tmp, /, …).
func nslProbe(machine string) (string, []string) {
	return "nsl", []string{"run", "-m", machine, "--cd", "/", "true"}
}

func setLima(ctx context.Context, enabled bool, progress func(string)) error {
	stage := func(text string) {
		if progress != nil {
			progress(text)
		}
	}
	if dryrun.Enabled() {
		if enabled {
			log.Print("[DRY-RUN] would install Lima, wire SSH, start Ubuntu LTS with writable home, enable autostart, and probe its shell")
		} else {
			log.Print("[DRY-RUN] would disable Ubuntu autostart and stop the VM without deleting data")
		}
		return nil
	}
	if enabled {
		if !HostSupported() {
			return errors.New("lima requires a Linux x86_64 or ARM64 host")
		}
		exists, accessible := KVMAvailable()
		if !exists {
			return errors.New("hardware virtualization is unavailable: /dev/kvm is missing; enable it in firmware")
		}
		if !accessible {
			stage("Asking for virtual machine access…")
			if err := ublue.EnableKVMAccess(ctx); err != nil {
				return err
			}
			return ErrNewLogin
		}
		stage("Installing…")
		if err := homebrew.Install("lima", false); err != nil {
			return err
		}
		if err := wireSSH(); err != nil {
			return err
		}
	}
	state, err := LimaStatus(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		if !state.Exists {
			return nil
		}
		stage("Stopping Ubuntu…")
		_, autoErr := command(ctx, true, "limactl", "autostart", "disable", "ubuntu")
		var stopErr error
		if state.Running {
			_, stopErr = command(ctx, true, "limactl", "stop", "--tty=false", "ubuntu")
		}
		return errors.Join(autoErr, stopErr)
	}
	stage("Starting Ubuntu…")
	if !state.Exists {
		_, err = command(ctx, true, "limactl", "start", "--name", "ubuntu", "--mount-writable", "--tty=false", "template:ubuntu-lts")
	} else if !state.Running {
		_, err = command(ctx, true, "limactl", "start", "--tty=false", "ubuntu")
	}
	if err != nil {
		return err
	}
	if _, err := command(ctx, true, "limactl", "autostart", "enable", "ubuntu"); err != nil {
		return err
	}
	stage("Checking that Ubuntu works…")
	_, err = command(ctx, false, "limactl", "shell", "ubuntu", "true")
	return err
}

func wireSSH() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config")
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	const include = "Include ~/.lima/*/ssh.config"
	found := false
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.TrimSpace(line) == include {
			found = true
			break
		}
	}
	if !found {
		b = append([]byte(include+"\n\n"), b...)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

type DockerState struct {
	Available bool
	Active    bool
	Ready     bool
}

// DockerStatus reads the system daemon independently of the optional CLI.
// Ready requires an accessible local socket that answers Docker's ping.
func DockerStatus(ctx context.Context) (DockerState, error) {
	return dockerStatus(ctx, "/var/run/docker.sock")
}

func dockerStatus(ctx context.Context, socket string) (DockerState, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	output, err := command(ctx, false, "systemctl", "show", "docker.service", "--property=LoadState,ActiveState")
	if err != nil {
		return DockerState{}, err
	}
	state := DockerState{}
	loadSeen, activeSeen := false, false
	for line := range strings.SplitSeq(output, "\n") {
		loadSeen = loadSeen || strings.HasPrefix(line, "LoadState=")
		activeSeen = activeSeen || strings.HasPrefix(line, "ActiveState=")
		if line == "LoadState=loaded" {
			state.Available = true
		}
		if line == "ActiveState=active" {
			state.Active = true
		}
	}
	if !loadSeen || !activeSeen {
		return DockerState{}, errors.New("docker daemon state was not reported by systemd")
	}
	if !state.Active {
		return state, nil
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return state, err
	}
	response, err := client.Do(request)
	if err != nil {
		return state, nil
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16))
	state.Ready = err == nil && response.StatusCode == http.StatusOK && strings.TrimSpace(string(body)) == "OK"
	return state, nil
}

func SetDocker(ctx context.Context, enabled bool, progress func(string)) error {
	if enabled {
		state, err := DockerStatus(ctx)
		if err != nil {
			return err
		}
		if !state.Available {
			return errors.New("the base image does not provide Docker's daemon; installing its CLI cannot enable containers")
		}
		if progress != nil {
			progress("Installing Docker tools…")
		}
		for _, pkg := range []string{"docker", "docker-compose", "lazydocker", "dive"} {
			if err := homebrew.Install(pkg, false); err != nil {
				return err
			}
		}
	}
	if progress != nil {
		if enabled {
			progress("Starting Docker…")
		} else {
			progress("Stopping Docker…")
		}
	}
	return ublue.SetDocker(ctx, enabled)
}
