// Package ubluehelper holds the argv-parsing logic for
// cmd/chairlift-ublue-helper, the privileged helper binary invoked via
// pkexec to perform the Bluefin-family write operations: switching the bootc
// release channel and toggling developer-mode group membership.
//
// It mirrors internal/updexhelper's contract and exists for the same reason:
// PolicyKit selects an action from the executable path and the first
// argument only — it does not validate anything after that — so the helper
// itself is the second, authoritative boundary and must reject every argv
// shape it does not support.
//
// Two inputs are deliberately absent from the argv surface:
//
//   - The target image reference. The helper derives it from
//     /usr/share/ublue-os/image-info.json via internal/imageinfo, so an
//     authenticated caller cannot direct `bootc switch` at an arbitrary
//     registry. Only a channel word crosses the boundary.
//   - The username. The helper resolves it from the PKEXEC_UID pkexec sets
//     on the invoking session, so an authenticated caller cannot add an
//     unrelated account to the privileged developer groups.
//
// The package is free of any puregotk/GTK import so its logic is unit
// testable on a headless host.
package ubluehelper

import (
	"fmt"
	"strconv"

	"github.com/projectbluefin/chairlift/internal/autoupdate"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
)

// The complete first-argument surface of the privileged helper. Each value
// is selected by exactly one PolicyKit action's
// org.freedesktop.policykit.exec.argv1 annotation in
// data/io.projectbluefin.chairlift.ublue.policy.
const (
	CommandChannelSwitch = "channel-switch"
	CommandDXEnable      = "dx-enable"
	CommandDXDisable     = "dx-disable"
	CommandRestart       = "restart"
	CommandRollback      = "rollback"
	CommandAutoEnable    = "auto-updates-enable"
	CommandAutoDisable   = "auto-updates-disable"
	CommandDriverSwitch  = "driver-switch"
	CommandFactoryReset  = "factory-reset"
)

// The channel words accepted as channel-switch's second argument. They are
// the string forms of imageinfo.ChannelStable and imageinfo.ChannelTesting;
// a compile-time-checked conversion below keeps them from drifting.
const (
	ChannelStable  = string(imageinfo.ChannelStable)
	ChannelTesting = string(imageinfo.ChannelTesting)
)

// DevGroups are the supplementary groups developer mode grants. They are
// bluefinctl's DEVMODE_GROUPS (src/bluefinctl/core/devmode.py) unchanged, so
// a host toggled by either tool ends up in the same set. The list is fixed
// in the binary rather than configurable: it is the exact privilege the
// PolicyKit action authorizes, and a user-writable group list would let a
// caller escalate to any group on the system.
func DevGroups() []string {
	return []string{"docker", "incus-admin", "libvirt", "dialout"}
}

// Invocation is a validated privileged-helper command.
type Invocation struct {
	Command string
	Channel imageinfo.Channel
	Driver  imageinfo.Driver
	DryRun  bool
}

// SupportedCommands returns the complete first-argument set accepted by the
// privileged helper, in the order the policy file declares its actions. The
// returned slice is a fresh value so callers cannot mutate the package's
// command surface.
func SupportedCommands() []string {
	return []string{
		CommandChannelSwitch,
		CommandDXEnable,
		CommandDXDisable,
		CommandRestart,
		CommandRollback,
		CommandAutoEnable,
		CommandAutoDisable,
		CommandDriverSwitch,
		CommandFactoryReset,
	}
}

