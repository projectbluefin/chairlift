package views

import (
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/views/updatepresent"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

type sourceRow struct {
	row         *adw.ActionRow
	detailRows  []*adw.ActionRow
	group       *adw.PreferencesGroup
	compactMode bool
	buttons     []*gtk.Button
	items       []updateflow.Item
	enabled     bool
	// restartButton is the "Restart now" suffix on the Operating system row,
	// created once and shown only when the OS source reports a staged
	// deployment. Hidden in every other state so the row's resting shape
	// never carries a control that cannot fire.
	restartButton *gtk.Button
	// restartShown remembers whether the button is currently on screen so
	// render() can hide or show it without re-parenting the suffix.
	restartShown bool
}

func newSourceRow(state updateflow.SourceState, group *adw.PreferencesGroup, shell *UpdateShell) *sourceRow {
	r := &sourceRow{
		row: adw.NewActionRow(), group: group, compactMode: shell.compactMode,
		items: state.Items, enabled: state.Configured && state.Available && state.Enabled,
	}
	title, subtitle := updatepresent.Source(state)
	r.row.SetTitle(title)
	r.row.SetSubtitle(subtitle)
	r.row.SetIconName(updatepresent.SourceIcon(state.ID))
	r.row.SetSubtitleLines(r.subtitleLines())
	group.Add(&r.row.Widget)
	if state.ID == updateflow.DeveloperTools && r.enabled {
		button := gtk.NewButtonWithLabel("Check")
		button.SetValign(gtk.AlignCenterValue)
		button.SetTooltipText("Check for new tool versions")
		button.SetSensitive(updatepresent.CanStartOperation(shell.Busy(), shell.closed.Load()) && !updatepresent.ShowProgress(shell.snapshot.Phase) && !shell.restartInFlight.Load())
		shell.updateButtons.connect(button, func(gtk.Button) { shell.startToolRefresh() })
		r.row.AddSuffix(&button.Widget)
		r.buttons = append(r.buttons, button)
	}
	if state.ID == updateflow.OperatingSystem {
		// The Operating system row owns its restart control. The button is
		// created once at build time — its callback identity is a single
		// shared trampoline from updateButtons, so reloads do not leak
		// slots — and render() decides whether it is currently on screen.
		button := gtk.NewButtonWithLabel("Restart now")
		button.SetValign(gtk.AlignCenterValue)
		button.SetTooltipText("Restart to finish installing the staged update")
		button.SetVisible(state.RestartRequired)
		button.SetSensitive(updatepresent.CanStartOperation(shell.Busy(), shell.closed.Load()) && !updatepresent.ShowProgress(shell.snapshot.Phase) && !shell.restartInFlight.Load())
		shell.updateButtons.connect(button, func(gtk.Button) { shell.StartRestart() })
		r.row.AddSuffix(&button.Widget)
		r.restartButton = button
		r.restartShown = state.RestartRequired
		r.buttons = append(r.buttons, button)
	}
	for _, item := range updatepresent.ItemRows(state) {
		detail := adw.NewActionRow()
		detail.SetTitle(updatepresent.ItemTitle(item))
		detail.SetSubtitle(updatepresent.ItemSubtitle(item))
		detail.SetSubtitleLines(r.subtitleLines())
		if (state.ID == updateflow.Applications || state.ID == updateflow.DeveloperTools) && r.enabled {
			button := gtk.NewButtonWithLabel("Update")
			button.SetValign(gtk.AlignCenterValue)
			button.SetSensitive(state.Configured && state.Available && state.Enabled &&
				updatepresent.CanStartOperation(shell.Busy(), shell.closed.Load()) && !updatepresent.ShowProgress(shell.snapshot.Phase))
			pending := item
			shell.updateButtons.connect(button, func(gtk.Button) { shell.startItemUpdate(state.ID, pending, detail) })
			detail.AddSuffix(&button.Widget)
			r.buttons = append(r.buttons, button)
		}
		group.Add(&detail.Widget)
		r.detailRows = append(r.detailRows, detail)
	}
	return r
}

func (r *sourceRow) render(state updateflow.SourceState, sensitive bool, restartInFlight bool) {
	title, subtitle := updatepresent.Source(state)
	r.row.SetTitle(title)
	r.row.SetSubtitle(subtitle)
	for index, row := range r.detailRows {
		row.SetSubtitle(updatepresent.ItemSubtitle(r.items[index]))
	}
	if r.restartButton != nil {
		// The button is parented once at build time; show or hide it
		// instead of re-parenting, so the row's suffix set is stable and
		// the puregotk trampoline keeps the same address.
		if state.RestartRequired != r.restartShown {
			r.restartButton.SetVisible(state.RestartRequired)
			r.restartShown = state.RestartRequired
		}
	}
	r.setSensitive(sensitive)
	if r.restartButton != nil && restartInFlight {
		r.restartButton.SetSensitive(false)
	}
}

func (r *sourceRow) setSensitive(sensitive bool) {
	for _, button := range r.buttons {
		button.SetSensitive(sensitive)
	}
}

// setRestartButtonSensitive toggles only the "Restart now" suffix. The
// shell calls it from StartRestart — the row owns the action now (#439) —
// while setSensitive continues to gate every suffix at once during a
// mutation, so a running update run cannot fire restart from either place.
func (r *sourceRow) setRestartButtonSensitive(sensitive bool) {
	if r.restartButton != nil {
		r.restartButton.SetSensitive(sensitive)
	}
}

func (r *sourceRow) remove() {
	r.group.Remove(&r.row.Widget)
	for _, detail := range r.detailRows {
		r.group.Remove(&detail.Widget)
	}
}

func (r *sourceRow) setCompact(compact bool) {
	r.compactMode = compact
	r.row.SetSubtitleLines(r.subtitleLines())
	for _, detail := range r.detailRows {
		detail.SetSubtitleLines(r.subtitleLines())
	}
}

func (r *sourceRow) subtitleLines() int32 {
	return updatepresent.SourceSubtitleLines(r.compactMode)
}

func cloneSourceStates(sources []updateflow.SourceState) []updateflow.SourceState {
	if sources == nil {
		return nil
	}
	cloned := make([]updateflow.SourceState, len(sources))
	for index, source := range sources {
		cloned[index] = source
		if source.Items != nil {
			cloned[index].Items = append([]updateflow.Item(nil), source.Items...)
		}
	}
	return cloned
}

func cloneSnapshot(snapshot updateflow.Snapshot) updateflow.Snapshot {
	snapshot.Sources = cloneSourceStates(snapshot.Sources)
	if snapshot.CompletedSources != nil {
		snapshot.CompletedSources = append([]updateflow.SourceID(nil), snapshot.CompletedSources...)
	}
	if snapshot.FailedSources != nil {
		snapshot.FailedSources = append([]updateflow.SourceID(nil), snapshot.FailedSources...)
	}
	return snapshot
}
