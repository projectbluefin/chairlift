package pageview

import "github.com/projectbluefin/chairlift/internal/aistack"

// AgentModeGroupTitle is the Agents page's group heading.
func AgentModeGroupTitle() string {
	return "Agent Mode"
}

// AgentModeGroupDescription states the two properties that make the feature
// worth having: where answers come from, and that prompts are not kept.
func AgentModeGroupDescription() string {
	return "Answers are generated on this computer. Nothing you type is sent to a cloud service, and prompts are not saved."
}

// AgentModeRowTitle is the switch row's title.
func AgentModeRowTitle() string {
	return "Run AI models on this computer"
}

// AgentModeSubtitle is the switch row's subtitle for a resolved state. The
// ready text says which processes see the endpoint, because environment
// changes never reach an application that is already running.
func AgentModeSubtitle(s aistack.State) string {
	switch s {
	case aistack.StateUnavailable:
		return "Not available on this computer — Homebrew is not installed."
	case aistack.StateProvisioning:
		return "Checking whether the model server is answering…"
	case aistack.StateReady:
		return "Ready. Apps and terminals opened from now on find it; restart any that are already open."
	case aistack.StateDegraded:
		return "Turned on, but the model server is not answering. Turn it off and on again to retry."
	case aistack.StateDisabled:
		return "Off. The software and any downloaded models were kept."
	default:
		return "Turning this on installs llmman from Homebrew and downloads its engine, which can take a while."
	}
}

// AgentModeWorkingSubtitle is shown while the switch is acting.
func AgentModeWorkingSubtitle(enabling bool) string {
	if enabling {
		return "Setting up… This can take several minutes the first time."
	}
	return "Stopping…"
}

// AgentModeFailureSubtitle is shown when the switch could not do what was
// asked. A failed enable removes the unit it wrote but keeps software that
// Homebrew already installed; a failed disable keeps everything, because
// the service could not be proven stopped.
func AgentModeFailureSubtitle(enabling bool) string {
	if enabling {
		return "Could not turn on. Software that was already installed was kept."
	}
	return "Still running — it could not be stopped, so nothing was removed."
}

// AgentModeFailureToast is the toast for the same two failures. The error
// itself is logged: it names commands and unit files.
func AgentModeFailureToast(enabling bool) string {
	if enabling {
		return "Agent Mode could not be turned on."
	}
	return "Agent Mode is still running. It could not be stopped, so nothing was removed."
}

// AgentModeDetailsTitle is the expander holding what a person needs only
// when pointing another application at the model server.
func AgentModeDetailsTitle() string {
	return "Details"
}

// AgentModeDetails returns the rows behind the Details expander. jan reports
// whether this processor gets the Jan chat app, whose Flathub build is
// x86_64-only.
func AgentModeDetails(jan bool) []Row {
	chat := "Jan"
	if !jan {
		chat = "Not offered on this processor"
	}
	return []Row{
		{Title: "Model server", Subtitle: "llmman, installed with Homebrew"},
		// Without the address a person has a running server and no way to
		// reach it from anything.
		{Title: "Address for other apps", Subtitle: aistack.Address},
		{Title: "Found automatically by", Subtitle: "Apps that read OLLAMA_HOST, once restarted"},
		{Title: "Chat app", Subtitle: chat},
	}
}
