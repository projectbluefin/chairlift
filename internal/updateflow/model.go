package updateflow

import (
	"context"
	"errors"
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
)

// Item is one pending update.
type Item struct {
	// ID is the provider's execution identity, distinct from the display name.
	ID               string
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

// Policy is the static stance on one source for this session, as two
// independent facts: whether configuration enables it, and whether the host
// capability floor can back it. They stay separate because they are reported
// differently — an administrator's choice is not a missing tool — and a
// source is checked only when both hold and its provider is available.
type Policy struct {
	Configured bool
	Supported  bool
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
	// Maintaining is true only while post-update maintenance runs. No source
	// is updating then, yet the run is not finished: cleanup still executes
	// and may prompt for a password, so the phase stays PhaseUpdating.
	Maintaining bool
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

// ApplyResult is the normalized result of a provider mutation. Changed is
// true only when the mutation is verified to have applied the pending
// inventory. A provider that ran to a zero exit but left items pending —
// flatpak update's "Nothing to update." no-op, for example — reports false,
// and the coordinator keeps those items pending rather than claiming the
// source completed.
type ApplyResult struct {
	Changed         bool
	Preview         bool
	RestartRequired bool
}

// ErrUnavailable, wrapped in a Check error, reports that the host cannot back
// the source after all — something only a slow probe could tell, such as
// whether bootc is booted. The coordinator shows the source as not available
// on this system instead of failing the check.
var ErrUnavailable = errors.New("updateflow: source is not available on this system")

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
	case updating || s.Maintaining:
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
		// No page-level action: the Operating system row's "Restart now"
		// suffix is the only restart control (#439).
		s.Action = ActionNone
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

// StaleFor reports whether a user preference change has made this snapshot's
// source enablement wrong: some source the administrator configured and the
// host can back is enabled when the preference now says off, or the reverse.
// Sources locked by configuration or the capability floor never count,
// because no preference can change them. A stale snapshot needs a new Check,
// which recomputes enablement and checks any source the user turned on.
func (s Snapshot) StaleFor(preferences userprefs.Values) bool {
	for _, source := range s.Sources {
		if !source.Configured || !source.Available {
			continue
		}
		if source.Enabled != preferenceEnabled(preferences, source.ID) {
			return true
		}
	}
	return false
}
