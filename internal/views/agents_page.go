package views

import (
	"context"
	"fmt"
	"log"
	"time"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
)

// Agent Mode is a loopback model server managed by a systemd user unit;
// Troubleshooting is Goose on that server's model. No operation on this page is
// privileged.
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

	modelRow := adw.NewActionRow()
	modelRow.SetTitle(pageview.AgentModeActiveModelTitle())
	modelRow.SetUseMarkup(false)
	uh.agentModelRow = modelRow
	group.Add(&modelRow.Widget)

	presetRow := adw.NewActionRow()
	presetRow.SetTitle(pageview.AgentModePresetsTitle())
	uh.agentPresetSpinner = newActivitySpinner()
	presetRow.AddSuffix(&uh.agentPresetSpinner.Widget)
	choose := gtk.NewButtonWithLabel(pageview.AgentModeSwitchPresetLabel())
	choose.SetValign(gtk.AlignCenterValue)
	clicked := func(_ gtk.Button) { uh.presentModelPresetChooser() }
	choose.ConnectClicked(&clicked)
	presetRow.AddSuffix(&choose.Widget)
	uh.agentPresetRow = presetRow
	group.Add(&presetRow.Widget)

	// Models, chat, and everything else llmman manages live in its own web
	// UI; ChairLift links there rather than growing a second copy.
	manageRow := adw.NewActionRow()
	manageRow.SetTitle(pageview.AgentModeManageTitle())
	manageRow.SetSubtitle(pageview.AgentModeManageSubtitle())
	manageBtn := gtk.NewButtonWithLabel(pageview.AgentModeManageLabel())
	manageBtn.SetValign(gtk.AlignCenterValue)
	manageClicked := func(_ gtk.Button) { uh.openURL(aistack.WebUIURL()) }
	manageBtn.ConnectClicked(&manageClicked)
	manageRow.AddSuffix(&manageBtn.Widget)
	manageRow.SetActivatableWidget(&manageBtn.Widget)
	manageRow.SetVisible(false)
	uh.agentManageRow = manageRow
	group.Add(&manageRow.Widget)

	address := adw.NewActionRow()
	address.SetTitle("Local connection")
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
	uh.agentModelRow.SetSensitive(ready)
	uh.agentPresetRow.SetSensitive(ready)
	uh.agentModelRow.SetSubtitle(pageview.AgentModeModelUnavailable(state))
	uh.agentPresetRow.SetSubtitle(pageview.AgentModeModelUnavailable(state))
	if uh.agentManageRow != nil {
		uh.agentManageRow.SetVisible(ready)
	}
	// Goose runs on this server's model; its row follows every change.
	uh.refreshGooseState()
	if !ready {
		return
	}
	uh.agentPresetRow.SetSubtitle(pageview.AgentModePresetsSubtitle())
	uh.agentModelRow.SetSubtitle("Checking…")
	go func() {
		ctx, cancel := aistack.DefaultContext()
		defer cancel()
		modelRef, err := aistack.ReadActiveModel(ctx)
		sgtk.RunOnMainThread(func() {
			if !uh.agentRefresh.IsCurrent(generation) {
				return
			}
			if err != nil {
				log.Printf("views: read active model failed: %v", err)
				uh.agentModelRow.SetSubtitle("Couldn't check the model. Choose one below.")
				return
			}
			uh.agentModelRow.SetSubtitle(pageview.AgentModeActiveModelSubtitle(modelRef))
		})
	}()
}

