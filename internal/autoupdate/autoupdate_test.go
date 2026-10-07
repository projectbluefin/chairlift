package autoupdate

import (
	"context"
	"testing"
)

// Collapsing systemd's several states into one switch is only safe if the
// mapping is explicit, so every state systemd can report is covered here.
func TestClassifyMapsEverySystemdState(t *testing.T) {
	tests := []struct {
		name      string
		isEnabled string
		isActive  string
		want      State
	}{
		{name: "enabled and running", isEnabled: "enabled", isActive: "active", want: StateOn},
		{name: "enabled at runtime and running", isEnabled: "enabled-runtime", isActive: "active", want: StateOn},
		// An enabled but dead timer updates nothing; reporting it as on is a
		// claim the user only disproves by never receiving an update.
		{name: "enabled but inactive", isEnabled: "enabled", isActive: "inactive", want: StateOff},
		{name: "enabled but failed", isEnabled: "enabled", isActive: "failed", want: StateOff},
		// Masking is how bluefinctl expresses both "manual" and "focus mode".
		{name: "masked", isEnabled: "masked", isActive: "inactive", want: StateOff},
		{name: "masked at runtime", isEnabled: "masked-runtime", isActive: "inactive", want: StateOff},
		// A masked timer cannot run whatever is-active claims.
		{name: "masked but reported active", isEnabled: "masked", isActive: "active", want: StateOff},
		{name: "disabled", isEnabled: "disabled", isActive: "inactive", want: StateOff},
		{name: "static", isEnabled: "static", isActive: "inactive", want: StateOff},
		{name: "indirect", isEnabled: "indirect", isActive: "inactive", want: StateOff},
		{name: "not installed", isEnabled: "not-found", isActive: "", want: StateUnavailable},
		{name: "query failed", isEnabled: "", isActive: "", want: StateUnavailable},
		{name: "whitespace is trimmed", isEnabled: "  enabled \n", isActive: " active \n", want: StateOn},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.isEnabled, test.isActive); got != test.want {
				t.Errorf("Classify(%q, %q) = %q, want %q", test.isEnabled, test.isActive, got, test.want)
			}
		})
	}
}

func TestStatePredicates(t *testing.T) {
	tests := []struct {
		state         State
		wantAvailable bool
		wantEnabled   bool
	}{
		{state: StateOn, wantAvailable: true, wantEnabled: true},
		{state: StateOff, wantAvailable: true, wantEnabled: false},
		{state: StateUnavailable, wantAvailable: false, wantEnabled: false},
	}

	for _, test := range tests {
		t.Run(string(test.state), func(t *testing.T) {
			if got := test.state.Available(); got != test.wantAvailable {
				t.Errorf("%q.Available() = %v, want %v", test.state, got, test.wantAvailable)
			}
			if got := test.state.Enabled(); got != test.wantEnabled {
				t.Errorf("%q.Enabled() = %v, want %v", test.state, got, test.wantEnabled)
			}
		})
	}
}

func TestDetectUsesTheProbe(t *testing.T) {
	previous := probe
	t.Cleanup(func() { probe = previous })

	probe = func(context.Context) (string, string) { return "enabled", "active" }
	if got := Detect(context.Background()); got != StateOn {
		t.Errorf("Detect() = %q, want %q", got, StateOn)
	}

	probe = func(context.Context) (string, string) { return "not-found", "" }
	if got := Detect(context.Background()); got != StateUnavailable {
		t.Errorf("Detect() = %q, want %q", got, StateUnavailable)
	}
}

func TestTimerUnitIsTheUniversalBlueUnit(t *testing.T) {
	if TimerUnit != "uupd.timer" {
		t.Errorf("TimerUnit = %q, want uupd.timer", TimerUnit)
	}
}

// Universal Blue's 01-uupd.preset enables both uupd.timer and
// uupd-resume.timer, and both start uupd.service. The switch must govern
// every unattended trigger, so the second name is pinned too.
func TestResumeTimerUnitIsTheUniversalBlueResumeUnit(t *testing.T) {
	if ResumeTimerUnit != "uupd-resume.timer" {
		t.Errorf("ResumeTimerUnit = %q, want uupd-resume.timer", ResumeTimerUnit)
	}
	if ResumeTimerUnit == TimerUnit {
		t.Error("ResumeTimerUnit must differ from TimerUnit")
	}
}
