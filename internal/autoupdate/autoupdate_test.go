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

// classifyResume only needs to answer the skew-detection question the
// GUI asks after a helper run: is the resume timer still armed? Every
// `systemctl is-enabled` answer that matters to that question is
// covered here, including the ones a default branch folds into
// "available" so a future systemd vocabulary change does not silently
// flip the post-check's verdict.
func TestClassifyResumeMapsEverySystemdState(t *testing.T) {
	tests := []struct {
		name      string
		isEnabled string
		want      ResumeState
	}{
		{name: "masked", isEnabled: "masked", want: ResumeStateMasked},
		{name: "masked at runtime", isEnabled: "masked-runtime", want: ResumeStateMasked},
		{name: "not installed", isEnabled: "not-found", want: ResumeStateAbsent},
		{name: "query failed", isEnabled: "", want: ResumeStateAbsent},
		{name: "whitespace is trimmed", isEnabled: "  masked \n", want: ResumeStateMasked},
		// Any non-masked answer is treated as "still armed" so the GUI
		// surfaces the skew. enabled and enabled-runtime are the
		// pre-disable states; disabled / static / indirect are transient
		// conditions under which the helper's --now stop may have
		// already taken effect. A default branch that folded them all
		// into ResumeStateAvailable was the deliberate choice so a
		// freshly-disabled unit does not read as "already silenced" while
		// uupd-resume.timer is still pending a stop.
		{name: "enabled", isEnabled: "enabled", want: ResumeStateAvailable},
		{name: "enabled at runtime", isEnabled: "enabled-runtime", want: ResumeStateAvailable},
		{name: "disabled", isEnabled: "disabled", want: ResumeStateAbsent},
		{name: "static", isEnabled: "static", want: ResumeStateAvailable},
		{name: "indirect", isEnabled: "indirect", want: ResumeStateAvailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyResume(test.isEnabled); got != test.want {
				t.Errorf("classifyResume(%q) = %q, want %q", test.isEnabled, got, test.want)
			}
		})
	}
}

func TestResumeStateString(t *testing.T) {
	tests := []struct {
		state ResumeState
		want  string
	}{
		{state: ResumeStateMasked, want: "masked"},
		{state: ResumeStateAvailable, want: "available"},
		{state: ResumeStateAbsent, want: "absent"},
		{state: ResumeState(99), want: "unknown"},
	}

	for _, test := range tests {
		if got := test.state.String(); got != test.want {
			t.Errorf("ResumeState(%d).String() = %q, want %q", test.state, got, test.want)
		}
	}
}

// DetectResume must route through the same injection seam Detect uses so
// tests can stand in for systemctl without touching the real timer.
func TestDetectResumeUsesTheProbe(t *testing.T) {
	previous := resumeProbe
	t.Cleanup(func() { resumeProbe = previous })

	resumeProbe = func(context.Context) string { return "masked" }
	if got := DetectResume(context.Background()); got != ResumeStateMasked {
		t.Errorf("DetectResume() with masked probe = %q, want %q", got, ResumeStateMasked)
	}

	resumeProbe = func(context.Context) string { return "not-found" }
	if got := DetectResume(context.Background()); got != ResumeStateAbsent {
		t.Errorf("DetectResume() with not-found probe = %q, want %q", got, ResumeStateAbsent)
	}

	resumeProbe = func(context.Context) string { return "enabled" }
	if got := DetectResume(context.Background()); got != ResumeStateAvailable {
		t.Errorf("DetectResume() with enabled probe = %q, want %q", got, ResumeStateAvailable)
	}
}
