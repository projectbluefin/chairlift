package views

import (
	"fmt"
	"log"

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

// buildFeaturesPage builds the Features page content
func (uh *UserHome) buildFeaturesPage() {
	page := uh.featuresPrefsPage
	if page == nil {
		return
	}

	uh.buildBluefinGroups(page)

	if uh.config.IsGroupEnabled("features_page", "ai_group") {
		uh.buildAIStackGroup(page)
	}

	if uh.config.IsGroupEnabled("features_page", "troubleshooting_group") {
		uh.buildTroubleshootGroup(page)
	}

	if uh.config.IsGroupEnabled("features_page", "features_group") {
		// Build the features group (shown if updex is available)
		uh.featuresGroup = adw.NewPreferencesGroup()
		uh.featuresGroup.SetTitle("System Features")
		uh.featuresGroup.SetDescription("Checking feature availability...")

		// Add Update button as header suffix (disabled until availability confirmed)
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

		// Build the "not available" group (hidden by default)
		uh.featuresUnavailableGroup = adw.NewPreferencesGroup()
		uh.featuresUnavailableGroup.SetTitle("System Features")
		uh.featuresUnavailableGroup.SetDescription("Manage system features")
		uh.featuresUnavailableGroup.SetVisible(false)

		unavailRow := adw.NewActionRow()
		unavailRow.SetTitle("Feature Manager Not Available")
		unavailRow.SetSubtitle("System features are not configured on this system")
		uh.featuresUnavailableGroup.Add(&unavailRow.Widget)
		page.Add(uh.featuresUnavailableGroup)

		// Check availability and load features asynchronously
		go uh.checkAndLoadFeatures(updateBtn)
	}
}

// checkAndLoadFeatures checks updex availability then loads features
func (uh *UserHome) checkAndLoadFeatures(updateBtn *gtk.Button) {
	if !updex.IsInstalledCached() {
		sgtk.RunOnMainThread(func() {
			if uh.featuresGroup != nil {
				uh.featuresGroup.SetVisible(false)
			}
			if uh.featuresUnavailableGroup != nil {
				uh.featuresUnavailableGroup.SetVisible(true)
			}
		})
		return
	}

	sgtk.RunOnMainThread(func() {
		updateBtn.SetSensitive(true)
	})

	uh.loadFeatures()
}

// loadFeatures loads feature information asynchronously
func (uh *UserHome) loadFeatures() {
	ctx, cancel := updex.DefaultContext()
	defer cancel()

	features, err := updex.ListFeatures(ctx)

	sgtk.RunOnMainThread(func() {
		if uh.featuresGroup == nil {
			return
		}

		if err != nil {
			uh.featuresGroup.SetDescription(fmt.Sprintf("Error: %v", err))
			return
		}

		if len(features) == 0 {
			uh.featuresGroup.SetDescription("No features available")
			return
		}

		uh.featuresGroup.SetDescription(pageview.FeatureGroupDescription(len(features)))
		uh.featureRows = make(map[string]*adw.ActionRow)

		for _, feat := range features {
			presentation := pageview.Feature(feat.Name, feat.Description)
			row := adw.NewActionRow()
			row.SetTitle(presentation.Title)
			row.SetSubtitle(presentation.Subtitle)

			toggle := gtk.NewSwitch()
			toggle.SetActive(feat.Enabled)
			toggle.SetValign(gtk.AlignCenterValue)

			featName := feat.Name
			sw := toggle
			stateSetCb := func(_ gtk.Switch, state bool) bool {
				uh.onFeatureToggled(featName, state, sw)
				return true // block visual change until confirmed
			}
			toggle.ConnectStateSet(&stateSetCb)

			row.AddSuffix(&toggle.Widget)
			row.SetActivatableWidget(&toggle.Widget)
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

	checks, err := updex.CheckFeatures(ctx)

	sgtk.RunOnMainThread(func() {
		if err != nil {
			log.Printf("Feature update check failed: %v", err)
			if uh.featuresGroup != nil {
				uh.featuresGroup.SetDescription(featurestatus.GroupDescriptionCheckFailed(totalFeatures))
			}
			return
		}

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
		}

		if uh.featuresGroup != nil {
			uh.featuresGroup.SetDescription(featurestatus.GroupDescription(totalFeatures, updateCount))
		}
	})
}

// onFeatureToggled handles enabling/disabling a feature
func (uh *UserHome) onFeatureToggled(name string, enabled bool, toggle *gtk.Switch) {
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
				toggle.SetActive(!enabled)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Failed to update %s: %v", name, err))
				return
			}

			decision := actionmsg.FeatureToggle(updex.IsDryRun(), enabled, name)
			if decision.Confirm {
				// Confirm the visual state change
				toggle.SetActive(enabled)
			} else {
				// Nothing actually changed under dry-run; revert to the
				// pre-click state.
				toggle.SetActive(!enabled)
			}

			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

// onUpdateFeaturesClicked handles the Update button click
func (uh *UserHome) onUpdateFeaturesClicked(button *gtk.Button) {
	button.SetSensitive(false)
	button.SetLabel("Updating...")

	go func() {
		ctx, cancel := updex.DefaultContext()
		defer cancel()

		err := updex.UpdateFeatures(ctx)

		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			button.SetLabel("Update")

			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Update failed: %v", err))
				return
			}

			uh.toastAdder.ShowToast(actionmsg.FeatureUpdate(updex.IsDryRun()))
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

// buildBluefinGroups builds the developer-mode and gaming-mode groups. The
// release channel and graphics driver live on the System page instead: they
// describe which image this machine runs, where these two are capabilities
// you switch on.
func (uh *UserHome) buildBluefinGroups(page *adw.PreferencesPage) {
	dxEnabled := uh.config.IsGroupEnabled("features_page", "dx_group")
	gamingEnabled := uh.config.IsGroupEnabled("features_page", "gaming_group")

	if !dxEnabled && !gamingEnabled {
		return
	}

	// One detection serves all three groups. It reads only local files, so
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
		uh.buildGamingGroup(page)
	}
}

// buildDeveloperGroup builds the developer-mode switch.
func (uh *UserHome) buildDeveloperGroup(page *adw.PreferencesPage, status ublue.Status) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Developer Mode")
	// Bluefin also publishes separate -dx images, and a user who has seen
	// those will expect this switch to rebase them. Say plainly that it does
	// not: this is group membership, which is the only form of developer
	// mode available on all three supported images (Dakota publishes no -dx
	// variant at all).
	group.SetDescription("Adds this account to the container, VM, and serial-device groups — it does not rebase to a -dx image")

	presentation := pageview.DeveloperRow(status.Developer, status.DevGroups)

	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	toggle := gtk.NewSwitch()
	toggle.SetActive(status.Developer)
	toggle.SetValign(gtk.AlignCenterValue)

	sw := toggle
	dxRow := row
	stateSetCb := func(_ gtk.Switch, state bool) bool {
		uh.onDeveloperToggled(state, sw, dxRow)
		return true
	}
	toggle.ConnectStateSet(&stateSetCb)

	row.AddSuffix(&toggle.Widget)
	row.SetActivatableWidget(&toggle.Widget)
	group.Add(&row.Widget)

	page.Add(group)
	uh.developerGroup = group
	uh.developerRow = row
	uh.developerSwitch = toggle
}

// buildGamingGroup builds the gaming-mode switch. Its state is unknown until
// the Flatpak query returns, so the switch starts insensitive and is
// populated asynchronously.
func (uh *UserHome) buildGamingGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Gaming")
	group.SetDescription("Installs the gaming stack as user Flatpaks — nothing is layered onto the system image")

	row := adw.NewActionRow()
	presentation := pageview.GamingRow("Checking installed components...")
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	toggle := gtk.NewSwitch()
	toggle.SetValign(gtk.AlignCenterValue)
	toggle.SetSensitive(false)

	sw := toggle
	gamingRow := row
	stateSetCb := func(_ gtk.Switch, state bool) bool {
		uh.onGamingToggled(state, sw, gamingRow)
		return true
	}
	toggle.ConnectStateSet(&stateSetCb)

	row.AddSuffix(&toggle.Widget)
	row.SetActivatableWidget(&toggle.Widget)
	group.Add(&row.Widget)

	page.Add(group)
	uh.gamingGroup = group
	uh.gamingRow = row
	uh.gamingSwitch = toggle

	go uh.refreshGamingState()
}

// refreshGamingState queries installed Flatpaks off the main thread and
// applies the result to the gaming row.
func (uh *UserHome) refreshGamingState() {
	state, err := gaming.Status()

	sgtk.RunOnMainThread(func() {
		if uh.gamingRow == nil || uh.gamingSwitch == nil {
			return
		}
		if err != nil {
			uh.gamingRow.SetSubtitle(fmt.Sprintf("Unavailable: %v", err))
			return
		}
		uh.gamingSwitch.SetSensitive(true)
		uh.gamingSwitch.SetActive(state.Enabled)
		uh.gamingRow.SetSubtitle(pageview.GamingRow(state.Summary()).Subtitle)
	})
}

// onDeveloperToggled adds or removes this account's developer groups.
func (uh *UserHome) onDeveloperToggled(enabled bool, toggle *gtk.Switch, row *adw.ActionRow) {
	toggle.SetSensitive(false)

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.SetDeveloperMode(ctx, enabled)

		sgtk.RunOnMainThread(func() {
			toggle.SetSensitive(true)

			if err != nil {
				toggle.SetActive(!enabled)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Developer mode failed: %v", err))
				return
			}

			decision := actionmsg.DeveloperMode(ublue.IsDryRun(), enabled)
			toggle.SetActive(decision.Confirm == enabled)
			if decision.Confirm {
				row.SetSubtitle(pageview.DeveloperResultSubtitle(enabled))
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}

// onGamingToggled installs or removes the gaming stack. Unlike the other two
// toggles this one can partly succeed, so the decision to confirm the switch
// comes from actionmsg.GamingMode rather than from the absence of an error.
func (uh *UserHome) onGamingToggled(enabled bool, toggle *gtk.Switch, row *adw.ActionRow) {
	toggle.SetSensitive(false)
	row.SetSubtitle("Working...")

	go func() {
		var changed, skipped []string
		var failures []error
		if enabled {
			changed, failures = gaming.Enable()
		} else {
			changed, skipped, failures = gaming.Disable()
		}

		sgtk.RunOnMainThread(func() {
			toggle.SetSensitive(true)

			decision := actionmsg.GamingMode(ublue.IsDryRun(), enabled, len(changed), len(failures), len(skipped))
			toggle.SetActive(decision.Confirm == enabled)
			row.SetSubtitle(pageview.GamingResultSubtitle(enabled, len(changed), len(failures)))

			if len(failures) > 0 {
				uh.toastAdder.ShowErrorToast(decision.Toast)
			} else {
				uh.toastAdder.ShowToast(decision.Toast)
			}

			go uh.refreshGamingState()
		})
	}()
}
