package views

import (
	"context"
	"fmt"
	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
	"log"
	"runtime"
	"time"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// buildAgentsPage builds the Agents page: one switch that turns Agent Mode —
// the llmman model server — on or off. It is a destination of its own
// because what it turns on is a service a person then points other
// applications at, not a system preference.
//
// Nothing here is privileged. The service is a systemd user unit in the
// invoking account, so there is no pkexec route on this page and there must
// not become one — see internal/aistack's package comment.
func (uh *UserHome) buildAgentsPage() {
	page := uh.agentsPrefsPage
	if page == nil {
		return
	}

	if uh.groupEnabled("agents_page", "agents_group") {
		uh.buildAgentModeGroup(page)
	}
}

// buildAgentModeGroup builds the Agent Mode switch and its Details expander.
// Every signal is connected here, once, for the page's lifetime: puregotk's
// callback table is fixed and never releases a slot, so a handler created in
// a path that reruns is a slow walk to a panic.
//
// The group is only built when the capability floor found Homebrew, so the
// facts start capable. Readiness is an HTTP probe and never runs on the GTK
// main thread; until it answers an installed unit reads as provisioning.
func (uh *UserHome) buildAgentModeGroup(page *adw.PreferencesPage) {
	facts := aistack.Observe(true)
	state := aistack.Resolve(facts)
	jan := runtime.GOARCH == "amd64"

	log.Printf("views: agents page built runtime=llmman address=%s state=%d llmman=%v unit=%v jan=%v",
		aistack.Address, state, facts.Installed, facts.UnitPresent, jan)

	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.AgentModeGroupTitle())
	group.SetDescription(pageview.AgentModeGroupDescription())

	row := adw.NewActionRow()
	row.SetTitle(pageview.AgentModeRowTitle())
	uh.agentModeRow = row
	uh.showAgentModeState(state)

	// guardedSwitch, not a bare gtk.Switch: GtkSwitch emits ::state-set from
	// gtk_switch_set_active, so showing the machine's real state here and
	// reverting after a failure are both indistinguishable from a click
	// unless they are marked. Unmarked, a failed stop would revert the switch
	// to on and immediately start the service again for real.
	var toggle *guardedSwitch
	toggle = newGuardedSwitch(state.On(), func(on bool) {
		uh.onAgentModeToggled(on, toggle)
	})
	row.AddSuffix(&toggle.widget.Widget)
	row.SetActivatableWidget(&toggle.widget.Widget)
	group.Add(&row.Widget)

	modelRow := adw.NewActionRow()
	modelRow.SetTitle(pageview.AgentModeActiveModelTitle())
	modelRow.SetSubtitle(pageview.AgentModeActiveModelSubtitle(""))
	uh.agentModelRow = modelRow
	group.Add(&modelRow.Widget)

	presetRow := adw.NewActionRow()
	presetRow.SetTitle(pageview.AgentModePresetsTitle())
	presetRow.SetSubtitle(pageview.AgentModePresetsSubtitle())
	switchBtn := gtk.NewButtonWithLabel(pageview.AgentModeSwitchPresetLabel())
	switchBtn.SetValign(gtk.AlignCenterValue)
	switchBtn.AddCssClass("suggested-action")
	switchClicked := func(_ gtk.Button) {
		uh.presentModelPresetChooser()
	}
	switchBtn.ConnectClicked(&switchClicked)
	presetRow.AddSuffix(&switchBtn.Widget)
	uh.agentPresetRow = presetRow
	group.Add(&presetRow.Widget)

	details := adw.NewExpanderRow()
	details.SetTitle(pageview.AgentModeDetailsTitle())
	for _, detail := range pageview.AgentModeDetails(jan) {
		detailRow := adw.NewActionRow()
		detailRow.SetTitle(detail.Title)
		detailRow.SetSubtitle(detail.Subtitle)
		details.AddRow(&detailRow.Widget)
	}
	group.Add(&details.Widget)
	page.Add(group)

	uh.buildPeersGroup(page)

	if facts.UnitPresent {
		go func() {
			facts.Checked, facts.Healthy = true, aistack.Healthy(context.Background())
			sgtk.RunOnMainThread(func() {
				// A toggle that started, or already finished, meanwhile owns
				// the row; this probe's facts are stale then.
				if !toggle.widget.GetActive() || !uh.agentModeGate.TryStart() {
					return
				}
				uh.showAgentModeState(aistack.Resolve(facts))
				uh.agentModeGate.Reset()
			})
		}()
	}
}

