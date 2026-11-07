package maintenanceexec

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRunTerminatesProcessGroup verifies that cancelling a maintenance command
// reaps the whole group, not just the direct child: the script spawns a long-lived
// background child and waits, so if only the child process were killed the
// backgrounded child would survive the timeout.
func TestRunTerminatesProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	scriptPath := filepath.Join(dir, "spawn.sh")

	script := "#!/bin/sh\n" +
		"sleep 60 &\n" +
		"echo $! > \"" + pidPath + "\"\n" +
		"wait\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := Run(ctx, scriptPath); err == nil {
		t.Fatalf("expected timeout error, got nil")
	}

	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}

	// Give the kernel a moment to deliver the group-kill signal, then confirm
	// the backgrounded child is gone. A process-group kill reaps it; killing
	// only the direct child would leave it alive.
	time.Sleep(200 * time.Millisecond)
	if processAlive(childPID) {
		t.Fatalf("child process %d still alive after timeout: group was not terminated", childPID)
	}
}

// processAlive reports whether pid is still running. A killed orphan whose
// adoptive parent has not reaped it yet lingers as a zombie, which still
// answers signal 0, so zombies count as terminated: the group kill already
// happened and only reaping, which is outside Run's control, remains.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "State:") {
			return !strings.Contains(line, "Z")
		}
	}
	return true
}
