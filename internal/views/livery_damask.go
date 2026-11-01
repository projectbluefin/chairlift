package views

import (
	"log"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
)

// probeDamask asks Flatpak whether Damask is installed. It shells out, so it
// runs off the main thread. Flatpak's own absence hides the control; any other
// failed query is unknown, never absent.
func probeDamask() pageview.DamaskState {
	installed, err := livery.DamaskInstalled()
	if err != nil {
		log.Printf("wallpapers: %v", err)
	}
	return pageview.DamaskOffer(installed, err != nil, installed || flatpak.IsInstalledCached())
}

// discoverDamask adds the single stateful Damask row to the Wallpapers group
// when Flatpak can install or open it. It runs off the main thread and never
// changes anything on its own: opening Livery only reads Flatpak's inventory.
func (uh *UserHome) discoverDamask(page *adw.PreferencesPage) {
	state := probeDamask()
	if state == pageview.DamaskHidden {
		return
	}
	sgtk.RunOnMainThread(func() {
		if uh.wallpaperDamaskRow != nil {
			return
		}
		group := uh.ensureWallpapersGroup(page)
		row := adw.NewActionRow()
		row.SetUseMarkup(false)
		uh.wallpaperDamaskSpinner = newActivitySpinner()
		row.AddSuffix(&uh.wallpaperDamaskSpinner.Widget)
		button := gtk.NewButtonWithLabel("")
		button.SetValign(gtk.AlignCenterValue)
		row.AddSuffix(&button.Widget)
		uh.wallpaperDamaskRow = row
		uh.wallpaperDamaskButton = button
		uh.showDamaskState(state)
		group.Add(&row.Widget)
		clicked := func(_ gtk.Button) { uh.onDamaskActivated() }
		button.ConnectClicked(&clicked)
	})
}

// showDamaskState records a confirmed Damask state and redraws the one row and
// its one button to match. Main thread only.
func (uh *UserHome) showDamaskState(state pageview.DamaskState) {
	uh.wallpaperDamaskState = state
	row := pageview.DamaskRow(state)
	uh.wallpaperDamaskRow.SetTitle(row.Title)
	uh.wallpaperDamaskRow.SetSubtitle(row.Subtitle)
	uh.wallpaperDamaskButton.SetLabel(pageview.DamaskActionName(state))
}

// onDamaskActivated runs the action the button currently names: open an
// installed Damask, install an absent one, or re-check an unknown probe. The
// row keeps its last confirmed state on any failure, so the same button is the
// retry. An install is confirmed by Flatpak's inventory afterwards, not by the
// install command returning.
func (uh *UserHome) onDamaskActivated() {
	if !uh.wallpaperDamaskGate.TryStart() {
		return
	}
	state := uh.wallpaperDamaskState
	uh.wallpaperDamaskButton.SetSensitive(false)
	setActivitySpinner(uh.wallpaperDamaskSpinner, true)
	go func() {
		preview := dryrun.Enabled()
		var err error
		next := state
		switch state {
		case pageview.DamaskInstalled:
			err = livery.DamaskOpen()
		case pageview.DamaskInstallable:
			err = livery.DamaskInstall()
			if err == nil && !preview {
				next = probeDamask()
			}
		case pageview.DamaskUnknown:
			next = probeDamask()
		}
		sgtk.RunOnMainThread(func() {
			uh.wallpaperDamaskGate.Reset()
			setActivitySpinner(uh.wallpaperDamaskSpinner, false)
			uh.wallpaperDamaskButton.SetSensitive(true)
			if err != nil {
				log.Printf("wallpapers: damask: %v", err)
				uh.toastAdder.ShowErrorToast(pageview.DamaskFailure(state))
				return
			}
			switch state {
			case pageview.DamaskInstalled:
				uh.toastAdder.ShowToast(actionmsg.DamaskOpen(preview))
			case pageview.DamaskInstallable:
				if preview {
					uh.toastAdder.ShowToast(actionmsg.DamaskInstall(true))
					return
				}
				if next != pageview.DamaskInstalled {
					log.Printf("wallpapers: %s install finished but Flatpak does not list it", livery.DamaskID)
					uh.toastAdder.ShowErrorToast(pageview.DamaskFailure(state))
					return
				}
				uh.showDamaskState(next)
				uh.toastAdder.ShowToast(actionmsg.DamaskInstall(false))
			case pageview.DamaskUnknown:
				if next == pageview.DamaskUnknown || next == pageview.DamaskHidden {
					uh.toastAdder.ShowErrorToast(pageview.DamaskFailure(state))
					return
				}
				uh.showDamaskState(next)
			}
		})
	}()
}
