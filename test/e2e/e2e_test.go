package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	startupTimeout  = 30 * time.Second
	stabilityWindow = time.Second
	shutdownTimeout = 5 * time.Second
	// The smoke test's descendants are reparented away from this process
	// when their leader dies, so they can only be waited for by polling.
	drainTimeout  = 15 * time.Second
	drainInterval = 50 * time.Millisecond

	// defaultProcTable is the kernel's process table. The membership scan
	// below takes it as an argument so a test can point it at a fixture tree.
	defaultProcTable = "/proc"
)

func TestApplicationHelp(t *testing.T) {
	app := filepath.Join(e2eBuildDir(t), "chairlift")
	requireExecutable(t, app)

	cmd := exec.Command(app, "--help")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s --help failed: %v\noutput:\n%s", app, err, output)
	}

	for _, want := range []string{
		"Usage:",
		"Application Options:",
		"--dry-run",
		"Don't make any changes to the system.",
	} {
		if !bytes.Contains(output, []byte(want)) {
			t.Errorf("%s --help output does not contain %q\noutput:\n%s", app, want, output)
		}
	}
}

func TestApplicationStartsInDryRun(t *testing.T) {
	app := filepath.Join(e2eBuildDir(t), "chairlift")
	requireExecutable(t, app)

	dbusRunSession := requireCommand(t, "dbus-run-session")
	xvfbRun := requireCommand(t, "xvfb-run")

	// Startup reads the user's Homebrew inventory, and `brew` populates
	// $HOME/.cache/Homebrew while it does. Go removes this directory when the
	// test ends, so the removal has to happen after every one of those writers
	// is gone; the cleanup registered below is what orders the two.
	home := t.TempDir()

	cmd := exec.Command(
		dbusRunSession,
		"--",
		xvfbRun,
		"-a",
		app,
		"--dry-run",
	)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(
		os.Environ(),
		"LANG=C",
		"LC_ALL=C",
		"NO_AT_BRIDGE=1",
		"GTK_A11Y=none",
		"GSETTINGS_BACKEND=memory",
		"G_DEBUG=fatal-criticals",
		"HOME="+home,
	)
	// A fresh session includes Homebrew children even when they create their own
	// process groups. Never scan or signal the test runner's shared session.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	output := &lockedBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start ChairLift dry-run smoke process: %v", err)
	}

	// Closed as well as sent to: the readiness loop below and the cleanup both
	// receive from this channel, and a cleanup that blocked waiting for a value
	// the loop had already taken would hang the E2E run rather than fail it.
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()

	// Cleanups run last-registered-first, so this one stops the workers before
	// Go removes the temporary HOME registered above — the ordering issue #91
	// reported as a nondeterministic `directory not empty`. Registering it
	// rather than calling it on each exit path also covers the paths that end
	// in t.Fatal.
	t.Cleanup(func() { stopProcessSession(t, cmd, done) })

	want := []string{
		"Running in dry-run mode",
		"ChairLift activated",
		"app: window presented",
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	var readyAt time.Time

	for {
		select {
		case err := <-done:
			t.Fatalf("ChairLift exited before startup readiness: %v\noutput:\n%s", err, output.String())
		case <-ticker.C:
			if missingOutput(output.String(), want) != "" {
				continue
			}
			if readyAt.IsZero() {
				readyAt = time.Now()
				continue
			}
			if time.Since(readyAt) >= stabilityWindow {
				return
			}
		case <-timer.C:
			missing := missingOutput(output.String(), want)
			t.Fatalf("ChairLift did not become ready within %s; missing %s\noutput:\n%s",
				startupTimeout, missing, output.String())
		}
	}
}

