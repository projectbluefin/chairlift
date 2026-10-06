package views

import (
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
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
		group.SetTitle("App collections")
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
		group.SetTitle("Packages from Homebrew")
		group.SetDescription("Apps and tools installed with Homebrew, a third-party source.")

		// Package-list export row
		dumpRow := adw.NewActionRow()
		dumpRow.SetTitle("Export package list")
		dumpRow.SetSubtitle("Saves a list of everything you installed here so you can put it back later. Replaces the list you exported last time.")
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
		uh.installedFormulae.SetTitle("Command line tools")
		uh.installedFormulae.SetDescription("Counting…")

		// Applications — Homebrew casks
		uh.installedCasks = adw.NewPreferencesGroup()
		uh.installedCasks.SetTitle("Homebrew applications")
		uh.installedCasks.SetDescription("Counting…")
		page.Add(uh.installedCasks)
		page.Add(uh.installedFormulae)
		page.Add(group)

		// Load packages asynchronously
		go uh.loadHomebrewPackages()
	}

}

// loadBrewBundles discovers configured collections off the GTK thread and
// builds their rows on the main thread. ConnectBundleInstall owns the shared
// callback and per-collection action gate; setup navigates these same widgets.
func (uh *UserHome) loadBrewBundles(paths []string) {
	bundles, discoveryErr := homebrew.AvailableBundles(paths)
	warning := ""
	if discoveryErr != nil {
		log.Printf("Error discovering app collections: %v", discoveryErr)
		warning = discoveryErr.Error()
	}
	presentation := bundleview.Present(len(bundles), warning)

	sgtk.RunOnMainThread(func() {
		if uh.brewBundlesGroup == nil {
			return
		}
		uh.brewBundlesGroup.SetDescription(presentation.Description)

		if len(bundles) == 0 {
			row := adw.NewActionRow()
			row.SetTitle(presentation.PlaceholderTitle)
			row.SetSubtitle(presentation.PlaceholderSubtitle)
			uh.brewBundlesGroup.Add(&row.Widget)
			return
		}

		for _, bundle := range bundles {
			row, installBtn, progress := newBundleRow(bundle)
			uh.ConnectBundleInstall(bundle, installBtn, progress)
			uh.brewBundlesGroup.Add(&row.Widget)
		}
	})
}

// newBundleRow builds one collection row with its Install button, unwired:
// the caller connects the button through ConnectBundleInstall.
func newBundleRow(bundle homebrew.Bundle) (*adw.ActionRow, *gtk.Button, *gtk.ProgressBar) {
	collection := bundleview.Describe(bundle.Name, bundle.Description, bundle.ItemCount)
	row := adw.NewActionRow()
	row.SetTitle(collection.Title)
	row.SetUseMarkup(false)
	row.SetSubtitle(collection.Subtitle)

	installBtn := gtk.NewButtonWithLabel("Install")
	installBtn.SetValign(gtk.AlignCenterValue)
	progress := newInstallProgress("Installing collection…")
	controls := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	controls.SetValign(gtk.AlignCenterValue)
	controls.Append(&installBtn.Widget)
	controls.Append(&progress.Widget)
	row.AddSuffix(&controls.Widget)
	return row, installBtn, progress
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
				uh.installedFormulae.SetDescription("Could not read the list")
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

					pinLabel := "Pin"
					pinTooltip := "Keep this version and skip it during updates"
					if pkg.Pinned {
						pinLabel = "Unpin"
						pinTooltip = "Let this be updated again"
					}
					pinBtn := gtk.NewButtonWithLabel(pinLabel)
					pinBtn.SetValign(gtk.AlignCenterValue)
					pinBtn.SetTooltipText(pinTooltip)

					uninstallBtn := gtk.NewButtonWithLabel("Uninstall")
					uninstallBtn.SetValign(gtk.AlignCenterValue)
					uninstallBtn.AddCssClass("destructive-action")
					uninstallBtn.SetTooltipText("Remove this tool from your system")

					gate := &actionstate.Gate{}
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
				uh.installedCasks.SetDescription("Could not read the list")
			})
		} else {
			sgtk.RunOnMainThread(func() {
				if !uh.brewPackagesRefresh.IsCurrent(generation) {
					return
				}
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

					uninstallBtn := gtk.NewButtonWithLabel("Uninstall")
					uninstallBtn.SetValign(gtk.AlignCenterValue)
					uninstallBtn.AddCssClass("destructive-action")
					uninstallBtn.SetTooltipText("Remove this app from your system")

					gate := &actionstate.Gate{}
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
	description := "It will be updated again with everything else."
	if pin {
		action = "Pin"
		description = "It stays at the version you have now and is skipped during updates, until you unpin it."
	}

	dialog := adw.NewAlertDialog(fmt.Sprintf("%s %s?", action, name), description)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("confirm", action)
	dialog.SetResponseAppearance("confirm", adw.ResponseSuggestedValue)

	uh.confirmations.connect(dialog, func(response string) {
		if response != "confirm" {
			gate.Reset()
			return
		}
		setHomebrewControlsSensitive(controls, false)
		primary.SetLabel(action + "ning…")
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

	idleLabel := "Unpin"
	completeLabel := "Unpinned"
	errorMessage := fmt.Sprintf("Could not unpin %s", name)
	if pin {
		idleLabel = "Pin"
		completeLabel = "Pinned"
		errorMessage = fmt.Sprintf("Could not pin %s", name)
	}
	uh.finishHomebrewPackageMutation(
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
		fmt.Sprintf("Removes %s and the files Homebrew installed with it. Anything you created yourself is left alone.", name),
	)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("uninstall", "Uninstall")
	dialog.SetResponseAppearance("uninstall", adw.ResponseDestructiveValue)

	uh.confirmations.connect(dialog, func(response string) {
		if response != "uninstall" {
			gate.Reset()
			return
		}
		setHomebrewControlsSensitive(controls, false)
		primary.SetLabel("Uninstalling…")
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
	uh.finishHomebrewPackageMutation(
		decision,
		err,
		fmt.Sprintf("Could not uninstall %s", name),
		actionmsg.Uninstall(dryRun, name),
		"Uninstall",
		"Uninstalled",
		primary,
		controls,
		gate,
	)
}

func (uh *UserHome) finishHomebrewPackageMutation(
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
		if decision.RestoreControl {
			gate.Reset()
			primary.SetLabel(idleLabel)
			setHomebrewControlsSensitive(controls, true)
		}
		if err != nil {
			// The command's error text quotes file locations and the
			// failing recipe; that belongs in the log, not in a toast.
			log.Printf("Homebrew package action failed: %v", err)
			uh.toastAdder.ShowErrorToast(errorMessage)
			return
		}
		if decision.CompleteControl {
			gate.Complete()
			primary.SetLabel(completeLabel)
			setHomebrewControlsSensitive(controls, false)
		}
		uh.toastAdder.ShowToast(toast)
		if decision.Refresh {
			go uh.loadHomebrewPackages()
		}
	})
}

func setHomebrewControlsSensitive(controls []*gtk.Button, sensitive bool) {
	for _, button := range controls {
		button.SetSensitive(sensitive)
	}
}
