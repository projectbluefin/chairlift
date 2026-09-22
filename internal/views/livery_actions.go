package views

import (
	"errors"
	"log"

	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"
)

// The Livery handlers all follow one shape: compare the widget's value
// against the last state the page loaded, do nothing when they agree, and
// otherwise persist and apply off the main thread.
//
// The comparison is not an optimization, it is what makes the handlers
// correct. AdwSwitchRow and AdwComboRow have no change-specific signal in
// these bindings, so the page listens to `notify`, which fires for every
// property — sensitivity, subtitle, title — not only the one that matters.
// Acting on each notify would write settings when refreshLiveryState merely
// made a row sensitive, and would loop when applyLiveryState set a switch to
// the value it just read. Comparing against known state makes both harmless.

// onLiveryAppGridToggled turns the personal mark on or off.
func (uh *UserHome) onLiveryAppGridToggled(enabled bool) {
	if uh.liverySuppress || !uh.liveryLoaded {
		return
	}

	if enabled == uh.liveryState.AppGridEnabled {
		return
	}
	if !uh.liveryAppGridGate.TryStart() {
		return
	}
	if uh.liveryAppGridSwitch != nil {
		uh.liveryAppGridSwitch.SetSensitive(false)
	}
	uh.liveryState.AppGridEnabled = enabled

	if uh.liveryAppGridRow != nil {
		uh.liveryAppGridRow.SetSensitive(enabled)
	}

	slug := uh.liveryState.AppGridSlug
	source := uh.liverySource(livery.AppGrid)
	go func() {
		defer uh.releaseLiveryToggle(livery.AppGrid)

		ctx, cancel := livery.DefaultContext()
		defer cancel()

		if err := livery.SetBool(ctx, livery.KeyAppGridEnabled, enabled); err != nil {
			uh.reportLiveryFailure("saving the app grid setting", err)
			return
		}
		if !enabled {
			if err := livery.Clear(ctx, livery.AppGrid); err != nil {
				uh.reportLiveryFailure("removing the app grid mark", err)
			}
			if err := livery.RefreshShellIcons(); err != nil {
				uh.reportLiveryFailure("refreshing the shell's icons", err)
			}
			return
		}
		// Turning the section on with nothing chosen is not a failure; the
		// entry row is now sensitive and says what to type.
		if slug == "" {
			return
		}
		if err := livery.Apply(ctx, livery.AppGrid, source); err != nil {
			uh.reportLiveryFailure("setting the app grid mark", err)
			return
		}
		if err := livery.RefreshShellIcons(); err != nil {
			uh.reportLiveryFailure("refreshing the shell's icons", err)
		}
	}()
}

// onLiveryBrandChosen applies a brand mark picked from the chooser.
//
// This is an explicit activation from a result row, so it needs no
// notify-storm guard beyond the load gate. The fetch is the one part of this
// page that needs a network, so its failures — an unreachable service, a
// brand whose artwork has been withdrawn — surface as toasts naming what went
// wrong rather than leaving the row looking applied.
func (uh *UserHome) onLiveryBrandChosen(slug string) {
	if !uh.liveryLoaded || slug == uh.liveryState.AppGridSlug {
		return
	}
	uh.liveryState.AppGridSlug = slug

	if uh.liveryAppGridRow != nil {
		uh.liveryAppGridRow.SetSubtitle(pageview.LiverySelectedBrandRow(slug).Subtitle)
	}

	enabled := uh.liveryState.AppGridEnabled
	uh.runLiverySelectionWork(livery.AppGrid, func() {
		ctx, cancel := livery.DefaultContext()
		defer cancel()

		// The choice is saved whether or not the section is on, so switching
		// it on later applies what the user already picked.
		if err := livery.SetString(ctx, livery.KeyAppGridSlug, slug); err != nil {
			uh.reportLiveryFailure("saving the brand", err)
			return
		}
		if !enabled {
			return
		}
		if err := livery.Apply(ctx, livery.AppGrid, livery.Source{Kind: livery.FromSimpleIcons, Value: slug}); err != nil {
			uh.reportLiveryFailure("fetching that brand mark", err)
			return
		}
		if err := livery.RefreshShellIcons(); err != nil {
			uh.reportLiveryFailure("refreshing the shell's icons", err)
		}
	})
}

