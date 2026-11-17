package views

import (
	"context"
	"fmt"
	"log"
	"time"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
)

// Agent Mode is a loopback model server managed by a systemd user unit;
// Troubleshooting is Goose on that server's model. Active Model lets users
// select from downloaded models; Manage Models links to llmman's web UI.
// No operation on this page is privileged.
func (uh *UserHome) buildAgentsPage() {
	page := uh.agentsPrefsPage
	if page == nil {
		return
	}
	agents := uh.groupEnabled("agents_page", "agents_group")
	if agents {
		uh.buildAgentModeGroup(page)
	}
	if uh.groupEnabled("agents_page", "troubleshooting_group") {
		uh.buildTroubleshootGroup(page)
	}
	if agents {
		uh.buildContributeGroup(page)
	}
}

// Signals are connected once; readiness and model reads run off the GTK thread.
func (uh *UserHome) buildAgentModeGroup(page *adw.PreferencesPage) {
	facts := aistack.Observe(true)
	state := aistack.Resolve(facts)
	log.Printf("views: agents page built runtime=llmman address=%s state=%d llmman=%v unit=%v",
		aistack.Address, state, facts.Installed, facts.UnitPresent)

	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.AgentModeGroupTitle())
	group.SetDescription(pageview.AgentModeGroupDescription())

	row := adw.NewActionRow()
	row.SetTitle(pageview.AgentModeRowTitle())
	uh.agentModeRow = row
	uh.agentModeSpinner = newActivitySpinner()
	row.AddSuffix(&uh.agentModeSpinner.Widget)
	// Do not present an installed file as a running service while checking.
	uh.agentModeToggle = newGuardedSwitch(false, func(on bool) {
		uh.onAgentModeToggled(on, uh.agentModeToggle)
	})
	row.AddSuffix(&uh.agentModeToggle.widget.Widget)
	row.SetActivatableWidget(&uh.agentModeToggle.widget.Widget)
	group.Add(&row.Widget)

	modelRow := adw.NewComboRow()
	modelRow.SetTitle(pageview.AgentModeActiveModelTitle())
	uh.agentModelRow = modelRow
	uh.agentModelSpinner = newActivitySpinner()
	uh.agentModelSelected = gtk.INVALID_LIST_POSITION
	modelRow.AddSuffix(&uh.agentModelSpinner.Widget)
	uh.agentModelNotify = func(_ gobject.Object, _ uintptr) {
		uh.onAgentModelSelected()
	}
	modelRow.ConnectNotify(&uh.agentModelNotify)
	group.Add(&modelRow.Widget)

	// Models, chat, and everything else llmman manages live in its own web
	// UI; ChairLift links there rather than growing a second copy (#568).
	manageRow := adw.NewActionRow()
	manageRow.SetTitle(pageview.AgentModeManageTitle())
	manageRow.SetSubtitle(pageview.AgentModeManageSubtitle())
	manageBtn := gtk.NewButtonWithLabel(pageview.AgentModeManageLabel())
	manageBtn.SetValign(gtk.AlignCenterValue)
	manageClicked := func(_ gtk.Button) { uh.openURL(aistack.WebUIURL()) }
	manageBtn.ConnectClicked(&manageClicked)
	manageRow.AddSuffix(&manageBtn.Widget)
	manageRow.SetActivatableWidget(&manageBtn.Widget)
	// AdwActionRow labels its activatable widget by the row title. The
	// title matches the button label today, so the reset changes nothing
	// audible; it keeps the button named by its own visible label if the
	// title and label ever diverge (WCAG 2.5.3).
	manageBtn.ResetRelation(gtk.AccessibleRelationLabelledByValue)
	manageRow.SetVisible(false)
	uh.agentManageRow = manageRow
	group.Add(&manageRow.Widget)

	address := adw.NewActionRow()
	address.SetTitle("Local Connection")
	address.SetSubtitle("http://" + aistack.Address + "/v1")
	address.SetSubtitleSelectable(true)
	group.Add(&address.Widget)
	page.Add(group)
	uh.showAgentModeState(state)

	if !facts.UnitPresent {
		uh.agentModeToggle.set(state.On())
		return
	}
	uh.agentModeToggle.widget.SetSensitive(false)
	setActivitySpinner(uh.agentModeSpinner, true)
	generation := uh.agentRefresh.Begin()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		migrationErr := aistack.ReconcileService(ctx)
		facts.Checked = true
		if migrationErr == nil {
			if dryrun.Enabled() {
				facts.Healthy = aistack.Healthy(ctx)
			} else {
				facts.Healthy = aistack.WaitHealthy(ctx, 10*time.Second)
			}
		}
		sgtk.RunOnMainThread(func() {
			if !uh.agentRefresh.IsCurrent(generation) {
				return
			}
			state := aistack.Resolve(facts)
			uh.agentModeToggle.set(state.On())
			uh.agentModeToggle.widget.SetSensitive(true)
			setActivitySpinner(uh.agentModeSpinner, false)
			uh.showAgentModeState(state)
			if migrationErr != nil {
				log.Printf("views: local-only model service migration failed: %v", migrationErr)
				uh.agentModeRow.SetSubtitle("Agent Mode needs an update. Turn it off and on again.")
				uh.toastAdder.ShowErrorToast("Couldn't update Agent Mode. Turn it off and on again.")
			}
		})
	}()
}

