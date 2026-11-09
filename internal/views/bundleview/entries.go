package bundleview

import (
	"errors"
	"fmt"
	"strings"

	"github.com/projectbluefin/chairlift/internal/homebrew"
)

// The sources a collection entry can come from. The vocabulary is closed on
// purpose: a row's subtitle names where an entry comes from, and a person
// reading six collections should not have to learn a new source per
// collection.
const (
	// SourceHomebrew covers Homebrew's official formula and cask repositories.
	SourceHomebrew = "Homebrew"
	// SourceBrewTap is a package from a third-party Homebrew tap.
	SourceBrewTap = "Brew Tap"
	// SourceFlathub is a Flatpak application from the Flathub remote.
	SourceFlathub = "Flathub"
	// SourceAppStore is an App Store application.
	SourceAppStore = "the App Store"
	// SourceVSCode is a VS Code extension.
	SourceVSCode = "the VS Code Marketplace"
)

// FlathubRemote is the remote Flatpak reports for an application installed
// from Flathub.
const FlathubRemote = "flathub"

// ContentsSource returns the source a collection entry comes from, derived
// from the entry's kind and the shape of its token.
//
// Bare Homebrew tokens and tokens qualified by its official repositories
// come from Homebrew; other qualified tokens name a third-party tap.
func ContentsSource(item homebrew.BundleItem) string {
	switch item.Kind {
	case homebrew.BundleItemBrew, homebrew.BundleItemCask:
		if strings.Contains(item.Name, "/") && !strings.HasPrefix(item.Name, homebrew.CoreTap+"/") && !strings.HasPrefix(item.Name, homebrew.CaskTap+"/") {
			return SourceBrewTap
		}
		return SourceHomebrew
	case homebrew.BundleItemFlatpak:
		return SourceFlathub
	case homebrew.BundleItemMas:
		return SourceAppStore
	case homebrew.BundleItemVSCode:
		return SourceVSCode
	default:
		return ""
	}
}

// ObservedSource maps what the inventory reported about an installed entry
// onto the source vocabulary. It returns "" when the observation names
// something ChairLift does not have a word for, so the caller falls back to
// the entry's static shape rather than inventing a new label.
//
// The observation is what tells a homebrew/core package from a tap package
// that shares its name, and what tells where a Flatpak application
// actually came from.
func ObservedSource(kind homebrew.BundleItemKind, observed string) string {
	switch kind {
	case homebrew.BundleItemBrew, homebrew.BundleItemCask:
		switch observed {
		case "":
			return ""
		case homebrew.CoreTap, homebrew.CaskTap:
			return SourceHomebrew
		default:
			return SourceBrewTap
		}
	case homebrew.BundleItemFlatpak:
		if observed == FlathubRemote {
			return SourceFlathub
		}
		return ""
	default:
		return ""
	}
}

// SourceLabel is the wording for an entry that is not on the system yet:
// where it comes from.
func SourceLabel(source string) string {
	if source == "" {
		return ""
	}
	return "from " + source
}

// InstalledSource is the wording for an entry that is already on the
// system: where it is installed from.
func InstalledSource(source string) string {
	if source == "" {
		return ""
	}
	return "Installed from " + source
}

// Selectable reports whether ChairLift can install an entry of this kind on
// its own.
//
// App Store and VS Code Marketplace directives cannot be selected here.
// A collection containing them uses per-manager installs, never the whole
// Brewfile, so an unchecked directive cannot be installed implicitly.
func Selectable(kind homebrew.BundleItemKind) bool {
	switch kind {
	case homebrew.BundleItemBrew, homebrew.BundleItemCask, homebrew.BundleItemFlatpak:
		return true
	default:
		return false
	}
}

// EntryState is what the view knows about one collection entry.
type EntryState struct {
	// Item is the entry as the Brewfile named it.
	Item homebrew.BundleItem
	// Installed reports whether the entry's package is already on the
	// system.
	Installed bool
	// Source is what the inventory reported about the installed package:
	// a tap name for Homebrew, a remote name for Flatpak. It is empty
	// when the inventory knew nothing.
	Source string
	// Checked reports whether the entry's checkbox is checked.
	Checked bool
	// DescriptionLoaded distinguishes missing metadata from a lookup not yet
	// requested; metadata loads only after expanding the collection.
	DescriptionLoaded bool
}

// EntryPresentation is one entry row's text and state.
type EntryPresentation struct {
	// Name is the raw Brewfile token, unchanged: it is the identifier a
	// person needs to look the package up.
	Name string
	// Label is the row's subtitle: where the entry comes from, or where
	// it is installed from.
	Label string
	// AccessibleName names the row's checkbox for a screen reader.
	AccessibleName string
	// Selectable reports whether the entry can be chosen for an install.
	Selectable bool
	// Installed reports whether the entry is already on the system.
	Installed bool
}