func (uh *UserHome) presentModelPresetChooser() {
	if uh.agentModeState != aistack.StateReady || !uh.agentPresetGate.TryStart() {
		return
	}
	dialog := adw.NewAlertDialog("Choose a Model", "Pick a model family. A size that fits this computer will be downloaded.")
	dialog.AddResponse("cancel", "Cancel")
	dialog.SetCloseResponse("cancel")
	for _, fam := range aistack.Families() {
		dialog.AddResponse(string(fam), fam.DisplayName())
	}
	uh.agentPresetDialogs.connect(dialog, func(response string) {
		fam := aistack.Family(response)
		valid := false
		for _, available := range aistack.Families() {
			valid = valid || fam == available
		}
		if !valid {
			uh.agentPresetGate.Reset()
			return
		}
		// The chooser may have remained open while a toggle changed readiness.
		// One mutation gate protects the service and model configuration together.
		if uh.agentModeState != aistack.StateReady || !uh.agentModeGate.TryStart() {
			uh.agentPresetGate.Reset()
			uh.toastAdder.ShowErrorToast("Wait until Agent Mode is ready before choosing a model.")
			return
		}
		uh.agentRefresh.Begin()
		previous := uh.agentModelRow.GetSubtitle()
		uh.agentModeToggle.widget.SetSensitive(false)
		uh.agentPresetRow.SetSensitive(false)
		uh.agentModelRow.SetSubtitle(fmt.Sprintf("Downloading a %s model…", fam.DisplayName()))
		setActivitySpinner(uh.agentPresetSpinner, true)
		go uh.applyModelFamilyPreset(fam, previous)
	})
	dialog.Present(&uh.agentsPrefsPage.Widget)
}

// Pull first, then configure the alias. Failed pulls and previews keep the
// previous model; controls and gates are restored together on the GTK thread.
func (uh *UserHome) applyModelFamilyPreset(fam aistack.Family, previous string) {
	ctx, cancel := aistack.DefaultContext()
	defer cancel()
	finish := func(modelRef, message string, failed bool) {
		facts := aistack.Observe(true)
		if facts.UnitPresent {
			facts.Checked, facts.Healthy = true, aistack.Healthy(context.Background())
		}
		sgtk.RunOnMainThread(func() {
			uh.agentModeGate.Reset()
			uh.agentPresetGate.Reset()
			uh.agentModeToggle.widget.SetSensitive(true)
			setActivitySpinner(uh.agentPresetSpinner, false)
			state := aistack.Resolve(facts)
			uh.agentModeToggle.set(state.On())
			uh.showAgentModeState(state)
			if state == aistack.StateReady {
				if modelRef == "" {
					uh.agentModelRow.SetSubtitle(previous)
				} else {
					uh.agentModelRow.SetSubtitle(pageview.AgentModeActiveModelSubtitle(modelRef))
				}
			}
			if failed {
				uh.toastAdder.ShowErrorToast(message)
			} else {
				uh.toastAdder.ShowToast(message)
			}
		})
	}
	status, err := aistack.FetchNodeStatus(ctx)
	if err != nil {
		log.Printf("views: fetch node status failed: %v", err)
		finish("", "Couldn't reach Agent Mode. Try again.", true)
		return
	}
	candidate, err := aistack.ResolveCandidate(ctx, fam, status.Memory, aistack.DefaultFetch)
	if err != nil {
		log.Printf("views: resolve candidate failed: %v", err)
		finish("", fmt.Sprintf("Couldn't find a %s model that fits this computer.", fam.DisplayName()), true)
		return
	}
	modelRef := candidate.ModelRef()
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would configure alias %s to %s and pull", aistack.ActiveModelAlias, modelRef)
		finish("", fmt.Sprintf("[DRY-RUN] Would switch to %s", modelRef), false)
		return
	}
	if err := aistack.PullModel(ctx, modelRef); err != nil {
		log.Printf("views: pull model failed: %v", err)
		finish("", "Couldn't download the model. Check your internet connection.", true)
		return
	}
	if err := aistack.ConfigureActiveModel(ctx, modelRef); err != nil {
		log.Printf("views: configure alias failed: %v", err)
		finish("", "Couldn't switch to the new model. Try again.", true)
		return
	}
	finish(modelRef, fmt.Sprintf("Selected %s.", fam.DisplayName()), false)
}

func (uh *UserHome) onAgentModeToggled(enabled bool, toggle *guardedSwitch) {
	if !uh.agentModeGate.TryStart() {
		toggle.set(uh.agentModeState.On())
		return
	}
	previous := uh.agentModeState
	uh.agentRefresh.Begin()
	toggle.widget.SetSensitive(false)
	uh.agentPresetRow.SetSensitive(false)
	uh.agentModelRow.SetSensitive(false)
	uh.agentModeRow.SetSubtitle(pageview.AgentModeWorkingSubtitle(enabled))
	uh.agentModelRow.SetSubtitle("Waiting for Agent Mode…")
	uh.agentPresetRow.SetSubtitle("Waiting for Agent Mode…")
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
