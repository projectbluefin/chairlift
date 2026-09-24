package views

import (
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
)

// buildAgentsPage builds the Agents page: one switch that runs an AI model on
// this computer. It was a group on the Features page, where it sat between
// developer mode and gaming mode and read as one more toggle; it is a
// destination of its own because the thing it turns on is a service a person
// then points other applications at, not a system preference.
//
// Nothing here is privileged. The model runs rootless in the invoking
// account, so there is no pkexec route on this page and there must not become
// one — see internal/aistack's package comment.
func (uh *UserHome) buildAgentsPage() {
	page := uh.agentsPrefsPage
	if page == nil {
		return
	}

	if uh.groupEnabled("agents_page", "agents_group") {
		uh.buildAIStackGroup(page)
	}
}

// buildAIStackGroup builds the local-AI switch and its Details expander.
// Every signal is connected here, once, for the page's lifetime: puregotk's
// callback table is fixed and never releases a slot, so a handler created in
// a path that reruns is a slow walk to a panic.
func (uh *UserHome) buildAIStackGroup(page *adw.PreferencesPage) {
	// Site overrides (a mirrored image, a larger model) come from the group's
	// own config entry. A rejected override is surfaced rather than ignored:
	// a site that mirrored its images needs to know the mirror is not in use.
	if group := uh.config.GetGroupConfig("agents_page", "agents_group"); group != nil {
		if err := aistack.ApplyOverrides(group.AIImages, group.AIModel); err != nil {
			log.Printf("views: ai stack override rejected: %v", err)
			uh.toastAdder.ShowErrorToast(fmt.Sprintf("Local AI configuration: %v", err))
		}
	}

	// Podman is agents_group's capability floor (internal/capability), so a
	// host without it never reaches this builder.
	stack := aistack.Detect()

	log.Printf("views: agents page built vendor=%s accelerator=%s image=%s",
		stack.Vendor, stack.Accelerator, stack.Image)

	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.AIStackGroupTitle())
	group.SetDescription(pageview.AIStackGroupDescription())

	presentation := pageview.AIStackRow(stack.Vendor.DisplayName(), stack.Accelerated())
	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	running := aistack.IsEnabled()
	if running {
		row.SetSubtitle(pageview.AIStackResultSubtitle(true))
	}

	// guardedSwitch, not a bare gtk.Switch: GtkSwitch emits ::state-set from
	// gtk_switch_set_active, so showing the machine's real state here and
	// reverting after a failure are both indistinguishable from a click
	// unless they are marked. Unmarked, a failed stop would revert the switch
	// to on and immediately start the model again for real.
	var toggle *guardedSwitch
	toggle = newGuardedSwitch(running, func(state bool) {
		uh.onAIStackToggled(state, toggle, row)
	})

	row.AddSuffix(&toggle.widget.Widget)
	row.SetActivatableWidget(&toggle.widget.Widget)
	group.Add(&row.Widget)

	// The technical identity lives behind Details rather than in the switch
	// row's subtitle: a person deciding whether to turn this on does not need
	// the model reference, and a person wiring an editor up to it needs
	// nothing else.
	details := adw.NewExpanderRow()
	details.SetTitle(pageview.AIStackDetailsTitle())
	for _, detail := range pageview.AIStackDetails(pageview.AIStackFacts{
		Model:       aistack.Model(),
		Hardware:    stack.Vendor.DisplayName(),
		Accelerator: stack.Accelerator,
		Accelerated: stack.Accelerated(),
		Port:        aistack.Port,
	}) {
		detailRow := adw.NewActionRow()
		detailRow.SetTitle(detail.Title)
		detailRow.SetSubtitle(detail.Subtitle)
		details.AddRow(&detailRow.Widget)
	}
	group.Add(&details.Widget)

	page.Add(group)
	uh.aiStackGroup = group
	uh.aiStackRow = row
	uh.aiStackSwitch = toggle.widget
}

// onAIStackToggled starts or stops the model service. Turning it on only
// writes the unit and starts the service: the multi-gigabyte image pull
// happens inside the container runtime afterwards, so the switch must not
// wait on it.
//
// Turning it off can fail in a way that leaves the model running — the
// service refused to stop and a follow-up check could not prove otherwise —
// and internal/aistack deliberately preserves the unit in that case. The
// failure path here matches it: the switch goes back on and the text says the
// model is still running, because it is.
func (uh *UserHome) onAIStackToggled(enabled bool, toggle *guardedSwitch, row *adw.ActionRow) {
	if !uh.aiStackGate.TryStart() {
		return
	}
	toggle.widget.SetSensitive(false)
	row.SetSubtitle(pageview.AIStackWorkingSubtitle(enabled))

	stack := aistack.Detect()
	idle := pageview.AIStackRow(stack.Vendor.DisplayName(), stack.Accelerated()).Subtitle

	go func() {
		defer uh.aiStackGate.Reset()

		ctx, cancel := aistack.DefaultContext()
		defer cancel()

		var err error
		if enabled {
			err = aistack.Enable(ctx, stack)
		} else {
			err = aistack.Disable(ctx)
		}

		sgtk.RunOnMainThread(func() {
			toggle.widget.SetSensitive(true)

			if err != nil {
				// The error names the background service by its unit file,
				// which is not something to put in front of a person; it is
				// logged instead, and the toast says what actually happened.
				log.Printf("views: local AI toggle to %v failed: %v", enabled, err)
				toggle.set(!enabled)
				row.SetSubtitle(pageview.AIStackFailureSubtitle(enabled))
				uh.toastAdder.ShowErrorToast(pageview.AIStackFailureToast(enabled))
				return
			}

			decision := actionmsg.AIStack(dryrun.Enabled(), enabled)
			toggle.set(decision.Confirm == enabled)
			if decision.Confirm {
				row.SetSubtitle(pageview.AIStackResultSubtitle(enabled))
			} else {
				row.SetSubtitle(idle)
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
