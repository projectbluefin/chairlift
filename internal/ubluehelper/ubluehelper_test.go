package ubluehelper

import (
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/autoupdate"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
)

func TestParseInvocationAcceptsSupportedShapes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Invocation
	}{
		{
			name: "channel switch to testing",
			args: []string{"channel-switch", "testing"},
			want: Invocation{Command: CommandChannelSwitch, Channel: imageinfo.ChannelTesting},
		},
		{
			name: "channel switch to stable",
			args: []string{"channel-switch", "stable"},
			want: Invocation{Command: CommandChannelSwitch, Channel: imageinfo.ChannelStable},
		},
		{
			name: "channel switch dry run",
			args: []string{"channel-switch", "testing", "--dry-run"},
			want: Invocation{Command: CommandChannelSwitch, Channel: imageinfo.ChannelTesting, DryRun: true},
		},
		{
			name: "dx enable",
			args: []string{"dx-enable"},
			want: Invocation{Command: CommandDXEnable},
		},
		{
			name: "dx enable dry run",
			args: []string{"dx-enable", "--dry-run"},
			want: Invocation{Command: CommandDXEnable, DryRun: true},
		},
		{
			name: "dx disable",
			args: []string{"dx-disable"},
			want: Invocation{Command: CommandDXDisable},
		},
		{
			name: "dx disable dry run",
			args: []string{"dx-disable", "--dry-run"},
			want: Invocation{Command: CommandDXDisable, DryRun: true},
		},
		{
			name: "restart",
			args: []string{"restart"},
			want: Invocation{Command: CommandRestart},
		},
		{
			name: "restart dry run",
			args: []string{"restart", "--dry-run"},
			want: Invocation{Command: CommandRestart, DryRun: true},
		},
		{
			name: "rollback",
			args: []string{"rollback"},
			want: Invocation{Command: CommandRollback},
		},
		{
			name: "rollback dry run",
			args: []string{"rollback", "--dry-run"},
			want: Invocation{Command: CommandRollback, DryRun: true},
		},
		{
			name: "factory reset",
			args: []string{"factory-reset"},
			want: Invocation{Command: CommandFactoryReset},
		},
		{
			name: "factory reset dry run",
			args: []string{"factory-reset", "--dry-run"},
			want: Invocation{Command: CommandFactoryReset, DryRun: true},
		},
		{
			name: "automatic updates on",
			args: []string{"auto-updates-enable"},
			want: Invocation{Command: CommandAutoEnable},
		},
		{
			name: "automatic updates off",
			args: []string{"auto-updates-disable"},
			want: Invocation{Command: CommandAutoDisable},
		},
		{
			name: "automatic updates dry run",
			args: []string{"auto-updates-enable", "--dry-run"},
			want: Invocation{Command: CommandAutoEnable, DryRun: true},
		},
		{
			name: "driver switch to nvidia",
			args: []string{"driver-switch", "nvidia"},
			want: Invocation{Command: CommandDriverSwitch, Driver: imageinfo.DriverNVIDIA},
		},
		{
			name: "driver switch to open modules",
			args: []string{"driver-switch", "nvidia-open"},
			want: Invocation{Command: CommandDriverSwitch, Driver: imageinfo.DriverNVIDIAOpen},
		},
		{
			name: "driver switch back to standard",
			args: []string{"driver-switch", "standard", "--dry-run"},
			want: Invocation{Command: CommandDriverSwitch, Driver: imageinfo.DriverStandard, DryRun: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseInvocation(test.args)
			if err != nil {
				t.Fatalf("ParseInvocation(%v) error = %v, want nil", test.args, err)
			}
			if got != test.want {
				t.Errorf("ParseInvocation(%v) = %+v, want %+v", test.args, got, test.want)
			}
		})
	}
}

