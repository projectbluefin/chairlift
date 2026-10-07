package views

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/registrytags"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// publishedVersionsTimeout bounds one catalog read: a paginated tag listing
// of a repository with a few thousand tags.
const publishedVersionsTimeout = time.Minute

// listPublishedBuilds is the seam the Published versions list is built on.
// Its production value reads the registry through one process-wide catalog,
// so a repeat press within the catalog's TTL reuses the last listing; a
// failed read is never cached (ADR-0013).
var listPublishedBuilds = (&registrytags.Catalog{}).Builds

// buildPublishedVersionsRow adds the Published versions row to its preferences
// group: the dated builds the registry still offers for the stream this
// machine follows. It is read-only — nothing it lists reaches a privileged
// path — and it reads the registry only when pressed, never on page load.
//
// The row is omitted on a host with no image descriptor, and on one whose
// running tag names no stream (a digest pin), because there is no stream to
// list.
func (uh *UserHome) buildPublishedVersionsRow(group *adw.PreferencesGroup) {
	status := ublue.StatusCached()
	stream := pageview.CatalogStream(status.Tag)
	if !status.Available || status.Ref == "" || stream == "" {
		return
	}

	row := adw.NewExpanderRow()
	presentation := pageview.PublishedVersionsRow(stream)
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)
	// Nothing to expand until the registry has been read.
	row.SetEnableExpansion(false)

	button := gtk.NewButtonWithLabel("Check")
	button.SetValign(gtk.AlignCenterValue)
	clickedCb := func(_ gtk.Button) {
		uh.onPublishedVersionsClicked()
	}
	button.ConnectClicked(&clickedCb)
	row.AddSuffix(&button.Widget)

	group.Add(&row.Widget)
	uh.publishedVersionsRow = row
	uh.publishedVersionsButton = button
	uh.publishedVersionsRepo = status.Ref
	uh.publishedVersionsStream = stream
}

// onPublishedVersionsClicked reads the catalog and renders one row per
// published day. A failed read keeps whatever list was last shown out of the
// way — it is removed, not left standing as if it were current.
func (uh *UserHome) onPublishedVersionsClicked() {
	if !uh.publishedVersionsGate.TryStart() {
		return
	}

	row := uh.publishedVersionsRow
	button := uh.publishedVersionsButton
	repo, stream := uh.publishedVersionsRepo, uh.publishedVersionsStream

	button.SetSensitive(false)
	button.SetLabel("Checking…")
	row.SetSubtitle("Asking the image registry…")

	since := time.Now().UTC().AddDate(0, 0, -pageview.PublishedVersionsDays)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), publishedVersionsTimeout)
		defer cancel()

		builds, err := listPublishedBuilds(ctx, repo, since)

		sgtk.RunOnMainThread(func() {
			defer uh.publishedVersionsGate.Reset()
			button.SetSensitive(true)
			button.SetLabel("Check Again")

			if err != nil {
				log.Printf("published versions: %s: %v", repo, err)
				uh.renderPublishedVersions(nil)
				row.SetSubtitle(pageview.PublishedVersionsRow(stream).Subtitle)
				uh.toastAdder.ShowErrorToast("Could not read the published versions from the image registry")
				return
			}

			versions := pageview.PublishedVersions(builds, stream, uh.runningVersion, uh.previousVersion)
			row.SetSubtitle(pageview.PublishedVersionsSummary(len(versions), stream))
			uh.renderPublishedVersions(versions)
		})
	}()
}

