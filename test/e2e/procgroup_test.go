package e2e

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// The membership scan decides when the smoke test may let Go remove the
// temporary HOME, so it is exercised against a fixture process table rather
// than against whatever happens to be running on the host. Each line below is
// the real /proc/<pid>/stat shape: pid, the parenthesized command, the run
// state, parent, process group, then session ID.
func writeProcessEntry(t *testing.T, procTable string, pid int, stat string) {
	t.Helper()

	dir := filepath.Join(procTable, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fixture process %d: %v", pid, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatalf("write fixture process %d stat: %v", pid, err)
	}
}

func TestLiveSessionMembersSelectsOnlyRunnableDescendants(t *testing.T) {
	procTable := t.TempDir()

	// A descendant still writing into the temporary HOME: the case the drain
	// exists for.
	writeProcessEntry(t, procTable, 4242, "4242 (brew) R 4200 4200 4200 0 -1 4194304 0 0")
	// A command name carrying spaces and parentheses. Splitting the whole line
	// on whitespace would read the state and session out of the wrong fields.
	writeProcessEntry(t, procTable, 4243, "4243 (ruby (brew cleanup)) S 4242 4200 4200 0 -1 4194304 0 0")
	// Provider subprocesses use Setpgid, leaving the leader's process group
	// but remaining in its session; they still write to the temporary HOME.
	writeProcessEntry(t, procTable, 4245, "4245 (brew) S 4242 4245 4200 0 -1 4194304 0 0")
	// Exited but not yet reaped: it owns a PID and answers signal 0, yet it
	// cannot create a file, so it must not hold the cleanup open.
	writeProcessEntry(t, procTable, 4244, "4244 (xvfb-run) Z 4200 4200 4200 0 -1 4194304 0 0")
	// The session id itself. The leader is reaped before the drain runs, so a
	// process wearing that PID is an unrelated one the kernel has recycled it
	// for — never counted, and never signalled.
	writeProcessEntry(t, procTable, 4200, "4200 (unrelated) S 1 4200 4200 0 -1 4194304 0 0")
	// Another session entirely.
	writeProcessEntry(t, procTable, 5000, "5000 (systemd) S 1 5000 5000 0 -1 4194304 0 0")
	// /proc carries non-numeric entries, and a process can exit between the
	// listing and the read.
	if err := os.MkdirAll(filepath.Join(procTable, "self"), 0o755); err != nil {
		t.Fatalf("create fixture /proc/self: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(procTable, "9999"), 0o755); err != nil {
		t.Fatalf("create fixture process 9999: %v", err)
	}

	members, err := liveSessionMembers(procTable, 4200)
	if err != nil {
		t.Fatalf("liveSessionMembers: %v", err)
	}

	want := []sessionMember{
		{pid: 4242, name: "brew"},
		{pid: 4243, name: "ruby (brew cleanup)"},
		{pid: 4245, name: "brew"},
	}
	if len(members) != len(want) {
		t.Fatalf("liveSessionMembers = %s, want %s", describeMembers(members), describeMembers(want))
	}
	for index, member := range members {
		if member != want[index] {
			t.Errorf("member %d = %s, want %s", index, member, want[index])
		}
	}
}

func TestLiveSessionMembersIsEmptyOnceTheSessionIsGone(t *testing.T) {
	procTable := t.TempDir()
	writeProcessEntry(t, procTable, 5000, "5000 (systemd) S 1 5000 5000 0 -1 4194304 0 0")

	members, err := liveSessionMembers(procTable, 4200)
	if err != nil {
		t.Fatalf("liveSessionMembers: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("liveSessionMembers = %s, want no members", describeMembers(members))
	}
}

func TestLiveSessionMembersReportsAnUnreadableProcessTable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")

	if _, err := liveSessionMembers(missing, 4200); err == nil {
		t.Fatal("liveSessionMembers accepted a process table that does not exist")
	}
}

func TestParseProcessStatReadsFieldsAfterTheCommand(t *testing.T) {
	tests := []struct {
		name        string
		stat        string
		wantName    string
		wantState   string
		wantSession int
		wantOK      bool
	}{
		{
			name:        "ordinary line",
			stat:        "4242 (brew) R 4200 4200 4200 0 -1 4194304 0 0\n",
			wantName:    "brew",
			wantState:   "R",
			wantSession: 4200,
			wantOK:      true,
		},
		{
			name:        "command containing spaces and parentheses",
			stat:        "4243 (ruby (brew cleanup)) S 4242 4200 4200 0 -1\n",
			wantName:    "ruby (brew cleanup)",
			wantState:   "S",
			wantSession: 4200,
			wantOK:      true,
		},
		{name: "new group same session", stat: "4245 (brew) S 4242 4245 4200 0 -1", wantName: "brew", wantState: "S", wantSession: 4200, wantOK: true},
		{name: "no command parentheses", stat: "4242 brew R 4200 4200"},
		{name: "unterminated command", stat: "4242 (brew R 4200 4200"},
		{name: "truncated after the command", stat: "4242 (brew) R 4200"},
		{name: "non-numeric session", stat: "4242 (brew) R 4200 4200 broken"},
		{name: "empty", stat: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name, state, session, ok := parseProcessStat(test.stat)
			if ok != test.wantOK {
				t.Fatalf("parseProcessStat(%q) ok = %t, want %t", test.stat, ok, test.wantOK)
			}
			if !test.wantOK {
				return
			}
			if name != test.wantName || state != test.wantState || session != test.wantSession {
				t.Errorf("parseProcessStat(%q) = (%q, %q, %d), want (%q, %q, %d)",
					test.stat, name, state, session, test.wantName, test.wantState, test.wantSession)
			}
		})
	}
}

