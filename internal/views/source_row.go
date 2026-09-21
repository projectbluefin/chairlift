package views

import (
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/views/updatepresent"

	"codeberg.org/puregotk/puregotk/v4/adw"
)

type sourceRow struct {
	row         *adw.ExpanderRow
	detailRows  []*adw.ActionRow
	compactMode bool
}

func newSourceRow(state updateflow.SourceState) *sourceRow {
	row := &sourceRow{row: adw.NewExpanderRow()}
	row.render(state)
	return row
}

func (r *sourceRow) render(state updateflow.SourceState) {
	title, subtitle := updatepresent.Source(state)
	r.row.SetTitle(title)
	r.row.SetSubtitle(subtitle)
	r.row.SetIconName(updatepresent.SourceIcon(state.ID))
	r.row.SetEnableExpansion(updatepresent.SourceHasDetails(state))
	r.row.SetSubtitleLines(r.subtitleLines())

	for _, detail := range r.detailRows {
		r.row.Remove(&detail.Widget)
	}
	r.detailRows = r.detailRows[:0]

	for _, item := range state.Items {
		detail := adw.NewActionRow()
		detail.SetTitle(item.Name)
		detail.SetSubtitle(updatepresent.ItemSubtitle(item))
		detail.SetTitleLines(1)
		detail.SetSubtitleLines(r.subtitleLines())
		r.row.AddRow(&detail.Widget)
		r.detailRows = append(r.detailRows, detail)
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
