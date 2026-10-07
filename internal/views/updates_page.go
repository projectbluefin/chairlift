package views

import (
	"fmt"
	"log"
	"sync"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/stageexec"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
	"github.com/projectbluefin/chairlift/internal/views/progresslog"
	"github.com/projectbluefin/chairlift/internal/views/rowset"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// buildUpdatesPage builds the Updates page content. The page owns the whole
// update story for this machine: what it is running now, what is waiting,
// the applications and tools that update separately, and — at the bottom,
// where a person looks only when they mean to — the two choices that
// replace the operating system itself.
func (uh *UserHome) buildUpdatesPage() {
	page := uh.updatesPrefsPage
	if page == nil {
		return
	}
	// Whether this machine updates itself on a schedule. The act of
	// updating now belongs to the status-first shell above this page
	// (internal/updateflow); this is the preference about the future.
	if uh.groupEnabled("updates_page", "automatic_updates_group") {
		uh.buildAutomaticUpdatesGroup(page)
	}

	// What this machine is running, in words. Built hidden and revealed
	// asynchronously, because reading it requires an exec.
	if uh.groupEnabled("updates_page", "bootc_status_group") {
		uh.buildSystemVersionGroup(page)
	}

	// bootc system updates group - built hidden, shown asynchronously on
	// bootc hosts that ship the update-stage script.
	if uh.groupEnabled("updates_page", "bootc_updates_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("System update details")
		group.SetDescription("Download a system update on its own, or see what it changes.")
		group.SetVisible(false)

		uh.bootcStageExpander = adw.NewExpanderRow()
		// Subtitles include the wrapped bootc error from a failed stage
		// (statusErr at onBootcStageClicked) and the version reported by
		// BootcUpdateSubtitle. AdwExpanderRow parses its subtitle as
		// Pango markup by default, so a '<' or '&' would garble the row
		// with a GTK warning (issue #437).
		uh.bootcStageExpander.SetUseMarkup(false)
		uh.bootcStageExpander.SetTitle("Download system update")
		uh.bootcStageExpander.SetSubtitle("Checking…")

		uh.bootcStageBtn = gtk.NewButtonWithLabel("Check for updates")
		uh.bootcStageBtn.SetValign(gtk.AlignCenterValue)
		stageClickedCb := func(btn gtk.Button) {
			uh.onBootcStageClicked()
		}
		uh.bootcStageBtn.ConnectClicked(&stageClickedCb)
		uh.bootcStageExpander.AddSuffix(&uh.bootcStageBtn.Widget)

		uh.buildChangelogRow(group)

		group.Add(&uh.bootcStageExpander.Widget)

		page.Add(group)

		go uh.loadBootcUpdateStatus(group)
	}

	// Sources whose updates Homebrew has paused - hidden unless there is
	// something a person can act on (Homebrew 6 tap trust).
	if uh.groupEnabled("updates_page", "brew_trust_group") {
		uh.brewTrustGroup = adw.NewPreferencesGroup()
		uh.brewTrustGroup.SetTitle("Unverified sources")
		uh.brewTrustGroup.SetDescription("Updates are paused for software from sources you haven't trusted yet.")
		uh.brewTrustGroup.SetVisible(false)
		page.Add(uh.brewTrustGroup)

		go uh.loadUntrustedTaps()
	}

	// The two choices that replace the operating system sit last: a person
	// reaches them deliberately, never on the way to something else. Hidden
	// entirely on a host with no image descriptor, like every other
	// Bluefin-family group.
	if uh.groupEnabled("updates_page", "channel_group") {
		uh.buildImageIdentityGroup(page)
	}
}

// loadUntrustedTaps populates the unverified-sources group. Runs in a
// goroutine; the group stays hidden when there is nothing actionable.
func (uh *UserHome) loadUntrustedTaps() {
	if uh.brewTrustGroup == nil {
		return
	}
	taps, err := homebrew.ListUntrustedTaps()
	if err != nil {
		log.Printf("untrusted tap check failed: %v", err)

	}
	sgtk.RunOnMainThread(func() {
		for _, row := range uh.brewTrustRows {
			uh.brewTrustGroup.Remove(&row.Widget)
		}
		uh.brewTrustRows = make(map[string]*adw.ActionRow)
		uh.trustButtons.clear()
		if err != nil {
			row := adw.NewActionRow()
			// The raw error is logged above; the row says what to do.
			row.SetUseMarkup(false)
			row.SetTitle("Couldn't check for paused updates")
			row.SetSubtitle("Check your internet connection and try again.")
			button := gtk.NewButtonWithLabel("Retry")
			button.SetValign(gtk.AlignCenterValue)
			uh.trustButtons.connect(button, func(btn gtk.Button) {
				btn.SetSensitive(false)
				go uh.loadUntrustedTaps()
			})
			row.AddSuffix(&button.Widget)
			uh.brewTrustGroup.Add(&row.Widget)
			uh.brewTrustRows[""] = row
		}
		for _, tap := range taps {
			t := tap
			presentation := pageview.UntrustedTap(t.Name, t.Formulae, t.Casks)
			row := adw.NewActionRow()
			row.SetTitle(presentation.Title)
			row.SetSubtitle(presentation.Subtitle)
			trustBtn := gtk.NewButtonWithLabel("Trust…")
			trustBtn.SetValign(gtk.AlignCenterValue)
			uh.trustButtons.connect(trustBtn, func(gtk.Button) {
				uh.confirmTrustTap(t, trustBtn)
			})
			row.AddSuffix(&trustBtn.Widget)
			uh.brewTrustGroup.Add(&row.Widget)
			uh.brewTrustRows[t.Name] = row
		}
		uh.brewTrustGroup.SetVisible(len(uh.brewTrustRows) > 0)
	})
}

// confirmTrustTap shows a confirmation dialog before trusting a source. The
// consequence is third-party code running on this machine, which is exactly
// the kind of thing a person must agree to rather than discover.
func (uh *UserHome) confirmTrustTap(tap homebrew.UntrustedTap, button *gtk.Button) {
	dialog := adw.NewAlertDialog(
		fmt.Sprintf("Trust software from %s?", tap.Name),
		"Software from this source can change anything on this computer. Only trust sources you recognize.",
	)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("trust", "Trust")
	dialog.SetResponseAppearance("trust", adw.ResponseSuggestedValue)

	uh.confirmations.connect(dialog, func(response string) {
		if response != "trust" {
			return
		}
		button.SetSensitive(false)
		button.SetLabel("Trusting…")
		go uh.trustTap(tap, button)
	})
	dialog.Present(&uh.updatesPrefsPage.Widget)
}

// trustTap runs brew trust and updates the UI on completion.
func (uh *UserHome) trustTap(tap homebrew.UntrustedTap, button *gtk.Button) {
	err := homebrew.TrustPackages(tap)
	if err != nil {
		log.Printf("trusting %s failed: %v", tap.Name, err)
	}

	sgtk.RunOnMainThread(func() {
		if err != nil {
			button.SetSensitive(true)
			button.SetLabel("Trust…")
			uh.toastAdder.ShowErrorToast(fmt.Sprintf("Couldn't trust %s. Try again.", tap.Name))
			return
		}

		decision := actionmsg.TapTrust(dryrun.Enabled(), tap.Name)
		if decision.MutateUI {
			if row, ok := uh.brewTrustRows[tap.Name]; ok {
				uh.brewTrustGroup.Remove(&row.Widget)
				delete(uh.brewTrustRows, tap.Name)
				uh.trustButtons.forget(button)
			}
			if len(uh.brewTrustRows) == 0 {
				uh.brewTrustGroup.SetVisible(false)
			}
			uh.toastAdder.ShowToast(decision.Toast)

			// Newly trusted software may now appear in the unified inventory.
			if uh.updateShell != nil {
				uh.updateShell.StartCheck()
			}
		} else {
			// Dry-run: nothing was actually trusted, so the row must not
			// disappear from the unverified-sources list. Reset the button
			// instead of leaving it stuck on "Trusting…".
			button.SetSensitive(true)
			button.SetLabel("Trust…")
			uh.toastAdder.ShowToast(decision.Toast)
		}
	})
}

// loadBootcUpdateStatus gates the bootc updates group and reflects the
// current staged/booted state in the expander subtitle and update badge.
func (uh *UserHome) loadBootcUpdateStatus(group *adw.PreferencesGroup) {
	if !bootc.IsBootcBootedCached() || !bootc.StageScriptAvailable() {
		return // group stays hidden
	}

	ctx, cancel := bootc.DefaultContext()
	defer cancel()

	status, err := bootc.GetStatus(ctx)

	staged := err == nil && status.Status.Staged != nil

	sgtk.RunOnMainThread(func() {
		group.SetVisible(true)
		if err != nil {
			log.Printf("reading system status failed: %v", err)
			uh.bootcStageExpander.SetSubtitle("Couldn't check for system updates.")
			return
		}
		version := ""
		if staged {
			version = status.Status.Staged.Version()
		}
		uh.bootcStageExpander.SetSubtitle(pageview.BootcUpdateSubtitle(staged, version))
		uh.refreshChangelogAvailability(status)
	})
}

// stageProgressSink renders the streamed output of an OS staging run into a
// bounded rolling log.
//
// It exists for the cost of the obvious alternative. Rendering each line as it
// arrives queues one sgtk.RunOnMainThread callback per line and leaves one
// permanent action row behind, so a verbose stage helper accumulates thousands
// of heavyweight widgets and callbacks until the run ends — an unresponsive
// window and unbounded memory. The sink coalesces a burst of lines into a
// single main-thread callback and keeps only the most recent
// progresslog.DefaultLimit rows, evicting older ones from the expander.
//
// The split of duties is the GTK main-thread rule: consume runs on the worker
// goroutine and touches no widget, flush runs on the main thread and owns
// every widget this type names.
type stageProgressSink struct {
	activityRow *adw.ActionRow
	logExpander *adw.ExpanderRow
	lines       *progresslog.Coalescer
	rows        rowset.Tracker[*adw.ActionRow]
}

// newStageProgressSink returns a sink rendering into the run's activity row
// and "Details" expander.
func newStageProgressSink(activityRow *adw.ActionRow, logExpander *adw.ExpanderRow) *stageProgressSink {
	return &stageProgressSink{
		activityRow: activityRow,
		logExpander: logExpander,
		lines:       progresslog.New(progresslog.DefaultLimit),
	}
}

// consume reads progressCh to closure and returns the last message the helper
// printed, which the caller needs for its result subtitle. A line schedules a
// flush only when no flush is already outstanding; the completion event always
// flushes, so the final lines of a run are never left pending.
func (s *stageProgressSink) consume(progressCh <-chan stageexec.ProgressEvent) string {
	var lastMessage string
	for event := range progressCh {
		switch event.Type {
		case stageexec.EventMessage:
			lastMessage = event.Message
			if s.lines.Append(event.Message) {
				sgtk.RunOnMainThread(s.flush)
			}
		case stageexec.EventComplete:
			sgtk.RunOnMainThread(func() {
				s.flush()
				s.activityRow.SetSubtitle("Complete")
			})
		}
	}
	return lastMessage
}

// flush renders one coalesced batch on the GTK main thread, then trims the
// expander back to the retention window so its row count stays bounded.
func (s *stageProgressSink) flush() {
	batch := s.lines.Drain()
	if len(batch.Lines) == 0 {
		return
	}

	for _, line := range batch.Lines {
		msgRow := adw.NewActionRow()
		// line.Text is streamed stage/llmman output, i.e. untrusted command
		// output; AdwActionRow parses its title as Pango markup by default, so
		// a '<' or '&' in that text would garble or drop the row with a
		// GTK warning. SetUseMarkup(false) renders it literally.
		msgRow.SetUseMarkup(false)
		msgRow.SetTitle(line.Text)
		msgRow.SetSubtitle(line.At.Format("15:04:05"))
		s.logExpander.AddRow(&msgRow.Widget)
		s.rows.Add(msgRow)
	}
	s.rows.TrimTo(s.lines.Limit(), func(row *adw.ActionRow) {
		s.logExpander.Remove(&row.Widget)
	})

	s.activityRow.SetSubtitle(batch.Lines[len(batch.Lines)-1].Text)
	s.logExpander.SetSubtitle(pageview.StagingLogSubtitle(s.rows.Len(), batch.Total))
}

// onBootcStageClicked runs the stage script with streamed log output.
// The script checks, downloads, and stages in one idempotent operation.
func (uh *UserHome) onBootcStageClicked() {
	if uh.updateShell == nil || !uh.updateShell.beginMutation() {
		return
	}
	button := uh.bootcStageBtn
	expander := uh.bootcStageExpander

	button.SetSensitive(false)
	button.SetLabel("Working…")
	expander.SetExpanded(true)
	expander.SetSubtitle("Checking for updates…")

	// Remove rows from any previous run before adding new ones, otherwise
	// repeated clicks stack duplicate Progress/Details rows.
	if uh.bootcActivityRow != nil {
		expander.Remove(&uh.bootcActivityRow.Widget)
	}
	if uh.bootcLogExpander != nil {
		expander.Remove(&uh.bootcLogExpander.Widget)
	}

	// Activity row with a spinner (the stage script emits no percentages,
	// so progress is indeterminate).
	activityRow := adw.NewActionRow()
	// flush() sets this row's subtitle from the last streamed stage/llmman
	// line, i.e. untrusted command output; AdwActionRow parses a subtitle as
	// Pango markup unless use-markup is FALSE, so a '<' or '&' would garble
	// the row with a GTK warning (issue #435).
	activityRow.SetUseMarkup(false)
	activityRow.SetTitle("Progress")
	activityRow.SetSubtitle("Working…")
	spinner := gtk.NewSpinner()
	spinner.Start()
	activityRow.AddSuffix(&spinner.Widget)
	expander.AddRow(&activityRow.Widget)
	uh.bootcActivityRow = activityRow

	logExpander := adw.NewExpanderRow()
	logExpander.SetTitle("Details")
	logExpander.SetSubtitle(pageview.StagingLogSubtitle(0, 0))
	expander.AddRow(&logExpander.Widget)
	uh.bootcLogExpander = logExpander

	go func() {
		ctx, cancel := bootc.DefaultContext()
		defer cancel()

		progressCh := make(chan bootc.ProgressEvent)

		var stageErr error
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			stageErr = bootc.StageUpdate(ctx, progressCh)
		}()

		// The sink's return value is the stage helper's own last line.
		// It is deliberately discarded: it is terminal output that can
		// name paths a person has no use for, which is why
		// pageview.BootcStageResultSubtitle takes no such argument.
		newStageProgressSink(activityRow, logExpander).consume(progressCh)

		wg.Wait()

		// Re-read status so the subtitle and badge reflect reality
		// (staged vs already-current) rather than guessing from output.
		statusCtx, statusCancel := bootc.DefaultContext()
		status, statusErr := bootc.GetStatus(statusCtx)
		statusCancel()

		staged := statusErr == nil && status.Status.Staged != nil

		sgtk.RunOnMainThread(func() {
			uh.updateShell.finishMutation()
			spinner.Stop()
			button.SetSensitive(true)
			button.SetLabel("Check for updates")

			if stageErr != nil {
				log.Printf("staging the system update failed: %v", stageErr)
				expander.SetSubtitle("Couldn't download the update. Open Details to see why.")
				uh.toastAdder.ShowErrorToast("Couldn't download the system update. Try again later.")
				return
			}

			if statusErr == nil {
				uh.refreshChangelogAvailability(status)
			}
			if statusErr != nil {
				log.Printf("reading status after staging failed: %v", statusErr)
				message := "Couldn't confirm the update is ready. Try again."
				expander.SetSubtitle(message)
				if dryrun.Enabled() {
					uh.toastAdder.ShowToast(actionmsg.SystemStage(true, false))
				} else {
					uh.toastAdder.ShowErrorToast(message)
				}
				return
			}

			version := ""
			if staged {
				version = status.Status.Staged.Version()
			}
			expander.SetSubtitle(pageview.BootcStageResultSubtitle(staged, version))
			uh.toastAdder.ShowToast(actionmsg.SystemStage(dryrun.Enabled(), staged))
			if !dryrun.Enabled() {
				uh.updateShell.StartCheck()
			}
		})
	}()
}

