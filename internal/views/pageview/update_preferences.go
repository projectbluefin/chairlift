package pageview

import (
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/views/updatepresent"
)

// UpdateSourcePreference maps one source to its user preference key.
type UpdateSourcePreference struct {
	ID    updateflow.SourceID
	Key   string
	Title string
}

var UpdateSourcePreferences = []UpdateSourcePreference{
	{ID: updateflow.OperatingSystem, Key: "operating-system-enabled", Title: "Operating system"},
	{ID: updateflow.Applications, Key: "applications-enabled", Title: "Applications"},
	{ID: updateflow.DeveloperTools, Key: "developer-tools-enabled", Title: "Developer tools"},
	{ID: updateflow.SystemComponents, Key: "system-components-enabled", Title: "System components"},
}

// UpdateSourcePreferenceSubtitle distinguishes pending, configured, and
// supported sources. A locked source carries updatepresent.SourceLockReason,
// the same words the Updates page's row shows for it.
func UpdateSourcePreferenceSubtitle(states []updateflow.SourceState, ready bool, id updateflow.SourceID) string {
	if !ready {
		return "Checking availability…"
	}
	state, ok := updateSourceState(states, id)
	if !ok {
		return "Checking availability…"
	}
	return updatepresent.SourceLockReason(state)
}

// UpdateSourcePreferenceLocked reports a source no preference can turn on:
// disabled by the administrator or not backed by this host. Its switch is
// shown off rather than bound to the stored preference, which would read as
// a source that is on when nothing will ever check it. A source whose state
// is not yet known is not locked: it shows the stored preference, insensitive.
func UpdateSourcePreferenceLocked(states []updateflow.SourceState, ready bool, id updateflow.SourceID) bool {
	if !ready {
		return false
	}
	state, ok := updateSourceState(states, id)
	return ok && updatepresent.SourceLockReason(state) != ""
}

func UpdateSourcePreferenceSensitive(states []updateflow.SourceState, ready bool, id updateflow.SourceID) bool {
	if !ready {
		return false
	}
	state, ok := updateSourceState(states, id)
	return ok && updatepresent.SourceLockReason(state) == ""
}

func updateSourceState(states []updateflow.SourceState, id updateflow.SourceID) (updateflow.SourceState, bool) {
	for _, state := range states {
		if state.ID == id {
			return state, true
		}
	}
	return updateflow.SourceState{}, false
}
