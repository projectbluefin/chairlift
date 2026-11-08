package activity

import (
	"slices"
	"sync"
	"testing"
	"time"
)

// clock is a manual time source, so estimates are tested without sleeping.
type clock struct{ at time.Time }

func (c *clock) now() time.Time             { return c.at }
func (c *clock) advance(step time.Duration) { c.at = c.at.Add(step) }

func newTestTracker() (*Tracker, *clock) {
	c := &clock{at: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	t := New()
	t.now = c.now
	return t, c
}

func titles(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Title)
	}
	return out
}

func TestTasksAreListedInStartOrderUntilFinished(t *testing.T) {
	tracker, _ := newTestTracker()
	check := tracker.Start("Checking for updates")
	install := tracker.Start("Installing app collections")
	gaming := tracker.Start("Turning on Game Mode")

	if got := titles(tracker.Snapshot()); !slices.Equal(got, []string{"Checking for updates", "Installing app collections", "Turning on Game Mode"}) {
		t.Fatalf("Snapshot lists %v, want start order", got)
	}

	install.Finish()
	if got := titles(tracker.Snapshot()); !slices.Equal(got, []string{"Checking for updates", "Turning on Game Mode"}) {
		t.Fatalf("after finishing the middle task Snapshot lists %v", got)
	}

	check.Finish()
	gaming.Finish()
	if got := tracker.Snapshot(); len(got) != 0 {
		t.Fatalf("after every task finished Snapshot lists %v, want nothing", titles(got))
	}
}

func TestFinishIsIdempotentAndLeavesOtherTasks(t *testing.T) {
	tracker, _ := newTestTracker()
	first := tracker.Start("first")
	tracker.Start("second")

	first.Finish()
	first.Finish()

	if got := titles(tracker.Snapshot()); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("a repeated Finish changed the inventory to %v", got)
	}
	// Reporting on a finished task must not resurrect it.
	first.Progress("late", 0.5)
	first.Working("late")
	if got := titles(tracker.Snapshot()); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("a report after Finish changed the inventory to %v", got)
	}
}

func TestNilTrackerAndTaskRecordNothing(t *testing.T) {
	var tracker *Tracker
	task := tracker.Start("ignored")
	if task != nil {
		t.Fatalf("a nil tracker returned task %v, want nil", task)
	}
	task.Working("ignored")
	task.Progress("ignored", 0.5)
	task.Finish()
	tracker.SetNotify(func() { t.Fatal("a nil tracker notified") })
	if got := tracker.Snapshot(); got != nil {
		t.Fatalf("a nil tracker's Snapshot = %v, want nil", got)
	}
}

func TestChangesNotifyOnceUntilTheNextSnapshot(t *testing.T) {
	tracker, _ := newTestTracker()
	calls := 0
	tracker.SetNotify(func() { calls++ })

	task := tracker.Start("Installing updates")
	task.Working("Applications: installing updates…")
	task.Progress("Applications: installing updates…", 0.25)
	if calls != 1 {
		t.Fatalf("a burst of changes notified %d times, want once until the window reads", calls)
	}

	entries := tracker.Snapshot()
	if len(entries) != 1 || entries[0].Fraction != 0.25 || !entries[0].Determinate {
		t.Fatalf("Snapshot = %+v, want the burst's last report", entries)
	}

	task.Progress("Applications: installing updates…", 0.5)
	if calls != 2 {
		t.Fatalf("a change after Snapshot notified %d times in total, want 2", calls)
	}
}

func TestAnUnchangedReportDoesNotNotify(t *testing.T) {
	tracker, _ := newTestTracker()
	calls := 0
	tracker.SetNotify(func() { calls++ })
	task := tracker.Start("Installing updates")
	task.Progress("step", 0.5)
	tracker.Snapshot()
	calls = 0

	task.Progress("step", 0.5)
	if calls != 0 {
		t.Fatalf("repeating the same report notified %d times, want 0", calls)
	}
	task.Working("step")
	if calls != 1 {
		t.Fatalf("losing the measure notified %d times, want 1", calls)
	}
}

func TestNotifyRunsOutsideTheLock(t *testing.T) {
	tracker, _ := newTestTracker()
	// The window's notification schedules a redraw that calls Snapshot; a
	// notification made under the lock would deadlock here.
	tracker.SetNotify(func() { tracker.Snapshot() })
	task := tracker.Start("Installing updates")
	task.Progress("step", 0.5)
	task.Finish()
}

func TestProgressIsClampedAndWorkingDropsTheMeasure(t *testing.T) {
	tracker, _ := newTestTracker()
	task := tracker.Start("Installing updates")

	task.Progress("over", 1.7)
	if got := tracker.Snapshot()[0]; got.Fraction != 1 || !got.Determinate {
		t.Fatalf("Progress(1.7) recorded %+v, want a full determinate bar", got)
	}
	task.Progress("under", -0.3)
	if got := tracker.Snapshot()[0]; got.Fraction != 0 {
		t.Fatalf("Progress(-0.3) recorded fraction %v, want 0", got.Fraction)
	}
	task.Working("quiet step")
	if got := tracker.Snapshot()[0]; got.Determinate || got.Detail != "quiet step" {
		t.Fatalf("Working recorded %+v, want an indeterminate task with its detail", got)
	}
}

