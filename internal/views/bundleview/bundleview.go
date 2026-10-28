// Package bundleview owns the pure presentation and action-state decisions
// used by the Applications page's app-collection group.
//
// A collection is discovered on disk under an identifier the person who
// installed the system never chose and should never read, so every name and
// description shown in the UI is resolved here: from the curated table below
// for the collections Bluefin ships, and from a readable transformation of
// the identifier for anything a site administrator adds. The definition's
// leading comment is used only when it reads as a human sentence — the
// shipped ones are variously a heading, a generated provenance note, or
// tooling jargon, none of which belong in a subtitle.
package bundleview

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/projectbluefin/chairlift/internal/homebrew"
)

// DeveloperAppsCollection is the collection Developer Mode installs when it
// is turned on. It is never offered as an App collection row.
const DeveloperAppsCollection = "dakota-dev-flatpaks"

// Presentation describes the group-level text after collection discovery.
// Placeholder fields are non-empty only when there are no collection rows.
type Presentation struct {
	Description         string
	PlaceholderTitle    string
	PlaceholderSubtitle string
}

// GroupDescription states what the group does and the one consequence that
// matters before pressing Install: a whole collection can be a large download.
const GroupDescription = "Install a group of apps and tools in one step. Some are large downloads."

// Present derives the collection group's complete loaded state. warning is
// empty when every existing configured location was read successfully; it is
// a diagnostic for the log and never reaches the UI, because it names the
// locations that were searched.
func Present(count int, warning string) Presentation {
	result := Presentation{Description: GroupDescription}

	switch {
	case count == 0 && warning == "":
		result.PlaceholderTitle = "No Collections Available"
		result.PlaceholderSubtitle = "This computer has no app collections."
	case count == 0:
		result.PlaceholderTitle = "Couldn't Load Collections"
		result.PlaceholderSubtitle = "Try again later."
	case warning != "":
		result.Description += " Some collections couldn't be loaded."
	}
	return result
}

// Collection is the user-facing row text for one app collection.
type Collection struct {
	Title    string
	Subtitle string
}

type catalogEntry struct {
	title    string
	summary  string
	curated  bool
	order    int
	defaults bool
	// members are sibling collections installed and checked together with
	// this one (a super-bundle). A member gets no row of its own while its
	// parent is discovered; see Visible.
	members []string
}

var catalog = map[string]catalogEntry{
	"cli": {
		title:    "Bluefin CLI",
		summary:  "A modern terminal environment with everyday utilities, Starship prompt, and shell bling.",
		curated:  true,
		order:    2,
		defaults: true,
	},
	"fonts": {
		title:    "Fonts",
		summary:  "A broad selection of everyday, design, office, and accessibility fonts.",
		curated:  true,
		order:    3,
		defaults: true,
		members:  []string{"dakota-fonts"},
	},
	"fonts-dev": {
		title:    "Monospace Fonts",
		summary:  "Fixed-width fonts made for reading code.",
		curated:  true,
		order:    4,
		defaults: true,
	},
	"ai-tools": {
		title:    "Agentic Tools",
		summary:  "AI assistants and model runners for the terminal.",
		curated:  true,
		order:    5,
		defaults: true,
	},
	"full-desktop": {
		title:    "Full Desktop Suite",
		summary:  "A complete set of desktop apps for everyday productivity.",
		curated:  true,
		order:    6,
		defaults: false,
	},
}

// retired collections are Bluefin-shipped bundles that have been explicitly
// removed from the UI. They are filtered out so they do not fall back to
// raw humanized names ("Cncf", "Nsl", etc.).
var retired = map[string]struct{}{
	"cncf":                {},
	"k8s-tools":           {},
	"swift":               {},
	"ide":                 {},
	"experimental-ide":    {},
	"nsl":                 {},
	"system-dx-flatpaks":  {},
	"system-flatpaks":     {},
	"video-wallpaper":     {},
	"dakota-dev-flatpaks": {}, // installed automatically with devmode
}

