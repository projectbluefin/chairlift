package devtools

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func recordedCalls(t *testing.T, log string) []string {
	t.Helper()
	calls, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(calls)), "\n")
}

func assertCalls(t *testing.T, log string, want ...string) {
	t.Helper()
	if got := recordedCalls(t, log); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

// A WSL Mode disable on a host where the Lima ubuntu instance was never
// created must only read the inventory: autostart/stop on a missing
// instance would fail and report the switch as stuck on.
func TestLimaDisableWithoutAMachineOnlyReadsTheInventory(t *testing.T) {
	log := fakeLima(t, "exit 0\n")
	if err := SetWSL(context.Background(), BackendLima, false, nil); err != nil {
		t.Fatalf("disable with no machine: %v", err)
	}
	assertCalls(t, log, "list --json")
}

func TestLimaDisableOfAStoppedMachineOnlyTurnsOffAutostart(t *testing.T) {
	log := fakeLima(t, "case \"$1\" in\nlist) echo '{\"name\":\"ubuntu\",\"status\":\"Stopped\"}' ;;\nesac\n")
	var stages []string
	if err := SetWSL(context.Background(), BackendLima, false, func(s string) { stages = append(stages, s) }); err != nil {
		t.Fatalf("disable stopped machine: %v", err)
	}
	assertCalls(t, log, "list --json", "autostart disable ubuntu")
	if strings.Join(stages, "|") != "Stopping Ubuntu…" {
		t.Fatalf("progress stages = %q", stages)
	}
}

// Only the instance named ubuntu is WSL Mode's; another running instance
// must not make WSL Mode look on, and a stopped ubuntu is never probed.
func TestLimaStatusCountsOnlyTheUbuntuInstance(t *testing.T) {
	log := fakeLima(t, "case \"$1\" in\nlist) echo '{\"name\":\"work\",\"status\":\"Running\"}'; echo '{\"name\":\"ubuntu\",\"status\":\"Stopped\"}' ;;\nesac\n")
	state, err := WSLStatus(context.Background(), BackendLima)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exists || state.Running || state.Ready || state.Machine != "" {
		t.Fatalf("state = %+v, want an existing stopped ubuntu", state)
	}
	assertCalls(t, log, "list --json")
}

func TestLimaStatusReportsAFailedListing(t *testing.T) {
	fakeLima(t, "echo 'lima broke' >&2\nexit 4\n")
	_, err := WSLStatus(context.Background(), BackendLima)
	if err == nil || !strings.Contains(err.Error(), "lima broke") {
		t.Fatalf("failed listing error = %v, want it to carry stderr", err)
	}
}

// With neither engine installed the view must see "no machine", not an error,
// so the WSL Mode row can still offer to install one.
func TestStatusWithoutTheEngineInstalledIsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if NSLInstalled() || LimaInstalled() {
		t.Skip("this host's Homebrew provides nsl or limactl outside PATH")
	}
	for _, backend := range []string{BackendNSL, BackendLima} {
		state, err := WSLStatus(context.Background(), backend)
		if err != nil || state != (WSLState{}) {
			t.Fatalf("%s without its engine: %+v %v", backend, state, err)
		}
	}
	if _, err := NSLDoctor(context.Background()); err == nil {
		t.Fatal("NSLDoctor without nsl reported success")
	}
}

func TestNSLDisableOfAStoppedMachineStopsNothing(t *testing.T) {
	log := fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' 'ubuntu stopped' ;;\n*) exit 0 ;;\nesac\n")
	if err := SetWSL(context.Background(), BackendNSL, false, nil); err != nil {
		t.Fatalf("disable stopped machine: %v", err)
	}
	assertCalls(t, log, "list")
}

func TestNSLDisableReportsAFailedStatusRead(t *testing.T) {
	log := fakeNSL(t, "exit 9\n")
	if err := SetWSL(context.Background(), BackendNSL, false, nil); err == nil {
		t.Fatal("disable after a failed status read reported success")
	}
	assertCalls(t, log, "list")
}

