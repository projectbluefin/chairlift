package views

import (
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/developerfeeds"
	"github.com/projectbluefin/chairlift/internal/devmenu"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/gaming"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/ublue"
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

	uh.buildBluefinGroups(page)

	// Enhanced Troubleshooting was moved to Help (issue #249), and Local AI
	// moved to Agents (issue #244), so neither is built here.

	if uh.groupEnabled("features_page", "features_group") {
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

		go uh.loadFeatures(updateBtn)
	}
}

// loadFeatures loads feature information asynchronously. A host that offers
// no features hides the group rather than rendering an inert one; a failed
// read is not evidence of that, so it keeps the group and says so.
func (uh *UserHome) loadFeatures(updateBtn *gtk.Button) {
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

// buildBluefinGroups builds the developer-tools and gaming groups. The
// release channel and graphics driver live elsewhere: they describe which
// system this machine runs, where these two are capabilities you switch on.
func (uh *UserHome) buildBluefinGroups(page *adw.PreferencesPage) {
	dxEnabled := uh.groupEnabled("features_page", "dx_group")
	gamingEnabled := uh.groupEnabled("features_page", "gaming_group")

	if !dxEnabled && !gamingEnabled {
		return
	}

	// One detection serves both groups. It reads only local files, so
	// it is cheap, but it is still done off the main thread and cached.
	status := ublue.StatusCached()
	if !status.Available {
		return
	}

	// A single structured readiness marker. The screenshot walkthrough
	// asserts on this line to confirm the captured session really built the
	// Bluefin-family rows, rather than capturing a page where they were
	// silently hidden and calling the frame "rendered".
	log.Printf("views: bluefin groups built variant=%s tag=%s channel=%s switchable=%v developer=%v dx_group=%v gaming_group=%v",
		status.Variant, status.Tag, status.Channel,
		status.CanSwitchTo != imageinfo.ChannelUnknown, status.Developer,
		dxEnabled, gamingEnabled)

	if dxEnabled {
		uh.buildDeveloperGroup(page, status)
	}
	if gamingEnabled {
		// A system that already ships the gaming apps gets a readonly
		// note instead of the switch. Gaming installs them for the
		// current account only, which here would shadow the copies the
		// system already provides. See ublue.Status.Gaming.
		if status.Gaming {
			uh.buildGamingIncludedGroup(page)
			log.Printf("views: gaming group suppressed, this system already ships the gaming apps ref=%s", status.Ref)
		} else {
			uh.buildGamingGroup(page)
		}
	}
}

// buildGamingIncludedGroup replaces the gaming switch with a single readonly
// row on systems that ship the gaming apps themselves. The group is still
// rendered so the feature does not simply vanish on the systems most likely
// to be looking for it.
func (uh *UserHome) buildGamingIncludedGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Gaming")
	group.SetDescription("Steam and the tools that go with it are already part of this system.")

	presentation := pageview.GamingIncludedRow()
	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)
	group.Add(&row.Widget)

	page.Add(group)
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

	dxRow := row
	var toggle *guardedSwitch
	toggle = newGuardedSwitch(status.Developer, func(state bool) {
		uh.onDeveloperToggled(state, toggle, dxRow)
	})

	row.AddSuffix(&toggle.widget.Widget)
	row.SetActivatableWidget(&toggle.widget.Widget)
	group.Add(&row.Widget)

	page.Add(group)
	uh.developerGroup = group
	uh.developerRow = row
	uh.developerSwitch = toggle.widget
}

// buildGamingGroup builds the gaming switch. What is installed is unknown
// until the query returns, so the switch starts insensitive and is populated
// asynchronously — through guardedSwitch, so showing the machine's real state
// cannot be mistaken for the user asking for it.
func (uh *UserHome) buildGamingGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Gaming")
	group.SetDescription("Steam and the tools that go with it, installed for your account only.")

	row := adw.NewActionRow()
	row.SetTitle(pageview.GamingRow(false, 0, 0).Title)
	row.SetSubtitle(pageview.GamingCheckingSubtitle)

	gamingRow := row
	var toggle *guardedSwitch
	toggle = newGuardedSwitch(false, func(state bool) {
		uh.onGamingToggled(state, toggle, gamingRow)
	})
	toggle.widget.SetSensitive(false)

	row.AddSuffix(&toggle.widget.Widget)
	row.SetActivatableWidget(&toggle.widget.Widget)
	group.Add(&row.Widget)

	page.Add(group)
	uh.gamingGroup = group
	uh.gamingRow = row
	uh.gamingSwitch = toggle.widget

	go uh.refreshGamingState(toggle, row)
}

// refreshGamingState queries the installed applications off the main thread
// and applies the result to the gaming row.
func (uh *UserHome) refreshGamingState(toggle *guardedSwitch, row *adw.ActionRow) {
	state, err := gaming.Status()
	total := gaming.ComponentCount()

	sgtk.RunOnMainThread(func() {
		if toggle == nil || row == nil {
			return
		}
		if err != nil {
			log.Printf("views: gaming status unavailable: %v", err)
			row.SetSubtitle(pageview.GamingUnavailableSubtitle)
			return
		}
		toggle.widget.SetSensitive(true)
		toggle.set(state.Enabled)
		row.SetSubtitle(pageview.GamingRow(state.Enabled, len(state.Installed), total).Subtitle)
	})
}

// onDeveloperToggled grants or withdraws this account's developer access.
func (uh *UserHome) onDeveloperToggled(enabled bool, toggle *guardedSwitch, row *adw.ActionRow) {
	if !uh.developerGate.TryStart() {
		return
	}
	toggle.widget.SetSensitive(false)

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
			toggle.widget.SetSensitive(true)

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

// onGamingToggled installs or removes the gaming applications. Unlike the
// developer switch this one can partly succeed, so the decision to confirm
// the switch comes from actionmsg.GamingMode rather than from the absence of
// an error.
func (uh *UserHome) onGamingToggled(enabled bool, toggle *guardedSwitch, row *adw.ActionRow) {
	toggle.widget.SetSensitive(false)
	row.SetSubtitle(pageview.GamingWorkingSubtitle(enabled))

	go func() {
		var changed, skipped []string
		var failures []error
		if enabled {
			changed, failures = gaming.Enable()
		} else {
			changed, skipped, failures = gaming.Disable()
		}

		sgtk.RunOnMainThread(func() {
			toggle.widget.SetSensitive(true)

			decision := actionmsg.GamingMode(dryrun.Enabled(), enabled, len(changed), len(failures), len(skipped))
			toggle.set(decision.Confirm == enabled)
			row.SetSubtitle(pageview.GamingResultSubtitle(enabled, len(changed), len(failures)))

			if len(failures) > 0 {
				uh.toastAdder.ShowErrorToast(decision.Toast)
			} else {
				uh.toastAdder.ShowToast(decision.Toast)
			}

			go uh.refreshGamingState(toggle, row)
		})
	}()
}
