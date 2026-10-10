package printerapp

import (
	"context"
	"errors"
	"strings"
	"time"
)

// State is one printer application's lifecycle state as the Features page
// shows it. It is the printer port of aistack.State.
type State int

const (
	// StateUnavailable: this host cannot run printer applications (no
	// Podman, so no quadlet to install into).
	StateUnavailable State = iota
	// StateOff: ChairLift's unit is not installed.
	StateOff
	// StateStarting: the unit is installed and the service is either not
	// yet checked or reported activating.
	StateStarting
	// StateReady: the unit is installed and systemd reports the service
	// active. Its web page and IPP endpoint are on the app's port.
	StateReady
	// StateFailed: the unit is installed but the service is not running —
	// failed, inactive, or the check itself could not be made.
	StateFailed
	// StateFailedDeviceAccess: the unit is installed but cannot access the
	// printer device in rootless mode (permission denied on /dev/usb, /dev/bus/usb, etc.).
	StateFailedDeviceAccess
	// StateFailedImage: the unit is installed but the container image is
	// unavailable or failed to pull.
	StateFailedImage
	// StateFailedPlugin: the unit is installed but proprietary driver/plugin
	// verification failed (signature or checksum mismatch).
	StateFailedPlugin
	// StateFailedCrash: the unit is installed but the service crashed
	// (exited with fatal signal, core dump, or fatal error).
	StateFailedCrash
)

// Facts are the observations Resolve derives a State from.
type Facts struct {
	// Capable is the host capability floor (Podman present).
	Capable bool
	// UnitPresent reports whether ChairLift's quadlet file exists.
	UnitPresent bool
	// Checked is true once ProbeActive has answered.
	Checked bool
	// Active is systemd's is-active word for the service; meaningful only
	// when Checked, and empty when the check could not be made.
	Active string
}

// Observe returns the non-blocking facts — the unit file's presence — and is
// safe on the GTK main thread. Readiness is a systemctl query and comes from
// ProbeActive, off the main thread.
func Observe(app App, capable bool) Facts {
	return Facts{
		Capable:     capable,
		UnitPresent: IsEnabled(app),
	}
}

// ProbeActive asks systemd whether the app's service is running and returns
// its is-active word: "active", "activating", "inactive", "failed", and so
// on. systemctl exits non-zero for anything but active, so the exit status
// is not the answer; the word is. Output that is not a single word is not a
// state — it is systemctl explaining why it could not answer (no user
// manager, no bus) — and is returned as the error instead.
func ProbeActive(ctx context.Context, app App) (string, error) {
	output, err := runSystemctlOutput(ctx, "is-active", app.ServiceName())
	word := strings.TrimSpace(output)
	if word != "" && !strings.ContainsAny(word, " \t\r\n") {
		return word, nil
	}
	if err != nil {
		return "", err
	}
	return "", errors.New("systemctl is-active gave no answer")
}

// settlePoll is how often WaitSettled re-asks while the service is still
// activating; a seam so the test does not sleep.
var settlePoll = 2 * time.Second

// WaitSettled probes until the service leaves its transitional states —
// activating, reloading — or limit elapses, and returns the last word seen.
// A quadlet's start returns once the container is up, but the image pull
// that precedes a first start can leave the unit activating for a while;
// a row that showed "Starting…" forever would be a false indicator of the
// other kind. The context bounds every probe; a failed probe is returned at
// once rather than retried, because it is systemctl that could not answer.
func WaitSettled(ctx context.Context, app App, limit time.Duration) (string, error) {
	deadline := time.Now().Add(limit)
	for {
		word, err := ProbeActive(ctx, app)
		if err != nil || (word != "activating" && word != "reloading") || !time.Now().Before(deadline) {
			return word, err
		}
		select {
		case <-ctx.Done():
			return word, ctx.Err()
		case <-time.After(settlePoll):
		}
	}
}

// Resolve maps observations to a State. The unit file's presence decides
// whether the application is on; the is-active word decides whether "on"
// means running. Detailed failure classification comes from Diagnose or
// ProbeDiagnostics.
func Resolve(f Facts) State {
	return Diagnose(DiagnosticInput{
		Capable:     f.Capable,
		UnitPresent: f.UnitPresent,
		Checked:     f.Checked,
		Active:      f.Active,
	})
}

// On reports whether the switch should read on in this state.
func (s State) On() bool {
	switch s {
	case StateStarting, StateReady, StateFailed,
		StateFailedDeviceAccess, StateFailedImage, StateFailedPlugin, StateFailedCrash:
		return true
	default:
		return false
	}
}
