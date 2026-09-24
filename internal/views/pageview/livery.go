package pageview

import (
	"fmt"
	"strings"

	"github.com/projectbluefin/chairlift/internal/livery"
)

// LiveryPageDescription is the page's one-line statement of what the three
// sections are for.
//
// It lands once, at page level, and each group's subtitle is the fragment
// that belongs to it — so the sentence is readable as a whole at the top and
// as a label beside each control. It lives here rather than inline in the
// builder so it is tested and so the fragments cannot drift out of step with
// the sentence they come from.
const LiveryPageDescription = "Who you are, who you stand with, and what you roll with."

// The three fragments, in the page's presentation order.
const (
	LiveryAppGridFragment = "Who you are"
	LiveryPanelFragment   = "Who you stand with"
	LiveryDockFragment    = "What you roll with"
)

// Section titles.
const (
	LiveryAppGridTitle = "App Grid Livery"
	LiveryPanelTitle   = "Foundational Livery"
	LiveryDockTitle    = "Dock Livery"
)

// LiveryChoice is the combo-row model for a foundation section: the labels in
// display order and the index of the current selection.
type LiveryChoice struct {
	Labels   []string
	Selected uint
}

// customLabel is the trailing entry that points at the user's own file.
const customLabel = "Custom SVG…"

// LiveryChoices builds the combo model for a foundation selection.
//
// The custom entry is always last so the shipped catalog's order is never
// disturbed by it. An id the catalog does not contain selects the default
// rather than leaving the combo unselected: dconf is writable by anything
// running as the user, and a stale or hand-edited value should land the page
// somewhere valid instead of blank.
func LiveryChoices(selectedID string) LiveryChoice {
	foundations := livery.Foundations()
	labels := make([]string, 0, len(foundations)+1)
	for _, f := range foundations {
		labels = append(labels, f.Name)
	}
	labels = append(labels, customLabel)

	if selectedID == livery.CustomID {
		return LiveryChoice{Labels: labels, Selected: uint(len(labels) - 1)}
	}
	if i := livery.IndexOfID(selectedID); i >= 0 {
		return LiveryChoice{Labels: labels, Selected: uint(i)}
	}
	return LiveryChoice{Labels: labels, Selected: uint(livery.IndexOfID(livery.DefaultID))}
}

// LiveryCustomResultID is the sentinel row that opens a file chooser.
//
// It lives inside each picker rather than as a fourth row in every section:
// choosing a mark is one decision, so it belongs in one place, and three
// sections each carrying a separate "Custom SVG…" row made the page taller
// than the window.
const LiveryCustomResultID = "\x00custom"

// LiveryPickerResult is one row in a search-picker dialog.
type LiveryPickerResult struct {
	ID       string
	Name     string
	Selected bool
}

// LiveryProjectResult is retained as the dock's result alias.
type LiveryProjectResult = LiveryPickerResult

// LiveryProjectResultLimit caps how many result rows the picker draws.
//
// The picker is a scrolling dialog rather than part of the page, so it can
// afford a long list; the cap exists to keep a bare query from building 214
// rows on every keystroke. An empty query returns the head of the catalog, so
// the list is populated before the user types.
const LiveryProjectResultLimit = 50

// LiveryProjectPickerTitle names the chooser dialog.
const LiveryProjectPickerTitle = "Choose a Project"

// LiveryFoundationResults returns the panel picker's rows for a query.
//
// The foundation catalog is ten entries, so its "search" mostly just lists
// them — but it is the same dialog as the other two, which is what lets every
// section be the same shape.
func LiveryFoundationResults(query, selectedID string) []LiveryPickerResult {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]LiveryPickerResult, 0, len(livery.Foundations())+1)
	for _, f := range livery.Foundations() {
		if q != "" && !strings.Contains(strings.ToLower(f.Name), q) && !strings.Contains(f.ID, q) {
			continue
		}
		out = append(out, LiveryPickerResult{ID: f.ID, Name: f.Name, Selected: f.ID == selectedID})
	}
	return out
}

