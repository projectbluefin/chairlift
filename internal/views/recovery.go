package views

import (
	"log"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/pkexec"

	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// createRecoveryPage builds the Powerwash detail page: a ToolbarView whose
// header bar carries a back button (this is a detail, reached from Maintenance),
// hosting the Powerwash preferences page.
func (uh *UserHome) createRecoveryPage() (*adw.ToolbarView, *adw.PreferencesPage) {
	toolbarView := adw.NewToolbarView()

	// Header bar with a back button: Powerwash is a detail, so the user
	// returns to Maintenance. The back button only fires once the window has
	// wired closeRecoveryDetail, which it does right after New.
	headerBar := adw.NewHeaderBar()
	backButton := gtk.NewButtonFromIconName("go-previous-symbolic")
	backButton.SetTooltipText("Back to Maintenance")
	SetAccessibleLabel(backButton, "Back to Maintenance")
	backClickedCb := func(gtk.Button) {
		if uh.closeRecoveryDetail != nil {
			uh.closeRecoveryDetail()
		}
	}
	backButton.ConnectClicked(&backClickedCb)
	headerBar.PackStart(&backButton.Widget)

	toolbarView.AddTopBar(&headerBar.Widget)

	scrolled := gtk.NewScrolledWindow()
	scrolled.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scrolled.SetVexpand(true)

	prefsPage := adw.NewPreferencesPage()
	scrolled.SetChild(&prefsPage.Widget)

	toolbarView.SetContent(&scrolled.Widget)

	return toolbarView, prefsPage
}

// Powerwash is the single named detail view a user opens deliberately to return
// to a previous system version or perform an explicitly scoped reset. It is
// reached from Maintenance, never from routine Free Up Space.
//
// The rollback controls stay gated by bootc_updates_group; the reset controls
// stay gated by reset_group (disabled by shipped default). Nothing here
// invents a mutation the backend cannot perform: the bootc Roll Back row
// appears only when bootc records a previous deployment.

// RecoveryPage returns the Powerwash detail ToolbarView so the window can add
// it to its content stack. Nil-guarded: the page is always built by New.
func (uh *UserHome) RecoveryPage() *adw.ToolbarView {
	return uh.recoveryPage
}

// SetOpenRecoveryDetail wires the callback the Maintenance page calls to open the
// Powerwash detail view.
func (uh *UserHome) SetOpenRecoveryDetail(fn func()) {
	uh.openRecoveryDetail = fn
}

// SetCloseRecoveryDetail wires the callback the Powerwash back button calls to
// return to Maintenance.
func (uh *UserHome) SetCloseRecoveryDetail(fn func()) {
	uh.closeRecoveryDetail = fn
}

// buildRecoveryPage builds the Powerwash detail page.
func (uh *UserHome) buildRecoveryPage() {
	page := uh.recoveryPrefsPage
	if page == nil {
		return
	}

	page.SetTitle("Powerwash")
	// The reset rows (built by buildMaintenancePage when reset_group is on)
	// carry their own description; without them the page says why, so the
	// destination's name does not send the user hunting for a reset.
	page.SetDescription(pageview.RecoveryPageDescription(pageview.ResetAvailabilityFor(
		uh.config.IsGroupEnabled("maintenance_page", "reset_group"),
		uh.groupEnabled("maintenance_page", "reset_group"),
	)))

	// Roll Back: gated by the OS provider group, built hidden, revealed
	// asynchronously once a previous deployment is confirmed to exist.
	// The published-versions calendar (pin / return to stream) is withdrawn
	// (#522): its dated tags exist only for the deprecated `latest` stream.
	if uh.groupEnabled("updates_page", "bootc_updates_group") {
		uh.buildRecoveryRollbackGroup(page)
		go uh.loadBootcRollbackStatus()
	}
	uh.refreshRecoveryEntry()
}

// refreshRecoveryEntry rewrites the Maintenance page's Powerwash entry
// subtitle from what the detail currently holds. Main thread only.
func (uh *UserHome) refreshRecoveryEntry() {
	if uh.recoveryEntryRow == nil {
		return
	}
	uh.recoveryEntryRow.SetSubtitle(pageview.RecoveryEntrySubtitle(pageview.RecoveryOffer{
		Rollback: uh.bootcRollbackOffered,
		Reset:    uh.groupEnabled("maintenance_page", "reset_group"),
	}))
}

// buildRecoveryRollbackGroup builds the bootc Roll Back group on the Powerwash
// page. The whole group is built hidden and revealed asynchronously, so a
// fresh install shows neither a rollback to nothing nor its heading.
func (uh *UserHome) buildRecoveryRollbackGroup(page *adw.PreferencesPage) {
	// bootc Roll Back returns to the deployment bootc records as the
	// rollback target. Hidden until that deployment is confirmed to exist.
	uh.bootcRollbackRow = adw.NewActionRow()
	rollbackPresentation := pageview.BootcRollbackRow("", "")
	uh.bootcRollbackRow.SetTitle(rollbackPresentation.Title)
	uh.bootcRollbackRow.SetSubtitle(rollbackPresentation.Subtitle)
	uh.bootcRollbackBtn = gtk.NewButtonWithLabel("Roll Back")
	uh.bootcRollbackBtn.SetValign(gtk.AlignCenterValue)
	rollbackClickedCb := func(gtk.Button) {
		uh.onBootcRollbackClicked()
	}
	uh.bootcRollbackBtn.ConnectClicked(&rollbackClickedCb)
	uh.bootcRollbackRow.AddSuffix(&uh.bootcRollbackBtn.Widget)
	uh.bootcRestartBtn = gtk.NewButtonWithLabel("Restart now")
	uh.bootcRestartBtn.SetValign(gtk.AlignCenterValue)
	uh.bootcRestartBtn.AddCssClass("suggested-action")
	uh.bootcRestartBtn.SetVisible(false)
	restartClickedCb := func(gtk.Button) {
		uh.onBootcRollbackRestartClicked()
	}
	uh.bootcRestartBtn.ConnectClicked(&restartClickedCb)
	uh.bootcRollbackRow.AddSuffix(&uh.bootcRestartBtn.Widget)

	group := adw.NewPreferencesGroup()
	group.SetTitle("Roll Back")
	group.SetDescription("Go back if an update caused problems.")
	group.Add(&uh.bootcRollbackRow.Widget)
	group.SetVisible(false)
	page.Add(group)
	uh.bootcRollbackGroup = group
}

// loadBootcRollbackStatus reveals the Roll Back group when bootc records a
// rollback deployment. A host with no previous image — a fresh install, or
// one whose rollback slot has been pruned — leaves the whole group hidden
// rather than showing an inert control or an orphaned heading.
func (uh *UserHome) loadBootcRollbackStatus() {
	ctx, cancel := bootc.DefaultContext()
	defer cancel()

	status, err := bootc.GetStatus(ctx)
	// Roll Back runs the helper; without the command the row stays hidden.
	helperSupported := ublue.StatusCached().Supports(ubluehelper.CommandRollback)

	sgtk.RunOnMainThread(func() {
		if uh.bootcRollbackRow == nil || uh.bootcRollbackGroup == nil {
			return
		}
		offered := err == nil && status != nil && status.Status.Rollback != nil && helperSupported
		uh.bootcRollbackGroup.SetVisible(offered)
		uh.bootcRollbackOffered = offered
		uh.refreshRecoveryEntry()
		if !offered {
			return
		}

		deployment := status.Status.Rollback
		presentation := pageview.BootcRollbackRow(deployment.Version(), deployment.Timestamp())
		uh.bootcRollbackRow.SetSubtitle(presentation.Subtitle)
		log.Printf("views: bootc rollback available version=%q", deployment.Version())
	})
}

// recoveryProvidersAvailable reports whether the Powerwash detail view has
// anything to show: a reset (reset_group) or the bootc rollback provider
// (bootc_updates_group) is enabled. The Maintenance page uses it to decide
// whether to show its Powerwash entry, so the entry's gate lives in one place
// and does not reach across pages from maintenance_page.go.
func (uh *UserHome) recoveryProvidersAvailable() bool {
	return uh.groupEnabled("maintenance_page", "reset_group") ||
		uh.groupEnabled("updates_page", "bootc_updates_group")
}

// onBootcRollbackClicked queues a rollback to the previous deployment. It
// does not restart: rolling back and restarting are separate decisions, so
// a live success swaps in a Restart now button rather than rebooting.
func (uh *UserHome) onBootcRollbackClicked() {
	if !uh.bootcRollbackGate.TryStart() {
		return
	}

	button := uh.bootcRollbackBtn
	row := uh.bootcRollbackRow
	button.SetSensitive(false)

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.Rollback(ctx)

		sgtk.RunOnMainThread(func() {
			if err != nil {
				log.Printf("views: rollback failed: %v", err)
				uh.bootcRollbackGate.Reset()
				button.SetSensitive(true)
				uh.toastAdder.ShowErrorToast(pkexec.UserMessage(err, "Couldn't go back to the previous version. Try again."))
				return
			}

			decision := actionmsg.Rollback(dryrun.Enabled())
			if decision.Confirm {
				uh.bootcRollbackGate.Complete()
				button.SetSensitive(false)
				row.SetSubtitle(pageview.BootcRollbackResultSubtitle())
				if ublue.StatusCached().Supports(ubluehelper.CommandRestart) {
					button.SetVisible(false)
					uh.bootcRestartBtn.SetVisible(true)
				}
			} else {
				uh.bootcRollbackGate.Reset()
				button.SetSensitive(true)
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

// onBootcRollbackRestartClicked restarts into the queued rollback through the
// helper's fixed restart command, the same one Updates offers for a staged
// deployment.
func (uh *UserHome) onBootcRollbackRestartClicked() {
	button := uh.bootcRestartBtn
	button.SetSensitive(false)
	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()
		err := ublue.Restart(ctx)
		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			if err != nil {
				uh.toastAdder.ShowErrorToast(pkexec.UserMessage(err, "Couldn't restart. Try again."))
			}
		})
	}()
}