// buildSystemVersionGroup builds the read-only "System version" group: one
// plain-language row saying what this machine runs, and a Details row
// holding the identifiers a support request asks for. It is built hidden
// and revealed asynchronously, because reading the version requires an exec
// and this host may not be image-based at all.
func (uh *UserHome) buildSystemVersionGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Your system")
	group.SetVisible(false)

	versionRow := adw.NewActionRow()
	versionRow.SetTitle("System version")
	versionRow.SetSubtitle("Checking…")
	group.Add(&versionRow.Widget)

	details := adw.NewExpanderRow()
	details.SetTitle("Details")
	details.SetSubtitle("Share these when asking for help.")
	group.Add(&details.Widget)

	page.Add(group)

	go uh.loadSystemVersion(group, versionRow, details)
}

// loadSystemVersion fills the System version group. Runs in a goroutine and
// shows the group only on a host whose version can actually be read; the
// widgets are parameters rather than fields because nothing refreshes them
// after this single pass.
func (uh *UserHome) loadSystemVersion(group *adw.PreferencesGroup, versionRow *adw.ActionRow, details *adw.ExpanderRow) {
	if !bootc.IsBootcBootedCached() {
		return // group stays hidden on hosts with no system image
	}

	ctx, cancel := bootc.DefaultContext()
	defer cancel()

	status, err := bootc.GetStatus(ctx)
	if err != nil {
		log.Printf("views: reading the system version failed: %v", err)
		return // an unreadable version is not worth a row that says so
	}

	booted := status.Status.Booted
	staged := status.Status.Staged
	presentation := pageview.SystemVersionRow(booted.Version(), booted.Timestamp(), staged != nil, staged.Version())
	detailRows := pageview.SystemVersionDetails(
		booted.Version(),
		booted.Timestamp(),
		booted.ImageRef(),
		booted.Digest(),
	)

	sgtk.RunOnMainThread(func() {
		versionRow.SetTitle(presentation.Title)
		versionRow.SetSubtitle(presentation.Subtitle)

		for _, detail := range detailRows {
			row := adw.NewActionRow()
			row.SetTitle(detail.Title)
			row.SetSubtitle(detail.Subtitle)
			details.AddRow(&row.Widget)
		}
		details.SetVisible(len(detailRows) > 0)

		group.SetVisible(true)
	})
}

