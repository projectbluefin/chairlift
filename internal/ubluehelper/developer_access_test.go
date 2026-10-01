package ubluehelper

import (
	"reflect"
	"testing"
)

func TestDeveloperOptionCommandsRejectCallerControlledTargets(t *testing.T) {
	for _, command := range []string{CommandKVMEnable, CommandDockerEnable, CommandDockerDisable} {
		for _, args := range [][]string{{command}, {command, "--dry-run"}} {
			invocation, err := ParseInvocation(args)
			if err != nil || invocation.Command != command || invocation.DryRun != (len(args) == 2) || invocation.UsesChannelTable() {
				t.Fatalf("fixed invocation %v = %+v %v", args, invocation, err)
			}
		}
		for _, tail := range [][]string{{"root"}, {"wheel"}, {"other.service"}, {"--now"}, {"--dry-run", "root"}, {"--dry-run", "--dry-run"}} {
			args := append([]string{command}, tail...)
			if got, err := ParseInvocation(args); err == nil || got != (Invocation{}) {
				t.Fatalf("accepted caller-controlled argv %v: %+v %v", args, got, err)
			}
		}
	}
}

func TestDeveloperAccessArgsGrantExactlyTheAuthorizedGroup(t *testing.T) {
	for _, test := range []struct{ command, group string }{{CommandKVMEnable, "kvm"}, {CommandDockerEnable, "docker"}} {
		program, args, ok := AccessArgs(test.command, "alice")
		if !ok || program != "usermod" || !reflect.DeepEqual(args, []string{"-aG", test.group, "alice"}) {
			t.Fatalf("AccessArgs(%s) = %s %v %v", test.command, program, args, ok)
		}
	}
	for _, command := range []string{CommandDockerDisable, CommandDXEnable, "wheel"} {
		if _, _, ok := AccessArgs(command, "alice"); ok {
			t.Fatalf("accepted unrelated access command %q", command)
		}
	}
	if _, _, ok := AccessArgs(CommandKVMEnable, ""); ok {
		t.Fatal("empty identity accepted")
	}
}

func TestDockerLifecycleStopsSocketActivationAsWellAsDaemon(t *testing.T) {
	for _, test := range []struct{ command, operation string }{{CommandDockerEnable, "enable"}, {CommandDockerDisable, "disable"}} {
		args, ok := DockerArgs(test.command)
		if !ok || !reflect.DeepEqual(args, []string{test.operation, "--now", "docker.socket", "docker.service"}) {
			t.Fatalf("DockerArgs(%s) = %v %v", test.command, args, ok)
		}
	}
	if _, ok := DockerArgs(CommandKVMEnable); ok {
		t.Fatal("unrelated command accepted")
	}
}
