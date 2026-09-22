package views

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/autoupdate"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/ublue"
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

// Automatic updates used to be one row inside a larger "Update All" group
// that also carried a hero button, a row per provider phase, and an
// on-demand full-update row driving the host's integrated updater. All of
// that duplicated the status-first shell above it (internal/updateflow and
// UpdateShell), which is now the single answer to "update this machine".
//
// What survives is the one thing the shell does not express: whether this
// system updates itself on a schedule. That is a preference about the
// future, not an action taken now, so it belongs in its own group rather
// than beside a button.

// buildAutomaticUpdatesGroup builds the automatic-background-updates group.
// The group is built hidden and revealed only once the systemd probe has
// answered: the two systemctl queries can each approach a multi-second
// timeout on a slow or wedged host, and neither may stall the first window
// (chairlift#79).
//
// The group's config guard is applied by its caller in updates_page.go,
// because internal/config and internal/navigation both scan that one file
// for each page's IsGroupEnabled call sites.
func (uh *UserHome) buildAutomaticUpdatesGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetVisible(false)

	page.Add(group)

	go uh.loadAutomaticUpdatesGroup(group)
}

// loadAutomaticUpdatesGroup probes the unattended-update timer and, when the
// host has one, populates the group on the GTK main thread. A host with no
// timer never shows a switch that would do nothing.
func (uh *UserHome) loadAutomaticUpdatesGroup(group *adw.PreferencesGroup) {
	ctx, cancel := context.WithTimeout(context.Background(), autoUpdateProbeTimeout)
	defer cancel()
	state := autoupdate.Detect(ctx)

	if !state.Available() {
		log.Printf("views: automatic updates unavailable (%s not installed)", autoupdate.TimerUnit)
		return
	}

	sgtk.RunOnMainThread(func() {
		uh.buildAutomaticUpdatesRow(group, state)
		group.SetVisible(true)
	})
}

// buildAutomaticUpdatesRow adds the switch itself. Main thread only, after
// the probe has answered.
//
// The handler is created once, here, and lives as long as the row: puregotk
// caches every connected callback by the address of its func variable and
// never releases the slot, so connecting inside a path that reruns is a slow
// leak toward its fixed table limit.
func (uh *UserHome) buildAutomaticUpdatesRow(group *adw.PreferencesGroup, state autoupdate.State) {
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
