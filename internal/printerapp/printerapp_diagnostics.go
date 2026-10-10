package printerapp

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DiagnosticInput holds the facts and probe observations needed to classify
// a printer application's lifecycle and failure state.
type DiagnosticInput struct {
	// Capable is the host capability floor (Podman present on $PATH).
	Capable bool
	// UnitPresent reports whether the quadlet container unit exists on disk.
	UnitPresent bool
	// Checked is true once readiness has been probed.
	Checked bool
	// Active is systemd's is-active word (e.g. "active", "activating", "failed", "inactive").
	Active string
	// SubState is systemd's SubState property (e.g. "running", "failed", "dead", "auto-restart").
	SubState string
	// Result is systemd's Result property (e.g. "success", "exit-code", "signal", "core-dump").
	Result string
	// ExecMainStatus is the main process exit code or signal number.
	ExecMainStatus int
	// ImageExists reports whether `podman image exists <image>` succeeded.
	ImageExists bool
	// LogTail contains recent log lines from journalctl --user -u <service>.
	LogTail string
}

// Diagnose is a pure classification function mapping probed systemd,
// container runtime, and journal state to an actionable printer application State.
func Diagnose(input DiagnosticInput) State {
	if !input.Capable {
		return StateUnavailable
	}
	if !input.UnitPresent {
		return StateOff
	}
	if !input.Checked {
		return StateStarting
	}
	if input.Active == "active" {
		return StateReady
	}
	if input.Active == "activating" || input.Active == "reloading" {
		return StateStarting
	}

	// The unit is present on disk, but the service is in a failed or inactive state.
	// Classify the failure based on logs, systemd properties, and container image presence.
	logLower := strings.ToLower(input.LogTail)

	// 1. Plugin verification failure
	if isPluginVerificationFailure(logLower) {
		return StateFailedPlugin
	}

	// 2. Rootless device access failure
	if isDeviceAccessFailure(logLower) {
		return StateFailedDeviceAccess
	}

	// 3. Service crash
	if isServiceCrash(input, logLower) {
		return StateFailedCrash
	}

	// 4. Unavailable container image
	if isImageUnavailableFailure(input, logLower) {
		return StateFailedImage
	}

	// 5. Generic service failure
	return StateFailed
}

func isPluginVerificationFailure(log string) bool {
	if strings.Contains(log, "plugin signature verification failed") ||
		strings.Contains(log, "plugin verification failed") ||
		strings.Contains(log, "signature verification failed") ||
		strings.Contains(log, "hplip_verify_plugin") ||
		strings.Contains(log, "invalid plugin signature") ||
		strings.Contains(log, "tampered payload") ||
		strings.Contains(log, "no plugin was extracted") ||
		(strings.Contains(log, "plugin") && (strings.Contains(log, "bad signature") || strings.Contains(log, "checksum mismatch"))) {
		return true
	}
	return false
}

func isDeviceAccessFailure(log string) bool {
	hasPermissionError := strings.Contains(log, "permission denied") ||
		strings.Contains(log, "operation not permitted") ||
		strings.Contains(log, "access denied")
	hasDeviceRef := strings.Contains(log, "/dev/") ||
		strings.Contains(log, "usb") ||
		strings.Contains(log, "device")

	if hasPermissionError && hasDeviceRef {
		return true
	}

	if strings.Contains(log, "cannot open device") ||
		strings.Contains(log, "failed to open device") ||
		strings.Contains(log, "device access denied") ||
		strings.Contains(log, "device permissions") ||
		strings.Contains(log, "could not open usb") ||
		strings.Contains(log, "failed to setup device") {
		return true
	}
	return false
}

func isImageUnavailableFailure(input DiagnosticInput, log string) bool {
	if strings.Contains(log, "unable to pull") ||
		strings.Contains(log, "failed to pull image") ||
		strings.Contains(log, "failed to pull") ||
		strings.Contains(log, "manifest unknown") ||
		strings.Contains(log, "image not found") ||
		strings.Contains(log, "404 not found") ||
		strings.Contains(log, "repository does not exist") ||
		strings.Contains(log, "name unknown") ||
		strings.Contains(log, "initializing source docker://") ||
		strings.Contains(log, "reading manifest") {
		return true
	}

	if !input.ImageExists && (input.Active == "failed" || input.Result == "exit-code") {
		if strings.Contains(log, "trying to pull") || strings.Contains(log, "pulling image") {
			return true
		}
	}
	return false
}

func isServiceCrash(input DiagnosticInput, log string) bool {
	if input.Result == "core-dump" || input.Result == "signal" {
		return true
	}
	if input.ExecMainStatus > 128 {
		return true
	}
	if strings.Contains(log, "code=dumped") ||
		strings.Contains(log, "code=killed") ||
		strings.Contains(log, "core dumped") ||
		strings.Contains(log, "segmentation fault") ||
		strings.Contains(log, "sigsegv") ||
		strings.Contains(log, "sigabrt") ||
		strings.Contains(log, "fatal glibc error") ||
		strings.Contains(log, "crashed") {
		return true
	}
	return false
}

// execCommand runs a command with context and bounds execution. It is unprivileged
// and runs as the calling user.
func execCommand(ctx context.Context, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

var runCommand = execCommand

func parseShowProperties(output string) (subState, result string, execStatus int) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.IndexByte(line, '='); idx >= 0 {
			k := line[:idx]
			v := line[idx+1:]
			switch k {
			case "SubState":
				subState = v
			case "Result":
				result = v
			case "ExecMainStatus":
				execStatus, _ = strconv.Atoi(v)
			}
		}
	}
	return subState, result, execStatus
}

// ProbeDiagnostics queries systemd, journalctl, and podman to classify the real
// runtime state and actionable failure mode of a printer application.
// It must run off the GTK main thread.
func ProbeDiagnostics(ctx context.Context, app App, capable bool) State {
	facts := Observe(app, capable)
	if !facts.Capable {
		return StateUnavailable
	}
	if !facts.UnitPresent {
		return StateOff
	}

	active, err := ProbeActive(ctx, app)
	if err == nil {
		if active == "active" {
			return StateReady
		}
		if active == "activating" || active == "reloading" {
			return StateStarting
		}
	}

	showOut, _ := runCommand(ctx, "systemctl", "--user", "show", app.ServiceName(), "--property=SubState,Result,ExecMainStatus")
	subState, result, execStatus := parseShowProperties(showOut)

	journalOut, _ := runCommand(ctx, "journalctl", "--user", "-u", app.ServiceName(), "-n", "50", "--no-pager")

	_, imageErr := runCommand(ctx, "podman", "image", "exists", app.Family.Image)
	imageExists := imageErr == nil

	return Diagnose(DiagnosticInput{
		Capable:        facts.Capable,
		UnitPresent:    facts.UnitPresent,
		Checked:        true,
		Active:         active,
		SubState:       subState,
		Result:         result,
		ExecMainStatus: execStatus,
		ImageExists:    imageExists,
		LogTail:        journalOut,
	})
}
