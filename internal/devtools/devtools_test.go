package devtools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func fakeLima(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	if err := os.WriteFile(filepath.Join(dir, "limactl"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$LIMA_TEST_LOG\"\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("LIMA_TEST_LOG", log)
	return log
}

func TestWSLDisableStopsOnlyUbuntuAndPreservesDataOnFailure(t *testing.T) {
	log := fakeLima(t, "case \"$1\" in\nlist) echo '{\"name\":\"ubuntu\",\"status\":\"Running\"}' ;;\nautostart) exit 7 ;;\nstop) echo 'stop failed' >&2; exit 8 ;;\nesac\n")
	t.Setenv("HOME", t.TempDir())
	data := filepath.Join(os.Getenv("HOME"), ".lima", "ubuntu", "disk")
	if err := os.MkdirAll(filepath.Dir(data), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(data, []byte("project data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetWSL(context.Background(), BackendLima, false, nil); err == nil {
		t.Fatal("failed stop must not report WSL disabled")
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "autostart disable ubuntu") || !strings.Contains(string(calls), "stop --tty=false ubuntu") {
		t.Fatalf("must attempt both disable steps, got %s", calls)
	}
	if strings.Contains(string(calls), "delete") {
		t.Fatalf("disable deletes VM: %s", calls)
	}
	if b, err := os.ReadFile(data); err != nil || string(b) != "project data" {
		t.Fatalf("disable lost data: %q %v", b, err)
	}
	state, err := WSLStatus(context.Background(), BackendLima)
	if err != nil || !state.Running {
		t.Fatalf("failed stop must remain running: %+v %v", state, err)
	}
}

func TestWSLReadinessRequiresShellNotRunningWord(t *testing.T) {
	fakeLima(t, "case \"$1\" in\nlist) echo '{\"name\":\"ubuntu\",\"status\":\"Running\"}' ;;\nshell) exit 1 ;;\nesac\n")
	state, err := WSLStatus(context.Background(), BackendLima)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running || state.Ready {
		t.Fatalf("running inaccessible guest must not claim ready: %+v", state)
	}
}

func TestWSLDryRunWritesNoSSHConfigurationOrVM(t *testing.T) {
	log := fakeLima(t, "exit 0\n")
	home := t.TempDir()
	t.Setenv("HOME", home)
	previous := dryrun.Enabled()
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(previous) })
	if err := SetWSL(context.Background(), BackendLima, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote SSH directory: %v", err)
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run ran Lima: %v", err)
	}
}

func TestSSHIncludePrecedesWildcardAndPreservesUserConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "Host *\n  ServerAliveInterval 10\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := wireSSH(); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "Include ~/.lima/*/ssh.config\n\n"+original {
		t.Fatalf("include ordering or preservation broken: %q", b)
	}
}

func TestLimaListingDoesNotTreatMalformedStateAsAbsent(t *testing.T) {
	fakeLima(t, "echo 'not-json'\n")
	if _, err := WSLStatus(context.Background(), BackendLima); err == nil {
		t.Fatal("malformed listing treated as no VM")
	}
}

func TestEmptyLimaListingIgnoresSuccessfulStderrWarnings(t *testing.T) {
	fakeLima(t, "echo 'WARN No instance found. Run limactl create to create an instance.' >&2\nexit 0\n")
	state, err := WSLStatus(context.Background(), BackendLima)
	if err != nil || state.Exists || state.Running || state.Ready {
		t.Fatalf("empty inventory warning must not block first setup: %+v %v", state, err)
	}
}

func fakeNSL(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	if err := os.WriteFile(filepath.Join(dir, "nsl"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$NSL_TEST_LOG\"\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("NSL_TEST_LOG", log)
	return log
}

func TestParseNSLDoctorCases(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantAction string
		wantKVM    bool
	}{
		{
			name: "clean output",
			output: `OK /usr/bin/systemd-vmspawn
OK /usr/bin/systemd-run
OK /usr/bin/systemctl
OK /usr/bin/qemu-system-x86_64
OK /usr/bin/qemu-img
OK /usr/bin/ssh
OK /usr/bin/ssh-keygen
OK /usr/bin/getent
OK /usr/bin/id
OK /usr/bin/sg
OK /usr/bin/unshare
OK /usr/libexec/virtiofsd
OK /usr/lib/systemd/systemd-ssh-proxy
OK ovmf
OK /dev/kvm
OK /dev/vhost-vsock
OK kvm group membership
GUI /usr/bin/waypipe
`,
			wantAction: "",
			wantKVM:    false,
		},
		{
			name: "missing tools",
			output: `MISSING systemd-vmspawn
MISSING /usr/libexec/virtiofsd
OK /usr/bin/systemctl
`,
			wantAction: NeedsPrerequisite,
			wantKVM:    false,
		},
		{
			name: "missing ovmf firmware",
			output: `OK /usr/bin/systemd-vmspawn
MISSING UEFI firmware (vmspawn found no x86_64 firmware without Secure Boot; install ovmf)
`,
			wantAction: NeedsPrerequisite,
			wantKVM:    false,
		},
		{
			name: "missing kvm group",
			output: `OK /usr/bin/systemd-vmspawn
MISSING you are not a member of the kvm group
`,
			wantAction: NeedsKVMAccess,
			wantKVM:    true,
		},
		{
			name: "kvm device access failed",
			output: `OK /usr/bin/systemd-vmspawn
OK kvm group membership
KVM/vsock group access: permission denied
`,
			wantAction: NeedsKVMAccess,
			wantKVM:    true,
		},
		{
			name: "user namespaces",
			output: `OK /usr/bin/systemd-vmspawn
User namespaces: operation not permitted
`,
			wantAction: NeedsPrerequisite,
			wantKVM:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAction, gotKVM := ParseNSLDoctor(tt.output)
			if gotAction != tt.wantAction {
				t.Errorf("ParseNSLDoctor() actionable = %q, want %q", gotAction, tt.wantAction)
			}
			if gotKVM != tt.wantKVM {
				t.Errorf("ParseNSLDoctor() isKVM = %v, want %v", gotKVM, tt.wantKVM)
			}
			// The row shows this text: it must not name programs, devices,
			// or commands a person cannot act on.
			for _, jargon := range []string{"nsl", "/", "systemd", "kvm", "ovmf"} {
				if strings.Contains(gotAction, jargon) {
					t.Errorf("ParseNSLDoctor() actionable = %q names %q", gotAction, jargon)
				}
			}
		})
	}
}

func TestPrerequisiteErrorKeepsRawErrorAndPlainMessage(t *testing.T) {
	raw := errors.New("nsl [doctor]: exit status 1: MISSING ovmf")
	err := error(&PrerequisiteError{Message: NeedsPrerequisite, Err: raw})
	var prerequisite *PrerequisiteError
	if !errors.As(err, &prerequisite) || prerequisite.Message != NeedsPrerequisite {
		t.Fatalf("errors.As(*PrerequisiteError) = %+v", prerequisite)
	}
	if !errors.Is(err, raw) || err.Error() != raw.Error() {
		t.Fatalf("PrerequisiteError lost the raw error: %v", err)
	}
}

func TestParseNSLListCases(t *testing.T) {
	t.Run("empty output", func(t *testing.T) {
		output := "No nsl VM yet\nNo machines; create one with nsl create NAME --distro DISTRO:RELEASE\n"
		state := ParseNSLList(output)
		if state.Exists || state.Running || state.Ready {
			t.Fatalf("empty list returned non-empty state: %+v", state)
		}
	})

	t.Run("stopped machine", func(t *testing.T) {
		output := `VM  STATE  IMAGE  RESOURCES  DATA DISK
shared  stopped  debian:13  -  20 GiB

MACHINE  STATE  IMAGE  TIER  DEFAULT
debian  stopped  debian:13  shared  *
`
		state := ParseNSLList(output)
		if !state.Exists || state.Running {
			t.Fatalf("stopped machine parsed wrong: %+v", state)
		}
	})

	t.Run("running machine", func(t *testing.T) {
		output := `VM  STATE  IMAGE  RESOURCES  DATA DISK
shared  running  debian:13  4 CPUs, 8 GiB  20 GiB

MACHINE  STATE  IMAGE  TIER  DEFAULT
debian  running  debian:13  shared  *
`
		state := ParseNSLList(output)
		if !state.Exists || !state.Running {
			t.Fatalf("running machine parsed wrong: %+v", state)
		}
	})
}

// WSL Mode manages the ubuntu machine, or the debian machine an older
// ChairLift created; the shared VM and machines with other names are not its.
func TestParseNSLListPicksTheManagedMachine(t *testing.T) {
	header := "VM  STATE  IMAGE  RESOURCES  DATA DISK\nshared  running  ubuntu:26.04  4 CPUs, 8 GiB  20 GiB\n\nMACHINE  STATE  IMAGE  TIER  DEFAULT\n"
	for _, tc := range []struct {
		name     string
		machines string
		want     WSLState
	}{
		{"ubuntu only", "ubuntu  running  ubuntu:26.04  shared  *\n", WSLState{Exists: true, Running: true, Machine: "ubuntu"}},
		{"legacy debian only", "debian  stopped  debian:13  shared  *\n", WSLState{Exists: true, Machine: "debian"}},
		{"ubuntu preferred over debian", "debian  running  debian:13  shared  *\nubuntu  stopped  ubuntu:26.04  shared\n", WSLState{Exists: true, Machine: "ubuntu"}},
		{"user's own machine is not WSL Mode's", "fedora  running  fedora:42  shared  *\n", WSLState{}},
		{"running VM without machines", "", WSLState{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseNSLList(header + tc.machines); got != tc.want {
				t.Fatalf("ParseNSLList = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestEngineSubtitleNamesALegacyDebianMachine(t *testing.T) {
	if got := EngineSubtitle("debian"); !strings.Contains(got, "Debian") {
		t.Fatalf("EngineSubtitle(debian) = %q, want it to say the built-in engine runs Debian", got)
	}
	for _, machine := range []string{"", "ubuntu"} {
		if got := EngineSubtitle(machine); got != "Both engines run Ubuntu." {
			t.Fatalf("EngineSubtitle(%q) = %q", machine, got)
		}
	}
}

// A host whose WSL Mode machine is the debian one an older ChairLift created
// must start and probe that machine, never create ubuntu beside it or start a
// machine that does not exist.
func TestNSLStartUsesTheExistingLegacyMachine(t *testing.T) {
	log := fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' 'debian stopped' ;;\n*) exit 0 ;;\nesac\n")
	if err := startNSL(context.Background(), func(string) {}); err != nil {
		t.Fatalf("startNSL: %v", err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(calls)), "\n")
	want := []string{"list", "start debian", "run -m debian --cd / true"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("nsl calls = %q, want %q", got, want)
	}
}

func TestNSLStartCreatesUbuntuOnlyWithoutAManagedMachine(t *testing.T) {
	log := fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' 'fedora running' ;;\n*) exit 0 ;;\nesac\n")
	if err := startNSL(context.Background(), func(string) {}); err != nil {
		t.Fatalf("startNSL: %v", err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(calls)), "\n")
	want := []string{"list", "create ubuntu --distro ubuntu:26.04", "start ubuntu", "run -m ubuntu --cd / true"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("nsl calls = %q, want %q", got, want)
	}
}

func TestNSLReadinessRequiresShellProbe(t *testing.T) {
	fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' 'debian running' ;;\nrun) exit 1 ;;\nesac\n")
	state, err := WSLStatus(context.Background(), BackendNSL)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running || state.Ready {
		t.Fatalf("running machine without ready shell must not report ready: %+v", state)
	}
}

func TestNSLDisableStopsOnlyTheManagedMachine(t *testing.T) {
	log := fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'VM STATE' 'shared running' 'MACHINE STATE' 'work running' 'debian running' ;;\n*) exit 0 ;;\nesac\n")
	if err := SetWSL(context.Background(), BackendNSL, false, nil); err != nil {
		t.Fatalf("SetWSL disable failed: %v", err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if got[len(got)-1] != "stop debian" {
		t.Fatalf("disable must stop only the managed machine, got calls %q", got)
	}
	for _, call := range got {
		if call == "shutdown" || strings.Contains(call, "work") || strings.HasPrefix(call, "remove") {
			t.Fatalf("disable touched more than the managed machine: %q", got)
		}
	}
}

func TestNSLDryRunWritesNoVM(t *testing.T) {
	log := fakeNSL(t, "exit 0\n")
	previous := dryrun.Enabled()
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(previous) })

	if err := SetWSL(context.Background(), BackendNSL, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run ran nsl: %v", err)
	}
}

func TestResolveBackendFollowsAnExistingMachine(t *testing.T) {
	for _, tc := range []struct {
		configured        string
		nslExists, limaOK bool
		want              string
	}{
		{BackendNSL, false, false, BackendNSL},
		{BackendNSL, false, true, BackendLima},
		{BackendNSL, true, true, BackendNSL},
		{BackendLima, true, false, BackendLima},
	} {
		if got := ResolveBackend(tc.configured, tc.nslExists, tc.limaOK); got != tc.want {
			t.Errorf("ResolveBackend(%q, %v, %v) = %q, want %q", tc.configured, tc.nslExists, tc.limaOK, got, tc.want)
		}
	}
}

// Issue #487: a status read that stalls must end at statusTimeout with an
// error, so the developer option worker reaches its completion and the view
// releases its gate and controls.
func TestStalledStatusProbeEndsAtItsBound(t *testing.T) {
	fakeNSL(t, "exec /usr/bin/sleep 30\n")
	previous := statusTimeout
	statusTimeout = 200 * time.Millisecond
	t.Cleanup(func() { statusTimeout = previous })

	start := time.Now()
	_, err := WSLStatus(context.Background(), BackendNSL)
	if err == nil {
		t.Fatal("WSLStatus on a stalled probe returned no error")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("WSLStatus took %v, want it bounded near statusTimeout", elapsed)
	}
}
