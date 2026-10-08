package pageview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/agentmode"
)

func TestGooseRowOffersOnlyWhatTheRowCanDo(t *testing.T) {
	tests := []struct {
		state      agentmode.State
		wantAction GooseAction
		wantLabel  string
	}{
		{agentmode.StateReady, GooseLaunch, "Launch"},
		{agentmode.StatePackagesMissing, GooseSetUp, "Set Up"},
		{agentmode.StateDaemonUnavailable, GooseNoAction, "Launch"},
		{agentmode.StateModelUnavailable, GooseNoAction, "Launch"},
		{agentmode.StateUnsupported, GooseNoAction, "Launch"},
	}
	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			view := GooseRow(tt.state)
			if view.Action != tt.wantAction || view.Action.Label() != tt.wantLabel {
				t.Errorf("GooseRow(%v) = %v %q, want %v %q", tt.state, view.Action, view.Action.Label(), tt.wantAction, tt.wantLabel)
			}
			if view.Subtitle != tt.state.Subtitle() {
				t.Errorf("GooseRow(%v).Subtitle = %q", tt.state, view.Subtitle)
			}
		})
	}
}

func TestGooseSetupToastSaysWhatStillBlocksALaunch(t *testing.T) {
	if got := GooseSetupToast(agentmode.StateReady); !strings.Contains(got, "ready") {
		t.Errorf("ready toast = %q", got)
	}
	got := GooseSetupToast(agentmode.StateDaemonUnavailable)
	if !strings.Contains(got, "installed") || !strings.Contains(got, "Agent Mode") {
		t.Errorf("blocked toast = %q, want it to name Agent Mode", got)
	}
}

// A launch handed to a session that already holds the profile must say so:
// on a host where Goose cannot draw (#544) that session has no window, and a
// silent success on every later click hid it.
func TestGooseLaunchToastNamesTheHandOff(t *testing.T) {
	tests := map[agentmode.LaunchResult]string{
		agentmode.LaunchStarted:   "started",
		agentmode.LaunchStarting:  "starting",
		agentmode.LaunchHandedOff: "already running",
	}
	seen := map[string]bool{}
	for result, want := range tests {
		got := GooseLaunchToast(result)
		if !strings.Contains(got, want) {
			t.Errorf("GooseLaunchToast(%v) = %q, want it to say %q", result, got, want)
		}
		if strings.Contains(strings.ToLower(got), "window opened") || strings.Contains(strings.ToLower(got), "is open") {
			t.Errorf("GooseLaunchToast(%v) = %q claims a window it never observed", result, got)
		}
		seen[got] = true
	}
	if len(seen) != len(tests) {
		t.Errorf("launch results share a toast: %v", seen)
	}
}

// Knowledge searches go online, so the copy must say so and never promise
// that questions stay on this computer.
func TestTroubleshootCopyNeverClaimsAnswersStayLocal(t *testing.T) {
	description := TroubleshootGroupDescription()
	if !strings.Contains(description, "online") {
		t.Errorf("description does not say knowledge searches go online: %q", description)
	}
	for _, claim := range []string{"stay on", "never leave", "locally", "private", "offline"} {
		if strings.Contains(strings.ToLower(description), claim) {
			t.Errorf("description claims locality (%q): %q", claim, description)
		}
	}
}