// PresentEntry derives the row text and state for one collection entry.
//
// The label names the source: the static shape of the token for an entry
// that is not on the system, and the observed source for one that is, so
// a tap package that shares a core package's name does not read as a core
// one.
func PresentEntry(state EntryState) EntryPresentation {
	source := ContentsSource(state.Item)
	if state.Installed {
		if observed := ObservedSource(state.Item.Kind, state.Source); observed != "" {
			source = observed
		}
	}
	label := SourceLabel(source)
	if state.Installed {
		label = InstalledSource(source)
	}
	if state.DescriptionLoaded {
		if description := EntryDescription(state.Item); description != "" {
			label += " · " + description
		}
	}
	return EntryPresentation{
		Name:           state.Item.Name,
		Label:          label,
		AccessibleName: EntryAccessibleName(state.Item.Name),
		Selectable:     Selectable(state.Item.Kind),
		Installed:      state.Installed,
	}
}

// RefreshChecked preserves deliberate selections but releases a checkbox
// auto-checked by an earlier installed observation when that package goes away.
func RefreshChecked(checked, wasInstalled, installed bool) bool {
	return installed || (checked && !wasInstalled)
}

// EntryAccessibleName names an entry's checkbox for a screen reader. Every
// entry row shows the same words otherwise, so the name carries the token
// the checkbox acts on.
func EntryAccessibleName(token string) string {
	return "Select " + token
}

// SelectAllAccessibleName names a collection's "Select All" button for a
// screen reader, after the collection it selects.
func SelectAllAccessibleName(title string) string {
	return "Select All in " + title
}

// Selection is one entry's selectable and checked state.
type Selection struct {
	Selectable bool
	Checked    bool
}

// SelectAllEnabled reports whether a collection's "Select All" button still
// has something to select.
//
// The button is one-way — it selects and never deselects — so it is
// insensitive once every selectable entry is checked. A collection with no
// selectable entry has nothing to select either.
func SelectAllEnabled(selections []Selection) bool {
	for _, selection := range selections {
		if selection.Selectable && !selection.Checked {
			return true
		}
	}
	return false
}

// AllSelected reports whether every entry can be selected and is checked.
// Unsupported directives must not become implicit whole-Brewfile installs.
func AllSelected(selections []Selection) bool {
	for _, selection := range selections {
		if !selection.Selectable || !selection.Checked {
			return false
		}
	}
	return len(selections) > 0
}

// DescribeEntries returns the collection row's subtitle for the number of
// its entries that are already on the system.
//
// A collection whose every entry is installed reads "(Installed)"; a
// partially installed one says how many of its entries are there. A
// collection with nothing installed keeps its plain description.
func DescribeEntries(id, comment string, itemCount, installedCount int) Collection {
	collection := Describe(id, comment, itemCount)
	switch {
	case itemCount <= 0:
	case installedCount >= itemCount:
		collection.Subtitle += " (Installed)"
	case installedCount > 0:
		collection.Subtitle += fmt.Sprintf(" %d of %d installed.", installedCount, itemCount)
	}
	return collection
}

// InstallPlan is what one collection's selection asks for.
type InstallPlan struct {
	// BundlePaths is set when the whole collection is selected: the
	// collection's own Brewfiles install through `brew bundle install`,
	// which keeps the tap and trust declarations the file carries.
	BundlePaths []string
	// Formulae, Casks and Flatpaks are the individual installs of a
	// partially selected collection.
	Formulae []string
	Casks    []string
	Flatpaks []string
	// Pending counts the entries the plan installs: the entries that are
	// checked and not already on the system.
	Pending int
}

// Empty reports whether the plan asks for nothing.
func (p InstallPlan) Empty() bool {
	return len(p.BundlePaths) == 0 && len(p.Formulae) == 0 && len(p.Casks) == 0 && len(p.Flatpaks) == 0
}

// PlanInstall derives what a collection's selection installs.
//
// A collection whose every entry is selectable and checked installs as a
// whole through `brew bundle`, preserving its tap and trust declarations.
// Otherwise only checked, supported entries install, batched per manager;
// unsupported App Store/VS Code directives never run implicitly.
//
// An entry that is already on the system is never part of the plan: its
// checkbox is disabled, and installing it again would only repeat work.
func PlanInstall(entries []EntryState, memberPaths []string) InstallPlan {
	selections := make([]Selection, 0, len(entries))
	var pending []EntryState
	type entryKey struct {
		kind homebrew.BundleItemKind
		name string
	}
	seenPending := make(map[entryKey]bool, len(entries))
	for _, entry := range entries {
		selectable := Selectable(entry.Item.Kind)
		selections = append(selections, Selection{Selectable: selectable, Checked: entry.Checked})
		if selectable && entry.Checked && !entry.Installed {
			key := entryKey{kind: entry.Item.Kind, name: entry.Item.Name}
			if !seenPending[key] {
				seenPending[key] = true
				pending = append(pending, entry)
			}
		}
	}
	if len(pending) == 0 {
		return InstallPlan{}
	}
	if AllSelected(selections) && len(memberPaths) > 0 {
		return InstallPlan{
			BundlePaths: append([]string(nil), memberPaths...),
			Pending:     len(pending),
		}
	}

	var plan InstallPlan
	for _, entry := range pending {
		switch entry.Item.Kind {
		case homebrew.BundleItemBrew:
			plan.Formulae = append(plan.Formulae, entry.Item.Name)
		case homebrew.BundleItemCask:
			plan.Casks = append(plan.Casks, entry.Item.Name)
		case homebrew.BundleItemFlatpak:
			plan.Flatpaks = append(plan.Flatpaks, entry.Item.Name)
		}
	}
	plan.Pending = len(pending)
	return plan
}

