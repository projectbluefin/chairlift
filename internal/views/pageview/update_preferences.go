package pageview

import "github.com/projectbluefin/chairlift/internal/updateflow"

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
// supported sources. Preferences remains independent of the explicit wizard.
func UpdateSourcePreferenceSubtitle(states []updateflow.SourceState, ready bool, id updateflow.SourceID) string {
	if !ready {
		return "Checking availability…"
	}
	state, ok := updateSourceState(states, id)
	if !ok {
		return "Checking availability…"
	}
	if !state.Configured {
		return "Disabled by your administrator"
	}
	if !state.Available {
		return "Not available on this computer"
	}
	return ""
}

func UpdateSourcePreferenceSensitive(states []updateflow.SourceState, ready bool, id updateflow.SourceID) bool {
	if !ready {
		return false
	}
	state, ok := updateSourceState(states, id)
	return ok && state.Configured && state.Available
}

func updateSourceState(states []updateflow.SourceState, id updateflow.SourceID) (updateflow.SourceState, bool) {
	for _, state := range states {
		if state.ID == id {
			return state, true
		}
	}
	return updateflow.SourceState{}, false
}
