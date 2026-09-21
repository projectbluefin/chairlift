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
		"case sysupdate.EventMessage:",
		`logExpander.SetSubtitle("View output")`,
	}
	for _, fragment := range retired {
		if strings.Contains(text, fragment) {
			t.Errorf("updates_page.go still renders staging output unbounded: %q", fragment)
		}
	}

	// One sink serves both providers, because bootc.ProgressEvent and
	// sysupdate.ProgressEvent are the same stageexec.ProgressEvent. Two
	// constructions and one definition is the whole expected inventory; a
	// third would mean a handler grew its own copy of the loop.
	if got := strings.Count(text, "newStageProgressSink("); got != 3 {
		t.Errorf("updates_page.go mentions newStageProgressSink %d times, want 3 (one definition, one call per staging provider)", got)
	}
}
