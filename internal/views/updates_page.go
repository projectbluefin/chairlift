package views

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/stageexec"
	"github.com/projectbluefin/chairlift/internal/sysupdate"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/badgestate"
	"github.com/projectbluefin/chairlift/internal/views/flatpakstatus"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
	"github.com/projectbluefin/chairlift/internal/views/progresslog"
	"github.com/projectbluefin/chairlift/internal/views/rowset"
	"github.com/projectbluefin/chairlift/internal/views/trustmsg"

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
	if uh.config.IsGroupEnabled("updates_page", "automatic_updates_group") {
		uh.buildAutomaticUpdatesGroup(page)
	}

	// What this machine is running, in words. Built hidden and revealed
	// asynchronously, because reading it requires an exec.
	if uh.config.IsGroupEnabled("updates_page", "bootc_status_group") {
		uh.buildSystemVersionGroup(page)
	}

	// bootc system updates group - built hidden, shown asynchronously on
	// bootc hosts that ship the update-stage script.
	if uh.config.IsGroupEnabled("updates_page", "bootc_updates_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("Operating system")
		group.SetDescription("New versions download in the background and install when you restart.")
		group.SetVisible(false)

		uh.bootcStageExpander = adw.NewExpanderRow()
		uh.bootcStageExpander.SetTitle("System updates")
		uh.bootcStageExpander.SetSubtitle("Checking…")

		uh.bootcStageBtn = gtk.NewButtonWithLabel("Check for updates")
		uh.bootcStageBtn.SetValign(gtk.AlignCenterValue)
		uh.bootcStageBtn.AddCssClass("suggested-action")
		stageClickedCb := func(btn gtk.Button) {
			uh.onBootcStageClicked()
		}
		uh.bootcStageBtn.ConnectClicked(&stageClickedCb)
		uh.bootcStageExpander.AddSuffix(&uh.bootcStageBtn.Widget)

		uh.buildChangelogRow(uh.bootcStageExpander)

		group.Add(&uh.bootcStageExpander.Widget)

		page.Add(group)

		go uh.loadBootcUpdateStatus(group)
	}

	// Native A/B (systemd-sysupdate) system updates group - built hidden,
	// shown asynchronously on native A/B hosts that ship the snosi stager.
	// Mutually exclusive with the bootc group at runtime: the bootc gate
	// requires the bootc binary (absent on native A/B images) and this gate
	// requires the native-ab marker (absent on bootc images), so at most one
	// operating-system group ever becomes visible.
	if uh.config.IsGroupEnabled("updates_page", "sysupdate_updates_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("Operating system")
		group.SetDescription("New versions download in the background and install when you restart.")
		group.SetVisible(false)

		uh.sysupdateStageExpander = adw.NewExpanderRow()
		uh.sysupdateStageExpander.SetTitle("System updates")
		uh.sysupdateStageExpander.SetSubtitle("Checking…")

		uh.sysupdateStageBtn = gtk.NewButtonWithLabel("Check for updates")
		uh.sysupdateStageBtn.SetValign(gtk.AlignCenterValue)
		uh.sysupdateStageBtn.AddCssClass("suggested-action")
		sysupdateClickedCb := func(btn gtk.Button) {
			uh.onSysupdateStageClicked()
		}
		uh.sysupdateStageBtn.ConnectClicked(&sysupdateClickedCb)
		uh.sysupdateStageExpander.AddSuffix(&uh.sysupdateStageBtn.Widget)

		group.Add(&uh.sysupdateStageExpander.Widget)
		page.Add(group)

		go uh.loadSysupdateUpdateStatus(group)
	}

	// Apps
	if uh.config.IsGroupEnabled("updates_page", "flatpak_updates_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("Apps")
		group.SetDescription("Updates for the apps installed on this computer.")

		uh.flatpakUpdatesExpander = adw.NewExpanderRow()
		uh.flatpakUpdatesExpander.SetTitle("Available updates")
		uh.flatpakUpdatesExpander.SetSubtitle("Checking…")
		group.Add(&uh.flatpakUpdatesExpander.Widget)

		page.Add(group)

		// Load app updates asynchronously
		uh.loadFlatpakUpdates()
	}

	// Developer tools
	if uh.config.IsGroupEnabled("updates_page", "brew_updates_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("Developer tools")
		group.SetDescription("Command-line tools you installed with Homebrew.")

		// Refreshing the catalogue is what reveals new versions, so it is
		// named for that rather than for the tool it runs.
		updateRow := adw.NewActionRow()
		updateRow.SetTitle("Check for new versions")
		updateRow.SetSubtitle("Refresh the list of tools and the versions they offer")

		updateBtn := gtk.NewButtonWithLabel("Check")
		updateBtn.SetValign(gtk.AlignCenterValue)
		updateBtn.AddCssClass("suggested-action")
		updateGate := &actionstate.Gate{}
		updateClickedCb := func(btn gtk.Button) {
			if !updateGate.TryStart() {
				return
			}
			btn.SetSensitive(false)
			btn.SetLabel("Checking…")
			go uh.updateHomebrew(btn, updateGate)
		}
		updateBtn.ConnectClicked(&updateClickedCb)

		updateRow.AddSuffix(&updateBtn.Widget)
		group.Add(&updateRow.Widget)

		uh.outdatedExpander = adw.NewExpanderRow()
		uh.outdatedExpander.SetTitle("Available updates")
		uh.outdatedExpander.SetSubtitle("Checking…")
		group.Add(&uh.outdatedExpander.Widget)

		page.Add(group)

		// Load outdated tools asynchronously
		uh.loadOutdatedPackages()
	}

	// Sources whose updates Homebrew has paused - hidden unless there is
	// something a person can act on (Homebrew 6 tap trust).
	if uh.config.IsGroupEnabled("updates_page", "brew_trust_group") {
		uh.brewTrustGroup = adw.NewPreferencesGroup()
		uh.brewTrustGroup.SetTitle("Unverified sources")
		uh.brewTrustGroup.SetDescription("Some software came from a source you have not said you trust, so it stays at the version you have. Trusting a source lets its software update again.")
		uh.brewTrustGroup.SetVisible(false)
		page.Add(uh.brewTrustGroup)

		go uh.loadUntrustedTaps()
	}

	// The two choices that replace the operating system sit last: a person
	// reaches them deliberately, never on the way to something else. Hidden
	// entirely on a host with no image descriptor, like every other
	// Bluefin-family group.
	if uh.config.IsGroupEnabled("updates_page", "channel_group") {
		uh.buildImageIdentityGroup(page)
	}
}

