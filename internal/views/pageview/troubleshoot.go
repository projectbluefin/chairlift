package pageview

import "github.com/projectbluefin/chairlift/internal/agentmode"

// GooseAction is what the Goose row's one button does.
type GooseAction int

const (
	// GooseNoAction shows the button insensitive: what the row names has to
	// happen elsewhere first (Agent Mode, a model) or cannot happen at all
	// (an unsupported architecture).
	GooseNoAction GooseAction = iota
	// GooseSetUp installs the missing packages.
	GooseSetUp
	// GooseLaunch starts a session.
	GooseLaunch
)

// Label is the button's text for the action.
func (a GooseAction) Label() string {
	if a == GooseSetUp {
		return "Set Up"
	}
	return GooseLaunchButtonLabel()
}

// GooseRowView is the Goose row's subtitle and button.
type GooseRowView struct {
	Subtitle string
	Action   GooseAction
}

// GooseRow decides the Goose row for a readiness state. Set Up is offered
// only for missing packages, because installing them is the one thing the
// row can do itself; Agent Mode and its model are the controls above it.
func GooseRow(state agentmode.State) GooseRowView {
	view := GooseRowView{Subtitle: state.Subtitle()}
	switch {
	case state.Ready():
		view.Action = GooseLaunch
	case state.CanSetUp():
		view.Action = GooseSetUp
	}
	return view
}

// GooseSetupToast reports a finished, live setup. Every step can succeed and
// still leave Goose blocked on Agent Mode, which the toast has to say rather
// than reporting a bare success.
func GooseSetupToast(state agentmode.State) string {
	if state.Ready() {
		return "Goose is ready."
	}
	return "Goose is installed. " + state.Subtitle()
}

// TroubleshootGroupTitle titles the group holding the Goose row and the
// Ask Bluefin menu switch. Ask Bluefin is one path into this feature, not
// its name.
func TroubleshootGroupTitle() string { return "Troubleshooting" }

// TroubleshootGroupDescription names both places a session's questions go: the
// Agent Mode model and the Project Bluefin knowledge base, which is searched
// online.
func TroubleshootGroupDescription() string {
	return "Goose looks into problems on this computer. Knowledge searches go online."
}
