package views

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// buildApplicationsPage builds the Applications page content
func (uh *UserHome) buildApplicationsPage() {
	page := uh.applicationsPrefsPage
	if page == nil {
		return
	}
	destroyed := func(_ gtk.Widget) {
		uh.appInstallProgress.dispose()
	}
	page.ConnectDestroy(&destroyed)

	// Collections are the first task; installed packages and export follow.
	if uh.groupEnabled("applications_page", "brew_bundles_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("App Collections")
		group.SetDescription("Looking for app collections…")
		page.Add(group)
		uh.brewBundlesGroup = group

		var bundlePaths []string
		groupCfg := uh.config.GetGroupConfig("applications_page", "brew_bundles_group")
		if groupCfg != nil {
			bundlePaths = append(bundlePaths, groupCfg.BundlesPaths...)
		}
		go uh.loadBrewBundles(bundlePaths)
	}
	// Homebrew group
	if uh.groupEnabled("applications_page", "brew_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("Backup")

		// Package-list export row
		dumpRow := adw.NewActionRow()
		dumpRow.SetTitle("Export Package List")
		dumpRow.SetSubtitle(pageview.PackageListExportSubtitle)
		dumpSpinner := newActivitySpinner()
		dumpRow.AddSuffix(&dumpSpinner.Widget)

		dumpBtn := gtk.NewButtonWithLabel("Export")
		dumpBtn.SetValign(gtk.AlignCenterValue)
		dumpGate := &actionstate.Gate{}
		dumpClickedCb := func(btn gtk.Button) {
			uh.onBrewBundleDumpClicked(dumpBtn, dumpSpinner, dumpGate)
		}
		dumpBtn.ConnectClicked(&dumpClickedCb)

		dumpRow.AddSuffix(&dumpBtn.Widget)
		group.Add(&dumpRow.Widget)

		// Command line tools — Homebrew formulae
		uh.installedFormulae = adw.NewPreferencesGroup()
		uh.installedFormulae.SetTitle("Command-Line Tools")
		uh.installedFormulae.SetDescription("Counting…")

		// Applications — Homebrew casks
		uh.installedCasks = adw.NewPreferencesGroup()
		uh.installedCasks.SetTitle("Installed Apps")
		uh.installedCasks.SetDescription("Counting…")
		page.Add(uh.installedCasks)
		page.Add(uh.installedFormulae)
		page.Add(group)

		// Load packages asynchronously
		go uh.loadHomebrewPackages()
	}

}