func (uh *UserHome) showAgentModeState(state aistack.State) {
	uh.agentModeState = state
	generation := uh.agentRefresh.Begin()
	uh.agentModeRow.SetSubtitle(pageview.AgentModeSubtitle(state))
	ready := state == aistack.StateReady
	uh.agentModelSuppress = true
	uh.agentModelRow.SetSensitive(ready)
	uh.agentModelRow.SetSubtitle(pageview.AgentModeModelUnavailable(state))
	uh.agentModelSuppress = false
	if uh.agentManageRow != nil {
		uh.agentManageRow.SetVisible(ready)
	}
	// Goose runs on this server's model; its row follows every change.
	uh.refreshGooseState()
	if !ready {
		return
	}
	uh.agentModelSuppress = true
	uh.agentModelRow.SetSubtitle("Checking…")
	uh.agentModelSuppress = false
	go func() {
		ctx, cancel := aistack.DefaultContext()
		defer cancel()
		node, errNode := aistack.FetchNodeStatus(ctx)
		modelRef, errAlias := aistack.ReadActiveModel(ctx)
		sgtk.RunOnMainThread(func() {
			if !uh.agentRefresh.IsCurrent(generation) {
				return
			}
			if errNode != nil || errAlias != nil {
				log.Printf("views: read active model failed: node=%v alias=%v", errNode, errAlias)
				uh.agentModelSuppress = true
				uh.agentModelRow.SetSensitive(false)
				uh.agentModelRow.SetSubtitle("Couldn't check the model. Use Manage Models to pick one.")
				uh.agentModelSuppress = false
				return
			}
			models := node.Models()
			// A lone model with no active model yet cannot be chosen from the
			// row (GTK snaps a single-item combo to its only item, so the
			// choice reads back as "no change"); make it active here.
			if target := aistack.SingleModelToActivate(models, modelRef); target != "" {
				uh.activateSingleModel(target)
				return
			}
			uh.agentModelSuppress = true
			uh.agentModelList = models
			uh.agentModelActive = modelRef
			uh.agentModelRow.SetModel(gtk.NewStringList(models))
			if len(models) == 0 {
				uh.agentModelRow.SetSensitive(false)
				uh.agentModelRow.SetSubtitle(pageview.AgentModeNoModelsStoredSubtitle())
			} else {
				uh.agentModelRow.SetSensitive(true)
				selectedIndex := -1
				for i, m := range models {
					if m == modelRef || "hf.co/"+m == modelRef || m == "hf.co/"+modelRef {
						selectedIndex = i
						break
					}
				}
				if selectedIndex >= 0 {
					uh.agentModelRow.SetSelected(uint32(selectedIndex))
					uh.agentModelRow.SetSubtitle(pageview.AgentModeActiveModelSubtitle(modelRef))
				} else {
					uh.agentModelRow.SetSelected(gtk.INVALID_LIST_POSITION)
					uh.agentModelRow.SetSubtitle(pageview.AgentModeActiveModelSubtitle(modelRef))
				}
				uh.agentModelSelected = uh.agentModelRow.GetSelected()
			}
			uh.agentModelSuppress = false
		})
	}()
	uh.refreshGooseState()
}

func (uh *UserHome) onAgentModelSelected() {
	if uh.agentModelSuppress || uh.agentModeState != aistack.StateReady {
		return
	}
	pos := uh.agentModelRow.GetSelected()
	if pos == gtk.INVALID_LIST_POSITION || int(pos) >= len(uh.agentModelList) || pos == uh.agentModelSelected {
		return
	}
	targetModel := uh.agentModelList[pos]
	if targetModel == uh.agentModelActive || "hf.co/"+targetModel == uh.agentModelActive || targetModel == "hf.co/"+uh.agentModelActive {
		return
	}
	if !uh.agentModeGate.TryStart() {
		return
	}
	uh.agentModelSelected = pos
	setActivitySpinner(uh.agentModelSpinner, true)
	uh.agentModeToggle.widget.SetSensitive(false)
	uh.agentModelRow.SetSensitive(false)
	uh.agentRefresh.Begin()
	go func() {
		ctx, cancel := aistack.DefaultContext()
		defer cancel()
		var err error
		if dryrun.Enabled() {
			log.Printf("[DRY-RUN] would configure alias %s to %s", aistack.ActiveModelAlias, targetModel)
		} else {
			err = aistack.ConfigureActiveModel(ctx, targetModel)
		}
		sgtk.RunOnMainThread(func() {
			uh.agentModelSuppress = true
			uh.agentModeGate.Reset()
			uh.agentModeToggle.widget.SetSensitive(true)
			uh.agentModelRow.SetSensitive(true)
			setActivitySpinner(uh.agentModelSpinner, false)
			if err != nil {
				log.Printf("views: configure active model failed: %v", err)
				uh.toastAdder.ShowErrorToast("Couldn't select the model.")
			} else if dryrun.Enabled() {
				uh.toastAdder.ShowToast(fmt.Sprintf("[DRY-RUN] Would switch to %s", targetModel))
			} else {
				uh.agentModelActive = targetModel
				uh.toastAdder.ShowToast(fmt.Sprintf("Selected %s.", targetModel))
			}
			uh.showAgentModeState(uh.agentModeState)
			uh.agentModelSuppress = false
		})
	}()
}

