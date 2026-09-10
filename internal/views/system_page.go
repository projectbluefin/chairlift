package views

import (
	"fmt"
	"log"
	"os"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// buildSystemPage builds the System page content
func (uh *UserHome) buildSystemPage() {
	page := uh.systemPrefsPage
	if page == nil {
		return
	}

	// System Information group
	if uh.config.IsGroupEnabled("system_page", "system_info_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("System Information")
		group.SetDescription("View system details and hardware information")

		// OS Release expander
		osExpander := adw.NewExpanderRow()
		osExpander.SetTitle("Operating System Details")

		uh.loadOSRelease(osExpander)
		group.Add(&osExpander.Widget)
		page.Add(group)
	}

	// bootc Status group - built hidden, shown asynchronously if this host
	// is booted from a bootc deployment (bootc status requires an exec, so
	// the gate must not run synchronously during page construction).
	if uh.config.IsGroupEnabled("system_page", "bootc_status_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("System Image")
		group.SetDescription("bootc deployment status")
		group.SetVisible(false)

		bootcExpander := adw.NewExpanderRow()
		bootcExpander.SetTitle("Deployment Details")
		bootcExpander.SetSubtitle("Loading...")

		group.Add(&bootcExpander.Widget)
		page.Add(group)

		// Gate + load asynchronously
		go uh.loadBootcStatus(group, bootcExpander)
	}

	// System Health group
	// Which image this machine runs. Hidden entirely on a host with no
	// ublue-os image descriptor, like every other Bluefin-family group.
	if uh.config.IsGroupEnabled("system_page", "channel_group") {
		uh.buildImageIdentityGroup(page)
	}

	if uh.config.IsGroupEnabled("system_page", "health_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("System Health")
		group.SetDescription("Overview of system health and diagnostics")

		perfRow := adw.NewActionRow()
		perfRow.SetTitle("System Performance")
		perfRow.SetSubtitle("Monitor CPU, memory, and system resources")
		perfRow.SetActivatable(true)

		icon := gtk.NewImageFromIconName("adw-external-link-symbolic")
		perfRow.AddSuffix(&icon.Widget)

		groupCfg := uh.config.GetGroupConfig("system_page", "health_group")
		appID := "io.missioncenter.MissionCenter"
		if groupCfg != nil && groupCfg.AppID != "" {
			appID = groupCfg.AppID
		}

		activatedCb := func(row adw.ActionRow) {
			uh.launchApp(appID)
		}
		perfRow.ConnectActivated(&activatedCb)

		group.Add(&perfRow.Widget)
		page.Add(group)
	}
}

// loadOSRelease loads /etc/os-release into the expander
func (uh *UserHome) loadOSRelease(expander *adw.ExpanderRow) {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		row := adw.NewActionRow()
		row.SetTitle("OS Information")
		row.SetSubtitle("Not available")
		expander.AddRow(&row.Widget)
		return
	}
	defer func() { _ = file.Close() }()

	entries, parseErr := pageview.ParseOSRelease(file)
	for _, entry := range entries {
		row := adw.NewActionRow()
		row.SetTitle(entry.Title)
		row.SetSubtitle(entry.Value)

		if entry.IsURL {
			row.SetActivatable(true)
			icon := gtk.NewImageFromIconName("adw-external-link-symbolic")
			row.AddSuffix(&icon.Widget)

			url := entry.Value
			activatedCb := func(row adw.ActionRow) {
				uh.openURL(url)
			}
			row.ConnectActivated(&activatedCb)
		}

		expander.AddRow(&row.Widget)
	}
	if parseErr != nil {
		row := adw.NewActionRow()
		row.SetTitle("OS Information")
		row.SetSubtitle("Not available")
		expander.AddRow(&row.Widget)
	}
}