// renderPublishedVersions replaces the rows under the Published versions
// row. The rows use buttonRoute so repeat reads reuse callback trampolines
// without leaking puregotk callback slots.
func (uh *UserHome) renderPublishedVersions(versions []pageview.PublishedVersion) {
	parent := uh.publishedVersionsRow
	if parent == nil {
		return
	}

	for _, row := range uh.publishedVersionRows {
		parent.Remove(&row.Widget)
	}
	uh.publishedVersionRows = nil
	uh.publishedVersionPins = nil
	uh.publishedVersionButtons.clear()

	status := ublue.StatusCached()
	offered, explanation := pageview.PinOffer(uh.publishedVersionsStream,
		pinSupport(status, ubluehelper.CommandPin, uh.publishedVersionsStream))
	uh.publishedVersionsOffered = offered
	bootedBuild, isBootedPinned := registrytags.ParseBuild(status.Tag)

	for _, version := range versions {
		row := adw.NewActionRow()
		row.SetTitle(version.Title)
		subtitle := version.Subtitle
		if !offered {
			subtitle += " · " + explanation
		}
		row.SetSubtitle(subtitle)

		btn := gtk.NewButtonWithLabel("Pin")
		btn.SetValign(gtk.AlignCenterValue)
		if !offered {
			btn.SetTooltipText(explanation)
		}
		// Every button is routed; its sensitivity, derived below, is what
		// admits a click.
		vTitle := version.Title
		vDay := version.Day
		uh.publishedVersionButtons.connect(btn, func(gtk.Button) {
			uh.confirmPin(vTitle, vDay)
		})
		uh.publishedVersionPins = append(uh.publishedVersionPins, publishedVersionPin{
			button: btn,
			day:    version.Day,
			pinned: isBootedPinned && bootedBuild.Date.Format("20060102") == version.Day,
		})

		row.AddSuffix(&btn.Widget)
		parent.AddRow(&row.Widget)
		uh.publishedVersionRows = append(uh.publishedVersionRows, row)
	}
	uh.applyPinButtons()

	parent.SetEnableExpansion(len(versions) > 0)
	parent.SetExpanded(len(versions) > 0)
}

// publishedVersionPin is one rendered Pin button and what decides it.
type publishedVersionPin struct {
	button *gtk.Button
	day    string
	pinned bool
}

// applyPinButtons derives every listed Pin button's label and sensitivity
// from the recovery switch state, in place, so a re-render (Check Again
// during a minutes-long pin) or another switch starting or finishing never
// leaves a fresh, clickable button beside a pin in flight. Main thread only.
func (uh *UserHome) applyPinButtons() {
	busy := uh.recoveryBusy()
	for _, pin := range uh.publishedVersionPins {
		state := pageview.PinButton(uh.publishedVersionsOffered, pin.pinned, pin.day, uh.pinningDay, busy)
		pin.button.SetLabel(state.Label)
		pin.button.SetSensitive(state.Sensitive)
	}
}

// pinSupport collects, from the cached host status, what PinOffer and
// UnpinOffer need: the helper command, the channel table's health, and
// whether the running stream is one the helper can derive a target for.
func pinSupport(status ublue.Status, command, stream string) pageview.PinSupport {
	return pageview.PinSupport{
		Helper:      status.Supports(command),
		TableBroken: status.ChannelTableError != "",
		KnownStream: imageinfo.KnownStream(status.Ref, stream),
	}
}

// confirmPin presents an AdwAlertDialog confirmation before staging a switch to
// the selected dated build. The shared recovery switch gate is held from here,
// so no other Powerwash switch can start behind the dialog.
func (uh *UserHome) confirmPin(date, day string) {
	if !uh.tryStartRecoverySwitch() {
		return
	}

	title, body := pageview.PinConfirmation(date)
	dialog := adw.NewAlertDialog(title, body)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("confirm", "Pin")
	dialog.SetResponseAppearance("confirm", adw.ResponseSuggestedValue)

	uh.recoveryDialogs.connect(dialog, func(response string) {
		if response != "confirm" {
			uh.recoverySwitchGate.Reset()
			uh.syncRecoverySwitches()
			return
		}
		uh.runPin(day)
	})
	dialog.Present(&uh.recoveryPrefsPage.Widget)
}

// runPin stages the dated build via pkexec chairlift-helper pin <day>.
func (uh *UserHome) runPin(day string) {
	uh.pinningDay = day
	uh.syncRecoverySwitches()

	go func() {
		ctx, cancel := ublue.ImageSwitchContext()
		defer cancel()

		err := ublue.Pin(ctx, day)

		sgtk.RunOnMainThread(func() {
			uh.pinningDay = ""
			uh.recoverySwitchGate.Reset()
			uh.syncRecoverySwitches()

			if err != nil {
				log.Printf("views: pin failed for day %s: %v", day, err)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Pin failed: %v", err))
				return
			}

			decision := actionmsg.PinBuild(dryrun.Enabled(), day)
			if decision.Confirm {
				go uh.loadBootcRollbackStatus()
				if uh.updateShell != nil {
					uh.updateShell.StartCheck()
				}
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
