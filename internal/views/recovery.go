package views

import (
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// createRecoveryPage builds the Recovery detail page: a ToolbarView whose
// header bar carries a back button (this is a detail, reached from System),
// hosting the Recovery preferences page.
func (uh *UserHome) createRecoveryPage() (*adw.ToolbarView, *adw.PreferencesPage) {
	toolbarView := adw.NewToolbarView()

	// Header bar with a back button: Recovery is a detail, so the user
	// returns to System. The back button only fires once the window has
	// wired closeRecoveryDetail, which it does right after New.
	headerBar := adw.NewHeaderBar()
	backButton := gtk.NewButtonFromIconName("go-previous-symbolic")
	backButton.SetTooltipText("Back to System")
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

// Recovery is the single named detail view a user opens deliberately to return
// to a previous system version or perform an explicitly scoped reset. It is
// reached from System, never from routine Free Up Space.
//
// The rollback controls stay gated by bootc_updates_group; the reset controls
// stay gated by reset_group (disabled by shipped default). Nothing here
// invents a mutation the backend cannot perform: the bootc Roll Back row
// appears only when bootc records a previous deployment.

// RecoveryPage returns the Recovery detail ToolbarView so the window can add
// it to its content stack. Nil-guarded: the page is always built by New.
func (uh *UserHome) RecoveryPage() *adw.ToolbarView {
	return uh.recoveryPage
}

// SetOpenRecoveryDetail wires the callback the System page calls to open the
// Recovery detail view.
func (uh *UserHome) SetOpenRecoveryDetail(fn func()) {
	uh.openRecoveryDetail = fn
}

// SetCloseRecoveryDetail wires the callback the Recovery back button calls to
// return to System.
func (uh *UserHome) SetCloseRecoveryDetail(fn func()) {
	uh.closeRecoveryDetail = fn
}

// buildRecoveryPage builds the Recovery detail page.
func (uh *UserHome) buildRecoveryPage() {
	page := uh.recoveryPrefsPage
	if page == nil {
		return
	}

	page.SetTitle("Recovery")

	// Roll Back: gated by the OS provider group, built hidden, revealed
	// asynchronously once a previous deployment is confirmed to exist.
	if uh.groupEnabled("updates_page", "bootc_updates_group") {
		uh.buildRecoveryRollbackGroup(page)
		go uh.loadBootcRollbackStatus()
	}
}

// buildRecoveryRollbackGroup builds the bootc Roll Back row on the Recovery
// page. It is built hidden and revealed asynchronously, so a fresh install
// never offers a rollback to nothing.
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
	uh.bootcRollbackRow.SetVisible(false)

	group := adw.NewPreferencesGroup()
	group.SetTitle("Roll Back")
	group.SetDescription("Return to the previous system version if an update went badly")
	group.Add(&uh.bootcRollbackRow.Widget)
	page.Add(group)
}

// loadBootcRollbackStatus reveals the Roll Back row when bootc records a
// rollback deployment. A host with no previous image — a fresh install, or
// one whose rollback slot has been pruned — leaves the row hidden rather than
// showing an inert control.
func (uh *UserHome) loadBootcRollbackStatus() {
	ctx, cancel := bootc.DefaultContext()
	defer cancel()

	status, err := bootc.GetStatus(ctx)

	sgtk.RunOnMainThread(func() {
		if uh.bootcRollbackRow == nil {
			return
		}
		if err != nil || status.Status.Rollback == nil {
			uh.bootcRollbackRow.SetVisible(false)
			return
		}

		deployment := status.Status.Rollback
		presentation := pageview.BootcRollbackRow(deployment.Version(), deployment.Timestamp())
		uh.bootcRollbackRow.SetSubtitle(presentation.Subtitle)
		uh.bootcRollbackRow.SetVisible(true)
		log.Printf("views: bootc rollback available version=%q", deployment.Version())
	})
}

// recoveryProvidersAvailable reports whether the Recovery detail view has
// anything to show: a reset (reset_group) or the bootc rollback provider
// (bootc_updates_group) is enabled. The System page uses it to decide
// whether to show its Recovery entry, so the entry's gate lives in one place
// and does not reach across pages from system_page.go.
func (uh *UserHome) recoveryProvidersAvailable() bool {
	return uh.groupEnabled("maintenance_page", "reset_group") ||
		uh.groupEnabled("updates_page", "bootc_updates_group")
}

// onBootcRollbackClicked stages a rollback to the previous deployment. It
// does not restart: rolling back and restarting are separate decisions.
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
				uh.bootcRollbackGate.Reset()
				button.SetSensitive(true)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Rollback failed: %v", err))
				return
			}

			decision := actionmsg.Rollback(dryrun.Enabled())
			if decision.Confirm {
				uh.bootcRollbackGate.Complete()
				button.SetSensitive(false)
				row.SetSubtitle(pageview.BootcRollbackResultSubtitle())
			} else {
				uh.bootcRollbackGate.Reset()
				button.SetSensitive(true)
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
