package views

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/hardware"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

type hardwareEnablementView struct {
	group   *adw.PreferencesGroup
	row     *adw.ActionRow
	setup   actionstate.Gate
	refresh actionstate.RefreshGate
}

// buildHardwareGroup only reads status. Ordinary activation must never install
// packages or change the desktop icon; explicit initial setup owns those writes.
func (uh *UserHome) buildHardwareGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Hardware Enablement")
	group.SetDescription("Customizations and tools for your computer.")
	group.SetVisible(false)
	row := adw.NewActionRow()
	icon := gtk.NewImageFromIconName("computer-symbolic")
	row.AddPrefix(&icon.Widget)
	group.Add(&row.Widget)
	page.Add(group)
	view := &hardwareEnablementView{group: group, row: row}
	uh.hardwareEnablement = view
	generation := view.refresh.Begin()
	go func() {
		status, err := hardware.CheckStatus("")
		sgtk.RunOnMainThread(func() {
			if view.refresh.IsCurrent(generation) {
				view.render(status, err, false)
			}
		})
	}()
}

func (view *hardwareEnablementView) render(status hardware.Status, err error, preview bool) {
	if status.Vendor == hardware.VendorNone || status.Profile.DisplayName == "" {
		return
	}
	view.row.SetTitle(status.Profile.DisplayName)
	switch {
	case err != nil:
		log.Printf("hardware: %v", err)
		view.row.SetSubtitle("Hardware support could not be verified. Run initial setup to try again.")
	case status.Installed:
		view.row.SetSubtitle("Active · " + strings.Join(status.Profile.Packages, ", "))
	case preview:
		view.row.SetSubtitle("Preview · Would install " + strings.Join(status.Missing, ", "))
	default:
		view.row.SetSubtitle("Not installed · Run initial setup to install " + strings.Join(status.Missing, ", "))
	}
	view.group.SetVisible(true)
}

// EnableHardware starts the OEM step of explicit initial setup. Reopening the
// wizard cannot launch a concurrent install. Only verified installation permits
// applying the OEM panel default; existing Livery choices remain untouched.
func (uh *UserHome) EnableHardware() {
	view := uh.hardwareEnablement
	if view == nil || !view.setup.TryStart() {
		return
	}
	generation := view.refresh.Begin()
	view.row.SetSubtitle("Checking and installing hardware support…")
	go func() {
		status, err := hardware.EnableHardware(context.Background(), "")
		var iconErr error
		if err == nil && status.Installed && !dryrun.Enabled() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			iconErr = livery.EnsurePanelDefault(ctx, string(status.Vendor))
			cancel()
		}
		sgtk.RunOnMainThread(func() {
			view.setup.Reset()
			if view.refresh.IsCurrent(generation) {
				view.render(status, err, dryrun.Enabled())
			}
			if err != nil {
				uh.toastAdder.ShowErrorToast("Couldn't enable hardware support: " + err.Error())
			}
			if iconErr != nil {
				log.Printf("hardware: configuring OEM panel icon: %v", iconErr)
				uh.toastAdder.ShowErrorToast("Hardware support is installed, but the OEM panel icon couldn't be configured.")
			}
		})
	}()
}