// loadBrewBundles discovers configured collections off the GTK thread and
// builds their rows on the main thread.
// Once the rows exist, refreshBundleStatuses observes which collections the
// system already holds.
func (uh *UserHome) loadBrewBundles(paths []string) {
	rawBundles, discoveryErr := homebrew.AvailableBundles(paths)
	warning := ""
	if discoveryErr != nil {
		log.Printf("Error discovering app collections: %v", discoveryErr)
		warning = discoveryErr.Error()
	}

	// Index discovered bundles by name so super-bundles can resolve their members.
	bundlesByName := make(map[string]homebrew.Bundle, len(rawBundles))
	for _, b := range rawBundles {
		if !bundleview.WallpaperCollection(b.Name) {
			bundlesByName[b.Name] = b
		}
	}

	// Filter out retired bundles and members folded into a discovered parent;
	// keep curated and custom user collections.
	var bundles []homebrew.Bundle
	for _, b := range rawBundles {
		if !bundleview.WallpaperCollection(b.Name) && bundleview.Visible(b.Name, bundlesByName) {
			bundles = append(bundles, b)
		}
	}

	// Sort bundles: curated in priority order, then custom by title.
	sort.SliceStable(bundles, func(i, j int) bool {
		orderI := bundleview.Order(bundles[i].Name)
		orderJ := bundleview.Order(bundles[j].Name)
		if orderI != orderJ {
			return orderI < orderJ
		}
		return bundles[i].Name < bundles[j].Name
	})

	presentation := bundleview.Present(len(bundles), warning)

	// Parse every Brewfile off the main thread before marshalling: the
	// discovery path already runs off-thread, BundleContents is a buffered
	// file scan, and the contract documented on the function above
	// promises off-thread discovery + main-thread row construction only.
	type parsed struct {
		bundle      homebrew.Bundle
		memberPaths []string
		items       []homebrew.BundleItem
		total       int
	}
	parsedBundles := make([]parsed, 0, len(bundles))
	for _, bundle := range bundles {
		items, itemsErr := homebrew.BundleContents(bundle.Path)
		if itemsErr != nil {
			// The Brewfile moved or became unreadable between discovery
			// and listing. Keep whatever was read before the failure and
			// make the total match it, so the presenter neither claims a
			// "Showing N of M" overflow nor blames the display limit.
			log.Printf("Could not list contents of app collection %q: %v", bundle.Name, itemsErr)
		}
		total := bundle.ItemCount
		if itemsErr != nil {
			total = len(items)
		}

		memberPaths := []string{bundle.Path}
		// Include items from sibling member bundles (e.g. wallpaper-slideshow for artwork)
		for _, member := range bundleview.MemberBundles(bundle.Name, bundlesByName) {
			memberPaths = append(memberPaths, member.Path)
			if mItems, mErr := homebrew.BundleContents(member.Path); mErr == nil {
				items = append(items, mItems...)
				total += member.ItemCount
			}
		}

		parsedBundles = append(parsedBundles, parsed{
			bundle:      bundle,
			memberPaths: memberPaths,
			items:       items,
			total:       total,
		})
	}

	sgtk.RunOnMainThread(func() {
		if uh.brewBundlesGroup == nil {
			return
		}
		uh.brewBundlesGroup.SetDescription(presentation.Description)

		if len(parsedBundles) == 0 {
			row := adw.NewActionRow()
			row.SetUseMarkup(false)
			row.SetTitle(presentation.PlaceholderTitle)
			row.SetSubtitle(presentation.PlaceholderSubtitle)
			uh.brewBundlesGroup.Add(&row.Widget)
			return
		}

		// Add group-level Install button header row
		headerRow := adw.NewActionRow()
		headerRow.SetTitle("Install Selected Collections")
		headerRow.SetSubtitle("Installs all checked collections in one step.")
		headerInstallBtn := gtk.NewButtonWithLabel("Install")
		headerInstallBtn.SetValign(gtk.AlignCenterValue)
		headerInstallBtn.AddCssClass("suggested-action")
		uh.bundleInstallButton = headerInstallBtn

		progress := newInstallProgress()
		uh.bundleInstallBar = progress
		headerRow.AddSuffix(&progress.Widget)
		headerRow.AddSuffix(&headerInstallBtn.Widget)

		uh.bundleButtons.connect(headerInstallBtn, func(gtk.Button) {
			uh.runSelectedBundlesInstall()
		})
		uh.brewBundlesGroup.Add(&headerRow.Widget)

		uh.bundleItems = nil
		for _, p := range parsedBundles {
			row, choice := newBundleRow(p.bundle, p.items, p.total)
			uh.brewBundlesGroup.Add(&row.Widget)
			uh.bundleItems = append(uh.bundleItems, &bundleItemRow{
				bundle:      p.bundle,
				memberPaths: p.memberPaths,
				total:       p.total,
				row:         row,
				choice:      choice,
			})
		}
		uh.refreshBundleStatuses()
	})
}

// newBundleRow builds one collection expander row with a CheckButton prefix.
// total is the merged item count across the row's super-bundle members, the
// same count refreshBundleStatuses describes the row with.
func newBundleRow(bundle homebrew.Bundle, items []homebrew.BundleItem, total int) (*adw.ExpanderRow, *gtk.CheckButton) {
	collection := bundleview.Describe(bundle.Name, bundle.Description, total)
	row := adw.NewExpanderRow()
	// Collection titles and descriptions may carry free-form text from a
	// shipped Brewfile (comment lines, the humanize() fallback for unknown
	// collections); AdwExpanderRow parses both as Pango markup by default,
	// so a '&' or '<' would garble the row with a GTK warning (#437).
	row.SetUseMarkup(false)
	row.SetTitle(collection.Title)
	row.SetSubtitle(collection.Subtitle)
	row.SetTitleLines(1)

	choice := gtk.NewCheckButton()
	choice.SetValign(gtk.AlignCenterValue)
	choice.SetActive(bundleview.DefaultSelected(bundle.Name))
	SetAccessibleLabel(choice, "Select "+collection.Title)
	row.AddPrefix(&choice.Widget)

	addContentsRows(row, items, total)
	return row, choice
}

