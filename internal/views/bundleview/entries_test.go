package bundleview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/homebrew"
)

// The source a row names comes from the shape of its token: a Homebrew
// token that carries a "/" comes from a tap, one without comes from
// homebrew/core.
func TestContentsSourceFollowsTheTokenShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		item homebrew.BundleItem
		want string
	}{
		{"core formula", homebrew.BundleItem{Name: "gh", Kind: homebrew.BundleItemBrew}, SourceHomebrew},
		{"tap formula", homebrew.BundleItem{Name: "ublue-os/tap/linux-mcp-server", Kind: homebrew.BundleItemBrew}, SourceBrewTap},
		{"core cask", homebrew.BundleItem{Name: "visual-studio-code", Kind: homebrew.BundleItemCask}, SourceHomebrew},
		{"tap cask", homebrew.BundleItem{Name: "ublue-os/tap/goose-linux", Kind: homebrew.BundleItemCask}, SourceBrewTap},
		{"flatpak", homebrew.BundleItem{Name: "app.drey.Damask", Kind: homebrew.BundleItemFlatpak}, SourceFlathub},
		{"app store", homebrew.BundleItem{Name: "497799835", Kind: homebrew.BundleItemMas}, SourceAppStore},
		{"vs code", homebrew.BundleItem{Name: "ms-vscode-remote.remote-containers", Kind: homebrew.BundleItemVSCode}, SourceVSCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ContentsSource(tc.item); got != tc.want {
				t.Errorf("ContentsSource(%+v) = %q, want %q", tc.item, got, tc.want)
			}
		})
	}
}

// The label of an entry that is not on the system says where it comes
// from; the label of one that is says where it is installed from.
func TestSourceAndInstalledWording(t *testing.T) {
	if got := SourceLabel(SourceHomebrew); got != "from Homebrew" {
		t.Errorf("SourceLabel(Homebrew) = %q, want %q", got, "from Homebrew")
	}
	if got := InstalledSource(SourceBrewTap); got != "Installed from Brew Tap" {
		t.Errorf("InstalledSource(Brew Tap) = %q, want %q", got, "Installed from Brew Tap")
	}
	if got := InstalledSource(SourceFlathub); got != "Installed from Flathub" {
		t.Errorf("InstalledSource(Flathub) = %q, want %q", got, "Installed from Flathub")
	}
	if got := InstalledSource(""); got != "" {
		t.Errorf("InstalledSource(\"\") = %q, want empty", got)
	}
}

// The observed source refines the static shape: a package from a tap reads
// as a tap package even when its token names none.
func TestObservedSourceRefinesTheStaticShape(t *testing.T) {
	if got := ObservedSource(homebrew.BundleItemBrew, "homebrew/core"); got != SourceHomebrew {
		t.Errorf("ObservedSource(core) = %q, want %q", got, SourceHomebrew)
	}
	if got := ObservedSource(homebrew.BundleItemCask, "ublue-os/tap"); got != SourceBrewTap {
		t.Errorf("ObservedSource(tap) = %q, want %q", got, SourceBrewTap)
	}
	if got := ObservedSource(homebrew.BundleItemFlatpak, "flathub"); got != SourceFlathub {
		t.Errorf("ObservedSource(flathub) = %q, want %q", got, SourceFlathub)
	}
	// An observation the vocabulary has no word for falls back to the
	// static shape rather than inventing a label.
	if got := ObservedSource(homebrew.BundleItemFlatpak, "mixtapes"); got != "" {
		t.Errorf("ObservedSource(mixtapes) = %q, want no source", got)
	}
	if got := ObservedSource(homebrew.BundleItemBrew, ""); got != "" {
		t.Errorf("ObservedSource(empty) = %q, want no source", got)
	}
}

