// chairlift-helper is a privileged helper binary for the
// Bluefin-family write operations ChairLift exposes on Bluefin, Bluefin LTS,
// and Dakota: switching the bootc release channel between stable and
// testing, adding or removing the invoking user's developer-mode group
// membership, and installing, enabling, updating, or disabling rootful
// system Docker.
//
// It is invoked only as `pkexec /usr/bin/chairlift-helper <command>`,
// with each command selected by one PolicyKit action in
// data/io.projectbluefin.chairlift.ublue.policy. pkexec authenticates the action
// and matches the executable path and first argument; everything after that
// is validated here, by internal/ubluehelper.ParseInvocation.
//
// Two values never cross the pkexec boundary as arguments: the target image
// reference is derived here from the read-only system image descriptor, and
// the target username is derived here from the PKEXEC_UID pkexec sets. See
// internal/ubluehelper's package documentation for why.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/dockerbrew"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/registrytags"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	invocation, err := ubluehelper.ParseInvocation(os.Args[1:])
	if err != nil {
		fatal(err.Error())
	}

	// Channel, driver, and gaming switches derive an image reference from the
	// same root-owned table as the GUI. A broken override must stop those switches
	// rather than let the helper resolve a different target, but unrelated
	// fixed commands do not depend on the optional table.
	if invocation.UsesChannelTable() {
		if _, err := imageinfo.LoadSystemTable(); err != nil {
			fatal(fmt.Sprintf("channel table: %v", err))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), ubluehelper.Timeout(invocation.Command))
	defer cancel()

	switch invocation.Command {
	case ubluehelper.CommandChannelSwitch:
		runChannelSwitch(ctx, invocation)
	case ubluehelper.CommandDXEnable, ubluehelper.CommandDXDisable:
		runDevGroups(ctx, invocation)
	case ubluehelper.CommandRestart:
		runRestart(ctx, invocation)
	case ubluehelper.CommandRollback:
		runRollback(ctx, invocation)
	case ubluehelper.CommandAutoEnable, ubluehelper.CommandAutoDisable:
		runAutoUpdates(ctx, invocation)
	case ubluehelper.CommandDriverSwitch:
		runDriverSwitch(ctx, invocation)
	case ubluehelper.CommandGamingSwitch:
		runGamingSwitch(ctx, invocation)
	case ubluehelper.CommandPin, ubluehelper.CommandUnpin:
		runPin(ctx, invocation)
	case ubluehelper.CommandFactoryReset:
		runFactoryReset(ctx, invocation)
	case ubluehelper.CommandKVMEnable:
		runDeveloperAccess(ctx, invocation)
	case ubluehelper.CommandDockerEnable, ubluehelper.CommandDockerUpdate:
		runDockerRuntime(ctx, invocation)
	case ubluehelper.CommandDockerDisable:
		runDocker(ctx, invocation)
	default:
		// Unreachable: ParseInvocation accepts only the commands above. The
		// arm exists so a command added to the parser without a dispatch arm
		// fails loudly instead of exiting 0 having done nothing, which is
		// indistinguishable from a successful privileged action.
		fatal(fmt.Sprintf("unhandled command: %s", invocation.Command))
	}
}

// runChannelSwitch resolves the target reference from the system image
// descriptor and executes `bootc switch`. A descriptor that is missing, or
// whose running tag has no counterpart in the requested channel, is a
// refusal rather than a guessed reference.
func runChannelSwitch(ctx context.Context, invocation ubluehelper.Invocation) {
	info, err := imageinfo.Detect()
	if err != nil {
		fatal(fmt.Sprintf("reading %s: %v", imageinfo.DescriptorPath, err))
	}

	args, ok := ubluehelper.SwitchArgs(info, invocation.Channel)
	if !ok {
		fatal(fmt.Sprintf("no %s image is defined for the running tag %q",
			invocation.Channel, info.EffectiveTag()))
	}

	if invocation.DryRun {
		fmt.Printf("[DRY-RUN] would execute: bootc %v\n", args)
		return
	}

	switchImage(ctx, args, "bootc switch failed")
}

