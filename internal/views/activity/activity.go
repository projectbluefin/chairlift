// Package activity is the window's one inventory of long-running work: the
// update check and run, the actions that replace or roll back the operating
// system, Free Up Space, and app collection installs. Each action registers a
// Task when it starts, reports what it is doing while it runs, and finishes
// the Task when it ends, whatever page the user has moved to in between. The
// window renders the inventory as one indicator in a window-wide header bar
// and an Activity dialog that lists every running task with its progress
// (issue #734).
//
// The inventory only describes work; it never admits, orders, or cancels it.
// Admission stays with each action's own gate and with the update shell's
// mutation guard, so a Task that is never started changes no behaviour.
//
// Like its sibling leaf packages, this one imports no puregotk, so its
// coalescing, ordering, and time estimates are unit-tested on a headless
// host. See docs/skills/gtk-headless-testing/SKILL.md.
package activity

import (
	"math"
	"sync"
	"time"

	"github.com/leonelquinteros/gotext"
)

const (
	// minEstimateElapsed and minEstimateProgress are how much determinate
	// progress a task must have shown before it is given a time estimate. A
	// rate measured over a couple of seconds or a sliver of the work swings
	// wildly, and an estimate that jumps from one minute to twenty and back
	// is worse than none.
	minEstimateElapsed  = 5 * time.Second
	minEstimateProgress = 0.05
)

// Entry is one running task as the window draws it.
type Entry struct {
	// ID is stable for the task's lifetime, so a view can keep one row per
	// task and update it in place.
	ID uint64
	// Title names the action in the user's words ("Installing updates").
	Title string
	// Detail is what the action is doing right now; it may be empty.
	Detail string
	// Fraction is the completed share, 0 to 1, when Determinate is true.
	Fraction float64
	// Determinate reports whether the action measures its progress. An
	// action that cannot say how far along it is pulses instead.
	Determinate bool
	// Remaining is the estimated time left when Estimated is true.
	Remaining time.Duration
	// Estimated reports whether there is a trustworthy time estimate; see
	// minEstimateElapsed.
	Estimated bool
}

type task struct {
	id          uint64
	title       string
	detail      string
	fraction    float64
	determinate bool
	// The first determinate observation: the rate behind an estimate is
	// measured from here, not from the start, because an indeterminate
	// preamble (authentication, a metadata refresh) says nothing about how
	// fast the measured work goes.
	baseFraction float64
	baseTime     time.Time
	haveBase     bool
}

// Tracker holds the running tasks. It is safe for concurrent use: actions
// report from their worker goroutines and the window reads on GTK's main
// thread. A nil *Tracker is valid and records nothing.
type Tracker struct {
	mu      sync.Mutex
	next    uint64
	tasks   []*task // in start order
	notify  func()
	pending bool
	now     func() time.Time
}

// New returns an empty tracker.
func New() *Tracker {
	return &Tracker{now: time.Now}
}

// SetNotify registers the function told that the inventory changed. It is
// called on the goroutine that made the change, outside the tracker's lock,
// and at most once until the next Snapshot: a run that reports a hundred
// progress lines before the main thread gets round to drawing costs one
// callback, not a hundred. The window passes a function that schedules a
// redraw on GTK's main thread, and the redraw calls Snapshot.
func (t *Tracker) SetNotify(fn func()) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.notify = fn
	t.mu.Unlock()
}

// Task is one running action's handle. A nil *Task is valid and records
// nothing, so an action needs no tracker to run.
type Task struct {
	tracker *Tracker
	id      uint64
}

// Start registers a running action and returns its handle. The caller must
// Finish it on every path, including failure and cancellation.
func (t *Tracker) Start(title string) *Task {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	t.next++
	id := t.next
	t.tasks = append(t.tasks, &task{id: id, title: title})
	notify := t.changedLocked()
	t.mu.Unlock()
	if notify != nil {
		notify()
	}
	return &Task{tracker: t, id: id}
}

// Working reports what the action is doing without a measure of how far
// along it is.
func (k *Task) Working(detail string) {
	k.update(func(s *task, _ time.Time) bool {
		changed := s.detail != detail || s.determinate
		s.detail = detail
		s.determinate = false
		return changed
	})
}