// Visible reports whether an on-disk collection should be offered as a row.
// Retired Bluefin bundles are excluded; user-added collections remain visible.
// A super-bundle member (e.g. dakota-fonts) is folded into its parent's
// row when that parent was discovered, and keeps its own row otherwise so it
// stays installable.
func Visible(id string, discovered map[string]homebrew.Bundle) bool {
	if _, isRetired := retired[id]; isRetired {
		return false
	}
	for parent, entry := range catalog {
		if _, ok := discovered[parent]; !ok {
			continue
		}
		for _, member := range entry.members {
			if member == id {
				return false
			}
		}
	}
	return true
}

// Curated reports whether id is one of the curated app collections shown in the UI.
func Curated(id string) bool {
	entry, ok := catalog[id]
	return ok && entry.curated
}

// DefaultSelected reports whether id defaults to checked.
// All collections default to checked except Full Desktop Suite.
func DefaultSelected(id string) bool {
	if entry, ok := catalog[id]; ok {
		return entry.defaults
	}
	return true
}

// Order returns the sort order for a collection.
func Order(id string) int {
	if entry, ok := catalog[id]; ok && entry.curated {
		return entry.order
	}
	return 100
}

// Members returns the sibling collection identifiers installed together with
// id as a super-bundle, excluding id itself. Most collections have none.
func Members(id string) []string {
	entry, ok := catalog[id]
	if !ok || len(entry.members) == 0 {
		return nil
	}
	return append([]string(nil), entry.members...)
}

// MemberBundles resolves id's super-bundle siblings against the collections
// discovered on disk, in catalog order. A sibling that was not discovered is
// skipped: the row then covers only what this computer actually ships.
func MemberBundles(id string, discovered map[string]homebrew.Bundle) []homebrew.Bundle {
	var result []homebrew.Bundle
	for _, name := range Members(id) {
		if b, ok := discovered[name]; ok {
			result = append(result, b)
		}
	}
	return result
}

// AggregateInstalled combines the observed states of every Brewfile a row
// covers. Any member observed not installed makes the row not installed;
// otherwise an undecided member leaves the row as it was (known is false),
// and only when every member is observed installed is the row installed.
func AggregateInstalled(statuses []homebrew.BundleStatus) (installed, known bool) {
	if len(statuses) == 0 {
		return false, false
	}
	undecided := false
	for _, status := range statuses {
		got, ok := ObservedInstalled(status)
		if !ok {
			undecided = true
			continue
		}
		if !got {
			return false, true
		}
	}
	if undecided {
		return false, false
	}
	return true, true
}

const fallbackSummary = "A set of apps and tools put together for this computer."

// jargonMarkers are the substrings that disqualify a collection's leading
// comment from being shown to a person. Each one is either a packaging term,
// a tool a person never runs directly, or the shape of a path or address.
var jargonMarkers = []string{
	"brewfile", "brew ", "tap ", "cask", "formula", "formulae",
	"flatpak", "flathub", "ujust", "bootc", "rpm-ostree", "ostree",
	"quadlet", "systemd", "generated from", "install with", "http", "/", "\\",
}

// Describe returns the row text for one app collection. id is the collection's
// on-disk identifier, comment is the leading comment of its definition, and
// itemCount is how many apps and tools it installs.
func Describe(id, comment string, itemCount int) Collection {
	entry, known := catalog[id]
	if !known {
		entry = catalogEntry{title: humanize(id), summary: fallbackSummary}
	}
	summary := entry.summary
	if !entry.curated {
		if usable := usableSummary(id, comment); usable != "" {
			summary = usable
		}
	}

	sub := joinSentences(summary, itemPhrase(itemCount))
	return Collection{Title: entry.title, Subtitle: sub}
}

// DescribeInstalled returns the row text for one app collection, appending
// "(Installed)" when installed is true.
func DescribeInstalled(id, comment string, itemCount int, installed bool) Collection {
	c := Describe(id, comment, itemCount)
	if installed {
		c.Subtitle += " (Installed)"
	}
	return c
}

