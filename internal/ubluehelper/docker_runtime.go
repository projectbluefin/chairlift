package ubluehelper

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrDockerRuntimeMigration refuses mutation until the running legacy daemon
// has been stopped by the user. Rewriting its unit does not change its PATH.
var ErrDockerRuntimeMigration = errors.New(DockerRuntimeMigrationPrefix + " an administrator must stop system Docker before migration")

// DockerRuntimeMigrationPrefix survives the helper's process boundary so the
// GUI can show a reachable prerequisite instead of an ineffective Retry.
const DockerRuntimeMigrationPrefix = "docker runtime update deferred:"

// DockerServiceState is one read-only systemd observation. Transitional states
// are not evidence that a legacy daemon is stopped.
type DockerServiceState struct {
	LoadState, ActiveState, SubState, MainPID, InvocationID, NeedDaemonReload string
}

// DockerProcess contains actual /proc evidence, not the unit's desired argv.
type DockerProcess struct {
	Argv, Environment []string
	Executable        string
}

// DockerRuntimeProbe supplies read-only host observations. Service is read
// before and after Process so a PID/invocation transition cannot be accepted.
type DockerRuntimeProbe struct {
	Service func(context.Context) (DockerServiceState, error)
	Process func(string) (DockerProcess, error)
}

var runtimeInvocation = regexp.MustCompile(`^[0-9a-f]{32}$`)
var runtimeSetID = regexp.MustCompile(`^[0-9a-f]{16}$`)
var runtimePID = regexp.MustCompile(`^[1-9][0-9]*$`)

// CheckDockerRuntime admits Install only after the locked-start unit has been
// loaded. Install calls it under the same lock startup uses to publish active.
// A stable stopped unit can only start through that blocked activation; an
// already-running daemon must be proven to use the retained active set.
func CheckDockerRuntime(ctx context.Context, root, active string, probe DockerRuntimeProbe) error {
	refuse := func(reason any) error { return fmt.Errorf("%w: %v", ErrDockerRuntimeMigration, reason) }
	before, err := probe.Service(ctx)
	if err != nil {
		return refuse(err)
	}
	if before.NeedDaemonReload != "no" {
		return refuse("updated service has not been loaded")
	}
	stopped := (before.LoadState == "loaded" || before.LoadState == "not-found") &&
		before.MainPID == "0" && ((before.ActiveState == "inactive" && before.SubState == "dead") ||
		(before.ActiveState == "failed" && before.SubState == "failed"))
	if !stopped {
		if before.LoadState != "loaded" || before.ActiveState != "active" || before.SubState != "running" ||
			!runtimePID.MatchString(before.MainPID) || !runtimeInvocation.MatchString(before.InvocationID) || !runtimeSetID.MatchString(active) {
			return refuse("daemon execution is not confirmed")
		}
		process, err := probe.Process(before.MainPID)
		if err != nil {
			return refuse(err)
		}
		bin := filepath.Join(root, "active", "bin")
		if len(process.Argv) == 0 || process.Argv[0] != filepath.Join(bin, "dockerd") ||
			process.Executable != filepath.Join(root, active, "bin", "dockerd") {
			return refuse("daemon did not start from the retained active set")
		}
		paths := 0
		for _, entry := range process.Environment {
			if strings.HasPrefix(entry, "PATH=") {
				paths++
				if entry != "PATH="+bin+":/usr/sbin:/usr/bin:/sbin:/bin" {
					return refuse("daemon still resolves tools outside the active runtime")
				}
			}
		}
		if paths != 1 {
			return refuse("daemon PATH is not confirmed")
		}
	}
	after, err := probe.Service(ctx)
	if err != nil {
		return refuse(err)
	}
	if before != after {
		return refuse("daemon invocation changed during observation")
	}
	return nil
}
