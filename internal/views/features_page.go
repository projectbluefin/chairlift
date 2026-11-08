package views

import (
	"context"
	"errors"
	"log"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/devmenu"
	"github.com/projectbluefin/chairlift/internal/devsetup"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/updex"
	"github.com/projectbluefin/chairlift/internal/updexhelper"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/featurestatus"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// guardedSwitch is a gtk.Switch together with the guard every switch on this
// page needs. GtkSwitch emits ::state-set from gtk_switch_set_active whenever
// the value actually changes, and a user's click reaches the handler by that
// same path — so ChairLift's own writes are indistinguishable from a person
// flipping the switch unless they are marked. Unmarked, restoring observed
// state on load or after a failure or dry run would trigger a real mutation.
type guardedSwitch struct {
	widget *gtk.Switch
	// applying is true only while set() moves the switch.
	applying bool
	// handler is retained for the page's lifetime because puregotk keys its
	// fixed callback table on the address of the func variable.
	handler func(gtk.Switch, bool) bool
}

// newGuardedSwitch builds a switch showing active and connects onUserChange
// once, at page-build time. onUserChange runs only for a change the user made.
func newGuardedSwitch(active bool, onUserChange func(bool)) *guardedSwitch {
	guarded := &guardedSwitch{widget: gtk.NewSwitch()}
	guarded.widget.SetValign(gtk.AlignCenterValue)
	guarded.widget.SetActive(active)
	guarded.widget.SetState(active)

	guarded.handler = func(_ gtk.Switch, state bool) bool {
		if guarded.applying {
			// ChairLift moved the switch. Let the default handler render
			// the new position and do nothing else.
			return false
		}
		onUserChange(state)
		// Hold the rendered state until the work resolves; set() confirms
		// or reverts it.
		return true
	}
	guarded.widget.ConnectStateSet(&guarded.handler)
	return guarded
}

// set moves the switch without treating it as a user action.
func (g *guardedSwitch) set(active bool) {
	g.applying = true
	g.widget.SetActive(active)
	g.widget.SetState(active)
	g.applying = false
}

// buildFeaturesPage builds the Features page content.
func (uh *UserHome) buildFeaturesPage() {
	page := uh.featuresPrefsPage
	if page == nil {
		return
	}

	bluefinGroups := uh.buildBluefinGroups(page)
	terminal := uh.groupEnabled("features_page", "bluefin_cli_group")
	if terminal {
		uh.buildBluefinCLIGroup(page)
	}
	desktopIntegrations := uh.groupEnabled("features_page", "desktop_integrations_group")
	if desktopIntegrations {
		uh.buildShellExtensionsGroup(page)
	}

	// Printer applications sit under Developer and Gaming: like them they
	// are capabilities you turn on, and each is a user-scope service.
	printers := uh.groupEnabled("features_page", "printers_group")
	if printers {
		uh.buildPrintersGroup(page)
	}
	compassEnabled := uh.groupEnabled("features_page", "compass_group")
	if compassEnabled {
		uh.buildCompassGroup(page)
	}
	uh.buildHardwareGroup(page)

	// A host that offers nothing here — no ublue-os descriptor, no Podman,
	// and no optional features — would otherwise show a blank page. The
	// empty state starts hidden and is revealed only once nothing is known
	// to be on offer.
	emptyState := adw.NewStatusPage()
	emptyState.SetIconName("application-x-addon-symbolic")
	// Compact: it sits inside the page's own scroller and clamp, so the
	// full-page status layout's large icon and margins do not apply.
	emptyState.AddCssClass("compact")
	emptyGroup := adw.NewPreferencesGroup()
	emptyGroup.Add(&emptyState.Widget)
	emptyGroup.SetVisible(false)
	showEmptyState := func(optionalFeatures bool) {
		text, empty := pageview.FeaturesEmptyState(bluefinGroups || terminal || desktopIntegrations || compassEnabled, printers, optionalFeatures)
		if empty {
			emptyState.SetTitle(text.Title)
			emptyState.SetDescription(text.Subtitle)
			log.Printf("views: features page offers nothing on this system")
		}
		emptyGroup.SetVisible(empty)
	}

	// Troubleshooting and Agent Mode both live on Agents (issues
	// #249 and #244), so neither is built here.

	featuresEnabled := uh.groupEnabled("features_page", "features_group")
	if featuresEnabled {
		// Build the features group (hidden once updex reports none)
		uh.featuresGroup = adw.NewPreferencesGroup()
		uh.featuresGroup.SetTitle("Optional Features")
		uh.featuresGroup.SetDescription("Checking what this computer offers…")

		// Add Update button as header suffix (disabled until features are listed)
		updateBtn := gtk.NewButtonWithLabel("Update")
		updateBtn.SetValign(gtk.AlignCenterValue)
		updateBtn.AddCssClass("suggested-action")
		updateBtn.SetSensitive(false)
		updateClickedCb := func(btn gtk.Button) {
			uh.onUpdateFeaturesClicked(updateBtn)
		}
		updateBtn.ConnectClicked(&updateClickedCb)
		uh.featuresGroup.SetHeaderSuffix(&updateBtn.Widget)

		page.Add(uh.featuresGroup)

		go uh.loadFeatures(updateBtn, showEmptyState)
	}

	page.Add(emptyGroup)
	// Until updex answers, a still-checking features group is on offer.
	showEmptyState(featuresEnabled)
}

// loadFeatures loads feature information asynchronously. A host that offers
// no features hides the group rather than rendering an inert one, and tells
// onListed so the page can explain itself if nothing else is on offer; a
// failed read is not evidence of that, so it keeps the group and says so.
func (uh *UserHome) loadFeatures(updateBtn *gtk.Button, onListed func(optionalFeatures bool)) {
	ctx, cancel := updex.DefaultContext()
	defer cancel()

	features, err := updex.ListFeatures(ctx)

	sgtk.RunOnMainThread(func() {
		if uh.featuresGroup == nil {
			return
		}

		if err != nil {
			log.Printf("views: listing optional features failed: %v", err)
			uh.featuresGroup.SetDescription("Couldn't check which features are available.")
			return
		}

		if len(features) == 0 {
			uh.featuresGroup.SetVisible(false)
			onListed(false)
			return
		}

		updateBtn.SetSensitive(true)
		uh.featuresGroup.SetDescription(pageview.FeatureGroupDescription(len(features)))
		uh.featureRows = make(map[string]*adw.ActionRow)

		for _, feat := range features {
			presentation := pageview.Feature(feat.Name, feat.Description)
			row := adw.NewActionRow()
			row.SetTitle(presentation.Title)
			row.SetSubtitle(presentation.Subtitle)

			featName := feat.Name
			var toggle *guardedSwitch
			toggle = newGuardedSwitch(feat.Enabled, func(state bool) {
				uh.onFeatureToggled(featName, state, toggle)
			})

			row.AddSuffix(&toggle.widget.Widget)
			row.SetActivatableWidget(&toggle.widget.Widget)
			uh.featuresGroup.Add(&row.Widget)
			uh.featureRows[feat.Name] = row
		}

		// Check for updates after rendering the feature list
		go uh.checkFeatureUpdates(len(features))
	})
}

// checkFeatureUpdates checks enabled features for available updates
func (uh *UserHome) checkFeatureUpdates(totalFeatures int) {
	ctx, cancel := updex.DefaultContext()
	defer cancel()

	checks, warnings, err := updex.CheckFeatures(ctx)

	sgtk.RunOnMainThread(func() {
		// Warnings are retained even when the check fails, so they are logged
		// before the error path returns rather than discarded with it.
		for _, w := range warnings {
			log.Printf("Feature update check warning: %s", w)
		}

		if err != nil {
			log.Printf("Feature update check failed: %v", err)
			if uh.featuresGroup != nil {
				uh.featuresGroup.SetDescription(featurestatus.GroupDescriptionCheckFailed(totalFeatures))
			}
			return
		}

		incomplete := len(warnings) > 0
		updateCount := 0
		for _, check := range checks {
			row, ok := uh.featureRows[check.Feature]
			if !ok {
				continue
			}

			status, ok := featurestatus.Feature(check.Feature, check.Results)
			if !ok {
				continue
			}

			row.SetSubtitle(status.Subtitle)
			if status.HasUpdate {
				updateCount++
			}
			if status.Incomplete {
				incomplete = true
			}
		}

		if uh.featuresGroup != nil {
			if incomplete {
				uh.featuresGroup.SetDescription(featurestatus.GroupDescriptionIncomplete(totalFeatures, updateCount))
			} else {
				uh.featuresGroup.SetDescription(featurestatus.GroupDescription(totalFeatures, updateCount))
			}
		}
	})
}

// onFeatureToggled handles enabling/disabling a feature. updateFeaturesGate
// admits one updex helper mutation at a time — a toggle or the Update button
// — so a second click while the first is authenticating or running cannot
// start an overlapping enable and disable whose completions race to set the
// switch. The switch is insensitive while its own change runs.
func (uh *UserHome) onFeatureToggled(name string, enabled bool, toggle *guardedSwitch) {
	if !uh.updateFeaturesGate.TryStart() {
		// Revert after the state-set handler returns, as the other
		// switches on this page do, rather than re-entering it.
		sgtk.RunOnMainThread(func() { toggle.set(!enabled) })
		uh.toastAdder.ShowToast(pageview.FeatureChangeBusyToast)
		return
	}
	toggle.widget.SetSensitive(false)
	go func() {
		command := updexhelper.CommandDisableFeature
		if enabled {
			command = updexhelper.CommandEnableFeature
		}
		ctx, cancel := updex.MutationContext(command)
		defer cancel()

		var err error
		if enabled {
			err = updex.EnableFeature(ctx, name)
		} else {
			err = updex.DisableFeature(ctx, name)
		}

		sgtk.RunOnMainThread(func() {
			defer uh.updateFeaturesGate.Reset()
			toggle.widget.SetSensitive(true)
			if err != nil {
				// Revert switch to previous state
				toggle.set(!enabled)
				log.Printf("views: changing feature %s failed: %v", name, err)
				uh.toastAdder.ShowErrorToast(pkexec.UserMessage(err, "Couldn't change this feature. Try again."))
				return
			}

			decision := actionmsg.FeatureToggle(dryrun.Enabled(), enabled, name)
			if decision.Confirm {
				// Confirm the visual state change
				toggle.set(enabled)
			} else {
				// Nothing actually changed under dry-run; revert to the
				// pre-click state.
				toggle.set(!enabled)
			}

			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

// onUpdateFeaturesClicked handles the Update button click. updateFeaturesGate
// admits one run at a time, shared with the feature toggles, and is reset on
// every completion, because the button is offered again after each run.
func (uh *UserHome) onUpdateFeaturesClicked(button *gtk.Button) {
	if !uh.updateFeaturesGate.TryStart() {
		uh.toastAdder.ShowToast(pageview.FeatureChangeBusyToast)
		return
	}
	button.SetSensitive(false)
	button.SetLabel("Updating…")

	go func() {
		ctx, cancel := updex.MutationContext(updexhelper.CommandUpdate)
		defer cancel()

		err := updex.UpdateFeatures(ctx)

		sgtk.RunOnMainThread(func() {
			defer uh.updateFeaturesGate.Reset()
			button.SetSensitive(true)
			button.SetLabel("Update")

			if err != nil {
				log.Printf("views: updating features failed: %v", err)
				uh.toastAdder.ShowErrorToast(pkexec.UserMessage(err, "Couldn't update features. Try again."))
				return
			}

			uh.toastAdder.ShowToast(actionmsg.FeatureUpdate(dryrun.Enabled()))
		})
	}()
}

// ── Bluefin-family groups ────────────────────────────────────────────────
//
// The groups below are ChairLift's port of bluefinctl's developer and gaming
// capabilities to Bluefin, Bluefin LTS, and Dakota. They are independent of the
// updex features group above: updex is Snow Linux's feature manager and does
// not exist on a Bluefin host, so hanging these off updex availability would
// make them invisible on exactly the systems they are for. Each group
// instead hides itself when internal/ublue reports no ublue-os image
// descriptor, which is every non-Bluefin host including Snow Linux.

// buildBluefinGroups builds the developer-tools and gaming groups and reports
// whether it built either. The release channel and graphics driver live
// elsewhere: they describe which system this machine runs, where these two
// are capabilities you switch on.
func (uh *UserHome) buildBluefinGroups(page *adw.PreferencesPage) bool {
	dxEnabled := uh.groupEnabled("features_page", "dx_group")
	gamingEnabled := uh.groupEnabled("features_page", "gaming_group")

	if !dxEnabled && !gamingEnabled {
		return false
	}

	// One detection serves both groups. It reads only local files, so
	// it is cheap, but it is still done off the main thread and cached.
	status := ublue.StatusCached()
	if !status.Available {
		return false
	}

	// A single structured readiness marker. The screenshot walkthrough
	// asserts on this line to confirm the captured session really built the
	// Bluefin-family rows, rather than capturing a page where they were
	// silently hidden and calling the frame "rendered".
	log.Printf("views: bluefin groups built variant=%s tag=%s channel=%s switchable=%v developer=%v dx_group=%v gaming_group=%v",
		status.Variant, status.Tag, status.Channel,
		status.CanSwitchTo != imageinfo.ChannelUnknown, status.Developer,
		dxEnabled, gamingEnabled)

	if gamingEnabled {
		uh.buildGamingGroup(page, status)
	}

	// Optional user tools remain discoverable even without installed access
	// actions; the privileged switch stays insensitive and explains why.
	if dxEnabled {
		uh.buildDeveloperEntryRow(page, status)
	}
	return true
}

// buildDeveloperGroup builds the developer-tools switch.
func (uh *UserHome) buildDeveloperGroup(page *adw.PreferencesPage, status ublue.Status) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Developer")
	group.SetDescription("Tools for building and running software on this computer.")

	presentation := pageview.DeveloperRow(status.Developer)

	row := adw.NewExpanderRow()
	row.SetUseMarkup(false)
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)
	uh.developerSpinner = newActivitySpinner()
	row.AddSuffix(&uh.developerSpinner.Widget)

	dxRow := row
	var toggle *guardedSwitch
	toggle = newGuardedSwitch(status.Developer, func(state bool) {
		uh.onDeveloperToggled(state, toggle, dxRow)
	})
	SetAccessibleLabel(toggle.widget, presentation.Title)
	uh.developerCanToggle = status.Supports(ubluehelper.CommandDXEnable, ubluehelper.CommandDXDisable)
	if !uh.developerCanToggle {
		toggle.widget.SetSensitive(false)
		row.SetSubtitle("Developer Mode isn't available on this computer. You can still install tools below.")
	}

	row.AddSuffix(&toggle.widget.Widget)
	uh.addDeveloperContents(row)
	group.Add(&row.Widget)

	page.Add(group)
	uh.developerGroup = group
	uh.developerRow = row
	uh.developerSwitch = toggle.widget
	uh.developerToggle = toggle
	uh.developerEnabled = status.Developer
	uh.buildDeveloperOptions(group, status)
}

// buildDeveloperEntryRow builds the entry row to Developer Mode detail subpage.
func (uh *UserHome) buildDeveloperEntryRow(page *adw.PreferencesPage, status ublue.Status) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Developer")
	row := adw.NewActionRow()
	presentation := pageview.DeveloperRow(status.Developer)
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)
	row.SetActivatable(true)
	icon := gtk.NewImageFromIconName("go-next-symbolic")
	row.AddSuffix(&icon.Widget)
	rowActivatedCb := func(r adw.ActionRow) {
		if uh.openDeveloperDetail != nil {
			uh.openDeveloperDetail()
		}
	}
	row.ConnectActivated(&rowActivatedCb)
	group.Add(&row.Widget)
	page.Add(group)
	uh.developerEntryRow = row
}

func (uh *UserHome) buildGamingGroup(page *adw.PreferencesPage, status ublue.Status) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Gaming")
	group.SetDescription("Switch system images for Game Mode. Requires a restart; your Flatpaks are kept.")

	// Game Mode stages a system image switch; it never manages Flatpaks.
	gameModeRow := adw.NewActionRow()
	gameModeRow.SetUseMarkup(false)
	gameModeSwitchable := status.CanSwitchGaming && status.Supports(ubluehelper.CommandGamingSwitch)
	uh.gameModeSwitchable = gameModeSwitchable
	gameModePresentation := pageview.GameModeRow(status.Gaming, gameModeSwitchable)
	gameModeRow.SetTitle(gameModePresentation.Title)
	gameModeRow.SetSubtitle(gameModePresentation.Subtitle)

	var gameModeToggle *guardedSwitch
	gameModeToggle = newGuardedSwitch(status.Gaming, func(state bool) {
		uh.onGameModeToggled(state, gameModeToggle, gameModeRow)
	})
	SetAccessibleLabel(gameModeToggle.widget, gameModePresentation.Title)
	gameModeToggle.widget.SetSensitive(gameModeSwitchable && uh.stagedImageLoaded && uh.pendingImage == "")
	gameModeRow.AddSuffix(&gameModeToggle.widget.Widget)
	if gameModeSwitchable {
		gameModeRow.SetActivatableWidget(&gameModeToggle.widget.Widget)
	}
	group.Add(&gameModeRow.Widget)
	uh.gameModeRow = gameModeRow
	uh.gameModeSwitch = gameModeToggle.widget
	uh.gameModeToggle = gameModeToggle
	page.Add(group)
}

