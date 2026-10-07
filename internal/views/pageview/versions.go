package pageview

import (
	"fmt"
	"time"

	"github.com/projectbluefin/chairlift/internal/registrytags"
)

// PublishedVersionsDays is how far back the Recovery page's list of published
// versions reaches. The day of each build comes from its tag name, so a longer
// window costs no extra registry requests — only a longer list to read.
const PublishedVersionsDays = 90

// CatalogStream returns the stream whose published versions the Recovery
// page lists, given the tag the machine is running. A stream tag ("stable",
// "lts-testing") is its own stream; a dated tag ("stable-20260908") is a
// build of the stream its date is attached to. "" means the running tag is
// neither — a digest pin, or no tag at all — and there is no stream to list.
func CatalogStream(tag string) string {
	if build, ok := registrytags.ParseBuild(tag); ok {
		return build.Stream
	}
	return tag
}

// PublishedVersionsRow returns the Recovery row that lists the versions this
// computer could go back to, before anything has been read. The row names no
// stream or registry: the list is always the stream the computer follows.
func PublishedVersionsRow() Row {
	return Row{
		Title:    "Published versions",
		Subtitle: fmt.Sprintf("See which versions came out in the last %d days.", PublishedVersionsDays),
	}
}

// PublishedVersionsSummary returns the row subtitle after a completed read.
func PublishedVersionsSummary(count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("No versions were released in the last %d days.", PublishedVersionsDays)
	case 1:
		return fmt.Sprintf("1 version released in the last %d days.", PublishedVersionsDays)
	default:
		return fmt.Sprintf("%d versions released in the last %d days.", count, PublishedVersionsDays)
	}
}

// PublishedVersion is one published build of a stream shown in the Recovery
// page's Published versions list.
type PublishedVersion struct {
	Row
	Day string
}

// PublishedVersions returns one row per day the registry lists a build of
// stream, newest first. builds must be registrytags.Builds output (already
// newest first); aliases of other streams are dropped, and several tags of
// the same stream on one day collapse into the first.
//
// running and previous are the bootc image versions of the booted and
// rollback deployments ("44.20260908"); either may be empty. A day matching
// one of them is marked, so the list says where the machine is and where
// Roll Back would take it. Other days have no subtitle: the tag a build was
// published under is registry detail nobody choosing a day needs.
func PublishedVersions(builds []registrytags.Build, stream, running, previous string) []PublishedVersion {
	runningDay, hasRunning := versionDay(running)
	previousDay, hasPrevious := versionDay(previous)

	rows := make([]PublishedVersion, 0, len(builds))
	var lastDay time.Time
	for _, build := range builds {
		if build.Stream != stream || build.Date.Equal(lastDay) {
			continue
		}
		lastDay = build.Date

		subtitle := ""
		switch {
		case hasRunning && build.Date.Equal(runningDay):
			subtitle = "Running now"
		case hasPrevious && build.Date.Equal(previousDay):
			subtitle = "Your previous version"
		}
		rows = append(rows, PublishedVersion{
			Row: Row{
				// The date is a calendar day named by the tag, not an instant,
				// so it is formatted as-is rather than shifted into local time.
				Title:    build.Date.Format("2 January 2006"),
				Subtitle: subtitle,
			},
			Day: build.Date.Format("20060102"),
		})
	}
	return rows
}

// PinConfirmation returns the title and body of the confirmation dialog shown
// before pinning to a published dated build.
func PinConfirmation(date string) (title, body string) {
	title = fmt.Sprintf("Pin the %s Version?", date)
	body = "Updates stop at this version until you go back to regular updates. " +
		"It takes effect the next time you restart."
	return title, body
}

// UnpinConfirmation returns the title and body of the confirmation dialog
// shown before returning to the regular release stream.
func UnpinConfirmation() (title, body string) {
	return "Go Back to Regular Updates?",
		"This computer moves to the newest version the next time you restart."
}

// UnpinRow returns the title and subtitle for the row that returns a pinned
// computer to its regular release stream.
func UnpinRow(supported bool) Row {
	subtitle := "Get the newest version again."
	if !supported {
		subtitle = UnpinUnsupportedExplanation()
	}
	return Row{
		Title:    "Go back to regular updates",
		Subtitle: subtitle,
	}
}

// PinUnsupportedExplanation returns the explanation when pinning is not
// supported by the system image.
func PinUnsupportedExplanation() string {
	return "Pinning isn't available on this computer."
}

// UnpinUnsupportedExplanation returns the explanation when returning to the
// stream is not supported by the system image.
func UnpinUnsupportedExplanation() string {
	return "Going back to regular updates isn't available on this computer."
}

// versionDay reads the build day out of a bootc image version. Bluefin's
// versions use the dated-tag grammar ("44.20260908"); anything else has no
// day to match.
func versionDay(version string) (time.Time, bool) {
	build, ok := registrytags.ParseBuild(version)
	if !ok {
		return time.Time{}, false
	}
	return build.Date, true
}