// onLiverySurfaceToggled turns the panel or dock mark on or off.
//
// The panel additionally captures the extension's user-layer values the
// first time it is enabled, so turning it off later restores exactly what
// was there — a distro default included.
func (uh *UserHome) onLiverySurfaceToggled(surface livery.Surface, enabled bool) {
	if uh.liverySuppress || !uh.liveryLoaded {
		return
	}

	current, key := uh.liveryToggleState(surface)
	if enabled == current {
		return
	}

	gate, toggle := uh.liveryToggleGate(surface)
	if !gate.TryStart() {
		return
	}
	if toggle != nil {
		toggle.SetSensitive(false)
	}

	uh.setLiveryToggleState(surface, enabled)
	uh.setLiverySectionSensitive(surface, enabled)

	source := uh.liverySource(surface)
	savedIcon, savedMode := uh.liveryState.SavedPanelIcon, uh.liveryState.SavedPanelMode

	go func() {
		defer uh.releaseLiveryToggle(surface)

		ctx, cancel := livery.DefaultContext()
		defer cancel()

		if enabled && surface == livery.Panel && savedIcon == "" && savedMode == "" {
			icon, mode, ok := livery.CapturePanelOverrides(ctx)
			if !ok {
				// The user layer could not be read, so there is nothing
				// trustworthy to restore later. Refuse rather than record an
				// empty value, which revert would read as "reset the key".
				uh.reportLiveryFailure("reading the current panel icon", errors.New("dconf is unavailable"))
				return
			}
			if err := livery.SetString(ctx, livery.KeySavedPanelIcon, icon); err != nil {
				uh.reportLiveryFailure("recording the previous panel icon", err)
				return
			}
			if err := livery.SetString(ctx, livery.KeySavedPanelMode, mode); err != nil {
				uh.reportLiveryFailure("recording the previous panel mode", err)
				return
			}
			sgtk.RunOnMainThread(func() {
				uh.liveryState.SavedPanelIcon = icon
				uh.liveryState.SavedPanelMode = mode
			})
		}

		if err := livery.SetBool(ctx, key, enabled); err != nil {
			uh.reportLiveryFailure("saving that setting", err)
			return
		}

		if enabled {
			if err := livery.Apply(ctx, surface, source); err != nil {
				uh.reportLiveryFailure("setting the mark", err)
				return
			}
			if surface != livery.Panel {
				// The panel is the exception: its extension redraws on
				// `changed::menuicon-setting` by itself.
				if err := livery.RefreshShellIcons(); err != nil {
					uh.reportLiveryFailure("refreshing the shell's icons", err)
				}
			}
			return
		}

		if err := livery.Clear(ctx, surface); err != nil {
			uh.reportLiveryFailure("removing the mark", err)
			return
		}
		if surface != livery.Panel {
			// The panel is the exception: its extension redraws on
			// `changed::menuicon-setting` by itself.
			if err := livery.RefreshShellIcons(); err != nil {
				uh.reportLiveryFailure("refreshing the shell's icons", err)
			}
		}
		if surface == livery.Panel {
			if err := livery.ClearPanelSettings(ctx, savedIcon, savedMode); err != nil {
				uh.reportLiveryFailure("restoring the previous panel icon", err)
				return
			}
			// The capture is only taken when both saved values are empty, and
			// ClearPanelSettings has just emptied the stored keys, so the
			// in-memory copy has to follow or the next enable would keep
			// reusing the first capture instead of reading what the user has
			// now.
			sgtk.RunOnMainThread(func() {
				uh.liveryState.SavedPanelIcon = ""
				uh.liveryState.SavedPanelMode = ""
			})
		}
	}()
}