// LiveryProjectResults returns the dock picker's rows for a query.
func LiveryProjectResults(query, selectedID string) []LiveryPickerResult {
	projects := livery.SearchCNCF(query, LiveryProjectResultLimit)
	out := make([]LiveryPickerResult, 0, len(projects))
	for _, p := range projects {
		out = append(out, LiveryPickerResult{ID: p.ID, Name: p.Name, Selected: p.ID == selectedID})
	}
	return out
}

// LiveryBrandResults returns the app-grid picker's rows for a query.
//
// The brand catalog is simpleicons.org's 3,461 marks — fifteen times the
// project catalog — which is precisely why this is a search dialog and not a
// text field the user has to already know the answer for.
func LiveryBrandResults(query, selectedSlug string) []LiveryPickerResult {
	icons := livery.SearchSimpleIcons(query, LiveryProjectResultLimit)
	out := make([]LiveryPickerResult, 0, len(icons))
	for _, icon := range icons {
		out = append(out, LiveryPickerResult{ID: icon.Slug, Name: icon.Title, Selected: icon.Slug == selectedSlug})
	}
	return out
}

// LiveryBrandPickerTitle names the brand chooser dialog.
const LiveryBrandPickerTitle = "Choose a Brand"

// LiveryBrandSearchPlaceholder is the brand search box's placeholder.
const LiveryBrandSearchPlaceholder = "Search brands…"

// LiverySelectedBrandRow describes the current app-grid selection.
func LiverySelectedBrandRow(slug string) Row {
	if slug == livery.CustomID {
		return Row{Title: "Brand", Subtitle: "Your own file"}
	}
	if icon, ok := livery.LookupSimpleIcon(slug); ok {
		return Row{Title: "Brand", Subtitle: icon.Title}
	}
	return Row{Title: "Brand", Subtitle: "Choose a brand mark"}
}

// LiverySelectedProjectRow describes the current dock selection.
func LiverySelectedProjectRow(selectedID string) Row {
	if selectedID == livery.CustomID {
		return Row{Title: "Project", Subtitle: "Your own file"}
	}
	if project, ok := livery.LookupCNCF(selectedID); ok {
		return Row{Title: "Project", Subtitle: project.Name}
	}
	return Row{Title: "Project", Subtitle: "Choose a CNCF project"}
}

// LiveryNoResultsRow is shown when a query matches nothing, so the section
// never renders as an empty box.
func LiveryNoResultsRow(query string) Row {
	return Row{
		Title:    "No matching project",
		Subtitle: fmt.Sprintf("Nothing in cncf/artwork matches %q", query),
	}
}

// LiveryIDForIndex maps a combo position back to a selection id.
func LiveryIDForIndex(index uint) string {
	foundations := livery.Foundations()
	if int(index) >= len(foundations) {
		return livery.CustomID
	}
	return foundations[index].ID
}

// LiveryAppGridRow is the app-grid section's switch row text.
//
// The subtitle says "wherever GNOME draws it" rather than naming the dash,
// because view-app-grid-symbolic is a shared Adwaita name: overriding it
// changes that glyph in the dash, the overview, and anything else drawing it.
// The same honesty the Files row owes, for the same reason.
func LiveryAppGridRow() Row {
	return Row{
		Title:    "Customize the App Grid Icon",
		Subtitle: "Replaces the Show Applications glyph wherever GNOME draws it",
	}
}

// LiveryPanelRow is the panel section's switch row text. A host without the
// Custom Command Menu extension never shows the section, so there is no
// unavailable variant.
func LiveryPanelRow() Row {
	return Row{Title: "Customize the Panel Icon", Subtitle: "Replaces the top-bar menu button, in your theme's colour"}
}

// LiveryDockRow is the dock section's switch row text.
//
// The subtitle names the full reach on purpose. GNOME stores one icon per
// application, so shadowing the Files mark changes it in the dash, the app
// grid, the window switcher, notifications, and every Open With menu — not
// only on the dock. A subtitle saying "dock" alone would be a quiet lie.
func LiveryDockRow() Row {
	return Row{
		Title:    "Customize the Files Icon",
		Subtitle: "Replaces the Files icon in full colour — on the dock, in the app grid, and in the window switcher",
	}
}

