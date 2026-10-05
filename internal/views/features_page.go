package views

import (
	"fmt"
	"log"
	"slices"

	"github.com/projectbluefin/chairlift/internal/developerfeeds"
	"github.com/projectbluefin/chairlift/internal/devmenu"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/gaming"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/updex"
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
// flipping the switch unless they are marked. Unmarked, showing the machine's
// real state on load would run the action that produces it (installing the
// gaming applications on a machine that already has them), and reverting
// after a failure or a dry run would run the opposite action for real.
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

	// A host that offers nothing here — no ublue-os descriptor, no Podman,
	// and no optional features — would otherwise show a blank page. The
	// empty state starts hidden and is revealed only once nothing is known
	// to be on offer.
	emptyState := adw.NewStatusPage()
	emptyState.SetIconName("preferences-other-symbolic")
	emptyGroup := adw.NewPreferencesGroup()
	emptyGroup.Add(&emptyState.Widget)
	emptyGroup.SetVisible(false)
	showEmptyState := func(optionalFeatures bool) {
		text, empty := pageview.FeaturesEmptyState(bluefinGroups || desktopIntegrations, printers, optionalFeatures)
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
		uh.featuresGroup.SetTitle("Optional features")
		uh.featuresGroup.SetDescription("Checking what this system offers…")

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
			uh.featuresGroup.SetDescription("Could not check which features are available.")
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

// onFeatureToggled handles enabling/disabling a feature
func (uh *UserHome) onFeatureToggled(name string, enabled bool, toggle *guardedSwitch) {
	go func() {
		ctx, cancel := updex.DefaultContext()
		defer cancel()

		var err error
		if enabled {
			err = updex.EnableFeature(ctx, name)
		} else {
			err = updex.DisableFeature(ctx, name)
		}

		sgtk.RunOnMainThread(func() {
			if err != nil {
				// Revert switch to previous state
				toggle.set(!enabled)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Could not change %s: %v", name, err))
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

// onUpdateFeaturesClicked handles the Update button click
func (uh *UserHome) onUpdateFeaturesClicked(button *gtk.Button) {
	button.SetSensitive(false)
	button.SetLabel("Updating…")

	go func() {
		ctx, cancel := updex.DefaultContext()
		defer cancel()

		err := updex.UpdateFeatures(ctx)

		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			button.SetLabel("Update")

			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Could not update the features: %v", err))
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

	// Optional user tools remain discoverable even without installed access
	// actions; the privileged switch stays insensitive and explains why.
	if dxEnabled {
		uh.buildDeveloperGroup(page, status)
	}
	if gamingEnabled {
		uh.buildGamingGroup(page)
	}
	return true
}

// buildDeveloperGroup builds the developer-tools switch.
func (uh *UserHome) buildDeveloperGroup(page *adw.PreferencesPage, status ublue.Status) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Developer")
	group.SetDescription("Tools for building and running software on this computer.")

	presentation := pageview.DeveloperRow(status.Developer)

	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)
	uh.developerSpinner = newActivitySpinner()
	row.AddSuffix(&uh.developerSpinner.Widget)

	dxRow := row
	var toggle *guardedSwitch
	toggle = newGuardedSwitch(status.Developer, func(state bool) {
		uh.onDeveloperToggled(state, toggle, dxRow)
	})
	uh.developerCanToggle = status.Supports(ubluehelper.CommandDXEnable, ubluehelper.CommandDXDisable)
	if !uh.developerCanToggle {
		toggle.widget.SetSensitive(false)
		row.SetSubtitle("Needs the installed Developer Mode actions in the system helper. Optional tools can still be selected below.")
	}

	row.AddSuffix(&toggle.widget.Widget)
	row.SetActivatableWidget(&toggle.widget.Widget)
	group.Add(&row.Widget)

	page.Add(group)
	uh.developerGroup = group
	uh.developerRow = row
	uh.developerSwitch = toggle.widget
	uh.buildDeveloperOptions(group, status)
}

// gamingComponentRow is built once; refresh changes only its observed subtitle.
type gamingComponentRow struct {
	component gaming.Component
	row       *adw.ActionRow
	choice    *gtk.CheckButton
}

func (uh *UserHome) buildGamingGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Gaming")
	group.SetDescription("Choose the apps to install for your account. System-managed copies are left alone.")
	row := adw.NewActionRow()
	row.SetTitle(pageview.GamingRow(false, 0, 0).Title)
	row.SetSubtitle(pageview.GamingCheckingSubtitle)
	uh.gamingSpinner = newActivitySpinner()
	row.AddSuffix(&uh.gamingSpinner.Widget)
	uh.gamingInstall = gtk.NewButtonWithLabel("Install Selected")
	uh.gamingRemove = gtk.NewButtonWithLabel("Remove Selected")
	for _, button := range []*gtk.Button{uh.gamingInstall, uh.gamingRemove} {
		button.SetValign(gtk.AlignCenterValue)
		button.SetSensitive(false)
		row.AddSuffix(&button.Widget)
	}
	uh.gamingButtons.connect(uh.gamingInstall, func(gtk.Button) { uh.onGamingSelected(true) })
	uh.gamingButtons.connect(uh.gamingRemove, func(gtk.Button) { uh.confirmGamingRemoval() })
	group.Add(&row.Widget)
	for _, component := range gaming.Components() {
		item := &gamingComponentRow{component: component, row: adw.NewActionRow(), choice: gtk.NewCheckButton()}
		item.row.SetTitle(component.Name)
		item.row.SetSubtitle(component.Description + " — checking installation…")
		item.choice.SetValign(gtk.AlignCenterValue)
		item.choice.SetSensitive(false)
		SetAccessibleLabel(item.choice, "Select "+component.Name)
		item.row.AddPrefix(&item.choice.Widget)
		item.row.SetActivatableWidget(&item.choice.Widget)
		group.Add(&item.row.Widget)
		uh.gamingComponents = append(uh.gamingComponents, item)
	}
	page.Add(group)
	uh.gamingGroup, uh.gamingRow = group, row
	go func() {
		state, err := gaming.Status()
		sgtk.RunOnMainThread(func() {
			if err != nil {
				log.Printf("views: gaming status unavailable: %v", err)
				row.SetSubtitle(pageview.GamingUnavailableSubtitle)
				for _, item := range uh.gamingComponents {
					item.row.SetSubtitle(item.component.Description + " — installed state unavailable")
				}
				return
			}
			uh.applyGamingState(state)
			row.SetSubtitle(pageview.GamingRow(state.Enabled, len(state.Installed), gaming.ComponentCount()).Subtitle)
			uh.setGamingSensitive(true)
		})
	}()
}

func (uh *UserHome) applyGamingState(state gaming.State) {
	for _, item := range uh.gamingComponents {
		status := "Not installed"
		if slices.Contains(state.UserInstalled, item.component.ID) {
			status = "Installed for your account"
		} else if slices.Contains(state.SystemOnly, item.component.ID) {
			status = "Installed system-wide; left in place"
		}
		item.row.SetSubtitle(item.component.Description + " — " + status)
	}
}

func (uh *UserHome) setGamingSensitive(sensitive bool) {
	uh.gamingInstall.SetSensitive(sensitive)
	uh.gamingRemove.SetSensitive(sensitive)
	for _, item := range uh.gamingComponents {
		item.choice.SetSensitive(sensitive)
	}
}

func (uh *UserHome) gamingSelection() []string {
	var selected []string
	for _, item := range uh.gamingComponents {
		if item.choice.GetActive() {
			selected = append(selected, item.component.ID)
		}
	}
	return selected
}

func (uh *UserHome) confirmGamingRemoval() {
	selected := uh.gamingSelection()
	if len(selected) == 0 {
		uh.toastAdder.ShowToast("Select the gaming apps to remove first.")
		return
	}
	if !uh.gamingGate.TryStart() {
		return
	}
	uh.setGamingSensitive(false)
	dialog := adw.NewAlertDialog("Remove selected gaming apps?", "Only the selected apps installed for your account will be removed. System-managed copies and game data are kept.")
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("remove", "Remove")
	dialog.SetResponseAppearance("remove", adw.ResponseDestructiveValue)
	dialog.SetDefaultResponse("cancel")
	dialog.SetCloseResponse("cancel")
	uh.gamingDialogs.connect(dialog, func(response string) {
		if response == "remove" {
			uh.runGamingSelected(false, selected)
		} else {
			uh.gamingGate.Reset()
			uh.setGamingSensitive(true)
		}
	})
	dialog.Present(&uh.featuresPage.Widget)
}

// onDeveloperToggled grants or withdraws this account's developer access.
func (uh *UserHome) onDeveloperToggled(enabled bool, toggle *guardedSwitch, row *adw.ActionRow) {
	if !uh.developerCanToggle || !uh.developerGate.TryStart() {
		return
	}
	uh.setDeveloperSensitive(false)
	setActivitySpinner(uh.developerSpinner, true)

	go func() {
		dispatched := false
		defer func() {
			if !dispatched {
				uh.developerGate.Reset()
			}
		}()

		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.SetDeveloperMode(ctx, enabled)
		succeeded := err == nil

		var menuErr error
		if succeeded {
			menuErr = devmenu.Apply(ctx, enabled)
			if menuErr != nil {
				log.Printf("views: updating custom command menu: %v", menuErr)
			}
		}

		dispatched = true
		sgtk.RunOnMainThread(func() {
			defer uh.developerGate.Reset()
			setActivitySpinner(uh.developerSpinner, false)
			uh.setDeveloperSensitive(true)

			if err != nil {
				toggle.set(!enabled)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Could not change developer tools: %v", err))
				return
			}
			if menuErr != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Custom Command Menu update failed: %v", menuErr))
			}

			decision := actionmsg.DeveloperMode(dryrun.Enabled(), enabled)
			toggle.set(decision.Confirm == enabled)
			if decision.Confirm {
				row.SetSubtitle(pageview.DeveloperResultSubtitle(enabled))
			}
			uh.toastAdder.ShowToast(decision.Toast)

			uh.openDeveloperOnboarding(enabled, succeeded)
			uh.startDeveloperFeedSetup(enabled, succeeded)
		})
	}()
}

// openDeveloperOnboarding opens the developer onboarding tabs for a confirmed
// live enable, and runs on the GTK main thread from the one branch of
// onDeveloperToggled that reached a successful promotion.
func (uh *UserHome) openDeveloperOnboarding(enabled, succeeded bool) {
	for _, link := range pageview.DeveloperOnboardingTargets(dryrun.Enabled(), enabled, succeeded) {
		uh.openURL(link.URL)
	}
}

// startDeveloperFeedSetup runs the optional developer feed work for a
// confirmed live enable: install the Pulp reader and stage the curated OPML
// catalog, each only when `dx_group` asks for it. Like openDeveloperOnboarding
// it is called from the one branch of onDeveloperToggled that reached a
// successful live promotion, so a page restore, a failed helper call, a
// disable, and a preview all reach the empty plan and spawn nothing.
//
// The optional steps are deliberately not part of the enable's own outcome.
// Developer access is granted by the privileged helper; an install that then
// fails is reported as its own failure and rolls nothing back, because
// withdrawing the groups the user asked for over an unrelated Flatpak would
// be a second, unrequested change.
//
// Everything the worker reads is captured here, on the main thread, before it
// starts: the plan is a value, and no widget is touched off the main thread.
// The one widget-adjacent call is the toast, which is marshalled back through
// sgtk.RunOnMainThread and nil-guarded — this window's group may have been
// rebuilt or dismissed while a Flatpak install was running.
func (uh *UserHome) startDeveloperFeedSetup(enabled, succeeded bool) {
	var installPulp, stageFeeds bool
	if group := uh.config.GetGroupConfig("features_page", "dx_group"); group != nil {
		installPulp, stageFeeds = group.InstallPulp, group.StageFeeds
	}

	setup := actionmsg.DeveloperFeedSetupPlan(dryrun.Enabled(), enabled, succeeded, installPulp, stageFeeds)
	if !setup.Any() {
		return
	}

	// Overlapping setup runs are refused rather than queued: two concurrent
	// `flatpak install` calls for the same application are one too many, and
	// the work left by the run already in flight is the same work.
	if !uh.developerFeedGate.TryStart() {
		return
	}

	go func() {
		var outcome actionmsg.DeveloperFeedOutcome

		if setup.InstallPulp {
			if err := developerfeeds.Provision(); err != nil {
				log.Printf("views: developer feed setup could not provision %s: %v", developerfeeds.PulpID, err)
			} else {
				outcome.PulpReady = true
			}
		}

		if setup.StageFeeds {
			if err := developerfeeds.StageOPML(); err != nil {
				log.Printf("views: developer feed setup could not stage the feed catalog: %v", err)
			} else {
				outcome.FeedsStaged = true
				// The path is only for the banner. A home directory that
				// cannot be resolved after a successful write costs the
				// message its exact location, not the feedback.
				if path, err := developerfeeds.OPMLPath(); err == nil {
					outcome.StagedPath = path
				}
			}
		}

		sgtk.RunOnMainThread(func() {
			defer uh.developerFeedGate.Reset()

			if uh.toastAdder == nil {
				return
			}
			result := actionmsg.DeveloperFeedFeedback(setup, outcome)
			if result.Failed {
				uh.toastAdder.ShowErrorToast(result.Message)
				return
			}
			uh.toastAdder.ShowToast(result.Message)
		})
	}()
}

func (uh *UserHome) onGamingSelected(enabled bool) {
	selected := uh.gamingSelection()
	if len(selected) == 0 {
		uh.toastAdder.ShowToast("Select the gaming apps to install first.")
		return
	}
	if !uh.gamingGate.TryStart() {
		return
	}
	uh.setGamingSensitive(false)
	uh.runGamingSelected(enabled, selected)
}

// A single gate owns the selection, both actions and the finishing refresh.
// The summary is retained separately from per-component observed state.
func (uh *UserHome) runGamingSelected(enabled bool, selected []string) {
	row := uh.gamingRow
	before := row.GetSubtitle()
	row.SetSubtitle(pageview.GamingWorkingSubtitle(enabled))
	setActivitySpinner(uh.gamingSpinner, true)
	go func() {
		var changed, skipped []string
		var failures []error
		if enabled {
			changed, failures = gaming.Enable(selected)
		} else {
			changed, skipped, failures = gaming.Disable(selected)
		}
		for _, failure := range failures {
			log.Printf("views: gaming component failed: %v", failure)
		}
		state, refreshErr := gaming.Status()
		if refreshErr != nil {
			log.Printf("views: gaming refresh failed: %v", refreshErr)
		}
		sgtk.RunOnMainThread(func() {
			defer uh.gamingGate.Reset()
			setActivitySpinner(uh.gamingSpinner, false)
			uh.setGamingSensitive(true)
			decision := actionmsg.GamingMode(dryrun.Enabled(), enabled, len(changed), len(failures), len(skipped))
			if dryrun.Enabled() {
				row.SetSubtitle(before)
			} else {
				result := pageview.GamingResultSubtitle(enabled, len(changed), len(failures))
				if len(skipped) > 0 {
					result += fmt.Sprintf(" %d system-managed app(s) left in place.", len(skipped))
				}
				if len(failures) > 0 {
					result += " Details are in the application log."
				}
				if refreshErr != nil {
					result += " Installed state could not be refreshed; previous observations are kept."
				}
				row.SetSubtitle(result)
			}
			if refreshErr == nil {
				uh.applyGamingState(state)
			}
			if !dryrun.Enabled() && len(changed) > 0 {
				go uh.loadFlatpakApplications()
			}
			if len(failures) > 0 {
				uh.toastAdder.ShowErrorToast(decision.Toast)
			} else {
				uh.toastAdder.ShowToast(decision.Toast)
			}
		})
	}()
}
