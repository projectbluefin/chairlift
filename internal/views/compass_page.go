package views

import (
	"log"

	"github.com/projectbluefin/chairlift/internal/compass"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
)

// buildCompassGroup builds the Compass switch. The switch stays insensitive
// until Flatpak answers, and then reflects only observed installed state.
func (uh *UserHome) buildCompassGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.CompassTitle)

	row := adw.NewActionRow()
	row.SetUseMarkup(false)
	row.SetTitle(pageview.CompassTitle)
	row.SetSubtitle(pageview.CompassCheckingSubtitle)
	uh.compassSpinner = newActivitySpinner()
	row.AddSuffix(&uh.compassSpinner.Widget)

	var toggle *guardedSwitch
	toggle = newGuardedSwitch(false, func(state bool) {
		if !uh.compassGate.TryStart() {
			sgtk.RunOnMainThread(func() { toggle.set(!state) })
			return
		}
		if state {
			uh.runCompassToggle(true, toggle, row)
			return
		}
		uh.confirmCompassDisable(toggle, row)
	})
	SetAccessibleLabel(toggle.widget, pageview.CompassTitle)
	toggle.widget.SetSensitive(false)
	row.AddSuffix(&toggle.widget.Widget)
	row.SetActivatableWidget(&toggle.widget.Widget)
	group.Add(&row.Widget)
	page.Add(group)
	// The walkthrough asserts this marker: Compass renders below the captured
	// Features viewport, so the screenshot alone cannot show it was built.
	log.Printf("views: compass group built")

	go func() {
		state, err := compass.Status()
		sgtk.RunOnMainThread(func() { uh.showCompassState(state, err, toggle, row) })
	}()
}

// showCompassState renders an observed state. A failed read leaves the switch
// insensitive rather than guessing; an unsupported architecture or a missing
// remote leaves an installed copy removable but refuses to start an install
// that cannot succeed.
func (uh *UserHome) showCompassState(state compass.State, err error, toggle *guardedSwitch, row *adw.ActionRow) {
	if err != nil {
		log.Printf("views: compass status unavailable: %v", err)
		row.SetSubtitle(pageview.CompassUnavailableSubtitle)
		toggle.widget.SetSensitive(false)
		return
	}
	toggle.set(state.Installed())
	toggle.widget.SetSensitive(state.Installed() || (state.Supported && state.RemoteConfigured))
	row.SetSubtitle(pageview.CompassSubtitle(state.Installed(), state.Supported, state.RemoteConfigured))
}

// confirmCompassDisable asks before removing Compass, because the system copy
// is shared by every account on this computer. The caller holds compassGate.
func (uh *UserHome) confirmCompassDisable(toggle *guardedSwitch, row *adw.ActionRow) {
	dialog := adw.NewAlertDialog("Remove Compass?", "Compass is removed for every account on this computer. Your Compass settings are kept.")
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("remove", "Remove")
	dialog.SetResponseAppearance("remove", adw.ResponseDestructiveValue)
	dialog.SetDefaultResponse("cancel")
	dialog.SetCloseResponse("cancel")
	uh.compassDialogs.connect(dialog, func(response string) {
		if response == "remove" {
			uh.runCompassToggle(false, toggle, row)
			return
		}
		uh.compassGate.Reset()
		toggle.set(true)
	})
	dialog.Present(&uh.featuresPage.Widget)
}

// runCompassToggle installs or removes Compass off the main thread, then
// re-reads the installations so the switch shows what actually happened.
// The caller holds compassGate.
func (uh *UserHome) runCompassToggle(enable bool, toggle *guardedSwitch, row *adw.ActionRow) {
	toggle.widget.SetSensitive(false)
	row.SetSubtitle(pageview.CompassWorkingSubtitle(enable))
	setActivitySpinner(uh.compassSpinner, true)
	preview := dryrun.Enabled()
	go func() {
		var changed bool
		var err error
		if enable {
			changed, err = compass.Enable()
		} else {
			changed, err = compass.Disable()
		}
		if err != nil {
			log.Printf("views: compass toggle failed: %v", err)
		}
		state, stateErr := compass.Status()
		sgtk.RunOnMainThread(func() {
			defer uh.compassGate.Reset()
			setActivitySpinner(uh.compassSpinner, false)
			uh.showCompassState(state, stateErr, toggle, row)
			toast, isError := pageview.CompassToast(preview, enable, changed, err != nil)
			if isError {
				uh.toastAdder.ShowErrorToast(toast)
				return
			}
			uh.toastAdder.ShowToast(toast)
		})
	}()
}