func TestInstalledBundleAndHelperBoundary(t *testing.T) {
	root := repoRoot(t)
	buildDir := e2eBuildDir(t)
	stage := t.TempDir()

	cmd := exec.Command(
		"make",
		"install",
		"DESTDIR="+stage,
		"PREFIX=/usr",
		"BUILD_DIR="+buildDir,
	)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("staged make install failed: %v\noutput:\n%s", err, output)
	}

	executables := []string{
		"usr/bin/chairlift",
		"usr/bin/chairlift-wrapper",
		"usr/bin/chairlift-updex-helper",
		"usr/bin/chairlift-ublue-helper",
	}
	for _, path := range executables {
		requireExecutable(t, filepath.Join(stage, path))
	}

	for _, path := range []string{
		"usr/share/chairlift/config.yml",
		"usr/share/applications/io.projectbluefin.chairlift.desktop",
		"usr/share/glib-2.0/schemas/io.projectbluefin.chairlift.livery.gschema.xml",
		"usr/share/icons/hicolor/scalable/apps/io.projectbluefin.chairlift.svg",
		"usr/share/icons/hicolor/symbolic/apps/io.projectbluefin.chairlift-symbolic.svg",
		"usr/share/polkit-1/actions/io.projectbluefin.chairlift.bootc.policy",
		"usr/share/polkit-1/actions/io.projectbluefin.chairlift.updex.policy",
		"usr/share/polkit-1/actions/io.projectbluefin.chairlift.sysupdate.policy",
		"usr/share/polkit-1/actions/io.projectbluefin.chairlift.ublue.policy",
		"usr/share/doc/chairlift/channels.example.yml",
	} {
		if info, err := os.Stat(filepath.Join(stage, path)); err != nil {
			t.Errorf("staged install is missing %s: %v", path, err)
		} else if !info.Mode().IsRegular() {
			t.Errorf("staged install path %s has mode %s, want regular file", path, info.Mode())
		}
	}

	if _, err := os.Stat(filepath.Join(stage, "etc/chairlift/config.yml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staged install created administrator-owned /etc/chairlift/config.yml: %v", err)
	}

	// The channel table decides the image reference the privileged helper
	// hands to bootc, so installing a live one would apply a switch mapping
	// nobody chose. Only the example under /usr/share/doc is shipped.
	for _, live := range []string{"etc/chairlift/channels.yml", "usr/share/chairlift/channels.yml"} {
		if _, err := os.Stat(filepath.Join(stage, live)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("staged install created a live channel table at %s: %v", live, err)
		}
	}

	// PolicyKit authenticates the action and matches only the executable
	// path and argv1, so every shape below reaches a root process on a real
	// system. Both helpers must reject them before doing anything.
	tests := []struct {
		name       string
		helper     string
		args       []string
		wantStderr string
	}{
		{name: "updex missing command", helper: "chairlift-updex-helper", wantStderr: "usage: chairlift-updex-helper"},
		{name: "updex unknown command", helper: "chairlift-updex-helper", args: []string{"unknown"}, wantStderr: "unknown command: unknown"},
		{name: "updex missing feature", helper: "chairlift-updex-helper", args: []string{"enable-feature"}, wantStderr: "usage: chairlift-updex-helper enable-feature"},

		{name: "ublue missing command", helper: "chairlift-ublue-helper", wantStderr: "usage: chairlift-ublue-helper"},
		{name: "ublue unknown command", helper: "chairlift-ublue-helper", args: []string{"powerwash"}, wantStderr: "unknown command: powerwash"},
		{name: "ublue channel switch without a channel", helper: "chairlift-ublue-helper", args: []string{"channel-switch"}, wantStderr: "usage: chairlift-ublue-helper channel-switch"},
		{name: "ublue channel switch with unknown channel", helper: "chairlift-ublue-helper", args: []string{"channel-switch", "nightly"}, wantStderr: "usage: chairlift-ublue-helper channel-switch"},
		// The argument that matters most: an image reference must never be
		// accepted in place of a channel word, or an authenticated caller
		// could point `bootc switch` at any registry.
		{name: "ublue channel switch with an image ref", helper: "chairlift-ublue-helper", args: []string{"channel-switch", "ghcr.io/evil/image:latest"}, wantStderr: "usage: chairlift-ublue-helper channel-switch"},
		{name: "ublue dx enable with a username", helper: "chairlift-ublue-helper", args: []string{"dx-enable", "root"}, wantStderr: "usage: chairlift-ublue-helper dx-enable"},
		{name: "ublue dx disable with a group", helper: "chairlift-ublue-helper", args: []string{"dx-disable", "wheel"}, wantStderr: "usage: chairlift-ublue-helper dx-disable"},
		// PKEXEC_UID is absent outside a pkexec session, so a direct
		// invocation cannot resolve a user to modify.
		{name: "ublue dx enable outside pkexec", helper: "chairlift-ublue-helper", args: []string{"dx-enable"}, wantStderr: "PKEXEC_UID is not set"},
		// Restart takes no delay and no target: either would be a value the
		// caller controls crossing an authenticated boundary.
		{name: "ublue restart with a delay", helper: "chairlift-ublue-helper", args: []string{"restart", "02:00"}, wantStderr: "usage: chairlift-ublue-helper restart"},
		{name: "ublue restart with a flag", helper: "chairlift-ublue-helper", args: []string{"restart", "--force"}, wantStderr: "usage: chairlift-ublue-helper restart"},
		{name: "ublue restart with extra argument", helper: "chairlift-ublue-helper", args: []string{"restart", "--dry-run", "now"}, wantStderr: "usage: chairlift-ublue-helper restart"},
		// Rolling back to an arbitrary image is a channel switch, not this
		// operation.
		{name: "ublue rollback with a target image", helper: "chairlift-ublue-helper", args: []string{"rollback", "ghcr.io/evil/image:old"}, wantStderr: "usage: chairlift-ublue-helper rollback"},
		{name: "ublue rollback with a deployment index", helper: "chairlift-ublue-helper", args: []string{"rollback", "1"}, wantStderr: "usage: chairlift-ublue-helper rollback"},
		// A caller-supplied unit would let an authenticated user enable or
		// mask any systemd unit on the machine.
		{name: "ublue auto updates with a unit", helper: "chairlift-ublue-helper", args: []string{"auto-updates-enable", "sshd.service"}, wantStderr: "usage: chairlift-ublue-helper auto-updates-enable"},
		{name: "ublue auto updates with a flag", helper: "chairlift-ublue-helper", args: []string{"auto-updates-disable", "--now"}, wantStderr: "usage: chairlift-ublue-helper auto-updates-disable"},
		// The driver word is validated against a fixed set; an image
		// reference must never stand in for it.
		{name: "ublue driver switch with an image ref", helper: "chairlift-ublue-helper", args: []string{"driver-switch", "ghcr.io/evil/image:latest"}, wantStderr: "usage: chairlift-ublue-helper driver-switch"},
		{name: "ublue driver switch with an unknown driver", helper: "chairlift-ublue-helper", args: []string{"driver-switch", "nouveau"}, wantStderr: "usage: chairlift-ublue-helper driver-switch"},
		{name: "ublue driver switch without a driver", helper: "chairlift-ublue-helper", args: []string{"driver-switch"}, wantStderr: "usage: chairlift-ublue-helper driver-switch"},
		// A factory reset takes no argument at all; the target is always the
		// image already booted.
		{name: "ublue factory reset with a flag", helper: "chairlift-ublue-helper", args: []string{"factory-reset", "--force"}, wantStderr: "usage: chairlift-ublue-helper factory-reset"},
		{name: "ublue factory reset with extra argument", helper: "chairlift-ublue-helper", args: []string{"factory-reset", "--dry-run", "now"}, wantStderr: "usage: chairlift-ublue-helper factory-reset"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			helper := filepath.Join(stage, "usr/bin", test.helper)
			cmd := exec.Command(helper, test.args...)
			cmd.Env = append(os.Environ(), "PKEXEC_UID=")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("%s %s error = %v, want exit status 1", helper, strings.Join(test.args, " "), err)
			}
			if stdout.Len() != 0 {
				t.Errorf("%s %s wrote unexpected stdout %q", helper, strings.Join(test.args, " "), stdout.String())
			}
			if !strings.Contains(stderr.String(), test.wantStderr) {
				t.Errorf("%s %s stderr = %q, want substring %q", helper, strings.Join(test.args, " "), stderr.String(), test.wantStderr)
			}
		})
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.Buffer.Write(data)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.Buffer.String()
}

