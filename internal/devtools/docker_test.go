package devtools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dockerbrew"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/journal"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
)

const (
	dockerServiceCall = "systemctl show docker.service --property=LoadState,ActiveState"
	dockerSocketCall  = "systemctl show docker.socket --property=LoadState,ActiveState"
)

func dockerReport(load, active string) string {
	return "LoadState=" + load + "\nActiveState=" + active + "\n"
}

// Every external command is a local fixture. In particular, neither systemctl
// nor a helper can reach the host, even when testing the exported functions.
// HOME is private, Homebrew and the Docker CLI are absent, and the account is
// a configured docker member whose session predates the grant unless a test
// says otherwise.
func fakeSystemDocker(t *testing.T, service, socket string) (string, string) {
	t.Helper()
	dir := fakeSystemctl(t, "")
	calls := filepath.Join(dir, "calls")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DOCKER_TEST_CALLS", calls)
	t.Setenv("DOCKER_TEST_STATE", dir)
	t.Setenv("DOCKER_TEST_STATUS_ERROR", "")
	t.Setenv("DOCKER_TEST_CONTEXT", "default")
	writeDockerReports(t, dir, service, socket)
	script := `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$DOCKER_TEST_CALLS"
case "$*" in
'show docker.service --property=LoadState,ActiveState') unit=service ;;
'show docker.socket --property=LoadState,ActiveState') unit=socket ;;
'--user disable --now docker.service') exit 0 ;;
*) exit 99 ;;
esac
if [ "$DOCKER_TEST_STATUS_ERROR" = "$unit" ]; then
  echo 'system manager read denied' >&2
  exit 14
fi
while IFS= read -r line; do printf '%s\n' "$line"; done < "$DOCKER_TEST_STATE/$unit"
`
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := dryrun.Enabled()
	dryrun.Set(false)
	previousDocker, previousBrew, previousInstall, previousGroups := dockerExecutable, dockerBrewExecutable, dockerInstallFormula, dockerGroupMembership
	dockerExecutable = func() string { return "" }
	dockerBrewExecutable = func() string { return "" }
	dockerInstallFormula = func(name string) error {
		t.Errorf("installed %s without a test Homebrew", name)
		return errors.New("unexpected Homebrew install")
	}
	dockerGroupMembership = func() (bool, bool, error) { return true, false, nil }
	t.Cleanup(func() {
		dryrun.Set(previous)
		dockerExecutable, dockerBrewExecutable, dockerInstallFormula, dockerGroupMembership = previousDocker, previousBrew, previousInstall, previousGroups
	})
	return calls, dir
}

