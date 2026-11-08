package window

import (
	"github.com/projectbluefin/chairlift/internal/ui"
	"github.com/projectbluefin/chairlift/internal/views/activity"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// activityView draws internal/views/activity's inventory: the sidebar
// header's indicator, shown with a spinner while anything runs, and the
// Activity dialog it opens, which lists every running task with its progress
// whichever page started it (issue #734).
//
// Rows are kept per task and updated in place; they connect no signals, so
// the callback-table invariant in AGENTS.md is not at stake. The dialog is
// bound once and re-presented, like the About and shortcuts dialogs.
type activityView struct {
	tracker *activity.Tracker
	button  *gtk.Button
	spinner *gtk.Spinner
	dialog  *adw.Dialog
	stack   *gtk.Stack
	group   *adw.PreferencesGroup
	rows    map[uint64]*activityRow
	// One pulse callback for every indeterminate bar, as installProgress
	// does in internal/views: an action that cannot measure its progress
	// still visibly runs.
	pulse glib.SourceFunc
	timer uint32
}

type activityRow struct {
	row         *adw.ActionRow
	bar         *gtk.ProgressBar
	determinate bool
}

// bindActivity binds the dialog and connects the tracker's change
// notification. It runs once, on GTK's main thread, during buildUI.
func (w *Window) bindActivity() {
	var view activityUI
	ui.Bind(&view)
	a := &activityView{
		tracker: w.activities,
		button:  w.ui.ActivityButton,
		spinner: w.ui.ActivitySpinner,
		dialog:  view.Dialog,
		stack:   view.Stack,
		group:   view.TaskGroup,
		rows:    make(map[uint64]*activityRow),
	}
	a.pulse = func(uintptr) bool {
		for _, r := range a.rows {
			if !r.determinate {
				r.bar.Pulse()
			}
		}
		return true
	}
	w.activity = a
	// The tracker notifies from whichever goroutine reported, at most once
	// until render reads it again.
	w.activities.SetNotify(func() { sgtk.RunOnMainThread(a.render) })
	a.render()
}

// present opens the Activity dialog over the window.
func (a *activityView) present(parent *gtk.Widget) {
	a.render()
	a.dialog.Present(parent)
}

// render applies the tracker's current inventory. It runs on GTK's main
// thread.
func (a *activityView) render() {
	entries := a.tracker.Snapshot()
	running := len(entries) > 0

	a.button.SetVisible(running)
	a.button.SetTooltipText(activity.Summary(entries))
	if running {
		a.spinner.Start()
		a.stack.SetVisibleChildName("tasks")
	} else {
		a.spinner.Stop()
		a.stack.SetVisibleChildName("empty")
	}

	seen := make(map[uint64]bool, len(entries))
	pulsing := false
	for _, entry := range entries {
		seen[entry.ID] = true
		r := a.rows[entry.ID]
		if r == nil {
			r = newActivityRow()
			a.rows[entry.ID] = r
			a.group.Add(&r.row.Widget)
		}
		r.row.SetTitle(entry.Title)
		r.row.SetSubtitle(activity.Subtitle(entry))
		r.determinate = entry.Determinate
		r.bar.SetShowText(entry.Determinate)
		if entry.Determinate {
			r.bar.SetFraction(entry.Fraction)
		} else {
			pulsing = true
		}
	}
	for id, r := range a.rows {
		if !seen[id] {
			a.group.Remove(&r.row.Widget)
			delete(a.rows, id)
		}
	}

	if pulsing && a.timer == 0 {
		a.timer = glib.TimeoutAdd(100, &a.pulse, 0)
	} else if !pulsing && a.timer != 0 {
		glib.SourceRemove(a.timer)
		a.timer = 0
	}
}

// newActivityRow builds one task's row: its title, what it is doing and how
// long it has left, and a progress bar showing the percentage when the task
// measures it. Titles and details are plain text, never Pango markup: a
// detail can quote a command's output.
func newActivityRow() *activityRow {
	row := adw.NewActionRow()
	row.SetUseMarkup(false)
	bar := gtk.NewProgressBar()
	bar.SetValign(gtk.AlignCenterValue)
	bar.SetSizeRequest(120, -1)
	row.AddSuffix(&bar.Widget)
	return &activityRow{row: row, bar: bar}
}