// onLiveryProjectChosen applies a CNCF project's artwork to the Files icon.
//
// This is an explicit activation — the user clicked a result row — so unlike
// the combo paths it needs no notify-storm guard beyond the load gate.
func (uh *UserHome) onLiveryProjectChosen(id string) {
	if !uh.liveryLoaded || id == uh.liveryState.DockID {
		return
	}
	uh.liveryState.DockID = id

	if uh.liveryDockSelectedRow != nil {
		selected := pageview.LiverySelectedProjectRow(id)
		uh.liveryDockSelectedRow.SetTitle(selected.Title)
		uh.liveryDockSelectedRow.SetSubtitle(selected.Subtitle)
	}
	uh.syncLiveryRotateSensitive(livery.Dock, uh.liveryState.DockEnabled)

	enabled := uh.liveryState.DockEnabled
	uh.runLiverySelectionWork(livery.Dock, func() {
		ctx, cancel := livery.DefaultContext()
		defer cancel()

		if err := livery.SetString(ctx, livery.KeyDockID, id); err != nil {
			uh.reportLiveryFailure("saving the project", err)
			return
		}
		if !enabled {
			return
		}
		if err := livery.Apply(ctx, livery.Dock, livery.Source{Kind: livery.FromCNCF, Value: id}); err != nil {
			uh.reportLiveryFailure("fetching that project's icon", err)
			return
		}
		// GNOME applies a choice when you make it, and the shell caches icon
		// textures by name — so without nudging it the new mark would not
		// appear until the next login, which is not "applied".
		if err := livery.RefreshShellIcons(); err != nil {
			uh.reportLiveryFailure("refreshing the shell's icons", err)
		}
	})
}

// onLiverySelectionChangedByID applies a foundation mark chosen by id.
func (uh *UserHome) onLiverySelectionChangedByID(surface livery.Surface, id string) {
	if !uh.liveryLoaded {
		return
	}
	current, key := uh.liverySelectionState(surface)
	if id == current {
		return
	}
	uh.setLiverySelectionState(surface, id)

	if surface == livery.Panel && uh.liveryPanelMarkRow != nil {
		uh.liveryPanelMarkRow.SetSubtitle(
			pageview.LiverySelectedFoundationRow(id, uh.liveryState.PanelCustom).Subtitle)
	}

	enabled, _ := uh.liveryToggleState(surface)
	uh.syncLiveryRotateSensitive(surface, enabled)
	source := uh.liverySource(surface)

	uh.runLiverySelectionWork(surface, func() {
		ctx, cancel := livery.DefaultContext()
		defer cancel()

		if err := livery.SetString(ctx, key, id); err != nil {
			uh.reportLiveryFailure("saving the selection", err)
			return
		}
		if !enabled {
			return
		}
		if err := livery.Apply(ctx, surface, source); err != nil {
			uh.reportLiveryFailure("setting the mark", err)
			return
		}
		if surface != livery.Panel {
			if err := livery.RefreshShellIcons(); err != nil {
				uh.reportLiveryFailure("refreshing the shell's icons", err)
			}
		}
	})
}

// onLiveryRotateToggled turns login rotation on or off for one section and
// syncs the systemd user unit, which exists only while some section rotates.
//
// Both rotate switches share one serializer, because both drive the same
// unit. An unordered goroutine per click lets a fast on-then-off flip run
// RemoveRotation before the earlier InstallRotation finishes, leaving the
// unit installed while the keys say nothing rotates. Claiming on the main
// thread fixes the order the user flipped the switches in, and an attempt a
// newer one has overtaken drops out — which is only safe because the work
// writes both rotate keys from the snapshot it was claimed with, so the
// surviving attempt persists every choice made before it.
func (uh *UserHome) onLiveryRotateToggled(surface livery.Surface, enabled bool) {
	if uh.liverySuppress || !uh.liveryLoaded {
		return
	}

	if enabled == uh.liveryRotateState(surface) {
		return
	}
	uh.setLiveryRotateState(surface, enabled)
	state := uh.liveryState

	generation := uh.liveryRotateWork.Claim()
	go func() {
		uh.liveryRotateWork.Run(generation, func() {
			ctx, cancel := livery.DefaultContext()
			defer cancel()

			if err := livery.SetBool(ctx, livery.KeyPanelRotate, state.PanelRotate); err != nil {
				uh.reportLiveryFailure("saving the rotation setting", err)
				return
			}
			if err := livery.SetBool(ctx, livery.KeyDockRotate, state.DockRotate); err != nil {
				uh.reportLiveryFailure("saving the rotation setting", err)
				return
			}
			if err := livery.SyncRotationUnit(ctx, state); err != nil {
				uh.reportLiveryFailure("scheduling rotation", err)
			}
		})
	}()
}

