package pageview

import (
	"strings"
	"testing"
)

// One row replaces bluefinctl's strategy picker, schedule rows, and per-layer
// switches, so the subtitle must say what the single switch governs.
func TestAutomaticUpdatesRowDistinguishesOnFromOff(t *testing.T) {
	on := AutomaticUpdatesRow(true)
	off := AutomaticUpdatesRow(false)

	for _, row := range []Row{on, off} {
		if row.Title != "Automatic updates" {
			t.Errorf("AutomaticUpdatesRow().Title = %q, want %q", row.Title, "Automatic updates")
		}
	}
	if on.Subtitle == off.Subtitle {
		t.Fatal("AutomaticUpdatesRow does not distinguish on from off")
	}
	if !strings.Contains(on.Subtitle, "background") {
		t.Errorf("AutomaticUpdatesRow(true).Subtitle = %q, want it to say updates happen in the background", on.Subtitle)
	}
	if !strings.Contains(off.Subtitle, "when you ask") {
		t.Errorf("AutomaticUpdatesRow(false).Subtitle = %q, want it to say updates are manual", off.Subtitle)
	}
}

// Turning the switch on schedules future updates; it does not update
// anything now, and must not imply that it did.
func TestAutomaticUpdatesResultDoesNotImplyAnImmediateUpdate(t *testing.T) {
	on := AutomaticUpdatesResultSubtitle(true)
	off := AutomaticUpdatesResultSubtitle(false)

	if on == off {
		t.Fatal("AutomaticUpdatesResultSubtitle does not distinguish the two directions")
	}
	for _, subtitle := range []string{on, off} {
		for _, forbidden := range []string{"Updating", "Downloading", "installed now"} {
			if strings.Contains(subtitle, forbidden) {
				t.Errorf("AutomaticUpdatesResultSubtitle = %q, want no claim of an immediate update (%q)", subtitle, forbidden)
			}
		}
	}
	if !strings.Contains(off, "Update all") {
		t.Errorf("AutomaticUpdatesResultSubtitle(false) = %q, want it to point at the manual action", off)
	}
}
