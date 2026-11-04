package aistack

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

const serviceInvocationMark = "\n# local-only policy invocation: "

// updateServiceUnit replaces only an existing ChairLift-owned unit. It never
// enables a disabled host or rewrites llmman's user configuration.
func updateServiceUnit(ctx context.Context) (bool, error) {
	path, err := UnitPath()
	if err != nil {
		return false, err
	}
	previous, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	exe := Executable()
	if exe == "" {
		return false, errors.New("the installed model runtime could not be resolved")
	}
	current, err := RenderUnit(exe)
	if err != nil {
		return false, err
	}
	// Disk equality alone says nothing about the running daemon: a crash may
	// have happened after the file write but before reload/restart. An
	// invocation stamped after a successful restart proves policy adoption;
	// so does a service systemd started from this exact file, which is the
	// usual state after a reboot, when the stamped InvocationID is stale.
	if unit, stamp, stamped := strings.Cut(string(previous), serviceInvocationMark); unit == current && !dryrun.Enabled() {
		state, err := observeService(ctx)
		if err != nil {
			return false, err
		}
		if stamped && state.running() && stamp == state.invocation+"\n" {
			return false, nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return false, err
		}
		switch state.adopted(info.ModTime()) {
		case adoptedRunning:
			// Record the proof so later launches in this boot take the
			// stamp path. No restart, so loaded models survive.
			if err := writeAtomic(path, unit+serviceInvocationMark+state.invocation+"\n"); err != nil {
				return false, err
			}
			return false, nil
		case adoptedStopped:
			// Nothing runs an older policy, and systemd starts the current
			// file next time, so there is nothing to migrate.
			return false, nil
		}
	}
	if dryrun.Enabled() {
		log.Print("[DRY-RUN] would update the owned model service to local-only configuration")
		return false, nil
	}
	if err := writeAtomic(path, current); err != nil {
		return false, err
	}
	return true, nil
}

// ReconcileService applies the current local-only policy to a service created
// by an older release before the page can call it ready.
func ReconcileService(ctx context.Context) error {
	changed, err := updateServiceUnit(ctx)
	if err == nil && changed {
		err = restartService(ctx)
	}
	if err != nil && !dryrun.Enabled() {
		// An old daemon may still offload prompts. Do not leave it silently
		// active when its local-only migration could not be completed.
		_, stopErr := systemctl(ctx, "stop", ServiceName)
		return errors.Join(fmt.Errorf("updating the local-only model service: %w", err), stopErr)
	}
	return err
}

func serviceInvocation(ctx context.Context) (string, error) {
	id, err := systemctl(ctx, "show", ServiceName, "--property=InvocationID", "--value")
	if err != nil {
		return "", err
	}
	return validInvocation(id)
}

func validInvocation(id string) (string, error) {
	id = strings.TrimSpace(id)
	if len(id) != 32 || strings.Trim(id, "0") == "" {
		return "", errors.New("the model service has no running invocation")
	}
	for _, value := range id {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') {
			return "", errors.New("the model service returned an invalid invocation")
		}
	}
	return id, nil
}

// serviceStateArgs reads the loaded and running unit in one query.
// --timestamp=unix makes ExecMainStartTimestamp comparable with the unit
// file's mtime; a systemd too old for it prints another format, which parses
// as unknown and falls back to the restart path.
var serviceStateArgs = []string{
	"show", ServiceName,
	"--property=ActiveState,NeedDaemonReload,ExecMainStartTimestamp,InvocationID",
	"--timestamp=unix",
}

// serviceState is what systemd reports about the model service.
type serviceState struct {
	active       string
	needReload   string
	mainStart    time.Time
	invocation   string
	invocationOK bool
}

func observeService(ctx context.Context) (serviceState, error) {
	out, err := systemctl(ctx, serviceStateArgs...)
	if err != nil {
		return serviceState{}, err
	}
	var state serviceState
	for line := range strings.SplitSeq(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ActiveState":
			state.active = value
		case "NeedDaemonReload":
			state.needReload = value
		case "ExecMainStartTimestamp":
			if seconds, ok := strings.CutPrefix(value, "@"); ok {
				if n, err := strconv.ParseInt(seconds, 10, 64); err == nil && n > 0 {
					state.mainStart = time.Unix(n, 0)
				}
			}
		case "InvocationID":
			if id, err := validInvocation(value); err == nil {
				state.invocation, state.invocationOK = id, true
			}
		}
	}
	return state, nil
}

func (s serviceState) running() bool {
	return s.active == "active" && s.invocationOK
}

type adoption int

const (
	notAdopted adoption = iota
	adoptedRunning
	adoptedStopped
)

// adopted reports whether the unit file on disk is what systemd runs. The
// manager must have loaded the file as it is now (NeedDaemonReload=no), and a
// running main process must have started strictly after the file was last
// written: a write, a daemon-reload, and a crash before the restart leave an
// older process under a freshly loaded unit, which this rejects. Timestamps
// have one-second resolution, so a start in the same second as the write is
// not trusted. An inactive or failed service runs no older policy at all.
func (s serviceState) adopted(written time.Time) adoption {
	if s.needReload != "no" {
		return notAdopted
	}
	switch s.active {
	case "active":
		if s.running() && !s.mainStart.IsZero() && s.mainStart.After(written.Truncate(time.Second)) {
			return adoptedRunning
		}
	case "inactive", "failed":
		return adoptedStopped
	}
	return notAdopted
}

// restartService records policy adoption only after systemd starts the current
// unit. The stamp stays in that owned unit, not a new state file or preference.
func restartService(ctx context.Context) error {
	path, err := UnitPath()
	if err != nil {
		return err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	unit, _, stamped := strings.Cut(string(contents), serviceInvocationMark)
	if stamped {
		if err := writeAtomic(path, unit); err != nil {
			return err
		}
	}
	if _, err := systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if _, err := systemctl(ctx, "restart", ServiceName); err != nil {
		return err
	}
	id, err := serviceInvocation(ctx)
	if err != nil {
		return err
	}
	return writeAtomic(path, unit+serviceInvocationMark+id+"\n")
}
