package devtools

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/journal"
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
	if err := SetWSL(context.Background(), false, nil); err == nil {
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
	state, err := WSLStatus(context.Background())
	if err != nil || !state.Running {
		t.Fatalf("failed stop must remain running: %+v %v", state, err)
	}
}

func TestWSLReadinessRequiresShellNotRunningWord(t *testing.T) {
	fakeLima(t, "case \"$1\" in\nlist) echo '{\"name\":\"ubuntu\",\"status\":\"Running\"}' ;;\nshell) exit 1 ;;\nesac\n")
	state, err := WSLStatus(context.Background())
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
	if err := SetWSL(context.Background(), true, nil); err != nil {
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
	if _, err := WSLStatus(context.Background()); err == nil {
		t.Fatal("malformed listing treated as no VM")
	}
}

func TestEmptyLimaListingIgnoresSuccessfulStderrWarnings(t *testing.T) {
	fakeLima(t, "echo 'WARN No instance found. Run limactl create to create an instance.' >&2\nexit 0\n")
	state, err := WSLStatus(context.Background())
	if err != nil || state.Exists || state.Running || state.Ready {
		t.Fatalf("empty inventory warning must not block first setup: %+v %v", state, err)
	}
}

func fakeDockerHost(t *testing.T, load, active string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte("#!/bin/sh\necho 'LoadState="+load+"'\necho 'ActiveState="+active+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return dir
}

func TestDockerCLIPresenceDoesNotProvideADaemon(t *testing.T) {
	fakeDockerHost(t, "not-found", "inactive")
	state, err := DockerStatus(context.Background())
	if err != nil || state.Available || state.Active || state.Ready {
		t.Fatalf("CLI-only host claimed a daemon: %+v %v", state, err)
	}
	if err := SetDocker(context.Background(), true, nil); err == nil {
		t.Fatal("CLI-only host enabled Docker")
	}
}

func TestDockerReadyRequiresSuccessfulLocalSocketPing(t *testing.T) {
	fakeDockerHost(t, "loaded", "active")
	dir, err := os.MkdirTemp("", "docker-ping-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "socket")
	state, err := dockerStatus(context.Background(), socket)
	if err != nil || !state.Active || state.Ready {
		t.Fatalf("missing socket reported ready: %+v %v", state, err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			t.Errorf("unexpected daemon request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("OK"))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	state, err = dockerStatus(context.Background(), socket)
	if err != nil || !state.Ready {
		t.Fatalf("reachable daemon not ready: %+v %v", state, err)
	}
}

func TestDockerFailedDisableKeepsObservedRunningState(t *testing.T) {
	dir := fakeDockerHost(t, "loaded", "active")
	t.Setenv(journal.PathEnv, filepath.Join(dir, "journal.jsonl"))
	journal.Reset()
	t.Cleanup(journal.Reset)
	if err := os.WriteFile(filepath.Join(dir, "pkexec"), []byte("#!/bin/sh\necho 'authentication dismissed' >&2\nexit 126\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SetDocker(context.Background(), false, nil); err == nil {
		t.Fatal("failed disable reported success")
	}
	state, err := dockerStatus(context.Background(), filepath.Join(dir, "absent-socket"))
	if err != nil || !state.Active || state.Ready {
		t.Fatalf("failed disable hid still-running daemon: %+v %v", state, err)
	}
}
