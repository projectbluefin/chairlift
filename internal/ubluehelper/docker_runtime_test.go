package ubluehelper

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDockerRuntimeAdmissionRequiresObservedActiveExecution(t *testing.T) {
	const root = "/var/lib/chairlift/docker"
	const id = "0123456789abcdef"
	ready := DockerServiceState{LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: "123", InvocationID: strings.Repeat("a", 32), NeedDaemonReload: "no"}
	process := DockerProcess{Argv: []string{root + "/active/bin/dockerd", "-H", "fd://"}, Environment: []string{"PATH=" + root + "/active/bin:/usr/sbin:/usr/bin:/sbin:/bin"}, Executable: root + "/" + id + "/bin/dockerd"}
	for _, name := range []string{"ready", "legacy argv", "legacy PATH", "missing PATH", "duplicate PATH", "empty argv", "wrong executable", "missing active", "foreign active", "invalid PID", "invalid invocation", "changed PID", "changed invocation", "transitional", "unloaded service", "unreadable process", "unreadable service", "second read fails", "stopped", "stopped changes", "missing", "failed"} {
		t.Run(name, func(t *testing.T) {
			before, after, p, active := ready, ready, process, id
			var processErr, serviceErr error
			wantOK := name == "ready" || name == "stopped" || name == "missing" || name == "failed"
			switch name {
			case "legacy argv":
				p.Argv = []string{root + "/current/bin/dockerd"}
			case "legacy PATH":
				p.Environment = []string{"PATH=" + root + "/current/bin:/usr/bin"}
			case "missing PATH":
				p.Environment = nil
			case "duplicate PATH":
				p.Environment = append(append([]string{}, p.Environment...), p.Environment[0])
			case "empty argv":
				p.Argv = nil
			case "wrong executable":
				p.Executable = root + "/fedcba9876543210/bin/dockerd"
			case "missing active":
				active = ""
			case "foreign active":
				active = "../foreign"
			case "invalid PID":
				before.MainPID = "../123"
			case "invalid invocation":
				before.InvocationID = ""
			case "changed PID":
				after.MainPID = "124"
			case "changed invocation":
				after.InvocationID = strings.Repeat("b", 32)
			case "transitional":
				before.ActiveState = "activating"
			case "unloaded service":
				before.NeedDaemonReload = "yes"
			case "unreadable process":
				processErr = errors.New("permission denied")
			case "unreadable service":
				serviceErr = errors.New("bus unavailable")
			case "stopped", "stopped changes":
				before = DockerServiceState{LoadState: "loaded", ActiveState: "inactive", SubState: "dead", MainPID: "0", NeedDaemonReload: "no"}
				after = before
				active = ""
				if name == "stopped changes" {
					after = ready
				}
			case "missing":
				before = DockerServiceState{LoadState: "not-found", ActiveState: "inactive", SubState: "dead", MainPID: "0", NeedDaemonReload: "no"}
				after = before
				active = ""
			case "failed":
				before = DockerServiceState{LoadState: "loaded", ActiveState: "failed", SubState: "failed", MainPID: "0", NeedDaemonReload: "no"}
				after = before
			}
			reads := 0
			probe := DockerRuntimeProbe{
				Service: func(context.Context) (DockerServiceState, error) {
					reads++
					if reads == 1 {
						return before, serviceErr
					}
					if name == "second read fails" {
						return DockerServiceState{}, errors.New("bus vanished")
					}
					return after, serviceErr
				},
				Process: func(pid string) (DockerProcess, error) {
					if pid != ready.MainPID {
						t.Fatalf("read unexpected PID %q", pid)
					}
					return p, processErr
				},
			}
			err := CheckDockerRuntime(t.Context(), root, active, probe)
			if (err == nil) != wantOK {
				t.Fatalf("admission = %v, want success=%t", err, wantOK)
			}
			if !wantOK && !errors.Is(err, ErrDockerRuntimeMigration) {
				t.Fatalf("refusal has no actionable migration classification: %v", err)
			}
			if wantOK && reads != 2 {
				t.Fatalf("accepted without a stable second service observation: reads=%d", reads)
			}
		})
	}
}
