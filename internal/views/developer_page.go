package views

import (
	"log"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ui"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// createDeveloperPage binds the Developer Mode detail page: page.blp with its
// back button shown, because this is a detail reached from Features.
func (uh *UserHome) createDeveloperPage() (*adw.ToolbarView, *adw.PreferencesPage) {
	var view pageUI
	ui.Bind(&view)
	view.Header.SetShowBackButton(false)
	view.BackButton.SetVisible(true)
	view.BackButton.SetTooltipText("Back to Features")
	SetAccessibleLabel(view.BackButton, "Back to Features")
	backClickedCb := func(gtk.Button) {
		if uh.closeDeveloperDetail != nil {
			uh.closeDeveloperDetail()
		}
	}
	view.BackButton.ConnectClicked(&backClickedCb)
	return view.Page, view.Preferences
}

// buildDeveloperPage builds the Developer Mode detail page.
func (uh *UserHome) buildDeveloperPage() {
	if !uh.groupEnabled("features_page", "dx_group") {
		return
	}
	page := uh.developerPrefsPage
	if page == nil {
		return
	}
	page.SetTitle("Developer Mode")
	page.SetDescription("Tools for building and running software on this computer.")

	status := ublue.StatusCached()
	if !status.Available {
		return
	}

	log.Printf("views: developer page built variant=%s tag=%s channel=%s switchable=%v developer=%v",
		status.Variant, status.Tag, status.Channel,
		status.CanSwitchTo != imageinfo.ChannelUnknown, status.Developer)

	uh.developerSetupActivity = uh.buildSetupActivity(page)
	uh.buildDeveloperGroup(page, status)
	uh.buildDeveloperAppsGroup(page)
}
