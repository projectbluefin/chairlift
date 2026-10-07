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
		return "This computer is missing a part Agent Mode needs."
	case aistack.StateProvisioning:
		return "Checking…"
	case aistack.StateReady:
		return "Ready. Restart open apps to use it."
	case aistack.StateDegraded:
		return "Agent Mode isn't responding. Turn it off and on again."
	case aistack.StateDisabled:
		return "Off. Your downloaded models were kept."
	default:
		return "Turn on to download and set up Agent Mode."
	}
}

func AgentModeWorkingSubtitle(enabling bool) string {
	if enabling {
		return "Setting up… The first time can take several minutes."
	}
	return "Turning off…"
}

// A failure may occur before or after the files changed. The view re-observes
// the actual state instead of claiming every failed disable is still running.
func AgentModeFailureToast(enabling bool) string {
	if enabling {
		return "Couldn't turn on Agent Mode. Try again."
	}
	return "Couldn't turn off Agent Mode. Try again."
}

func AgentModeActiveModelTitle() string {
	return "Active Model"
}

func AgentModeActiveModelSubtitle(modelRef string) string {
	if modelRef == "" {
		return "No model selected. Choose one below."
	}
	return modelRef
}

func AgentModeModelUnavailable(s aistack.State) string {
	if s == aistack.StateProvisioning {
		return "Waiting for Agent Mode…"
	}
	if s == aistack.StateDegraded {
		return "Available when Agent Mode is ready."
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

func GooseRowTitle() string {
	return "Goose"
}

func GooseLaunchButtonLabel() string {
	return "Launch"
}

func AskBluefinMenuRowTitle() string {
	return "Show Ask Bluefin in menu"
}

func AskBluefinMenuRowSubtitle() string {
	return "Show Ask Bluefin in the top bar menu."
}

// AgentModeManageTitle titles the row that opens llmman's web UI.
func AgentModeManageTitle() string { return "Models and Chat" }

// AgentModeManageSubtitle says what the web UI is for.
func AgentModeManageSubtitle() string {
	return "Download, remove, and chat with models."
}

// AgentModeManageLabel is the button that opens it.
func AgentModeManageLabel() string { return "Open" }