// fakeDockerCLI provides a docker client whose current context is
// $DOCKER_TEST_CONTEXT and whose calls land in the shared call log.
func fakeDockerCLI(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "docker")
	script := `#!/bin/sh
printf 'docker %s\n' "$*" >> "$DOCKER_TEST_CALLS"
case "$*" in
'context show') [ "$DOCKER_TEST_CONTEXT" = fail ] && { echo 'context store corrupt' >&2; exit 3; }; echo "$DOCKER_TEST_CONTEXT" ;;
'context use default') exit 0 ;;
*) exit 99 ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	dockerExecutable = func() string { return path }
}

func appendCall(t *testing.T, calls, call string) {
	t.Helper()
	file, err := os.OpenFile(calls, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(call + "\n"); err != nil {
		t.Fatal(err)
	}
}

func writeDockerReports(t *testing.T, dir, service, socket string) {
	t.Helper()
	for name, report := range map[string]string{"service": service, "socket": socket} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(report), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func dockerTestDial(t *testing.T, dial func(context.Context, string, string) (net.Conn, error)) {
	t.Helper()
	previous := dockerDial
	dockerDial = dial
	t.Cleanup(func() { dockerDial = previous })
}

func routeDockerSocket(t *testing.T, socket string) {
	t.Helper()
	dockerTestDial(t, func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "unix" || address != "/var/run/docker.sock" {
			t.Errorf("Docker selected %s %s instead of the fixed system socket", network, address)
			return nil, errors.New("unexpected Docker socket address")
		}
		return (&net.Dialer{}).DialContext(ctx, network, socket)
	})
}

func TestDockerSystemUnitCombinations(t *testing.T) {
	for _, service := range []string{"inactive", "active", "failed", "activating", "deactivating", "reloading", "maintenance", "refreshing"} {
		for _, socketState := range []string{"inactive", "active", "failed"} {
			t.Run(service+"/"+socketState, func(t *testing.T) {
				calls, _ := fakeSystemDocker(t, dockerReport("loaded", service), dockerReport("loaded", socketState))
				socket, hits := pingServer(t, http.StatusOK, "OK")
				routeDockerSocket(t, socket)
				state, err := DockerStatus(context.Background())
				active := (service != "inactive" && service != "failed") || socketState == "active"
				if err != nil || !state.Available || !state.Installed || state.Active != active || state.Ready != active || state.NeedsLogin || state.NoAccess || state.Unavailable != "" {
					t.Fatalf("system Docker = %+v %v, want active/ready %v", state, err, active)
				}
				if (hits.Load() == 1) != active {
					t.Fatalf("socket pings = %d, active %v", hits.Load(), active)
				}
				assertCalls(t, calls, dockerServiceCall, dockerSocketCall)
			})
		}
	}
}

func TestDockerReadinessUsesOnlyTheFixedSystemSocket(t *testing.T) {
	fakeSystemDocker(t, dockerReport("loaded", "inactive"), dockerReport("loaded", "active"))
	decoy, decoyHits := pingServer(t, http.StatusOK, "OK")
	t.Setenv("DOCKER_HOST", "unix://"+decoy)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Dir(decoy))
	socket, hits := pingServer(t, http.StatusOK, "OK\n")
	routeDockerSocket(t, socket)
	state, err := DockerStatus(context.Background())
	if err != nil || !state.Ready || !state.Active || hits.Load() != 1 || decoyHits.Load() != 0 {
		t.Fatalf("fixed socket readiness = %+v %v, pings %d/%d", state, err, hits.Load(), decoyHits.Load())
	}
}

func TestDockerUnusableUnitsAreNotInstallable(t *testing.T) {
	for _, unit := range []string{"service", "socket"} {
		for _, load := range []string{"masked", "error", "bad-setting", "merged", "stub"} {
			t.Run(unit+"/"+load, func(t *testing.T) {
				// The paired unit is missing: an unusable unit must still win
				// over the installable state a missing unit alone would mean.
				service, socket := dockerReport(load, "inactive"), dockerReport("not-found", "inactive")
				if unit == "socket" {
					service, socket = socket, service
				}
				calls, _ := fakeSystemDocker(t, service, socket)
				state, err := DockerStatus(context.Background())
				if err != nil || state.Available || state.Installed || state.Active || state.Ready || state.Unavailable == "" {
					t.Fatalf("unusable unit = %+v %v", state, err)
				}
				var prerequisite *PrerequisiteError
				if err := setDocker(context.Background(), true, nil, func(context.Context, bool) error {
					t.Fatal("unusable units attempted a privileged mutation")
					return nil
				}); !errors.As(err, &prerequisite) || prerequisite.Message != state.Unavailable {
					t.Fatalf("unusable units did not refuse activation: %v", err)
				}
				assertCalls(t, calls, dockerServiceCall, dockerSocketCall, dockerServiceCall, dockerSocketCall)
			})
		}
	}
}

// A host without Docker's units is where ChairLift installs them: the helper
// downloads the verified bottles and writes the units, so not-found is an
// installable state rather than a request for a different image.
func TestDockerMissingUnitsAreInstallableThroughTheHelper(t *testing.T) {
	for _, missing := range []string{"service", "socket", "both"} {
		t.Run(missing, func(t *testing.T) {
			service, socket := dockerReport("loaded", "inactive"), dockerReport("loaded", "inactive")
			if missing != "socket" {
				service = dockerReport("not-found", "inactive")
			}
			if missing != "service" {
				socket = dockerReport("not-found", "inactive")
			}
			calls, dir := fakeSystemDocker(t, service, socket)
			state, err := DockerStatus(context.Background())
			if err != nil || !state.Available || state.Installed || state.Active || state.Unavailable != "" {
				t.Fatalf("missing units = %+v %v, want installable", state, err)
			}
			if err := setDocker(context.Background(), false, nil, func(context.Context, bool) error {
				t.Fatal("disabling an uninstalled Docker dispatched the helper")
				return nil
			}); err != nil {
				t.Fatalf("disable with nothing installed = %v", err)
			}
			socketPath, _ := pingServer(t, http.StatusOK, "OK")
			routeDockerSocket(t, socketPath)
			var stages []string
			var dispatched []bool
			err = setDocker(context.Background(), true, func(stage string) { stages = append(stages, stage) }, func(_ context.Context, enabled bool) error {
				dispatched = append(dispatched, enabled)
				writeDockerReports(t, dir, dockerReport("loaded", "active"), dockerReport("loaded", "active"))
				return nil
			})
			if err != nil || !reflect.DeepEqual(dispatched, []bool{true}) {
				t.Fatalf("install = %v, helper calls %v", err, dispatched)
			}
			if !reflect.DeepEqual(stages, []string{"Installing Docker for this computer…"}) {
				t.Fatalf("install stages = %q", stages)
			}
			if got := mutationCalls(t, calls); len(got) != 0 {
				t.Fatalf("install ran commands outside the helper: %q", got)
			}
		})
	}
}

// Permission to the socket separates two situations: a configured docker
// member whose session predates the grant needs a new login, and anyone else
// needs the grant docker-enable makes.
func TestDockerPermissionDeniedDistinguishesNewLoginFromNoAccess(t *testing.T) {
	membershipFailure := errors.New("group database unreadable")
	for _, test := range []struct {
		name                string
		configured, session bool
		err                 error
		login               bool
	}{
		{"configured, not in this session", true, false, nil, true},
		{"not configured", false, false, nil, false},
		{"configured and in this session", true, true, nil, false},
		{"membership unreadable", true, false, membershipFailure, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fakeSystemDocker(t, dockerReport("loaded", "active"), dockerReport("loaded", "active"))
			dockerGroupMembership = func() (bool, bool, error) { return test.configured, test.session, test.err }
			dockerTestDial(t, func(context.Context, string, string) (net.Conn, error) { return nil, os.ErrPermission })
			state, err := DockerStatus(context.Background())
			if !errors.Is(err, os.ErrPermission) || !state.Active || state.Ready {
				t.Fatalf("denied socket = %+v %v", state, err)
			}
			if state.NeedsLogin != test.login || state.NoAccess == test.login || errors.Is(err, ErrDockerNewLogin) != test.login {
				t.Fatalf("denied socket = %+v %v, want new login %v", state, err, test.login)
			}
		})
	}
}

func TestDockerReportsBothUnitReadAndParseFailures(t *testing.T) {
	for _, unit := range []string{"service", "socket"} {
		t.Run(unit+" read", func(t *testing.T) {
			fakeSystemDocker(t, dockerReport("loaded", "active"), dockerReport("loaded", "active"))
			t.Setenv("DOCKER_TEST_STATUS_ERROR", unit)
			state, err := DockerStatus(context.Background())
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 14 || !strings.Contains(err.Error(), "system manager read denied") {
				t.Fatalf("lost original systemctl read failure: %v", err)
			}
			if state != (DockerState{}) {
				t.Fatalf("partial %s unit read exposed unverified state: %+v", unit, state)
			}
			if err := setDocker(context.Background(), true, nil, func(context.Context, bool) error {
				t.Fatal("failed read allowed activation")
				return nil
			}); err == nil {
				t.Fatal("failed observation reported success")
			}
		})
		for _, report := range []string{"", "LoadState=loaded\n", "ActiveState=active\n", "LoadState=\nActiveState=active\n", "LoadState=garbage\nActiveState=active\n", "LoadState=loaded\nActiveState=garbage\n", "LoadState=loaded\nLoadState=loaded\nActiveState=active\n", "LoadState=loaded\nActiveState=active\nActiveState=active\n"} {
			t.Run(unit+" parse "+report, func(t *testing.T) {
				service, socket := dockerReport("loaded", "active"), dockerReport("loaded", "active")
				if unit == "service" {
					service = report
				} else {
					socket = report
				}
				fakeSystemDocker(t, service, socket)
				if state, err := DockerStatus(context.Background()); err == nil || state != (DockerState{}) {
					t.Fatalf("malformed %s exposed unverified state: %+v %v", unit, state, err)
				}
			})
		}
	}
}

func TestDockerSocketFailuresPreserveTheirCause(t *testing.T) {
	for _, failure := range []error{os.ErrPermission, os.ErrNotExist, context.Canceled, context.DeadlineExceeded, errors.New("connection refused")} {
		t.Run(failure.Error(), func(t *testing.T) {
			fakeSystemDocker(t, dockerReport("loaded", "inactive"), dockerReport("loaded", "active"))
			dockerTestDial(t, func(context.Context, string, string) (net.Conn, error) { return nil, failure })
			state, err := DockerStatus(context.Background())
			login := errors.Is(failure, os.ErrPermission)
			if !errors.Is(err, failure) || !state.Available || state.Ready || !state.Active || state.NeedsLogin != login || state.NoAccess || errors.Is(err, ErrDockerNewLogin) != login {
				t.Fatalf("socket failure lost state/cause: %+v %v", state, err)
			}
		})
	}
}

func TestDockerEnableRequiresObservedReadinessAfterTheHelper(t *testing.T) {
	for _, mode := range []string{"ready", "new login", "connection refused", "bad ping", "inactive", "failed", "post service read", "post socket read"} {
		t.Run(mode, func(t *testing.T) {
			calls, dir := fakeSystemDocker(t, dockerReport("loaded", "inactive"), dockerReport("loaded", "inactive"))
			socket, _ := pingServer(t, http.StatusOK, "OK")
			routeDockerSocket(t, socket)
			failure := errors.New("daemon connection refused")
			if mode == "new login" || mode == "connection refused" {
				if mode == "new login" {
					failure = os.ErrPermission
				}
				dockerTestDial(t, func(context.Context, string, string) (net.Conn, error) { return nil, failure })
			}
			if mode == "bad ping" {
				socket, _ = pingServer(t, http.StatusInternalServerError, "OK")
				routeDockerSocket(t, socket)
			}
			applied := 0
			var stages []string
			err := setDocker(context.Background(), true, func(stage string) { stages = append(stages, stage) }, func(_ context.Context, enabled bool) error {
				applied++
				if !enabled {
					t.Fatal("enable dispatched disable")
				}
				active := "active"
				if mode == "inactive" || mode == "failed" {
					active = mode
				}
				writeDockerReports(t, dir, dockerReport("loaded", active), dockerReport("loaded", active))
				if strings.HasPrefix(mode, "post ") {
					t.Setenv("DOCKER_TEST_STATUS_ERROR", strings.Fields(mode)[1])
				}
				return nil
			})
			if applied != 1 || len(stages) != 1 || (err == nil) != (mode == "ready") {
				t.Fatalf("activation %q = %v, mutations %d, stages %v", mode, err, applied, stages)
			}
			if mode == "new login" && (!errors.Is(err, ErrDockerNewLogin) || !errors.Is(err, os.ErrPermission) || errors.Is(err, ErrNewLogin)) {
				t.Fatalf("completed group grant lost its distinct new-login outcome: %v", err)
			}
			if mode == "connection refused" && !errors.Is(err, failure) {
				t.Fatalf("daemon failure was hidden as a login request: %v", err)
			}
			for _, call := range recordedCalls(t, calls) {
				if call != dockerServiceCall && call != dockerSocketCall {
					t.Fatalf("activation ran Homebrew or a user service mutation: %s", call)
				}
			}
		})
	}
}

func TestDockerHelperFailuresPreserveObservedStateAndOriginalErrors(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		for _, code := range []int{1, 126, 127} {
			failure := &ublue.Error{Message: "helper failed", Err: exec.Command("/bin/sh", "-c", "exit "+strconv.Itoa(code)).Run()}
			calls, _ := fakeSystemDocker(t, dockerReport("loaded", "active"), dockerReport("loaded", "active"))
			socket, _ := pingServer(t, http.StatusOK, "OK")
			routeDockerSocket(t, socket)
			if err := setDocker(context.Background(), enabled, nil, func(context.Context, bool) error { return failure }); err != failure {
				t.Fatalf("helper failure replaced: %v, want %v", err, failure)
			}
			assertCalls(t, calls, dockerServiceCall, dockerSocketCall)
			state, err := DockerStatus(context.Background())
			if err != nil || !state.Active || !state.Ready {
				t.Fatalf("failed/dismissed mutation changed observed state: %+v %v", state, err)
			}
		}
	}
}

func TestDockerDisableConfirmsBothUnitsStoppedAndRetainsFiles(t *testing.T) {
	for _, mode := range []string{"stopped", "service active", "socket active", "socket activating", "service deactivating", "post service read", "post socket read"} {
		t.Run(mode, func(t *testing.T) {
			calls, dir := fakeSystemDocker(t, dockerReport("loaded", "active"), dockerReport("loaded", "active"))
			paths := []string{filepath.Join(dir, "docker-data", "volumes", "project"), filepath.Join(dir, "docker.service"), filepath.Join(dir, "group"), filepath.Join(dir, "docker-ce")}
			for _, path := range paths {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("keep Docker files"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Stopping never depends on this session's permission to ping Docker.
			dockerTestDial(t, func(context.Context, string, string) (net.Conn, error) {
				t.Error("disable pinged the socket, which could reactivate Docker")
				return nil, os.ErrPermission
			})
			err := setDocker(context.Background(), false, nil, func(_ context.Context, enabled bool) error {
				if enabled {
					t.Fatal("disable dispatched enable")
				}
				service, socket := "inactive", "inactive"
				switch mode {
				case "service active":
					service = "active"
				case "socket active":
					socket = "active"
				case "socket activating":
					socket = "activating"
				case "service deactivating":
					service = "deactivating"
				}
				writeDockerReports(t, dir, dockerReport("loaded", service), dockerReport("loaded", socket))
				if strings.HasPrefix(mode, "post ") {
					t.Setenv("DOCKER_TEST_STATUS_ERROR", strings.Fields(mode)[1])
				}
				return nil
			})
			if (err == nil) != (mode == "stopped") {
				t.Fatalf("disable mode %q = %v", mode, err)
			}
			for _, path := range paths {
				if data, err := os.ReadFile(path); err != nil || string(data) != "keep Docker files" {
					t.Fatalf("disable changed %s: %q %v", path, data, err)
				}
			}
			for _, call := range recordedCalls(t, calls) {
				if call != dockerServiceCall && call != dockerSocketCall {
					t.Fatalf("disable ran an install or user-systemctl mutation: %s", call)
				}
			}
		})
	}
}

func TestDockerCanStopAnActiveServiceWhenItsSocketIsMasked(t *testing.T) {
	_, dir := fakeSystemDocker(t, dockerReport("loaded", "active"), dockerReport("masked", "inactive"))
	if err := setDocker(context.Background(), false, nil, func(context.Context, bool) error {
		writeDockerReports(t, dir, dockerReport("loaded", "inactive"), dockerReport("masked", "inactive"))
		return nil
	}); err != nil {
		t.Fatalf("unavailable socket stranded running Docker: %v", err)
	}
}

func TestDockerDryRunJournalsOnlyTheFixedSystemHelper(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enable", false: "disable"}[enabled], func(t *testing.T) {
			calls, _ := fakeSystemDocker(t, dockerReport("loaded", "inactive"), dockerReport("loaded", "inactive"))
			path := filepath.Join(t.TempDir(), "journal.jsonl")
			t.Setenv(journal.PathEnv, path)
			journal.Reset()
			t.Cleanup(journal.Reset)
			dryrun.Set(true)
			if err := SetDocker(context.Background(), enabled, nil); err != nil {
				t.Fatalf("dry-run required live Docker: %v", err)
			}
			assertCalls(t, calls, dockerServiceCall, dockerSocketCall)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			command := "docker-disable"
			if enabled {
				command = "docker-enable"
			}
			var entries []journal.Entry
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var entry journal.Entry
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				entries = append(entries, entry)
			}
			want := []string{pkexec.Command, "/usr/bin/chairlift-helper", command, "--dry-run"}
			if len(entries) != 1 || !reflect.DeepEqual(entries[0].WouldRun, want) || entries[0].Suppressed != journal.SuppressedDryRun {
				t.Fatalf("preview did not remain at the journaled fixed-helper boundary: %+v", entries)
			}
			state, err := DockerStatus(context.Background())
			if err != nil || !state.Available || state.Active || state.Ready {
				t.Fatalf("preview invented Docker state: %+v %v", state, err)
			}
		})
	}
}

func TestDockerCancellationPreventsMutationAndSurvivesPostApplyRead(t *testing.T) {
	for _, afterApply := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			fakeSystemDocker(t, dockerReport("loaded", "inactive"), dockerReport("loaded", "inactive"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !afterApply {
				cancel()
			}
			applied := false
			err := setDocker(ctx, enabled, nil, func(context.Context, bool) error {
				applied = true
				cancel()
				return nil
			})
			if !errors.Is(err, context.Canceled) || applied != afterApply {
				t.Fatalf("canceled mutation = %v, applied %v, want %v", err, applied, afterApply)
			}
		}
	}
}

func TestDockerStatusTimeoutBoundsUnitReadsAndSocketPing(t *testing.T) {
	for _, stage := range []string{"units", "ping"} {
		t.Run(stage, func(t *testing.T) {
			_, dir := fakeSystemDocker(t, dockerReport("loaded", "active"), dockerReport("loaded", "active"))
			previous := statusTimeout
			statusTimeout = 50 * time.Millisecond
			t.Cleanup(func() { statusTimeout = previous })
			if stage == "units" {
				if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte("#!/bin/sh\nexec /bin/sleep 10\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				dockerTestDial(t, func(ctx context.Context, _, _ string) (net.Conn, error) {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(100 * time.Millisecond):
						return nil, context.DeadlineExceeded
					}
				})
			}
			_, err := DockerStatus(context.Background())
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("unbounded or lost %s timeout: %v", stage, err)
			}
		})
	}
}

func TestDockerPingBodyReadFailureRemainsAnError(t *testing.T) {
	fakeSystemDocker(t, dockerReport("loaded", "active"), dockerReport("loaded", "active"))
	dockerTestDial(t, func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer func() { _ = server.Close() }()
			if _, err := http.ReadRequest(bufio.NewReader(server)); err != nil {
				return
			}
			_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 9\r\n\r\nOK")
		}()
		return client, nil
	})
	state, err := DockerStatus(context.Background())
	if !state.Active || state.Ready || state.NeedsLogin || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated ping body hid its cause: %+v %v", state, err)
	}
}

// enableDockerHelper stands in for the fixed helper: it records its place in
// the call log and leaves both units running behind a responding socket.
func enableDockerHelper(t *testing.T, calls, dir string) func(context.Context, bool) error {
	t.Helper()
	socket, _ := pingServer(t, http.StatusOK, "OK")
	routeDockerSocket(t, socket)
	return func(_ context.Context, enabled bool) error {
		if !enabled {
			t.Fatal("enable dispatched disable")
		}
		appendCall(t, calls, "helper docker-enable")
		writeDockerReports(t, dir, dockerReport("loaded", "active"), dockerReport("loaded", "active"))
		return nil
	}
}

func mutationCalls(t *testing.T, calls string) []string {
	t.Helper()
	var got []string
	for _, call := range recordedCalls(t, calls) {
		if call != dockerServiceCall && call != dockerSocketCall {
			got = append(got, call)
		}
	}
	return got
}

func writeUserDockerUnit(t *testing.T, unit string) {
	t.Helper()
	path := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user", "docker.service")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		t.Fatal(err)
	}
}

// v26.10.10 ran dockerd-rootless-setuptool.sh, leaving a per-user daemon and a
// "rootless" CLI context. Enabling system Docker retires exactly that setup
// after the helper succeeds, and leaves any other user unit alone.
func TestDockerEnableRetiresOnlyTheRootlessSetuptoolSetup(t *testing.T) {
	const setuptoolUnit = "[Unit]\nDescription=Docker Application Container Engine (Rootless)\n[Service]\nEnvironment=PATH=/home/linuxbrew/.linuxbrew/bin\nExecStart=/home/linuxbrew/.linuxbrew/bin/dockerd-rootless.sh\n"
	for _, test := range []struct {
		name, unit, context string
		cli, preview        bool
		want                []string
	}{
		{"setuptool unit and context", setuptoolUnit, "rootless", true, false, []string{"helper docker-enable", "systemctl --user disable --now docker.service", "docker context show", "docker context use default"}},
		{"setuptool unit without a CLI", setuptoolUnit, "", false, false, []string{"helper docker-enable", "systemctl --user disable --now docker.service"}},
		{"user's own unit", "[Service]\nExecStart=/opt/proxy/docker-shim --listen\n", "default", true, false, []string{"helper docker-enable", "docker context show"}},
		{"commented mention", "[Service]\n# ExecStart=dockerd-rootless.sh\nExecStart=/usr/bin/true\n", "default", true, false, []string{"helper docker-enable", "docker context show"}},
		{"rootless context only", "", "rootless", true, false, []string{"helper docker-enable", "docker context show", "docker context use default"}},
		{"other user context", "", "remote-build", true, false, []string{"helper docker-enable", "docker context show"}},
		{"preview reads only", setuptoolUnit, "rootless", true, true, []string{"helper docker-enable", "docker context show"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, dir := fakeSystemDocker(t, dockerReport("loaded", "inactive"), dockerReport("loaded", "inactive"))
			if test.unit != "" {
				writeUserDockerUnit(t, test.unit)
			}
			if test.cli {
				fakeDockerCLI(t, dir)
				t.Setenv("DOCKER_TEST_CONTEXT", test.context)
			}
			dryrun.Set(test.preview)
			if err := setDocker(context.Background(), true, nil, enableDockerHelper(t, calls, dir)); err != nil {
				t.Fatalf("enable = %v", err)
			}
			if got := mutationCalls(t, calls); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("calls = %q, want %q", got, test.want)
			}
			if test.unit != "" {
				if data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user", "docker.service")); err != nil || string(data) != test.unit {
					t.Fatalf("user unit changed: %q %v", data, err)
				}
			}
		})
	}
}

// A dismissed prompt or failed install must leave a working rootless daemon
// and its CLI context exactly as they were.
func TestDockerEnableFailureKeepsTheRootlessSetup(t *testing.T) {
	calls, dir := fakeSystemDocker(t, dockerReport("loaded", "inactive"), dockerReport("loaded", "inactive"))
	writeUserDockerUnit(t, "[Service]\nExecStart=/home/linuxbrew/.linuxbrew/bin/dockerd-rootless.sh\n")
	fakeDockerCLI(t, dir)
	t.Setenv("DOCKER_TEST_CONTEXT", "rootless")
	failure := errors.New("authentication dismissed")
	if err := setDocker(context.Background(), true, nil, func(context.Context, bool) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("enable = %v, want helper failure", err)
	}
	if got := mutationCalls(t, calls); len(got) != 0 {
		t.Fatalf("failed enable touched the rootless setup: %q", got)
	}
}

func TestDockerEnableReportsAnUnreadableCLIContextAfterTheHelper(t *testing.T) {
	calls, dir := fakeSystemDocker(t, dockerReport("loaded", "inactive"), dockerReport("loaded", "inactive"))
	fakeDockerCLI(t, dir)
	t.Setenv("DOCKER_TEST_CONTEXT", "fail")
	err := setDocker(context.Background(), true, nil, enableDockerHelper(t, calls, dir))
	if err == nil || !strings.Contains(err.Error(), "context store corrupt") {
		t.Fatalf("context read failure = %v", err)
	}
	if got := mutationCalls(t, calls); !reflect.DeepEqual(got, []string{"helper docker-enable", "docker context show"}) {
		t.Fatalf("calls = %q", got)
	}
}

// fakeDockerBrew provides a Homebrew prefix whose formula installs land in
// the call log and which ships both CLI plugins.
func fakeDockerBrew(t *testing.T, calls string, failing string) string {
	t.Helper()
	prefix := t.TempDir()
	plugins := filepath.Join(prefix, "lib", "docker", "cli-plugins")
	if err := os.MkdirAll(plugins, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docker-buildx", "docker-compose"} {
		if err := os.WriteFile(filepath.Join(plugins, name), []byte("plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dockerBrewExecutable = func() string { return filepath.Join(prefix, "bin", "brew") }
	dockerInstallFormula = func(name string) error {
		appendCall(t, calls, "brew install "+name)
		if name == failing {
			return errors.New("bottle download failed")
		}
		return nil
	}
	return prefix
}

func TestDockerEnableInstallsTheHomebrewCLIAfterTheHelper(t *testing.T) {
	calls, dir := fakeSystemDocker(t, dockerReport("not-found", "inactive"), dockerReport("not-found", "inactive"))
	prefix := fakeDockerBrew(t, calls, "")
	var stages []string
	if err := setDocker(context.Background(), true, func(stage string) { stages = append(stages, stage) }, enableDockerHelper(t, calls, dir)); err != nil {
		t.Fatalf("enable = %v", err)
	}
	want := []string{"helper docker-enable"}
	for _, name := range dockerbrew.CLIFormulae() {
		want = append(want, "brew install "+name)
	}
	if got := mutationCalls(t, calls); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if len(stages) != 1+len(dockerbrew.CLIFormulae()) || stages[0] != "Installing Docker for this computer…" {
		t.Fatalf("stages = %q", stages)
	}
	for _, name := range []string{"docker-buildx", "docker-compose"} {
		link := filepath.Join(os.Getenv("HOME"), ".docker", "cli-plugins", name)
		if target, err := os.Readlink(link); err != nil || target != filepath.Join(prefix, "lib", "docker", "cli-plugins", name) {
			t.Fatalf("%s link = %q %v", name, target, err)
		}
	}
}

// The daemon is the point of the switch; a CLI that failed to install is
// reported after the helper ran, not by skipping the helper.
func TestDockerCLIFailureFollowsASuccessfulHelper(t *testing.T) {
	for _, login := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "new login"}[login], func(t *testing.T) {
			calls, dir := fakeSystemDocker(t, dockerReport("loaded", "inactive"), dockerReport("loaded", "inactive"))
			fakeDockerBrew(t, calls, "docker-buildx")
			apply := enableDockerHelper(t, calls, dir)
			if login {
				dockerTestDial(t, func(context.Context, string, string) (net.Conn, error) { return nil, os.ErrPermission })
			}
			err := setDocker(context.Background(), true, nil, apply)
			var cli *DockerCLIError
			if !errors.As(err, &cli) || !strings.Contains(err.Error(), "bottle download failed") || errors.Is(err, ErrDockerNewLogin) != login {
				t.Fatalf("CLI failure = %v", err)
			}
			if got := mutationCalls(t, calls); !reflect.DeepEqual(got, []string{"helper docker-enable", "brew install docker", "brew install docker-buildx"}) {
				t.Fatalf("calls = %q", got)
			}
			if _, err := os.Lstat(filepath.Join(os.Getenv("HOME"), ".docker")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed install still linked plugins: %v", err)
			}
		})
	}
}

func TestDockerPluginLinksNeverReplaceUserFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	previous := dryrun.Enabled()
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(previous) })
	prefix := t.TempDir()
	plugins := filepath.Join(prefix, "lib", "docker", "cli-plugins")
	if err := os.MkdirAll(plugins, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docker-buildx", "docker-compose"} {
		if err := os.WriteFile(filepath.Join(plugins, name), []byte("plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(home, ".docker", "cli-plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildx, compose := filepath.Join(dir, "docker-buildx"), filepath.Join(dir, "docker-compose")
	reset := func() {
		for _, path := range []string{buildx, compose} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		}
	}
	for _, test := range []struct {
		name                 string
		setup                func()
		wantBuildx, wantComp string // "" means the Homebrew target
	}{
		{"user binary and foreign symlink", func() {
			if err := os.WriteFile(buildx, []byte("my buildx"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/opt/compose/docker-compose", compose); err != nil {
				t.Fatal(err)
			}
		}, "file:my buildx", "/opt/compose/docker-compose"},
		{"relative symlink outside the prefix", func() {
			if err := os.Symlink("../../bin/docker-buildx", buildx); err != nil {
				t.Fatal(err)
			}
		}, "../../bin/docker-buildx", ""},
		{"stale Homebrew symlink", func() {
			if err := os.Symlink(filepath.Join(prefix, "Cellar", "docker-compose", "2.0.0", "bin", "docker-compose"), compose); err != nil {
				t.Fatal(err)
			}
		}, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			reset()
			test.setup()
			if err := linkDockerCLIPlugins(prefix); err != nil {
				t.Fatal(err)
			}
			for path, want := range map[string]string{buildx: test.wantBuildx, compose: test.wantComp} {
				if want == "" {
					want = filepath.Join(plugins, filepath.Base(path))
				}
				if content, ok := strings.CutPrefix(want, "file:"); ok {
					if data, err := os.ReadFile(path); err != nil || string(data) != content {
						t.Fatalf("%s user file changed: %q %v", path, data, err)
					}
					continue
				}
				if got, err := os.Readlink(path); err != nil || got != want {
					t.Fatalf("%s -> %q %v, want %q", path, got, err, want)
				}
			}
		})
	}

	t.Run("preview links nothing", func(t *testing.T) {
		reset()
		dryrun.Set(true)
		defer dryrun.Set(false)
		if err := linkDockerCLIPlugins(prefix); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{buildx, compose} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preview created %s: %v", path, err)
			}
		}
	})
	t.Run("missing Homebrew plugin is an error", func(t *testing.T) {
		reset()
		if err := os.Remove(filepath.Join(plugins, "docker-compose")); err != nil {
			t.Fatal(err)
		}
		if err := linkDockerCLIPlugins(prefix); err == nil || !strings.Contains(err.Error(), "docker-compose") {
			t.Fatalf("dangling link accepted: %v", err)
		}
		if _, err := os.Lstat(compose); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dangling link created: %v", err)
		}
	})
}

// Exercise the exported provider and real helperexec process boundary without
// privilege: only the namespace-local fake escalation executable can run.
func TestDockerMigrationRefusalShowsReachableRecoveryWithoutFollowons(t *testing.T) {
	calls, dir := fakeSystemDocker(t, dockerReport("loaded", "active"), dockerReport("loaded", "active"))
	dockerGroupMembership = func() (bool, bool, error) { return false, false, nil }
	dockerTestDial(t, func(context.Context, string, string) (net.Conn, error) { return nil, os.ErrPermission })
	state, err := DockerStatus(t.Context())
	if !errors.Is(err, os.ErrPermission) || !state.Active || !state.NoAccess {
		t.Fatalf("fixture is not a running inaccessible daemon: %+v, %v", state, err)
	}
	writeUserDockerUnit(t, "[Service]\nExecStart=/home/linuxbrew/.linuxbrew/bin/dockerd-rootless.sh\n")
	fakeDockerCLI(t, dir)
	t.Setenv("DOCKER_TEST_CONTEXT", "rootless")
	script := "#!/bin/sh\nprintf '%s\\n' \"helper $*\" >> \"$DOCKER_TEST_CALLS\"\nprintf '%s\\n' '" + ubluehelper.ErrDockerRuntimeMigration.Error() + "' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, pkexec.Command), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	err = SetDocker(t.Context(), true, nil)
	var prerequisite *PrerequisiteError
	if !errors.As(err, &prerequisite) || prerequisite.Message != DockerMigrationRecovery {
		t.Fatalf("migration refusal has no visible prerequisite recovery: %v", err)
	}
	if !strings.Contains(prerequisite.Message, pkexec.Command+" "+ublue.HelperPath+" docker-disable") || !strings.Contains(prerequisite.Message, "stops running containers") {
		t.Fatalf("recovery omits the reachable fixed command or its consequence: %q", prerequisite.Message)
	}
	if !strings.Contains(err.Error(), ubluehelper.ErrDockerRuntimeMigration.Error()) {
		t.Fatalf("recovery lost the original diagnostic: %v", err)
	}
	if got, want := mutationCalls(t, calls), []string{"helper " + ublue.HelperPath + " docker-enable"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("refusal ran follow-on work: %q, want %q", got, want)
	}
}