func TestEstimateWaitsForEnoughTimeAndProgress(t *testing.T) {
	tracker, clock := newTestTracker()
	task := tracker.Start("Installing updates")

	task.Progress("", 0.1)
	clock.advance(2 * time.Second)
	task.Progress("", 0.4)
	if got := tracker.Snapshot()[0]; got.Estimated {
		t.Fatalf("estimated %v after 2s, want no estimate before %v", got.Remaining, minEstimateElapsed)
	}

	clock.advance(8 * time.Second)
	task.Progress("", 0.12)
	// Progress went backwards: the measurement restarted at 0.12 just now.
	if got := tracker.Snapshot()[0]; got.Estimated {
		t.Fatalf("estimated %v right after progress went backwards", got.Remaining)
	}

	clock.advance(10 * time.Second)
	task.Progress("", 0.14)
	if got := tracker.Snapshot()[0]; got.Estimated {
		t.Fatalf("estimated %v from 2%% of the work, want no estimate below %v", got.Remaining, minEstimateProgress)
	}
}

func TestEstimateExtrapolatesTheMeasuredRate(t *testing.T) {
	tracker, clock := newTestTracker()
	task := tracker.Start("Installing updates")

	// An indeterminate preamble does not count against the rate.
	task.Working("Waiting for authentication")
	clock.advance(5 * time.Minute)
	task.Progress("", 0.2)
	clock.advance(30 * time.Second)
	task.Progress("", 0.4)

	got := tracker.Snapshot()[0]
	// 20% in 30s leaves 60% for 90s.
	if !got.Estimated || got.Remaining != 90*time.Second {
		t.Fatalf("estimate = %v (estimated %v), want 1m30s", got.Remaining, got.Estimated)
	}

	task.Working("A step without a measure")
	if got := tracker.Snapshot()[0]; got.Estimated {
		t.Fatalf("an indeterminate task kept estimate %v", got.Remaining)
	}
	task.Progress("", 1)
	if got := tracker.Snapshot()[0]; got.Estimated {
		t.Fatalf("a complete task kept estimate %v", got.Remaining)
	}
}

func TestConcurrentReportsAreSafe(t *testing.T) {
	tracker := New()
	tracker.SetNotify(func() {})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			task := tracker.Start("worker")
			for i := range 100 {
				task.Progress("step", float64(i)/100)
				tracker.Snapshot()
			}
			task.Finish()
		})
	}
	wg.Wait()
	if got := tracker.Snapshot(); len(got) != 0 {
		t.Fatalf("%d tasks left running after every worker finished", len(got))
	}
}

func TestRemainingTextRoundsUpToWholeMinutes(t *testing.T) {
	for _, c := range []struct {
		remaining time.Duration
		want      string
	}{
		{0, "Less than a minute left"},
		{59 * time.Second, "Less than a minute left"},
		{time.Minute, "About 1 minute left"},
		{61 * time.Second, "About 2 minutes left"},
		{59*time.Minute + 30*time.Second, "About 1 hour left"},
		{time.Hour, "About 1 hour left"},
		{100 * time.Minute, "About 2 hours left"},
	} {
		if got := RemainingText(c.remaining); got != c.want {
			t.Errorf("RemainingText(%v) = %q, want %q", c.remaining, got, c.want)
		}
	}
}

func TestSubtitleJoinsDetailAndEstimate(t *testing.T) {
	for _, c := range []struct {
		entry Entry
		want  string
	}{
		{Entry{}, ""},
		{Entry{Detail: "Applications: installing updates…"}, "Applications: installing updates…"},
		{Entry{Estimated: true, Remaining: 3 * time.Minute}, "About 3 minutes left"},
		{Entry{Detail: "Applications: installing updates…", Estimated: true, Remaining: 3 * time.Minute},
			"Applications: installing updates…\nAbout 3 minutes left"},
	} {
		if got := Subtitle(c.entry); got != c.want {
			t.Errorf("Subtitle(%+v) = %q, want %q", c.entry, got, c.want)
		}
	}
}

func TestSummaryNamesOneTaskOrCountsSeveral(t *testing.T) {
	for _, c := range []struct {
		entries []Entry
		want    string
	}{
		{nil, ""},
		{[]Entry{{Title: "Checking for updates"}}, "Checking for updates"},
		{[]Entry{{Title: "Installing updates", Determinate: true, Fraction: 0.429}}, "Installing updates — 42%"},
		{[]Entry{{Title: "a"}, {Title: "b"}}, "2 tasks in progress"},
	} {
		if got := Summary(c.entries); got != c.want {
			t.Errorf("Summary(%+v) = %q, want %q", c.entries, got, c.want)
		}
	}
}
