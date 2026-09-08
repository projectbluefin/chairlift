package pageview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

func TestTroubleshootRowStates(t *testing.T) {
	tests := []struct {
		name  string
		state troubleshoot.State
		want  string
	}{
		{
			name:  "nothing installed",
			state: troubleshoot.State{},
			want:  "Ask an AI assistant",
		},
		{
			name:  "installed but never connected",
			state: troubleshoot.State{ServerInstalled: true, AgentInstalled: true},
			want:  "not connected",
		},
		{
			name: "ready",
			state: troubleshoot.State{
				ServerInstalled: true, AgentInstalled: true, Wired: true, Provider: "gemini-cli",
			},
			want: "Ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TroubleshootRow(tt.state).Subtitle; !strings.Contains(got, tt.want) {
				t.Errorf("subtitle = %q, want it to mention %q", got, tt.want)
			}
		})
	}
}

// The default configuration sends system details to Google. A row that only
// said "AI assistant" would let a user assume it runs locally.
func TestTroubleshootRowNamesWhoAnswersTheQuestions(t *testing.T) {
	ready := troubleshoot.State{ServerInstalled: true, AgentInstalled: true, Wired: true}

	ready.Provider = "gemini-cli"
	if got := TroubleshootRow(ready).Subtitle; !strings.Contains(got, "Google") {
		t.Errorf("gemini subtitle does not name Google: %q", got)
	}

	ready.Provider = "ollama"
	if got := TroubleshootRow(ready).Subtitle; !strings.Contains(got, "stay on this machine") {
		t.Errorf("local subtitle does not say questions stay local: %q", got)
	}

	ready.Provider = "anthropic"
	if got := TroubleshootRow(ready).Subtitle; !strings.Contains(got, "anthropic") {
		t.Errorf("subtitle drops an unrecognized provider: %q", got)
	}

	ready.Provider = ""
	if got := TroubleshootRow(ready).Subtitle; !strings.Contains(got, "no AI service") {
		t.Errorf("subtitle claims a provider when none is set: %q", got)
	}
}

// Every step can succeed while the feature stays unusable, because
// goose-mcp-setup exits 0 without writing when a configuration exists.
func TestTroubleshootSetupSubtitleReportsASilentNoOp(t *testing.T) {
	got := TroubleshootSetupSubtitle(troubleshoot.State{ServerInstalled: true, AgentInstalled: true})

	if !strings.Contains(got, "by hand") {
		t.Errorf("subtitle = %q, want it to say what the user must do", got)
	}
}

func TestTroubleshootSetupNoteSaysTheToolsAreReadOnly(t *testing.T) {
	if !strings.Contains(TroubleshootSetupNote(), "read-only") {
		t.Errorf("setup note does not say the system access is read-only: %q", TroubleshootSetupNote())
	}
}
