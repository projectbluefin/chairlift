package printerapp

import (
	"context"
	"testing"
)

func TestPrinterAppDiagnoseCoversEveryFailureClass(t *testing.T) {
	tests := []struct {
		name  string
		input DiagnosticInput
		want  State
		on    bool
	}{
		// 1. Missing Podman
		{
			name:  "missing podman outranks everything",
			input: DiagnosticInput{Capable: false, UnitPresent: true, Active: "active"},
			want:  StateUnavailable,
			on:    false,
		},
		{
			name:  "missing podman when off",
			input: DiagnosticInput{Capable: false, UnitPresent: false},
			want:  StateUnavailable,
			on:    false,
		},

		// Off
		{
			name:  "off when unit not installed",
			input: DiagnosticInput{Capable: true, UnitPresent: false},
			want:  StateOff,
			on:    false,
		},

		// Starting & Ready
		{
			name:  "unit present but unchecked",
			input: DiagnosticInput{Capable: true, UnitPresent: true, Checked: false},
			want:  StateStarting,
			on:    true,
		},
		{
			name:  "unit present and activating",
			input: DiagnosticInput{Capable: true, UnitPresent: true, Checked: true, Active: "activating"},
			want:  StateStarting,
			on:    true,
		},
		{
			name:  "unit present and active",
			input: DiagnosticInput{Capable: true, UnitPresent: true, Checked: true, Active: "active"},
			want:  StateReady,
			on:    true,
		},

		// 2. Failed rootless device access
		{
			name: "rootless device access failed on usb lp0",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				LogTail:     "open /dev/usb/lp0: permission denied",
			},
			want: StateFailedDeviceAccess,
			on:   true,
		},
		{
			name: "rootless device access failed on usb bus",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				LogTail:     "failed to open device /dev/bus/usb/001/002: Operation not permitted",
			},
			want: StateFailedDeviceAccess,
			on:   true,
		},
		{
			name: "device access denied error message",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				LogTail:     "cannot open device: access denied for usb printer",
			},
			want: StateFailedDeviceAccess,
			on:   true,
		},

		// 3. Unavailable container image
		{
			name: "image pull failure: unable to pull",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				LogTail:     "Error: unable to pull ghcr.io/projectbluefin/ghostscript-printer-app: 404 Not Found",
			},
			want: StateFailedImage,
			on:   true,
		},
		{
			name: "image pull failure: manifest unknown",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				LogTail:     "Error: reading manifest: manifest unknown",
			},
			want: StateFailedImage,
			on:   true,
		},
		{
			name: "image not found locally and pull failed",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				ImageExists: false,
				LogTail:     "trying to pull ghcr.io/projectbluefin/ps-printer-app...",
			},
			want: StateFailedImage,
			on:   true,
		},

		// 4. Plugin verification failure
		{
			name: "hplip plugin signature verification failed",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				LogTail:     "pappl: Plugin signature verification failed; no plugin was extracted.",
			},
			want: StateFailedPlugin,
			on:   true,
		},
		{
			name: "plugin verification failed on tampered payload",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				LogTail:     "hplip_verify_plugin: tampered payload detected, aborting",
			},
			want: StateFailedPlugin,
			on:   true,
		},
		{
			name: "plugin checksum mismatch",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				LogTail:     "plugin archive checksum mismatch against index",
			},
			want: StateFailedPlugin,
			on:   true,
		},

		// 5. Service crash
		{
			name: "crashed with core-dump result",
			input: DiagnosticInput{
				Capable:        true,
				UnitPresent:    true,
				Checked:        true,
				Active:         "failed",
				Result:         "core-dump",
				ExecMainStatus: 139,
			},
			want: StateFailedCrash,
			on:   true,
		},
		{
			name: "crashed with signal result",
			input: DiagnosticInput{
				Capable:        true,
				UnitPresent:    true,
				Checked:        true,
				Active:         "failed",
				Result:         "signal",
				ExecMainStatus: 134,
			},
			want: StateFailedCrash,
			on:   true,
		},
		{
			name: "crashed logged in journal",
			input: DiagnosticInput{
				Capable:     true,
				UnitPresent: true,
				Checked:     true,
				Active:      "failed",
				LogTail:     "Main process exited, code=dumped, status=11/SEGV: Segmentation fault (core dumped)",
			},
			want: StateFailedCrash,
			on:   true,
		},

		// Generic fallback failure
		{
			name: "generic service exit without diagnostic matches",
			input: DiagnosticInput{
				Capable:        true,
				UnitPresent:    true,
				Checked:        true,
				Active:         "failed",
				Result:         "exit-code",
				ExecMainStatus: 1,
				LogTail:        "exited with error",
			},
			want: StateFailed,
			on:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Diagnose(tt.input)
			if got != tt.want {
				t.Errorf("Diagnose() = %v, want %v", got, tt.want)
			}
			if got.On() != tt.on {
				t.Errorf("Diagnose().On() = %v, want %v", got.On(), tt.on)
			}
		})
	}
}

