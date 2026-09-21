// Package progresslog bounds what a view renders from a streamed staging run.
//
// A stage helper prints an unbounded number of lines, and the naive rendering
// — one sgtk.RunOnMainThread callback and one permanent action row per line —
// costs the main thread a callback per line and leaks a heavyweight widget per
// line for the lifetime of the run. A verbose helper therefore accumulates
// thousands of widgets and callbacks, which is the freeze reported in issue
// #81. A Coalescer fixes both halves: lines accumulate on the worker goroutine
// and are handed to the main thread in one batch per outstanding callback, and
// the batch retains only the most recent Limit lines, so memory is flat no
// matter how much the helper prints.
//
// Like its sibling leaf packages, this one imports no puregotk — it names no
// widget type at all — so its logic is unit-tested on a headless host. A test
// binary for a package that imports puregotk panics while resolving GTK and
// graphene shared libraries at package init, before any test function runs.
// See docs/skills/gtk-headless-testing/SKILL.md.
package progresslog

import (
	"sync"
	"time"
)

// DefaultLimit is how many staging output lines a run retains. It is a window
// onto a diagnostic log rather than the log itself: the stage helpers print
// tens of lines in the ordinary case, and a run verbose enough to exceed this
// is one whose earliest lines have already scrolled out of any usable reading
// position. The full output remains in the journal.
const DefaultLimit = 200

// Line is one retained output line and the moment it arrived. The arrival
// time is stamped on Append rather than on render, so a batch that renders
// after a busy main thread still reports when the helper actually printed.
type Line struct {
	Text string
	At   time.Time
}

// Batch is one coalesced hand-off to the main thread.
type Batch struct {
	// Lines are the retained lines accumulated since the previous Drain,
	// oldest first, and never more than Limit of them.
	Lines []Line
	// Total counts every line appended since the Coalescer was created,
	// including lines discarded to stay within Limit, so a view can say how
	// much of the run it is actually showing.
	Total int
}

// Coalescer accumulates lines produced by a worker goroutine and releases them
// to the main thread in batches. It is safe for concurrent use: Append runs on
// the worker goroutine and Drain on the GTK main thread.
type Coalescer struct {
	mu       sync.Mutex
	limit    int
	pending  []Line
	total    int
	flushing bool
	now      func() time.Time
}

// New returns a Coalescer retaining at most limit lines per batch. A limit
// below one is raised to one, because a progress view that retains no line at
// all cannot even show the most recent one.
func New(limit int) *Coalescer {
	if limit < 1 {
		limit = 1
	}
	return &Coalescer{limit: limit, now: time.Now}
}

// Limit reports the retention cap, which is also the maximum number of rows a
// caller ever has to hold for this run.
func (c *Coalescer) Limit() int {
	return c.limit
}

// Append records line and reports whether the caller must schedule a Drain on
// the main thread. It returns true only for the line that opens a batch: every
// further line arriving before that Drain runs joins the same batch, so a
// burst of output costs one main-thread callback rather than one per line.
// Once the batch holds Limit lines, each new line evicts the oldest pending
// one, which is what keeps memory flat under a verbose helper.
func (c *Coalescer) Append(line string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.total++
	entry := Line{Text: line, At: c.now()}
	if len(c.pending) < c.limit {
		c.pending = append(c.pending, entry)
	} else {
		copy(c.pending, c.pending[1:])
		c.pending[len(c.pending)-1] = entry
	}

	if c.flushing {
		return false
	}
	c.flushing = true
	return true
}

// Drain returns the pending batch and clears the outstanding-callback marker,
// so the next Append opens a new batch and schedules a fresh callback. Calling
// it without a preceding Append reports an empty batch and is not an error:
// the completion event drains unconditionally so no trailing line is lost.
func (c *Coalescer) Drain() Batch {
	c.mu.Lock()
	defer c.mu.Unlock()

	batch := Batch{Lines: c.pending, Total: c.total}
	c.pending = nil
	c.flushing = false
	return batch
}
