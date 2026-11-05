// Package devtools implements Bluefin's developer workstation controls:
// user-scope WSL Mode (nsl or Lima) and rootful system Docker installed from
// verified Homebrew bottles. Fixed ublue helper commands grant host access
// and install and manage system Docker.
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
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dockerbrew"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/outputtail"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
)

const (
	// BackendNSL is the default backend using nsl.
	BackendNSL = "nsl"
	// BackendLima is the alternative backend using Lima.
	BackendLima = "lima"
)

// ErrNewLogin means access was granted but this session cannot use it yet.
var ErrNewLogin = errors.New("hardware virtualization access granted; log out and back in, then enable WSL Mode again")

// PrerequisiteError means a developer tool needs an explicit prerequisite,
// so merely trying again will not help. Message is shown on its row; Err
// keeps the raw command output for the log.
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

// DockerMigrationRecovery remains reachable when a legacy daemon runs but this
// account lacks Docker access, so its switch reads off and cannot stop it.
const DockerMigrationRecovery = "An administrator must stop system Docker before migration. In a terminal, run " +
	pkexec.Command + " " + ublue.HelperPath + " docker-disable. This stops running containers. Then turn Docker on again."

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
	cmd.Env = homebrew.WithBrewPath(os.Environ(), homebrew.ExecutablePath())
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
	// Diagnostic retains a failed shell probe without clearing observed Running.
	// Stop admission still requires a fresh successful inventory read.
	Diagnostic string
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
		if err != nil {
			state.Diagnostic = err.Error()
		}
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
		if err != nil {
			state.Diagnostic = err.Error()
		}
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

// NSLKVMGroupMember checks the invoking account's groups in the account
// database, as nsl's prerequisite check does; nsl starts its VM through the kvm
// group, so a grant counts without a new login. /dev/kvm may be writable
// without kvm group membership.
func NSLKVMGroupMember() (bool, error) {
	group, err := exec.Command("getent", "group", "kvm").Output()
	if err != nil {
		return false, fmt.Errorf("look up kvm group: %w", err)
	}
	fields := strings.Split(strings.TrimSpace(string(group)), ":")
	if len(fields) != 4 || fields[0] != "kvm" || fields[2] == "" {
		return false, errors.New("invalid kvm group entry")
	}
	account, err := user.Current()
	if err != nil {
		return false, fmt.Errorf("look up current user: %w", err)
	}
	ids, err := exec.Command("id", "-G", "--", account.Username).Output()
	if err != nil {
		return false, fmt.Errorf("read account groups: %w", err)
	}
	for _, id := range strings.Fields(string(ids)) {
		if id == fields[2] {
			return true, nil
		}
	}
	return false, nil
}

var nslKVMAvailable = KVMAvailable
var nslEnableKVMAccess = ublue.EnableKVMAccess

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
		exists, accessible := nslKVMAvailable()
		if !exists {
			return errors.New("hardware virtualization is unavailable: /dev/kvm is missing; enable it in firmware")
		}
		member := false
		if accessible {
			var err error
			member, err = NSLKVMGroupMember()
			if err != nil {
				return err
			}
		}
		if !accessible || !member {
			stage("Asking for virtual machine access…")
			if err := nslEnableKVMAccess(ctx); err != nil {
				return err
			}
			// Only device access waits for a new login; nsl reads kvm
			// membership from the account database.
			if !accessible {
				return ErrNewLogin
			}
			member, err := NSLKVMGroupMember()
			if err != nil {
				return err
			}
			if !member {
				return errors.New("virtual machine access was not granted to this account")
			}
		}
		if !NSLInstalled() {
			name, cask := WSLPackage(BackendNSL)
			stage("Installing…")
			if err := homebrew.Tap("frostyard/tap"); err != nil {
				return err
			}
			if err := homebrew.TrustPackages(homebrew.UntrustedTap{Name: "frostyard/tap", Casks: []string{name}}); err != nil {
				return err
			}
			if err := homebrew.Install(name, cask); err != nil {
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
		name, cask := WSLPackage(BackendLima)
		if err := homebrew.Install(name, cask); err != nil {
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
	// A dotfile manager often makes ~/.ssh/config a symlink. Write through
	// to its target, which is what ssh reads, so the link survives the
	// rename below instead of being replaced by a regular file.
	target, err := sshConfigTarget(path)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(target)
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
		// Never truncate the user's SSH config in place: a crash or a full
		// disk mid-write would leave it empty. Write a sibling and rename.
		if err := writeFileAtomic(target, append([]byte(include+"\n\n"), b...), 0o600); err != nil {
			return err
		}
	}
	// ssh refuses a group- or world-writable config, so tighten an existing
	// one even when it already carries the Include.
	if err := os.Chmod(target, 0o600); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// sshConfigTarget follows the config's links while allowing only the final
// file to be absent, just as writing through the link did before atomic writes.
func sshConfigTarget(path string) (string, error) {
	for links := 0; ; links++ {
		// Split rather than Dir preserves symlink/.. components until the
		// parent is resolved; lexical cleaning could select a different file.
		dir, base := filepath.Split(path)
		dir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return "", err
		}
		path = filepath.Join(dir, base)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		if links == 40 {
			return "", fmt.Errorf("resolve SSH config %q: too many symbolic links", path)
		}
		link, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		path = link
		if !filepath.IsAbs(link) {
			path = dir + string(filepath.Separator) + link
		}
	}
}

// writeFileAtomic replaces p with data through a synced sibling temporary
// file and a rename, so a reader or a crash sees either the old content or
// the new, never a truncated file.
func writeFileAtomic(p string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, p)
}

