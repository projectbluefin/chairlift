package views

import (
	"errors"
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/settings"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/trustmsg"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// SetupHost is what the setup assistant's controls act through. Every
// operation the assistant offers already exists on a page, so the assistant
// never runs one itself: an Appearance switch flips the Livery page's own
// switch, a collection installs through the Apps page's per-collection gate,
// and an update source is bound to the same GSettings key the Preferences
// dialog binds. One admission, one state, and the pages need no refresh when
// the dialog closes because they were never out of date.
type SetupHost interface {
	// LiveryChoice reports one Appearance surface as the Livery page holds
	// it: whether its mark is on, whether this desktop can apply it at all,
	// and whether the page has finished its first load. Before ready the
	// other two answers are meaningless and the row must stay insensitive.
	LiveryChoice(surface livery.Surface) (enabled, available, ready bool)
	// OnLiveryLoaded runs fn on the main thread each time the Livery page
	// finishes loading its state, and immediately when it already has.
	OnLiveryLoaded(fn func())
	// SetLiveryEnabled turns a surface's mark on or off through the Livery
	// page's own switch, so the page's handler applies it under its gate. It
	// returns false, having done nothing, when the page is not ready or its
	// gate is busy; the caller restores its control and says so.
	SetLiveryEnabled(surface livery.Surface, enabled bool) bool
	// Bundles is the collection inventory the Apps page discovered, and
	// whether discovery has finished. Before it finishes the slice is nil.
	Bundles() (bundles []homebrew.Bundle, loaded bool)
	// OnBundlesLoaded runs fn on the main thread once discovery finishes, or
	// immediately when it already has.
	OnBundlesLoaded(fn func())
	// ConnectBundleInstall makes button install bundle through the shared
	// per-collection gate and keeps every button bound to that collection
	// showing the same state.
	ConnectBundleInstall(bundle homebrew.Bundle, button *gtk.Button)
	// UpdateSource reports one update source's latest state from the update
	// shell, and whether the shell's first availability check has finished.
	UpdateSource(id updateflow.SourceID) (state updateflow.SourceState, ready bool)
	// OnUpdateSourcesRendered runs fn on the main thread after each update
	// shell render, which is when source availability can change.
	OnUpdateSourcesRendered(fn func())
	// UpdatePreferences is the store the Preferences dialog binds its source
	// switches to; the assistant binds to the same keys.
	UpdatePreferences() *settings.Store
}

// LiveryChoice implements SetupHost.
func (uh *UserHome) LiveryChoice(surface livery.Surface) (enabled, available, ready bool) {
	if uh == nil || !uh.liveryLoaded {
		return false, false, false
	}
	if uh.liverySchemaMissing {
		return false, false, true
	}
	switch surface {
	case livery.AppGrid:
		return uh.liveryState.AppGridEnabled, uh.liveryAppGridAvailable, true
	case livery.Panel:
		return uh.liveryState.PanelEnabled && uh.liveryPanelAvailable, uh.liveryPanelAvailable, true
	default:
		return uh.liveryState.DockEnabled, true, true
	}
}

// OnLiveryLoaded implements SetupHost.
func (uh *UserHome) OnLiveryLoaded(fn func()) {
	if uh == nil || fn == nil {
		return
	}
	uh.liveryLoadWaiters = append(uh.liveryLoadWaiters, fn)
	if uh.liveryLoaded {
		fn()
	}
}

// notifyLiveryLoaded runs every OnLiveryLoaded callback, on the main thread,
// once a load has applied (or failed) and liveryLoaded is armed.
func (uh *UserHome) notifyLiveryLoaded() {
	for _, fn := range uh.liveryLoadWaiters {
		fn()
	}
}

// OnUpdateSourcesRendered implements SetupHost.
func (uh *UserHome) OnUpdateSourcesRendered(fn func()) {
	if uh == nil || uh.updateShell == nil {
		return
	}
	uh.updateShell.SetOnSourcesRendered(fn)
}

// SetLiveryEnabled implements SetupHost by driving the page's own switch.
//
// gtk_switch_set_active emits state-set when the value changes, which runs
// the same handler a click does — comparison against liveryState, the
// section's gate, persistence and apply off the main thread — and leaves
// the page's switch showing what the assistant chose. The switch is
// insensitive exactly while its gate holds a run or the surface is
// unavailable, so its sensitivity is the admission answer.
func (uh *UserHome) SetLiveryEnabled(surface livery.Surface, enabled bool) bool {
	if uh == nil || !uh.liveryLoaded || uh.liverySuppress {
		return false
	}
	_, toggle, _ := uh.liveryToggleGate(surface)
	if toggle == nil || !toggle.GetSensitive() {
		return false
	}
	if toggle.GetActive() == enabled {
		return true
	}
	toggle.SetActive(enabled)
	return true
}

// Bundles implements SetupHost.
func (uh *UserHome) Bundles() ([]homebrew.Bundle, bool) {
	if uh == nil || !uh.brewBundlesLoaded {
		return nil, false
	}
	return append([]homebrew.Bundle(nil), uh.brewBundles...), true
}

// OnBundlesLoaded implements SetupHost.
func (uh *UserHome) OnBundlesLoaded(fn func()) {
	if uh == nil || fn == nil {
		return
	}
	if uh.brewBundlesLoaded {
		fn()
		return
	}
	uh.brewBundlesWaiters = append(uh.brewBundlesWaiters, fn)
}

// publishBundles records the discovered collections on the main thread and
// releases everything waiting on them. It runs at most once per process.
func (uh *UserHome) publishBundles(bundles []homebrew.Bundle) {
	uh.brewBundles = bundles
	uh.brewBundlesLoaded = true
	waiters := uh.brewBundlesWaiters
	uh.brewBundlesWaiters = nil
	for _, fn := range waiters {
		fn()
	}
}

// AttachUpdateSources gives the views the update shell and the preference
// store the window built after them, which the setup assistant's Update
// Preferences step reads through UpdateSource and UpdatePreferences.
func (uh *UserHome) AttachUpdateSources(shell *UpdateShell, store *settings.Store) {
	if uh == nil {
		return
	}
	uh.updateShell = shell
	uh.updatePrefs = store
}

// UpdateSource implements SetupHost.
func (uh *UserHome) UpdateSource(id updateflow.SourceID) (updateflow.SourceState, bool) {
	if uh == nil || uh.updateShell == nil || !uh.updateShell.SourcesReady() {
		return updateflow.SourceState{}, false
	}
	for _, state := range uh.updateShell.Sources() {
		if state.ID == id {
			return state, true
		}
	}
	return updateflow.SourceState{}, true
}

// UpdatePreferences implements SetupHost.
func (uh *UserHome) UpdatePreferences() *settings.Store {
	if uh == nil {
		return nil
	}
	return uh.updatePrefs
}

// bundleInstall is the state one collection's install shares across every
// surface that offers it: the gate that admits one run at a time and the
// buttons that must all show the same phase.
type bundleInstall struct {
	gate    bundleview.InstallGate
	buttons []bundleInstallButton
}

type bundleInstallButton struct {
	button  *gtk.Button
	label   *gtk.Label
	spinner *gtk.Spinner
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
	}
}

// ConnectBundleInstall implements SetupHost. The Apps page connects its own
// rows through it too, so both surfaces share one callback (bundleButtons)
// and one gate per collection.
func (uh *UserHome) ConnectBundleInstall(bundle homebrew.Bundle, button *gtk.Button) {
	if uh == nil || button == nil {
		return
	}
	if uh.bundleInstalls == nil {
		uh.bundleInstalls = make(map[string]*bundleInstall)
	}
	shared := uh.bundleInstalls[bundle.Path]
	if shared == nil {
		shared = &bundleInstall{}
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
	shared.buttons = append(shared.buttons, bundleInstallButton{button, labelWidget, spinner})
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
					uh.toastAdder.ShowErrorToast(trustmsg.BundleMessage(collection.Title, trustErr.Tap))
					return
				}
				uh.toastAdder.ShowErrorToast(fmt.Sprintf(
					"Could not install %s. Part of it may have been installed before it stopped.",
					collection.Title,
				))
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
