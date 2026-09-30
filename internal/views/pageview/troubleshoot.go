package pageview

import (
	"fmt"

	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

// TroubleshootRow returns the Enhanced Troubleshooting row text for a host's
// current state.
func TroubleshootRow(state troubleshoot.State) Row {
	row := Row{Title: "Enhanced Troubleshooting"}

	switch {
	case state.Ready():
		row.Subtitle = "Ready — " + TroubleshootProviderNote(state.Provider)
	case state.ServerInstalled && state.AgentInstalled:
		// The packages exist, but their diagnostic configuration is not ready.
		row.Subtitle = "Installed, but not connected to this system yet"
	default:
		row.Subtitle = "Ask an AI assistant about your logs, services, and network"
	}
	return row
}

// TroubleshootProviderNote names the user's selected service. The premade
// configuration chooses no provider, and a provider name alone proves no locality.
func TroubleshootProviderNote(provider string) string {
	switch provider {
	case "":
		return "no AI service configured yet"
	case "gemini-cli":
		return "questions go to Google Gemini"
	case "ollama":
		return "uses your configured Ollama service"
	default:
		return fmt.Sprintf("questions go to %s", provider)
	}
}

// TroubleshootSetupSubtitle returns the subtitle after setup finishes.
// Setup can succeed at every step and still leave the feature unusable,
// which the row has to say rather than reporting a bare success.
func TroubleshootSetupSubtitle(state troubleshoot.State) string {
	if state.Ready() {
		return "Ready — " + TroubleshootProviderNote(state.Provider)
	}
	if state.ServerInstalled && state.AgentInstalled {
		return "Installed, but Linux diagnostics are not ready — review /usr/share/ublue-os/goose/config.yaml"
	}
	return "Setup did not complete"
}

// TroubleshootSetupNote is the one-line explanation shown before setup runs.
func TroubleshootSetupNote() string {
	return "Installs Goose and the read-only Linux tools it uses to inspect this system"
}