// The smoke test's cleanup calls the drain after its session leader has been
// reaped; an already-empty session must return without polling to timeout.
func TestAwaitSessionExitReturnsOnceDrained(t *testing.T) {
	shell := requireCommand(t, "sh")

	cmd := exec.Command(shell, "-c", "exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the fixture session: %v", err)
	}
	group := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("fixture session: %v", err)
	}

	start := time.Now()
	if err := awaitSessionExit(hostDrain(), group, shutdownTimeout, drainTimeout); err != nil {
		t.Errorf("awaitSessionExit on a drained session: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= shutdownTimeout {
		t.Errorf("awaitSessionExit took %s on an already drained session", elapsed)
	}
}

// Homebrew's readers spawn their own children, so a member can fork while the
// drain is escalating and that child is only visible to a later scan. Sweeping
// once would leave it unsignalled, and the drain would poll to its own timeout
// while the child kept writing into the temporary HOME — the same "directory
// not empty" the removal hits.
func TestAwaitSessionExitKillsMembersThatAppearAfterTheFirstSweep(t *testing.T) {
	procTable := t.TempDir()
	writeProcessEntry(t, procTable, 4242, "4242 (brew) R 4200 4200 4200 0 -1 4194304 0 0")

	var killed []int
	drain := sessionDrain{
		procTable: procTable,
		kill: func(pid int) error {
			killed = append(killed, pid)
			if err := os.RemoveAll(filepath.Join(procTable, strconv.Itoa(pid))); err != nil {
				t.Fatalf("remove fixture process %d: %v", pid, err)
			}
			// The parent had already forked; the child is discovered only
			// after the sweep that killed it.
			if pid == 4242 {
				writeProcessEntry(t, procTable, 4243, "4243 (ruby) R 4242 4200 4200 0 -1 4194304 0 0")
			}
			return nil
		},
	}

	if err := awaitSessionExit(drain, 4200, 0, drainTimeout); err != nil {
		t.Fatalf("awaitSessionExit: %v", err)
	}

	want := []int{4242, 4243}
	if len(killed) != len(want) {
		t.Fatalf("killed = %v, want %v", killed, want)
	}
	for index, pid := range killed {
		if pid != want[index] {
			t.Errorf("kill %d = %d, want %d", index, pid, want[index])
		}
	}
}

// Killing on every scan makes signalling a process that has already gone the
// ordinary case rather than the exceptional one, so ESRCH must not fail the
// drain.
func TestAwaitSessionExitToleratesAMemberThatExitsBeforeTheSignal(t *testing.T) {
	procTable := t.TempDir()
	writeProcessEntry(t, procTable, 4242, "4242 (brew) R 4200 4200 4200 0 -1 4194304 0 0")

	drain := sessionDrain{
		procTable: procTable,
		kill: func(pid int) error {
			if err := os.RemoveAll(filepath.Join(procTable, strconv.Itoa(pid))); err != nil {
				t.Fatalf("remove fixture process %d: %v", pid, err)
			}
			return syscall.ESRCH
		},
	}

	if err := awaitSessionExit(drain, 4200, 0, drainTimeout); err != nil {
		t.Errorf("awaitSessionExit on a member that exited before the signal: %v", err)
	}
}

// A signal failure that is not ESRCH means the drain cannot establish that the
// session is gone, so it has to report rather than poll to its timeout.
func TestAwaitSessionExitReportsASignalFailure(t *testing.T) {
	procTable := t.TempDir()
	writeProcessEntry(t, procTable, 4242, "4242 (brew) R 4200 4200 4200 0 -1 4194304 0 0")

	drain := sessionDrain{
		procTable: procTable,
		kill:      func(int) error { return syscall.EPERM },
	}

	err := awaitSessionExit(drain, 4200, 0, drainTimeout)
	if err == nil {
		t.Fatal("awaitSessionExit accepted a signal it could not send")
	}
	if !errors.Is(err, syscall.EPERM) {
		t.Errorf("awaitSessionExit error = %v, want one wrapping EPERM", err)
	}
}
