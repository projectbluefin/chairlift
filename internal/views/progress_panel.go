package views

import (
	"codeberg.org/puregotk/puregotk/v4/adw"

	"github.com/projectbluefin/chairlift/internal/views/pageview"
	"github.com/projectbluefin/chairlift/internal/views/progresslog"
	"github.com/projectbluefin/chairlift/internal/views/rowset"
)

// renderProgressBatch drains one coalesced batch of streamed output into
// expander as timestamped rows, trims the tracked rows back to the
// coalescer's retention window, and restates how much of the run is shown.
//
// Every streaming panel renders the same way, and the retention cap is only
// honoured if the add and the trim stay together, so both panels share this
// one body rather than repeating it. The returned batch lets a caller use the
// lines it just rendered; its Lines are empty when nothing was pending.
//
// It must be called on the GTK main thread: rowset.Tracker does no locking.
func renderProgressBatch(expander *adw.ExpanderRow, lines *progresslog.Coalescer, rows *rowset.Tracker[*adw.ActionRow]) progresslog.Batch {
	batch := lines.Drain()
	if len(batch.Lines) == 0 {
		return batch
	}

	for _, line := range batch.Lines {
		lineRow := adw.NewActionRow()
		lineRow.SetTitle(line.Text)
		lineRow.SetSubtitle(line.At.Format("15:04:05"))
		expander.AddRow(&lineRow.Widget)
		rows.Add(lineRow)
	}
	rows.TrimTo(lines.Limit(), func(row *adw.ActionRow) {
		expander.Remove(&row.Widget)
	})

	expander.SetSubtitle(pageview.StagingLogSubtitle(rows.Len(), batch.Total))
	return batch
}
