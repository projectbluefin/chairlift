// Package livery implements ChairLift's Livery page: the foundation mark
// shown in the GNOME panel by the Custom Command Menu extension, and the
// color mark shown on the dock.
//
// Both halves are the same operation. A GNOME panel icon is a *themed icon
// name*, never a path: the extension builds `new St.Icon({icon_name: ...})`
// from its `menuicon-setting` string, so an absolute path there renders
// nothing at all. Setting a mark therefore means installing an SVG into the
// user's icon theme and referencing it by bare name. The dock is the same
// trick one layer down: `~/.local/share/icons` outranks `/usr/share/icons`
// in XDG search order, so writing an SVG under an application's existing
// icon name shadows the system one without touching a desktop entry that
// another package owns.
//
// That shadowing is why nothing here edits a .desktop file. Desktop entries
// replace rather than merge, and Nautilus's is 250+ lines of localized names,
// a MimeType list, and a [Desktop Action] group; a generated override that
// dropped any of it would silently cost the user their default file-manager
// association. Shadowing the icon name costs one file and reverts with one
// unlink.
//
// Nothing here is privileged. Every write lands under the invoking user's
// $XDG_DATA_HOME and $XDG_CONFIG_HOME, and the only settings touched are that
// user's own dconf values — the same unprivileged, per-user posture as
// gaming mode and the AI stack.
package livery

import (
	"embed"
	"fmt"
)

// assets holds the vendored foundation marks. They are embedded rather than
// installed so the feature has no missing-asset failure mode and no packaging
// step: a mark cannot go missing from a binary that contains it.
//
// Marks are derived from Simple Icons (CC0), except Universal Blue's, which
// is the logo Bluefin already ships. They carry fill="currentColor" so GTK
// and GNOME Shell recolor them to the theme foreground, per GNOME's
// symbolic-icon guidance.
//
//go:embed assets/*-symbolic.svg
var assets embed.FS

// Foundation is one selectable mark.
type Foundation struct {
	// ID is the stable identifier persisted in GSettings and used to name
	// the embedded asset files. It never changes once shipped.
	ID string
	// Name is the user-visible label.
	Name string
}

// foundations is the shipped catalog, in the order the page presents it.
//
// The order is the one the product owner specified and is not derived from
// anything measurable. An earlier draft of this feature proposed ranking the
// tail "by how much of their code ships in Bluefin"; that ranking was dropped
// rather than invented, because the package inventory it would need is not
// available to this program and a plausible-looking guess presented as a
// measurement is worse than no ranking at all. Adding an entry is a one-line
// change here plus two SVGs, and the custom-icon path already covers anything
// not listed.
var foundations = []Foundation{
	{ID: "cncf", Name: "Cloud Native Computing Foundation"},
	{ID: "linux-foundation", Name: "Linux Foundation"},
	{ID: "gnome", Name: "GNOME Foundation"},
	{ID: "freedesktop", Name: "freedesktop.org"},
	{ID: "apache", Name: "Apache Software Foundation"},
	{ID: "rust", Name: "Rust Foundation"},
	// Universal Blue is the mark Bluefin ships and boots with, so it is both
	// a foundational entry in its own right and the selection that restores
	// the stock look.
	{ID: "universal-blue", Name: "Universal Blue"},
	{ID: "bazzite", Name: "Bazzite"},
	{ID: "aurora", Name: "Aurora"},
	// The Open Gaming Collective is the default on a gaming image; see
	// DefaultFoundationID.
	{ID: "open-gaming-collective", Name: "Open Gaming Collective"},
}

// DefaultID is the selection a fresh install starts from on an ordinary
// image. A gaming image starts somewhere else; see DefaultFoundationID.
const DefaultID = "cncf"

// GamingDefaultID is the mark a gaming image starts from.
const GamingDefaultID = "open-gaming-collective"

// DefaultFoundationID returns the starting mark for this machine.
//
// A gaming image gets the Open Gaming Collective, because that is the
// community whose work the image ships — the same reasoning that puts
// Universal Blue's own mark in the list. It is a *default*, so it applies
// only where the user has not chosen for themselves: switching to a gaming
// image adopts the mark, and someone who has deliberately picked CNCF keeps
// CNCF.
func DefaultFoundationID(gaming bool) string {
	if gaming {
		return GamingDefaultID
	}
	return DefaultID
}

// CustomID is the sentinel selection meaning "use the user's own SVG",
// whose path is held separately in GSettings. It is deliberately not a
// possible Foundation.ID.
const CustomID = "custom"

// Foundations returns a copy of the shipped catalog, so callers cannot
// mutate shared state.
func Foundations() []Foundation {
	out := make([]Foundation, len(foundations))
	copy(out, foundations)
	return out
}

// Lookup returns the foundation with the given ID.
func Lookup(id string) (Foundation, bool) {
	for _, f := range foundations {
		if f.ID == id {
			return f, true
		}
	}
	return Foundation{}, false
}

// IndexOfID returns the catalog position of id, or -1. Rotation uses it to
// advance deterministically.
func IndexOfID(id string) int {
	for i, f := range foundations {
		if f.ID == id {
			return i
		}
	}
	return -1
}

// Asset returns the embedded SVG bytes for a foundation mark.
//
// There is one rendition, symbolic. The foundation marks are only ever drawn
// in the panel, which recolors symbolic icons to the theme foreground; the
// dock draws a CNCF project's own artwork instead, fetched in full color from
// cncf/artwork. A second embedded color rendition existed while the dock used
// this catalog and is gone with it.
func Asset(id string) ([]byte, error) {
	if _, ok := Lookup(id); !ok {
		return nil, fmt.Errorf("livery: unknown foundation %q", id)
	}
	name := "assets/" + id + "-symbolic.svg"
	data, err := assets.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("livery: reading embedded asset %s: %w", name, err)
	}
	return data, nil
}

// NextID returns the selection after id, wrapping at the end of the catalog.
//
// It is deterministic and total: an unknown or custom id (a selection the
// catalog no longer contains, or a user's own SVG) advances to the first
// entry rather than erroring, so a rotation run can never leave the selection
// unset.
func NextID(id string) string {
	i := IndexOfID(id)
	if i < 0 || i == len(foundations)-1 {
		return foundations[0].ID
	}
	return foundations[i+1].ID
}