// loadUntrustedTaps populates the unverified-sources group. Runs in a
// goroutine; the group stays hidden when there is nothing actionable.
func (uh *UserHome) loadUntrustedTaps() {
	if !homebrew.IsInstalledCached() {
		return
	}

	taps, err := homebrew.ListUntrustedTaps()
	if err != nil {
		log.Printf("untrusted tap check failed: %v", err)
		return
	}
	if len(taps) == 0 {
		return
	}

	sgtk.RunOnMainThread(func() {
		uh.brewTrustRows = make(map[string]*adw.ActionRow)
		for _, tap := range taps {
			t := tap // capture
			presentation := pageview.UntrustedTap(t.Name, t.Formulae, t.Casks)
			row := adw.NewActionRow()
			row.SetTitle(presentation.Title)
			row.SetSubtitle(presentation.Subtitle)

			trustBtn := gtk.NewButtonWithLabel("Trust…")
			trustBtn.SetValign(gtk.AlignCenterValue)
			btn := trustBtn
			clickedCb := func(_ gtk.Button) {
				uh.confirmTrustTap(t, btn)
			}
			trustBtn.ConnectClicked(&clickedCb)
			row.AddSuffix(&trustBtn.Widget)

			uh.brewTrustGroup.Add(&row.Widget)
			uh.brewTrustRows[t.Name] = row
		}
		uh.brewTrustGroup.SetVisible(true)
	})
}