// DockerState is the observed system Docker state. ChairLift installs the
// daemon from verified Homebrew bottles through the fixed helper, so missing
// units are installable; masked or broken units are not.
type DockerState struct {
	// Available means the helper can enable Docker here: both units are
	// loaded, or one is missing and docker-enable installs them.
	Available bool
	// Installed means both docker.service and docker.socket are loaded.
	Installed bool
	// Active includes either unit running or transitioning, including socket activation.
	Active bool
	Ready  bool
	// NeedsLogin means this account is a configured docker group member whose
	// current session predates the grant, so only a new login gives access.
	NeedsLogin bool
	// NoAccess means the daemon runs but this account is not a configured
	// member; enabling Docker grants that membership.
	NoAccess bool
	// Unavailable explains masked or broken system units.
	Unavailable string
}

// ErrDockerNewLogin means this session cannot access the enabled Docker socket.
var ErrDockerNewLogin = errors.New("docker access requires logging out and back in")

// DockerCLIError reports that system Docker was enabled but its user-scope
// Homebrew command-line tools could not be installed or linked.
type DockerCLIError struct{ Err error }

func (e *DockerCLIError) Error() string { return "install Docker command-line tools: " + e.Err.Error() }

func (e *DockerCLIError) Unwrap() error { return e.Err }

const dockerSocket = "/var/run/docker.sock"

// DockerStatus reads both system units without escalation, then checks the
// fixed system socket. DOCKER_HOST and the user's runtime directory are ignored.
// Unit read or parse failures return a zero state. Once both units are read,
// Active and Available remain observed state even if the socket ping fails.
func DockerStatus(ctx context.Context) (DockerState, error) {
	return dockerStatus(ctx, dockerSocket)
}

func dockerUnits(ctx context.Context) (DockerState, error) {
	var state DockerState
	loaded, unusable := 0, false
	for _, unit := range []string{"docker.service", "docker.socket"} {
		output, err := command(ctx, false, "systemctl", "show", unit, "--property=LoadState,ActiveState")
		if err != nil {
			if ctx.Err() != nil {
				return state, fmt.Errorf("reading %s: %w: %w", unit, ctx.Err(), err)
			}
			return state, err
		}
		var load, active string
		for line := range strings.SplitSeq(output, "\n") {
			key, value, _ := strings.Cut(line, "=")
			switch key {
			case "LoadState":
				if load != "" || value == "" {
					return state, fmt.Errorf("%s reported an invalid LoadState", unit)
				}
				load = value
			case "ActiveState":
				if active != "" || value == "" {
					return state, fmt.Errorf("%s reported an invalid ActiveState", unit)
				}
				active = value
			}
		}
		switch load {
		case "loaded":
			loaded++
		case "not-found":
		case "masked", "error", "bad-setting", "merged", "stub":
			unusable = true
		default:
			return state, fmt.Errorf("%s did not report a valid LoadState", unit)
		}
		switch active {
		case "inactive", "failed":
		case "active", "reloading", "activating", "deactivating", "maintenance", "refreshing":
			state.Active = true
		default:
			return state, fmt.Errorf("%s did not report a valid ActiveState", unit)
		}
	}
	state.Installed = loaded == 2
	if unusable {
		state.Unavailable = "Docker's system units are unavailable. Ask your administrator to repair or unmask them."
	} else {
		state.Available = true
	}
	return state, nil
}

