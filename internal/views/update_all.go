package views

import (
	"context"
	"fmt"
	"log"

	"time"

	"github.com/projectbluefin/chairlift/internal/autoupdate"
	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/notify"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/updateall"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// autoUpdateProbeTimeout bounds the two unprivileged systemctl queries that
// decide whether the automatic-updates switch is shown. It runs during page
// construction, so it must not be able to stall the window.
const autoUpdateProbeTimeout = 5 * time.Second

// Update All is ChairLift's port of bluefinctl's `bctl update` and
// finupdate's hero update button: one action that brings the OS image,
// Flatpak applications, and Homebrew packages up to date, followed by a
// single restart prompt when — and only when — an OS image was actually
// staged.
//
// The sequencing, failure handling, and restart decision live in the pure
// internal/updateall package. This file is widget wiring only: it supplies
// the provider seams, marshals events back to the GTK main thread, and
// renders the result.

// buildUpdateAllGroup builds the Update All group at the top of the Updates
// page. The group is omitted entirely when no provider on this host can be
// updated, rather than offering a button that would do nothing.
// The group's config guard is applied by its caller in updates_page.go,
// because internal/config and internal/navigation both scan that one file for
// each page's IsGroupEnabled call sites.
func (uh *UserHome) buildUpdateAllGroup(page *adw.PreferencesPage) {
	plan := updateall.Plan(hostAvailability())
	if len(plan) == 0 {
		return
	}

	group := adw.NewPreferencesGroup()
	group.SetTitle("Update All")

	row := adw.NewActionRow()
	presentation := pageview.UpdateAllRow(len(plan))
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	button := gtk.NewButtonWithLabel("Update All")
	button.SetValign(gtk.AlignCenterValue)
	button.AddCssClass("suggested-action")
	clickedCb := func(gtk.Button) { uh.onUpdateAllClicked(plan) }
	button.ConnectClicked(&clickedCb)
	row.AddSuffix(&button.Widget)
	group.Add(&row.Widget)

	// One row per phase, so a run's progress is legible without opening
	// anything. They start in the "Waiting" state.
	uh.updateAllPhases = make(map[string]*adw.ActionRow, len(plan))
	for _, phase := range plan {
		phaseRow := adw.NewActionRow()
		phaseRow.SetTitle(phase.Title)
		phaseRow.SetSubtitle(pageview.UpdateAllPhaseSubtitle(false, ""))
		group.Add(&phaseRow.Widget)
		uh.updateAllPhases[string(phase.ID)] = phaseRow
	}

	// The restart prompt is hidden until a run actually stages an image.
	restart := adw.NewActionRow()
	restartPresentation := pageview.RestartRow("")
	restart.SetTitle(restartPresentation.Title)
	restart.SetSubtitle(restartPresentation.Subtitle)
	restartBtn := gtk.NewButtonWithLabel("Restart")
	restartBtn.SetValign(gtk.AlignCenterValue)
	restartBtn.AddCssClass("destructive-action")
	restartClickedCb := func(gtk.Button) { uh.onRestartClicked(restartBtn) }
	restartBtn.ConnectClicked(&restartClickedCb)
	restart.AddSuffix(&restartBtn.Widget)
	restart.SetVisible(false)
	group.Add(&restart.Widget)

	// Automatic updates sit with Update All rather than in a group of their
	// own: they answer the same question — how does this system get updated
	// — and separating them would imply they are unrelated settings. They
	// share update_all_group's config key for the same reason; see
	// config.yml.
	uh.buildAutomaticUpdatesRow(group)

	page.Add(group)
	uh.updateAllGroup = group
	uh.updateAllRow = row
	uh.updateAllBtn = button
	uh.updateAllRestart = restart

	log.Printf("views: update all group built phases=%d", len(plan))
}

// buildAutomaticUpdatesRow adds the automatic-background-updates switch. The
// row is omitted entirely when the unattended-update timer is not installed,
// so a host that cannot update itself never shows a switch that would do
// nothing.
func (uh *UserHome) buildAutomaticUpdatesRow(group *adw.PreferencesGroup) {
	ctx, cancel := context.WithTimeout(context.Background(), autoUpdateProbeTimeout)
	defer cancel()

	state := autoupdate.Detect(ctx)
	if !state.Available() {
		log.Printf("views: automatic updates unavailable (%s not installed)", autoupdate.TimerUnit)
		return
	}

	enabled := state.Enabled()
	row := adw.NewActionRow()
	presentation := pageview.AutomaticUpdatesRow(enabled)
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	toggle := gtk.NewSwitch()
	toggle.SetActive(enabled)
	toggle.SetValign(gtk.AlignCenterValue)

	sw := toggle
	autoRow := row
	stateSetCb := func(_ gtk.Switch, wanted bool) bool {
		uh.onAutomaticUpdatesToggled(wanted, sw, autoRow)
		return true // block the visual change until the switch is confirmed
	}
	toggle.ConnectStateSet(&stateSetCb)

	row.AddSuffix(&toggle.Widget)
	row.SetActivatableWidget(&toggle.Widget)
	group.Add(&row.Widget)

	uh.autoUpdatesRow = row
	uh.autoUpdatesSwitch = toggle
	log.Printf("views: automatic updates row built state=%s", state)
}

// onAutomaticUpdatesToggled turns unattended updates on or off.
func (uh *UserHome) onAutomaticUpdatesToggled(enabled bool, toggle *gtk.Switch, row *adw.ActionRow) {
	toggle.SetSensitive(false)

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.SetAutomaticUpdates(ctx, enabled)

		sgtk.RunOnMainThread(func() {
			toggle.SetSensitive(true)

			if err != nil {
				toggle.SetActive(!enabled)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Automatic updates: %v", err))
				return
			}

			decision := actionmsg.AutomaticUpdates(dryrun.Enabled(), enabled)
			toggle.SetActive(decision.Confirm == enabled)
			if decision.Confirm {
				row.SetSubtitle(pageview.AutomaticUpdatesResultSubtitle(enabled))
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

// hostAvailability reports which providers this host can update. Each check
// is the same cached probe the corresponding per-provider group already uses,
// so Update All can never offer a phase whose own group is hidden.
func hostAvailability() updateall.Availability {
	return updateall.Availability{
		OS:      bootc.IsBootcBootedCached() && bootc.StageScriptAvailable(),
		Flatpak: flatpak.IsInstalledCached(),
		Brew:    homebrew.IsInstalledCached(),
	}
}

// hostRunner wires the pure sequencer to the real providers.
func hostRunner() updateall.Runner {
	return updateall.Runner{
		StageOS: func(ctx context.Context, emitLine func(string)) error {
			progressCh := make(chan bootc.ProgressEvent, 64)
			done := make(chan error, 1)
			go func() { done <- bootc.StageUpdate(ctx, progressCh) }()
			for event := range progressCh {
				if event.Type == bootc.EventMessage {
					emitLine(event.Message)
				}
			}
			return <-done
		},
		StagedAfter: func(ctx context.Context) (bool, string) {
			status, err := bootc.GetStatus(ctx)
			if err != nil || status.Status.Staged == nil {
				return false, ""
			}
			return true, status.Status.Staged.Version()
		},
		UpdateFlatpak: func(context.Context) error {
			// The empty application ID updates every installed application;
			// the user scope is the one ChairLift installs into.
			return flatpak.Update("", true)
		},
		UpdateBrew: func(context.Context) error {
			return homebrew.Update()
		},
	}
}

// onUpdateAllClicked runs every planned phase. The gate makes repeated
// clicks a no-op rather than starting a second concurrent run.
func (uh *UserHome) onUpdateAllClicked(plan []updateall.Phase) {
	if !uh.updateAllGate.TryStart() {
		return
	}

	uh.updateAllBtn.SetSensitive(false)
	uh.updateAllBtn.SetLabel("Updating…")
	uh.updateAllRestart.SetVisible(false)
	for _, phase := range plan {
		if row := uh.updateAllPhases[string(phase.ID)]; row != nil {
			row.SetSubtitle(pageview.UpdateAllPhaseSubtitle(false, ""))
		}
	}

	go func() {
		ctx, cancel := bootc.DefaultContext()
		defer cancel()

		events := make(chan updateall.Event, 64)
		done := make(chan []updateall.Result, 1)
		go func() { done <- hostRunner().Run(ctx, plan, events) }()

		for event := range events {
			uh.applyUpdateAllEvent(event)
		}
		uh.finishUpdateAll(<-done)
	}()
}

// applyUpdateAllEvent renders one progress event. Streamed output lines are
// logged rather than shown: the per-phase row carries the state a user needs,
// and a scrolling log is the kind of detail the per-provider groups below
// already provide for anyone who wants it.
func (uh *UserHome) applyUpdateAllEvent(event updateall.Event) {
	switch event.Type {
	case updateall.EventMessage:
		log.Printf("update all [%s]: %s", event.Phase.ID, event.Message)
		return
	case updateall.EventPhaseStarted, updateall.EventPhaseFinished:
	default:
		return
	}

	running := event.Type == updateall.EventPhaseStarted
	detail := ""
	if !running {
		detail = event.Result.Detail
	}
	phaseID := string(event.Phase.ID)

	sgtk.RunOnMainThread(func() {
		row, ok := uh.updateAllPhases[phaseID]
		if !ok || row == nil {
			return
		}
		row.SetSubtitle(pageview.UpdateAllPhaseSubtitle(running, detail))
	})
}

// finishUpdateAll renders the aggregate outcome and reveals the restart
// prompt when an image was actually staged.
func (uh *UserHome) finishUpdateAll(results []updateall.Result) {
	summary := updateall.Summarize(results)

	log.Printf("views: update all finished succeeded=%d failed=%d skipped=%d restart_required=%v",
		summary.Succeeded, summary.Failed, summary.Skipped, summary.RestartRequired)

	sgtk.RunOnMainThread(func() {
		uh.updateAllGate.Complete()

		if uh.updateAllBtn != nil {
			uh.updateAllBtn.SetSensitive(true)
			uh.updateAllBtn.SetLabel("Update All")
		}
		if uh.updateAllRow != nil {
			uh.updateAllRow.SetSubtitle(summary.Headline)
		}
		if uh.updateAllRestart != nil && summary.RestartRequired {
			presentation := pageview.RestartRow(summary.StagedVersion)
			uh.updateAllRestart.SetSubtitle(presentation.Subtitle)
			uh.updateAllRestart.SetVisible(true)
		}

		notification := notify.UpdateAllComplete(summary.Succeeded, summary.Failed, summary.Skipped, summary.RestartRequired)
		uh.toastAdder.NotifyBackground(notification.Title, notification.Body, notification.Urgency == notify.UrgencyHigh)

		if summary.Failed > 0 {
			uh.toastAdder.ShowErrorToast(summary.Headline)
			return
		}
		uh.toastAdder.ShowToast(summary.Headline)
	})
}

// onRestartClicked restarts the machine. The button is disabled immediately:
// on a live run this process is about to go away, and a second press would
// only queue a redundant PolicyKit prompt.
func (uh *UserHome) onRestartClicked(button *gtk.Button) {
	button.SetSensitive(false)

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.Restart(ctx)

		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Restart failed: %v", err))
				return
			}
			if dryrun.Enabled() {
				uh.toastAdder.ShowToast("[DRY-RUN] Preview: the system would restart now — no changes made")
			}
		})
	}()
}