// addContentsRows fills the expander with one row per package the
// collection would install. The expander starts collapsed (AdwExpanderRow's
// default); the Install button stays in view on the expander's suffix
// whether or not the row is open, so a person who never expands the row
// still has the affordance they had before. A row whose parser returned
// nothing renders the empty placeholder so the expanded view still has
// something to read. total is the Brewfile's own entry count, which can
// exceed the items slice when the parser hit its own cap; passing it lets
// the presenter show an honest "showing N of M" notice instead of
// pretending the truncated tail is the whole file.
func addContentsRows(expander *adw.ExpanderRow, items []homebrew.BundleItem, total int) {
	presentation := bundleview.PresentContents(items, total)
	if !presentation.HasRows() {
		placeholder := adw.NewActionRow()
		placeholder.SetTitle(presentation.EmptyTitle)
		if presentation.EmptySubtitle != "" {
			placeholder.SetSubtitle(presentation.EmptySubtitle)
		}
		expander.AddRow(&placeholder.Widget)
		return
	}
	entries := make([]*adw.ActionRow, 0, len(presentation.Rows))
	for _, item := range presentation.Rows {
		entry := adw.NewActionRow()
		// AdwActionRow parses title/subtitle as Pango markup by default;
		// the Brewfile token may contain a free-form identifier or '&'/'<'
		// that would otherwise render as a GTK warning (#437).
		entry.SetUseMarkup(false)
		entry.SetTitle(item.Name)
		if item.KindLabel != "" {
			entry.SetSubtitle(item.KindLabel)
		}
		SetAccessibleLabel(entry, item.AccessibleName)
		expander.AddRow(&entry.Widget)
		entries = append(entries, entry)
	}
	bindBundleDescriptions(expander, entries, items)
	if presentation.OverflowNotice != "" {
		overflow := adw.NewActionRow()
		overflow.SetUseMarkup(false)
		overflow.SetTitle(presentation.OverflowNotice)
		overflow.SetTitleLines(1)
		expander.AddRow(&overflow.Widget)
	}
}

// homebrewInventoryChanged re-reads what a Homebrew install anywhere in
// ChairLift can change on the Apps page: the installed lists and which
// collections the system already holds. It must be called on the GTK main
// thread and is safe when either Apps group is disabled, because both
// readers guard their own widgets.
func (uh *UserHome) homebrewInventoryChanged() {
	if uh.wallpaperRefresh != nil {
		uh.wallpaperRefresh()
	}
	go uh.loadHomebrewPackages()
	uh.refreshBundleStatuses()
}