// An installed entry says where it is installed from; an entry that is not
// says where it comes from.
func TestPresentEntryNamesTheSource(t *testing.T) {
	item := homebrew.BundleItem{Name: "ublue-os/tap/goose-linux", Kind: homebrew.BundleItemCask}

	pending := PresentEntry(EntryState{Item: item})
	if pending.Label != "from Brew Tap" {
		t.Errorf("label = %q, want %q", pending.Label, "from Brew Tap")
	}
	if pending.Installed {
		t.Error("an entry that is not installed must not read as installed")
	}
	if pending.Name != item.Name {
		t.Errorf("name = %q, want the raw token %q", pending.Name, item.Name)
	}
	if pending.AccessibleName != "Select ublue-os/tap/goose-linux" {
		t.Errorf("accessible name = %q, want the token", pending.AccessibleName)
	}
	if !pending.Selectable {
		t.Error("a cask must be selectable")
	}

	installed := PresentEntry(EntryState{Item: item, Installed: true, Source: "ublue-os/tap"})
	if installed.Label != "Installed from Brew Tap" {
		t.Errorf("label = %q, want %q", installed.Label, "Installed from Brew Tap")
	}
	if !installed.Installed {
		t.Error("an installed entry must read as installed")
	}
}

// A core package whose token names no tap still reads as a Homebrew
// package, and an installed one reads as installed from Homebrew.
func TestPresentEntryNamesACorePackage(t *testing.T) {
	item := homebrew.BundleItem{Name: "gh", Kind: homebrew.BundleItemBrew}
	installed := PresentEntry(EntryState{Item: item, Installed: true, Source: "homebrew/core"})
	if installed.Label != "Installed from Homebrew" {
		t.Errorf("label = %q, want %q", installed.Label, "Installed from Homebrew")
	}
}

// An entry ChairLift cannot install on its own is never offered for
// selection: a checkbox that promised an install that cannot happen would
// be worse than no checkbox.
func TestOnlyInstallableKindsAreSelectable(t *testing.T) {
	for kind, want := range map[homebrew.BundleItemKind]bool{
		homebrew.BundleItemBrew:    true,
		homebrew.BundleItemCask:    true,
		homebrew.BundleItemFlatpak: true,
		homebrew.BundleItemMas:     false,
		homebrew.BundleItemVSCode:  false,
	} {
		if got := Selectable(kind); got != want {
			t.Errorf("Selectable(%q) = %v, want %v", kind, got, want)
		}
	}
}

// "Select All" is one-way: it is insensitive once every entry it can
// select is selected.
func TestSelectAllEnabledTracksTheUncheckedEntries(t *testing.T) {
	if !SelectAllEnabled([]Selection{{Selectable: true, Checked: false}}) {
		t.Error("Select All must be enabled while an entry is unchecked")
	}
	if SelectAllEnabled([]Selection{{Selectable: true, Checked: true}}) {
		t.Error("Select All must be disabled once every entry is checked")
	}
	if SelectAllEnabled(nil) {
		t.Error("Select All must be disabled for a collection with no entries")
	}
	// An entry that cannot be selected never becomes checked, so it must
	// not keep the button alive.
	if SelectAllEnabled([]Selection{{Selectable: false, Checked: false}, {Selectable: true, Checked: true}}) {
		t.Error("Select All must be disabled once every selectable entry is checked")
	}
}

// The collection's subtitle says how much of it is already on the system.
func TestDescribeEntriesCountsTheInstalledEntries(t *testing.T) {
	none := DescribeEntries("cli", "", 16, 0)
	if !strings.HasSuffix(none.Subtitle, "Includes 16 apps and tools.") {
		t.Errorf("subtitle = %q, want the plain count", none.Subtitle)
	}
	if strings.Contains(none.Subtitle, "installed") {
		t.Errorf("subtitle = %q, want nothing about installed entries", none.Subtitle)
	}

	all := DescribeEntries("cli", "", 16, 16)
	if !strings.HasSuffix(all.Subtitle, "Includes 16 apps and tools. (Installed)") {
		t.Errorf("subtitle = %q, want the installed marker", all.Subtitle)
	}

	some := DescribeEntries("cli", "", 16, 2)
	if !strings.HasSuffix(some.Subtitle, "Includes 16 apps and tools. 2 of 16 installed.") {
		t.Errorf("subtitle = %q, want the partial count", some.Subtitle)
	}

	empty := DescribeEntries("cli", "", 0, 0)
	if strings.Contains(empty.Subtitle, "installed") {
		t.Errorf("subtitle = %q, want nothing about installed entries", empty.Subtitle)
	}
}

