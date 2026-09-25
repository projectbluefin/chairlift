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

// PeersGroupTitle is the peer-offload section's heading. It is deliberately
// not "Cluster" or "Nodes": this machine only ever asks another one for
// help, and is never made reachable itself.
func PeersGroupTitle() string {
	return "Use another machine"
}

// PeersGroupDescription states the one-way nature of the feature up front,
// including that the remote side needs its own separate setup.
func PeersGroupDescription() string {
	return "Send some requests, and their responses, to an llmman service on another machine you already set up. " +
		"This computer is never made reachable by anyone else, and that other machine must " +
		"separately turn on a non-loopback, authenticated llmman service and open its own firewall. " +
		"A plain http:// address sends prompts, responses, and the peer key to that machine unencrypted; " +
		"use https:// if the other machine supports it."
}

// PeersAddRowTitle is the action row that opens the add-peer dialog.
func PeersAddRowTitle() string {
	return "Add a peer…"
}

// PeersEmptyRowTitle is shown when no peer is configured yet.
func PeersEmptyRowTitle() string {
	return "No peers configured"
}

// PeersAPIKeyRowTitle labels the shared credential field, sent to every
// authenticated peer. It is never displayed once entered.
func PeersAPIKeyRowTitle() string {
	return "Peer key (if the other machine requires one)"
}

// PeerAddDialogTitle is the add-peer dialog's heading.
func PeerAddDialogTitle() string {
	return "Add a peer"
}

// PeerAddDialogBody explains the address grammar accepted.
func PeerAddDialogBody() string {
	return "Enter the other machine's address, such as 10.0.0.5, spark.local:17434, or https://spark.local."
}

// PeerAddDialogPlaceholder is the entry's placeholder text.
func PeerAddDialogPlaceholder() string {
	return "host or host:port"
}

// PeerRemoveConfirmTitle confirms removing a configured peer.
func PeerRemoveConfirmTitle(address string) string {
	return "Remove " + address + "?"
}

// PeerRemoveConfirmBody explains that removal only stops offload, never
// touches the remote machine.
func PeerRemoveConfirmBody() string {
	return "This computer stops sending it requests. Nothing changes on the other machine."
}

// PeerDisabledSubtitle is shown for a disabled peer, which is never probed:
// llmman does not currently route requests to it, so a status check would
// only send its address and the shared key over the network for nothing.
func PeerDisabledSubtitle() string {
	return "Disabled — not checked"
}

// PeerStatusSubtitle renders one peer's probed status line. A peer that has
// never been probed yet reads as checking, never as unreachable.
func PeerStatusSubtitle(probed bool, status aistack.PeerStatus) string {
	if !probed {
		return "Checking…"
	}
	if status.Unauthorized {
		return "Rejected the configured key"
	}
	if !status.Reachable {
		return "Not reachable"
	}
	return "Reachable"
}

// PeerAddFailedToast is the toast for a rejected add.
func PeerAddFailedToast(reason string) string {
	return "Could not add that peer: " + reason
}

// PeerRemoveFailedToast is the toast for a failed remove.
func PeerRemoveFailedToast(reason string) string {
	return "Could not remove that peer: " + reason
}

// PeerEnableFailedToast is the toast for a failed enable.
func PeerEnableFailedToast(reason string) string {
	return "Could not enable that peer: " + reason
}

// PeerDisableFailedToast is the toast for a failed disable, worded
// separately from PeerEnableFailedToast rather than reusing
// PeerAddFailedToast's "add" wording for a disable.
func PeerDisableFailedToast(reason string) string {
	return "Could not disable that peer: " + reason
}

// PeerBusyToast is shown when a peer add, remove, or enable/disable is
// requested while another one is already in flight.
func PeerBusyToast() string {
	return "Another peer change is still in progress. Try again in a moment."
}

// PeerKeySavedToast confirms the shared key was sent to llmman's own
// configuration. The key itself is never echoed back.
func PeerKeySavedToast() string {
	return "Peer key saved."
}

// PeerKeyFailedToast is the toast for a failed key save.
func PeerKeyFailedToast(reason string) string {
	return "Could not save the peer key: " + reason
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