// loadHomebrewPackages loads installed Homebrew packages asynchronously
func (uh *UserHome) loadHomebrewPackages() {
	generation := uh.brewPackagesRefresh.Begin()

	// Load formulae
	if uh.installedFormulae != nil {
		formulae, err := homebrew.ListInstalledFormulae()
		if err != nil {
			// The command's error text names files and taps; the row says
			// what happened and the log keeps the detail.
			log.Printf("Error listing installed Homebrew formulae: %v", err)
			sgtk.RunOnMainThread(func() {
				if !uh.brewPackagesRefresh.IsCurrent(generation) {
					return
				}
				uh.installedFormulae.SetDescription("Couldn't load this list.")
			})
		} else {
			// Dependencies are managed by Homebrew, not individual choices here.
			requested := formulae[:0]
			for _, pkg := range formulae {
				if pkg.InstalledOnRequest {
					requested = append(requested, pkg)
				}
			}
			formulae = requested
			sgtk.RunOnMainThread(func() {
				if !uh.brewPackagesRefresh.IsCurrent(generation) {
					return
				}
				// A running row action owns its row's controls; replacing
				// them now would hide its progress and admit a second
				// start. settleHomebrewRows reloads once it finishes.
				if uh.formulaGates.Defer() {
					return
				}
				uh.formulaGates.Rebuild()
				uh.formulaeRows.Clear(func(row *adw.ActionRow) {
					uh.installedFormulae.Remove(&row.Widget)
				})
				uh.formulaButtons.clear()
				uh.installedFormulae.SetDescription(fmt.Sprintf("%d installed", len(formulae)))
				for _, pkg := range formulae {
					pkg := pkg
					presentation := pageview.HomebrewPackage(pkg.Name, pkg.Version, pkg.Pinned)
					row := adw.NewActionRow()
					row.SetTitle(presentation.Title)
					row.SetSubtitle(presentation.Subtitle)

					pinLabel := "Pin…"
					pinTooltip := "Keep this version during updates"
					if pkg.Pinned {
						pinLabel = "Unpin…"
						pinTooltip = "Allow updates again"
					}
					pinBtn := gtk.NewButton()
					setPackageButtonLabel(pinBtn, pinLabel, pkg.Name)
					pinBtn.SetValign(gtk.AlignCenterValue)
					pinBtn.SetTooltipText(pinTooltip)

					uninstallBtn := gtk.NewButton()
					setPackageButtonLabel(uninstallBtn, "Uninstall…", pkg.Name)
					uninstallBtn.SetValign(gtk.AlignCenterValue)
					uninstallBtn.SetTooltipText("Remove this tool")

					gate := uh.formulaGates.New()
					controls := []*gtk.Button{pinBtn, uninstallBtn}
					uh.formulaButtons.connect(pinBtn, func(gtk.Button) {
						if !gate.TryStart() {
							return
						}
						uh.confirmHomebrewPin(pkg.Name, !pkg.Pinned, pinBtn, controls, gate)
					})
					uh.formulaButtons.connect(uninstallBtn, func(gtk.Button) {
						if !gate.TryStart() {
							return
						}
						uh.confirmHomebrewUninstall(pkg.Name, homebrew.Formula, uninstallBtn, controls, gate)
					})

					row.AddSuffix(&pinBtn.Widget)
					row.AddSuffix(&uninstallBtn.Widget)
					uh.installedFormulae.Add(&row.Widget)
					uh.formulaeRows.Add(row)
				}
			})
		}
	}

	// Load casks
	if uh.installedCasks != nil {
		casks, err := homebrew.ListInstalledCasks()
		if err != nil {
			log.Printf("Error listing installed Homebrew casks: %v", err)
			sgtk.RunOnMainThread(func() {
				if !uh.brewPackagesRefresh.IsCurrent(generation) {
					return
				}
				uh.installedCasks.SetDescription("Couldn't load this list.")
			})
		} else {
			sgtk.RunOnMainThread(func() {
				if !uh.brewPackagesRefresh.IsCurrent(generation) {
					return
				}
				if uh.caskGates.Defer() {
					return
				}
				uh.caskGates.Rebuild()
				uh.caskRows.Clear(func(row *adw.ActionRow) {
					uh.installedCasks.Remove(&row.Widget)
				})
				uh.caskButtons.clear()
				uh.installedCasks.SetDescription(fmt.Sprintf("%d installed", len(casks)))
				for _, pkg := range casks {
					pkg := pkg
					presentation := pageview.HomebrewPackage(pkg.Name, pkg.Version, false)
					row := adw.NewActionRow()
					row.SetTitle(presentation.Title)
					row.SetSubtitle(presentation.Subtitle)

					// The running application's own cask stays listed but
					// offers no Uninstall: removing it here would delete the
					// app from under the person using it (issue #493).
					if pageview.IsSelfCask(pkg.Name) {
						uh.installedCasks.Add(&row.Widget)
						uh.caskRows.Add(row)
						continue
					}

					uninstallBtn := gtk.NewButton()
					setPackageButtonLabel(uninstallBtn, "Uninstall…", pkg.Name)
					uninstallBtn.SetValign(gtk.AlignCenterValue)
					uninstallBtn.SetTooltipText("Remove this app")

					gate := uh.caskGates.New()
					controls := []*gtk.Button{uninstallBtn}
					uh.caskButtons.connect(uninstallBtn, func(gtk.Button) {
						if !gate.TryStart() {
							return
						}
						uh.confirmHomebrewUninstall(pkg.Name, homebrew.Cask, uninstallBtn, controls, gate)
					})

					row.AddSuffix(&uninstallBtn.Widget)
					uh.installedCasks.Add(&row.Widget)
					uh.caskRows.Add(row)
				}
			})
		}
	}
}

