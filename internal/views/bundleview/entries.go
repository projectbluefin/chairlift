package bundleview

import (
	"fmt"
	"strings"

	"github.com/projectbluefin/chairlift/internal/homebrew"
)

// The sources a collection entry can come from. The vocabulary is closed on
// purpose: a row's subtitle names where an entry comes from, and a person
// reading six collections should not have to learn a new source per
// collection.
const (
	// SourceHomebrew is a package from homebrew/core.
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
// A Homebrew token that carries a "/" names a package from a tap; one
// without names a homebrew/core package. The token's shape is the only
// thing available before the package is installed, so it is what the row
// says about an entry that is not on the system yet.
func ContentsSource(item homebrew.BundleItem) string {
	switch item.Kind {
	case homebrew.BundleItemBrew, homebrew.BundleItemCask:
		if strings.Contains(item.Name, "/") {
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
		case homebrew.CoreTap:
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
// The App Store and VS Code Marketplace entries a Brewfile may carry are
// installed by `brew bundle`, not by ChairLift's own per-item commands, so
// they are never offered for selection: a checkbox that promised an
// install ChairLift cannot perform would be worse than no checkbox.
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
	return EntryPresentation{
		Name:           state.Item.Name,
		Label:          label,
		AccessibleName: EntryAccessibleName(state.Item.Name),
		Selectable:     Selectable(state.Item.Kind),
		Installed:      state.Installed,
	}
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

// AllSelected reports whether every selectable entry of a collection is
// checked.
func AllSelected(selections []Selection) bool {
	return !SelectAllEnabled(selections)
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
// A collection whose every selectable entry is checked installs as a whole
// through `brew bundle`, which is what keeps the collection's own tap and
// trust declarations in force. A partially selected collection installs
// the checked entries one by one, batched per package manager, because
// `brew bundle` can only install a whole file.
//
// An entry that is already on the system is never part of the plan: its
// checkbox is disabled, and installing it again would only repeat work.
func PlanInstall(entries []EntryState, memberPaths []string) InstallPlan {
	selections := make([]Selection, 0, len(entries))
	var pending []EntryState
	for _, entry := range entries {
		selectable := Selectable(entry.Item.Kind)
		selections = append(selections, Selection{Selectable: selectable, Checked: entry.Checked})
		if selectable && entry.Checked && !entry.Installed {
			pending = append(pending, entry)
		}
	}
	if len(pending) == 0 {
		return InstallPlan{}
	}
	if AllSelected(selections) {
		if len(memberPaths) == 0 {
			return InstallPlan{}
		}
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
	// Failed names the collections whose install did not finish.
	Failed []string
	// TrustedPackages counts the packages trusted one by one.
	TrustedPackages int
	// TrustedTaps counts the taps trusted wholesale.
	TrustedTaps int
}

// InstallToast returns the toast text for a finished install run.
//
// The toast says what the trust store gained whenever it gained anything:
// trusting a source changes what this computer will load from it, so the
// change is never silent.
func InstallToast(outcome InstallOutcome) string {
	var text string
	if len(outcome.Failed) > 0 {
		text = fmt.Sprintf("Couldn't install all selected apps (%s). Try again.", strings.Join(outcome.Failed, ", "))
	} else {
		text = fmt.Sprintf("Installed %d apps and tools.", outcome.Installed)
	}
	if notice := TrustNotice(outcome.TrustedPackages, outcome.TrustedTaps); notice != "" {
		text += " " + notice
	}
	return text
}

// TrustNotice states that the trust store changed.
func TrustNotice(packages, taps int) string {
	switch {
	case packages > 0 && taps > 0:
		return fmt.Sprintf("Trusted %d packages and %d taps to install them.", packages, taps)
	case packages > 0:
		return fmt.Sprintf("Trusted %d packages to install them.", packages)
	case taps > 0:
		return fmt.Sprintf("Trusted %d taps to install them.", taps)
	default:
		return ""
	}
}