// loadBootcStatus checks the bootc boot gate and populates the status
// expander. Runs in a goroutine; shows the group only on bootc hosts.
func (uh *UserHome) loadBootcStatus(group *adw.PreferencesGroup, expander *adw.ExpanderRow) {
	if !bootc.IsBootcBootedCached() {
		return // group stays hidden on non-bootc hosts
	}

	ctx, cancel := bootc.DefaultContext()
	defer cancel()

	status, err := bootc.GetStatus(ctx)

	sgtk.RunOnMainThread(func() {
		group.SetVisible(true)

		if err != nil {
			expander.SetSubtitle(fmt.Sprintf("Error: %v", err))
			return
		}

		expander.SetSubtitle("Loaded")

		addRow := func(title, subtitle string) {
			row := adw.NewActionRow()
			row.SetTitle(title)
			row.SetSubtitle(subtitle)
			expander.AddRow(&row.Widget)
		}

		booted := status.Status.Booted
		if booted.ImageRef() != "" {
			addRow("Image", booted.ImageRef())
		}
		if booted.Version() != "" {
			addRow("Version", booted.Version())
		}
		if booted.Timestamp() != "" {
			addRow("Built", booted.Timestamp())
		}
		if digest := pageview.ShortDigest(booted.Digest()); digest != "" {
			addRow("Digest", digest)
		}

		if staged := status.Status.Staged; staged != nil {
			subtitle := "Restart to apply"
			if staged.Version() != "" {
				subtitle = fmt.Sprintf("%s — restart to apply", staged.Version())
			}
			addRow("Staged Update", subtitle)
		}

		if rollback := status.Status.Rollback; rollback != nil {
			subtitle := rollback.Version()
			if subtitle == "" {
				subtitle = "Available"
			}
			addRow("Rollback", subtitle)
		}
	})
}

// buildImageIdentityGroup builds the release-channel and graphics-driver
// rows: the two controls that decide which image boots. Both stage a `bootc
// switch` — one changes the tag, the other the image name — so they share a
// group, and that group sits on the System page beside the rest of this
// machine's identity rather than among the capabilities you switch on.
func (uh *UserHome) buildImageIdentityGroup(page *adw.PreferencesPage) {
	status := ublue.StatusCached()
	if !status.Available {
		return
	}

	description := pageview.BluefinGroupDescription(status.Variant.DisplayName(), status.Tag)
	uh.buildChannelGroup(page, status, description)
	uh.buildDriverRow(status)

	log.Printf("views: image identity group built variant=%s tag=%s channel=%s switchable=%v driver=%s",
		status.Variant, status.Tag, status.Channel,
		status.CanSwitchTo != imageinfo.ChannelUnknown, status.Driver)
}

// buildChannelGroup builds the release-channel switch. The switch is
// insensitive when the running image publishes no counterpart tag — every
// Bluefin Stable host, for one — because there is no reference to hand
// bootc. See internal/imageinfo's channel table.
func (uh *UserHome) buildChannelGroup(page *adw.PreferencesPage, status ublue.Status, description string) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Release Channel")
	group.SetDescription(description)

	onTesting := status.Channel == imageinfo.ChannelTesting
	switchable := status.CanSwitchTo != imageinfo.ChannelUnknown
	presentation := pageview.ChannelRow(onTesting, switchable, status.Tag)

	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	toggle := gtk.NewSwitch()
	toggle.SetActive(onTesting)
	toggle.SetValign(gtk.AlignCenterValue)
	toggle.SetSensitive(switchable)

	sw := toggle
	channelRow := row
	stateSetCb := func(_ gtk.Switch, state bool) bool {
		uh.onChannelToggled(state, sw, channelRow)
		return true // block the visual change until the switch is confirmed
	}
	toggle.ConnectStateSet(&stateSetCb)

	row.AddSuffix(&toggle.Widget)
	if switchable {
		row.SetActivatableWidget(&toggle.Widget)
	}
	group.Add(&row.Widget)

	page.Add(group)
	uh.channelGroup = group
	uh.channelRow = row
	uh.channelSwitch = toggle
}

// buildDriverRow adds the graphics-driver row to the Release Channel group.
// The row is always informational and only offers an action when the
// hardware wants a different image than the one running and that image is
// actually published for the current stream.
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

// onDriverSwitchClicked stages a switch to the recommended driver image.
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
			uh.driverGate.Complete()
			button.SetSensitive(true)
			button.SetLabel("Switch")

			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Graphics driver switch failed: %v", err))
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

// onChannelToggled stages a release-channel switch.
func (uh *UserHome) onChannelToggled(toTesting bool, toggle *gtk.Switch, row *adw.ActionRow) {
	channel := imageinfo.ChannelStable
	if toTesting {
		channel = imageinfo.ChannelTesting
	}

	toggle.SetSensitive(false)

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.SwitchChannel(ctx, channel)

		sgtk.RunOnMainThread(func() {
			toggle.SetSensitive(true)

			if err != nil {
				toggle.SetActive(!toTesting)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Channel switch failed: %v", err))
				return
			}

			decision := actionmsg.ChannelSwitch(dryrun.Enabled(), toTesting)
			toggle.SetActive(decision.Confirm == toTesting)
			if decision.Confirm {
				row.SetSubtitle(pageview.ChannelSwitchResultSubtitle(toTesting))
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
