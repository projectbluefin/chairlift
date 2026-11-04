package views

import (
	"context"
	"errors"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/devsetup"
	"github.com/projectbluefin/chairlift/internal/pkexec"
)

// setupActivity owns a bounded checklist, not a second source of feature state.
// Feature switches continue to render only confirmed provider observations.
type setupActivity struct {
	group       *adw.PreferencesGroup
	rows        map[devsetup.Stage]*adw.ActionRow
	bar         *gtk.ProgressBar
	progress    installProgress
	retry       *gtk.Button
	retryAction func()
	busy        bool
	completed   map[devsetup.Stage]bool
}

func (uh *UserHome) buildSetupActivity(page *adw.PreferencesPage) {
	uh.setupActivity = newSetupActivity(page, devsetup.Stages[:])
}

func newSetupActivity(page *adw.PreferencesPage, stages []devsetup.Stage) *setupActivity {
	a := &setupActivity{group: adw.NewPreferencesGroup(), rows: make(map[devsetup.Stage]*adw.ActionRow)}
	a.group.SetTitle("Setup activity")
	a.group.SetVisible(false)
	header := adw.NewActionRow()
	header.SetTitle("Setup progress")
	a.bar = newInstallProgress()
	a.bar.SetShowText(false)
	header.AddSuffix(&a.bar.Widget)
	a.retry = gtk.NewButtonWithLabel("Retry")
	a.retry.SetValign(gtk.AlignCenterValue)
	a.retry.SetVisible(false)
	clicked := func(gtk.Button) {
		if !a.busy && a.retryAction != nil {
			a.retryAction()
		}
	}
	a.retry.ConnectClicked(&clicked)
	header.AddSuffix(&a.retry.Widget)
	a.group.Add(&header.Widget)
	for _, stage := range stages {
		row := adw.NewActionRow()
		row.SetTitle(string(stage))
		row.SetVisible(false)
		a.rows[stage] = row
		a.group.Add(&row.Widget)
	}
	destroyed := func(gtk.Widget) { a.progress.dispose() }
	page.ConnectDestroy(&destroyed)
	page.Add(a.group)
	return a
}

func (a *setupActivity) begin(title string, stages []devsetup.Stage, retry bool, action func()) bool {
	if a == nil || a.busy {
		return false
	}
	a.busy = true
	if !retry {
		a.completed = nil
	}
	a.retryAction = action
	a.retry.SetVisible(false)
	a.group.SetVisible(true)
	a.group.SetDescription(title)
	for _, row := range a.rows {
		row.SetVisible(false)
	}
	for _, stage := range stages {
		row := a.rows[stage]
		row.SetVisible(true)
		status := devsetup.Waiting
		if a.completed[stage] && stage != devsetup.Tools && stage != devsetup.Verify {
			status = devsetup.Complete
		}
		row.SetSubtitle(string(status))
	}
	a.progress.start(a.bar)
	return true
}

func (a *setupActivity) report(stage devsetup.Stage, status devsetup.Status) {
	sgtk.RunOnMainThread(func() {
		if !a.progress.disposed {
			a.rows[stage].SetSubtitle(string(status))
		}
	})
}

func (a *setupActivity) detail(message string) {
	sgtk.RunOnMainThread(func() {
		if !a.progress.disposed {
			a.group.SetDescription(message)
		}
	})
}

// finish runs on the GTK thread after every provider and opt-in has settled.
func (a *setupActivity) finish(result devsetup.Result, preview bool) {
	a.busy = false
	a.completed = result.Completed
	a.progress.stop(a.bar)
	if a.progress.disposed {
		return
	}
	switch {
	case result.Err != nil:
		message := "Setup needs attention. Completed steps are kept."
		if pkexec.IsAuthDismissed(result.Err) || errors.Is(result.Err, context.Canceled) {
			message = "Setup canceled. Confirmed changes are kept."
		}
		a.group.SetDescription(message)
		a.retry.SetVisible(true)
	case preview:
		a.group.SetDescription("Preview — no changes made")
	case result.AwaitingLogin:
		a.group.SetDescription("Awaiting new login — log out and back in to finish setup.")
	default:
		a.group.SetDescription("Setup complete")
	}
}