func missingOutput(output string, markers []string) string {
	missing := make([]string, 0, len(markers))
	for _, marker := range markers {
		if !strings.Contains(output, marker) {
			missing = append(missing, fmt.Sprintf("%q", marker))
		}
	}
	return strings.Join(missing, ", ")
}

// stopProcessSession ends the smoke run and drains every subprocess in its
// private session, including Homebrew workers in new process groups. Reaping
// the leader alone cannot stop grandchildren still writing into temporary HOME.
func stopProcessSession(t *testing.T, cmd *exec.Cmd, done <-chan error) {
	t.Helper()

	group := cmd.Process.Pid
	// Once the leader has been reaped its process-group ID may be reused;
	// group-wide signals are safe only while it is alive. The later session
	// scan signals remaining workers individually, including other groups.
	if !hasExited(done) {
		if err := syscall.Kill(-group, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("terminate ChairLift smoke process group: %v", err)
		}
		select {
		case <-done:
		case <-time.After(shutdownTimeout):
			if err := syscall.Kill(-group, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				t.Errorf("kill ChairLift smoke process group: %v", err)
			}
			<-done
		}
	}

	if err := awaitSessionExit(hostDrain(), group, shutdownTimeout, drainTimeout); err != nil {
		t.Errorf("ChairLift smoke session %d: %v", group, err)
	}
}

func hasExited(done <-chan error) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// sessionDrain is what awaitSessionExit needs from the host: the process
// table to scan and the signal to send. The smoke test hands it the kernel's,
// so that the drain acts on the processes actually holding the temporary HOME
// open; the unit tests hand it a fixture, so the escalation can be observed
// without racing real processes.
type sessionDrain struct {
	procTable string
	kill      func(pid int) error
}