func (uh *UserHome) showAgentModeState(state aistack.State) {
	uh.agentModeState = state
	if uh.agentModeRow != nil {
		uh.agentModeRow.SetSubtitle(pageview.AgentModeSubtitle(state))
	}
	running := state == aistack.StateReady
	if uh.agentModelRow != nil {
		uh.agentModelRow.SetVisible(running)
		// The row shows the model the server is actually serving, read from
		// the configured alias, not the catalog default. Read off the main
		// thread: it shells out to llmman.
		if running {
			go func() {
				ctx, cancel := aistack.DefaultContext()
				defer cancel()
				modelRef, err := aistack.ReadActiveModel(ctx)
				if err != nil {
					return
				}
				sgtk.RunOnMainThread(func() {
					if uh.agentModelRow != nil {
						uh.agentModelRow.SetSubtitle(pageview.AgentModeActiveModelSubtitle(modelRef))
					}
				})
			}()
		}
	}
	if uh.agentPresetRow != nil {
		uh.agentPresetRow.SetVisible(running)
	}
}

func (uh *UserHome) presentModelPresetChooser() {
	if !uh.agentPresetGate.TryStart() {
		return
	}

	dialog := adw.NewAlertDialog(
		"Switch Model Preset",
		"Select a recommended model family. The model will be verified against system memory and downloaded via llmman.",
	)
	dialog.AddResponse("cancel", "Cancel")
	for _, fam := range aistack.Families() {
		dialog.AddResponse(string(fam), fam.DisplayName())
	}

	responseCb := func(_ adw.AlertDialog, response string) {
		// Reset only for the non-worker path: cancel, or any response that is
		// not a family. A family response hands the gate to the worker, whose
		// defer owns the reset once a pull actually starts, so a second preset
		// cannot begin while the first is still pulling.
		if response == "cancel" {
			uh.agentPresetGate.Reset()
			return
		}
		fam := aistack.Family(response)
		go uh.applyModelFamilyPreset(fam)
	}
	dialog.ConnectResponse(&responseCb)
	if uh.agentsPrefsPage != nil {
		dialog.Present(&uh.agentsPrefsPage.Widget)
	}
}

func (uh *UserHome) applyModelFamilyPreset(fam aistack.Family) {
	// The worker owns the preset gate from here until it returns, so a
	// second preset cannot start while this one is still pulling.
	defer uh.agentPresetGate.Reset()

	ctx, cancel := aistack.DefaultContext()
	defer cancel()

	sgtk.RunOnMainThread(func() {
		if uh.agentModelRow != nil {
			uh.agentModelRow.SetSubtitle(fmt.Sprintf("Switching to %s…", fam.DisplayName()))
		}
	})

	// Get node status for memory fitting. Without the memory we cannot fit a
	// model to the machine, so a failure to reach the server aborts rather
	// than silently picking the largest model.
	status, err := aistack.FetchNodeStatus(ctx)
	if err != nil {
		log.Printf("views: fetch node status failed: %v", err)
		sgtk.RunOnMainThread(func() {
			uh.toastAdder.ShowErrorToast("Could not reach the model server to check memory")
		})
		return
	}
	candidate, err := aistack.ResolveCandidate(ctx, fam, status.Memory, aistack.DefaultFetch)
	if err != nil {
		log.Printf("views: resolve candidate failed: %v", err)
		sgtk.RunOnMainThread(func() {
			uh.toastAdder.ShowErrorToast(fmt.Sprintf("Could not find a matching model for %s", fam.DisplayName()))
		})
		return
	}
	modelRef := candidate.ModelRef()
	dryRun := dryrun.Enabled()
	if dryRun {
		log.Printf("[DRY-RUN] would configure alias %s to %s and pull", aistack.ActiveModelAlias, modelRef)
		sgtk.RunOnMainThread(func() {
			if uh.agentModelRow != nil {
				uh.agentModelRow.SetSubtitle(pageview.AgentModeActiveModelSubtitle(modelRef))
			}
			uh.toastAdder.ShowToast(fmt.Sprintf("[DRY-RUN] Would switch to %s", modelRef))
		})
		return
	}

	// Pull first, then set the alias. If the pull fails the alias is left
	// untouched, so Agent Mode keeps serving the previously working model
	// instead of pointing at something that is not there.
	if err := aistack.PullModel(ctx, modelRef); err != nil {
		log.Printf("views: pull model failed: %v", err)
		sgtk.RunOnMainThread(func() {
			uh.toastAdder.ShowErrorToast(fmt.Sprintf("Failed to pull model %s", modelRef))
		})
		return
	}

	if err := aistack.ConfigureActiveModel(ctx, modelRef); err != nil {
		log.Printf("views: configure alias failed: %v", err)
		sgtk.RunOnMainThread(func() {
			uh.toastAdder.ShowErrorToast("Could not configure active model alias")
		})
		return
	}

	sgtk.RunOnMainThread(func() {
		if uh.agentModelRow != nil {
			uh.agentModelRow.SetSubtitle(pageview.AgentModeActiveModelSubtitle(modelRef))
		}
		uh.toastAdder.ShowToast(fmt.Sprintf("Active model switched to %s", fam.DisplayName()))
	})
}