func (uh *UserHome) activateSingleModel(target string) {
	// The single-item row cannot register this choice, so ChairLift sets the
	// alias itself. The gate keeps it from overlapping a user's own selection.
	if !uh.agentModeGate.TryStart() {
		return
	}
	uh.agentModelSuppress = true
	uh.agentModelRow.SetSensitive(false)
	uh.agentModeToggle.widget.SetSensitive(false)
	setActivitySpinner(uh.agentModelSpinner, true)
	generation := uh.agentRefresh.Begin()
	go func() {
		defer uh.agentModeGate.Reset()
		ctx, cancel := aistack.DefaultContext()
		defer cancel()
		var err error
		if dryrun.Enabled() {
			log.Printf("[DRY-RUN] would configure alias %s to %s (auto-selected single model)", aistack.ActiveModelAlias, target)
		} else {
			err = aistack.ConfigureActiveModel(ctx, target)
		}
		sgtk.RunOnMainThread(func() {
			if !uh.agentRefresh.IsCurrent(generation) {
				return
			}
			uh.agentModelSuppress = false
			uh.agentModeToggle.widget.SetSensitive(true)
			uh.agentModelRow.SetSensitive(true)
			setActivitySpinner(uh.agentModelSpinner, false)
			if err != nil {
				log.Printf("views: auto-select active model failed: %v", err)
				uh.toastAdder.ShowErrorToast("Couldn't select the model.")
				return
			}
			if dryrun.Enabled() {
				// The alias is not written in dry-run, so a refresh would
				// re-trigger the auto-select; stop here instead.
				uh.toastAdder.ShowToast(fmt.Sprintf("[DRY-RUN] Would switch to %s", target))
				return
			}
			uh.toastAdder.ShowToast(fmt.Sprintf("Selected %s.", target))
			uh.showAgentModeState(uh.agentModeState)
		})
	}()
}

func (uh *UserHome) onAgentModeToggled(enabled bool, toggle *guardedSwitch) {
	if !uh.agentModeGate.TryStart() {
		toggle.set(uh.agentModeState.On())
		return
	}
	previous := uh.agentModeState
	uh.agentRefresh.Begin()
	toggle.widget.SetSensitive(false)
	uh.agentModelRow.SetSensitive(false)
	uh.agentModeRow.SetSubtitle(pageview.AgentModeWorkingSubtitle(enabled))
	uh.agentModelRow.SetSubtitle("Waiting for Agent Mode…")
	setActivitySpinner(uh.agentModeSpinner, true)
	dryRun := dryrun.Enabled()
	go func() {
		ctx, cancel := aistack.DefaultContext()
		defer cancel()
		var err error
		if enabled {
			err = aistack.Enable(ctx)
		} else {
			err = aistack.Disable(ctx)
		}
		facts := aistack.Observe(true)
		if !dryRun && facts.UnitPresent {
			facts.Checked = true
			if enabled && err == nil {
				facts.Healthy = aistack.WaitHealthy(ctx, agentModeReadyWait)
			} else {
				facts.Healthy = aistack.Healthy(context.Background())
			}
		}
		sgtk.RunOnMainThread(func() {
			uh.agentModeGate.Reset()
			setActivitySpinner(uh.agentModeSpinner, false)
			toggle.widget.SetSensitive(true)
			state := aistack.Resolve(facts)
			if dryRun {
				state = previous
			}
			toggle.set(state.On())
			uh.showAgentModeState(state)
			if enabled && !dryRun {
				// Enabling installs llmman through Homebrew when it is
				// missing, possibly before a later step fails.
				uh.homebrewInventoryChanged()
			}
			if err != nil {
				log.Printf("views: agent mode toggle to %v failed: %v", enabled, err)
				uh.toastAdder.ShowErrorToast(pageview.AgentModeFailureToast(enabled))
				return
			}
			if enabled && !dryRun && state != aistack.StateReady {
				uh.toastAdder.ShowErrorToast("Agent Mode didn't start. Turn it off and on again.")
				return
			}
			uh.toastAdder.ShowToast(actionmsg.AgentMode(dryRun, enabled).Toast)
		})
	}()
}

const agentModeReadyWait = time.Minute