func (uh *UserHome) onGameModeToggled(enabled bool, toggle *guardedSwitch, row *adw.ActionRow) {
	if uh.pendingImage != "" {
		sgtk.RunOnMainThread(func() { toggle.set(!enabled) })
		uh.toastAdder.ShowToast("A system switch is already waiting for a restart")
		return
	}
	if enabled {
		uh.confirmGameModeEnable(toggle, row)
		return
	}
	uh.runGameModeSwitch(false, toggle, row)
}

func (uh *UserHome) confirmGameModeEnable(toggle *guardedSwitch, row *adw.ActionRow) {
	if !uh.gameModeGate.TryStart() {
		sgtk.RunOnMainThread(func() { toggle.set(false) })
		return
	}
	title, body := pageview.GameModeConfirmation()
	dialog := adw.NewAlertDialog(title, body)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("switch", "Switch to Game Mode")
	dialog.SetResponseAppearance("switch", adw.ResponseSuggestedValue)
	dialog.SetDefaultResponse("switch")
	dialog.SetCloseResponse("cancel")
	uh.gameModeDialogs.connect(dialog, func(response string) {
		if response == "switch" {
			uh.runGameModeSwitchConfirmed(true, toggle, row)
		} else {
			uh.gameModeGate.Reset()
			toggle.set(false)
		}
	})
	dialog.Present(&uh.featuresPage.Widget)
}

