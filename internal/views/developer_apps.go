package views

import (
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/developerapps"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

type developerAppRow struct {
	app     developerapps.App
	row     *adw.ActionRow
	button  *gtk.Button
	spinner *gtk.Spinner
	status  developerapps.Status
	checked bool
}

type developerAppsView struct {
	rows       []*developerAppRow
	buttons    buttonRoute
	refresh    actionstate.RefreshGate
	installing bool // GTK-thread only; passive reads stand aside during a mutation.
}

// buildDeveloperAppsGroup makes the curated IDE catalog part of Developer Mode
// itself. Installing an editor does not enable Developer Mode or launch a store.
func (uh *UserHome) buildDeveloperAppsGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Development Apps")
	group.SetDescription("Choose your editors and IDEs. Developer Mode does not need to be enabled.")
	view := &developerAppsView{}
	uh.developerApps = view
	for _, app := range developerapps.Catalog() {
		row := adw.NewActionRow()
		row.SetUseMarkup(false)
		row.SetTitle(app.Name)
		row.SetSubtitle(developerAppSubtitle(app))
		button := gtk.NewButtonWithLabel("Checking…")
		button.SetValign(gtk.AlignCenterValue)
		button.SetSensitive(false)
		spinner := newActivitySpinner()
		row.AddSuffix(&spinner.Widget)
		row.AddSuffix(&button.Widget)
		row.SetActivatableWidget(&button.Widget)
		setPackageButtonLabel(button, "Checking…", app.Name)
		item := &developerAppRow{app: app, row: row, button: button, spinner: spinner}
		view.rows = append(view.rows, item)
		view.buttons.connect(button, func(gtk.Button) { uh.installDeveloperApp(item) })
		group.Add(&row.Widget)
	}
	page.Add(group)
	uh.refreshDeveloperApps()
}

func developerAppSubtitle(app developerapps.App) string {
	source := string(app.Source)
	if app.Source == developerapps.Flatpak {
		source = fmt.Sprintf("Flatpak · %s · system", app.Remote)
	}
	return app.Description + " · " + source + " · " + app.Package
}

func (uh *UserHome) renderDeveloperApp(item *developerAppRow) {
	text := developerAppSubtitle(item.app)
	switch {
	case item.status.Installed:
		setPackageButtonLabel(item.button, "Installed", item.app.Name)
	case item.status.Err != nil:
		setPackageButtonLabel(item.button, "Retry", item.app.Name)
		text += " · Couldn't check installed status."
	case !item.status.Available:
		setPackageButtonLabel(item.button, "Unavailable", item.app.Name)
		text += " · " + item.status.Reason
	default:
		setPackageButtonLabel(item.button, "Install", item.app.Name)
	}
	item.row.SetSubtitle(text)
	item.button.SetSensitive(!uh.developerGate.Running() && !item.status.Installed && (item.status.Available || item.status.Err != nil))
}

// refreshDeveloperApps shares one batch across all rows; generations keep a
// passive inventory read from overwriting a newer install result.
func (uh *UserHome) refreshDeveloperApps() {
	view := uh.developerApps
	if view == nil || view.installing {
		return
	}
	generation := view.refresh.Begin()
	apps := make([]developerapps.App, len(view.rows))
	for i, item := range view.rows {
		apps[i] = item.app
	}
	go func() {
		states := developerapps.Inspect(apps)
		sgtk.RunOnMainThread(func() {
			if view.installing || !view.refresh.IsCurrent(generation) {
				return
			}
			for i, item := range view.rows {
				item.status = states[i]
				item.checked = true
				uh.renderDeveloperApp(item)
			}
		})
	}()
}

func (uh *UserHome) installDeveloperApp(item *developerAppRow) {
	if item.status.Err != nil {
		uh.refreshDeveloperApps()
		return
	}
	if !item.checked || item.status.Installed || !item.status.Available || !uh.developerGate.TryStart() {
		return
	}
	view := uh.developerApps
	view.installing = true
	view.refresh.Begin() // Invalidate observations made before this mutation.
	uh.setDeveloperSensitive(false)
	setPackageButtonLabel(item.button, "Installing…", item.app.Name)
	setActivitySpinner(item.spinner, true)
	preview := dryrun.Enabled()
	go func() {
		state, err := developerapps.Install(item.app)
		sgtk.RunOnMainThread(func() {
			view.refresh.Begin() // Reject any inventory snapshot predating completion.
			view.installing = false
			uh.developerGate.Reset()
			setActivitySpinner(item.spinner, false)
			item.status = state
			uh.renderDeveloperApp(item)
			uh.setDeveloperSensitive(true)
			switch {
			case err != nil:
				log.Printf("views: installing %s: %v", item.app.Name, err)
				item.row.SetSubtitle(developerAppSubtitle(item.app) + " · Install failed; try again.")
				uh.toastAdder.ShowErrorToast("Couldn't install " + item.app.Name + ": " + err.Error())
			case preview:
				uh.toastAdder.ShowToast("Preview: would install " + item.app.Name + " — no changes made.")
			default:
				uh.toastAdder.ShowToast(item.app.Name + " is installed.")
			}
			if err == nil && !preview && item.app.Source == developerapps.Homebrew {
				uh.homebrewInventoryChanged()
			} else {
				uh.refreshDeveloperApps()
			}
		})
	}()
}