// ParseInvocation accepts only the three argv shapes ChairLift emits:
//
//	channel-switch <stable|testing> [--dry-run]
//	dx-enable [--dry-run]
//	dx-disable [--dry-run]
//	restart [--dry-run]
//	rollback [--dry-run]
//	auto-updates-enable [--dry-run]
//	auto-updates-disable [--dry-run]
//	driver-switch <standard|nvidia|nvidia-open> [--dry-run]
//	factory-reset [--dry-run]
//
// Everything else — extra arguments, a misplaced flag, an unknown channel
// word, an unknown command — is rejected.
func ParseInvocation(args []string) (Invocation, error) {
	if len(args) == 0 {
		return Invocation{}, fmt.Errorf("usage: chairlift-ublue-helper <command> [args...]")
	}

	switch args[0] {
	case CommandChannelSwitch:
		usage := fmt.Errorf("usage: chairlift-ublue-helper %s <%s|%s> [--dry-run]",
			CommandChannelSwitch, ChannelStable, ChannelTesting)
		if len(args) != 2 && len(args) != 3 {
			return Invocation{}, usage
		}
		channel, ok := parseChannel(args[1])
		if !ok {
			return Invocation{}, usage
		}
		dryRun := len(args) == 3
		if dryRun && args[2] != "--dry-run" {
			return Invocation{}, usage
		}
		return Invocation{Command: CommandChannelSwitch, Channel: channel, DryRun: dryRun}, nil

	case CommandDriverSwitch:
		usage := fmt.Errorf("usage: chairlift-ublue-helper %s <%s|%s|%s> [--dry-run]",
			CommandDriverSwitch, imageinfo.DriverStandard, imageinfo.DriverNVIDIA, imageinfo.DriverNVIDIAOpen)
		if len(args) != 2 && len(args) != 3 {
			return Invocation{}, usage
		}
		driver, ok := parseDriver(args[1])
		if !ok {
			return Invocation{}, usage
		}
		dryRun := len(args) == 3
		if dryRun && args[2] != "--dry-run" {
			return Invocation{}, usage
		}
		return Invocation{Command: CommandDriverSwitch, Driver: driver, DryRun: dryRun}, nil

	case CommandDXEnable, CommandDXDisable, CommandRestart, CommandRollback,
		CommandAutoEnable, CommandAutoDisable, CommandFactoryReset:
		if len(args) > 2 || (len(args) == 2 && args[1] != "--dry-run") {
			return Invocation{}, fmt.Errorf("usage: chairlift-ublue-helper %s [--dry-run]", args[0])
		}
		return Invocation{Command: args[0], DryRun: len(args) == 2}, nil

	default:
		return Invocation{}, fmt.Errorf("unknown command: %s", args[0])
	}
}

// parseChannel accepts only the two switchable channel words. It rejects
// imageinfo.ChannelUnknown, which is a classification result rather than a
// switch destination.
func parseChannel(word string) (imageinfo.Channel, bool) {
	switch word {
	case ChannelStable:
		return imageinfo.ChannelStable, true
	case ChannelTesting:
		return imageinfo.ChannelTesting, true
	default:
		return imageinfo.ChannelUnknown, false
	}
}

// RestartArgs returns the argv that restarts the machine.
//
// `systemctl reboot` takes no target, no delay, and no options here: the only
// restart ChairLift performs is an immediate one the user just asked for.
// Scheduled and conditional restarts (finupdate's "Restart Tonight",
// bluefinctl's reboot-on-logout) would each need their own action and their
// own argv validation rather than a parameter on this one, because a
// time argument crossing the pkexec boundary is another value the caller
// would control.
func RestartArgs() []string {
	return []string{"reboot"}
}

// parseDriver accepts only the three published graphics-driver words. An
// arbitrary suffix would let a caller name any image in the registry
// namespace, which is exactly what keeping the reference out of argv
// prevents.
func parseDriver(word string) (imageinfo.Driver, bool) {
	switch imageinfo.Driver(word) {
	case imageinfo.DriverStandard:
		return imageinfo.DriverStandard, true
	case imageinfo.DriverNVIDIA:
		return imageinfo.DriverNVIDIA, true
	case imageinfo.DriverNVIDIAOpen:
		return imageinfo.DriverNVIDIAOpen, true
	default:
		return "", false
	}
}

// DriverSwitchArgs returns the complete `bootc` argv for moving this host to
// the requested graphics-driver image, keeping the current stream. ok is
// false when that image is not published for the running image and stream —
// every LTS host asking for NVIDIA, for one — so the helper refuses rather
// than staging a switch that would fail to pull.
//
// It carries the same --enforce-container-sigpolicy as a channel switch: a
// driver switch is no less a change of the booted image.
func DriverSwitchArgs(info imageinfo.Info, driver imageinfo.Driver) ([]string, bool) {
	target, ok := imageinfo.DriverTarget(info.CleanRef(), info.EffectiveTag(), driver)
	if !ok {
		return nil, false
	}
	return []string{"switch", "--enforce-container-sigpolicy", target}, true
}

// RollbackArgs returns the `bootc` argv that makes the previous deployment
// the default for the next boot.
//
// Like RestartArgs it takes no caller-supplied value: `bootc rollback`
// operates on the rollback deployment the host already records, so there is
// no target for a caller to influence. Rolling back to an arbitrary earlier
// image is a different operation — it is a switch to a pinned reference, and
// belongs to the channel-switch action's validation, not here.
func RollbackArgs() []string {
	return []string{"rollback"}
}

