package updateflow

import (
	"context"
	"time"

	"github.com/projectbluefin/chairlift/internal/userprefs"
)

// SourceID identifies one update provider.
type SourceID string

const (
	Applications     SourceID = "applications"
	DeveloperTools   SourceID = "developer-tools"
	SystemComponents SourceID = "system-components"
	OperatingSystem  SourceID = "operating-system"
)

// Phase is the aggregate state of the update flow.
type Phase uint8

const (
	PhaseIdle Phase = iota
	PhaseChecking
	PhaseReady
	PhaseCheckFailed
	PhaseUpdating
	PhasePartialFailure
	PhaseRestartRequired
)

// Action is the primary action offered for a snapshot.
type Action uint8

const (
	ActionNone Action = iota
	ActionCheck
	ActionUpdateAll
	ActionRetryFailed
	// ActionRestart is offered when an update is staged and only a restart
	// can finish it. Without it PhaseRestartRequired renders a status that
	// tells the user to restart and gives them nothing to press.
	ActionRestart
)

// Item is one pending update.
type Item struct {
	Name             string
	CurrentVersion   string
	AvailableVersion string
	Scope            string
}

// SourceState is the coordinator-owned state for one provider.
type SourceState struct {
	ID              SourceID
	Configured      bool
	Available       bool
	Enabled         bool
	Checking        bool
	Updating        bool
	Items           []Item
	CheckErr        error
	ApplyErr        error
	Completed       bool
	RestartRequired bool
}

// Snapshot is an immutable value published by Coordinator.
type Snapshot struct {
	Generation       uint64
	Phase            Phase
	Action           Action
	Sources          []SourceState
	Current          SourceID
	Progress         string
	TotalUpdates     int
	CompletedSources []SourceID
	FailedSources    []SourceID
	LastChecked      time.Time
	Preview          bool
	MaintenanceRan   bool
	MaintenanceErr   error
}

// Progress is one provider progress update.
type Progress struct {
	Source  SourceID
	Message string
}

// CheckResult is the normalized result of a provider check.
type CheckResult struct {
	Items           []Item
	RestartRequired bool
}

// ApplyResult is the normalized result of a provider mutation.
type ApplyResult struct {
	Changed         bool
	Preview         bool
	RestartRequired bool
}

// Provider supplies read-only checks and mutations for one source.
type Provider interface {
	ID() SourceID
	Available() bool
	Check(context.Context) (CheckResult, error)
	Apply(context.Context, []Item, func(Progress)) (ApplyResult, error)
}

// Maintenance supplies typed post-update cleanup.
type Maintenance interface {
	Run(context.Context, func(Progress)) error
}

func (s Snapshot) clone() Snapshot {
	s.Sources = cloneSources(s.Sources)
	s.CompletedSources = cloneSourceIDs(s.CompletedSources)
	s.FailedSources = cloneSourceIDs(s.FailedSources)
	return s
}

// RestartRequired reports whether any source still requires a restart.
func (s Snapshot) RestartRequired() bool {
	for _, source := range s.Sources {
		if source.RestartRequired {
			return true
		}
	}
	return false
}

func derive(s Snapshot) Snapshot {
	s = s.clone()
	s.TotalUpdates = 0
	if s.CompletedSources == nil {
		s.CompletedSources = make([]SourceID, 0)
	}
	s.FailedSources = make([]SourceID, 0)

	var checking, updating bool
	for _, source := range s.Sources {
		if source.Checking {
			checking = true
		}
		if source.Updating {
			updating = true
		}
		if source.Enabled && source.Configured && source.Available {
			s.TotalUpdates += len(source.Items)
		}
		if source.CheckErr != nil || source.ApplyErr != nil {
			if !containsSourceID(s.FailedSources, source.ID) {
				s.FailedSources = append(s.FailedSources, source.ID)
			}
		}
	}

	switch {
	case checking:
		s.Phase = PhaseChecking
		s.Action = ActionNone
	case updating:
		s.Phase = PhaseUpdating
		s.Action = ActionNone
	case hasApplyFailure(s.Sources):
		s.Phase = PhasePartialFailure
		s.Action = ActionRetryFailed
	case hasCheckFailure(s.Sources):
		s.Phase = PhaseCheckFailed
		s.Action = ActionCheck
	case s.MaintenanceErr != nil:
		s.Phase = PhasePartialFailure
		s.Action = ActionRetryFailed
	case s.RestartRequired() && s.TotalUpdates == 0:
		s.Phase = PhaseRestartRequired
		s.Action = ActionRestart
	case !hasConfiguredAvailable(s.Sources):
		s.Phase = PhaseReady
		s.Action = ActionNone
	default:
		s.Phase = PhaseReady
		if s.TotalUpdates > 0 {
			s.Action = ActionUpdateAll
		} else {
			s.Action = ActionCheck
		}
	}
	return s
}

func retrySources(s Snapshot) []SourceID {
	ids := make([]SourceID, 0)
	for _, source := range s.Sources {
		if source.ApplyErr != nil {
			ids = append(ids, source.ID)
		}
	}
	return ids
}

func cloneSources(sources []SourceState) []SourceState {
	if sources == nil {
		return nil
	}
	cloned := make([]SourceState, len(sources))
	for i, source := range sources {
		cloned[i] = source
		if source.Items != nil {
			cloned[i].Items = append([]Item(nil), source.Items...)
		}
	}
	return cloned
}

func cloneSourceIDs(ids []SourceID) []SourceID {
	if ids == nil {
		return nil
	}
	return append([]SourceID(nil), ids...)
}

func containsSourceID(ids []SourceID, want SourceID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func hasApplyFailure(sources []SourceState) bool {
	for _, source := range sources {
		if source.ApplyErr != nil {
			return true
		}
	}
	return false
}

func hasCheckFailure(sources []SourceState) bool {
	for _, source := range sources {
		if source.CheckErr != nil {
			return true
		}
	}
	return false
}

func hasConfiguredAvailable(sources []SourceState) bool {
	for _, source := range sources {
		if source.Configured && source.Available {
			return true
		}
	}
	return false
}

func preferenceEnabled(values userprefs.Values, id SourceID) bool {
	switch id {
	case Applications:
		return values.Applications
	case DeveloperTools:
		return values.DeveloperTools
	case SystemComponents:
		return values.SystemComponents
	case OperatingSystem:
		return values.OperatingSystem
	default:
		return false
	}
}