// confirmTrustTap shows a confirmation dialog before trusting a source. The
// consequence is third-party code running on this machine, which is exactly
// the kind of thing a person must agree to rather than discover.
func (uh *UserHome) confirmTrustTap(tap homebrew.UntrustedTap, button *gtk.Button) {
	dialog := adw.NewAlertDialog(
		fmt.Sprintf("Trust software from %s?", tap.Name),
		"Software from this source can run its own code on your computer while it installs and updates. Only trust sources you recognize.",
	)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("trust", "Trust")
	dialog.SetResponseAppearance("trust", adw.ResponseSuggestedValue)

	responseCb := func(_ adw.AlertDialog, response string) {
		if response != "trust" {
			return
		}
		button.SetSensitive(false)
		button.SetLabel("Trusting…")
		go uh.trustTap(tap, button)
	}
	dialog.ConnectResponse(&responseCb)
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
			uh.toastAdder.ShowErrorToast(fmt.Sprintf("Could not trust %s", tap.Name))
			return
		}

		decision := actionmsg.TapTrust(dryrun.Enabled(), tap.Name)
		if decision.MutateUI {
			if row, ok := uh.brewTrustRows[tap.Name]; ok {
				uh.brewTrustGroup.Remove(&row.Widget)
				delete(uh.brewTrustRows, tap.Name)
			}
			if len(uh.brewTrustRows) == 0 {
				uh.brewTrustGroup.SetVisible(false)
			}
			uh.toastAdder.ShowToast(decision.Toast)

			// Newly trusted software may now appear as out of date.
			uh.loadOutdatedPackages()
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

// loadOutdatedPackages loads out-of-date Homebrew tools asynchronously.
// It is reachable from trustTap (a newly-trusted source's software may now
// be out of date) as well as from buildUpdatesPage, so it must stay nil-safe
// against brew_updates_group being disabled — trustTap only depends on
// brew_trust_group and has no way to know whether outdatedExpander exists.
func (uh *UserHome) loadOutdatedPackages() {
	uh.loadOutdatedPackagesWithDone(nil)
}

// loadOutdatedPackagesWithDone starts a versioned refresh. Only the newest
// request may replace rows/count, while every supplied completion callback
// still runs so a superseded action cannot remain disabled.
func (uh *UserHome) loadOutdatedPackagesWithDone(done func(bool)) {
	generation := uh.brewRefresh.Begin()
	go uh.loadOutdatedPackagesGeneration(generation, done)
}

func (uh *UserHome) loadOutdatedPackagesGeneration(generation uint64, done func(bool)) {
	if uh.outdatedExpander == nil {
		if done != nil {
			sgtk.RunOnMainThread(func() {
				done(false)
			})
		}
		return
	}

	if !homebrew.IsInstalledCached() {
		sgtk.RunOnMainThread(func() {
			if !uh.brewRefresh.IsCurrent(generation) {
				if done != nil {
					done(false)
				}
				return
			}
			uh.updateCounts.Set(badgestate.Homebrew, 0)
			uh.updateBadgeCount()
			uh.outdatedRows.Clear(func(row *adw.ActionRow) {
				uh.outdatedExpander.Remove(&row.Widget)
			})
			uh.outdatedExpander.SetSubtitle("Homebrew is not installed")
			uh.outdatedExpander.SetEnableExpansion(false)
			if done != nil {
				done(false)
			}
		})
		return
	}

	packages, err := homebrew.ListOutdated()
	currentCount := uh.updateCounts.Get(badgestate.Homebrew)
	refresh := actionstate.OutdatedRefresh(err == nil, currentCount, len(packages))
	if err != nil {
		sgtk.RunOnMainThread(func() {
			if !uh.brewRefresh.IsCurrent(generation) {
				if done != nil {
					done(false)
				}
				return
			}
			log.Printf("refreshing outdated packages failed: %v", err)
			uh.outdatedExpander.SetSubtitle("Could not check for tool updates")
			if done != nil {
				done(false)
			}
		})
		return
	}

	sgtk.RunOnMainThread(func() {
		if !uh.brewRefresh.IsCurrent(generation) {
			if done != nil {
				done(false)
			}
			return
		}
		presentation := actionstate.OutdatedPresentation(refresh.Count)
		uh.updateCounts.Set(badgestate.Homebrew, refresh.Count)
		uh.updateBadgeCount()
		if refresh.ReplaceRows {
			uh.outdatedRows.Clear(func(row *adw.ActionRow) {
				uh.outdatedExpander.Remove(&row.Widget)
			})
		}

		uh.outdatedExpander.SetSubtitle(presentation.Subtitle)
		uh.outdatedExpander.SetEnableExpansion(presentation.Expandable)
		for _, pkg := range packages {
			row := adw.NewActionRow()
			row.SetTitle(pkg.Name)
			row.SetSubtitle(fmt.Sprintf("Version %s", pkg.Version))

			upgradeBtn := gtk.NewButtonWithLabel("Update")
			upgradeBtn.SetValign(gtk.AlignCenterValue)
			upgradeGate := &actionstate.Gate{}
			pkgName := pkg.Name
			clickedCb := func(btn gtk.Button) {
				if !upgradeGate.TryStart() {
					return
				}
				btn.SetSensitive(false)
				btn.SetLabel("Updating…")
				go func() {
					err := homebrew.Upgrade(context.Background(), pkgName)
					dryRun := dryrun.Enabled()
					decision := actionstate.PackageUpgrade(err == nil, dryRun)
					if err != nil {
						var trustErr *homebrew.UntrustedTapError
						log.Printf("upgrading %s failed: %v", pkgName, err)
						msg := fmt.Sprintf("Could not update %s", pkgName)
						if errors.As(err, &trustErr) {
							// uh.brewTrustGroup is only ever assigned once, in
							// buildUpdatesPage on the main thread before this
							// goroutine (or any goroutine) starts, so reading
							// it here is race-free.
							msg = trustmsg.UpgradeMessage(pkgName, uh.brewTrustGroup != nil)
						}
						sgtk.RunOnMainThread(func() {
							if decision.RestoreControl {
								upgradeGate.Reset()
								btn.SetSensitive(true)
								btn.SetLabel("Update")
							}
							uh.toastAdder.ShowErrorToast(msg)
						})
						return
					}
					sgtk.RunOnMainThread(func() {
						uh.toastAdder.ShowToast(actionmsg.Upgrade(dryRun, pkgName))
						if decision.RemoveRow {
							upgradeGate.Complete()
						}
						if decision.RemoveRow && uh.outdatedRows.Remove(row, func(row *adw.ActionRow) {
							uh.outdatedExpander.Remove(&row.Widget)
						}) {
							remaining := uh.updateCounts.Add(badgestate.Homebrew, -1).Count
							presentation := actionstate.OutdatedPresentation(remaining)
							uh.outdatedExpander.SetSubtitle(presentation.Subtitle)
							uh.outdatedExpander.SetEnableExpansion(presentation.Expandable)
							uh.updateBadgeCount()
						}
						if decision.RestoreControl {
							upgradeGate.Reset()
							btn.SetSensitive(true)
							btn.SetLabel("Update")
						}
						if decision.Refresh {
							uh.loadOutdatedPackages()
						}
					})
				}()
			}
			upgradeBtn.ConnectClicked(&clickedCb)

			row.AddSuffix(&upgradeBtn.Widget)
			uh.outdatedExpander.AddRow(&row.Widget)
			uh.outdatedRows.Add(row)
		}
		if done != nil {
			done(true)
		}
	})
}

// loadFlatpakUpdates starts a versioned refresh of the Flatpak update
// inventory. Only the newest request may publish its result, so a slow reload
// that is overtaken by a later one is discarded instead of restoring rows the
// later reload already retired.
func (uh *UserHome) loadFlatpakUpdates() {
	generation := uh.flatpakUpdatesRefresh.Begin()
	go uh.loadFlatpakUpdatesGeneration(generation)
}

func (uh *UserHome) loadFlatpakUpdatesGeneration(generation uint64) {
	if !flatpak.IsInstalledCached() {
		sgtk.RunOnMainThread(func() {
			if !uh.flatpakUpdatesRefresh.IsCurrent(generation) {
				return
			}
			uh.updateCounts.Set(badgestate.Flatpak, 0)
			uh.updateBadgeCount()

			if uh.flatpakUpdatesExpander != nil {
				uh.flatpakUpdatesExpander.SetSubtitle("App updates are not available on this system")
			}
		})
		return
	}

	// Collect updates from both user and system installations
	var allUpdates []flatpak.UpdateInfo

	// Load user updates. The error is kept as a value, not just logged: the
	// expander subtitle has to say that half the picture is missing.
	userUpdates, userErr := flatpak.ListUpdates(true)
	if userErr != nil {
		log.Printf("Error loading user flatpak updates: %v", userErr)
	} else {
		allUpdates = append(allUpdates, userUpdates...)
	}

	// Load system updates
	systemUpdates, systemErr := flatpak.ListUpdates(false)
	if systemErr != nil {
		log.Printf("Error loading system flatpak updates: %v", systemErr)
	} else {
		allUpdates = append(allUpdates, systemUpdates...)
	}

	status := flatpakstatus.Subtitle(len(allUpdates), userErr != nil, systemErr != nil)

	sgtk.RunOnMainThread(func() {
		if !uh.flatpakUpdatesRefresh.IsCurrent(generation) {
			return
		}

		// A load in which both queries failed knows nothing, so it keeps the
		// last known count instead of publishing an empty inventory.
		refresh := actionstate.OutdatedRefresh(
			status.Authoritative,
			uh.updateCounts.Get(badgestate.Flatpak),
			len(allUpdates),
		)
		uh.updateCounts.Set(badgestate.Flatpak, refresh.Count)
		uh.updateBadgeCount()

		if uh.flatpakUpdatesExpander == nil {
			return
		}

		if !refresh.ReplaceRows {
			// Say the check failed, but leave the previously discovered rows
			// reachable rather than wiping them.
			uh.flatpakUpdatesExpander.SetSubtitle(status.Subtitle)
			return
		}

		// Clear existing rows
		for _, row := range uh.flatpakUpdateRows {
			uh.flatpakUpdatesExpander.Remove(&row.Widget)
		}
		uh.flatpakUpdateRows = nil

		uh.flatpakUpdatesExpander.SetSubtitle(status.Subtitle)
		uh.flatpakUpdatesExpander.SetEnableExpansion(status.Expandable)

		if len(allUpdates) == 0 {
			return
		}

		for _, update := range allUpdates {
			presentation := pageview.FlatpakUpdate(
				update.Name,
				update.ApplicationID,
				update.NewVersion,
				update.Installation,
			)
			row := adw.NewActionRow()
			row.SetTitle(presentation.Title)
			row.SetSubtitle(presentation.Subtitle)

			// Add update button
			updateBtn := gtk.NewButtonWithLabel("Update")
			updateBtn.SetValign(gtk.AlignCenterValue)
			updateBtn.AddCssClass("suggested-action")

			appID := update.ApplicationID
			appName := update.Name
			if appName == "" {
				appName = appID
			}
			isUser := update.Installation == "user"
			clickedCb := func(btn gtk.Button) {
				btn.SetSensitive(false)
				btn.SetLabel("Updating…")
				go func() {
					if err := flatpak.Update(context.Background(), appID, isUser); err != nil {
						log.Printf("updating %s failed: %v", appID, err)
						sgtk.RunOnMainThread(func() {
							btn.SetSensitive(true)
							btn.SetLabel("Update")
							uh.toastAdder.ShowErrorToast(fmt.Sprintf("Could not update %s", appName))
						})
						return
					}
					sgtk.RunOnMainThread(func() {
						uh.toastAdder.ShowToast(actionmsg.Update(dryrun.Enabled(), appName))
						// Refresh the updates list. Called on the main thread so
						// concurrent completions take generations in the order
						// they finished.
						uh.loadFlatpakUpdates()
					})
				}()
			}
			updateBtn.ConnectClicked(&clickedCb)

			row.AddSuffix(&updateBtn.Widget)
			uh.flatpakUpdatesExpander.AddRow(&row.Widget)
			uh.flatpakUpdateRows = append(uh.flatpakUpdateRows, row)
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
	count := 0
	if staged {
		count = 1
	}
	uh.updateCounts.SetObserved(badgestate.Bootc, count, err == nil)
	uh.updateBadgeCount()

	sgtk.RunOnMainThread(func() {
		group.SetVisible(true)
		if err != nil {
			log.Printf("reading system status failed: %v", err)
			uh.bootcStageExpander.SetSubtitle("The system's update status could not be read")
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
// bounded rolling log. Both staging providers share it because
// bootc.ProgressEvent and sysupdate.ProgressEvent are the same
// stageexec.ProgressEvent.
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
		count := 0
		if staged {
			count = 1
		}
		uh.updateCounts.SetObserved(badgestate.Bootc, count, statusErr == nil)
		uh.updateBadgeCount()

		sgtk.RunOnMainThread(func() {
			spinner.Stop()
			button.SetSensitive(true)
			button.SetLabel("Check for updates")

			if stageErr != nil {
				log.Printf("staging the system update failed: %v", stageErr)
				expander.SetSubtitle("The update could not be downloaded. Open Details to see what happened.")
				uh.toastAdder.ShowErrorToast("The system update could not be downloaded")
				return
			}

			if statusErr == nil {
				uh.refreshChangelogAvailability(status)
			}
			if statusErr != nil {
				message := fmt.Sprintf("Could not verify staged update: %v", statusErr)
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
		})
	}()
}

// loadSysupdateUpdateStatus gates the native A/B updates group and reflects
// the /run/snosi state files in the expander subtitle and update badge. The
// reads are unprivileged: the stager's state files are world-readable.
func (uh *UserHome) loadSysupdateUpdateStatus(group *adw.PreferencesGroup) {
	if !sysupdate.IsNativeABCached() || !sysupdate.StageScriptAvailable() {
		return // group stays hidden
	}

	status, statusErr := sysupdate.GetStatus()
	count := 0
	if status.IsStaged() {
		count = 1
	}
	uh.updateCounts.SetObserved(badgestate.Sysupdate, count, statusErr == nil)
	uh.updateBadgeCount()

	outcome, version, checkedAt := status.Presentation()
	sgtk.RunOnMainThread(func() {
		group.SetVisible(true)
		uh.sysupdateStageExpander.SetSubtitle(pageview.SysupdateUpdateSubtitle(outcome, version, checkedAt))
	})
}

// onSysupdateStageClicked runs the snosi stager with streamed log output.
// The script checks, downloads, and stages in one idempotent operation; the
// downloaded version installs at the next restart.
func (uh *UserHome) onSysupdateStageClicked() {
	button := uh.sysupdateStageBtn
	expander := uh.sysupdateStageExpander

	button.SetSensitive(false)
	button.SetLabel("Working…")
	expander.SetExpanded(true)
	expander.SetSubtitle("Checking for updates…")

	// Remove rows from any previous run before adding new ones, otherwise
	// repeated clicks stack duplicate Progress/Details rows.
	if uh.sysupdateActivityRow != nil {
		expander.Remove(&uh.sysupdateActivityRow.Widget)
	}
	if uh.sysupdateLogExpander != nil {
		expander.Remove(&uh.sysupdateLogExpander.Widget)
	}

	// Activity row with a spinner (the stage script emits no percentages,
	// so progress is indeterminate).
	activityRow := adw.NewActionRow()
	activityRow.SetTitle("Progress")
	activityRow.SetSubtitle("Working…")
	spinner := gtk.NewSpinner()
	spinner.Start()
	activityRow.AddSuffix(&spinner.Widget)
	expander.AddRow(&activityRow.Widget)
	uh.sysupdateActivityRow = activityRow

	logExpander := adw.NewExpanderRow()
	logExpander.SetTitle("Details")
	logExpander.SetSubtitle(pageview.StagingLogSubtitle(0, 0))
	expander.AddRow(&logExpander.Widget)
	uh.sysupdateLogExpander = logExpander

	go func() {
		ctx, cancel := sysupdate.DefaultContext()
		defer cancel()

		progressCh := make(chan sysupdate.ProgressEvent)

		var stageErr error
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			stageErr = sysupdate.StageUpdate(ctx, progressCh)
		}()

		// Discarded for the same reason as the bootc path above.
		newStageProgressSink(activityRow, logExpander).consume(progressCh)

		wg.Wait()

		// Re-read the state files so the subtitle and badge reflect reality
		// (staged vs already-current) rather than guessing from output. A
		// stage fills the inactive slot with the newer version.
		status, statusErr := sysupdate.GetStatus()

		staged := status.IsStaged()
		count := 0
		if staged {
			count = 1
		}
		uh.updateCounts.SetObserved(badgestate.Sysupdate, count, statusErr == nil)
		uh.updateBadgeCount()

		_, version, _ := status.Presentation()
		sgtk.RunOnMainThread(func() {
			spinner.Stop()
			button.SetSensitive(true)
			button.SetLabel("Check for updates")

			if stageErr != nil {
				log.Printf("staging the system update failed: %v", stageErr)
				expander.SetSubtitle("The update could not be downloaded. Open Details to see what happened.")
				uh.toastAdder.ShowErrorToast("The system update could not be downloaded")
				return
			}

			if statusErr != nil {
				message := fmt.Sprintf("Could not verify staged update: %v", statusErr)
				expander.SetSubtitle(message)
				if dryrun.Enabled() {
					uh.toastAdder.ShowToast(actionmsg.SystemStage(true, false))
				} else {
					uh.toastAdder.ShowErrorToast(message)
				}
				return
			}

			expander.SetSubtitle(pageview.SysupdateStageResultSubtitle(staged, version))
			uh.toastAdder.ShowToast(actionmsg.SystemStage(dryrun.Enabled(), staged))
		})
	}()
}

// updateHomebrew refreshes Homebrew's catalogue, then reloads the
// out-of-date list before restoring the top-level action.
func (uh *UserHome) updateHomebrew(button gtk.Button, gate *actionstate.Gate) {
	err := homebrew.Update(context.Background())
	dryRun := dryrun.Enabled()
	decision := actionstate.MetadataUpdate(err == nil, dryRun)
	if err != nil {
		log.Printf("refreshing the Homebrew catalogue failed: %v", err)
		sgtk.RunOnMainThread(func() {
			if decision.RestoreControl {
				gate.Reset()
				button.SetSensitive(true)
				button.SetLabel("Check")
			}
			uh.toastAdder.ShowErrorToast("Could not check for new tool versions")
		})
		return
	}

	sgtk.RunOnMainThread(func() {
		uh.toastAdder.ShowToast(actionmsg.SelfUpdate(dryRun, "Homebrew"))
		if decision.RestoreControl {
			gate.Reset()
			button.SetSensitive(true)
			button.SetLabel("Check")
		}
		if decision.Refresh {
			uh.loadOutdatedPackagesWithDone(func(bool) {
				gate.Reset()
				button.SetSensitive(true)
				button.SetLabel("Check")
			})
		}
	})
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
	details.SetSubtitle("Identifiers to quote when asking for help")
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
	staged := ""
	if status.Status.Staged != nil {
		staged = status.Status.Staged.Version()
	}
	presentation := pageview.SystemVersionRow(booted.Version(), booted.Timestamp(), staged)
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
	row.SetSubtitle("Early updates and graphics driver choices can't be changed until this system's update settings are repaired.")
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
	group.SetDescription("These replace the operating system itself. Both need your administrator password, a large download, and a restart.")

	onTesting := status.Channel == imageinfo.ChannelTesting
	switchable := status.CanSwitchTo != imageinfo.ChannelUnknown
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

	recommended := ""
	if status.RecommendedDriver != "" {
		recommended = status.RecommendedDriver.DisplayName()
	}
	presentation := pageview.GraphicsDriverRow(status.Driver.DisplayName(), status.GPU, recommended)

	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	if status.RecommendedDriver != "" {
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
				uh.toastAdder.ShowErrorToast("Could not switch the graphics driver")
				return
			}

			decision := actionmsg.DriverSwitch(dryrun.Enabled(), driver.DisplayName())
			if decision.Confirm {
				row.SetSubtitle(pageview.GraphicsDriverResultSubtitle(driver.DisplayName()))
				button.SetVisible(false)
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
				uh.toastAdder.ShowErrorToast("Could not change when this system gets updates")
				return
			}

			decision := actionmsg.ChannelSwitch(dryrun.Enabled(), toTesting)
			toggle.set(decision.Confirm == toTesting)
			if decision.Confirm {
				row.SetSubtitle(pageview.ChannelSwitchResultSubtitle(toTesting))
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
