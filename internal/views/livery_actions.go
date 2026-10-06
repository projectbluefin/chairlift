package views

import (
	"errors"
	"log"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/liverystate"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"
)

// The Livery handlers all follow one shape: compare the widget's value
// against the last state the page loaded, do nothing when they agree, and
// otherwise persist and apply off the main thread.
//
// Switches use state-set, never notify. Programmatic restores are suppressed
// and all worker completions publish only confirmed state on the main thread.
//
// A failed save or preview keeps confirmed state. A persisted selection can
// be shown even if artwork failed, with a failure toast; rotation requires
// both its preference pair and real schedule to land.

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
	setActivitySpinner(uh.liveryAppGridSpinner, true)

	slug := uh.liveryState.AppGridSlug
	source := uh.liveryState.AppGridSource()
	decision := actionmsg.LiveryToggle(dryrun.Enabled(), enabled, pageview.LiverySectionName(livery.AppGrid))
	preview := !decision.MutateUI
	saved := false

	go func() {
		defer func() {
			uh.finishLiveryToggle(livery.AppGrid, liverystate.Toggle(liverystate.Result{Saved: saved}, preview), enabled, decision)
		}()
		ctx, cancel := livery.DefaultContext()
		defer cancel()

		if !enabled {
			if err := livery.SetBool(ctx, livery.KeyAppGridEnabled, false); err != nil {
				uh.reportLiveryFailure("saving the app grid setting", err)
				return
			}
			saved = true
			if err := livery.Clear(ctx, livery.AppGrid); err != nil {
				uh.reportLiveryFailure("removing the app grid mark", err)
			}
			if err := livery.RefreshShellIcons(); err != nil {
				uh.reportLiveryFailure("refreshing the shell's icons", err)
			}
			return
		}

		if err := livery.SetBool(ctx, livery.KeyAppGridEnabled, true); err != nil {
			uh.reportLiveryFailure("saving the app grid setting", err)
			return
		}
		saved = true
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

	enabled := uh.liveryState.AppGridEnabled
	preview := dryrun.Enabled()
	uh.runLiverySelectionWork(livery.AppGrid, func(generation uint64) {
		ctx, cancel := livery.DefaultContext()
		defer cancel()

		saved := false
		if err := livery.SetString(ctx, livery.KeyAppGridSlug, slug); err != nil {
			uh.reportLiveryFailure("saving the brand", err)
		} else {
			saved = true
			// The choice is saved whether or not the section is on, so
			// switching it on later applies what the user already picked.
			if enabled {
				if err := livery.Apply(ctx, livery.AppGrid, livery.Source{Kind: livery.FromSimpleIcons, Value: slug}); err != nil {
					uh.reportLiveryFailure("fetching that brand mark", err)
				} else if err := livery.RefreshShellIcons(); err != nil {
					uh.reportLiveryFailure("refreshing the shell's icons", err)
				}
			}
		}

		uh.publishLiverySelection(livery.AppGrid, generation, liverystate.Selection(liverystate.Result{Saved: saved}, preview), func() {
			uh.liveryState.AppGridSlug = slug
			if uh.liveryAppGridRow != nil {
				uh.liveryAppGridRow.SetSubtitle(pageview.LiverySelectedBrandRow(slug).Subtitle)
			}
		})
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

	gate, toggle, spinner := uh.liveryToggleGate(surface)
	if !gate.TryStart() {
		return
	}
	if toggle != nil {
		toggle.SetSensitive(false)
	}
	setActivitySpinner(spinner, true)

	source := uh.liverySource(surface)
	savedIcon, savedMode := uh.liveryState.SavedPanelIcon, uh.liveryState.SavedPanelMode
	decision := actionmsg.LiveryToggle(dryrun.Enabled(), enabled, pageview.LiverySectionName(surface))
	preview := !decision.MutateUI
	saved := false

	go func() {
		defer func() {
			uh.finishLiveryToggle(surface, liverystate.Toggle(liverystate.Result{Saved: saved}, preview), enabled, decision)
		}()
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
			if decision.MutateUI {
				sgtk.RunOnMainThread(func() {
					uh.liveryState.SavedPanelIcon = icon
					uh.liveryState.SavedPanelMode = mode
				})
			}
		}

		if err := livery.SetBool(ctx, key, enabled); err != nil {
			uh.reportLiveryFailure("saving that setting", err)
			return
		}
		saved = true

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
			if decision.MutateUI {
				sgtk.RunOnMainThread(func() {
					uh.liveryState.SavedPanelIcon = ""
					uh.liveryState.SavedPanelMode = ""
				})
			}
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

	enabled := uh.liveryState.DockEnabled
	preview := dryrun.Enabled()
	uh.runLiverySelectionWork(livery.Dock, func(generation uint64) {
		ctx, cancel := livery.DefaultContext()
		defer cancel()

		saved := false
		if err := livery.SetString(ctx, livery.KeyDockID, id); err != nil {
			uh.reportLiveryFailure("saving the project", err)
		} else {
			saved = true
			if enabled {
				if err := livery.Apply(ctx, livery.Dock, livery.Source{Kind: livery.FromCNCF, Value: id}); err != nil {
					uh.reportLiveryFailure("fetching that project's icon", err)
				} else if err := livery.RefreshShellIcons(); err != nil {
					// GNOME applies a choice when you make it, and the shell
					// caches icon textures by name — so without nudging it the
					// new mark would not appear until the next login, which is
					// not "applied".
					uh.reportLiveryFailure("refreshing the shell's icons", err)
				}
			}
		}

		uh.publishLiverySelection(livery.Dock, generation, liverystate.Selection(liverystate.Result{Saved: saved}, preview), func() {
			uh.liveryState.DockID = id
			if uh.liveryDockSelectedRow != nil {
				selected := pageview.LiverySelectedProjectRow(id)
				uh.liveryDockSelectedRow.SetTitle(selected.Title)
				uh.liveryDockSelectedRow.SetSubtitle(selected.Subtitle)
			}
			uh.syncLiveryRotateSensitive(livery.Dock, uh.liveryState.DockEnabled)
		})
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

	enabled, _ := uh.liveryToggleState(surface)
	source := liverySelectionSource(surface, id)
	preview := dryrun.Enabled()
	uh.runLiverySelectionWork(surface, func(generation uint64) {
		ctx, cancel := livery.DefaultContext()
		defer cancel()

		saved := false
		if err := livery.SetString(ctx, key, id); err != nil {
			uh.reportLiveryFailure("saving the selection", err)
		} else {
			saved = true
			if enabled {
				if err := livery.Apply(ctx, surface, source); err != nil {
					uh.reportLiveryFailure("setting the mark", err)
				} else if surface != livery.Panel {
					if err := livery.RefreshShellIcons(); err != nil {
						uh.reportLiveryFailure("refreshing the shell's icons", err)
					}
				}
			}
		}

		uh.publishLiverySelection(surface, generation, liverystate.Selection(liverystate.Result{Saved: saved}, preview), func() {
			uh.setLiverySelectionState(surface, id)
			if surface == livery.Panel && uh.liveryPanelMarkRow != nil {
				uh.liveryPanelMarkRow.SetSubtitle(
					pageview.LiverySelectedFoundationRow(id, uh.liveryState.PanelCustom).Subtitle)
			}
			// Read the section's confirmed state now, not the value captured
			// when the selection started: a toggle that committed while this
			// work ran must not be overwritten by a stale "off" (#496).
			uh.restoreLiveryRotateSensitive(surface)
		})
	})
}

// onLiveryRotateToggled turns login rotation on or off for one section and
// syncs the systemd user unit, which exists only while some section rotates.
//
// Both rotate switches share one serializer, because both drive the same
// unit. An unordered goroutine per click lets a fast on-then-off flip run
// RemoveRotation before the earlier InstallRotation finishes, leaving the
// unit installed while the keys say nothing rotates. Claiming on the main
// thread fixes the order the user flipped the switches in.
//
// The two switches also share one persisted pair of keys, so every attempt
// snapshots the confirmed state with the in-flight candidates overlaid and
// writes both. The newest attempt to publish owns the whole pair; a
// superseded one leaves its optimistic switch in place rather than reverting
// it to a value the newer attempt is about to replace.
func (uh *UserHome) onLiveryRotateToggled(surface livery.Surface, enabled bool) {
	if uh.liverySuppress || !uh.liveryLoaded {
		return
	}

	candidate := uh.liveryRotatePending.Overlay(uh.liveryState)
	current := candidate.DockRotate
	if surface == livery.Panel {
		current = candidate.PanelRotate
	}
	if enabled == current {
		return
	}
	// Record this candidate before snapshotting so the snapshot carries it,
	// then overlay every candidate already in flight. Taking the snapshot from
	// confirmed state alone would write the other section's older confirmed
	// value back over a rotation that already landed.
	generation := uh.liveryRotateWork.Claim()
	uh.liveryRotatePending.Set(surface, enabled)
	state := uh.liveryRotatePending.Overlay(uh.liveryState)
	preview := dryrun.Enabled()

	go func() {
		uh.liveryRotateWork.Run(generation, func() {
			sgtk.RunOnMainThread(func() {
				setActivitySpinner(uh.liveryPanelRotateSpinner, true)
				setActivitySpinner(uh.liveryDockRotateSpinner, true)
			})
			ctx, cancel := livery.DefaultContext()
			defer cancel()

			observed, err := livery.ConfigureRotation(ctx, state)
			if err != nil {
				uh.reportLiveryFailure("scheduling rotation", err)
			}

			outcome := liverystate.Rotation(liverystate.Result{Saved: err == nil}, preview)
			sgtk.RunOnMainThread(func() {
				if !uh.liveryRotateWork.IsCurrent(generation) {
					// A newer flip owns the whole pair and will publish it,
					// carrying this candidate in its snapshot. Leave this
					// attempt's optimistic switch alone; reverting it here
					// would show off a rotation the newer attempt persists.
					return
				}
				uh.liveryRotatePending.Clear()
				setActivitySpinner(uh.liveryPanelRotateSpinner, false)
				setActivitySpinner(uh.liveryDockRotateSpinner, false)
				if outcome.Commit {
					uh.liveryState.PanelRotate = observed.PanelRotate
					uh.liveryState.DockRotate = observed.DockRotate
				}
				uh.applyLiveryRotateSwitches()
			})
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
	case livery.Panel:
		pathKey, idKey = livery.KeyPanelCustom, livery.KeyPanelID
		enabled = uh.liveryState.PanelEnabled
	default:
		pathKey, idKey = livery.KeyDockCustom, livery.KeyDockID
		enabled = uh.liveryState.DockEnabled
	}
	source := livery.Source{Kind: livery.FromFile, Value: path}
	preview := dryrun.Enabled()

	uh.runLiverySelectionWork(surface, func(generation uint64) {
		ctx, cancel := livery.DefaultContext()
		defer cancel()

		saved := false
		if err := livery.SetString(ctx, pathKey, path); err != nil {
			uh.reportLiveryFailure("saving the file", err)
		} else if err := livery.SetString(ctx, idKey, livery.CustomID); err != nil {
			uh.reportLiveryFailure("saving the selection", err)
		} else {
			saved = true
			if enabled {
				if err := livery.Apply(ctx, surface, source); err != nil {
					uh.reportLiveryFailure("using that file", err)
				} else if surface != livery.Panel {
					if err := livery.RefreshShellIcons(); err != nil {
						uh.reportLiveryFailure("refreshing the shell's icons", err)
					}
				}
			}
		}

		uh.publishLiverySelection(surface, generation, liverystate.Selection(liverystate.Result{Saved: saved}, preview), func() {
			switch surface {
			case livery.AppGrid:
				uh.liveryState.AppGridCustom, uh.liveryState.AppGridSlug = path, livery.CustomID
			case livery.Panel:
				uh.liveryState.PanelCustom, uh.liveryState.PanelID = path, livery.CustomID
			default:
				uh.liveryState.DockCustom, uh.liveryState.DockID = path, livery.CustomID
			}
			uh.showLiveryCustomPath(surface, path)
			if surface != livery.AppGrid {
				uh.restoreLiveryRotateSensitive(surface)
			}
		})
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
func (uh *UserHome) liveryToggleGate(s livery.Surface) (*actionstate.Gate, *gtk.Switch, *gtk.Spinner) {
	switch s {
	case livery.AppGrid:
		return &uh.liveryAppGridGate, uh.liveryAppGridSwitch, uh.liveryAppGridSpinner
	case livery.Panel:
		return &uh.liveryPanelGate, uh.liveryPanelSwitch, uh.liveryPanelSpinner
	default:
		return &uh.liveryDockGate, uh.liveryDockSwitch, uh.liveryDockSpinner
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
//
// The work receives its generation so its completion can publish to the UI
// only while it is still the newest; see publishLiverySelection.
func (uh *UserHome) runLiverySelectionWork(s livery.Surface, work func(generation uint64)) {
	serializer := uh.liverySelectionWork(s)
	generation := serializer.Claim()
	go func() {
		serializer.Run(generation, func() {
			work(generation)
		})
	}()
}

// publishLiverySelection runs commit on the main thread, but only while this
// attempt is still the section's newest.
//
// Run already guarantees currency when the work starts; a newer pick can be
// claimed while it is still running, and this callback is queued rather than
// inline. The re-check is what stops a stale completion from replacing the
// newer confirmed row and selection with an older result.
func (uh *UserHome) publishLiverySelection(s livery.Surface, generation uint64, outcome liverystate.Outcome, commit func()) {
	serializer := uh.liverySelectionWork(s)
	sgtk.RunOnMainThread(func() {
		if !serializer.IsCurrent(generation) {
			return
		}
		if outcome.Commit {
			commit()
		}
	})
}

// finishLiveryToggle releases a section's gate on the main thread.
//
// When the attempt committed, it records the new switch state and updates the
// section's dependent rows. Otherwise it restores the switch the user flipped,
// so a failed or previewed toggle never shows a state that did not land. The
// programmatic restore re-enters the handler, which compares against the
// unchanged confirmed state and does nothing.
func (uh *UserHome) finishLiveryToggle(s livery.Surface, outcome liverystate.Outcome, enabled bool, decision actionmsg.LiveryToggleDecision) {
	sgtk.RunOnMainThread(func() {
		gate, toggle, spinner := uh.liveryToggleGate(s)
		setActivitySpinner(spinner, false)
		if outcome.Commit {
			uh.setLiveryToggleState(s, enabled)
			uh.setLiverySectionSensitive(s, enabled)
		} else if toggle != nil {
			// Revert while the gate still holds, so the re-entrant handler
			// cannot start a second run even if the value comparison missed.
			previous := uh.liverySuppress
			uh.liverySuppress = true
			confirmed, _ := uh.liveryToggleState(s)
			toggle.SetActive(confirmed)
			toggle.SetState(confirmed)
			uh.liverySuppress = previous
		}
		gate.Reset()
		if toggle != nil {
			toggle.SetSensitive(true)
		}
		if decision.Toast != "" {
			uh.toastAdder.ShowToast(decision.Toast)
		}
	})
}

// restoreLiveryRotateSensitive recomputes a rotate switch's sensitivity from
// the confirmed state once its attempt finishes.
//
// A rotation attempt can outlive the section it belongs to: turning the
// section off while the attempt is in flight leaves the switch insensitive,
// so the completion must not blindly make it sensitive again.
func (uh *UserHome) restoreLiveryRotateSensitive(s livery.Surface) {
	if s == livery.Panel {
		uh.syncLiveryRotateSensitive(s, uh.liveryPanelAvailable && uh.liveryState.PanelEnabled)
		return
	}
	uh.syncLiveryRotateSensitive(s, uh.liveryState.DockEnabled)
}

// applyLiveryRotateSwitches puts both rotate switches back to the confirmed
// state and recomputes their sensitivity.
//
// Both keys are written and published as one pair, so the completion that
// owns the pair updates both switches; a section's switch may have been
// optimistically flipped and left alone by a superseded attempt. The
// programmatic set re-enters the handler, which compares against the
// confirmed state just written and does nothing.
func (uh *UserHome) applyLiveryRotateSwitches() {
	previous := uh.liverySuppress
	uh.liverySuppress = true
	defer func() { uh.liverySuppress = previous }()
	if uh.liveryPanelRotate != nil {
		uh.liveryPanelRotate.SetActive(uh.liveryState.PanelRotate)
	}
	if uh.liveryDockRotate != nil {
		uh.liveryDockRotate.SetActive(uh.liveryState.DockRotate)
	}
	uh.restoreLiveryRotateSensitive(livery.Panel)
	uh.restoreLiveryRotateSensitive(livery.Dock)
}

func (uh *UserHome) liveryToggleState(s livery.Surface) (bool, string) {
	return liverystate.ToggleState(uh.liveryState, s)
}

func (uh *UserHome) setLiveryToggleState(s livery.Surface, v bool) {
	liverystate.SetToggleState(&uh.liveryState, s, v)
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

// liverySelectionSource builds the artwork source for a candidate id, before
// that id has been committed to the confirmed state.
//
// liverySource reads the confirmed selection, which is exactly what the
// selection handlers must not do: their candidate is not confirmed until the
// work lands.
func liverySelectionSource(surface livery.Surface, id string) livery.Source {
	if surface == livery.Panel {
		return livery.Source{Kind: livery.FromCatalog, Value: id}
	}
	return livery.Source{Kind: livery.FromCNCF, Value: id}
}

// setLiverySectionSensitive follows the section's master switch, so a
// selection cannot be changed for a mark that is not being set.
func (uh *UserHome) setLiverySectionSensitive(s livery.Surface, enabled bool) {
	switch s {
	case livery.AppGrid:
		if uh.liveryAppGridRow != nil {
			uh.liveryAppGridRow.SetSensitive(enabled)
		}
	case livery.Panel:
		if uh.liveryPanelMarkRow != nil {
			uh.liveryPanelMarkRow.SetSensitive(enabled)
		}
		if uh.liveryFoundationGrid != nil {
			uh.liveryFoundationGrid.SetSensitive(enabled)
		}
		uh.syncLiveryRotateSensitive(s, enabled)
	default:
		if uh.liveryDockSelectedRow != nil {
			uh.liveryDockSelectedRow.SetSensitive(enabled)
		}
		uh.syncLiveryRotateSensitive(s, enabled)
	}
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
