package views

import (
	"context"
	"log"
	"time"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/projectbluefin/chairlift/internal/contribute"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/launcher"
)

// buildContributeGroup adds the Contribute section to the Agents page.
func (uh *UserHome) buildContributeGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Contribute")

	row := adw.NewActionRow()
	row.SetTitle("Contribute to Bluefin")
	row.SetUseMarkup(false)
	row.SetSubtitle("Checking requirements…")

	uh.contributeSpinner = newActivitySpinner()
	row.AddSuffix(&uh.contributeSpinner.Widget)

	button := gtk.NewButtonWithLabel("Contribute")
	button.SetValign(gtk.AlignCenterValue)
	button.SetSensitive(false)

	clickedCb := func(_ gtk.Button) {
		uh.onContributeClicked()
	}
	button.ConnectClicked(&clickedCb)
	row.AddSuffix(&button.Widget)

	group.Add(&row.Widget)
	page.Add(group)

	uh.contributeRow = row
	uh.contributeButton = button

	uh.refreshContributePreflight()
}

func (uh *UserHome) refreshContributePreflight() {
	if uh.contributeRow == nil || uh.contributeButton == nil {
		return
	}
	setActivitySpinner(uh.contributeSpinner, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result := contribute.Preflight(ctx, contribute.RealProber())
		sgtk.RunOnMainThread(func() {
			setActivitySpinner(uh.contributeSpinner, false)
			if uh.contributeRow == nil || uh.contributeButton == nil {
				return
			}
			uh.contributeRow.SetSubtitle(result.Subtitle)
			uh.contributeButton.SetSensitive(result.Ready)
		})
	}()
}

func (uh *UserHome) onContributeClicked() {
	if !uh.contributeGate.TryStart() {
		return
	}

	if dryrun.Enabled() {
		uh.contributeGate.Reset()
		log.Printf("[DRY-RUN] would launch %s %s %s", contribute.DefaultRunner, contribute.DefaultUjust, contribute.DefaultRecipe)
		uh.toastAdder.ShowToast("[DRY-RUN] Preview: would launch Contribute to Bluefin in a terminal")
		return
	}

	if uh.contributeButton != nil {
		uh.contributeButton.SetSensitive(false)
	}

	cmd := contribute.Command("", "", "")
	// Run, not Start: Start reports only failures, so a session that ended
	// cleanly never re-enabled the button.
	err := launcher.Run(cmd, func(exitErr error) {
		sgtk.RunOnMainThread(func() {
			uh.contributeGate.Reset()
			if uh.contributeButton != nil {
				uh.contributeButton.SetSensitive(true)
			}
			if exitErr != nil {
				log.Printf("views: contribute session exited with error: %v", exitErr)
				uh.toastAdder.ShowErrorToast("Contribute session exited with an error.")
			}
		})
	})
	if err != nil {
		uh.contributeGate.Reset()
		if uh.contributeButton != nil {
			uh.contributeButton.SetSensitive(true)
		}
		log.Printf("views: launch contribute failed: %v", err)
		uh.toastAdder.ShowErrorToast("Could not launch Contribute to Bluefin.")
	}
}