// buildImageIdentityGroup builds the two controls that decide which
// operating system boots: early updates and the graphics driver. Both
// replace the running system and take effect at the next restart, so they
// share one group at the foot of the Updates page rather than sitting among
// the things a person changes casually.
func (uh *UserHome) buildImageIdentityGroup(page *adw.PreferencesPage) {
	status := ublue.StatusCached()
	if !status.Available {
		return
	}

	// Fail closed on a broken authoritative channel table: render a
	// diagnostic instead of the two controls, so no replacement can be
	// requested against a table the privileged helper rejects. See
	// imageinfo.SystemTableError.
	if status.ChannelTableError != "" {
		uh.buildChannelErrorGroup(page)
		log.Printf("views: image identity group failed closed channel_table_error=%q", status.ChannelTableError)
		return
	}

	uh.buildChannelGroup(page, status)
	uh.buildDriverRow(status)

	log.Printf("views: image identity group built variant=%s tag=%s channel=%s switchable=%v driver=%s",
		status.Variant, status.Tag, status.Channel,
		status.CanSwitchTo != imageinfo.ChannelUnknown, status.Driver)
}

// buildChannelErrorGroup replaces the early-updates switch and the graphics
// driver row with a single read-only row when the authoritative channel
// table could not be applied. Both actions resolve their target through that
// table, and the privileged helper re-reads it and refuses after
// authentication, so keeping the controls enabled would only ask a person to
// authenticate for an error. The underlying fault names a file, so it goes
// to the log rather than to the row.
func (uh *UserHome) buildChannelErrorGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Advanced")

	row := adw.NewActionRow()
	row.SetTitle("Unavailable right now")
	row.SetSubtitle("These options are off because of a problem with this computer's update settings.")
	group.Add(&row.Widget)

	page.Add(group)
	uh.channelGroup = group
	uh.channelRow = row
}

