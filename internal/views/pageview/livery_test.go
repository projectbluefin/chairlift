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
	fragments := []string{LiveryAppGridFragment, LiveryPanelFragment, LiveryDockFragment}
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
	for _, fragment := range []string{LiveryAppGridFragment, LiveryPanelFragment, LiveryDockFragment} {
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
func TestAppGridTextCoversGNOMEAndPlasmaAvailability(t *testing.T) {
	subtitle := strings.ToLower(LiveryAppGridRow().Subtitle)
	for _, desktop := range []string{"gnome", "kickoff", "kde plasma"} {
		if !strings.Contains(subtitle, desktop) {
			t.Errorf("app-grid subtitle %q does not mention %q", subtitle, desktop)
		}
	}
	if got := LiveryAppGridGroupDescription(true); got != LiveryAppGridFragment {
		t.Errorf("available app-grid description = %q, want %q", got, LiveryAppGridFragment)
	}
	if got := LiveryAppGridGroupDescription(false); !strings.Contains(strings.ToLower(got), "unavailable") || !strings.Contains(strings.ToLower(got), "kickoff") {
		t.Errorf("unavailable app-grid description = %q, want it to explain the missing Kickoff target", got)
	}
}

func TestDockRowNamesItsFullReach(t *testing.T) {
	subtitle := strings.ToLower(LiveryDockRow().Subtitle)
	for _, surface := range []string{"dock", "app grid", "window switcher"} {
		if !strings.Contains(subtitle, surface) {
			t.Errorf("dock subtitle %q does not mention %q", subtitle, surface)
		}
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

// TestNoResultsRowNamesTheCatalogItSearched holds issue #350: the brand and
// mark choosers search Simple Icons and the foundation marks, so their empty
// result must neither blame cncf/artwork nor call the missing item a project.
func TestNoResultsRowNamesTheCatalogItSearched(t *testing.T) {
	cases := []struct {
		surface livery.Surface
		title   string
		catalog string
	}{
		{livery.AppGrid, "No matching brand", "Simple Icons"},
		{livery.Panel, "No matching mark", "foundation mark"},
		{livery.Dock, "No matching project", "cncf/artwork"},
	}
	for _, tc := range cases {
		row := LiveryNoResultsRow(tc.surface, "zzq")
		if row.Title != tc.title {
			t.Errorf("surface %d: title %q, want %q", tc.surface, row.Title, tc.title)
		}
		if !strings.Contains(row.Subtitle, tc.catalog) || !strings.Contains(row.Subtitle, `"zzq"`) {
			t.Errorf("surface %d: subtitle %q does not name %q and the query", tc.surface, row.Subtitle, tc.catalog)
		}
		if tc.surface != livery.Dock && strings.Contains(row.Subtitle, "cncf/artwork") {
			t.Errorf("surface %d: subtitle %q blames cncf/artwork", tc.surface, row.Subtitle)
		}
	}
}