// dockerGroupMembership reports whether the account database lists this
// account in the docker group, and whether this session already carries it.
// The pure-Go os/user reads /etc/group, which is where the helper adds the
// group and membership. A missing group is not an error: nobody is a member.
var dockerGroupMembership = func() (configured, session bool, err error) {
	group, err := user.LookupGroup("docker")
	if err != nil {
		var unknown user.UnknownGroupError
		if errors.As(err, &unknown) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("look up docker group: %w", err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return false, false, fmt.Errorf("invalid docker group id %q", group.Gid)
	}
	account, err := user.Current()
	if err != nil {
		return false, false, fmt.Errorf("look up current user: %w", err)
	}
	ids, err := account.GroupIds()
	if err != nil {
		return false, false, fmt.Errorf("read account groups: %w", err)
	}
	groups, err := os.Getgroups()
	if err != nil {
		return false, false, fmt.Errorf("read session groups: %w", err)
	}
	return slices.Contains(ids, group.Gid), slices.Contains(groups, gid), nil
}

func dockerStatus(ctx context.Context, socket string) (DockerState, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	state, err := dockerUnits(ctx)
	if err != nil {
		return DockerState{}, err
	}
	if !state.Active {
		return state, nil
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dockerDial(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return state, err
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			// Only a configured member missing from this session's groups
			// is helped by logging in again; anyone else needs the grant.
			configured, session, groupErr := dockerGroupMembership()
			if groupErr != nil {
				log.Printf("devtools: docker group check failed: %v", groupErr)
			}
			if groupErr == nil && configured && !session {
				state.NeedsLogin = true
				return state, fmt.Errorf("%w: %w", ErrDockerNewLogin, err)
			}
			state.NoAccess = true
		}
		return state, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 17))
	if err != nil {
		return state, err
	}
	if len(body) > 16 || response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "OK" {
		return state, fmt.Errorf("system Docker socket ping returned HTTP %d with body %q", response.StatusCode, body)
	}
	state.Ready = true
	return state, nil
}

var dockerDial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext

// SetDocker installs or enables rootful system Docker from verified Homebrew
// bottles through the fixed helper, after retiring a rootless setup an earlier
// release created, and then installs the user-scope Homebrew CLI. Disabling
// retains the installation, data, and the account's Docker group access.
func SetDocker(ctx context.Context, enabled bool, progress func(string)) error {
	return setDocker(ctx, enabled, progress, ublue.SetDocker)
}

func setDocker(ctx context.Context, enabled bool, progress func(string), apply func(context.Context, bool) error) error {
	readCtx, cancel := context.WithTimeout(ctx, statusTimeout)
	state, err := dockerUnits(readCtx)
	cancel()
	if err != nil {
		return err
	}
	if !state.Available && (enabled || !state.Active) {
		return &PrerequisiteError{Message: state.Unavailable, Err: errors.New(state.Unavailable)}
	}
	if !enabled && !state.Installed && !state.Active {
		return nil // Nothing is installed, so nothing runs to stop.
	}
	report := func(stage string) {
		if progress != nil {
			progress(stage)
		}
	}
	if enabled {
		if state.Installed {
			report("Starting system Docker…")
		} else {
			report("Installing Docker for this computer…")
		}
	} else {
		report("Stopping system Docker…")
	}
	if err := apply(ctx, enabled); err != nil {
		if enabled && strings.Contains(err.Error(), ubluehelper.DockerRuntimeMigrationPrefix) {
			return &PrerequisiteError{Message: DockerMigrationRecovery, Err: err}
		}
		return err
	}
	if enabled {
		// Only once system Docker is on: a dismissed prompt or failed install
		// must leave a working v26.10.10 rootless daemon untouched. The two
		// daemons use different sockets, so the brief overlap is harmless.
		if err := retireRootlessDocker(ctx); err != nil {
			return err
		}
	}
	var cliErr error
	if enabled {
		if err := installDockerCLI(ctx, report); err != nil {
			cliErr = &DockerCLIError{Err: err}
		}
	}
	if dryrun.Enabled() {
		return cliErr
	}
	if enabled {
		state, err = DockerStatus(ctx)
		if err != nil {
			return errors.Join(err, cliErr)
		}
		if !state.Ready {
			return errors.Join(errors.New("system Docker socket did not respond after enabling Docker"), cliErr)
		}
		return cliErr
	}
	readCtx, cancel = context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	state, err = dockerUnits(readCtx)
	if err != nil {
		return err
	}
	if state.Active {
		return errors.New("system Docker service or socket is still running after disabling Docker")
	}
	return nil
}

