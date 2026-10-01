package liverystate

import (
	"testing"

	"github.com/projectbluefin/chairlift/internal/livery"
)

// Both rotate switches write one persisted pair of keys. A flip on one
// section must carry the other section's in-flight candidate into its
// snapshot, or it writes the older confirmed value back over a rotation that
// already landed.
func TestRotationCandidatesOverlayCarriesInFlightFlips(t *testing.T) {
	confirmed := livery.State{PanelRotate: false, DockRotate: false}

	var pending RotationCandidates
	pending.Set(livery.Panel, true)

	// The panel flip is in flight and the dock flip is claimed before it
	// publishes: the dock's snapshot must see the panel candidate.
	got := pending.Overlay(confirmed)
	if !got.PanelRotate {
		t.Fatalf("Overlay dropped the in-flight panel candidate: %#v", got)
	}
	if got.DockRotate {
		t.Fatalf("Overlay changed an untouched dock value: %#v", got)
	}

	// The dock candidate is added too; the next snapshot carries both.
	pending.Set(livery.Dock, true)
	got = pending.Overlay(confirmed)
	if !got.PanelRotate || !got.DockRotate {
		t.Fatalf("Overlay did not carry both candidates: %#v", got)
	}

	// The newest completion clears the pair, so a later snapshot reads the
	// confirmed state it published rather than a stale candidate.
	pending.Clear()
	got = pending.Overlay(confirmed)
	if got.PanelRotate || got.DockRotate {
		t.Fatalf("Clear left candidates behind: %#v", got)
	}
}

// An overlay candidate wins over the confirmed value for its own field only;
// the other field keeps whatever the confirmed state holds.
func TestRotationCandidatesOverlayIsPerField(t *testing.T) {
	confirmed := livery.State{PanelRotate: true, DockRotate: true}
	var pending RotationCandidates
	pending.Set(livery.Panel, false)

	got := pending.Overlay(confirmed)
	if got.PanelRotate {
		t.Fatalf("Overlay did not apply the panel candidate: %#v", got)
	}
	if !got.DockRotate {
		t.Fatalf("Overlay clobbered the confirmed dock value: %#v", got)
	}
}