// A collection selected as a whole installs through `brew bundle`, which
// keeps the collection's own tap and trust declarations in force.
func TestPlanInstallUsesTheBundleForAFullySelectedCollection(t *testing.T) {
	entries := []EntryState{
		{Item: homebrew.BundleItem{Name: "gh", Kind: homebrew.BundleItemBrew}, Checked: true},
		{Item: homebrew.BundleItem{Name: "jq", Kind: homebrew.BundleItemBrew}, Checked: true},
	}
	plan := PlanInstall(entries, []string{"/b/cli.Brewfile"})
	if len(plan.BundlePaths) != 1 || plan.BundlePaths[0] != "/b/cli.Brewfile" {
		t.Errorf("plan = %+v, want the bundle path", plan)
	}
	if len(plan.Formulae) != 0 {
		t.Errorf("plan = %+v, want no per-item installs", plan)
	}
}

// A partially selected collection installs the checked entries one by one,
// batched per package manager.
func TestPlanInstallBatchesTheCheckedEntries(t *testing.T) {
	entries := []EntryState{
		{Item: homebrew.BundleItem{Name: "gh", Kind: homebrew.BundleItemBrew}, Checked: true},
		{Item: homebrew.BundleItem{Name: "jq", Kind: homebrew.BundleItemBrew}, Checked: false},
		{Item: homebrew.BundleItem{Name: "visual-studio-code", Kind: homebrew.BundleItemCask}, Checked: true},
		{Item: homebrew.BundleItem{Name: "app.drey.Damask", Kind: homebrew.BundleItemFlatpak}, Checked: true},
	}
	plan := PlanInstall(entries, []string{"/b/cli.Brewfile"})
	if len(plan.BundlePaths) != 0 {
		t.Errorf("plan = %+v, want no bundle path", plan)
	}
	if len(plan.Formulae) != 1 || plan.Formulae[0] != "gh" {
		t.Errorf("formulae = %v, want only the checked formula", plan.Formulae)
	}
	if len(plan.Casks) != 1 || plan.Casks[0] != "visual-studio-code" {
		t.Errorf("casks = %v, want only the checked cask", plan.Casks)
	}
	if len(plan.Flatpaks) != 1 || plan.Flatpaks[0] != "app.drey.Damask" {
		t.Errorf("flatpaks = %v, want only the checked flatpak", plan.Flatpaks)
	}
}

// An entry that is already on the system is never installed again: its
// checkbox is disabled, and installing it would only repeat work.
func TestPlanInstallSkipsInstalledEntries(t *testing.T) {
	entries := []EntryState{
		{Item: homebrew.BundleItem{Name: "gh", Kind: homebrew.BundleItemBrew}, Checked: true, Installed: true},
		{Item: homebrew.BundleItem{Name: "jq", Kind: homebrew.BundleItemBrew}, Checked: true},
		{Item: homebrew.BundleItem{Name: "ripgrep", Kind: homebrew.BundleItemBrew}, Checked: false},
	}
	plan := PlanInstall(entries, []string{"/b/cli.Brewfile"})
	if len(plan.Formulae) != 1 || plan.Formulae[0] != "jq" {
		t.Errorf("formulae = %v, want only the entry that is missing", plan.Formulae)
	}
	if plan.Pending != 1 {
		t.Errorf("Pending = %d, want the one entry that is missing", plan.Pending)
	}
}

