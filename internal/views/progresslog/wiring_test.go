package progresslog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestStagingHandlersRenderThroughTheBoundedSink is the CI-enforced half of
// the fix for issue #81. internal/views cannot host a test binary (puregotk
// panics resolving GTK and graphene at package init —
// docs/skills/gtk-headless-testing/SKILL.md), so the behavior that a reviewer
// would otherwise have to re-check by eye is asserted here against the source
// of the two staging handlers: every progress line must reach the UI through
// the coalescing, row-capping sink, and neither handler may go back to
// building a permanent action row inside a per-line main-thread callback.
func TestStagingHandlersRenderThroughTheBoundedSink(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	path := filepath.Join(filepath.Clean(filepath.Join(filepath.Dir(filename), "..")), "updates_page.go")

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(source)

	required := []string{
		// The sink is constructed from the shared retention cap, not from a
		// number spelled inline in the view.
		"progresslog.New(progresslog.DefaultLimit)",
		// Both staging handlers consume their channel through it.
		"newStageProgressSink(activityRow, logExpander).consume(progressCh)",
		// The expander is trimmed back to the window after every batch, so
		// its row count is bounded rather than merely its pending batch.
		"s.rows.TrimTo(s.lines.Limit(),",
		// And the user is told when the window hid older lines.
		"pageview.StagingLogSubtitle(",
	}
	for _, fragment := range required {
		if !strings.Contains(text, fragment) {
			t.Errorf("updates_page.go does not use %q", fragment)
		}
	}

	retired := []string{
		// The unbounded shape: a per-event loop variable captured into one
		// main-thread callback per event, each callback adding a row that is
		// never removed.
		"evt := event",
		"case bootc.EventMessage:",
		`logExpander.SetSubtitle("View output")`,
	}
	for _, fragment := range retired {
		if strings.Contains(text, fragment) {
			t.Errorf("updates_page.go still renders staging output unbounded: %q", fragment)
		}
	}

	// One definition and one construction for the bootc staging handler is
	// the whole expected inventory; another would mean a handler grew its own
	// copy of the loop.
	if got := strings.Count(text, "newStageProgressSink("); got != 2 {
		t.Errorf("updates_page.go mentions newStageProgressSink %d times, want 2 (one definition, one bootc staging call)", got)
	}
}

// TestStagingTitlesAreNotPangoMarkup is the CI-enforced half of the fix for
// issue #435. internal/views cannot host a test binary (puregotk panics
// resolving GTK and graphene at package init —
// docs/skills/gtk-headless-testing/SKILL.md), so the behavior a reviewer would
// otherwise re-check by eye is asserted here against the flush handler's
// source: every streamed line becomes a row title, and that title is command
// output, so the row must render it literally rather than let AdwActionRow
// parse it as Pango markup.
func TestStagingTitlesAreNotPangoMarkup(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	path := filepath.Join(filepath.Clean(filepath.Join(filepath.Dir(filename), "..")), "updates_page.go")

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(source)

	// The streamed line reaches the row title, and the row disables Pango
	// parsing before that title is set. Both must sit in the flush handler
	// that renders the batch, not merely appear somewhere in the file.
	if !strings.Contains(text, "msgRow.SetTitle(line.Text)") {
		t.Error("updates_page.go does not render a streamed line as a row title: the markup assertions below no longer cover the streamed text (issue #435)")
	}
	if !strings.Contains(text, "msgRow.SetUseMarkup(false)") {
		t.Error("updates_page.go renders streamed command output as a row title without SetUseMarkup(false): it is parsed as Pango markup (issue #435)")
	}

	// The same batch's last line also reaches the activity row's subtitle, and
	// AdwActionRow parses subtitles as Pango markup too, so that row must
	// disable markup where it is constructed.
	if !strings.Contains(text, "s.activityRow.SetSubtitle(batch.Lines[len(batch.Lines)-1].Text)") {
		t.Error("updates_page.go does not render the last streamed line as the activity row subtitle: the markup assertion below no longer covers the streamed text (issue #435)")
	}
	if !strings.Contains(text, "activityRow.SetUseMarkup(false)") {
		t.Error("updates_page.go renders streamed command output as the activity row subtitle without SetUseMarkup(false): it is parsed as Pango markup (issue #435)")
	}
}

// TestErrorSubtitlesAreNotPangoMarkup is the CI-enforced half of the fix for
// issue #437. internal/views cannot host a test binary (puregotk panics
// resolving GTK and graphene at package init —
// docs/skills/gtk-headless-testing/SKILL.md), so the behavior a reviewer
// would otherwise have to re-check by eye is asserted here against the
// source: two error strings reach row and expander subtitles, and AdwActionRow
// and AdwExpanderRow parse subtitles as Pango markup by default, so the row
// and the expander must render that error text literally. The twin test for
// streamed stage/llmman output reaching row titles lives in this same file
// (TestStagingTitlesAreNotPangoMarkup).
func TestErrorSubtitlesAreNotPangoMarkup(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	path := filepath.Join(filepath.Clean(filepath.Join(filepath.Dir(filename), "..")), "updates_page.go")

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(source)

	// Raw errors stay in the log: a failure subtitle is plain text a person
	// can act on, never a command's error string (which could also carry
	// '<' or '&' into Pango markup, issue #437).
	for _, retired := range []string{
		"row.SetSubtitle(err.Error())",
		`fmt.Sprintf("Could not verify staged update: %v", statusErr)`,
	} {
		if strings.Contains(text, retired) {
			t.Errorf("updates_page.go shows a raw error to the user again: %q", retired)
		}
	}
	// The stage expander's subtitle still carries the version string
	// bootc reports, so it must render literally.
	if !strings.Contains(text, "uh.bootcStageExpander.SetUseMarkup(false)") {
		t.Error("updates_page.go renders the stage expander subtitle without SetUseMarkup(false): it is parsed as Pango markup (issue #437)")
	}
}
