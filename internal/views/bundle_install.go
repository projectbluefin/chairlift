package views

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/trustmsg"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// bundleItemRow tracks one collection row in Apps or Developer Mode.
type bundleItemRow struct {
	bundle        homebrew.Bundle
	memberPaths   []string
	total         int
	row           *adw.ExpanderRow
	title         string
	selectAll     *gtk.Button
	entries       []*bundleEntryRow
	wholeFileSafe bool // every entry was read and offered on the page
}

// bundleEntryRow tracks one entry row inside a collection. The checkbox is
// the only state a person changes; the installed state is observed.
type bundleEntryRow struct {
	item              homebrew.BundleItem
	row               *adw.ActionRow
	check             *gtk.CheckButton
	installed         bool
	selectable        bool
	source            string
	descriptionLoaded bool
}

func (entry *bundleEntryRow) renderSubtitle() {
	presentation := bundleview.PresentEntry(bundleview.EntryState{
		Item: entry.item, Installed: entry.installed, Source: entry.source,
		DescriptionLoaded: entry.descriptionLoaded,
	})
	entry.row.SetSubtitle(presentation.Label)
}

type bundleSelection struct {
	items    []*bundleItemRow
	button   *gtk.Button
	progress *gtk.ProgressBar
}

// collectionsAvailable retains the collections' configuration and Homebrew
// capability floor for both Apps and Developer Mode.
func (uh *UserHome) collectionsAvailable() bool {
	return uh.groupEnabled("applications_page", "brew_bundles_group")
}

// packageInventory indexes installed Homebrew packages under the two names
// a collection entry may use: the bare name and the qualified name.
//
// A bare token names the homebrew/core package of that name, because that is
// what `brew install <name>` would install; a qualified token names one
// package exactly.
type packageInventory struct {
	byName map[string]homebrew.Package
	byFull map[string]homebrew.Package
}

func newPackageInventory(packages []homebrew.Package) packageInventory {
	inventory := packageInventory{
		byName: make(map[string]homebrew.Package, len(packages)),
		byFull: make(map[string]homebrew.Package, len(packages)),
	}
	for _, pkg := range packages {
		name := strings.ToLower(pkg.Name)
		if existing, ok := inventory.byName[name]; !ok || (existing.FromTap() && !pkg.FromTap()) {
			// A bare name prefers the homebrew/core package: it is the
			// one a bare Brewfile entry installs.
			inventory.byName[name] = pkg
		}
		if full := strings.ToLower(pkg.QualifiedName()); full != "" {
			inventory.byFull[full] = pkg
		}
	}
	return inventory
}

// lookup finds the installed package a collection entry names.
func (i packageInventory) lookup(token string) (homebrew.Package, bool) {
	if strings.Contains(token, "/") {
		pkg, ok := i.byFull[strings.ToLower(token)]
		return pkg, ok
	}
	pkg, ok := i.byName[strings.ToLower(token)]
	return pkg, ok
}

// bundleInventory is what the Apps page observed about the packages a
// collection's entries are matched against.
type bundleInventory struct {
	formulae packageInventory
	casks    packageInventory
	flatpaks map[string]string // application id -> remote it came from
}

// readBundleInventory reads the installed packages the collections' entries
// are matched against.
//
// It is its own read, separate from the installed-package lists the page
// shows, so the two paths stay independent: a failure here leaves those
// lists alone, and a change in one does not have to invalidate the other.
func readBundleInventory() bundleInventory {
	var inventory bundleInventory
	if formulae, err := homebrew.ListInstalledFormulae(); err != nil {
		log.Printf("Error listing installed Homebrew formulae for the app collections: %v", err)
	} else {
		inventory.formulae = newPackageInventory(formulae)
	}
	if casks, err := homebrew.ListInstalledCasks(); err != nil {
		log.Printf("Error listing installed Homebrew casks for the app collections: %v", err)
	} else {
		inventory.casks = newPackageInventory(casks)
	}
	if origins, err := flatpak.ListInstalledOrigins(); err != nil {
		log.Printf("Error listing installed Flatpak applications for the app collections: %v", err)
	} else {
		inventory.flatpaks = origins
	}
	return inventory
}

