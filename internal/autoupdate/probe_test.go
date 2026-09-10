package autoupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// withFakeSystemctl puts an executable script named "systemctl" first on
// $PATH, so systemctlOutput and systemctlProbe exercise a real subprocess
// without depending on the test host running systemd at all.
func withFakeSystemctl(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake systemctl: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// systemctl exits non-zero for perfectly ordinary answers — "disabled" and
// "inactive" both exit 1, and "not-found" exits 4. Reading the exit status
// instead of stdout would classify every off timer as unavailable and hide
// the switch from users who had merely turned automatic updates off.
func TestSystemctlOutputReadsStdoutDespiteNonZeroExit(t *testing.T) {
	tests := []struct {
		name   string
		script string
		verb   string
		want   string
	}{
		{
			name:   "enabled exits zero",
			script: "#!/bin/sh\necho enabled\nexit 0\n",
			verb:   "is-enabled",
			want:   "enabled",
		},
		{
			name:   "disabled exits one",
			script: "#!/bin/sh\necho disabled\nexit 1\n",
			verb:   "is-enabled",
			want:   "disabled",
		},
		{
			name:   "inactive exits three",
			script: "#!/bin/sh\necho inactive\nexit 3\n",
			verb:   "is-active",
			want:   "inactive",
		},
		{
			name:   "not-found exits four",
			script: "#!/bin/sh\necho not-found\nexit 4\n",
			verb:   "is-enabled",
			want:   "not-found",
		},
		{
			name:   "trailing newline is trimmed",
			script: "#!/bin/sh\nprintf 'masked\\n\\n'\nexit 1\n",
			verb:   "is-enabled",
			want:   "masked",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withFakeSystemctl(t, test.script)

			if got := systemctlOutput(context.Background(), test.verb); got != test.want {
				t.Errorf("systemctlOutput(%q) = %q, want %q", test.verb, got, test.want)
			}
		})
	}
}

// A failure that produces no output is the one case that must read as empty,
// because Classify maps the empty string to StateUnavailable and hides the
// switch rather than showing it inert.
func TestSystemctlOutputIsEmptyWhenTheQueryProducesNothing(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{name: "silent failure", script: "#!/bin/sh\nexit 1\n"},
		// Diagnostics go to stderr; only stdout is an answer.
		{name: "stderr only", script: "#!/bin/sh\necho 'Failed to get unit file state' >&2\nexit 1\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withFakeSystemctl(t, test.script)

			if got := systemctlOutput(context.Background(), "is-enabled"); got != "" {
				t.Errorf("systemctlOutput() = %q, want empty", got)
			}
		})
	}
}

// A host without systemd at all must classify as unavailable, not panic or
// hang, so the switch is simply absent on such an image.
func TestSystemctlOutputIsEmptyWithoutSystemctlOnPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if got := systemctlOutput(context.Background(), "is-enabled"); got != "" {
		t.Errorf("systemctlOutput() = %q, want empty with no systemctl on PATH", got)
	}
}

// The probe is bounded so a wedged systemd cannot block the UI thread that
// renders the switch. A cancelled context stands in for that timeout firing.
func TestSystemctlOutputHonorsContextCancellation(t *testing.T) {
	withFakeSystemctl(t, "#!/bin/sh\necho enabled\nexit 0\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := systemctlOutput(ctx, "is-enabled"); got != "" {
		t.Errorf("systemctlOutput() = %q, want empty for a cancelled context", got)
	}
}

// systemctlProbe must ask is-enabled and is-active in that order, about
// uupd.timer specifically, and return the pair in the order Classify reads
// them. Swapping the two would report an active-but-disabled timer as on.
func TestSystemctlProbeQueriesBothVerbsForTheTimerUnit(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\n" +
		"case \"$1\" in\n" +
		"  is-enabled) echo enabled ;;\n" +
		"  is-active) echo active ;;\n" +
		"esac\n"
	withFakeSystemctl(t, script)

	isEnabled, isActive := systemctlProbe(context.Background())

	if isEnabled != "enabled" {
		t.Errorf("isEnabled = %q, want %q", isEnabled, "enabled")
	}
	if isActive != "active" {
		t.Errorf("isActive = %q, want %q", isActive, "active")
	}

	recorded, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("reading recorded systemctl calls: %v", err)
	}
	want := "is-enabled " + TimerUnit + "\nis-active " + TimerUnit + "\n"
	if string(recorded) != want {
		t.Errorf("systemctl calls = %q, want %q", string(recorded), want)
	}
}

// The whole point of the probe seam is that Detect classifies real systemctl
// output, so exercise the production probe end to end through Detect.
func TestDetectClassifiesRealSystemctlOutput(t *testing.T) {
	previous := probe
	t.Cleanup(func() { probe = previous })
	probe = systemctlProbe

	tests := []struct {
		name   string
		script string
		want   State
	}{
		{
			name:   "enabled and active",
			script: "#!/bin/sh\ncase \"$1\" in is-enabled) echo enabled ;; is-active) echo active ;; esac\n",
			want:   StateOn,
		},
		{
			name:   "disabled timer still exits non-zero",
			script: "#!/bin/sh\ncase \"$1\" in is-enabled) echo disabled ;; is-active) echo inactive ;; esac\nexit 1\n",
			want:   StateOff,
		},
		{
			name:   "unit not installed",
			script: "#!/bin/sh\necho not-found\nexit 4\n",
			want:   StateUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withFakeSystemctl(t, test.script)

			if got := Detect(context.Background()); got != test.want {
				t.Errorf("Detect() = %q, want %q", got, test.want)
			}
		})
	}
}

// SetProbe is reachable only from the chairlift_e2e build, but a nil argument
// must never leave Detect calling a nil probe and panicking the app.
func TestSetProbeIgnoresNil(t *testing.T) {
	previous := probe
	t.Cleanup(func() { probe = previous })

	probe = func(context.Context) (string, string) { return "enabled", "active" }
	SetProbe(nil)

	if got := Detect(context.Background()); got != StateOn {
		t.Errorf("Detect() = %q after SetProbe(nil), want %q", got, StateOn)
	}
}

func TestSetProbeReplacesTheProbe(t *testing.T) {
	previous := probe
	t.Cleanup(func() { probe = previous })

	// No systemctl on PATH, so a passing result can only come from the
	// replacement rather than from the host.
	t.Setenv("PATH", t.TempDir())

	SetProbe(func(context.Context) (string, string) { return "enabled", "active" })
	if got := Detect(context.Background()); got != StateOn {
		t.Errorf("Detect() = %q, want %q", got, StateOn)
	}

	SetProbe(func(context.Context) (string, string) { return "masked", "active" })
	if got := Detect(context.Background()); got != StateOff {
		t.Errorf("Detect() = %q, want %q", got, StateOff)
	}
}
