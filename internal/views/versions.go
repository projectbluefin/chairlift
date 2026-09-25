package views

import (
	"context"
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/registrytags"
	"github.com/projectbluefin/chairlift/internal/ublue"
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

// buildPublishedVersionsRow adds the Published versions row to the Roll Back
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
// row. The rows carry no signal handlers, so a repeat read rebuilds them
// without growing the process's callback count.
func (uh *UserHome) renderPublishedVersions(versions []pageview.Row) {
	parent := uh.publishedVersionsRow
	if parent == nil {
		return
	}

	for _, row := range uh.publishedVersionRows {
		parent.Remove(&row.Widget)
	}
	uh.publishedVersionRows = nil

	for _, version := range versions {
		row := adw.NewActionRow()
		row.SetTitle(version.Title)
		row.SetSubtitle(version.Subtitle)
		parent.AddRow(&row.Widget)
		uh.publishedVersionRows = append(uh.publishedVersionRows, row)
	}

	parent.SetEnableExpansion(len(versions) > 0)
	parent.SetExpanded(len(versions) > 0)
}
