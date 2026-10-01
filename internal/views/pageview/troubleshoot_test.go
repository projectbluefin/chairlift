package pageview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

// A user-selected provider must remain visible; the preset chooses none.
func TestTroubleshootRowNamesWhoAnswersTheQuestions(t *testing.T) {
	ready := troubleshoot.State{ServerInstalled: true, AgentInstalled: true, Wired: true}

	ready.Provider = "gemini-cli"
	if got := TroubleshootRow(ready).Subtitle; !strings.Contains(got, "Google") {
		t.Errorf("gemini subtitle does not name Google: %q", got)
	}

	ready.Provider = "ollama"
	if got := TroubleshootRow(ready).Subtitle; !strings.Contains(got, "Ollama") || strings.Contains(got, "stay on this machine") {
		t.Errorf("Ollama subtitle misrepresents the selected service: %q", got)
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