// switchImage runs a derived `bootc switch` argv through
// ubluehelper.SwitchImage, which owns the fallback to the fixed
// `bootc rollback` when a composefs `bootc switch` refuses a target that is
// the rollback deployment. This wrapper only supplies the real runner and
// `bootc status` and prints or exits on the result.
func switchImage(ctx context.Context, args []string, failure string) {
	line, err := ubluehelper.SwitchImage(ctx, runCapture, rollbackImageRef, args, failure)
	if err != nil {
		fatal(err.Error())
	}
	fmt.Println(line)
}

// rollbackImageRef reads the rollback deployment's image reference from
// `bootc status`.
func rollbackImageRef(ctx context.Context) (string, error) {
	status, err := bootc.GetStatus(ctx)
	if err != nil {
		return "", err
	}
	return status.Status.Rollback.ImageRef(), nil
}

// runDriverSwitch moves the host to a different graphics-driver image on the
// same stream. Like the channel switch, the target reference is derived here
// from the system image descriptor rather than accepted as an argument.
func runDriverSwitch(ctx context.Context, invocation ubluehelper.Invocation) {
	info, err := imageinfo.Detect()
	if err != nil {
		fatal(fmt.Sprintf("reading %s: %v", imageinfo.DescriptorPath, err))
	}

	args, ok := ubluehelper.DriverSwitchArgs(info, invocation.Driver)
	if !ok {
		fatal(fmt.Sprintf("no %s image is published for %s:%s",
			invocation.Driver, info.CleanRef(), info.EffectiveTag()))
	}

	if invocation.DryRun {
		fmt.Printf("[DRY-RUN] would execute: bootc %v\n", args)
		return
	}

	switchImage(ctx, args, "driver switch failed")
}

// runGamingSwitch derives the matching gaming or non-gaming image root-side.
// It shares signature enforcement and rollback recovery with other image
// switches and never combines staging the change with an automatic reboot.
func runGamingSwitch(ctx context.Context, invocation ubluehelper.Invocation) {
	info, err := imageinfo.Detect()
	if err != nil {
		fatal(fmt.Sprintf("reading %s: %v", imageinfo.DescriptorPath, err))
	}

	args, ok := ubluehelper.GamingSwitchArgs(info, invocation.Gaming)
	if !ok {
		fatal(fmt.Sprintf("no gaming image is published for %s:%s (gaming=%t)",
			info.CleanRef(), info.EffectiveTag(), invocation.Gaming))
	}

	if invocation.DryRun {
		fmt.Printf("[DRY-RUN] would execute: bootc %v\n", args)
		return
	}

	switchImage(ctx, args, "gaming switch failed")
}

// runPin supplies only the system descriptor and single-tag registry resolver;
// the gated helper package owns all target derivation and refusal decisions.
// The switch goes through switchImage like every other image switch: unpin's
// target is the stream the host left when it pinned, which is exactly the
// rollback deployment a composefs `bootc switch` refuses to switch to.
func runPin(ctx context.Context, invocation ubluehelper.Invocation) {
	info, err := imageinfo.Detect()
	if err != nil {
		fatal(fmt.Sprintf("reading %s: %v", imageinfo.DescriptorPath, err))
	}
	client := &registrytags.Client{}
	args, err := ubluehelper.PinArgs(ctx, info, invocation, client.Tag)
	if err != nil {
		fatal(err.Error())
	}
	if invocation.DryRun {
		fmt.Printf("[DRY-RUN] would execute: bootc %v\n", args)
		return
	}
	switchImage(ctx, args, "bootc switch failed")
}

// runDevGroups adds or removes the invoking user across every developer
// group through ubluehelper.ApplyDevGroups, which owns the skip-and-continue
// and none-applied decisions. This wrapper resolves the user from PKEXEC_UID,
// supplies the real runner and group-file copier, and prints or exits.
func runDevGroups(ctx context.Context, invocation ubluehelper.Invocation) {
	uid, err := ubluehelper.TargetUID(os.Getenv("PKEXEC_UID"))
	if err != nil {
		fatal(err.Error())
	}

	account, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		fatal(fmt.Sprintf("resolving uid %d: %v", uid, err))
	}

	applied, err := ubluehelper.ApplyDevGroups(ctx, runCapture, ensureLocalGroup,
		invocation.Command, account.Username, invocation.DryRun, os.Stdout, os.Stderr)
	if err != nil {
		fatal(err.Error())
	}
	fmt.Println(ubluehelper.DevGroupsAppliedLine(invocation.Command, applied, account.Username))
}

