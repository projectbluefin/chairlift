package rowset

import (
	"reflect"
	"testing"
)

// fakeRow stands in for an expander row without naming any widget type, so the
// test builds and runs on a headless host (docs/agents/skills/gtk-headless-tests.md).
type fakeRow struct {
	title string
}

// fakeContainer records exactly what a real expander would hold: rows that were
// added and not since removed, in insertion order. It also keeps an audit log of
// every add and remove so the test can assert the removal callback fired once
// per tracked row.
type fakeContainer struct {
	rows    []fakeRow
	added   []string
	removed []string
}

func (c *fakeContainer) add(row fakeRow) {
	c.rows = append(c.rows, row)
	c.added = append(c.added, row.title)
}

func (c *fakeContainer) remove(row fakeRow) {
	c.removed = append(c.removed, row.title)
	for i, existing := range c.rows {
		if existing == row {
			c.rows = append(c.rows[:i], c.rows[i+1:]...)
			return
		}
	}
}

func (c *fakeContainer) titles() []string {
	titles := []string{}
	for _, row := range c.rows {
		titles = append(titles, row.title)
	}
	return titles
}

// TestTrackerSuccessiveLoads drives several simulated loads of a list through a
// Tracker the way a view reload does — Clear with the container's removal
// callback, then Add per new row — and asserts after *every* load that the
// container holds exactly that load's rows in order and that Len matches. This
// is the no-accumulation regression: without Clear, load 2's rows would sit
// below load 1's. It includes an empty load after a non-empty one, which must
// leave the container empty and Len at 0.
func TestTrackerSuccessiveLoads(t *testing.T) {
	loads := [][]string{
		{"alpha", "beta", "gamma"},
		{"delta"},
		{"epsilon", "zeta"},
		{},
		{"eta", "theta", "iota", "kappa"},
	}

	container := &fakeContainer{}
	var tracker Tracker[fakeRow]

	for i, load := range loads {
		removedBefore := len(container.removed)
		trackedBefore := tracker.Len()

		tracker.Clear(container.remove)
		if got := tracker.Len(); got != 0 {
			t.Fatalf("load %d: Len() after Clear = %d, want 0", i, got)
		}
		if got := len(container.removed) - removedBefore; got != trackedBefore {
			t.Fatalf("load %d: Clear invoked remove %d times, want %d", i, got, trackedBefore)
		}
		if len(container.rows) != 0 {
			t.Fatalf("load %d: container not empty after Clear: %v", i, container.titles())
		}

		for _, title := range load {
			row := fakeRow{title: title}
			container.add(row)
			tracker.Add(row)
		}

		if got, want := container.titles(), append([]string{}, load...); !reflect.DeepEqual(got, want) {
			t.Fatalf("load %d: container holds %v, want %v", i, got, want)
		}
		if got := tracker.Len(); got != len(load) {
			t.Fatalf("load %d: Len() = %d, want %d", i, got, len(load))
		}
	}

	if got, want := container.added, []string{
		"alpha", "beta", "gamma",
		"delta",
		"epsilon", "zeta",
		"eta", "theta", "iota", "kappa",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("added log = %v, want %v", got, want)
	}
	if got, want := container.removed, []string{
		"alpha", "beta", "gamma",
		"delta",
		"epsilon", "zeta",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("removed log = %v, want %v (insertion order, once per tracked row)", got, want)
	}
}

// TestTrackerClearOnZeroValue asserts the zero-value and already-cleared
// Tracker are both safe: Clear must not panic and must not invoke the removal
// callback at all when nothing is tracked.
func TestTrackerClearOnZeroValue(t *testing.T) {
	var tracker Tracker[string]

	calls := 0
	remove := func(string) { calls++ }

	tracker.Clear(remove)
	if calls != 0 {
		t.Fatalf("Clear on zero-value Tracker invoked remove %d times, want 0", calls)
	}
	if got := tracker.Len(); got != 0 {
		t.Fatalf("Len() on zero-value Tracker = %d, want 0", got)
	}

	tracker.Add("only")
	tracker.Clear(remove)
	if calls != 1 {
		t.Fatalf("remove call count after clearing one row = %d, want 1", calls)
	}

	tracker.Clear(remove)
	if calls != 1 {
		t.Fatalf("Clear on an already-cleared Tracker invoked remove again: %d calls, want 1", calls)
	}
	if got := tracker.Len(); got != 0 {
		t.Fatalf("Len() after second Clear = %d, want 0", got)
	}
}

