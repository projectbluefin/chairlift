package views

import (
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/trustmsg"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// bundleInstall is the state one collection's install shares across every
// surface that offers it: the gate that admits one run at a time, the
// buttons that must all show the same phase, and the collection's display
// title that names those buttons for assistive technology.
type bundleInstall struct {
	gate     bundleview.InstallGate
	title    string
	buttons  []bundleInstallButton
	progress *installProgress
}

type bundleInstallButton struct {
	button  *gtk.Button
	label   *gtk.Label
	spinner *gtk.Spinner
	bar     *gtk.ProgressBar
}

// show applies the label and sensitivity every bound button shows
// for a phase of the shared install.
func (b *bundleInstall) show(label string, sensitive bool) {
	for _, control := range b.buttons {
		control.label.SetLabel(label)
		SetAccessibleLabel(control.button, bundleview.InstallButtonName(label, b.title))
		control.button.SetSensitive(sensitive)
		busy := label == bundleview.InstallLabelRunning
		control.spinner.SetVisible(busy)
		control.spinner.SetSpinning(busy)
		if control.bar != nil {
			if busy {
				b.progress.start(control.bar)
			} else {
				b.progress.stop(control.bar)
			}
		}
	}
}

// ConnectBundleInstall connects an Apps collection to its per-collection gate
// and the page's shared native progress timer.
func (uh *UserHome) ConnectBundleInstall(bundle homebrew.Bundle, button *gtk.Button, progress *gtk.ProgressBar) {
	if uh == nil || button == nil {
		return
	}
	if uh.bundleInstalls == nil {
		uh.bundleInstalls = make(map[string]*bundleInstall)
	}
	shared := uh.bundleInstalls[bundle.Path]
	if shared == nil {
		shared = &bundleInstall{
			title:    bundleview.Describe(bundle.Name, bundle.Description, bundle.ItemCount).Title,
			progress: &uh.appInstallProgress,
		}
		uh.bundleInstalls[bundle.Path] = shared
	}
	content := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	spinner := gtk.NewSpinner()
	SetAccessibleLabel(spinner, "Installing collection")
	labelWidget := gtk.NewLabel("")
	content.Append(&labelWidget.Widget)
	content.Append(&spinner.Widget)
	button.SetChild(&content.Widget)
	button.AddCssClass("text-button")
	shared.buttons = append(shared.buttons, bundleInstallButton{button: button, label: labelWidget, spinner: spinner, bar: progress})
	// A button connected while a run is in progress, or after one completed,
	// joins at the phase the others already show; a fresh "Install" here
	// would be a button that does nothing when clicked.
	label, sensitive := shared.gate.InstallPhase()
	shared.show(label, sensitive)
	uh.bundleButtons.connect(button, func(gtk.Button) {
		uh.runBundleInstall(bundle, shared)
	})
}

// refreshBundleStatuses observes, off the GTK thread, whether each connected
// collection is already installed, so a row reads "Installed" because the
// system holds the collection, not only because this session installed it.
// It must be called on the GTK main thread. Only the newest refresh may
// publish: each result is dropped once a later refresh has begun, and a
// worker stops checking as soon as it is superseded. A collection whose
// check cannot decide keeps the row it had.
func (uh *UserHome) refreshBundleStatuses() {
	if uh == nil || len(uh.bundleInstalls) == 0 {
		return
	}
	paths := make([]string, 0, len(uh.bundleInstalls))
	for path := range uh.bundleInstalls {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	generation := uh.bundleStatusRefresh.Begin()

	go func() {
		for _, path := range paths {
			if !uh.bundleStatusRefresh.IsCurrent(generation) {
				return
			}
			status, err := homebrew.BundleCheck(path)
			if err != nil {
				log.Printf("Could not check whether app collection %q is installed: %v", path, err)
			}
			installed, known := bundleview.ObservedInstalled(status)
			if !known {
				continue
			}
			sgtk.RunOnMainThread(func() {
				if !uh.bundleStatusRefresh.IsCurrent(generation) {
					return
				}
				shared := uh.bundleInstalls[path]
				if shared == nil || !shared.gate.Observe(installed) {
					return
				}
				shared.show(shared.gate.InstallPhase())
			})
		}
	}()
}

// runBundleInstall installs one collection behind its shared gate and
// reports the outcome the way the Apps page always has: a trust refusal
// names the tap, a partial failure says so, a dry run restores the
// buttons, and a live success closes the gate for good and refreshes the
// installed inventory.
func (uh *UserHome) runBundleInstall(bundle homebrew.Bundle, shared *bundleInstall) {
	if !shared.gate.TryStart() {
		return
	}
	collection := bundleview.Describe(bundle.Name, bundle.Description, bundle.ItemCount)
	shared.show(bundleview.InstallLabelRunning, false)

	go func() {
		if err := homebrew.BundleInstall(bundle.Path); err != nil {
			// The error names the file and the failing entry, which is a
			// log detail; the person gets the one thing they can act on.
			log.Printf("Error installing app collection %q: %v", bundle.Name, err)
			sgtk.RunOnMainThread(func() {
				shared.gate.Reset()
				shared.show(bundleview.InstallLabelReady, true)
				// Part of it, or of a collection sharing its items, may
				// have been installed before it stopped: `brew bundle`
				// carries on past a failed entry.
				uh.homebrewInventoryChanged()
				var trustErr *homebrew.UntrustedTapError
				if errors.As(err, &trustErr) {
					uh.toastAdder.ShowErrorToast(trustmsg.BundleMessage(collection.Title))
					return
				}
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Couldn't install all of %s. Try again.", collection.Title))
			})
			return
		}

		decision := actionmsg.BundleInstall(dryrun.Enabled(), collection.Title)
		sgtk.RunOnMainThread(func() {
			if decision.Complete {
				shared.gate.Complete()
				shared.show(bundleview.InstallLabelCompleted, false)
				// A live install can add packages the current inventory
				// snapshot predates, so refresh the installed list to
				// match, and re-observe every collection: they can share
				// items, and a check begun before this install must not
				// publish over it. Under dry-run decision.Complete is
				// false — nothing was changed — so both stay put.
				uh.homebrewInventoryChanged()
			} else {
				shared.gate.Reset()
				shared.show(bundleview.InstallLabelReady, true)
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