// onLiveryCustomFileChosen points a surface at the user's own SVG.
//
// The file is not copied into the icon theme until it is applied, and Apply
// validates it — an unreadable file or a PNG chosen by mistake is reported
// naming the path, rather than installing bytes that resolve to a blank icon.
func (uh *UserHome) onLiveryCustomFileChosen(surface livery.Surface, path string) {
	if !uh.liveryLoaded {
		return
	}

	var pathKey, idKey string
	var enabled bool
	switch surface {
	case livery.AppGrid:
		pathKey, idKey = livery.KeyAppGridCustom, livery.KeyAppGridSlug
		enabled = uh.liveryState.AppGridEnabled
		uh.liveryState.AppGridCustom, uh.liveryState.AppGridSlug = path, livery.CustomID
	case livery.Panel:
		pathKey, idKey = livery.KeyPanelCustom, livery.KeyPanelID
		enabled = uh.liveryState.PanelEnabled
		uh.liveryState.PanelCustom, uh.liveryState.PanelID = path, livery.CustomID
	default:
		pathKey, idKey = livery.KeyDockCustom, livery.KeyDockID
		enabled = uh.liveryState.DockEnabled
		uh.liveryState.DockCustom, uh.liveryState.DockID = path, livery.CustomID
	}
	uh.showLiveryCustomPath(surface, path)
	if surface != livery.AppGrid {
		uh.syncLiveryRotateSensitive(surface, enabled)
	}

	source := uh.liverySource(surface)
	uh.runLiverySelectionWork(surface, func() {
		ctx, cancel := livery.DefaultContext()
		defer cancel()

		if err := livery.SetString(ctx, pathKey, path); err != nil {
			uh.reportLiveryFailure("saving the file", err)
			return
		}
		if err := livery.SetString(ctx, idKey, livery.CustomID); err != nil {
			uh.reportLiveryFailure("saving the selection", err)
			return
		}
		if !enabled {
			return
		}
		if err := livery.Apply(ctx, surface, source); err != nil {
			uh.reportLiveryFailure("using that file", err)
			return
		}
		if surface != livery.Panel {
			if err := livery.RefreshShellIcons(); err != nil {
				uh.reportLiveryFailure("refreshing the shell's icons", err)
			}
		}
	})
}

// showLiveryCustomPath updates the section's summary row to name the file.
func (uh *UserHome) showLiveryCustomPath(surface livery.Surface, path string) {
	summary := pageview.LiveryCustomRow(path).Subtitle
	switch surface {
	case livery.AppGrid:
		if uh.liveryAppGridRow != nil {
			uh.liveryAppGridRow.SetSubtitle(summary)
		}
	case livery.Panel:
		if uh.liveryPanelMarkRow != nil {
			uh.liveryPanelMarkRow.SetSubtitle(summary)
		}
	default:
		if uh.liveryDockSelectedRow != nil {
			uh.liveryDockSelectedRow.SetSubtitle(summary)
		}
	}
}

// reportLiveryFailure logs and toasts, on the main thread.
func (uh *UserHome) reportLiveryFailure(what string, err error) {
	log.Printf("livery: %s: %v", what, err)
	sgtk.RunOnMainThread(func() {
		uh.toastAdder.ShowErrorToast("Livery: " + what + " failed — " + err.Error())
	})
}

// ── per-surface state accessors ──────────────────────────────────────────
//
// The panel and dock sections are identical in behavior and differ only in
// which State fields and settings keys they read and write, so the handlers
// above are written once against these.

// liveryToggleGate returns the gate that serializes one section's toggle work
// and the switch that must go insensitive while it runs.
//
// Every section needs this, not only the panel. Apply and Clear for a surface
// write and delete the same mark file, and an unordered goroutine per click
// lets a fast off-then-on flip run Apply before the earlier Clear finishes —
// leaving the switch showing enabled with the mark gone. The gate makes the
// second click a no-op until the first one lands, and the insensitive switch
// says so.
func (uh *UserHome) liveryToggleGate(s livery.Surface) (*actionstate.Gate, *gtk.Switch) {
	switch s {
	case livery.AppGrid:
		return &uh.liveryAppGridGate, uh.liveryAppGridSwitch
	case livery.Panel:
		return &uh.liveryPanelGate, uh.liveryPanelSwitch
	default:
		return &uh.liveryDockGate, uh.liveryDockSwitch
	}
}

// liverySelectionWork returns the serializer that orders one section's
// selection work — brand, project, foundation, and custom file all write the
// same keys and install into the same mark file.
func (uh *UserHome) liverySelectionWork(s livery.Surface) *actionstate.Serializer {
	switch s {
	case livery.AppGrid:
		return &uh.liveryAppGridWork
	case livery.Panel:
		return &uh.liveryPanelWork
	default:
		return &uh.liveryDockWork
	}
}