func (uh *UserHome) confirmHomebrewPin(
	name string,
	pin bool,
	primary *gtk.Button,
	controls []*gtk.Button,
	gate *actionstate.Gate,
) {
	action := "Unpin"
	description := "It will get updates again."
	if pin {
		action = "Pin"
		description = "It stays at this version until you unpin it."
	}

	dialog := adw.NewAlertDialog(fmt.Sprintf("%s %s?", action, name), description)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("confirm", action)
	dialog.SetResponseAppearance("confirm", adw.ResponseSuggestedValue)
	// Escape and the default both cancel: nothing happens unless asked.
	dialog.SetDefaultResponse("cancel")
	dialog.SetCloseResponse("cancel")

	uh.confirmations.connect(dialog, func(response string) {
		if response != "confirm" {
			gate.Reset()
			uh.settleHomebrewRows(false)
			return
		}
		setHomebrewControlsSensitive(controls, false)
		setPackageButtonLabel(primary, action+"ning…", name)
		go uh.runHomebrewPin(name, pin, primary, controls, gate)
	})
	dialog.Present(&uh.applicationsPrefsPage.Widget)
}

func (uh *UserHome) runHomebrewPin(
	name string,
	pin bool,
	primary *gtk.Button,
	controls []*gtk.Button,
	gate *actionstate.Gate,
) {
	var err error
	if pin {
		err = homebrew.Pin(name)
	} else {
		err = homebrew.Unpin(name)
	}
	dryRun := dryrun.Enabled()
	decision := actionstate.PackagePin(err == nil, dryRun)

	idleLabel := "Unpin…"
	completeLabel := "Unpinned"
	errorMessage := fmt.Sprintf("Couldn't unpin %s. Try again.", name)
	if pin {
		idleLabel = "Pin…"
		completeLabel = "Pinned"
		errorMessage = fmt.Sprintf("Couldn't pin %s. Try again.", name)
	}
	uh.finishHomebrewPackageMutation(
		name,
		decision,
		err,
		errorMessage,
		actionmsg.Pin(dryRun, name, pin),
		idleLabel,
		completeLabel,
		primary,
		controls,
		gate,
	)
}

func (uh *UserHome) confirmHomebrewUninstall(
	name string,
	kind homebrew.PackageKind,
	primary *gtk.Button,
	controls []*gtk.Button,
	gate *actionstate.Gate,
) {
	dialog := adw.NewAlertDialog(
		fmt.Sprintf("Uninstall %s?", name),
		fmt.Sprintf("This removes %s from this computer. Your own files are kept.", name),
	)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("uninstall", "Uninstall")
	dialog.SetResponseAppearance("uninstall", adw.ResponseDestructiveValue)
	// Escape and the default both cancel: nothing happens unless asked.
	dialog.SetDefaultResponse("cancel")
	dialog.SetCloseResponse("cancel")

	uh.confirmations.connect(dialog, func(response string) {
		if response != "uninstall" {
			gate.Reset()
			uh.settleHomebrewRows(false)
			return
		}
		setHomebrewControlsSensitive(controls, false)
		setPackageButtonLabel(primary, "Uninstalling…", name)
		go uh.runHomebrewUninstall(name, kind, primary, controls, gate)
	})
	dialog.Present(&uh.applicationsPrefsPage.Widget)
}