func TestNSLStartLeavesARunningMachineRunning(t *testing.T) {
	log := fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' 'ubuntu running' ;;\n*) exit 0 ;;\nesac\n")
	var stages []string
	if err := startNSL(context.Background(), func(s string) { stages = append(stages, s) }); err != nil {
		t.Fatalf("startNSL: %v", err)
	}
	probe := "run -m ubuntu --cd / true"
	assertCalls(t, log, "list", probe, probe)
	if strings.Join(stages, "|") != "Checking that it works…" {
		t.Fatalf("progress stages = %q", stages)
	}
}

// `nsl start` can fail on a machine that nevertheless comes up (for example
// when another client started it first); the shell probe is what decides.
func TestNSLStartAcceptsAMachineTheProbeReachesAfterAFailedStart(t *testing.T) {
	log := fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' 'ubuntu stopped' ;;\nstart) exit 1 ;;\n*) exit 0 ;;\nesac\n")
	if err := startNSL(context.Background(), func(string) {}); err != nil {
		t.Fatalf("startNSL with reachable machine: %v", err)
	}
	probe := "run -m ubuntu --cd / true"
	assertCalls(t, log, "list", "start ubuntu", probe, probe)
}

func TestNSLStartReportsTheStartFailureWhenTheProbeAlsoFails(t *testing.T) {
	log := fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' 'ubuntu stopped' ;;\nstart) echo 'start boom' >&2; exit 1 ;;\nrun) echo 'probe boom' >&2; exit 1 ;;\nesac\n")
	err := startNSL(context.Background(), func(string) {})
	if err == nil {
		t.Fatal("startNSL with unreachable machine reported success")
	}
	if !strings.Contains(err.Error(), "start boom") || strings.Contains(err.Error(), "probe boom") {
		t.Fatalf("error = %v, want the start failure, not the fallback probe's", err)
	}
	assertCalls(t, log, "list", "start ubuntu", "run -m ubuntu --cd / true")
}

func TestNSLStartStopsAtAFailedCreate(t *testing.T) {
	log := fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' ;;\ncreate) echo 'no space' >&2; exit 3 ;;\n*) exit 0 ;;\nesac\n")
	err := startNSL(context.Background(), func(string) {})
	if err == nil || !strings.Contains(err.Error(), "no space") {
		t.Fatalf("failed create error = %v", err)
	}
	assertCalls(t, log, "list", "create ubuntu --distro ubuntu:26.04")
}

func TestNSLStartReportsAFailedStatusRead(t *testing.T) {
	log := fakeNSL(t, "exit 5\n")
	if err := startNSL(context.Background(), func(string) {}); err == nil {
		t.Fatal("startNSL after a failed status read reported success")
	}
	assertCalls(t, log, "list")
}

func fakeProgram(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+log+"\"\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return log
}

func TestCommandReadSuccessReturnsStdoutOnly(t *testing.T) {
	fakeProgram(t, "probe", "echo out\necho 'warning' >&2\n")
	output, err := command(context.Background(), false, "probe", "a")
	if err != nil || output != "out\n" {
		t.Fatalf("command = %q %v, want stdout only", output, err)
	}
}

func TestCommandReadFailureCarriesStdoutAndStderr(t *testing.T) {
	fakeProgram(t, "probe", "echo out\necho diag >&2\nexit 2\n")
	output, err := command(context.Background(), false, "probe", "a", "b")
	if err == nil {
		t.Fatal("failed read reported success")
	}
	if !strings.Contains(output, "out") || !strings.Contains(output, "diag") {
		t.Fatalf("output = %q, want stdout and stderr", output)
	}
	for _, want := range []string{"probe [a b]", "exit status 2", "diag"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
}

func TestCommandMutationSuccessReturnsNoOutput(t *testing.T) {
	log := fakeProgram(t, "probe", "echo progress\necho more >&2\n")
	output, err := command(context.Background(), true, "probe", "go")
	if err != nil || output != "" {
		t.Fatalf("mutation = %q %v, want empty output", output, err)
	}
	assertCalls(t, log, "go")
}

func TestCommandMutationFailureCarriesCombinedOutput(t *testing.T) {
	fakeProgram(t, "probe", "echo progress\necho broke >&2\nexit 1\n")
	output, err := command(context.Background(), true, "probe")
	if err == nil {
		t.Fatal("failed mutation reported success")
	}
	if !strings.Contains(output, "progress") || !strings.Contains(output, "broke") {
		t.Fatalf("output = %q, want stdout and stderr", output)
	}
}

func TestCommandNamesAMissingProgram(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := command(context.Background(), false, "chairlift-absent-program")
	if err == nil || err.Error() != "chairlift-absent-program is not installed" {
		t.Fatalf("missing program error = %v", err)
	}
}

func TestCommandDryRunSkipsOnlyMutations(t *testing.T) {
	log := fakeProgram(t, "probe", "echo read\n")
	previous := dryrun.Enabled()
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(previous) })

	if output, err := command(context.Background(), true, "probe", "mutate"); err != nil || output != "" {
		t.Fatalf("dry-run mutation = %q %v", output, err)
	}
	if output, err := command(context.Background(), false, "probe", "read"); err != nil || output != "read\n" {
		t.Fatalf("dry-run read = %q %v", output, err)
	}
	assertCalls(t, log, "read")
}

func TestWireSSHCreatesAPrivateConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := wireSSH(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".ssh")
	path := filepath.Join(dir, "config")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "Include ~/.lima/*/ssh.config\n\n" {
		t.Fatalf("new config = %q", b)
	}
	assertMode(t, dir, 0o700)
	assertMode(t, path, 0o600)
}

// ssh refuses a group- or world-writable config, so wiring must also
// tighten permissions on an existing one, and an indented Include the user
// already wrote must not be duplicated.
func TestWireSSHTightensPermissionsAndKeepsAnIndentedInclude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".ssh")
	path := filepath.Join(dir, "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "Host work\n  User me\n  Include ~/.lima/*/ssh.config  \n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wireSSH(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != original {
		t.Fatalf("existing include duplicated or config changed: %q", b)
	}
	assertMode(t, dir, 0o700)
	assertMode(t, path, 0o600)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

func fakeSystemctl(t *testing.T, report string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte("#!/bin/sh\nprintf '%s' '"+report+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func pingServer(t *testing.T, status int, body string) (string, *atomic.Int32) {
	t.Helper()
	dir, err := os.MkdirTemp("", "docker-ping-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return socket, &hits
}

// A systemd that answers without one of the requested properties must not be
// read as "no daemon": the row would then offer an enable that cannot work.
func TestDockerStatusRejectsAnIncompleteSystemdReport(t *testing.T) {
	for _, report := range []string{"LoadState=loaded\n", "ActiveState=active\n", ""} {
		fakeSystemctl(t, report)
		if state, err := dockerStatus(context.Background(), filepath.Join(t.TempDir(), "absent")); err == nil {
			t.Fatalf("report %q accepted as %+v", report, state)
		}
	}
}

func TestDockerStatusDoesNotPingAnInactiveDaemon(t *testing.T) {
	fakeSystemctl(t, "LoadState=loaded\nActiveState=inactive\n")
	socket, hits := pingServer(t, http.StatusOK, "OK")
	state, err := dockerStatus(context.Background(), socket)
	if err != nil || !state.Available || state.Active || state.Ready {
		t.Fatalf("inactive daemon state = %+v %v", state, err)
	}
	if hits.Load() != 0 {
		t.Fatal("inactive daemon was pinged")
	}
}

func TestDockerReadyRequiresAnOKPing(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{http.StatusInternalServerError, "OK"},
		{http.StatusOK, "NOT OK"},
		{http.StatusOK, ""},
	} {
		fakeSystemctl(t, "LoadState=loaded\nActiveState=active\n")
		socket, hits := pingServer(t, tc.status, tc.body)
		state, err := dockerStatus(context.Background(), socket)
		if err != nil || !state.Active || state.Ready {
			t.Fatalf("ping %d %q gave %+v %v, want active but not ready", tc.status, tc.body, state, err)
		}
		if hits.Load() != 1 {
			t.Fatalf("ping %d %q: daemon hit %d times", tc.status, tc.body, hits.Load())
		}
	}
}

func TestSetDockerReportsAFailedStatusRead(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := SetDocker(context.Background(), true, nil); err == nil {
		t.Fatal("enable without a readable daemon state reported success")
	}
}