// runRestart restarts the machine. It is the last thing this process does on
// a live run: systemctl returns once the transition is under way, so there is
// nothing meaningful to report afterwards.
func runRestart(ctx context.Context, invocation ubluehelper.Invocation) {
	args := ubluehelper.RestartArgs()

	if invocation.DryRun {
		fmt.Printf("[DRY-RUN] would execute: systemctl %v\n", args)
		return
	}

	if err := run(ctx, "systemctl", args...); err != nil {
		fatal(fmt.Sprintf("restart failed: %v", err))
	}
}

// runRollback makes the previous deployment the default for the next boot.
// It does not restart: rolling back and restarting are separate decisions,
// and a user who rolls back may want to finish what they were doing first.
func runRollback(ctx context.Context, invocation ubluehelper.Invocation) {
	args := ubluehelper.RollbackArgs()

	if invocation.DryRun {
		fmt.Printf("[DRY-RUN] would execute: bootc %v\n", args)
		return
	}

	if err := run(ctx, "bootc", args...); err != nil {
		fatal(fmt.Sprintf("rollback failed: %v", err))
	}
	fmt.Println("rolled back — restart to boot the previous image")
}

// runFactoryReset replaces the running deployment with a fresh install of
// the same image. It does not restart on its own — bootc's reset takes
// effect at the next boot, and combining a factory reset with an
// unrequested restart would compound one irreversible action with another.
func runFactoryReset(ctx context.Context, invocation ubluehelper.Invocation) {
	args := ubluehelper.FactoryResetArgs()

	if invocation.DryRun {
		fmt.Printf("[DRY-RUN] would execute: bootc %v\n", args)
		return
	}

	if err := run(ctx, "bootc", args...); err != nil {
		fatal(fmt.Sprintf("factory reset failed: %v", err))
	}
	fmt.Println("factory reset applied — restart to complete it")
}

// runAutoUpdates turns the unattended-update timers on or off. Each step must
// succeed: a partial application would leave the timers in a state that does
// not match what the user was shown.
func runAutoUpdates(ctx context.Context, invocation ubluehelper.Invocation) {
	steps, ok := ubluehelper.AutoUpdateArgs(invocation.Command)
	if !ok {
		fatal(fmt.Sprintf("unsupported automatic-update command %q", invocation.Command))
	}

	for _, args := range steps {
		if invocation.DryRun {
			fmt.Printf("[DRY-RUN] would execute: systemctl %v\n", args)
			continue
		}
		if err := run(ctx, "systemctl", args...); err != nil {
			fatal(fmt.Sprintf("systemctl %v failed: %v", args, err))
		}
	}
}

// runDeveloperAccess grants the invoking account the one group KVM access
// needs.
func runDeveloperAccess(ctx context.Context, invocation ubluehelper.Invocation) {
	name, args, ok := ubluehelper.AccessArgs(invocation.Command, invokingUsername())
	group, groupOK := ubluehelper.AccessGroup(invocation.Command)
	if !ok || !groupOK {
		fatal("unsupported developer access command")
	}
	if err := ensureLocalGroup(group, invocation.DryRun); err != nil {
		fatal(fmt.Sprintf("granting developer access: %v", err))
	}
	if invocation.DryRun {
		fmt.Printf("[DRY-RUN] would execute: %s %v\n", name, args)
		return
	}
	if err := run(ctx, name, args...); err != nil {
		fatal(fmt.Sprintf("granting developer access: %v", err))
	}
}

// runDocker disables system Docker. The runtime, its data, and the docker
// group are kept, so turning Docker back on restores the same containers.
func runDocker(ctx context.Context, invocation ubluehelper.Invocation) {
	args, ok := ubluehelper.DockerArgs(invocation.Command)
	if !ok {
		fatal("unsupported Docker command")
	}
	if invocation.DryRun {
		fmt.Printf("[DRY-RUN] would execute: systemctl %v\n", args)
		return
	}
	if err := run(ctx, "systemctl", args...); err != nil {
		fatal(fmt.Sprintf("changing Docker daemon: %v", err))
	}
}

