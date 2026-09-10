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
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// buildAIStackGroup builds the local-AI switch. Unlike the other groups on
// this page it is not Bluefin-specific: quadlets are a Podman feature, so it
// is offered wherever Podman exists and stays visible-but-inert where it
// does not, rather than vanishing without explanation.
func (uh *UserHome) buildAIStackGroup(page *adw.PreferencesPage) {
	// Site overrides (a mirrored image, a larger model) come from the group's
	// own config entry. A rejected override is surfaced rather than ignored:
	// a site that mirrored its images needs to know the mirror is not in use.
	if group := uh.config.GetGroupConfig("features_page", "ai_group"); group != nil {
		if err := aistack.ApplyOverrides(group.AIImages, group.AIModel); err != nil {
			log.Printf("views: ai stack override rejected: %v", err)
			uh.toastAdder.ShowErrorToast(fmt.Sprintf("Local AI configuration: %v", err))
		}
	}

	stack := aistack.Detect()
	available := aistack.IsAvailable()

	log.Printf("views: ai stack group built vendor=%s accelerator=%s image=%s podman=%v",
		stack.Vendor, stack.Accelerator, stack.Image, available)

	group := adw.NewPreferencesGroup()
	group.SetTitle("Local AI")
	group.SetDescription("Runs a language model in a rootless container on your own hardware")

	presentation := pageview.AIStackRow(stack.Accelerator, stack.Accelerated())
	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	toggle := gtk.NewSwitch()
	toggle.SetValign(gtk.AlignCenterValue)

	if available {
		toggle.SetActive(aistack.IsEnabled())
		sw := toggle
		aiRow := row
		stateSetCb := func(_ gtk.Switch, state bool) bool {
			uh.onAIStackToggled(state, sw, aiRow)
			return true
		}
		toggle.ConnectStateSet(&stateSetCb)
	} else {
		toggle.SetSensitive(false)
		row.SetSubtitle(pageview.AIStackUnavailableSubtitle())
	}

	row.AddSuffix(&toggle.Widget)
	row.SetActivatableWidget(&toggle.Widget)
	group.Add(&row.Widget)

	page.Add(group)
	uh.aiStackGroup = group
	uh.aiStackRow = row
	uh.aiStackSwitch = toggle
}

// onAIStackToggled installs or removes the quadlet. Enabling only writes the
// unit and starts the service: the multi-gigabyte image pull happens inside
// the container runtime afterwards, so the switch must not wait on it.
func (uh *UserHome) onAIStackToggled(enabled bool, toggle *gtk.Switch, row *adw.ActionRow) {
	if !uh.aiStackGate.TryStart() {
		return
	}
	toggle.SetSensitive(false)
	row.SetSubtitle("Working...")

	stack := aistack.Detect()

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
			toggle.SetSensitive(true)

			if err != nil {
				toggle.SetActive(!enabled)
				row.SetSubtitle(pageview.AIStackRow(stack.Accelerator, stack.Accelerated()).Subtitle)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Local AI failed: %v", err))
				return
			}

			decision := actionmsg.AIStack(dryrun.Enabled(), enabled, stack.Accelerator)
			toggle.SetActive(decision.Confirm == enabled)
			if decision.Confirm {
				row.SetSubtitle(pageview.AIStackResultSubtitle(enabled, aistack.Port))
			} else {
				row.SetSubtitle(pageview.AIStackRow(stack.Accelerator, stack.Accelerated()).Subtitle)
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
