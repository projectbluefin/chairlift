package window

import (
	"github.com/projectbluefin/chairlift/internal/settings"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	"codeberg.org/puregotk/puregotk/v4/adw"
)

func (w *Window) onShowPreferences() {
	dialog := w.buildPreferences()
	dialog.Present(&w.Widget)
}

// buildPreferences builds the Preferences dialog. Its update-source rows
// come from pageview.UpdateSourcePreferences, the same table the setup
// assistant's Update Preferences step renders, so both surfaces bind the
// same keys under the same titles and availability rules.
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
	for _, preference := range pageview.UpdateSourcePreferences {
		row := adw.NewSwitchRow()
		row.SetTitle(preference.Title)
		row.SetSubtitle(pageview.UpdateSourcePreferenceSubtitle(states, ready, preference.ID))
		// Bind first: the default GSettings binding sets sensitivity from
		// the key's writability (dconf lockdown), which overwrote an
		// earlier "unavailable" decision. Narrow what the bind left.
		store.BindBoolean(preference.Key, &row.Object)
		row.SetSensitive(row.GetSensitive() && pageview.UpdateSourcePreferenceSensitive(states, ready, preference.ID))
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