// lookup reports installed state and provenance only when that manager's
// inventory read succeeded. A failed read must not release installed entries.
func (i bundleInventory) lookup(item homebrew.BundleItem) (installed bool, source string, known bool) {
	switch item.Kind {
	case homebrew.BundleItemBrew:
		if i.formulae.byName == nil {
			return false, "", false
		}
		pkg, ok := i.formulae.lookup(item.Name)
		if !ok {
			return false, "", true
		}
		return true, pkg.Tap, true
	case homebrew.BundleItemCask:
		if i.casks.byName == nil {
			return false, "", false
		}
		pkg, ok := i.casks.lookup(item.Name)
		if !ok {
			return false, "", true
		}
		return true, pkg.Tap, true
	case homebrew.BundleItemFlatpak:
		if i.flatpaks == nil {
			return false, "", false
		}
		origin, ok := i.flatpaks[item.Name]
		if !ok {
			return false, "", true
		}
		return true, origin, true
	default:
		return false, "", true
	}
}

// refreshBundleStatuses observes, off the GTK thread, which of each
// collection's entries are already on the system.
//
// An installed entry's checkbox is checked and insensitive, and its row
// says where the package is installed from. An entry that is not installed
// keeps an empty, sensitive checkbox, because an install is opt-in entry by
// entry.
func (uh *UserHome) refreshBundleStatuses() {
	if uh == nil || len(uh.bundleItems) == 0 {
		return
	}

	generation := uh.bundleStatusRefresh.Begin()

	go func() {
		inventory := readBundleInventory()
		sgtk.RunOnMainThread(func() {
			if !uh.bundleStatusRefresh.IsCurrent(generation) {
				return
			}
			uh.applyBundleEntryStates(inventory)
		})
	}()
}

// applyBundleEntryStates renders the observed entry states onto the page.
func (uh *UserHome) applyBundleEntryStates(inventory bundleInventory) {
	for _, item := range uh.bundleItems {
		installedCount := 0
		for _, entry := range item.entries {
			if installed, source, known := inventory.lookup(entry.item); known {
				wasInstalled := entry.installed
				entry.installed = installed
				entry.source = source
				entry.renderSubtitle()
				// Observed installations are not deliberate reinstall selections.
				entry.check.SetActive(bundleview.RefreshChecked(entry.check.GetActive(), wasInstalled, installed))
				entry.check.SetSensitive(!installed && entry.selectable)
			}
			if entry.installed {
				installedCount++
			}
		}
		item.row.SetSubtitle(
			bundleview.DescribeEntries(item.bundle.Name, item.bundle.Description, item.total, installedCount).Subtitle,
		)
		uh.updateBundleSelectionState(item)
	}
}

// updateBundleSelectionState keeps a collection's "Select All" button in
// step with its entries.
//
// It reads the widgets rather than a copy of the selection, so a
// programmatic change — Select All itself, or the refresh that marks an
// installed entry — needs no separate bookkeeping, and the handler stays
// idempotent when it is re-entered by one.
func (uh *UserHome) updateBundleSelectionState(item *bundleItemRow) {
	if item.selectAll == nil {
		return
	}
	selections := make([]bundleview.Selection, 0, len(item.entries))
	for _, entry := range item.entries {
		selections = append(selections, bundleview.Selection{
			Selectable: entry.selectable,
			Checked:    entry.check.GetActive(),
		})
	}
	item.selectAll.SetSensitive(bundleview.SelectAllEnabled(selections))
}