// NoSelectionToast is what an install run says when nothing is checked.
const NoSelectionToast = "No apps selected to install."

// PreviewToast is what a dry-run install says instead of claiming it
// installed anything.
const PreviewToast = "[DRY-RUN] Preview: selected apps would be installed — no changes made"

// InstallOutcome describes what one install run did.
type InstallOutcome struct {
	// Installed counts the apps and tools the run installed.
	Installed int
	// Failed lists the collections whose install did not finish.
	Failed []InstallFailure
	// TrustedPackages counts the packages trusted one by one.
	TrustedPackages int
	// TrustedTaps counts the taps trusted wholesale.
	TrustedTaps int
}

// InstallFailure names a collection whose install did not finish and, when
// the package manager said why, its reason.
type InstallFailure struct {
	// Collection is the collection's display title.
	Collection string
	// Reason is the package manager's own one-line diagnosis, or empty
	// when it gave none.
	Reason string
}

// FailureReason extracts the reason a collection install failed from its
// error, or "" when the error carries no diagnosis worth showing.
//
// Only Homebrew's distilled diagnosis qualifies: it is one bounded line that
// names the failing package ("font-opendyslexic: Cask 'font-opendyslexic'
// has been disabled …"), which is what tells a person a retry cannot help
// (issue #732). Brew's "Error:" prefix is dropped because the toast already
// says the install failed.
func FailureReason(err error) string {
	var brewErr *homebrew.Error
	if !errors.As(err, &brewErr) {
		return ""
	}
	reason := strings.TrimSpace(brewErr.Detail)
	const prefix = "error:"
	if len(reason) >= len(prefix) && strings.EqualFold(reason[:len(prefix)], prefix) {
		reason = strings.TrimSpace(reason[len(prefix):])
	}
	return reason
}

// InstallToast returns the toast text for a finished install run.
//
// A failed run names each collection that did not install and, when the
// package manager said why, the reason, so a person can tell an entry that
// can never install from a transient failure worth retrying.
//
// The toast says what the trust store gained whenever it gained anything:
// trusting a source changes what this computer will load from it, so the
// change is never silent.
func InstallToast(outcome InstallOutcome) string {
	var text string
	if len(outcome.Failed) > 0 {
		text = failureToast(outcome.Failed)
	} else {
		text = fmt.Sprintf("Installed %d %s.", outcome.Installed, plural(outcome.Installed, "app or tool", "apps and tools"))
	}
	if notice := TrustNotice(outcome.TrustedPackages, outcome.TrustedTaps); notice != "" {
		text += " " + notice
	}
	return text
}

// failureToast states which collections failed and, where known, why.
func failureToast(failed []InstallFailure) string {
	titles := make([]string, 0, len(failed))
	explained := false
	for _, failure := range failed {
		titles = append(titles, failure.Collection)
		explained = explained || failure.Reason != ""
	}
	if !explained {
		return fmt.Sprintf("Couldn't install all selected apps (%s). Try again.", strings.Join(titles, ", "))
	}
	parts := make([]string, 0, len(failed)+1)
	parts = append(parts, "Couldn't install all selected apps.")
	for _, failure := range failed {
		reason := failure.Reason
		if reason == "" {
			reason = "try again"
		}
		parts = append(parts, failure.Collection+": "+sentence(reason))
	}
	return strings.Join(parts, " ")
}

// sentence ends text with a full stop unless it already ends a sentence.
func sentence(text string) string {
	if strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?") {
		return text
	}
	return text + "."
}

// TrustNotice states that the trust store changed.
func TrustNotice(packages, taps int) string {
	switch {
	case packages > 0 && taps > 0:
		return fmt.Sprintf("Trusted %d %s and %d %s to install the selected apps.", packages, plural(packages, "package", "packages"), taps, plural(taps, "tap", "taps"))
	case packages > 0:
		return fmt.Sprintf("Trusted %d %s to install %s.", packages, plural(packages, "package", "packages"), plural(packages, "it", "them"))
	case taps > 0:
		return fmt.Sprintf("Trusted %d %s to install the selected apps.", taps, plural(taps, "tap", "taps"))
	default:
		return ""
	}
}

func plural(count int, singular, multiple string) string {
	if count == 1 {
		return singular
	}
	return multiple
}
