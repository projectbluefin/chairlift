package printerapp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestObserveReadsTheUnit(t *testing.T) {
	_, calls := stubUnitDir(t)
	app := Select(Families()[0])

	got := Observe(app, true)
	want := Facts{Capable: true, UnitPresent: false}
	if got != want {
		t.Errorf("Observe before enable = %+v, want %+v", got, want)
	}

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	*calls = nil

	got = Observe(app, false)
	want = Facts{Capable: false, UnitPresent: true}
	if got != want {
		t.Errorf("Observe after enable = %+v, want %+v", got, want)
	}
	if len(*calls) != 0 {
		t.Errorf("Observe ran systemctl: %v — it must stay non-blocking", *calls)
	}
}

func TestProbeActiveReturnsTheStateWordNotTheExitStatus(t *testing.T) {
	_, _ = stubUnitDir(t)
	app := Select(Families()[1])

	cases := []struct {
		name    string
		output  string
		err     error
		want    string
		wantErr bool
	}{
		{name: "active exits zero", output: "active\n", want: "active"},
		// systemctl is-active exits 3 for inactive and failed; the word is
		// still the answer.
		{name: "inactive exits non-zero", output: "inactive", err: errors.New("exit status 3"), want: "inactive"},
		{name: "failed exits non-zero", output: "failed", err: errors.New("exit status 3"), want: "failed"},
		{name: "activating", output: "activating", err: errors.New("exit status 3"), want: "activating"},
		// No user manager: systemctl explains itself instead of answering.
		{name: "no bus", output: "Failed to connect to bus: No medium found", err: errors.New("exit status 1"), wantErr: true},
		{name: "silence", output: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var asked string
			runSystemctlOutput = func(_ context.Context, args ...string) (string, error) {
				asked = strings.Join(args, " ")
				return tc.output, tc.err
			}
			got, err := ProbeActive(context.Background(), app)
			if asked != "is-active "+app.ServiceName() {
				t.Errorf("ProbeActive asked %q, want %q", asked, "is-active "+app.ServiceName())
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ProbeActive = %q, nil; want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ProbeActive: %v", err)
			}
			if got != tc.want {
				t.Errorf("ProbeActive = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWaitSettledLeavesTransitionalStatesAndStopsAtTheLimit(t *testing.T) {
	_, _ = stubUnitDir(t)
	app := Select(Families()[2])
	previousPoll := settlePoll
	settlePoll = 0
	t.Cleanup(func() { settlePoll = previousPoll })

	answers := []string{"activating", "activating", "active"}
	asked := 0
	runSystemctlOutput = func(_ context.Context, _ ...string) (string, error) {
		word := answers[min(asked, len(answers)-1)]
		asked++
		return word, nil
	}
	got, err := WaitSettled(context.Background(), app, time.Minute)
	if err != nil || got != "active" || asked != 3 {
		t.Errorf("WaitSettled = %q, %v after %d probes; want active after 3", got, err, asked)
	}

	// Still activating at the limit: the last word is returned, not an
	// invented failure, and the probing stops.
	asked = 0
	runSystemctlOutput = func(_ context.Context, _ ...string) (string, error) {
		asked++
		return "activating", nil
	}
	got, err = WaitSettled(context.Background(), app, 0)
	if err != nil || got != "activating" || asked != 1 {
		t.Errorf("WaitSettled at limit = %q, %v after %d probes; want activating after 1", got, err, asked)
	}

	// systemctl that cannot answer is returned at once, not retried.
	asked = 0
	runSystemctlOutput = func(_ context.Context, _ ...string) (string, error) {
		asked++
		return "Failed to connect to bus", errors.New("exit status 1")
	}
	if _, err = WaitSettled(context.Background(), app, time.Minute); err == nil || asked != 1 {
		t.Errorf("WaitSettled on a failed probe: err=%v probes=%d; want an error after 1", err, asked)
	}
}

func TestResolveNeverReportsReadyFromUnitPresenceAlone(t *testing.T) {
	cases := []struct {
		name  string
		facts Facts
		want  State
		on    bool
	}{
		{name: "no podman", facts: Facts{}, want: StateUnavailable},
		{name: "no podman even with a unit", facts: Facts{UnitPresent: true, Checked: true, Active: "active"}, want: StateUnavailable},
		{name: "off", facts: Facts{Capable: true}, want: StateOff},
		{name: "unit present, not yet checked", facts: Facts{Capable: true, UnitPresent: true}, want: StateStarting, on: true},
		{name: "unit present, activating", facts: Facts{Capable: true, UnitPresent: true, Checked: true, Active: "activating"}, want: StateStarting, on: true},
		{name: "unit present, active", facts: Facts{Capable: true, UnitPresent: true, Checked: true, Active: "active"}, want: StateReady, on: true},
		{name: "unit present, failed", facts: Facts{Capable: true, UnitPresent: true, Checked: true, Active: "failed"}, want: StateFailed, on: true},
		{name: "unit present, inactive", facts: Facts{Capable: true, UnitPresent: true, Checked: true, Active: "inactive"}, want: StateFailed, on: true},
		{name: "unit present, check failed", facts: Facts{Capable: true, UnitPresent: true, Checked: true}, want: StateFailed, on: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.facts)
			if got != tc.want {
				t.Errorf("Resolve(%+v) = %d, want %d", tc.facts, got, tc.want)
			}
			if got.On() != tc.on {
				t.Errorf("Resolve(%+v).On() = %v, want %v", tc.facts, got.On(), tc.on)
			}
		})
	}
}
