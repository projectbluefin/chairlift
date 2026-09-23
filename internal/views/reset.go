package views

import (
	"context"
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/distrobox"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/powerwash"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// Powerwash and Factory Reset are ChairLift's two irreversible maintenance
// actions, ported from finupdate's status_view.rs. Both are opt-in: the
// reset_group they live in ships disabled in config.yml, the same default
// maintenance_cleanup_group uses for its own privileged script actions.
// Both require an AdwAlertDialog confirmation with a destructive-styled
// response before anything runs, per the HIG's rule that destructive
// dialogs are reserved for genuinely non-undoable actions — which these are.
//
// The reset rows are built on the Recovery detail page, so the dialogs are
// parented to the Recovery page, never the Maintenance page. A reset is a
// deliberate Recovery action, not a routine cleanup control. maintenance_page
// guards reset_group and builds these rows on the recovery page; the rows are
// gated by reset_group (disabled by shipped default), the same default
// maintenance_cleanup_group uses.
// Powerwash needs no privilege: both its steps (removing user Flatpaks,
// removing Distrobox containers) run in the invoking account, the same
// reasoning as gaming mode. Factory Reset replaces the OS image itself and
// goes through chairlift-ublue-helper's factory-reset action.

// Button labels for the two recovery actions. Both open a confirmation
// dialog rather than acting immediately, which is what the ellipsis says.
const (
	powerwashButtonLabel    = "Remove…"
	powerwashBusyLabel      = "Removing…"
	factoryResetButtonLabel = "Reset…"
	factoryResetBusyLabel   = "Resetting…"
)

// buildResetGroup builds the Powerwash and Factory Reset rows. It is the
// last group on the Maintenance page and reads as its own section: these
// are recovery actions, not routine maintenance, and grouping them beside
// "Free up space" would invite a person looking for disk space to press one
// of them.
func (uh *UserHome) buildResetGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Recovery")
	group.SetDescription("For when something has gone wrong. Each one asks you to confirm, and cannot be undone.")

	powerwashRow := adw.NewActionRow()
	presentation := pageview.PowerwashRow()
	powerwashRow.SetTitle(presentation.Title)
	powerwashRow.SetSubtitle(presentation.Subtitle)
	powerwashBtn := gtk.NewButtonWithLabel(powerwashButtonLabel)
	powerwashBtn.SetValign(gtk.AlignCenterValue)
	powerwashBtn.AddCssClass("destructive-action")
	powerwashClickedCb := func(gtk.Button) { uh.onPowerwashClicked(powerwashBtn, powerwashRow) }
	powerwashBtn.ConnectClicked(&powerwashClickedCb)
	powerwashRow.AddSuffix(&powerwashBtn.Widget)
	group.Add(&powerwashRow.Widget)

	resetRow := adw.NewActionRow()
	resetPresentation := pageview.FactoryResetRow()
	resetRow.SetTitle(resetPresentation.Title)
	resetRow.SetSubtitle(resetPresentation.Subtitle)
	resetBtn := gtk.NewButtonWithLabel(factoryResetButtonLabel)
	resetBtn.SetValign(gtk.AlignCenterValue)
	resetBtn.AddCssClass("destructive-action")
	resetClickedCb := func(gtk.Button) { uh.onFactoryResetClicked(resetBtn, resetRow) }
	resetBtn.ConnectClicked(&resetClickedCb)
	resetRow.AddSuffix(&resetBtn.Widget)
	group.Add(&resetRow.Widget)

	page.Add(group)
	log.Printf("views: recovery group built")
}

// onPowerwashClicked shows the confirmation dialog, then runs Powerwash on
// confirm. The gate blocks a second click while a confirmation or a run is
// already in flight.
func (uh *UserHome) onPowerwashClicked(button *gtk.Button, row *adw.ActionRow) {
	if !uh.powerwashGate.TryStart() {
		return
	}

	title, body := pageview.PowerwashConfirmation()
	dialog := adw.NewAlertDialog(title, body)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("confirm", "Remove Everything")
	dialog.SetResponseAppearance("confirm", adw.ResponseDestructiveValue)

	responseCb := func(_ adw.AlertDialog, response string) {
		if response != "confirm" {
			uh.powerwashGate.Reset()
			return
		}
		uh.runPowerwash(button, row)
	}
	dialog.ConnectResponse(&responseCb)
	dialog.Present(&uh.recoveryPrefsPage.Widget)
}

func (uh *UserHome) runPowerwash(button *gtk.Button, row *adw.ActionRow) {
	button.SetSensitive(false)
	button.SetLabel(powerwashBusyLabel)

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		runner := powerwash.Runner{
			FlatpakInstalled:   flatpak.IsInstalledCached,
			RemoveFlatpaks:     func(context.Context) error { return flatpak.RemoveAllUser() },
			DistroboxInstalled: distrobox.IsInstalled,
			RemoveDistroboxes:  distrobox.RemoveAll,
		}
		results := runner.Run(ctx)
		summary := powerwash.Summarize(results)

		log.Printf("views: powerwash finished succeeded=%d failed=%d skipped=%d",
			summary.Succeeded, summary.Failed, summary.Skipped)

		sgtk.RunOnMainThread(func() {
			uh.powerwashGate.Reset()
			button.SetSensitive(true)
			button.SetLabel(powerwashButtonLabel)

			decision := actionmsg.Powerwash(dryrun.Enabled(), summary.Succeeded, summary.Failed)
			if decision.Confirm {
				row.SetSubtitle(pageview.PowerwashResultSubtitle(summary.Succeeded, summary.Failed))
			}
			if summary.Failed > 0 {
				uh.toastAdder.ShowErrorToast(decision.Toast)
				return
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

// onFactoryResetClicked shows the confirmation dialog, then runs Factory
// Reset on confirm.
func (uh *UserHome) onFactoryResetClicked(button *gtk.Button, row *adw.ActionRow) {
	if !uh.factoryResetGate.TryStart() {
		return
	}

	title, body := pageview.FactoryResetConfirmation()
	dialog := adw.NewAlertDialog(title, body)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("confirm", "Factory Reset")
	dialog.SetResponseAppearance("confirm", adw.ResponseDestructiveValue)

	responseCb := func(_ adw.AlertDialog, response string) {
		if response != "confirm" {
			uh.factoryResetGate.Reset()
			return
		}
		uh.runFactoryReset(button, row)
	}
	dialog.ConnectResponse(&responseCb)
	dialog.Present(&uh.recoveryPrefsPage.Widget)
}

func (uh *UserHome) runFactoryReset(button *gtk.Button, row *adw.ActionRow) {
	button.SetSensitive(false)
	button.SetLabel(factoryResetBusyLabel)

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.FactoryReset(ctx)

		sgtk.RunOnMainThread(func() {
			uh.factoryResetGate.Reset()
			button.SetSensitive(true)
			button.SetLabel(factoryResetButtonLabel)

			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Factory reset failed: %v", err))
				return
			}

			decision := actionmsg.FactoryReset(dryrun.Enabled())
			if decision.Confirm {
				row.SetSubtitle(pageview.FactoryResetResultSubtitle())
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