// dockerExecutable and dockerBrewExecutable are seams over the Docker CLI and
// Homebrew resolution, so tests never reach a host installation.
var (
	dockerExecutable     = func() string { return executable("docker") }
	dockerBrewExecutable = homebrew.ExecutablePath
	dockerInstallFormula = func(name string) error { return homebrew.Install(name, false) }
)

// retireRootlessDocker stops the per-user rootless daemon that ChairLift
// v26.10.10 set up with dockerd-rootless-setuptool.sh, and moves the CLI off
// its "rootless" context, so the system daemon is the one the user reaches.
// Unit files the user wrote for anything else are left alone.
func retireRootlessDocker(ctx context.Context) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	unit, err := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "docker.service"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read rootless Docker unit: %w", err)
	}
	if err == nil && rootlessDockerUnit(string(unit)) {
		if _, err := command(ctx, true, "systemctl", "--user", "disable", "--now", "docker.service"); err != nil {
			return fmt.Errorf("stop rootless Docker: %w", err)
		}
	}
	docker := dockerExecutable()
	if docker == "" {
		return nil
	}
	current, err := command(ctx, false, docker, "context", "show")
	if err != nil {
		return fmt.Errorf("read Docker context: %w", err)
	}
	if strings.TrimSpace(current) != "rootless" {
		return nil
	}
	if _, err := command(ctx, true, docker, "context", "use", "default"); err != nil {
		return fmt.Errorf("switch Docker context to default: %w", err)
	}
	return nil
}

// rootlessDockerUnit recognizes the unit dockerd-rootless-setuptool.sh
// writes: its ExecStart runs dockerd-rootless.sh.
func rootlessDockerUnit(unit string) bool {
	for line := range strings.SplitSeq(unit, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(key) == "ExecStart" && strings.Contains(value, "dockerd-rootless.sh") {
			return true
		}
	}
	return false
}

// installDockerCLI installs the user-scope Homebrew Docker CLI formulae and
// links their plugins where the CLI looks for them. Hosts without Homebrew
// get the daemon only.
func installDockerCLI(ctx context.Context, report func(string)) error {
	brew := dockerBrewExecutable()
	if brew == "" {
		return nil
	}
	for _, name := range dockerbrew.CLIFormulae() {
		if err := ctx.Err(); err != nil {
			return err
		}
		report("Installing " + name + "…")
		if err := dockerInstallFormula(name); err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
	}
	return linkDockerCLIPlugins(filepath.Dir(filepath.Dir(brew)))
}

// dockerCLIPlugins are the plugins the docker CLI loads from
// ~/.docker/cli-plugins; Homebrew keeps them in <prefix>/lib/docker/cli-plugins.
var dockerCLIPlugins = [...]string{"docker-buildx", "docker-compose"}

// linkDockerCLIPlugins links each plugin into ~/.docker/cli-plugins when the
// entry is absent or is a symlink into the Homebrew prefix. A regular file or
// a symlink elsewhere is the user's and is never replaced.
func linkDockerCLIPlugins(prefix string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".docker", "cli-plugins")
	for _, name := range dockerCLIPlugins {
		target := filepath.Join(prefix, "lib", "docker", "cli-plugins", name)
		link := filepath.Join(dir, name)
		replace := false
		if info, err := os.Lstat(link); err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			existing, err := os.Readlink(link)
			if err != nil {
				return err
			}
			if !filepath.IsAbs(existing) {
				existing = filepath.Join(dir, existing)
			}
			if filepath.Clean(existing) == target || !withinDir(prefix, existing) {
				continue
			}
			replace = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if dryrun.Enabled() {
			log.Printf("[DRY-RUN] would link %s -> %s", link, target)
			continue
		}
		if _, err := os.Stat(target); err != nil {
			return fmt.Errorf("homebrew did not provide %s: %w", name, err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if !replace {
			if err := os.Symlink(target, link); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			continue
		}
		temporary := filepath.Join(dir, "."+name+".link-tmp")
		_ = os.Remove(temporary)
		if err := os.Symlink(target, temporary); err != nil {
			return err
		}
		if err := os.Rename(temporary, link); err != nil {
			return errors.Join(err, os.Remove(temporary))
		}
	}
	return nil
}

func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, filepath.Clean(path))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