// LiveryRotationRow is a section's rotation switch text.
//
// A source build gets a different subtitle, because the login unit it writes
// points at the binary's current path. That path is stable for an installed
// build and disposable for a source one, and a unit whose ExecStart has gone
// away fails into the journal while the switch still reads "on".
func LiveryRotationRow(systemInstall bool) Row {
	if !systemInstall {
		return Row{
			Title:    "Rotate at Login",
			Subtitle: "Runs this build by its current path — reinstall or move it and rotation stops",
		}
	}
	return Row{
		Title:    "Rotate at Login",
		Subtitle: "Advances to the next mark each time you log in",
	}
}

// LiveryRotationAvailable reports whether a section's rotation switch may be
// operated.
//
// A custom mark is not part of a cycle: livery.Rotate refuses to advance a
// section pinned to the user's own SVG, so leaving the switch sensitive for
// that selection would offer a setting that does nothing. sectionAvailable
// carries the section's own conditions — the master switch, and for the panel
// whether the extension is installed.
func LiveryRotationAvailable(sectionAvailable bool, selectionID string) bool {
	return sectionAvailable && selectionID != livery.CustomID
}

// LiveryCustomRow shows which file a custom selection points at.
func LiveryCustomRow(path string) Row {
	if path == "" {
		return Row{Title: "Custom SVG", Subtitle: "No file chosen"}
	}
	return Row{Title: "Custom SVG", Subtitle: path}
}

// LiveryCNCFArtworkRow links to where the dock marks come from.
func LiveryCNCFArtworkRow() Row {
	return Row{
		Title:    "Browse CNCF Artwork",
		Subtitle: "Every mark here is the project's own icon from cncf/artwork",
	}
}

// CNCFArtworkURL is the repository the CNCF row opens.
const CNCFArtworkURL = livery.CNCFArtworkURL

// LiveryProjectSearchPlaceholder is the search box's placeholder text.
const LiveryProjectSearchPlaceholder = "Search CNCF projects…"

// LiveryCustomResult is the chooser row every picker ends with.
func LiveryCustomResult(selected bool) LiveryPickerResult {
	return LiveryPickerResult{ID: LiveryCustomResultID, Name: LiveryCustomRowTitle, Selected: selected}
}

// LiveryFoundationPickerTitle names the foundation chooser.
const LiveryFoundationPickerTitle = "Choose a Mark"

// LiveryFoundationSearchPlaceholder is its placeholder text.
const LiveryFoundationSearchPlaceholder = "Search marks…"

// LiverySelectedFoundationRow describes the current panel selection.
func LiverySelectedFoundationRow(id, customPath string) Row {
	if id == livery.CustomID {
		return Row{Title: "Mark", Subtitle: LiveryCustomRow(customPath).Subtitle}
	}
	if f, ok := livery.Lookup(id); ok {
		return Row{Title: "Mark", Subtitle: f.Name}
	}
	return Row{Title: "Mark", Subtitle: "Choose a mark"}
}

// LiveryFileChooserTitle names the custom-SVG chooser.
const LiveryFileChooserTitle = "Choose an SVG"

// LiveryFileChooserFilterName labels its file filter.
const LiveryFileChooserFilterName = "SVG images"

// LiveryCustomRowTitle labels the row that opens the chooser.
const LiveryCustomRowTitle = "Custom SVG…"

// LiveryCustomRowSubtitle describes what a custom mark is for.
//
// It names the constraint rather than leaving it to be discovered: the panel
// and app-grid surfaces draw silhouettes, so a full-colour SVG will arrive
// flattened, and a user who supplies one should know that before they are
// surprised by it.
const LiveryCustomRowSubtitle = "Use your own file instead of the list above"

// LiverySchemaMissingMessage is the diagnostic for a source build whose
// GSettings schema has not been compiled.
const LiverySchemaMissingMessage = "Livery settings are unavailable: run `make schemas`, or install the package, so the settings schema is present"