func hostDrain() sessionDrain {
	return sessionDrain{
		procTable: defaultProcTable,
		kill:      func(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) },
	}
}

// awaitSessionExit blocks until no live process is left in the private session, killing
// whatever ignored the earlier signal once escalateAfter has passed. It kills
// on every scan from then on rather than sweeping once: a member that forks
// during or after a single sweep leaves a child that was never signalled, and
// the drain would then only poll to its own timeout while that child keeps
// writing into the temporary HOME.
//
// The session's leader must already have been reaped: its PID is the session
// id, and once the kernel is free to reuse that PID an unrelated process might
// wear it. The leader PID itself is never counted or signalled here.
func awaitSessionExit(drain sessionDrain, sessionID int, escalateAfter, timeout time.Duration) error {
	start := time.Now()
	for {
		members, err := liveSessionMembers(drain.procTable, sessionID)
		if err != nil {
			return err
		}
		if len(members) == 0 {
			return nil
		}

		elapsed := time.Since(start)
		if elapsed >= escalateAfter {
			for _, member := range members {
				// By PID, not by group: signalling the leader's recycled
				// process-group ID could reach an unrelated process. A member
				// can still exit between this scan and the signal, so this is
				// subject to the ordinary kill-by-PID race
				// on a recycled PID; ESRCH is the expected outcome there, and
				// repeating SIGKILL on a PID already killed is harmless.
				if err := drain.kill(member.pid); err != nil && !errors.Is(err, syscall.ESRCH) {
					return fmt.Errorf("kill %s: %w", member, err)
				}
			}
		}
		if elapsed >= timeout {
			return fmt.Errorf("still running %s after %s: %s", plural(len(members), "process"), timeout, describeMembers(members))
		}
		time.Sleep(drainInterval)
	}
}

// sessionMember is one entry of the kernel's process table.
type sessionMember struct {
	pid  int
	name string
}

func (member sessionMember) String() string {
	return fmt.Sprintf("%s (pid %d)", member.name, member.pid)
}

func describeMembers(members []sessionMember) string {
	described := make([]string, 0, len(members))
	for _, member := range members {
		described = append(described, member.String())
	}
	return strings.Join(described, ", ")
}

func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// liveSessionMembers reports processes under procTable in the private session,
// even if a child changed process group with Setpgid. Zombies cannot write to
// HOME and are excluded. The session leader PID is excluded because it was
// reaped before this scan and may have been recycled.
func liveSessionMembers(procTable string, sessionID int) ([]sessionMember, error) {
	entries, err := os.ReadDir(procTable)
	if err != nil {
		return nil, fmt.Errorf("read the process table at %s: %w", procTable, err)
	}

	members := make([]sessionMember, 0, 4)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == sessionID {
			continue
		}
		// A process that exits between the listing and the read is exactly
		// what this function is waiting for, so a missing stat is not an error.
		stat, err := os.ReadFile(filepath.Join(procTable, entry.Name(), "stat"))
		if err != nil {
			continue
		}
		name, state, session, ok := parseProcessStat(string(stat))
		if !ok || session != sessionID || state == "Z" {
			continue
		}
		members = append(members, sessionMember{pid: pid, name: name})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].pid < members[j].pid })
	return members, nil
}

// parseProcessStat reads the command name, run state, and session out of
// a /proc/<pid>/stat line. The command is parenthesized and may itself contain
// spaces and parentheses, so the fixed fields are taken after the final ')'
// rather than by splitting the whole line.
func parseProcessStat(stat string) (name, state string, session int, ok bool) {
	open := strings.Index(stat, "(")
	closed := strings.LastIndex(stat, ")")
	if open < 0 || closed < open {
		return "", "", 0, false
	}
	fields := strings.Fields(stat[closed+1:])
	if len(fields) < 4 {
		return "", "", 0, false
	}
	session, err := strconv.Atoi(fields[3])
	if err != nil {
		return "", "", 0, false
	}
	return stat[open+1 : closed], fields[0], session, true
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate the E2E test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func e2eBuildDir(t *testing.T) string {
	t.Helper()

	dir := os.Getenv("CHAIRLIFT_E2E_BUILD_DIR")
	if dir == "" {
		t.Skip("E2E prerequisites are prepared by `make e2e`")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve CHAIRLIFT_E2E_BUILD_DIR %q: %v", dir, err)
	}
	return absolute
}

func requireExecutable(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("required executable %s is unavailable: %v", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("required executable %s has mode %s", path, info.Mode())
	}
}

func requireCommand(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("required E2E command %q is unavailable: %v", name, err)
	}
	return path
}
