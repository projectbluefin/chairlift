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
		row.Subtitle = "Ready — " + providerNote(state.Provider)
	case state.ServerInstalled && state.AgentInstalled:
		// Everything is installed but the agent cannot see the system: the
		// state goose-mcp-setup leaves behind when a config already exists.
		row.Subtitle = "Installed, but not connected to this system yet"
	default:
		row.Subtitle = "Ask an AI assistant about your logs, services, and network"
	}
	return row
}

// providerNote says which service answers the questions. It is the one thing
// about this feature a user cannot discover from the UI: the setup script
// configures Google's Gemini by default, so the row says so rather than
// leaving "AI assistant" to imply the work happens locally.
func providerNote(provider string) string {
	switch provider {
	case "":
		return "no AI service configured yet"
	case "gemini-cli":
		return "questions go to Google Gemini"
	case "ollama":
		return "questions stay on this machine"
	default:
		return fmt.Sprintf("questions go to %s", provider)
	}
}

// TroubleshootProviderNote exposes providerNote for the rows that report a
// provider outside the main row text.
func TroubleshootProviderNote(provider string) string {
	return providerNote(provider)
}

// TroubleshootSetupSubtitle returns the subtitle after setup finishes.
// Setup can succeed at every step and still leave the feature unusable,
// which the row has to say rather than reporting a bare success.
func TroubleshootSetupSubtitle(state troubleshoot.State) string {
	if state.Ready() {
		return "Ready — " + providerNote(state.Provider)
	}
	if state.ServerInstalled && state.AgentInstalled {
		return "Installed, but Goose already had a configuration — add the linux-tools extension to it by hand"
	}
	return "Setup did not complete"
}

// TroubleshootSetupNote is the one-line explanation shown before setup runs.
func TroubleshootSetupNote() string {
	return "Installs Goose and the read-only Linux tools it uses to inspect this system"
}
