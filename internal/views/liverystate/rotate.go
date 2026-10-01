package liverystate

import "github.com/projectbluefin/chairlift/internal/livery"

// RotationCandidates overlays rotate-at-login attempts that have been claimed
// but not yet published onto the confirmed state.
//
// Both rotate switches write one persisted pair of keys and share one systemd
// user unit, so a flip on one section has to carry the other section's
// in-flight candidate into its snapshot. Taking the snapshot from confirmed
// state alone writes the older value back over a rotation the other section
// already landed — the flip is silently lost while its switch shows on. Being
// a pure overlay keeps that rule decidable in a headless test.
type RotationCandidates struct {
	panelSet, panelValue bool
	dockSet, dockValue   bool
}

// Set records the candidate for one section.
func (c *RotationCandidates) Set(surface livery.Surface, value bool) {
	if surface == livery.Panel {
		c.panelSet, c.panelValue = true, value
		return
	}
	c.dockSet, c.dockValue = true, value
}

// Clear drops every candidate. The newest attempt to publish owns the whole
// pair, so one Clear is enough; a superseded attempt must leave the
// candidates for the newest one to carry.
func (c *RotationCandidates) Clear() {
	*c = RotationCandidates{}
}

// Overlay returns the confirmed state with every in-flight candidate applied.
func (c RotationCandidates) Overlay(state livery.State) livery.State {
	if c.panelSet {
		state.PanelRotate = c.panelValue
	}
	if c.dockSet {
		state.DockRotate = c.dockValue
	}
	return state
}