func (uh *UserHome) runGameModeSwitch(enabled bool, toggle *guardedSwitch, row *adw.ActionRow) {
	if !uh.gameModeGate.TryStart() {
		sgtk.RunOnMainThread(func() { toggle.set(!enabled) })
		return
	}
	uh.runGameModeSwitchConfirmed(enabled, toggle, row)
}

func (uh *UserHome) runGameModeSwitchConfirmed(enabled bool, toggle *guardedSwitch, row *adw.ActionRow) {
	if uh.pendingImage != "" {
		uh.gameModeGate.Reset()
		sgtk.RunOnMainThread(func() { toggle.set(!enabled) })
		uh.toastAdder.ShowToast("A system switch is already waiting for a restart")
		return
	}
	title := "Turning off Game Mode"
	if enabled {
		title = "Turning on Game Mode"
	}
	if !uh.updateShell.beginMutation(title) {
		uh.gameModeGate.Reset()
		sgtk.RunOnMainThread(func() { toggle.set(!enabled) })
		uh.toastAdder.ShowToast(pageview.UpdateBusyToast)
		return
	}
	toggle.widget.SetSensitive(false)
	before := row.GetSubtitle()
	row.SetSubtitle("Switching…")

	go func() {
		ctx, cancel := ublue.ImageSwitchContext()
		defer cancel()

		err := ublue.SetGamingMode(ctx, enabled)

		sgtk.RunOnMainThread(func() {
			uh.updateShell.finishMutation()
			uh.gameModeGate.Reset()

			if err != nil {
				toggle.widget.SetSensitive(true)
				toggle.set(!enabled)
				row.SetSubtitle(before)
				log.Printf("views: switching Game Mode failed: %v", err)
				uh.showPrivilegedFailure(err, "Couldn't switch Game Mode")
				return
			}

			decision := actionmsg.GameModeSwitch(dryrun.Enabled(), enabled)
			if decision.Confirm {
				// The new image deployment is staged for the next boot.
				// Keep the switch insensitive and flipped to reflect the staged
				// target until the machine is rebooted, preventing contradictory
				// re-requests before restart.
				uh.pendingImage = "gaming"
				toggle.set(enabled)
				toggle.widget.SetSensitive(false)
				row.SetSubtitle(pageview.GameModeResultSubtitle(enabled))
				uh.refreshAfterOSSwitch()
			} else {
				// In dry run, revert the switch to the booted state and re-enable it.
				toggle.set(!enabled)
				toggle.widget.SetSensitive(true)
				row.SetSubtitle(before)
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

func (uh *UserHome) applyStagedImage(status *bootc.Status) {
	if status == nil || status.Status.Booted == nil || status.Status.Staged == nil {
		return
	}
	target, isGaming, pending := imageinfo.PendingImageSwitch(
		status.Status.Booted.ImageRef(),
		status.Status.Staged.ImageRef(),
	)
	if !pending {
		return
	}
	uh.pendingImage = target
	if uh.channelSwitch != nil {
		uh.channelSwitch.SetSensitive(false)
	}
	if uh.channelRow != nil {
		uh.channelRow.SetSubtitle("Restart to finish installing the staged operating system.")
	}
	if uh.driverButton != nil {
		uh.driverButton.SetVisible(false)
	}
	if uh.driverRow != nil {
		uh.driverRow.SetSubtitle("Restart to finish installing the staged operating system.")
	}
	if uh.gameModeToggle != nil {
		uh.gameModeToggle.set(isGaming)
		uh.gameModeToggle.widget.SetSensitive(false)
	}
	if uh.gameModeRow != nil {
		uh.gameModeRow.SetSubtitle(pageview.GameModeResultSubtitle(isGaming))
	}
}
func (uh *UserHome) loadInitialStagedDeployment() {
	ctx, cancel := bootc.DefaultContext()
	defer cancel()

	status, err := bootc.GetStatus(ctx)
	sgtk.RunOnMainThread(func() {
		if err != nil {
			var notFound *bootc.NotFoundError
			if errors.As(err, &notFound) {
				// Non-bootc host: bootc binary absent, no staged deployment possible.
				uh.stagedImageLoaded = true
				uh.applyInitialStagedSettled()
				return
			}
			log.Printf("views: reading initial staged deployment status failed: %v", err)
			// Fail closed: retain replacement controls insensitive with an explanatory subtitle.
			if uh.channelRow != nil {
				uh.channelRow.SetSubtitle("Unavailable right now: couldn't verify the system update state.")
			}
			if uh.gameModeRow != nil {
				uh.gameModeRow.SetSubtitle("Unavailable right now: couldn't verify the system update state.")
			}
			return
		}
		uh.stagedImageLoaded = true
		if status != nil {
			uh.applyStagedImage(status)
		}
		uh.applyInitialStagedSettled()
	})
}

func (uh *UserHome) applyInitialStagedSettled() {
	if uh.pendingImage != "" {
		return
	}
	if uh.channelSwitch != nil {
		uh.channelSwitch.SetSensitive(uh.channelSwitchable)
	}
	if uh.driverButton != nil {
		uh.driverButton.SetSensitive(uh.driverSwitchable)
	}
	if uh.gameModeToggle != nil {
		uh.gameModeToggle.widget.SetSensitive(uh.gameModeSwitchable)
	}
}

// onDeveloperToggled grants or withdraws this account's developer access.
func (uh *UserHome) onDeveloperToggled(enabled bool, toggle *guardedSwitch, row *adw.ExpanderRow) {
	uh.runDeveloperToggle(enabled, toggle, row, false)
}

func (uh *UserHome) runDeveloperToggle(enabled bool, toggle *guardedSwitch, row *adw.ExpanderRow, retry bool) {
	if !uh.developerCanToggle || !uh.developerGate.TryStart() {
		sgtk.RunOnMainThread(func() { toggle.set(uh.developerEnabled) })
		return
	}
	request := devsetup.Request{Developer: uh.developerEnabled, ChangeDeveloper: enabled != uh.developerEnabled, EnableDeveloper: enabled, Preview: dryrun.Enabled()}
	tasks := uh.developerPostTasks(enabled, request.ChangeDeveloper || retry)
	if !uh.developerSetupActivity.begin(row.GetTitle(), setupStages(request.ChangeDeveloper, tasks), retry, func() {
		uh.runDeveloperToggle(enabled, toggle, row, true)
	}) {
		uh.developerGate.Reset()
		sgtk.RunOnMainThread(func() { toggle.set(uh.developerEnabled) })
		return
	}
	previous := uh.developerSetupActivity.completed
	before := row.GetSubtitle()
	uh.setDeveloperSensitive(false)
	setActivitySpinner(uh.developerSpinner, true)
	go func() {
		ctx := context.Background()
		var skipped []string
		var menuErr error
		var grantSucceeded bool
		result := devsetup.Run(ctx, request, func() error {
			helperCtx, cancel := ublue.DefaultContext()
			defer cancel()
			var err error
			skipped, err = ublue.SetDeveloperMode(helperCtx, enabled)
			grantSucceeded = err == nil
			return err
		}, tasks, previous, uh.developerSetupReport(enabled))
		if grantSucceeded {
			menuCtx, cancel := ublue.DefaultContext()
			menuErr = devmenu.Apply(menuCtx, enabled)
			cancel()
		}
		sgtk.RunOnMainThread(func() {
			defer uh.developerGate.Reset()
			setActivitySpinner(uh.developerSpinner, false)
			uh.developerEnabled = result.Developer
			toggle.set(result.Developer)
			row.SetSubtitle(before)
			if !request.Preview && request.ChangeDeveloper && result.Developer == enabled {
				row.SetSubtitle(pageview.DeveloperResultSubtitle(enabled))
			}
			uh.developerSetupActivity.finish(result, request.Preview)
			if uh.developerEntryRow != nil {
				uh.developerEntryRow.SetSubtitle(pageview.DeveloperRow(result.Developer).Subtitle)
			}
			uh.setDeveloperSensitive(true)
			if menuErr != nil {
				log.Printf("views: updating custom command menu: %v", menuErr)
				uh.toastAdder.ShowErrorToast("Couldn't update the developer entries in the top bar menu.")
			}
			if result.Promoted {
				uh.openDeveloperOnboarding(true, true)
			}
			if !request.Preview && len(result.Completed) > 0 {
				uh.homebrewInventoryChanged()
			}
			if result.Err != nil {
				log.Printf("views: developer setup failed: %v", result.Err)
				message := "Couldn't change Developer Mode. Try again."
				if result.Developer && enabled {
					message = "Developer access is enabled, but tool setup needs attention. Retry the failed steps."
				}
				uh.toastAdder.ShowErrorToast(pkexec.UserMessage(result.Err, message))
				return
			}
			uh.toastAdder.ShowToast(actionmsg.DeveloperMode(request.Preview, enabled, skipped).Toast)
		})
	}()
}

// openDeveloperOnboarding opens only after the original helper grants access.
func (uh *UserHome) openDeveloperOnboarding(enabled, succeeded bool) {
	for _, link := range pageview.DeveloperOnboardingTargets(dryrun.Enabled(), enabled, succeeded) {
		uh.openURL(link.URL)
	}
}
