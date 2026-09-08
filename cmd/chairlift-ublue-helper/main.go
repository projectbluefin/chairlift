// chairlift-ublue-helper is a privileged helper binary for the
// Bluefin-family write operations ChairLift exposes on Bluefin, Bluefin LTS,
// and Dakota: switching the bootc release channel between stable and
// testing, and adding or removing the invoking user's developer-mode group
// membership.
//
// It is invoked only as `pkexec /usr/bin/chairlift-ublue-helper <command>`,
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
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"time"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
)

const defaultTimeout = 10 * time.Minute

func main() {
	invocation, err := ubluehelper.ParseInvocation(os.Args[1:])
	if err != nil {
		fatal(err.Error())
	}

	// Apply the same channel-table override the GUI applies, from the same
	// fixed root-owned paths. If the two sides disagreed, the GUI would
	// offer a switch the helper then refuses. A broken override is fatal
	// here rather than logged: the helper is about to change which OS image
	// this machine boots, so it must not fall back to a different table
	// than the one the user was shown.
	if _, err := imageinfo.LoadSystemTable(); err != nil {
		fatal(fmt.Sprintf("channel table: %v", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
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
	case ubluehelper.CommandFactoryReset:
		runFactoryReset(ctx, invocation)
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

	if err := run(ctx, "bootc", args...); err != nil {
		fatal(fmt.Sprintf("bootc switch failed: %v", err))
	}
	fmt.Printf("switched to %s — restart to apply\n", args[len(args)-1])
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

	if err := run(ctx, "bootc", args...); err != nil {
		fatal(fmt.Sprintf("driver switch failed: %v", err))
	}
	fmt.Printf("switched to %s — restart to apply\n", args[len(args)-1])
}

// runDevGroups adds or removes the invoking user across every developer
// group. A group that does not exist on this image is skipped rather than
// failing the whole operation: the group set is shared across Bluefin,
// Bluefin LTS, and Dakota, which do not all ship the same ones.
func runDevGroups(ctx context.Context, invocation ubluehelper.Invocation) {
	uid, err := ubluehelper.TargetUID(os.Getenv("PKEXEC_UID"))
	if err != nil {
		fatal(err.Error())
	}

	account, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		fatal(fmt.Sprintf("resolving uid %d: %v", uid, err))
	}

	applied := 0
	for _, group := range ubluehelper.DevGroups() {
		name, args, ok := ubluehelper.GroupArgs(invocation.Command, account.Username, group)
		if !ok {
			fatal(fmt.Sprintf("unsupported group operation for command %q", invocation.Command))
		}

		if invocation.DryRun {
			fmt.Printf("[DRY-RUN] would execute: %s %v\n", name, args)
			applied++
			continue
		}

		if err := run(ctx, name, args...); err != nil {
			// usermod/gpasswd fail on a group this image does not define;
			// report it and carry on with the rest.
			fmt.Fprintf(os.Stderr, "skipped group %s: %v\n", group, err)
			continue
		}
		applied++
	}

	if applied == 0 {
		fatal("no developer groups could be changed")
	}
	fmt.Printf("%s applied to %d group(s) for %s — log out and back in to take effect\n",
		invocation.Command, applied, account.Username)
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

// runAutoUpdates turns the unattended-update timer on or off. Each step must
// succeed: a partial application would leave the timer in a state that does
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

// run executes one privileged command, forwarding its output so the calling
// GUI can surface a real failure message instead of a bare exit code.
func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
