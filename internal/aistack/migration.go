package aistack

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

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
	// have happened after the file write but before reload/restart. Only an
	// invocation stamped after a successful restart proves policy adoption.
	if unit, stamp, ok := strings.Cut(string(previous), serviceInvocationMark); ok && unit == current && !dryrun.Enabled() {
		invocation, err := serviceInvocation(ctx)
		if err != nil {
			return false, err
		}
		if stamp == invocation+"\n" {
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
