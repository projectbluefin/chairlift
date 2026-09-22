package window

import (
	"github.com/projectbluefin/chairlift/internal/settings"
	"github.com/projectbluefin/chairlift/internal/updateflow"

	"codeberg.org/puregotk/puregotk/v4/adw"
)

type sourcePreference struct {
	id  updateflow.SourceID
	key string
}

var sourcePreferences = []sourcePreference{
	{
		id:  updateflow.OperatingSystem,
		key: "operating-system-enabled",
	},
	{
		id:  updateflow.Applications,
		key: "applications-enabled",
	},
	{
		id:  updateflow.DeveloperTools,
		key: "developer-tools-enabled",
	},
	{
		id:  updateflow.SystemComponents,
		key: "system-components-enabled",
	},
}

func (w *Window) onShowPreferences() {
	dialog := w.buildPreferences()
	dialog.Present(&w.Widget)
}

func (w *Window) buildPreferences() *adw.PreferencesDialog {
	dialog := adw.NewPreferencesDialog()
	dialog.SetTitle("Preferences")

	store := settings.New()
	page := adw.NewPreferencesPage()
	page.SetTitle("Updates")

	sourcesGroup := adw.NewPreferencesGroup()
	sourcesGroup.SetTitle("Update sources")
	states := w.updateSourceStates()
	ready := w.updateSourcesReady()
	for _, preference := range sourcePreferences {
		row := adw.NewSwitchRow()
		switch preference.id {
		case updateflow.OperatingSystem:
			row.SetTitle("Operating system")
		case updateflow.Applications:
			row.SetTitle("Applications")
		case updateflow.DeveloperTools:
			row.SetTitle("Developer tools")
		case updateflow.SystemComponents:
			row.SetTitle("System components")
		}
		row.SetSubtitle(sourcePreferenceSubtitle(states, ready, preference.id))
		row.SetSensitive(sourcePreferenceSensitive(states, ready, preference.id))
		store.BindBoolean(preference.key, &row.Object)
		sourcesGroup.Add(&row.Widget)
	}
	page.Add(sourcesGroup)

	maintenanceGroup := adw.NewPreferencesGroup()
	maintenanceGroup.SetTitle("Maintenance")
	maintenanceRow := adw.NewSwitchRow()
	maintenanceRow.SetTitle("Run maintenance after updates")
	store.BindBoolean("maintenance-after-updates", &maintenanceRow.Object)
	maintenanceGroup.Add(&maintenanceRow.Widget)
	page.Add(maintenanceGroup)

	dialog.Add(page)
	return dialog
}

func (w *Window) updateSourceStates() []updateflow.SourceState {
	if w == nil || w.updateShell == nil {
		return nil
	}
	return w.updateShell.Sources()
}

func (w *Window) updateSourcesReady() bool {
	return w != nil && w.updateShell != nil && w.updateShell.SourcesReady()
}
func sourcePreferenceSubtitle(states []updateflow.SourceState, ready bool, id updateflow.SourceID) string {
	if !ready {
		return "Checking availability…"
	}
	state, ok := sourceState(states, id)
	if !ok {
		return "Checking availability…"
	}
	if !state.Configured {
		return "Disabled by your administrator"
	}
	if !state.Available {
		return "Not available on this system"
	}
	return ""
}

func sourcePreferenceSensitive(states []updateflow.SourceState, ready bool, id updateflow.SourceID) bool {
	if !ready {
		return false
	}
	state, ok := sourceState(states, id)
	return ok && state.Configured && state.Available
}

func sourceState(states []updateflow.SourceState, id updateflow.SourceID) (updateflow.SourceState, bool) {
	for _, state := range states {
		if state.ID == id {
			return state, true
		}
	}
	return updateflow.SourceState{}, false
}
