// Package agentmode provides readiness evaluation, launch coordination, and
// the Ask Bluefin dispatcher for Agent Mode's Goose Desktop client.
//
// Goose runs in a profile ChairLift writes immediately before every launch
// (internal/troubleshoot), so readiness never inspects a configuration file:
// it is the packages, the architecture they are published for, and Agent
// Mode's running model.
package agentmode

import (
	"context"
	"fmt"

	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

// State represents the readiness of Goose Desktop as the Agent Mode client.
type State int

const (
	// StateReady indicates all prerequisites are satisfied: supported
	// architecture, installed packages, healthy daemon, and active model.
	StateReady State = iota
	// StateDaemonUnavailable indicates llmman daemon is not running or unhealthy.
	StateDaemonUnavailable
	// StateModelUnavailable indicates no active model is selected.
	StateModelUnavailable
	// StatePackagesMissing indicates Goose Desktop or linux-mcp-server is
	// missing. It is the one state the Goose row can resolve itself, with
	// Set Up.
	StatePackagesMissing
	// StateUnsupported indicates Goose Desktop is not published for this
	// architecture.
	StateUnsupported
)

// String returns a human-readable representation of State.
func (s State) String() string {
	switch s {
	case StateReady:
		return "Ready"
	case StateDaemonUnavailable:
		return "DaemonUnavailable"
	case StateModelUnavailable:
		return "ModelUnavailable"
	case StatePackagesMissing:
		return "PackagesMissing"
	case StateUnsupported:
		return "Unsupported"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// Ready reports whether all launch prerequisites are satisfied.
func (s State) Ready() bool {
	return s == StateReady
}

// CanSetUp reports whether installing the packages is what stands between
// this host and a launch.
func (s State) CanSetUp() bool {
	return s == StatePackagesMissing
}

// MissingPrerequisite returns the description of the unmet requirement.
func (s State) MissingPrerequisite() string {
	switch s {
	case StateDaemonUnavailable:
		return "Agent Mode is not running."
	case StateModelUnavailable:
		return "No model is selected in Agent Mode."
	case StatePackagesMissing:
		return "Goose Desktop or linux-mcp-server is not installed."
	case StateUnsupported:
		return "Goose Desktop is only published for x86_64 computers."
	default:
		return ""
	}
}

// Subtitle returns a user-facing explanation of the current state.
func (s State) Subtitle(model string) string {
	switch s {
	case StateReady:
		if model != "" {
			return fmt.Sprintf("Ready to launch with %s.", model)
		}
		return "Ready to launch."
	case StateDaemonUnavailable:
		return "Turn on Agent Mode to launch Goose."
	case StateModelUnavailable:
		return "Choose a model to launch Goose."
	case StatePackagesMissing:
		return "Goose Desktop or linux-mcp-server is not installed."
	case StateUnsupported:
		return "Goose Desktop is only published for x86_64 computers."
	default:
		return ""
	}
}

// ReadinessFacts captures observed facts about Agent Mode and Goose.
type ReadinessFacts struct {
	DaemonHealthy bool
	// ActiveModel is the reference Agent Mode's alias resolves to, for
	// display. A launch asks llmman for the alias itself.
	ActiveModel string
	// Tools is what troubleshoot.Detect resolved: the architecture check
	// and the absolute paths of linux-mcp-server, goose-desktop, and llmman.
	Tools troubleshoot.State
}

// Evaluate determines the readiness State from the supplied facts.
//
// The packages are judged first because installing them is the one thing
// the Goose row can do itself; Agent Mode and its model are the switch and
// the chooser above it.
func Evaluate(facts ReadinessFacts) State {
	if !facts.Tools.Supported {
		return StateUnsupported
	}
	if !facts.Tools.Installed() {
		return StatePackagesMissing
	}
	if !facts.DaemonHealthy || facts.Tools.LLMManPath == "" {
		return StateDaemonUnavailable
	}
	if facts.ActiveModel == "" {
		return StateModelUnavailable
	}
	return StateReady
}

// Injection seams for testing live observation.
var (
	detectTools = troubleshoot.Detect
	checkDaemon = aistack.Healthy
	checkModel  = aistack.ReadActiveModel
)

// ObserveLive queries the live system state and evaluates readiness. It
// probes HTTP and runs llmman, so callers keep it off the GTK main thread.
func ObserveLive(ctx context.Context) (State, ReadinessFacts, error) {
	facts := ReadinessFacts{Tools: detectTools()}
	facts.DaemonHealthy = checkDaemon(ctx)
	if facts.DaemonHealthy {
		if model, err := checkModel(ctx); err == nil {
			facts.ActiveModel = model
		}
	}
	return Evaluate(facts), facts, nil
}
