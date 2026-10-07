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