// pkexec authenticates the action, not the arguments after argv1, so every
// shape below reaches a root process. The helper is the boundary that must
// reject them.
func TestParseInvocationRejectsEveryUnsupportedShape(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no arguments", args: nil},
		{name: "empty command", args: []string{""}},
		{name: "unknown command", args: []string{"powerwash"}},
		{name: "channel switch without a channel", args: []string{"channel-switch"}},
		{name: "channel switch with unknown channel", args: []string{"channel-switch", "unknown"}},
		{name: "channel switch with an image ref", args: []string{"channel-switch", "ghcr.io/evil/image:latest"}},
		{name: "channel switch with empty channel", args: []string{"channel-switch", ""}},
		{name: "channel switch with flag as channel", args: []string{"channel-switch", "--dry-run"}},
		{name: "channel switch with misplaced flag", args: []string{"channel-switch", "testing", "--force"}},
		{name: "channel switch with extra argument", args: []string{"channel-switch", "testing", "--dry-run", "extra"}},
		{name: "dx enable with a username", args: []string{"dx-enable", "root"}},
		{name: "dx enable with extra argument", args: []string{"dx-enable", "--dry-run", "extra"}},
		{name: "dx disable with a group", args: []string{"dx-disable", "wheel"}},
		{name: "uppercase command", args: []string{"DX-ENABLE"}},
		// A delay or target crossing the boundary would be a value the
		// caller controls; restart takes neither.
		{name: "restart with a delay", args: []string{"restart", "02:00"}},
		{name: "restart with a target", args: []string{"restart", "--force"}},
		{name: "restart with extra argument", args: []string{"restart", "--dry-run", "now"}},
		// Rolling back to an arbitrary image is a channel switch, not this
		// operation; a target here must be rejected outright.
		{name: "rollback with a target image", args: []string{"rollback", "ghcr.io/evil/image:old"}},
		{name: "rollback with a deployment index", args: []string{"rollback", "1"}},
		{name: "rollback with extra argument", args: []string{"rollback", "--dry-run", "1"}},
		{name: "factory reset with a flag", args: []string{"factory-reset", "--force"}},
		{name: "factory reset with extra argument", args: []string{"factory-reset", "--dry-run", "now"}},
		// A caller-supplied unit would let an authenticated user enable or
		// mask any systemd unit on the machine.
		{name: "auto updates with a unit name", args: []string{"auto-updates-enable", "sshd.service"}},
		{name: "auto updates with a flag", args: []string{"auto-updates-disable", "--now"}},
		// An arbitrary suffix would name any image in the registry
		// namespace, which is what keeping the reference out of argv
		// prevents.
		{name: "driver switch without a driver", args: []string{"driver-switch"}},
		{name: "driver switch with an unknown driver", args: []string{"driver-switch", "nouveau"}},
		{name: "driver switch with an image ref", args: []string{"driver-switch", "ghcr.io/evil/image:latest"}},
		{name: "driver switch with a suffix", args: []string{"driver-switch", "asus"}},
		{name: "driver switch with extra argument", args: []string{"driver-switch", "nvidia", "--dry-run", "now"}},
		{name: "updex command is not accepted here", args: []string{"enable-feature", "demo"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseInvocation(test.args)
			if err == nil {
				t.Fatalf("ParseInvocation(%v) = %+v, want an error", test.args, got)
			}
			if got != (Invocation{}) {
				t.Errorf("ParseInvocation(%v) = %+v on error, want the zero Invocation", test.args, got)
			}
		})
	}
}

func TestSupportedCommandsMatchesParser(t *testing.T) {
	commands := SupportedCommands()
	want := []string{
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
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("SupportedCommands() = %v, want %v", commands, want)
	}

	// A command advertised here but not parseable would be an action the
	// policy authorizes and the helper then rejects.
	for _, command := range commands {
		args := []string{command}
		switch command {
		case CommandChannelSwitch:
			args = append(args, ChannelTesting)
		case CommandDriverSwitch:
			args = append(args, string(imageinfo.DriverNVIDIA))
		}
		if _, err := ParseInvocation(args); err != nil {
			t.Errorf("ParseInvocation(%v) error = %v, want nil for advertised command", args, err)
		}
	}

	// The returned slice must not alias package state.
	commands[0] = "mutated"
	if SupportedCommands()[0] != CommandChannelSwitch {
		t.Error("SupportedCommands() returned an aliased slice")
	}
}