// selectAllBundleEntries checks every entry of a collection that can be
// checked.
//
// The button is one-way: it selects and never deselects, so a person who
// wants one entry back unchecks that entry themselves.
func (uh *UserHome) selectAllBundleEntries(item *bundleItemRow) {
	for _, entry := range item.entries {
		if !entry.selectable || entry.check.GetActive() {
			continue
		}
		entry.check.SetActive(true)
	}
	uh.updateBundleSelectionState(item)
}

// bundleInstallPlan is one collection's share of an install run.
type bundleInstallPlan struct {
	item *bundleItemRow
	plan bundleview.InstallPlan
	// pending counts the entries the plan installs, so the run can say
	// how many apps and tools it added.
	pending int
}

// planBundleInstalls derives what the checked entries of every collection
// install. It reads the checkboxes, so it runs on the GTK main thread.
func (uh *UserHome) planBundleInstalls(selection *bundleSelection) []bundleInstallPlan {
	var plans []bundleInstallPlan
	for _, item := range selection.items {
		entries := make([]bundleview.EntryState, 0, len(item.entries))
		for _, entry := range item.entries {
			entries = append(entries, bundleview.EntryState{
				Item:      entry.item,
				Installed: entry.installed,
				Checked:   entry.check.GetActive(),
			})
		}
		paths := item.memberPaths
		if !item.wholeFileSafe || len(entries) != item.total {
			paths = nil
		}
		plan := bundleview.PlanInstall(entries, paths)
		if plan.Empty() {
			continue
		}
		plans = append(plans, bundleInstallPlan{
			item:    item,
			plan:    plan,
			pending: plan.Pending,
		})
	}
	return plans
}

// runSelectedBundlesInstall installs only this destination's checked entries.
// Fully selected collections retain their Brewfile's tap/trust declarations;
// partial selections use batched commands for only the checked entries.
func (uh *UserHome) runSelectedBundlesInstall(selection *bundleSelection) {
	if !uh.bundleInstallGate.TryStart() {
		return
	}

	plans := uh.planBundleInstalls(selection)
	if len(plans) == 0 {
		uh.bundleInstallGate.Reset()
		if uh.toastAdder != nil {
			uh.toastAdder.ShowToast(bundleview.NoSelectionToast)
		}
		return
	}
	uh.setBundleInstallProgress(selection, true, 0, len(plans), plans[0].item.title)
	go func() {
		outcome := bundleview.InstallOutcome{}
		var trustErrCollection string

		for idx, plan := range plans {
			title := plan.item.title
			if idx > 0 {
				sgtk.RunOnMainThread(func() {
					uh.setBundleInstallProgress(selection, true, idx, len(plans), title)
				})
			}
			var err error
			if len(plan.plan.BundlePaths) > 0 {
				err = installBundleWhole(plan.plan.BundlePaths)
			} else {
				err = installBundleEntries(plan.item, plan.plan, &outcome)
			}
			if err != nil {
				log.Printf("Error installing app collection %q: %v", plan.item.bundle.Name, err)
				var untrusted *homebrew.UntrustedTapError
				if errors.As(err, &untrusted) {
					trustErrCollection = title
				}
				outcome.Failed = append(outcome.Failed, title)
				continue
			}
			outcome.Installed += plan.pending
		}

		sgtk.RunOnMainThread(func() {
			defer uh.bundleInstallGate.Reset()
			uh.setBundleInstallProgress(selection, false, len(plans), len(plans), "")
			if dryrun.Enabled() {
				if uh.toastAdder != nil {
					uh.toastAdder.ShowToast(bundleview.PreviewToast)
				}
				return
			}

			uh.homebrewInventoryChanged()

			if uh.toastAdder == nil {
				return
			}
			if trustErrCollection != "" {
				toast := trustmsg.BundleMessage(trustErrCollection)
				// The trust store may still have changed on the way
				// to the failure; say so rather than leaving it
				// silent.
				if notice := bundleview.TrustNotice(outcome.TrustedPackages, outcome.TrustedTaps); notice != "" {
					toast += " " + notice
				}
				uh.toastAdder.ShowErrorToast(toast)
			} else if len(outcome.Failed) == 0 {
				uh.toastAdder.ShowToast(bundleview.InstallToast(outcome))
			} else {
				uh.toastAdder.ShowErrorToast(bundleview.InstallToast(outcome))
			}
		})
	}()
}

