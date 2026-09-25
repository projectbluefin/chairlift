package e2e

import (
	"strings"
	"testing"
)

// TestParseATSPIReport pins the probe's output protocol from the reading
// side. The protocol is the whole interface between the Python probe and the
// Go assertions, and it is the part of the AT-SPI suite that can be exercised
// on any machine — including one with no display, no accessibility bus and no
// GTK runtime. A parser that silently dropped a malformed record would let a
// half-working probe read as a passing test, so the failure cases carry as
// much weight here as the success case.
func TestParseATSPIReport(t *testing.T) {
	t.Run("full report", func(t *testing.T) {
		output := strings.Join([]string{
			"ROW\tindex=0\tname=Updates\tselected=1",
			"ROW\tindex=1\tname=Apps\tselected=0",
			"PAGE\tpage=updates\tselected_index=0\tselected_name=Updates\tselected_count=1\tcontent_title_labels=1",
			"PAGE\tpage=applications\tselected_index=1\tselected_name=Apps\tselected_count=1\tcontent_title_labels=2",
			"DONE",
			"",
		}, "\n")

		report, err := parseATSPIReport(output)
		if err != nil {
			t.Fatalf("parseATSPIReport() error = %v", err)
		}
		if !report.Done {
			t.Error("report is not marked done")
		}

		wantRows := []atspiRow{
			{Index: 0, Name: "Updates", Selected: true},
			{Index: 1, Name: "Apps", Selected: false},
		}
		if len(report.Rows) != len(wantRows) {
			t.Fatalf("parsed %d rows, want %d", len(report.Rows), len(wantRows))
		}
		for index, want := range wantRows {
			if report.Rows[index] != want {
				t.Errorf("row %d = %+v, want %+v", index, report.Rows[index], want)
			}
		}

		wantPages := []atspiPage{
			{Page: "updates", SelectedIndex: 0, SelectedName: "Updates", SelectedCount: 1, ContentTitleLabels: 1},
			{Page: "applications", SelectedIndex: 1, SelectedName: "Apps", SelectedCount: 1, ContentTitleLabels: 2},
		}
		if len(report.Pages) != len(wantPages) {
			t.Fatalf("parsed %d pages, want %d", len(report.Pages), len(wantPages))
		}
		for index, want := range wantPages {
			if report.Pages[index] != want {
				t.Errorf("page %d = %+v, want %+v", index, report.Pages[index], want)
			}
		}
	})

	// A page title can legitimately be empty in the tree — that is the
	// blank-accessible-name failure the suite exists to catch — so the
	// parser must carry it through rather than reject the record.
	t.Run("blank accessible name is data, not a parse error", func(t *testing.T) {
		report, err := parseATSPIReport("ROW\tindex=0\tname=\tselected=0\nDONE\n")
		if err != nil {
			t.Fatalf("parseATSPIReport() error = %v", err)
		}
		if len(report.Rows) != 1 || report.Rows[0].Name != "" {
			t.Fatalf("rows = %+v, want one row with a blank name", report.Rows)
		}
	})

	t.Run("a report without DONE is not done", func(t *testing.T) {
		report, err := parseATSPIReport("ROW\tindex=0\tname=Updates\tselected=1\n")
		if err != nil {
			t.Fatalf("parseATSPIReport() error = %v", err)
		}
		if report.Done {
			t.Error("report without a DONE record is marked done")
		}
	})

	failures := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "unknown record",
			output: "WINDOW\tname=Control Center\n",
			want:   "unknown record",
		},
		{
			name:   "field without a value",
			output: "ROW\tindex\n",
			want:   "not key=value",
		},
		{
			name:   "duplicate field",
			output: "ROW\tindex=0\tindex=1\tname=Updates\tselected=0\n",
			want:   "appears twice",
		},
		{
			name:   "row missing selected",
			output: "ROW\tindex=0\tname=Updates\n",
			want:   `"selected" is missing`,
		},
		{
			name:   "row with a non-numeric index",
			output: "ROW\tindex=first\tname=Updates\tselected=0\n",
			want:   "is not a number",
		},
		{
			name:   "row with a selected value that is neither true nor false",
			output: "ROW\tindex=0\tname=Updates\tselected=2\n",
			want:   "want 0 or 1",
		},
		{
			name:   "page missing content_title_labels",
			output: "PAGE\tpage=updates\tselected_index=0\tselected_name=Updates\tselected_count=1\n",
			want:   `"content_title_labels" is missing`,
		},
		{
			name:   "records after DONE",
			output: "DONE\nROW\tindex=0\tname=Updates\tselected=1\n",
			want:   "follows DONE",
		},
	}

	for _, failure := range failures {
		t.Run(failure.name, func(t *testing.T) {
			_, err := parseATSPIReport(failure.output)
			if err == nil {
				t.Fatalf("parseATSPIReport(%q) succeeded, want an error", failure.output)
			}
			if !strings.Contains(err.Error(), failure.want) {
				t.Errorf("parseATSPIReport(%q) error = %v, want substring %q", failure.output, err, failure.want)
			}
		})
	}
}