// usableSummary returns the collection's own comment when it reads as a
// description a person would recognize, and "" when it is jargon, a heading,
// or a restatement of the identifier.
func usableSummary(id, comment string) string {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return ""
	}

	lowered := strings.ToLower(comment)
	for _, marker := range jargonMarkers {
		if strings.Contains(lowered, marker) {
			return ""
		}
	}
	if isHeading(comment) || lowered == strings.ToLower(humanize(id)) {
		return ""
	}

	runes := []rune(comment)
	runes[0] = unicode.ToUpper(runes[0])
	return endSentence(string(runes))
}

// isHeading reports whether every word is capitalized, which is a title
// someone wrote for a file rather than a sentence describing its contents.
func isHeading(comment string) bool {
	words := strings.Fields(comment)
	if len(words) < 2 {
		return true
	}
	for _, word := range words {
		// Only the leading rune decides. An administrator's "k9s" or "GPU"
		// must survive intact, so a lowercase tail is not evidence of a
		// sentence — and decoding just the first rune avoids allocating a
		// []rune per word.
		first, _ := utf8.DecodeRuneInString(word)
		if unicode.IsLower(first) {
			return false
		}
	}
	return true
}

// humanize turns an unknown collection's identifier into something readable:
// separators become spaces and the first word is capitalized. The remaining
// words keep their own case, because an administrator's "k9s" or "GPU" must
// survive intact.
func humanize(id string) string {
	spaced := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '.' {
			return ' '
		}
		return r
	}, id)

	words := strings.Fields(spaced)
	if len(words) == 0 {
		return "Collection"
	}
	first := []rune(words[0])
	first[0] = unicode.ToUpper(first[0])
	words[0] = string(first)
	return strings.Join(words, " ")
}

// itemPhrase states how much a person is about to install. A collection whose
// entries could not be counted says nothing rather than claiming zero.
func itemPhrase(itemCount int) string {
	switch {
	case itemCount <= 0:
		return ""
	case itemCount == 1:
		return "Includes 1 app or tool."
	default:
		return fmt.Sprintf("Includes %d apps and tools.", itemCount)
	}
}

func joinSentences(sentences ...string) string {
	parts := make([]string, 0, len(sentences))
	for _, sentence := range sentences {
		if sentence != "" {
			parts = append(parts, sentence)
		}
	}
	return strings.Join(parts, " ")
}

func endSentence(text string) string {
	if strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?") {
		return text
	}
	return text + "."
}

// ObservedInstalled maps a `brew bundle check` status onto a collection's
// installed state. known is false when the check could not decide, which
// must leave the row as it was. Update Available counts as installed: every
// item is present, and upgrading them is the Updates page's job, not a
// reason to offer the collection again.
func ObservedInstalled(status homebrew.BundleStatus) (installed, known bool) {
	switch status {
	case homebrew.BundleInstalled, homebrew.BundleUpdateAvailable:
		return true, true
	case homebrew.BundleNotInstalled:
		return false, true
	default:
		return false, false
	}
}

// ContentsHiddenTitle is what the list rows call themselves when nothing
// is left to show: it appears once a parse produced no recognised entries,
// so the user knows the row is not a glitch and that the Install button is
// still safe to press.
const ContentsHiddenTitle = "No entries could be read for this collection."

// ContentsHiddenSubtitle names the limit that hid them: a Brewfile too
// large for one read. Keeping the message concrete avoids the "could not
// load" tone that would otherwise admit a real failure has happened.
const ContentsHiddenSubtitle = "The Brewfile is larger than the display limit."

// ContentsRowName is the accessible name of an entry inside the expanded
// list. The bare formula or cask token is enough for a screen reader to
// distinguish one item from the next, so the affordance stays predictable
// without inventing a description a person may not recognize.
func ContentsRowName(name string) string {
	return name
}