// A collection whose every entry is checked installs as a whole, even when
// some of its entries are already on the system: `brew bundle` skips what
// is installed and keeps the collection's own declarations in force.
func TestPlanInstallUsesTheBundleWhenEveryEntryIsChecked(t *testing.T) {
	entries := []EntryState{
		{Item: homebrew.BundleItem{Name: "gh", Kind: homebrew.BundleItemBrew}, Checked: true, Installed: true},
		{Item: homebrew.BundleItem{Name: "jq", Kind: homebrew.BundleItemBrew}, Checked: true},
	}
	plan := PlanInstall(entries, []string{"/b/cli.Brewfile"})
	if len(plan.BundlePaths) != 1 {
		t.Errorf("plan = %+v, want the bundle path", plan)
	}
	// The count is what the run reports, so it must count the entries
	// the plan installs even on the bundle path.
	if plan.Pending != 1 {
		t.Errorf("Pending = %d, want the one entry that is missing", plan.Pending)
	}
}

// A collection whose entries are all installed asks for nothing.
func TestPlanInstallOfAFullyInstalledCollectionIsEmpty(t *testing.T) {
	entries := []EntryState{
		{Item: homebrew.BundleItem{Name: "gh", Kind: homebrew.BundleItemBrew}, Checked: true, Installed: true},
	}
	plan := PlanInstall(entries, []string{"/b/cli.Brewfile"})
	if !plan.Empty() {
		t.Errorf("plan = %+v, want nothing to install", plan)
	}
}

// An entry ChairLift cannot install on its own is never part of a plan.
func TestPlanInstallSkipsTheKindsItCannotInstall(t *testing.T) {
	entries := []EntryState{
		{Item: homebrew.BundleItem{Name: "497799835", Kind: homebrew.BundleItemMas}, Checked: true},
	}
	plan := PlanInstall(entries, []string{"/b/ide.Brewfile"})
	if !plan.Empty() {
		t.Errorf("plan = %+v, want nothing to install", plan)
	}
}

// The toast says what the trust store gained whenever it gained anything:
// trusting a source changes what this computer will load, so the change is
// never silent.
func TestToastStatesTheTrustChange(t *testing.T) {
	plain := InstallToast(InstallOutcome{Installed: 3})
	if !strings.Contains(plain, "3") {
		t.Errorf("toast = %q, want the count", plain)
	}
	if strings.Contains(plain, "Trusted") {
		t.Errorf("toast = %q, want nothing about trust", plain)
	}

	trusted := InstallToast(InstallOutcome{Installed: 3, TrustedPackages: 3})
	if !strings.Contains(trusted, "Trusted 3 packages") {
		t.Errorf("toast = %q, want the trust change", trusted)
	}

	tapTrusted := InstallToast(InstallOutcome{Installed: 1, TrustedTaps: 2})
	if !strings.Contains(tapTrusted, "Trusted 2 taps") {
		t.Errorf("toast = %q, want the trust change", tapTrusted)
	}

	both := InstallToast(InstallOutcome{Installed: 1, TrustedPackages: 1, TrustedTaps: 1})
	if !strings.Contains(both, "Trusted 1 packages and 1 taps") {
		t.Errorf("toast = %q, want both trust changes", both)
	}
}

// A failed run names what did not install, and still says what the trust
// store gained.
func TestToastNamesTheFailedCollections(t *testing.T) {
	toast := InstallToast(InstallOutcome{Failed: []string{"Agentic Tools"}, TrustedPackages: 1})
	if !strings.Contains(toast, "Agentic Tools") {
		t.Errorf("toast = %q, want the failed collection", toast)
	}
	if !strings.Contains(toast, "Trusted 1 packages") {
		t.Errorf("toast = %q, want the trust change", toast)
	}
}

// Nothing selected says so rather than claiming an install.
func TestNoSelectionToastNamesTheEmptySelection(t *testing.T) {
	if !strings.Contains(NoSelectionToast, "No apps selected") {
		t.Errorf("NoSelectionToast = %q, want it to name the empty selection", NoSelectionToast)
	}
	if !strings.Contains(PreviewToast, "DRY-RUN") {
		t.Errorf("PreviewToast = %q, want it to say it was a preview", PreviewToast)
	}
}
