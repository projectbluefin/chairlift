package liverystate

import "github.com/projectbluefin/chairlift/internal/livery"

// ToggleState reports the confirmed on/off value and the settings key for one
// icon surface.
//
// Every surface has its own field on livery.State, and an AppGrid flip that
// fell through to the Dock branch would silently leave the app grid stale
// while overwriting the dock. Keeping the mapping here, beside the transition
// rule it feeds, makes both halves decidable in a headless test.
func ToggleState(state livery.State, surface livery.Surface) (bool, string) {
	switch surface {
	case livery.AppGrid:
		return state.AppGridEnabled, livery.KeyAppGridEnabled
	case livery.Panel:
		return state.PanelEnabled, livery.KeyPanelEnabled
	default:
		return state.DockEnabled, livery.KeyDockEnabled
	}
}

// SetToggleState writes the confirmed on/off value for one icon surface. It is
// the write half of ToggleState and must cover the same three surfaces.
func SetToggleState(state *livery.State, surface livery.Surface, value bool) {
	switch surface {
	case livery.AppGrid:
		state.AppGridEnabled = value
	case livery.Panel:
		state.PanelEnabled = value
	default:
		state.DockEnabled = value
	}
}
