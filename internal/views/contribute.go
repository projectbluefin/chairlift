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
	row.SetSubtitle("Checking…")

	uh.contributeSpinner = newActivitySpinner()
	row.AddSuffix(&uh.contributeSpinner.Widget)

	// The registration guide is a real control, built once and shown only
	// when preflight names a page that resolves the unmet requirement; a URL
	// spelled out in the subtitle is not clickable.
	guide := gtk.NewButtonWithLabel(contribute.RegistrationGuideLabel)
	guide.SetValign(gtk.AlignCenterValue)
	guide.SetTooltipText(contribute.RegistrationURL)
	guide.SetVisible(false)
	guideClicked := func(_ gtk.Button) {
		uh.openURL(contribute.RegistrationURL)
	}
	guide.ConnectClicked(&guideClicked)
	row.AddSuffix(&guide.Widget)

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
	uh.contributeGuide = guide

	// Preflight runs whenever the group is shown, not once at build: the
	// row names a requirement — Hive registration, Podman, the recipe — that
	// the user fixes outside ChairLift, and a one-time read kept the button
	// insensitive until restart. The handler is connected once, here.
	uh.contributeMapped = func(gtk.Widget) { uh.refreshContributePreflight() }
	group.ConnectMap(&uh.contributeMapped)
	uh.refreshContributePreflight()
}

// refreshContributePreflight re-reads the requirements off the main thread.
// It is passive: it stands aside while a session holds the gate, and a
// session start begins a new generation, so a read that started earlier can
// never re-enable the button under a running session.
func (uh *UserHome) refreshContributePreflight() {
	if uh.contributeRow == nil || uh.contributeButton == nil || uh.contributeGate.Running() {
		return
	}
	generation := uh.contributeRefresh.Begin()
	setActivitySpinner(uh.contributeSpinner, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result := contribute.Preflight(ctx, contribute.RealProber())
		sgtk.RunOnMainThread(func() {
			if !uh.contributeRefresh.IsCurrent(generation) {
				return
			}
			setActivitySpinner(uh.contributeSpinner, false)
			if uh.contributeRow == nil || uh.contributeButton == nil || uh.contributeGate.Running() {
				return
			}
			uh.contributeRow.SetSubtitle(result.Subtitle)
			uh.contributeButton.SetSensitive(result.Ready)
			if uh.contributeGuide != nil {
				uh.contributeGuide.SetVisible(result.HelpURL != "")
			}
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

	// A preflight read still in flight must not re-enable the button under
	// the session this click starts.
	uh.contributeRefresh.Begin()
	setActivitySpinner(uh.contributeSpinner, false)
	if uh.contributeButton != nil {
		uh.contributeButton.SetSensitive(false)
	}

	cmd := contribute.Command("", "", "")
	// Run, not Start: Start reports only failures, so a session that ended
	// cleanly never re-enabled the button. The button comes back through a
	// fresh preflight rather than a blind re-enable.
	err := launcher.Run(cmd, func(exitErr error) {
		sgtk.RunOnMainThread(func() {
			uh.contributeGate.Reset()
			uh.refreshContributePreflight()
			if exitErr != nil {
				log.Printf("views: contribute session exited with error: %v", exitErr)
				uh.toastAdder.ShowErrorToast("Contribute closed unexpectedly.")
			}
		})
	})
	if err != nil {
		uh.contributeGate.Reset()
		uh.refreshContributePreflight()
		log.Printf("views: launch contribute failed: %v", err)
		uh.toastAdder.ShowErrorToast("Couldn't open Contribute. Try again.")
	}
}