// runDockerRuntime enables Docker or updates its Homebrew runtime through
// ubluehelper.EnableDocker and UpdateDocker, which own the ordering and
// dry-run decisions. This wrapper wires them to internal/dockerbrew and the
// real host. Root only ever executes the runtime from dockerbrew.Root, a
// root-owned copy of verified bottles — never the user-writable Homebrew
// prefix.
func runDockerRuntime(ctx context.Context, invocation ubluehelper.Invocation) {
	host := dockerHost()
	var err error
	if invocation.Command == ubluehelper.CommandDockerUpdate {
		err = ubluehelper.UpdateDocker(ctx, host, invocation.DryRun, os.Stdout)
	} else {
		username := invokingUsername()
		err = ubluehelper.EnableDocker(ctx, host, username, invocation.DryRun, os.Stdout)
	}
	if err != nil {
		fatal(err.Error())
	}
}

// dockerHost observes the host and binds every dockerbrew operation to its
// fixed root and unit directory.
func dockerHost() ubluehelper.DockerHost {
	_, imageUnitErr := os.Stat(dockerbrew.ImageUnitPath)
	// The file exists whenever SELinux is enabled; "1" means enforcing.
	enforce, _ := os.ReadFile("/sys/fs/selinux/enforce")
	_, restoreconErr := exec.LookPath("restorecon")
	_, semanageErr := exec.LookPath("semanage")
	// installedSet is the set id Install made current in this run, shared
	// by the Install and RestartDone closures below.
	var installedSet string
	return ubluehelper.DockerHost{
		ImageUnits:       imageUnitErr == nil,
		SELinuxEnforcing: strings.TrimSpace(string(enforce)) == "1",
		Restorecon:       restoreconErr == nil,
		Semanage:         semanageErr == nil,
		HasEquivalence:   hasDockerEquivalence,
		Root:             dockerbrew.Root,
		UnitDir:          dockerbrew.UnitDir,
		IndexURL:         dockerbrew.IndexURL,
		Formulae:         dockerbrew.Formulae(),
		Run:              runCapture,
		EnsureGroup:      ensureLocalGroup,
		GroupExists: func(group string) bool {
			// Built with CGO_ENABLED=0, os/user reads /etc/group directly,
			// which is the database groupadd and usermod edit.
			_, err := user.LookupGroup(group)
			return err == nil
		},
		RuntimeProbe: ubluehelper.DockerRuntimeProbe{Service: dockerServiceState, Process: dockerProcess},
		Install: func(ctx context.Context, admit func(context.Context, string) error) ([]byte, bool, error) {
			manifest, changed, err := dockerbrew.Install(ctx, dockerbrew.Client{}, dockerbrew.Root, admit)
			if err != nil {
				return nil, false, err
			}
			installedSet = manifest.SetID()
			data, err := json.Marshal(manifest)
			return data, changed, err
		},
		CheckUnits: func() error { return dockerbrew.CheckUnits(dockerbrew.UnitDir) },
		// The restart this run performed was onto the set its own Install
		// made current; only that set's marker may be cleared (#697).
		RestartDone: func(ctx context.Context) error {
			if installedSet == "" {
				return errors.New("no Docker runtime was installed by this run")
			}
			return dockerbrew.ClearRestartPending(ctx, dockerbrew.Root, installedSet)
		},
		RuntimeInstalled: func() (bool, error) {
			_, err := dockerbrew.ReadManifest(dockerbrew.Root)
			if errors.Is(err, fs.ErrNotExist) {
				return false, nil
			}
			return err == nil, err
		},
		WriteUnits: func() (bool, error) {
			return dockerbrew.WriteUnits(dockerbrew.UnitDir, dockerbrew.Root)
		},
		RemoveManagedUnits: func() (bool, error) {
			return dockerbrew.RemoveManagedUnits(dockerbrew.UnitDir)
		},
		ContainersRunning: dockerContainersRunning,
	}
}

func dockerServiceState(ctx context.Context) (ubluehelper.DockerServiceState, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "show", "docker.service", "--property=LoadState,ActiveState,SubState,MainPID,InvocationID,NeedDaemonReload").Output()
	if err != nil {
		return ubluehelper.DockerServiceState{}, err
	}
	values := make(map[string]string, 6)
	for _, line := range strings.Split(string(out), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = value
		}
	}
	return ubluehelper.DockerServiceState{
		LoadState: values["LoadState"], ActiveState: values["ActiveState"], SubState: values["SubState"],
		MainPID: values["MainPID"], InvocationID: values["InvocationID"], NeedDaemonReload: values["NeedDaemonReload"],
	}, nil
}