func TestDriverSwitchArgsBuildPublishedReferencesOnly(t *testing.T) {
	tests := []struct {
		name   string
		info   imageinfo.Info
		driver imageinfo.Driver
		want   []string
		wantOK bool
	}{
		{
			name:   "dakota to nvidia",
			info:   imageinfo.Info{Name: "dakota", Tag: "latest", Ref: "docker://ghcr.io/projectbluefin/dakota"},
			driver: imageinfo.DriverNVIDIA,
			want:   []string{"switch", "--enforce-container-sigpolicy", "ghcr.io/projectbluefin/dakota-nvidia:latest"},
			wantOK: true,
		},
		{
			name:   "bluefin back to standard",
			info:   imageinfo.Info{Name: "bluefin", Tag: "stable", Ref: "docker://ghcr.io/ublue-os/bluefin-nvidia"},
			driver: imageinfo.DriverStandard,
			want:   []string{"switch", "--enforce-container-sigpolicy", "ghcr.io/ublue-os/bluefin:stable"},
			wantOK: true,
		},
		{
			// The reference that must never be produced: the driver images
			// are not published for the LTS streams.
			name:   "lts cannot reach nvidia",
			info:   imageinfo.Info{Name: "bluefin", Tag: "lts", Ref: "docker://ghcr.io/ublue-os/bluefin"},
			driver: imageinfo.DriverNVIDIA,
			wantOK: false,
		},
		{
			name:   "unknown image",
			info:   imageinfo.Info{Name: "custom", Tag: "latest", Ref: "docker://ghcr.io/someone/custom"},
			driver: imageinfo.DriverNVIDIA,
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := DriverSwitchArgs(test.info, test.driver)
			if ok != test.wantOK {
				t.Fatalf("DriverSwitchArgs() ok = %v, want %v", ok, test.wantOK)
			}
			if !ok {
				if got != nil {
					t.Errorf("DriverSwitchArgs() = %v on refusal, want nil", got)
				}
				return
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("DriverSwitchArgs() = %v, want %v", got, test.want)
			}
			if !containsArg(got, "--enforce-container-sigpolicy") {
				t.Error("a driver switch changes the booted image and must enforce the signature policy")
			}
		})
	}
}

func TestParseDriverAcceptsOnlyPublishedFlavours(t *testing.T) {
	for _, word := range []string{"standard", "nvidia", "nvidia-open"} {
		if _, ok := parseDriver(word); !ok {
			t.Errorf("parseDriver(%q) ok = false, want true", word)
		}
	}
	for _, word := range []string{"", "asus", "surface", "dx", "nouveau", "NVIDIA", "ghcr.io/x/y"} {
		if driver, ok := parseDriver(word); ok {
			t.Errorf("parseDriver(%q) = (%q, true), want a refusal", word, driver)
		}
	}
}

func TestRestartArgsTakeNoCallerControlledValues(t *testing.T) {
	args := RestartArgs()
	if !reflect.DeepEqual(args, []string{"reboot"}) {
		t.Fatalf("RestartArgs() = %v, want [reboot]", args)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, ":") || strings.Contains(arg, "/") {
			t.Errorf("RestartArgs() carries %q; the restart argv must be fixed", arg)
		}
	}
}

// FactoryResetArgs is the one helper argv that legitimately carries flags —
// --experimental and --apply are fixed, not caller-supplied, so they are not
// the caller-controlled-value risk the other Args functions guard against.
// What must still never appear is a registry reference or an image path,
// since a factory reset takes exactly one target: the image already booted.
func TestFactoryResetArgsAreFixedAndCarryNoTarget(t *testing.T) {
	args := FactoryResetArgs()
	want := []string{"install", "reset", "--experimental", "--apply"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("FactoryResetArgs() = %v, want %v", args, want)
	}
	for _, arg := range args {
		if strings.Contains(arg, "/") || strings.Contains(arg, ":") {
			t.Errorf("FactoryResetArgs() carries %q; a factory reset must not name a target", arg)
		}
	}
	// bootc itself requires --experimental for this reset path; ChairLift
	// must not hide that behind a shorter, friendlier-looking argv.
	found := false
	for _, arg := range args {
		if arg == "--experimental" {
			found = true
		}
	}
	if !found {
		t.Error("FactoryResetArgs() does not carry --experimental")
	}
}

func TestRollbackArgsTakeNoCallerControlledValues(t *testing.T) {
	args := RollbackArgs()
	if !reflect.DeepEqual(args, []string{"rollback"}) {
		t.Fatalf("RollbackArgs() = %v, want [rollback]", args)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "/") || strings.Contains(arg, ":") {
			t.Errorf("RollbackArgs() carries %q; the rollback argv must be fixed", arg)
		}
	}
}