// buildChannelGroup builds the early-updates switch. The switch is
// insensitive when the running system publishes no counterpart to switch to
// — every Bluefin Stable host, for one — because there is nothing to switch
// to. See internal/imageinfo's channel table.
func (uh *UserHome) buildChannelGroup(page *adw.PreferencesPage, status ublue.Status) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Advanced")
	group.SetDescription("These change the whole operating system. Each needs your administrator password, a large download, and a restart.")

	onTesting := status.Channel == imageinfo.ChannelTesting
	// Nothing to switch to, or an image that does not provide the helper
	// command, both leave the switch inert.
	switchable := status.CanSwitchTo != imageinfo.ChannelUnknown && status.Supports(ubluehelper.CommandChannelSwitch)
	presentation := pageview.ChannelRow(onTesting, switchable)

	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	// guardedSwitch, because gtk_switch_set_active emits ::state-set by the
	// same path a person's click does: an unmarked revert after a failure or
	// a dry run would ask the helper to switch back, for real.
	var toggle *guardedSwitch
	channelRow := row
	toggle = newGuardedSwitch(onTesting, func(state bool) {
		uh.onChannelToggled(state, toggle, channelRow)
	})
	toggle.widget.SetSensitive(switchable)

	row.AddSuffix(&toggle.widget.Widget)
	if switchable {
		row.SetActivatableWidget(&toggle.widget.Widget)
	}
	group.Add(&row.Widget)

	page.Add(group)
	uh.channelGroup = group
	uh.channelRow = row
	uh.channelSwitch = toggle.widget
}