// Progress reports what the action is doing and the share of it that is
// complete. fraction is clamped to [0, 1].
func (k *Task) Progress(detail string, fraction float64) {
	if math.IsNaN(fraction) {
		fraction = 0
	}
	fraction = min(max(fraction, 0), 1)
	k.update(func(s *task, now time.Time) bool {
		changed := s.detail != detail || !s.determinate || s.fraction != fraction
		// Progress that goes backwards belongs to a new measurement (a
		// retried step, say); a rate from the old one would mislead.
		if !s.haveBase || fraction < s.fraction {
			s.baseFraction, s.baseTime, s.haveBase = fraction, now, true
		}
		s.detail = detail
		s.fraction = fraction
		s.determinate = true
		return changed
	})
}

// Finish removes the task. It is safe to call more than once and from any
// goroutine.
func (k *Task) Finish() {
	if k == nil || k.tracker == nil {
		return
	}
	t := k.tracker
	t.mu.Lock()
	removed := false
	for i, s := range t.tasks {
		if s.id == k.id {
			t.tasks = append(t.tasks[:i], t.tasks[i+1:]...)
			removed = true
			break
		}
	}
	var notify func()
	if removed {
		notify = t.changedLocked()
	}
	t.mu.Unlock()
	if notify != nil {
		notify()
	}
}

func (k *Task) update(apply func(*task, time.Time) bool) {
	if k == nil || k.tracker == nil {
		return
	}
	t := k.tracker
	t.mu.Lock()
	var notify func()
	for _, s := range t.tasks {
		if s.id == k.id {
			if apply(s, t.now()) {
				notify = t.changedLocked()
			}
			break
		}
	}
	t.mu.Unlock()
	if notify != nil {
		notify()
	}
}

// changedLocked marks the inventory changed and returns the notification to
// send once the lock is released, or nil when one is already outstanding.
func (t *Tracker) changedLocked() func() {
	if t.pending || t.notify == nil {
		return nil
	}
	t.pending = true
	return t.notify
}

// Snapshot returns the running tasks in the order they started and re-arms
// the change notification.
func (t *Tracker) Snapshot() []Entry {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = false
	now := t.now()
	entries := make([]Entry, 0, len(t.tasks))
	for _, s := range t.tasks {
		entry := Entry{
			ID:          s.id,
			Title:       s.title,
			Detail:      s.detail,
			Fraction:    s.fraction,
			Determinate: s.determinate,
		}
		entry.Remaining, entry.Estimated = s.estimate(now)
		entries = append(entries, entry)
	}
	return entries
}

// estimate extrapolates the rate measured since the first determinate
// report. It offers nothing while the task is indeterminate, before enough
// time or work has passed to measure a rate, and once the work is complete.
func (s *task) estimate(now time.Time) (time.Duration, bool) {
	if !s.determinate || !s.haveBase || s.fraction >= 1 {
		return 0, false
	}
	elapsed := now.Sub(s.baseTime)
	done := s.fraction - s.baseFraction
	if elapsed < minEstimateElapsed || done < minEstimateProgress {
		return 0, false
	}
	remaining := time.Duration(float64(elapsed) * (1 - s.fraction) / done)
	return remaining, true
}

// RemainingText says how long a task has left in plain words. Minutes round
// up, so a task never claims to be nearer done than its estimate.
func RemainingText(remaining time.Duration) string {
	switch {
	case remaining < time.Minute:
		return gotext.Get("Less than a minute left")
	case remaining < time.Hour:
		minutes := int(math.Ceil(remaining.Minutes()))
		if minutes >= 60 {
			return gotext.Get("About 1 hour left")
		}
		return gotext.GetN("About %d minute left", "About %d minutes left", minutes, minutes)
	default:
		hours := int(math.Round(remaining.Hours()))
		return gotext.GetN("About %d hour left", "About %d hours left", hours, hours)
	}
}

// Subtitle is the line under a task's title: what it is doing, then how long
// it has left when that is known. Either part may be absent.
func Subtitle(entry Entry) string {
	subtitle := entry.Detail
	if entry.Estimated {
		if subtitle != "" {
			subtitle += "\n"
		}
		subtitle += RemainingText(entry.Remaining)
	}
	return subtitle
}

// Summary is the indicator's tooltip: the one task's title and progress, or
// how many tasks are running. It is empty when nothing runs.
func Summary(entries []Entry) string {
	switch len(entries) {
	case 0:
		return ""
	case 1:
		entry := entries[0]
		if entry.Determinate {
			return gotext.Get("%s — %d%%", entry.Title, int(math.Floor(entry.Fraction*100)))
		}
		return entry.Title
	default:
		return gotext.GetN("%d task in progress", "%d tasks in progress", len(entries), len(entries))
	}
}