// PresentContents derives the package-list presentation one collection row
// renders when a person opens it. The items come in the order the Brewfile
// named them, deduplicated by the homebrew layer. Truncation is reported
// separately from emptiness, because a real Brewfile that exceeds the
// display cap and a Brewfile that holds no installable entries must look
// different: one hides details behind a count, the other says so outright.
//
// items is the slice the homebrew layer actually returned. total is the
// raw entry count the same layer reported; it can be larger than
// len(items) when the parser hit its own cap and stopped reading. When the
// parser produced nothing at all (an empty or unrecognised file), total
// will be 0 too — that case is the "no recognised entries" placeholder
// rather than the "Brewfile is larger" one.
func PresentContents(items []homebrew.BundleItem, total int) ContentsPresentation {
	if len(items) == 0 {
		if total > 0 {
			return ContentsPresentation{
				EmptyTitle:    ContentsHiddenTitle,
				EmptySubtitle: ContentsHiddenSubtitle,
			}
		}
		return ContentsPresentation{
			EmptyTitle:    ContentsHiddenTitle,
			EmptySubtitle: "This collection's Brewfile has no recognized entries.",
		}
	}

	rows := make([]ContentsRow, 0, len(items))
	for _, item := range items {
		rows = append(rows, ContentsRow{
			Name:           item.Name,
			KindLabel:      ContentsKindLabel(item.Kind),
			AccessibleName: ContentsRowName(item.Name),
		})
	}

	var overflow string
	if total > len(items) {
		overflow = fmt.Sprintf("Showing %d of %d entries.", len(items), total)
	}

	return ContentsPresentation{
		Rows:           rows,
		OverflowNotice: overflow,
	}
}

// ContentsKindLabel names the kind of one entry as a person would read it:
// formulae are "Command-line tool", casks are "App", the rest keep their
// on-disk identifiers so an "App Store" item does not read like a "Flatpak".
// The labels are stable across collections so a screen reader user can
// tell the shape of an entry from its kind text alone.
func ContentsKindLabel(kind homebrew.BundleItemKind) string {
	switch kind {
	case homebrew.BundleItemBrew:
		return "Command-line tool"
	case homebrew.BundleItemCask:
		return "App"
	case homebrew.BundleItemFlatpak:
		return "Flatpak"
	case homebrew.BundleItemMas:
		return "App Store"
	case homebrew.BundleItemVSCode:
		return "VS Code extension"
	default:
		return ""
	}
}

// ContentsPresentation is the immutable view of one collection's expanded
// list. It holds the rows to render and, when a row was cropped, the note
// that explains why; the empty placeholders are non-empty only when the
// underlying Brewfile had nothing to show.
type ContentsPresentation struct {
	Rows           []ContentsRow
	OverflowNotice string
	EmptyTitle     string
	EmptySubtitle  string
}

// HasRows reports whether there is at least one entry to render. Views
// use it to decide between the row list and the empty placeholder.
func (p ContentsPresentation) HasRows() bool {
	return len(p.Rows) > 0
}

// ContentsRow is one entry in the expanded package list. Name is the
// on-disk identifier a person sees, KindLabel is the human-readable kind
// used as the row's subtitle, and AccessibleName is the name a screen
// reader announces when this row is focused.
type ContentsRow struct {
	Name           string
	KindLabel      string
	AccessibleName string
}

// ContentsSubtitle preserves the package type when metadata is unavailable.
// Kinds with no description provider show the type alone, so the row does
// not read as a failed lookup.
func ContentsSubtitle(item homebrew.BundleItem) string {
	kind := ContentsKindLabel(item.Kind)
	description := strings.TrimSpace(item.Description)
	if description == "" {
		if !hasDescriptionProvider(item.Kind) {
			return kind
		}
		description = "Description unavailable"
	}
	if kind == "" {
		return description
	}
	return kind + " · " + description
}

func hasDescriptionProvider(kind homebrew.BundleItemKind) bool {
	return kind == homebrew.BundleItemBrew || kind == homebrew.BundleItemCask || kind == homebrew.BundleItemFlatpak
}
