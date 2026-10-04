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
// time and lets a superseded attempt drop out.
//
// A Gate is the wrong tool where every request carries a distinct value the
// user picked: refusing the second click would silently discard the newer
// choice. A Serializer instead queues each request behind the one in flight
// and, once it reaches the front, skips it when a newer request has already
// been claimed — so the last value the user chose is the one that is both
// persisted and applied, and no two attempts for the same resource overlap.
//
// Its zero value is ready for use.
type Serializer struct {
	mu         sync.Mutex
	generation atomic.Uint64
}

// Claim records a new request and returns its generation. Call it on the
// thread that read the user's choice, before starting the worker.
func (s *Serializer) Claim() uint64 {
	return s.generation.Add(1)
}

// Run waits for any earlier work on this resource to finish, then runs fn
// unless a newer generation has been claimed in the meantime. It reports
// whether fn ran.
func (s *Serializer) Run(generation uint64, fn func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation == 0 || generation != s.generation.Load() {
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
