package views

import (
	"errors"
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/trustmsg"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// bundleInstall is the state one collection's install shares across every
// surface that offers it: the gate that admits one run at a time and the
// buttons that must all show the same phase.
type bundleInstall struct {
	gate     bundleview.InstallGate
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
		SetAccessibleLabel(control.button, label)
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
		shared = &bundleInstall{progress: &uh.appInstallProgress}
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
				// match. Under dry-run decision.Complete is false —
				// nothing was changed — so the inventory stays put.
				go uh.loadHomebrewPackages()
			} else {
				shared.gate.Reset()
				shared.show(bundleview.InstallLabelReady, true)
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