func TestTrackerRemoveOneRowAndPreserveTheRest(t *testing.T) {
	container := &fakeContainer{}
	var tracker Tracker[fakeRow]
	for _, title := range []string{"alpha", "beta", "gamma"} {
		row := fakeRow{title: title}
		container.add(row)
		tracker.Add(row)
	}

	if !tracker.Remove(fakeRow{title: "beta"}, container.remove) {
		t.Fatal("Remove(beta) = false, want true")
	}
	if got, want := container.titles(), []string{"alpha", "gamma"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("container after Remove(beta) = %v, want %v", got, want)
	}
	if got := tracker.Len(); got != 2 {
		t.Fatalf("Len() after Remove(beta) = %d, want 2", got)
	}

	if tracker.Remove(fakeRow{title: "missing"}, container.remove) {
		t.Fatal("Remove(missing) = true, want false")
	}
	if got, want := container.removed, []string{"beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("removed log = %v, want %v", got, want)
	}

	tracker.Clear(container.remove)
	if got, want := container.removed, []string{"beta", "alpha", "gamma"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("removed log after Clear = %v, want %v", got, want)
	}
}

// TestTrackerTrimToKeepsTheMostRecentRows drives the rolling-log shape: rows
// are appended one at a time and trimmed to a cap after every append, the way
// a streamed-output view renders. The container must never hold more than the
// cap, and what it holds must always be the newest rows in order — an older
// row is evicted from the container, not merely forgotten by the Tracker.
func TestTrackerTrimToKeepsTheMostRecentRows(t *testing.T) {
	const limit = 3

	container := &fakeContainer{}
	var tracker Tracker[fakeRow]

	appended := []string{"a", "b", "c", "d", "e", "f", "g"}
	for i, title := range appended {
		row := fakeRow{title: title}
		container.add(row)
		tracker.Add(row)
		tracker.TrimTo(limit, container.remove)

		if got := tracker.Len(); got > limit {
			t.Fatalf("after %d appends Len() = %d, want at most %d", i+1, got, limit)
		}
		want := appended[:i+1]
		if len(want) > limit {
			want = want[len(want)-limit:]
		}
		if got := container.titles(); !reflect.DeepEqual(got, want) {
			t.Fatalf("after %d appends container = %v, want %v", i+1, got, want)
		}
	}

	if got, want := container.removed, []string{"a", "b", "c", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("removed log = %v, want the oldest rows in insertion order %v", got, want)
	}
}

func TestTrackerTrimToReportsEvictionsAndHandlesEdgeLimits(t *testing.T) {
	container := &fakeContainer{}
	var tracker Tracker[fakeRow]
	for _, title := range []string{"alpha", "beta", "gamma"} {
		row := fakeRow{title: title}
		container.add(row)
		tracker.Add(row)
	}

	if got := tracker.TrimTo(5, container.remove); got != 0 {
		t.Fatalf("TrimTo(5) evicted %d rows, want 0 when the limit is not reached", got)
	}
	if got := tracker.TrimTo(3, container.remove); got != 0 {
		t.Fatalf("TrimTo(3) evicted %d rows, want 0 when the count equals the limit", got)
	}
	if got, want := container.titles(), []string{"alpha", "beta", "gamma"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("container after no-op trims = %v, want %v", got, want)
	}

	if got := tracker.TrimTo(-1, container.remove); got != 3 {
		t.Fatalf("TrimTo(-1) evicted %d rows, want 3 (a negative limit is zero)", got)
	}
	if got := tracker.Len(); got != 0 {
		t.Fatalf("Len() after TrimTo(-1) = %d, want 0", got)
	}
	if got := container.titles(); len(got) != 0 {
		t.Fatalf("container after TrimTo(-1) = %v, want empty", got)
	}

	calls := 0
	if got := tracker.TrimTo(2, func(fakeRow) { calls++ }); got != 0 || calls != 0 {
		t.Fatalf("TrimTo on an empty Tracker evicted %d rows with %d callbacks, want 0 and 0", got, calls)
	}
}