func (uh *UserHome) runHomebrewUninstall(
	name string,
	kind homebrew.PackageKind,
	primary *gtk.Button,
	controls []*gtk.Button,
	gate *actionstate.Gate,
) {
	err := homebrew.Uninstall(name, kind == homebrew.Cask)
	dryRun := dryrun.Enabled()
	decision := actionstate.PackageUninstall(err == nil, dryRun)
	var dependents []string
	var depErr *homebrew.DependentsError
	if errors.As(err, &depErr) {
		dependents = depErr.Dependents
	}
	uh.finishHomebrewPackageMutation(
		name,
		decision,
		err,
		actionmsg.UninstallFailure(name, dependents),
		actionmsg.Uninstall(dryRun, name),
		"Uninstall…",
		"Uninstalled",
		primary,
		controls,
		gate,
	)
	if err == nil && decision.Refresh {
		// A collection holding the removed package is no longer fully
		// installed; re-observe so its row offers Install again.
		sgtk.RunOnMainThread(func() {
			uh.refreshBundleStatuses()
			if uh.wallpaperRefresh != nil {
				uh.wallpaperRefresh()
			}
		})
	}
}

func (uh *UserHome) finishHomebrewPackageMutation(
	name string,
	decision actionstate.Decision,
	err error,
	errorMessage string,
	toast string,
	idleLabel string,
	completeLabel string,
	primary *gtk.Button,
	controls []*gtk.Button,
	gate *actionstate.Gate,
) {
	sgtk.RunOnMainThread(func() {
		refresh := false
		if decision.RestoreControl {
			gate.Reset()
			setPackageButtonLabel(primary, idleLabel, name)
			setHomebrewControlsSensitive(controls, true)
		}
		if err != nil {
			// The command's error text quotes file locations and the
			// failing recipe; that belongs in the log, not in a toast.
			log.Printf("Homebrew package action failed: %v", err)
			uh.toastAdder.ShowErrorToast(errorMessage)
		} else {
			if decision.CompleteControl {
				gate.Complete()
				setPackageButtonLabel(primary, completeLabel, name)
				setHomebrewControlsSensitive(controls, false)
			}
			uh.toastAdder.ShowToast(toast)
			refresh = decision.Refresh
		}
		uh.settleHomebrewRows(refresh)
	})
}

// setPackageButtonLabel sets a package row button's visible label and names
// it after the package for assistive technology: every row shows the same
// words, so the label alone announced "Uninstall" for every package.
// GtkButton names itself from its label child through a LABELLED_BY
// relation, which outranks the LABEL property, so the relation is dropped
// after every label change.
func setPackageButtonLabel(button *gtk.Button, label, name string) {
	button.SetLabel(label)
	button.ResetRelation(gtk.AccessibleRelationLabelledByValue)
	SetAccessibleLabel(button, pageview.HomebrewPackageButtonName(label, name))
}

// settleHomebrewRows runs on the GTK main thread once a row action has
// released its gate. It starts one inventory load when the action asked for
// one or when a list rebuild was deferred while the action ran, so the
// deferral never leaves the lists stale.
func (uh *UserHome) settleHomebrewRows(refresh bool) {
	formulaeOwed := uh.formulaGates.Settled()
	casksOwed := uh.caskGates.Settled()
	if refresh || formulaeOwed || casksOwed {
		go uh.loadHomebrewPackages()
	}
}

func setHomebrewControlsSensitive(controls []*gtk.Button, sensitive bool) {
	for _, button := range controls {
		button.SetSensitive(sensitive)
	}
}

// Each expander owns one lookup and its rows. Destruction cancels the worker;
// callbacks never publish into a replacement page or a destroyed widget.
func bindBundleDescriptions(expander *adw.ExpanderRow, rows []*adw.ActionRow, items []homebrew.BundleItem) {
	started, disposed := false, false
	ctx, cancel := context.WithCancel(context.Background())
	destroyed := func(_ gtk.Widget) { disposed = true; cancel() }
	expander.ConnectDestroy(&destroyed)
	expanded := func(_ gobject.Object, _ uintptr) {
		if started || disposed || !expander.GetExpanded() {
			return
		}
		started = true
		go func() {
			defer cancel()
			described, err := homebrew.DescribeBundleItems(ctx, items)
			if err != nil {
				log.Printf("Could not load collection descriptions: %v", err)
			}
			sgtk.RunOnMainThread(func() {
				if disposed {
					return
				}
				for i, item := range described {
					rows[i].SetSubtitle(bundleview.ContentsSubtitle(item))
				}
			})
		}()
	}
	expander.ConnectNotify(&expanded)
}
