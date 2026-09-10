package views

import (
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/troubleshoot"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// gooseDesktopID is the desktop file the goose-linux cask installs, which
// gtk-launch resolves by name.
const gooseDesktopID = "Goose"

// buildTroubleshootGroup builds the Enhanced Troubleshooting row: one action
// row that sets the feature up, then launches it.
//
// It is an action row rather than a switch because there is no clean "off".
// Turning it off would mean either leaving Goose configured to call a
// binary ChairLift had removed — an error on every session — or rewriting a
// YAML file that another tool owns and whose own setup script refuses to
// touch when it already exists.
func (uh *UserHome) buildTroubleshootGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Enhanced Troubleshooting")
	group.SetDescription(pageview.TroubleshootSetupNote())

	row := adw.NewActionRow()
	row.SetTitle("Enhanced Troubleshooting")
	row.SetSubtitle("Checking...")

	button := gtk.NewButtonWithLabel("Set Up")
	button.SetValign(gtk.AlignCenterValue)
	button.AddCssClass("suggested-action")
	button.SetSensitive(false)
	clickedCb := func(_ gtk.Button) {
		uh.onTroubleshootClicked()
	}
	button.ConnectClicked(&clickedCb)

	row.AddSuffix(&button.Widget)
	group.Add(&row.Widget)
	page.Add(group)

	uh.troubleshootGroup = group
	uh.troubleshootRow = row
	uh.troubleshootButton = button

	go uh.refreshTroubleshootState()
}

// refreshTroubleshootState reads the host's state off the main thread and
// applies it to the row. Homebrew is what every piece is installed with, so
// without it the group has nothing to offer and hides itself — the same
// treatment the other Homebrew-backed groups get.
func (uh *UserHome) refreshTroubleshootState() {
	if !homebrew.IsInstalledCached() {
		sgtk.RunOnMainThread(func() {
			if uh.troubleshootGroup != nil {
				uh.troubleshootGroup.SetVisible(false)
			}
		})
		return
	}

	state := troubleshoot.Detect()

	log.Printf("views: troubleshoot group built server=%v agent=%v desktop=%v wired=%v provider=%q",
		state.ServerInstalled, state.AgentInstalled, state.DesktopInstalled, state.Wired, state.Provider)

	sgtk.RunOnMainThread(func() {
		uh.applyTroubleshootState(state)
	})
}

// applyTroubleshootState puts the row and its button into the shape the
// host's state calls for.
func (uh *UserHome) applyTroubleshootState(state troubleshoot.State) {
	if uh.troubleshootRow == nil || uh.troubleshootButton == nil {
		return
	}

	uh.troubleshootState = state
	uh.troubleshootRow.SetSubtitle(pageview.TroubleshootRow(state).Subtitle)
	uh.troubleshootButton.SetSensitive(true)

	// The desktop app is what the button launches, so a host that is
	// otherwise ready but lacks it still needs the setup run.
	if state.Ready() && state.DesktopInstalled {
		uh.troubleshootButton.SetLabel("Start Session")
		return
	}
	uh.troubleshootButton.SetLabel("Set Up")
}

// onTroubleshootClicked either launches the session or runs the setup,
// depending on which the row is currently offering.
func (uh *UserHome) onTroubleshootClicked() {
	if uh.troubleshootState.Ready() && uh.troubleshootState.DesktopInstalled {
		uh.launchApp(gooseDesktopID)
		return
	}

	if !uh.troubleshootGate.TryStart() {
		return
	}

	button := uh.troubleshootButton
	row := uh.troubleshootRow
	state := uh.troubleshootState

	button.SetSensitive(false)
	button.SetLabel("Setting up...")

	go func() {
		defer uh.troubleshootGate.Reset()

		after, err := troubleshoot.Setup(state, func(step string) {
			sgtk.RunOnMainThread(func() { row.SetSubtitle(step + "...") })
		})

		sgtk.RunOnMainThread(func() {
			uh.applyTroubleshootState(after)

			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Setup failed: %v", err))
				return
			}

			row.SetSubtitle(pageview.TroubleshootSetupSubtitle(after))
			if dryrun.Enabled() {
				uh.toastAdder.ShowToast("[DRY-RUN] Preview: Enhanced Troubleshooting would be set up — no changes made")
				return
			}
			if !after.Ready() {
				// Every step reported success and the feature still is not
				// usable, so this is not an error toast — but it must not
				// read as done either.
				uh.toastAdder.ShowToast("Packages installed, but Goose already had a configuration — see the row for what to add")
				return
			}
			uh.toastAdder.ShowToast("Enhanced Troubleshooting is ready — " +
				pageview.TroubleshootProviderNote(after.Provider))
		})
	}()
}
