package views

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
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
	uh.publishedVersionButtons.clear()

	supported := ublue.StatusCached().Supports(ubluehelper.CommandPin)
	status := ublue.StatusCached()
	bootedBuild, isBootedPinned := registrytags.ParseBuild(status.Tag)

	for _, version := range versions {
		row := adw.NewActionRow()
		row.SetTitle(version.Title)
		subtitle := version.Subtitle
		if !supported {
			subtitle += " · " + pageview.PinUnsupportedExplanation()
		}
		row.SetSubtitle(subtitle)

		btn := gtk.NewButtonWithLabel("Pin")
		btn.SetValign(gtk.AlignCenterValue)

		isAlreadyPinned := isBootedPinned && bootedBuild.Date.Format("20060102") == version.Day
		switch {
		case !supported:
			btn.SetSensitive(false)
			btn.SetTooltipText(pageview.PinUnsupportedExplanation())
		case isAlreadyPinned:
			btn.SetLabel("Pinned")
			btn.SetSensitive(false)
		default:
			btn.SetSensitive(true)
			vTitle := version.Title
			vDay := version.Day
			uh.publishedVersionButtons.connect(btn, func(gtk.Button) {
				uh.confirmPin(vTitle, vDay, btn)
			})
		}

		row.AddSuffix(&btn.Widget)
		parent.AddRow(&row.Widget)
		uh.publishedVersionRows = append(uh.publishedVersionRows, row)
	}

	parent.SetEnableExpansion(len(versions) > 0)
	parent.SetExpanded(len(versions) > 0)
}

// confirmPin presents an AdwAlertDialog confirmation before staging a switch to
// the selected dated build.
func (uh *UserHome) confirmPin(date, day string, button *gtk.Button) {
	if !uh.pinGate.TryStart() {
		return
	}

	title, body := pageview.PinConfirmation(date)
	dialog := adw.NewAlertDialog(title, body)
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("confirm", "Pin")
	dialog.SetResponseAppearance("confirm", adw.ResponseSuggestedValue)

	uh.recoveryDialogs.connect(dialog, func(response string) {
		if response != "confirm" {
			uh.pinGate.Reset()
			return
		}
		uh.runPin(day, button)
	})
	dialog.Present(&uh.recoveryPrefsPage.Widget)
}

// runPin stages the dated build via pkexec chairlift-helper pin <day>.
func (uh *UserHome) runPin(day string, button *gtk.Button) {
	button.SetSensitive(false)
	button.SetLabel("Pinning…")

	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.Pin(ctx, day)

		sgtk.RunOnMainThread(func() {
			uh.pinGate.Reset()
			button.SetSensitive(true)
			button.SetLabel("Pin")

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
