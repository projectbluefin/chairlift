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
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

const (
	gateIdle uint32 = iota
	gateRunning
	gateComplete
)

// Presentation describes the group-level text after collection discovery.
// Placeholder fields are non-empty only when there are no collection rows.
type Presentation struct {
	Description         string
	PlaceholderTitle    string
	PlaceholderSubtitle string
}

// groupDescription states what the group does and the two consequences that
// matter once, at the group, rather than repeating them on every row: the
// software comes from a third party, and a whole collection is a large
// download.
const groupDescription = "Install a set of apps and tools together in one step. " +
	"Collections come from Homebrew, a third-party source, and can be a large download."

// Present derives the collection group's complete loaded state. warning is
// empty when every existing configured location was read successfully; it is
// a diagnostic for the log and never reaches the UI, because it names the
// locations that were searched.
func Present(count int, warning string, homebrewAvailable bool) Presentation {
	result := Presentation{Description: groupDescription}

	switch {
	case count == 0 && warning == "":
		result.PlaceholderTitle = "No collections available"
		result.PlaceholderSubtitle = "This system does not offer any app collections."
	case count == 0:
		result.PlaceholderTitle = "Collections could not be loaded"
		result.PlaceholderSubtitle = "Something went wrong while looking for app collections."
	case warning != "":
		result.Description += " Some collections could not be read."
	}

	if !homebrewAvailable {
		result.Description += " Homebrew is not installed, so nothing here can be installed yet."
	}
	return result
}

// Collection is the user-facing row text for one app collection.
type Collection struct {
	Title    string
	Subtitle string
}

type catalogEntry struct {
	title   string
	summary string
}

// catalog names the collections Bluefin publishes. Their on-disk identifiers
// are tooling names ("cli", "cncf", "system-flatpaks") and their leading
// comments are headings or provenance notes, so both the name and the
// description a person reads are written here.
var catalog = map[string]catalogEntry{
	"ai-tools":            {"AI tools", "Assistants and model runners you can use from the terminal."},
	"artwork":             {"Wallpapers", "Wallpapers and artwork for your desktop."},
	"cli":                 {"Command line tools", "A modern set of everyday terminal utilities."},
	"cncf":                {"Cloud native tools", "Tools for building and running cloud native software."},
	"dakota-dev-flatpaks": {"Extra developer apps", "Optional desktop apps for development work."},
	"dakota-fonts":        {"Extra fonts", "An additional font selection for design and development."},
	"experimental-ide":    {"Experimental code editors", "Early builds of code editors, still being tested."},
	"fonts":               {"Fonts", "A broad selection of fonts for documents and design."},
	"fonts-dev":           {"Coding fonts", "Fixed-width fonts made for reading code."},
	"full-desktop":        {"Full desktop", "A complete set of desktop apps for everyday use."},
	"ide":                 {"Code editors", "Popular code editors and development environments."},
	"k8s-tools":           {"Kubernetes tools", "Command line tools for working with Kubernetes clusters."},
	"swift":               {"Swift development", "Everything needed to build Swift projects."},
	"system-dx-flatpaks":  {"Developer apps", "Desktop apps for development work."},
	"system-flatpaks":     {"Everyday apps", "The desktop apps recommended for everyday use."},
}

const fallbackSummary = "A set of apps and tools put together for this system."

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
	if usable := usableSummary(id, comment); usable != "" {
		summary = usable
	}

	return Collection{Title: entry.title, Subtitle: joinSentences(summary, itemPhrase(itemCount))}
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

// InstallGate prevents overlapping invocations of one collection row's
// install action. Its zero value is ready for use.
type InstallGate struct {
	state atomic.Uint32
}

// TryStart starts an idle action and reports whether this caller acquired it.
func (g *InstallGate) TryStart() bool {
	return g.state.CompareAndSwap(gateIdle, gateRunning)
}

// Reset makes a failed or dry-run action available again.
func (g *InstallGate) Reset() {
	g.state.CompareAndSwap(gateRunning, gateIdle)
}

// Complete permanently closes a successfully installed collection action.
func (g *InstallGate) Complete() {
	g.state.CompareAndSwap(gateRunning, gateComplete)
}