// runLiverySelectionWork claims this selection as the newest one and runs its
// persist-and-apply behind the section's serializer.
//
// Each handler previously started a bare goroutine, so two picks made in
// quick succession ran unordered: the second could persist its id while the
// first's Apply landed afterwards, leaving the stored selection and the
// installed icon naming different marks. Claiming on the main thread fixes
// the order the user made the picks in; a pick already overtaken by a newer
// one does no work at all, because the newer one writes both halves.
func (uh *UserHome) runLiverySelectionWork(s livery.Surface, work func()) {
	serializer := uh.liverySelectionWork(s)
	generation := serializer.Claim()
	go func() {
		serializer.Run(generation, work)
	}()
}

// releaseLiveryToggle reopens a section's gate and its switch on the main
// thread, so the widget touch happens where GTK requires it.
func (uh *UserHome) releaseLiveryToggle(s livery.Surface) {
	sgtk.RunOnMainThread(func() {
		gate, toggle := uh.liveryToggleGate(s)
		gate.Reset()
		if toggle != nil {
			toggle.SetSensitive(true)
		}
	})
}

func (uh *UserHome) liveryToggleState(s livery.Surface) (bool, string) {
	if s == livery.Panel {
		return uh.liveryState.PanelEnabled, livery.KeyPanelEnabled
	}
	return uh.liveryState.DockEnabled, livery.KeyDockEnabled
}

func (uh *UserHome) setLiveryToggleState(s livery.Surface, v bool) {
	if s == livery.Panel {
		uh.liveryState.PanelEnabled = v
		return
	}
	uh.liveryState.DockEnabled = v
}

func (uh *UserHome) liverySelectionState(s livery.Surface) (string, string) {
	if s == livery.Panel {
		return uh.liveryState.PanelID, livery.KeyPanelID
	}
	return uh.liveryState.DockID, livery.KeyDockID
}

func (uh *UserHome) setLiverySelectionState(s livery.Surface, id string) {
	if s == livery.Panel {
		uh.liveryState.PanelID = id
		return
	}
	uh.liveryState.DockID = id
}

func (uh *UserHome) liveryRotateState(s livery.Surface) bool {
	if s == livery.Panel {
		return uh.liveryState.PanelRotate
	}
	return uh.liveryState.DockRotate
}

func (uh *UserHome) setLiveryRotateState(s livery.Surface, v bool) {
	if s == livery.Panel {
		uh.liveryState.PanelRotate = v
		return
	}
	uh.liveryState.DockRotate = v
}

func (uh *UserHome) liverySource(s livery.Surface) livery.Source {
	switch s {
	case livery.AppGrid:
		return uh.liveryState.AppGridSource()
	case livery.Panel:
		return uh.liveryState.PanelSource()
	default:
		return uh.liveryState.DockSource()
	}
}

// setLiverySectionSensitive follows the section's master switch, so a
// selection cannot be changed for a mark that is not being set.
func (uh *UserHome) setLiverySectionSensitive(s livery.Surface, enabled bool) {
	if s == livery.Panel {
		if uh.liveryPanelMarkRow != nil {
			uh.liveryPanelMarkRow.SetSensitive(enabled)
		}
		uh.syncLiveryRotateSensitive(s, enabled)
		return
	}
	if uh.liveryDockSelectedRow != nil {
		uh.liveryDockSelectedRow.SetSensitive(enabled)
	}
	uh.syncLiveryRotateSensitive(s, enabled)
}

// syncLiveryRotateSensitive follows the section switch *and* the selection.
//
// livery.Rotate refuses to advance a section pinned to the user's own SVG —
// rotation cycles a catalog, and a custom file is not in one — so a switch
// left sensitive for that selection would promise a login-time change that
// never happens.
func (uh *UserHome) syncLiveryRotateSensitive(s livery.Surface, sectionAvailable bool) {
	if s == livery.Panel {
		if uh.liveryPanelRotate != nil {
			uh.liveryPanelRotate.SetSensitive(
				pageview.LiveryRotationAvailable(sectionAvailable, uh.liveryState.PanelID))
		}
		return
	}
	if uh.liveryDockRotate != nil {
		uh.liveryDockRotate.SetSensitive(
			pageview.LiveryRotationAvailable(sectionAvailable, uh.liveryState.DockID))
	}
}