// installBundleWhole installs a collection as a whole through `brew bundle`,
// which keeps the tap directives and the trust flags the collection's own
// Brewfile carries.
func installBundleWhole(paths []string) error {
	for _, path := range paths {
		if err := homebrew.BundleInstall(path); err != nil {
			return err
		}
	}
	return nil
}

// installBundleEntries installs a partially selected collection's entries.
//
// Each tap-qualified Homebrew entry is prepared first: its tap is cloned
// when it is missing, and its source is trusted when nothing trusts it
// yet. The trust store's own rules decide the width — one package, or the
// whole tap when the collection's Brewfile declares the tap trusted.
//
// The outcome carries what the trust store gained, so the run can say so
// rather than escalating silently.
func installBundleEntries(item *bundleItemRow, plan bundleview.InstallPlan, outcome *bundleview.InstallOutcome) error {
	store, err := homebrew.ReadTrustStore()
	if err != nil {
		log.Printf("Could not read the Homebrew trust store: %v", err)
	}
	declared := declaredTrustedTaps(item.memberPaths)

	for _, token := range plan.Formulae {
		if err := prepareBundleEntry(token, homebrew.Formula, &store, declared, outcome); err != nil {
			return err
		}
	}
	for _, token := range plan.Casks {
		if err := prepareBundleEntry(token, homebrew.Cask, &store, declared, outcome); err != nil {
			return err
		}
	}
	if err := homebrew.InstallMany(plan.Formulae, false); err != nil {
		return err
	}
	if err := homebrew.InstallMany(plan.Casks, true); err != nil {
		return err
	}
	return flatpak.InstallMany(plan.Flatpaks)
}

// prepareBundleEntry makes one tap-qualified entry installable and records
// what the trust store gained.
func prepareBundleEntry(
	token string,
	kind homebrew.PackageKind,
	store *homebrew.TrustStore,
	declared map[string]bool,
	outcome *bundleview.InstallOutcome,
) error {
	change, err := homebrew.PrepareInstall(token, kind, store, declared)
	if err != nil {
		return err
	}
	switch change {
	case homebrew.TrustChangedPackage:
		outcome.TrustedPackages++
	case homebrew.TrustChangedTap:
		outcome.TrustedTaps++
	}
	return nil
}

// declaredTrustedTaps reads the taps a collection's own Brewfiles mark
// `trusted: true`. Those taps the distribution declared safe, and
// `brew bundle` trusts them wholesale, so the per-item path does the same.
func declaredTrustedTaps(paths []string) map[string]bool {
	declared := map[string]bool{}
	for _, path := range paths {
		taps, err := homebrew.BundleTaps(path)
		if err != nil {
			log.Printf("Could not read the tap declarations of %q: %v", path, err)
			continue
		}
		for _, tap := range taps {
			if tap.Trusted {
				declared[tap.Name] = true
			}
		}
	}
	return declared
}

func (uh *UserHome) setBundleInstallProgress(selection *bundleSelection, busy bool, current, total int, currentName string) {
	for _, controls := range uh.bundleSelections {
		controls.button.SetSensitive(!busy)
	}
	if busy {
		text := fmt.Sprintf("Installing %s (%d of %d)…", currentName, current+1, total)
		if currentName == "" {
			text = fmt.Sprintf("Installing (%d of %d)…", current+1, total)
		}
		selection.progress.SetText(text)
		uh.appInstallProgress.start(selection.progress)
	} else {
		uh.appInstallProgress.stop(selection.progress)
	}
}
