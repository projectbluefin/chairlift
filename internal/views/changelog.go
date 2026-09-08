package views

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/sbom"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// changelogTimeout bounds the two registry round-trips. The Bluefin SBOM is
// tens of megabytes per side, so this is generous compared with the rest of
// the app's timeouts.
const changelogTimeout = 3 * time.Minute

// fetchSBOM is the seam the changelog is built on. Its production value goes
// to the registry; the pure diff it feeds is covered by fixtures.
var fetchSBOM sbom.FetchFunc = (&sbom.RegistryClient{}).Fetch

// buildChangelogRow adds the "What's Changing" drill-down inside the
// staged-update expander. It is a row rather than a page: the diff only
// means anything relative to a specific staged update, and a top-level page
// would have to invent an answer for a system with nothing staged.
func (uh *UserHome) buildChangelogRow(expander *adw.ExpanderRow) {
	row := adw.NewActionRow()
	presentation := pageview.ChangelogRow(false)
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	button := gtk.NewButtonWithLabel("Compare")
	button.SetValign(gtk.AlignCenterValue)
	// Nothing to compare until a staged deployment exists, and the fetch is
	// large enough that it must never happen on its own.
	button.SetSensitive(false)
	clickedCb := func(_ gtk.Button) {
		uh.onChangelogClicked()
	}
	button.ConnectClicked(&clickedCb)
	row.AddSuffix(&button.Widget)

	expander.AddRow(&row.Widget)
	uh.changelogRow = row
	uh.changelogButton = button
}

// refreshChangelogAvailability enables the drill-down once bootc reports a
// staged deployment, and records the two references to compare.
func (uh *UserHome) refreshChangelogAvailability(status *bootc.Status) {
	if uh.changelogRow == nil || uh.changelogButton == nil {
		return
	}

	var booted, staged string
	if status != nil {
		booted = sbom.PinnedReference(status.Status.Booted.ImageRef(), status.Status.Booted.Digest())
		staged = sbom.PinnedReference(status.Status.Staged.ImageRef(), status.Status.Staged.Digest())
	}

	uh.changelogBooted = booted
	uh.changelogStaged = staged

	available := booted != "" && staged != ""
	uh.changelogButton.SetSensitive(available)
	uh.changelogRow.SetSubtitle(pageview.ChangelogRow(available).Subtitle)
}

// onChangelogClicked fetches both SBOMs and renders the diff.
func (uh *UserHome) onChangelogClicked() {
	if !uh.changelogGate.TryStart() {
		return
	}

	button := uh.changelogButton
	row := uh.changelogRow
	booted, staged := uh.changelogBooted, uh.changelogStaged

	button.SetSensitive(false)
	button.SetLabel("Comparing...")
	row.SetSubtitle("Downloading both package lists...")

	go func() {
		defer uh.changelogGate.Reset()

		ctx, cancel := context.WithTimeout(context.Background(), changelogTimeout)
		defer cancel()

		result, err := sbom.Compare(ctx, fetchSBOM, booted, staged)

		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			button.SetLabel("Compare")

			if err != nil {
				log.Printf("changelog: %v", err)
				row.SetSubtitle(pageview.ChangelogRow(true).Subtitle)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Could not compare images: %v", err))
				return
			}

			row.SetSubtitle(pageview.ChangelogSummary(result))
			uh.renderChangelogSections(result)
		})
	}()
}

// renderChangelogSections replaces the per-category expanders under the
// changelog row. A repeat comparison must replace them rather than stack a
// second copy underneath the first.
func (uh *UserHome) renderChangelogSections(result sbom.Result) {
	parent := uh.bootcStageExpander
	if parent == nil {
		return
	}

	for _, expander := range uh.changelogSections {
		parent.Remove(&expander.Widget)
	}
	uh.changelogSections = nil

	for _, section := range pageview.ChangelogSections(result) {
		expander := adw.NewExpanderRow()
		expander.SetTitle(section.Title)
		expander.SetSubtitle(fmt.Sprintf("%d package(s)", len(section.Entries)))

		for _, entry := range section.Entries {
			row := adw.NewActionRow()
			row.SetTitle(entry.Title)
			row.SetSubtitle(entry.Subtitle)
			expander.AddRow(&row.Widget)
		}

		parent.AddRow(&expander.Widget)
		uh.changelogSections = append(uh.changelogSections, expander)
	}
}