// onAgentModeToggled sets Agent Mode up or tears it down off the main thread.
// Enabling waits, bounded, for /llmman/node before calling the result ready;
// a service that started but does not answer is shown as degraded rather
// than as working.
//
// Turning it off can fail in a way that leaves the service running — the
// stop failed and a follow-up check could not prove otherwise — and
// internal/aistack deliberately preserves the unit in that case. The failure
// path here matches it: the switch goes back on and the text says so.
func (uh *UserHome) onAgentModeToggled(enabled bool, toggle *guardedSwitch) {
	if !uh.agentModeGate.TryStart() {
		return
	}
	toggle.widget.SetSensitive(false)
	uh.agentModeRow.SetSubtitle(pageview.AgentModeWorkingSubtitle(enabled))
	dryRun := dryrun.Enabled()

	go func() {
		defer uh.agentModeGate.Reset()

		ctx, cancel := aistack.DefaultContext()
		defer cancel()

		var err error
		if enabled {
			err = aistack.Enable(ctx)
		} else {
			err = aistack.Disable(ctx)
		}
		facts := aistack.Observe(true)
		if err == nil && !dryRun && facts.UnitPresent {
			facts.Checked, facts.Healthy = true, aistack.WaitHealthy(ctx, agentModeReadyWait)
		}

		sgtk.RunOnMainThread(func() {
			toggle.widget.SetSensitive(true)

			if err != nil {
				// The error names commands and unit files; it is logged,
				// and the toast says what actually happened.
				log.Printf("views: agent mode toggle to %v failed: %v", enabled, err)
				toggle.set(!enabled)
				uh.agentModeRow.SetSubtitle(pageview.AgentModeFailureSubtitle(enabled))
				uh.toastAdder.ShowErrorToast(pageview.AgentModeFailureToast(enabled))
				return
			}

			decision := actionmsg.AgentMode(dryRun, enabled)
			toggle.set(decision.Confirm == enabled)
			if decision.Confirm {
				uh.showAgentModeState(aistack.Resolve(facts))
			} else {
				uh.showAgentModeState(uh.agentModeState)
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

// agentModeReadyWait bounds how long an enable waits for the daemon to
// answer before reporting it degraded.
const agentModeReadyWait = time.Minute

// buildPeersGroup builds "Use another machine": the list of already-
// configured llmman peers this host may route requests to, plus the shared
// key sent to an authenticated one. It is built unconditionally alongside
// the Agent Mode switch — offload is configured independently of whether
// this host's own daemon is currently on, since a disabled local daemon can
// still forward through llmman once turned on.
func (uh *UserHome) buildPeersGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.PeersGroupTitle())
	group.SetDescription(pageview.PeersGroupDescription())
	uh.peersGroup = group

	addRow := adw.NewActionRow()
	addRow.SetTitle(pageview.PeersAddRowTitle())
	addRow.SetActivatable(true)
	addIcon := gtk.NewImageFromIconName("list-add-symbolic")
	addRow.AddSuffix(&addIcon.Widget)
	addActivated := func(_ adw.ActionRow) { uh.presentAddPeerDialog() }
	addRow.ConnectActivated(&addActivated)
	group.Add(&addRow.Widget)

	keyRow := adw.NewPasswordEntryRow()
	keyRow.SetTitle(pageview.PeersAPIKeyRowTitle())
	uh.peersKeyEntry = keyRow
	keyApply := func(_ adw.EntryRow) { uh.savePeerAPIKey() }
	keyRow.ConnectApply(&keyApply)
	group.Add(&keyRow.Widget)

	empty := adw.NewActionRow()
	empty.SetTitle(pageview.PeersEmptyRowTitle())
	empty.SetSensitive(false)
	uh.peersEmptyRow = empty
	uh.peersEmptyRowShown = false

	page.Add(group)
	uh.refreshPeersList()
}

// refreshPeersList rebuilds the peer rows from disk and kicks off a status
// probe for each. Safe to call repeatedly; it removes every row it
// previously added before re-adding the current set. Main thread only.
func (uh *UserHome) refreshPeersList() {
	group := uh.peersGroup
	if group == nil {
		return
	}
	for _, row := range uh.peersListRows {
		group.Remove(&row.Widget)
	}
	uh.peersListRows = nil
	if uh.peersEmptyRowShown {
		group.Remove(&uh.peersEmptyRow.Widget)
		uh.peersEmptyRowShown = false
	}

	peers, err := aistack.Peers()
	if err != nil {
		log.Printf("views: listing agent mode peers: %v", err)
	}
	if len(peers) == 0 {
		group.Add(&uh.peersEmptyRow.Widget)
		uh.peersEmptyRowShown = true
		return
	}

	// Read on the main thread: puregotk widgets, including this entry, must
	// never be touched from the probe goroutines started below.
	apiKey := uh.peerAPIKeyForProbe()

	for _, peer := range peers {
		address := peer.Address
		enabled := peer.Enabled
		row := adw.NewActionRow()
		row.SetTitle(address)
		if enabled {
			row.SetSubtitle(pageview.PeerStatusSubtitle(false, aistack.PeerStatus{}))
		} else {
			row.SetSubtitle(pageview.PeerDisabledSubtitle())
		}

		// newGuardedSwitch, not a bare gtk.Switch: without it, a revert of
		// this row's own optimistic state (refreshPeersList rebuilding from
		// the store after a failed toggle) would re-enter ::state-set and
		// fire setPeerEnabled a second time for the same click.
		var enabledSwitch *guardedSwitch
		enabledSwitch = newGuardedSwitch(peer.Enabled, func(state bool) {
			uh.setPeerEnabled(address, state, enabledSwitch)
		})
		row.AddSuffix(&enabledSwitch.widget.Widget)

		removeBtn := gtk.NewButtonFromIconName("user-trash-symbolic")
		removeBtn.SetValign(gtk.AlignCenterValue)
		removeBtn.AddCssClass("flat")
		removeClicked := func(_ gtk.Button) { uh.confirmRemovePeer(address) }
		removeBtn.ConnectClicked(&removeClicked)
		row.AddSuffix(&removeBtn.Widget)

		group.Add(&row.Widget)
		uh.peersListRows = append(uh.peersListRows, row)

		// Only an enabled peer is currently sent prompts by llmman, and
		// probing it still sends the shared key in the clear over plain
		// http:// unless the address is https://. A disabled peer is left
		// showing "Not checked" rather than being probed for no operational
		// reason.
		if !enabled {
			continue
		}
		go func(address string, row *adw.ActionRow) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			status := aistack.ProbePeer(ctx, address, apiKey)
			sgtk.RunOnMainThread(func() {
				row.SetSubtitle(pageview.PeerStatusSubtitle(true, status))
			})
		}(address, row)
	}
}

// peerAPIKeyForProbe returns the key most recently entered in this session,
// if any. ChairLift never reads a stored key back from llmman, so a probe
// after restart is unauthenticated unless the key is re-entered; that
// matches ADR-0015's "never read back" rule for this one credential.
func (uh *UserHome) peerAPIKeyForProbe() string {
	if uh.peersKeyEntry == nil {
		return ""
	}
	return uh.peersKeyEntry.GetText()
}

// presentAddPeerDialog opens the add-peer prompt. Built fresh each time:
// unlike the Agent Mode switch, this dialog carries no long-lived state
// that a rebuilt callback slot would orphan mid-flight, and rebuilding
// avoids stale text left over from a previous, cancelled attempt.
func (uh *UserHome) presentAddPeerDialog() {
	dialog := adw.NewAlertDialog(pageview.PeerAddDialogTitle(), pageview.PeerAddDialogBody())
	entry := adw.NewEntryRow()
	entry.SetTitle(pageview.PeerAddDialogPlaceholder())
	dialog.SetExtraChild(&entry.Widget)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("add", "Add")
	dialog.SetResponseAppearance("add", adw.ResponseSuggestedValue)
	responseCb := func(_ adw.AlertDialog, response string) {
		if response != "add" {
			return
		}
		uh.addPeer(entry.GetText())
	}
	dialog.ConnectResponse(&responseCb)
	dialog.Present(&uh.agentsPrefsPage.Widget)
}

// addPeer validates and adds one peer off the main thread, then refreshes
// the list. A rejected address or a failed llmman config command surfaces
// as a toast naming the reason; neither leaves a partial row behind.
func (uh *UserHome) addPeer(address string) {
	if !uh.peersMutateGate.TryStart() {
		uh.toastAdder.ShowErrorToast(pageview.PeerBusyToast())
		return
	}
	go func() {
		defer uh.peersMutateGate.Reset()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := aistack.AddPeer(ctx, address)
		sgtk.RunOnMainThread(func() {
			if err != nil {
				log.Printf("views: adding agent mode peer: %v", err)
				uh.toastAdder.ShowErrorToast(pageview.PeerAddFailedToast(err.Error()))
				return
			}
			uh.refreshPeersList()
		})
	}()
}

// confirmRemovePeer asks before removing a configured peer; removal is
// reversible only by re-adding the address, so it is treated like the
// destructive actions elsewhere in the app.
func (uh *UserHome) confirmRemovePeer(address string) {
	dialog := adw.NewAlertDialog(pageview.PeerRemoveConfirmTitle(address), pageview.PeerRemoveConfirmBody())
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("remove", "Remove")
	dialog.SetResponseAppearance("remove", adw.ResponseDestructiveValue)
	responseCb := func(_ adw.AlertDialog, response string) {
		if response != "remove" {
			return
		}
		uh.removePeer(address)
	}
	dialog.ConnectResponse(&responseCb)
	dialog.Present(&uh.agentsPrefsPage.Widget)
}

func (uh *UserHome) removePeer(address string) {
	if !uh.peersMutateGate.TryStart() {
		uh.toastAdder.ShowErrorToast(pageview.PeerBusyToast())
		return
	}
	go func() {
		defer uh.peersMutateGate.Reset()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := aistack.RemovePeer(ctx, address)
		sgtk.RunOnMainThread(func() {
			if err != nil {
				log.Printf("views: removing agent mode peer: %v", err)
				uh.toastAdder.ShowErrorToast(pageview.PeerRemoveFailedToast(err.Error()))
				return
			}
			uh.refreshPeersList()
		})
	}()
}

// setPeerEnabled turns one peer on or off. The switch already shows the
// requested state optimistically via GTK's own state-set handling; a
// failure here is corrected by refreshPeersList rebuilding from the store's
// real contents.
func (uh *UserHome) setPeerEnabled(address string, enabled bool, sw *guardedSwitch) {
	if !uh.peersMutateGate.TryStart() {
		// Another peer mutation is already in flight. The switch has
		// already rendered the requested state via GTK's own
		// gtk_switch_set_active; revert it and say why, rather than
		// leaving it showing a state nothing is applying.
		sw.set(!enabled)
		uh.toastAdder.ShowErrorToast(pageview.PeerBusyToast())
		return
	}
	go func() {
		defer uh.peersMutateGate.Reset()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := aistack.SetPeerEnabled(ctx, address, enabled)
		sgtk.RunOnMainThread(func() {
			if err != nil {
				log.Printf("views: setting agent mode peer %s enabled=%v: %v", address, enabled, err)
				if enabled {
					uh.toastAdder.ShowErrorToast(pageview.PeerEnableFailedToast(err.Error()))
				} else {
					uh.toastAdder.ShowErrorToast(pageview.PeerDisableFailedToast(err.Error()))
				}
			}
			uh.refreshPeersList()
		})
	}()
}

// savePeerAPIKey sends the shared credential to llmman's own configuration.
// The key is never stored by ChairLift and never logged; only success or
// failure is reported. The field is left as typed — ChairLift never reads a
// stored key back, so clearing it here would make a later probe look
// unauthenticated even though the key was accepted.
func (uh *UserHome) savePeerAPIKey() {
	if uh.peersKeyEntry == nil {
		return
	}
	key := uh.peersKeyEntry.GetText()
	if key == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := aistack.SetPeerAPIKey(ctx, key)
		sgtk.RunOnMainThread(func() {
			if err != nil {
				log.Printf("views: saving agent mode peer key: %v", err)
				uh.toastAdder.ShowErrorToast(pageview.PeerKeyFailedToast(err.Error()))
				return
			}
			uh.toastAdder.ShowToast(pageview.PeerKeySavedToast())
		})
	}()
}
