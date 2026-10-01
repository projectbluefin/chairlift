package liverystate

import (
	"testing"

	"github.com/projectbluefin/chairlift/internal/livery"
)

// Every surface owns a distinct field and settings key. The AppGrid case is
// called out because the toggle handlers once fell through to the Dock branch,
// so a live app-grid flip left AppGridEnabled stale and overwrote DockEnabled.
func TestToggleStateMapsEverySurface(t *testing.T) {
	state := livery.State{
		AppGridEnabled: true,
		PanelEnabled:   true,
		DockEnabled:    true,
	}

	tests := []struct {
		surface livery.Surface
		wantKey string
	}{
		{livery.AppGrid, livery.KeyAppGridEnabled},
		{livery.Panel, livery.KeyPanelEnabled},
		{livery.Dock, livery.KeyDockEnabled},
	}
	for _, tt := range tests {
		t.Run(tt.wantKey, func(t *testing.T) {
			enabled, key := ToggleState(state, tt.surface)
			if !enabled {
				t.Fatalf("ToggleState(%v) enabled = false, want true", tt.surface)
			}
			if key != tt.wantKey {
				t.Fatalf("ToggleState(%v) key = %q, want %q", tt.surface, key, tt.wantKey)
			}

			var other livery.State
			SetToggleState(&other, tt.surface, true)
			if got, _ := ToggleState(other, tt.surface); !got {
				t.Fatalf("SetToggleState(%v, true) did not round-trip", tt.surface)
			}
			want := livery.State{}
			switch tt.surface {
			case livery.AppGrid:
				want.AppGridEnabled = true
			case livery.Panel:
				want.PanelEnabled = true
			default:
				want.DockEnabled = true
			}
			if other != want {
				t.Fatalf("SetToggleState(%v, true) = %#v, want %#v", tt.surface, other, want)
			}
		})
	}
}
