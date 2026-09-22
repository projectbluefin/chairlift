package pageview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/livery"
)

// TestPageDescriptionContainsEveryFragment holds the three group subtitles to
// the page-level sentence they are drawn from, so editing one without the
// other cannot pass.
func TestPageDescriptionContainsEveryFragment(t *testing.T) {
	fragments := LiveryFragments()
	if len(fragments) != 3 {
		t.Fatalf("LiveryFragments() returned %d entries, want 3", len(fragments))
	}
	lowered := strings.ToLower(LiveryPageDescription)
	for _, fragment := range fragments {
		if !strings.Contains(lowered, strings.ToLower(fragment)) {
			t.Errorf("page description %q does not contain fragment %q", LiveryPageDescription, fragment)
		}
	}
}

// TestFragmentsAppearInPresentationOrder asserts the sentence reads in the
// same order the page lays the sections out.
func TestFragmentsAppearInPresentationOrder(t *testing.T) {
	lowered := strings.ToLower(LiveryPageDescription)
	previous := -1
	for _, fragment := range LiveryFragments() {
		at := strings.Index(lowered, strings.ToLower(fragment))
		if at <= previous {
			t.Fatalf("fragment %q appears out of order in %q", fragment, LiveryPageDescription)
		}
		previous = at
	}
}

// TestChoicesRoundTripForEveryFoundation covers the whole catalog plus the
// custom sentinel, so an entry whose index did not survive the round trip
// could not pass.
func TestChoicesRoundTripForEveryFoundation(t *testing.T) {
	for _, f := range livery.Foundations() {
		choice := LiveryChoices(f.ID)
		if got := LiveryIDForIndex(choice.Selected); got != f.ID {
			t.Errorf("LiveryChoices(%q) selected %d, which maps back to %q", f.ID, choice.Selected, got)
		}
	}
	custom := LiveryChoices(livery.CustomID)
	if got := LiveryIDForIndex(custom.Selected); got != livery.CustomID {
		t.Errorf("custom selection mapped back to %q", got)
	}
}

// TestUnknownSelectionFallsBackToTheDefault asserts a stale dconf value lands
// the combo somewhere valid rather than leaving it unselected.
func TestUnknownSelectionFallsBackToTheDefault(t *testing.T) {
	choice := LiveryChoices("retired-foundation")
	if got := LiveryIDForIndex(choice.Selected); got != livery.DefaultID {
		t.Errorf("unknown selection resolved to %q, want %q", got, livery.DefaultID)
	}
}

// TestDockRowNamesItsFullReach is a wording regression test with teeth: the
// Files mark is shared by every surface GNOME draws that application on, and
// a subtitle mentioning only the dock would understate what the switch does.
func TestDockRowNamesItsFullReach(t *testing.T) {
	subtitle := strings.ToLower(LiveryDockRow().Subtitle)
	for _, surface := range []string{"dock", "app grid", "window switcher"} {
		if !strings.Contains(subtitle, surface) {
			t.Errorf("dock subtitle %q does not mention %q", subtitle, surface)
		}
	}
}

// TestPanelRowExplainsAMissingExtension asserts the unavailable case says why
// rather than presenting a control that silently does nothing.
func TestPanelRowExplainsAMissingExtension(t *testing.T) {
	if got := LiveryPanelRow(false).Subtitle; !strings.Contains(got, "not installed") {
		t.Errorf("unavailable subtitle = %q, want it to say the extension is absent", got)
	}
}

// TestRotationRowWarnsForASourceBuild asserts the switch says when the unit
// it writes is tied to a disposable path, rather than failing silently at
// some future login.
func TestRotationRowWarnsForASourceBuild(t *testing.T) {
	installed := LiveryRotationRow(true).Subtitle
	source := LiveryRotationRow(false).Subtitle
	if installed == source {
		t.Fatal("a source build gets the same promise as an installed one")
	}
	if !strings.Contains(strings.ToLower(source), "path") {
		t.Errorf("source-build subtitle %q does not explain the dependency", source)
	}
}

// TestRotationIsUnavailableForACustomMark holds the UI half of the rotation
// guard: livery.Rotate refuses to advance a section pinned to the user's own
// SVG, so the switch that arms it must not stay operable for that selection.
func TestRotationIsUnavailableForACustomMark(t *testing.T) {
	if LiveryRotationAvailable(true, livery.CustomID) {
		t.Error("rotation offered for a custom mark, which livery.Rotate will not advance")
	}
	if !LiveryRotationAvailable(true, livery.DefaultID) {
		t.Error("rotation refused for a catalog mark in an available section")
	}
	if LiveryRotationAvailable(false, livery.DefaultID) {
		t.Error("rotation offered for a section that is not available")
	}
}
