package pageview

import "github.com/projectbluefin/chairlift/internal/aistack"

func AgentModeGroupTitle() string {
	return "Local AI"
}

func AgentModeGroupDescription() string {
	return "Run models on this computer. Prompts are not saved."
}

func AgentModeRowTitle() string {
	return "Agent Mode"
}

// Readiness is observed, not inferred from installed software.
func AgentModeSubtitle(s aistack.State) string {
	switch s {
	case aistack.StateUnavailable:
		return "Not available on this computer — Homebrew is not installed."
	case aistack.StateProvisioning:
		return "Checking the model server…"
	case aistack.StateReady:
		return "Ready. Restart already-open apps and terminals to connect."
	case aistack.StateDegraded:
		return "The model server is not answering. Turn Agent Mode off and on to retry."
	case aistack.StateDisabled:
		return "Off. Downloaded software and models were kept."
	default:
		return "Turn on to install the model server and download its engine."
	}
}

func AgentModeWorkingSubtitle(enabling bool) string {
	if enabling {
		return "Setting up… This can take several minutes the first time."
	}
	return "Stopping…"
}

// A failure may occur before or after the files changed. The view re-observes
// the actual state instead of claiming every failed disable is still running.
func AgentModeFailureToast(enabling bool) string {
	if enabling {
		return "Agent Mode could not be turned on. Check its current status above."
	}
	return "Could not finish turning off Agent Mode. Check its current status above."
}

func AgentModeActiveModelTitle() string {
	return "Active Model"
}

func AgentModeActiveModelSubtitle(modelRef string) string {
	if modelRef == "" {
		return "No model selected — choose a preset."
	}
	return modelRef
}

func AgentModeModelUnavailable(s aistack.State) string {
	if s == aistack.StateProvisioning {
		return "Waiting for the model server…"
	}
	if s == aistack.StateDegraded {
		return "Available when the model server is ready."
	}
	return "Turn on Agent Mode to choose a model."
}

func AgentModePresetsTitle() string {
	return "Recommended Presets"
}

func AgentModePresetsSubtitle() string {
	return "Choose a model that fits this computer's memory."
}

func AgentModeSwitchPresetLabel() string {
	return "Choose…"
}
