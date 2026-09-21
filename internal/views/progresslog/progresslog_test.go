package progresslog

import (
	"slices"
	"sync"
	"testing"
	"time"
)

// texts reduces a batch to the line text the assertions care about; the
// arrival timestamps have their own test.
func texts(lines []Line) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, line.Text)
	}
	return out
}

func TestAppendSchedulesOneCallbackPerBatch(t *testing.T) {
	c := New(DefaultLimit)

	if !c.Append("first") {
		t.Fatal("the first Append must schedule a drain")
	}
	for _, line := range []string{"second", "third"} {
		if c.Append(line) {
			t.Fatalf("Append(%q) scheduled a second drain while one was outstanding", line)
		}
	}

	batch := c.Drain()
	if got := texts(batch.Lines); !slices.Equal(got, []string{"first", "second", "third"}) {
		t.Fatalf("drained %v, want the whole burst in one batch", got)
	}

	if !c.Append("fourth") {
		t.Fatal("Append after a Drain must schedule the next drain")
	}
}

func TestAppendRetainsOnlyTheMostRecentLines(t *testing.T) {
	c := New(3)

	for _, line := range []string{"a", "b", "c", "d", "e"} {
		c.Append(line)
	}

	batch := c.Drain()
	if got := texts(batch.Lines); !slices.Equal(got, []string{"c", "d", "e"}) {
		t.Fatalf("drained %v, want the last three lines", got)
	}
	if batch.Total != 5 {
		t.Fatalf("Total = %d, want every appended line counted (5)", batch.Total)
	}
}

func TestRetentionStaysBoundedAcrossManyBatches(t *testing.T) {
	c := New(4)

	for round := 0; round < 100; round++ {
		for i := 0; i < 50; i++ {
			c.Append("line")
		}
		if got := len(c.Drain().Lines); got > c.Limit() {
			t.Fatalf("round %d drained %d lines, want at most %d", round, got, c.Limit())
		}
	}

	if got := c.Drain().Total; got != 100*50 {
		t.Fatalf("Total = %d, want 5000", got)
	}
}

func TestDrainWithoutAppendReportsAnEmptyBatch(t *testing.T) {
	c := New(DefaultLimit)

	batch := c.Drain()
	if len(batch.Lines) != 0 || batch.Total != 0 {
		t.Fatalf("Drain on a fresh Coalescer = %+v, want an empty batch", batch)
	}

	c.Append("only")
	if got := texts(c.Drain().Lines); !slices.Equal(got, []string{"only"}) {
		t.Fatalf("second Drain = %v, want the appended line", got)
	}
}

func TestDrainedBatchDoesNotAliasLaterAppends(t *testing.T) {
	c := New(2)

	c.Append("first")
	batch := c.Drain()
	c.Append("second")
	c.Append("third")

	if got := texts(batch.Lines); !slices.Equal(got, []string{"first"}) {
		t.Fatalf("earlier batch = %v, want it unchanged by later appends", got)
	}
}

func TestAppendStampsArrivalTime(t *testing.T) {
	c := New(DefaultLimit)
	stamp := time.Date(2026, time.September, 20, 13, 45, 6, 0, time.UTC)
	c.now = func() time.Time { return stamp }

	c.Append("staging")

	lines := c.Drain().Lines
	if len(lines) != 1 || !lines[0].At.Equal(stamp) {
		t.Fatalf("drained %+v, want one line stamped %s", lines, stamp)
	}
}

func TestNewRaisesANonPositiveLimit(t *testing.T) {
	for _, limit := range []int{0, -7} {
		c := New(limit)
		if c.Limit() != 1 {
			t.Fatalf("New(%d).Limit() = %d, want 1", limit, c.Limit())
		}

		c.Append("first")
		c.Append("second")
		if got := texts(c.Drain().Lines); !slices.Equal(got, []string{"second"}) {
			t.Fatalf("New(%d) drained %v, want the most recent line", limit, got)
		}
	}
}

// TestConcurrentAppendAndDrainLoseNoLine models the real wiring: one worker
// goroutine appending stage output while the main thread drains whenever a
// batch was announced. Every line must arrive exactly once in some batch, and
// no batch may exceed the limit. Run under `make ci`'s race gate this also
// covers the Coalescer's locking.
func TestConcurrentAppendAndDrainLoseNoLine(t *testing.T) {
	const lines = 500
	c := New(lines) // large enough that nothing is evicted, so counting is exact

	drains := make(chan struct{}, lines)
	var seen int
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range drains {
			seen += len(c.Drain().Lines)
		}
	}()

	for i := 0; i < lines; i++ {
		if c.Append("line") {
			drains <- struct{}{}
		}
	}
	close(drains)
	wg.Wait()

	seen += len(c.Drain().Lines)
	if seen != lines {
		t.Fatalf("drained %d lines in total, want %d", seen, lines)
	}
}
