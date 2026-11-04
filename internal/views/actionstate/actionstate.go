// Package actionstate owns the pure state transitions used by asynchronous
// Applications- and Updates-page actions. It has no GTK or puregotk
// dependency, so command completion, refresh ordering, and repeated-click
// behavior can be tested headlessly.
package actionstate

import (
	"sync"
	"sync/atomic"
)

const (
	gateIdle uint32 = iota
	gateRunning
	gateComplete
)

// Gate prevents overlapping invocations of one action. Its zero value is
// ready for use.
type Gate struct {
	state atomic.Uint32
}

// RefreshGate gives refresh requests monotonically increasing generations so
// only the newest request may publish its result. Its zero value is ready.
type RefreshGate struct {
	generation atomic.Uint64
}

// Begin returns the generation assigned to a new refresh request.
func (g *RefreshGate) Begin() uint64 {
	return g.generation.Add(1)
}

// IsCurrent reports whether generation still belongs to the newest request.
func (g *RefreshGate) IsCurrent(generation uint64) bool {
	return generation != 0 && generation == g.generation.Load()
}

// Serializer runs one resource's asynchronous work strictly one attempt at a
// time, in the order the attempts were queued, and lets a superseded attempt
// drop out.
//
// A Gate is the wrong tool where every request carries a distinct value the
// user picked: refusing the second click would silently discard the newer
// choice. A Serializer instead queues each request behind the one in flight
// and, once it reaches the front, skips it when a newer request has already
// been claimed — so the last value the user chose is the one that is both
// persisted and applied, and no two attempts for the same resource overlap.
//
// The queue is first in, first out. A mutex alone does not promise that:
// when several attempts wait for the one in flight, any of them may run next,
// so an action queued after a pick could overtake it.
//
// Its zero value is ready for use.
type Serializer struct {
	mu sync.Mutex
	// tail is closed when the most recently queued attempt finishes; nil
	// while nothing has been queued.
	tail       chan struct{}
	generation atomic.Uint64
}

// Ticket is one attempt's place in a Serializer's queue. Pass it to Run
// exactly once: a ticket that is never run holds up every later attempt.
type Ticket struct {
	generation uint64
	exclusive  bool
	prev, done chan struct{}
}

// Generation is the claim a queued completion checks with IsCurrent. It is
// zero for a Reserve ticket, which no completion publishes against.
func (t Ticket) Generation() uint64 {
	return t.generation
}

// Claim records a new request, superseding every earlier claim, and queues
// it. Call it on the thread that read the user's choice, before starting the
// worker.
func (s *Serializer) Claim() Ticket {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enqueue(s.generation.Add(1), false)
}

// Reserve queues work that must be ordered with the claimed requests but
// neither supersedes them nor can be superseded: it runs once every attempt
// queued before it has finished, and every attempt queued after it waits.
func (s *Serializer) Reserve() Ticket {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enqueue(0, true)
}

func (s *Serializer) enqueue(generation uint64, exclusive bool) Ticket {
	t := Ticket{generation: generation, exclusive: exclusive, prev: s.tail, done: make(chan struct{})}
	s.tail = t.done
	return t
}

// Run waits for every attempt queued before t to finish, then runs fn unless
// t is a claim a newer one has superseded. It reports whether fn ran.
func (s *Serializer) Run(t Ticket, fn func()) bool {
	if t.done == nil {
		return false
	}
	defer close(t.done)
	if t.prev != nil {
		<-t.prev
	}
	if !t.exclusive && (t.generation == 0 || t.generation != s.generation.Load()) {
		return false
	}
	fn()
	return true
}

// IsCurrent protects a queued completion from a newer request.
func (s *Serializer) IsCurrent(generation uint64) bool {
	return generation != 0 && generation == s.generation.Load()
}

// TryStart moves an idle action to running. It reports false while the action
// is already running or after it has completed permanently.
func (g *Gate) TryStart() bool {
	return g.state.CompareAndSwap(gateIdle, gateRunning)
}

// Reset makes a running action available again after a failure, preview, or
// refresh completion.
func (g *Gate) Reset() {
	g.state.CompareAndSwap(gateRunning, gateIdle)
}

// Complete permanently closes a successfully completed row action.
func (g *Gate) Complete() {
	g.state.CompareAndSwap(gateRunning, gateComplete)
}

// Running reports whether an action currently holds the gate. A passive
// refresh uses it to stand aside while that action owns the state it would
// publish.
func (g *Gate) Running() bool {
	return g.state.Load() == gateRunning
}

// RowGates holds the gates of the rows one list rebuild created, so the next
// rebuild can tell whether replacing those rows would discard a running
// action's controls. A rebuild that would is deferred and recorded as owed,
// and runs once that action settles; until then the old rows keep showing the
// action's progress and keep refusing a second start. It is not safe for
// concurrent use: every method runs on the one thread that rebuilds the rows.
// The gates it hands out are.
type RowGates struct {
	gates   []*Gate
	pending bool
}

// Defer reports whether a rebuild must wait because a tracked row action is
// running, recording the rebuild as owed when it must.
func (r *RowGates) Defer() bool {
	if r.running() {
		r.pending = true
		return true
	}
	return false
}

// Rebuild forgets the replaced rows' gates and any owed rebuild. Call it
// immediately before replacing the rows, after Defer reported false.
func (r *RowGates) Rebuild() {
	clear(r.gates)
	r.gates = r.gates[:0]
	r.pending = false
}

// New returns an idle gate for a row the current rebuild adds and tracks it.
func (r *RowGates) New() *Gate {
	gate := &Gate{}
	r.gates = append(r.gates, gate)
	return gate
}

// Settled reports whether a deferred rebuild is owed and may now run because
// no tracked action is running. Reporting true clears the debt, so one
// settlement starts one rebuild.
func (r *RowGates) Settled() bool {
	if !r.pending || r.running() {
		return false
	}
	r.pending = false
	return true
}

func (r *RowGates) running() bool {
	for _, gate := range r.gates {
		if gate.Running() {
			return true
		}
	}
	return false
}

// Idle reports whether TryStart would succeed now: the action is neither
// running nor permanently completed. A control derives its sensitivity from
// it when another action may have changed what it can offer.
func (g *Gate) Idle() bool {
	return g.state.Load() == gateIdle
}

// Decision describes the UI work following one command attempt.
type Decision struct {
	Refresh         bool
	RestoreControl  bool
	CompleteControl bool
}

// PackageUninstall decides whether an installed-package uninstall control
// resets or completes and whether the installed inventory must refresh.
func PackageUninstall(succeeded, dryRun bool) Decision {
	return completedPackageMutation(succeeded, dryRun)
}

// PackagePin decides whether a formula pin/unpin control resets or completes
// and whether the installed inventory must refresh.
func PackagePin(succeeded, dryRun bool) Decision {
	return completedPackageMutation(succeeded, dryRun)
}

func completedPackageMutation(succeeded, dryRun bool) Decision {
	switch {
	case !succeeded:
		return Decision{RestoreControl: true}
	case dryRun:
		return Decision{RestoreControl: true}
	default:
		return Decision{Refresh: true, CompleteControl: true}
	}
}
