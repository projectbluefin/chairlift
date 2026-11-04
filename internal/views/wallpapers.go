package views

import (
	"context"
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/devsetup"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/valhalla"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

func (uh *UserHome) buildWallpapersGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.LiveryWallpapersTitle)
	group.SetDescription(pageview.LiveryWallpapersDescription)
	presentation := pageview.WallpaperSetupRow(false, nil)
	row := adw.NewExpanderRow()
	row.SetUseMarkup(false)
	row.SetTitle(presentation.Title)
	row.SetSubtitle("Checking setup…")
	choice := gtk.NewCheckButton()
	choice.SetActive(true)
	choice.SetSensitive(false)
	choice.SetValign(gtk.AlignCenterValue)
	SetAccessibleLabel(choice, "Select More Dinosaurs")
	row.AddPrefix(&choice.Widget)
	spinner := newActivitySpinner()
	row.AddSuffix(&spinner.Widget)
	button := gtk.NewButtonWithLabel("Install")
	button.SetValign(gtk.AlignCenterValue)
	button.SetSensitive(false)
	row.AddSuffix(&button.Widget)
	for _, pkg := range pageview.WallpaperPackages() {
		entry := adw.NewActionRow()
		entry.SetUseMarkup(false)
		entry.SetTitle(pkg.Title)
		entry.SetSubtitle(pkg.Subtitle)
		row.AddRow(&entry.Widget)
	}
	uh.wallpaperRow, uh.wallpaperChoice, uh.wallpaperButton = row, choice, button
	uh.wallpaperSpinner = spinner
	clicked := func(gtk.Button) { uh.runWallpaperInstall() }
	button.ConnectClicked(&clicked)
	mapped := func(gtk.Widget) { uh.refreshWallpaperSetup() }
	group.ConnectMap(&mapped)
	destroyed := func(gtk.Widget) {
		uh.wallpaperDisposed = true
		uh.wallpaperRefresh.Begin()
	}
	group.ConnectDestroy(&destroyed)
	group.Add(&row.Widget)
	page.Add(group)
	uh.wallpaperActivity = newSetupActivity(page, valhalla.Stages[:])
}

// Readiness is passive and refreshed whenever Livery is shown. Only the newest
// observation may publish, and an installation invalidates earlier readers.
func (uh *UserHome) refreshWallpaperSetup() {
	if uh.wallpaperRow == nil || uh.wallpaperDisposed || uh.wallpaperGate.Running() {
		return
	}
	generation := uh.wallpaperRefresh.Begin()
	uh.wallpaperButton.SetSensitive(false)
	uh.wallpaperChoice.SetSensitive(false)
	setActivitySpinner(uh.wallpaperSpinner, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		installed, err := valhalla.Installed(ctx)
		sgtk.RunOnMainThread(func() {
			if uh.wallpaperDisposed || !uh.wallpaperRefresh.IsCurrent(generation) {
				return
			}
			if err == nil {
				uh.wallpaperInstalled = installed
			} else {
				log.Printf("Could not check More Dinosaurs setup: %v", err)
			}
			uh.renderWallpaperSetup(err)
		})
	}()
}

func (uh *UserHome) renderWallpaperSetup(err error) {
	if uh.wallpaperDisposed || uh.wallpaperRow == nil || uh.wallpaperChoice == nil || uh.wallpaperButton == nil {
		return
	}
	setActivitySpinner(uh.wallpaperSpinner, false)
	presentation := pageview.WallpaperSetupRow(uh.wallpaperInstalled, err)
	uh.wallpaperRow.SetSubtitle(presentation.Subtitle)
	if uh.wallpaperInstalled {
		uh.wallpaperChoice.SetActive(true)
	}
	uh.wallpaperChoice.SetSensitive(!uh.wallpaperInstalled)
	uh.wallpaperButton.SetSensitive(!uh.wallpaperInstalled)
	label := "Install"
	if uh.wallpaperInstalled {
		label = "Installed"
	}
	uh.wallpaperButton.SetLabel(label)
}

func (uh *UserHome) runWallpaperInstall() {
	if uh.wallpaperDisposed || uh.wallpaperChoice == nil || !uh.wallpaperChoice.GetActive() {
		if uh.toastAdder != nil {
			uh.toastAdder.ShowToast("No wallpaper setup selected.")
		}
		return
	}
	if !uh.wallpaperGate.TryStart() {
		return
	}
	if !uh.wallpaperActivity.begin("More Dinosaurs", valhalla.Stages[:], false, uh.runWallpaperInstall) {
		uh.wallpaperGate.Reset()
		return
	}
	uh.wallpaperRefresh.Begin()
	uh.wallpaperButton.SetSensitive(false)
	uh.wallpaperButton.SetLabel("Installing…")
	uh.wallpaperChoice.SetSensitive(false)
	setActivitySpinner(uh.wallpaperSpinner, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		report := func(stage devsetup.Stage, status devsetup.Status) {
			uh.wallpaperActivity.report(stage, status)
			if status == devsetup.Working {
				uh.wallpaperActivity.detail(string(stage) + "…")
			}
		}
		err := valhalla.Install(ctx, report)
		cancel()
		preview := dryrun.Enabled()
		sgtk.RunOnMainThread(func() {
			uh.wallpaperGate.Reset()
			uh.wallpaperActivity.finish(devsetup.Result{Err: err}, preview)
			if uh.wallpaperDisposed {
				return
			}
			if !preview {
				// The backend verifies complete readiness before live success.
				if err == nil {
					uh.wallpaperInstalled = true
				}
				uh.homebrewInventoryChanged()
			}
			uh.renderWallpaperSetup(nil)
			message := pageview.WallpaperSetupResult(err, preview)
			if err != nil {
				log.Print(message)
				uh.wallpaperRow.SetSubtitle(message)
			}
			if uh.toastAdder != nil {
				if err != nil {
					uh.toastAdder.ShowErrorToast(message)
				} else {
					uh.toastAdder.ShowToast(message)
				}
			}
		})
	}()
}