func dockerProcess(pid string) (ubluehelper.DockerProcess, error) {
	// pid was validated by the pure admission before it reaches this reader.
	root := "/proc/" + pid
	argv, err := os.ReadFile(root + "/cmdline")
	if err != nil {
		return ubluehelper.DockerProcess{}, err
	}
	environment, err := os.ReadFile(root + "/environ")
	if err != nil {
		return ubluehelper.DockerProcess{}, err
	}
	executable, err := os.Readlink(root + "/exe")
	if err != nil {
		return ubluehelper.DockerProcess{}, err
	}
	return ubluehelper.DockerProcess{
		Argv:        strings.Split(strings.TrimSuffix(string(argv), "\x00"), "\x00"),
		Environment: strings.Split(strings.TrimSuffix(string(environment), "\x00"), "\x00"),
		Executable:  executable,
	}, nil
}

// dockerContainersRunning asks the daemon on /run/docker.sock for its
// running containers. A stopped docker.service is not queried, because a
// connection would socket-activate it; a missing socket or a refused
// connection likewise means no daemon is serving, so nothing can be
// interrupted.
func dockerContainersRunning(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// is-active exits non-zero for every state but active; read-only query.
	if err := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "docker.service").Run(); err != nil {
		// Only "not active" (3) or "no such unit" (4) mean stopped. A timeout
		// or a D-Bus failure proves nothing, so the caller defers the restart.
		var exit *exec.ExitError
		if ctx.Err() == nil && errors.As(err, &exit) && (exit.ExitCode() == 3 || exit.ExitCode() == 4) {
			return false, nil
		}
		return false, fmt.Errorf("docker.service state: %w", err)
	}
	client := http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", "/run/docker.sock")
		},
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json", nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("docker containers query: %s", resp.Status)
	}
	var containers []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return false, fmt.Errorf("decoding docker containers: %w", err)
	}
	return len(containers) > 0, nil
}

// hasDockerEquivalence reports whether the local SELinux equivalence rule
// mapping dockerbrew.Root onto ubluehelper.SELinuxSource already exists.
// `semanage fcontext -l -C` lists local customizations as "<target> = <source>".
func hasDockerEquivalence(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, "semanage", "fcontext", "-l", "-C").Output()
	if err != nil {
		return false, err
	}
	want := dockerbrew.Root + " = " + ubluehelper.SELinuxSource
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.TrimSpace(line) == want {
			return true, nil
		}
	}
	return false, nil
}

// invokingUsername resolves the account pkexec authenticated for from
// PKEXEC_UID; no username crosses the pkexec boundary.
func invokingUsername() string {
	uid, err := ubluehelper.TargetUID(os.Getenv("PKEXEC_UID"))
	if err != nil {
		fatal(err.Error())
	}
	account, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		fatal(fmt.Sprintf("resolving uid %d: %v", uid, err))
	}
	return account.Username
}

// ensureLocalGroup copies an image-only group into /etc/group so the usermod
// that follows can add a member to it.
func ensureLocalGroup(group string, dryRun bool) error {
	copied, err := ubluehelper.EnsureLocalGroup(ubluehelper.EtcGroupPath, ubluehelper.LibGroupPath, group, dryRun)
	if err != nil {
		return fmt.Errorf("preparing group %s: %w", group, err)
	}
	if copied {
		verb := "copied"
		if dryRun {
			verb = "[DRY-RUN] would copy"
		}
		fmt.Printf("%s group %s from %s to %s\n", verb, group, ubluehelper.LibGroupPath, ubluehelper.EtcGroupPath)
	}
	return nil
}

// runCapture executes one privileged command, forwarding stdout/stderr to the caller's console
// while also teeing stderr into the provided buffer for inspection.
func runCapture(ctx context.Context, stderrBuf io.Writer, name string, args ...string) error {
	full := append([]string{name}, args...)
	if line, err := json.Marshal(full); err == nil {
		fmt.Printf("%s%s\n", ubluehelper.HelperExecPrefix, line)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	if stderrBuf != nil {
		cmd.Stderr = io.MultiWriter(os.Stderr, stderrBuf)
	} else {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// run executes one privileged command, forwarding its output so the calling
// GUI can surface a real failure message instead of a bare exit code.
func run(ctx context.Context, name string, args ...string) error {
	return runCapture(ctx, nil, name, args...)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