// buildDriverRow adds the graphics-driver row to the Advanced group. The row
// is always informational and only offers an action when the hardware wants
// a different driver than the one running and that driver is actually
// published for the current stream.
func (uh *UserHome) buildDriverRow(status ublue.Status) {
	if uh.channelGroup == nil {
		return
	}

	// Recommend a switch only when the image provides the command that
	// performs it; otherwise the row describes the current driver.
	recommended := ""
	if status.RecommendedDriver != "" && status.Supports(ubluehelper.CommandDriverSwitch) {
		recommended = status.RecommendedDriver.DisplayName()
	}
	presentation := pageview.GraphicsDriverRow(status.Driver.DisplayName(), status.GPU, recommended)

	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	if status.RecommendedDriver != "" && status.Supports(ubluehelper.CommandDriverSwitch) {
		driver := status.RecommendedDriver
		driverRow := row
		button := gtk.NewButtonWithLabel("Switch")
		button.SetValign(gtk.AlignCenterValue)
		button.AddCssClass("suggested-action")
		clickedCb := func(gtk.Button) {
			uh.onDriverSwitchClicked(driver, button, driverRow)
		}
		button.ConnectClicked(&clickedCb)
		row.AddSuffix(&button.Widget)
		uh.driverButton = button
	}

	uh.channelGroup.Add(&row.Widget)
	uh.driverRow = row
	log.Printf("views: graphics driver row built current=%s recommended=%q gpu=%q",
		status.Driver, status.RecommendedDriver, status.GPU)
}

