package views

import (
	"context"
	"log"
	"runtime"
	"time"

	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
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

// showAgentModeState records the last known state and renders it. Main
// thread only.
func (uh *UserHome) showAgentModeState(state aistack.State) {
	uh.agentModeState = state
	if uh.agentModeRow != nil {
		uh.agentModeRow.SetSubtitle(pageview.AgentModeSubtitle(state))
	}
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