// FactoryResetArgs returns the argv that replaces the running deployment
// with a fresh install of the same image, discarding every local change:
// `bootc install reset`.
//
// `--experimental` is required by bootc itself — this reset path is not
// stabilized upstream — and ChairLift does not hide that from the argv or
// from the confirmation dialog that must be shown before this ever runs
// (pageview.FactoryResetConfirmation). `--apply` makes the reset take effect
// immediately rather than only staging it, because a "staged" factory reset
// that silently applies at the next unrelated restart is a worse surprise
// than the operation itself. Like Restart and Rollback this takes no
// caller-supplied value: a factory reset has exactly one target, the image
// already booted.
func FactoryResetArgs() []string {
	return []string{"install", "reset", "--experimental", "--apply"}
}

// AutoUpdateArgs returns the ordered systemctl argv lists that turn automatic
// background updates on or off. ok is false for any other command.
//
// Enabling takes two steps because "off" has two representations on disk: a
// disabled timer and a masked one. bluefinctl's "manual" strategy and its
// "focus mode" both mask the unit, so enabling must unmask before it can
// enable, or a machine that had ever been set to manual would silently refuse
// to turn automatic updates back on. Disabling masks rather than merely
// disabling, so that a package upgrade re-running `systemctl preset` cannot
// quietly re-enable something the user turned off.
//
// The unit name is fixed to autoupdate.TimerUnit rather than passed in: a
// caller-supplied unit would let an authenticated user enable or mask any
// systemd unit on the machine.
func AutoUpdateArgs(command string) ([][]string, bool) {
	switch command {
	case CommandAutoEnable:
		return [][]string{
			{"unmask", autoupdate.TimerUnit},
			{"enable", "--now", autoupdate.TimerUnit},
		}, true
	case CommandAutoDisable:
		return [][]string{
			{"disable", "--now", autoupdate.TimerUnit},
			{"mask", autoupdate.TimerUnit},
		}, true
	default:
		return nil, false
	}
}

// SwitchArgs returns the complete `bootc` argv for switching this host to
// the invocation's channel, given the running image descriptor. ok is false
// when the descriptor offers no counterpart tag, which the helper reports as
// a refusal rather than executing a guessed reference.
//
// --enforce-container-sigpolicy is included deliberately. bluefinctl's two
// call sites disagree — its `toggle-testing` CLI command passes the flag
// while its TUI channel switch does not — and ChairLift takes the stricter
// one, so a channel switch cannot silently drop signature enforcement.
func SwitchArgs(info imageinfo.Info, channel imageinfo.Channel) ([]string, bool) {
	target, ok := info.SwitchTarget(channel)
	if !ok {
		return nil, false
	}
	return []string{"switch", "--enforce-container-sigpolicy", target}, true
}

// TargetUID returns the numeric uid the helper must act on, parsed from the
// PKEXEC_UID environment variable pkexec sets to the *invoking* session's
// uid. Reading the identity from the environment pkexec controls — rather
// than from argv, which the caller controls — is what keeps
// `pkexec chairlift-ublue-helper dx-enable` from being usable to add an
// arbitrary account to the privileged developer groups.
//
// An absent, empty, non-numeric, or negative value is rejected. uid 0 is
// rejected too: adding root to the developer groups is never the intent, and
// a PKEXEC_UID of 0 means the helper was invoked outside a pkexec session.
func TargetUID(pkexecUID string) (int, error) {
	if pkexecUID == "" {
		return 0, fmt.Errorf("PKEXEC_UID is not set; this helper must be invoked through pkexec")
	}
	uid, err := strconv.Atoi(pkexecUID)
	if err != nil {
		return 0, fmt.Errorf("PKEXEC_UID %q is not a number", pkexecUID)
	}
	if uid <= 0 {
		return 0, fmt.Errorf("PKEXEC_UID %d is not an unprivileged user", uid)
	}
	return uid, nil
}

// GroupArgs returns the argv for adding or removing username from group.
// Adding uses `usermod -aG` and removing uses `gpasswd -d`, matching
// bluefinctl's enable_devmode/disable_devmode exactly. ok is false for any
// command other than the two dx subcommands, and for an empty username or
// group, so a caller cannot turn this into a general-purpose group editor.
func GroupArgs(command, username, group string) (string, []string, bool) {
	if username == "" || group == "" {
		return "", nil, false
	}
	switch command {
	case CommandDXEnable:
		return "usermod", []string{"-aG", group, username}, true
	case CommandDXDisable:
		return "gpasswd", []string{"-d", username, group}, true
	default:
		return "", nil, false
	}
}
