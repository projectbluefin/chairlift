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

// PublishedVersionsRow returns the Recovery row that lists the stream's
// published versions, before anything has been read. The read is a network
// request to the image registry, so the subtitle says so.
func PublishedVersionsRow(stream string) Row {
	return Row{
		Title: "Published versions",
		Subtitle: fmt.Sprintf("See the %s versions the image registry still offers from the last %d days. This asks the registry each time.",
			stream, PublishedVersionsDays),
	}
}

// PublishedVersionsSummary returns the row subtitle after a completed read.
func PublishedVersionsSummary(count int, stream string) string {
	switch count {
	case 0:
		return fmt.Sprintf("The registry lists no %s versions from the last %d days", stream, PublishedVersionsDays)
	case 1:
		return fmt.Sprintf("1 %s version from the last %d days", stream, PublishedVersionsDays)
	default:
		return fmt.Sprintf("%d %s versions from the last %d days", count, stream, PublishedVersionsDays)
	}
}

// PublishedVersions returns one row per day the registry lists a build of
// stream, newest first. builds must be registrytags.Builds output (already
// newest first); aliases of other streams are dropped, and several tags of
// the same stream on one day collapse into the first.
//
// running and previous are the bootc image versions of the booted and
// rollback deployments ("44.20260908"); either may be empty. A day matching
// one of them is marked, so the list says where the machine is and where
// Roll Back would take it.
func PublishedVersions(builds []registrytags.Build, stream, running, previous string) []Row {
	runningDay, hasRunning := versionDay(running)
	previousDay, hasPrevious := versionDay(previous)

	rows := make([]Row, 0, len(builds))
	var lastDay time.Time
	for _, build := range builds {
		if build.Stream != stream || build.Date.Equal(lastDay) {
			continue
		}
		lastDay = build.Date

		subtitle := "Published as " + build.Tag
		switch {
		case hasRunning && build.Date.Equal(runningDay):
			subtitle = "Running now · " + subtitle
		case hasPrevious && build.Date.Equal(previousDay):
			subtitle = "Your previous version · " + subtitle
		}
		rows = append(rows, Row{
			// The date is a calendar day named by the tag, not an instant,
			// so it is formatted as-is rather than shifted into local time.
			Title:    build.Date.Format("2 January 2006"),
			Subtitle: subtitle,
		})
	}
	return rows
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