func TestPrinterAppProbeDiagnosticsWithStubbedCommands(t *testing.T) {
	_, _ = stubUnitDir(t)
	app := Select(Families()[0])
	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	prevRunCommand := runCommand
	prevRunSystemctlOutput := runSystemctlOutput
	t.Cleanup(func() {
		runCommand = prevRunCommand
		runSystemctlOutput = prevRunSystemctlOutput
	})

	// 1. When active, returns StateReady immediately
	runSystemctlOutput = func(_ context.Context, _ ...string) (string, error) {
		return "active", nil
	}
	commandCalled := false
	runCommand = func(_ context.Context, _ string, _ ...string) (string, error) {
		commandCalled = true
		return "", nil
	}
	state := ProbeDiagnostics(context.Background(), app, true)
	if state != StateReady {
		t.Errorf("ProbeDiagnostics active = %v, want StateReady", state)
	}
	if commandCalled {
		t.Errorf("ProbeDiagnostics ran secondary diagnostic commands when service was active")
	}

	// 2. When failed due to device access in journal
	runSystemctlOutput = func(_ context.Context, _ ...string) (string, error) {
		return "failed", nil
	}
	runCommand = func(_ context.Context, name string, args ...string) (string, error) {
		switch name {
		case "systemctl":
			return "SubState=failed\nResult=exit-code\nExecMainStatus=1\n", nil
		case "journalctl":
			return "open /dev/usb/lp0: permission denied\n", nil
		case "podman":
			return "", nil
		}
		return "", nil
	}
	state = ProbeDiagnostics(context.Background(), app, true)
	if state != StateFailedDeviceAccess {
		t.Errorf("ProbeDiagnostics device error = %v, want StateFailedDeviceAccess", state)
	}

	// 3. When failed due to image pull in journal
	runCommand = func(_ context.Context, name string, args ...string) (string, error) {
		switch name {
		case "systemctl":
			return "SubState=failed\nResult=exit-code\nExecMainStatus=125\n", nil
		case "journalctl":
			return "Error: unable to pull image: 404 Not Found\n", nil
		case "podman":
			return "", nil
		}
		return "", nil
	}
	state = ProbeDiagnostics(context.Background(), app, true)
	if state != StateFailedImage {
		t.Errorf("ProbeDiagnostics image error = %v, want StateFailedImage", state)
	}

	// 4. When failed due to crash in systemctl show
	runCommand = func(_ context.Context, name string, args ...string) (string, error) {
		switch name {
		case "systemctl":
			return "SubState=failed\nResult=core-dump\nExecMainStatus=139\n", nil
		case "journalctl":
			return "service terminated\n", nil
		case "podman":
			return "", nil
		}
		return "", nil
	}
	state = ProbeDiagnostics(context.Background(), app, true)
	if state != StateFailedCrash {
		t.Errorf("ProbeDiagnostics crash error = %v, want StateFailedCrash", state)
	}

	// 5. When failed due to plugin verification failure
	runCommand = func(_ context.Context, name string, args ...string) (string, error) {
		switch name {
		case "systemctl":
			return "SubState=failed\nResult=exit-code\nExecMainStatus=1\n", nil
		case "journalctl":
			return "Plugin signature verification failed; no plugin was extracted.\n", nil
		case "podman":
			return "", nil
		}
		return "", nil
	}
	state = ProbeDiagnostics(context.Background(), app, true)
	if state != StateFailedPlugin {
		t.Errorf("ProbeDiagnostics plugin error = %v, want StateFailedPlugin", state)
	}
}

func TestPrinterAppParseShowProperties(t *testing.T) {
	input := "SubState=dead\nResult=signal\nExecMainStatus=134\nOther=value\n"
	subState, result, status := parseShowProperties(input)
	if subState != "dead" {
		t.Errorf("subState = %q, want dead", subState)
	}
	if result != "signal" {
		t.Errorf("result = %q, want signal", result)
	}
	if status != 134 {
		t.Errorf("status = %d, want 134", status)
	}
}