// onDriverSwitchClicked asks for the recommended graphics driver. Only the
// driver word crosses the privileged boundary; the helper derives the
// operating system to install from its own read-only table.
func (uh *UserHome) onDriverSwitchClicked(driver imageinfo.Driver, button *gtk.Button, row *adw.ActionRow) {
	if !uh.driverGate.TryStart() {
		return
	}

	button.SetSensitive(false)
	button.SetLabel("Switching…")

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.SwitchDriver(ctx, driver)

		sgtk.RunOnMainThread(func() {
			uh.driverGate.Reset()
			button.SetSensitive(true)
			button.SetLabel("Switch")

			if err != nil {
				log.Printf("switching the graphics driver failed: %v", err)
				uh.toastAdder.ShowErrorToast(pkexec.UserMessage(err, "Couldn't switch the graphics driver. Try again."))
				return
			}

			decision := actionmsg.DriverSwitch(dryrun.Enabled(), driver.DisplayName())
			if decision.Confirm {
				row.SetSubtitle(pageview.GraphicsDriverResultSubtitle(driver.DisplayName()))
				button.SetVisible(false)
				uh.refreshAfterOSSwitch()
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

// onChannelToggled asks for the other release channel. Only the channel word
// crosses the privileged boundary.
func (uh *UserHome) onChannelToggled(toTesting bool, toggle *guardedSwitch, row *adw.ActionRow) {
	channel := imageinfo.ChannelStable
	if toTesting {
		channel = imageinfo.ChannelTesting
	}

	toggle.widget.SetSensitive(false)

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.SwitchChannel(ctx, channel)

		sgtk.RunOnMainThread(func() {
			toggle.widget.SetSensitive(true)

			if err != nil {
				toggle.set(!toTesting)
				log.Printf("switching the release channel failed: %v", err)
				uh.toastAdder.ShowErrorToast(pkexec.UserMessage(err, "Couldn't change early updates. Try again."))
				return
			}

			decision := actionmsg.ChannelSwitch(dryrun.Enabled(), toTesting)
			toggle.set(decision.Confirm == toTesting)
			if decision.Confirm {
				row.SetSubtitle(pageview.ChannelSwitchResultSubtitle(toTesting))
				uh.refreshAfterOSSwitch()
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

// refreshAfterOSSwitch re-reads what a live channel or driver switch staged,
// the way a dedicated stage does: the Operating system row learns a restart
// is due and offers Restart now, and Compare learns the new pending image.
// Without it both kept describing the system before the switch until the
// next manual check.
func (uh *UserHome) refreshAfterOSSwitch() {
	if dryrun.Enabled() {
		return
	}
	uh.updateShell.StartCheck()
	uh.OnUpdateFinished(updateflow.Snapshot{CompletedSources: []updateflow.SourceID{updateflow.OperatingSystem}})
}