// "Off" has two on-disk representations, so enabling must unmask before it
// enables — otherwise a machine ever set to bluefinctl's manual strategy or
// focus mode would silently refuse to turn automatic updates back on.
func TestAutoUpdateArgsHandleBothRepresentationsOfOff(t *testing.T) {
	enable, ok := AutoUpdateArgs(CommandAutoEnable)
	if !ok {
		t.Fatal("AutoUpdateArgs(enable) ok = false, want true")
	}
	want := [][]string{
		{"unmask", autoupdate.TimerUnit},
		{"enable", "--now", autoupdate.TimerUnit},
	}
	if !reflect.DeepEqual(enable, want) {
		t.Errorf("AutoUpdateArgs(enable) = %v, want %v", enable, want)
	}

	disable, ok := AutoUpdateArgs(CommandAutoDisable)
	if !ok {
		t.Fatal("AutoUpdateArgs(disable) ok = false, want true")
	}
	// Masking, not merely disabling, so a `systemctl preset` run during a
	// package upgrade cannot quietly re-enable what the user turned off.
	wantDisable := [][]string{
		{"disable", "--now", autoupdate.TimerUnit},
		{"mask", autoupdate.TimerUnit},
	}
	if !reflect.DeepEqual(disable, wantDisable) {
		t.Errorf("AutoUpdateArgs(disable) = %v, want %v", disable, wantDisable)
	}

	for _, command := range []string{CommandRestart, CommandRollback, CommandChannelSwitch, "systemctl"} {
		if _, ok := AutoUpdateArgs(command); ok {
			t.Errorf("AutoUpdateArgs(%q) ok = true, want false", command)
		}
	}
}

// Every step must name only the unattended-update timer.
func TestAutoUpdateArgsTouchOnlyTheUpdateTimer(t *testing.T) {
	for _, command := range []string{CommandAutoEnable, CommandAutoDisable} {
		steps, ok := AutoUpdateArgs(command)
		if !ok {
			t.Fatalf("AutoUpdateArgs(%q) ok = false", command)
		}
		for _, args := range steps {
			named := false
			for _, arg := range args {
				if arg == autoupdate.TimerUnit {
					named = true
					continue
				}
				if strings.Contains(arg, ".service") || strings.Contains(arg, ".timer") {
					t.Errorf("AutoUpdateArgs(%q) touches unit %q", command, arg)
				}
			}
			if !named {
				t.Errorf("AutoUpdateArgs(%q) step %v does not name %s", command, args, autoupdate.TimerUnit)
			}
		}
	}
}

func TestDevGroupsMatchBluefinctl(t *testing.T) {
	groups := DevGroups()
	want := []string{"docker", "incus-admin", "libvirt", "dialout"}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("DevGroups() = %v, want %v (bluefinctl DEVMODE_GROUPS)", groups, want)
	}

	groups[0] = "wheel"
	if DevGroups()[0] != "docker" {
		t.Error("DevGroups() returned an aliased slice — a caller could widen the privilege it grants")
	}
}

func TestSwitchArgsEnforcesSignaturePolicy(t *testing.T) {
	tests := []struct {
		name    string
		info    imageinfo.Info
		channel imageinfo.Channel
		want    []string
		wantOK  bool
	}{
		{
			name:    "dakota to testing",
			info:    imageinfo.Info{Name: "dakota", Tag: "latest", Ref: "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota"},
			channel: imageinfo.ChannelTesting,
			want:    []string{"switch", "--enforce-container-sigpolicy", "ghcr.io/projectbluefin/dakota:testing"},
			wantOK:  true,
		},
		{
			name:    "bluefin on the lts stream to testing",
			info:    imageinfo.Info{Name: "bluefin", Tag: "lts", Ref: "docker://ghcr.io/ublue-os/bluefin"},
			channel: imageinfo.ChannelTesting,
			want:    []string{"switch", "--enforce-container-sigpolicy", "ghcr.io/ublue-os/bluefin:lts-testing"},
			wantOK:  true,
		},
		{
			// Bluefin Stable publishes no testing tag, so there is nothing
			// to hand bootc. See internal/imageinfo's channel table.
			name:    "bluefin stable has nowhere to switch",
			info:    imageinfo.Info{Name: "bluefin", Tag: "stable", Ref: "docker://ghcr.io/ublue-os/bluefin"},
			channel: imageinfo.ChannelTesting,
			wantOK:  false,
		},
		{
			name:    "bluefin lts to testing",
			info:    imageinfo.Info{Name: "bluefin-lts", Tag: "lts", Ref: "docker://ghcr.io/projectbluefin/bluefin-lts"},
			channel: imageinfo.ChannelTesting,
			want:    []string{"switch", "--enforce-container-sigpolicy", "ghcr.io/projectbluefin/bluefin-lts:testing"},
			wantOK:  true,
		},
		{
			name:    "bluefin lts back to stable",
			info:    imageinfo.Info{Name: "bluefin-lts", Tag: "testing", Ref: "docker://ghcr.io/projectbluefin/bluefin-lts"},
			channel: imageinfo.ChannelStable,
			want:    []string{"switch", "--enforce-container-sigpolicy", "ghcr.io/projectbluefin/bluefin-lts:stable"},
			wantOK:  true,
		},
		{
			name:    "pinned build has no counterpart",
			info:    imageinfo.Info{Name: "dakota", Tag: "20260817", Ref: "docker://ghcr.io/projectbluefin/dakota"},
			channel: imageinfo.ChannelTesting,
			wantOK:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := SwitchArgs(test.info, test.channel)
			if ok != test.wantOK {
				t.Fatalf("SwitchArgs() ok = %v, want %v", ok, test.wantOK)
			}
			if !ok {
				if got != nil {
					t.Errorf("SwitchArgs() = %v on refusal, want nil", got)
				}
				return
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("SwitchArgs() = %v, want %v", got, test.want)
			}
		})
	}
}

// The signature-enforcement flag is the one place ChairLift resolves a
// divergence between bluefinctl's own two call sites, so assert it directly
// rather than only through the table above.
func TestSwitchArgsAlwaysPassesEnforceFlag(t *testing.T) {
	info := imageinfo.Info{Name: "dakota", Tag: "latest", Ref: "docker://ghcr.io/projectbluefin/dakota"}
	for _, channel := range []imageinfo.Channel{imageinfo.ChannelTesting} {
		args, ok := SwitchArgs(info, channel)
		if !ok {
			t.Fatalf("SwitchArgs(%q) ok = false, want true", channel)
		}
		if !containsArg(args, "--enforce-container-sigpolicy") {
			t.Errorf("SwitchArgs(%q) = %v, want it to include --enforce-container-sigpolicy", channel, args)
		}
		if args[0] != "switch" {
			t.Errorf("SwitchArgs(%q)[0] = %q, want %q", channel, args[0], "switch")
		}
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") && arg != "--enforce-container-sigpolicy" {
				t.Errorf("SwitchArgs(%q) carries unexpected flag %q", channel, arg)
			}
		}
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestTargetUIDAcceptsOnlyUnprivilegedPkexecSessions(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "ordinary user", value: "1000", want: 1000},
		{name: "high uid", value: "65534", want: 65534},
		{name: "unset", value: "", wantErr: true},
		{name: "root", value: "0", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
		{name: "not a number", value: "james", wantErr: true},
		{name: "trailing text", value: "1000x", wantErr: true},
		{name: "whitespace", value: " 1000", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := TargetUID(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("TargetUID(%q) = %d, want an error", test.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("TargetUID(%q) error = %v, want nil", test.value, err)
			}
			if got != test.want {
				t.Errorf("TargetUID(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestGroupArgsMatchBluefinctlCommands(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		username string
		group    string
		wantName string
		wantArgs []string
		wantOK   bool
	}{
		{
			name:     "enable uses usermod append",
			command:  CommandDXEnable,
			username: "james",
			group:    "docker",
			wantName: "usermod",
			wantArgs: []string{"-aG", "docker", "james"},
			wantOK:   true,
		},
		{
			name:     "disable uses gpasswd delete",
			command:  CommandDXDisable,
			username: "james",
			group:    "libvirt",
			wantName: "gpasswd",
			wantArgs: []string{"-d", "james", "libvirt"},
			wantOK:   true,
		},
		{name: "channel switch is not a group operation", command: CommandChannelSwitch, username: "james", group: "docker"},
		{name: "unknown command", command: "adduser", username: "james", group: "docker"},
		{name: "empty username", command: CommandDXEnable, group: "docker"},
		{name: "empty group", command: CommandDXEnable, username: "james"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name, args, ok := GroupArgs(test.command, test.username, test.group)
			if ok != test.wantOK {
				t.Fatalf("GroupArgs() ok = %v, want %v", ok, test.wantOK)
			}
			if !ok {
				if name != "" || args != nil {
					t.Errorf("GroupArgs() = (%q, %v) on refusal, want (\"\", nil)", name, args)
				}
				return
			}
			if name != test.wantName {
				t.Errorf("GroupArgs() name = %q, want %q", name, test.wantName)
			}
			if !reflect.DeepEqual(args, test.wantArgs) {
				t.Errorf("GroupArgs() args = %v, want %v", args, test.wantArgs)
			}
		})
	}
}
