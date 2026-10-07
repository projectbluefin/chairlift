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
// request to the image registry, so the subtitle says so — and says that a
// repeat check inside registrytags.DefaultTTL reuses the last answer, because
// the process-wide catalog serves it from cache without asking again. The
// stream is quoted as a name: Dakota's stream is "latest", and "the latest
// versions" read as a claim about recency rather than the stream being
// listed.
func PublishedVersionsRow(stream string) Row {
	return Row{
		Title: "Published versions",
		Subtitle: fmt.Sprintf("See the versions of the “%s” stream the image registry still offers from the last %d days. A check within %d minutes of the last one reuses its answer.",
			stream, PublishedVersionsDays, int(registrytags.DefaultTTL/time.Minute)),
	}
}

// PublishedVersionsSummary returns the row subtitle after a completed read.
func PublishedVersionsSummary(count int, stream string) string {
	switch count {
	case 0:
		return fmt.Sprintf("The registry lists no versions of the “%s” stream from the last %d days", stream, PublishedVersionsDays)
	case 1:
		return fmt.Sprintf("1 version of the “%s” stream from the last %d days", stream, PublishedVersionsDays)
	default:
		return fmt.Sprintf("%d versions of the “%s” stream from the last %d days", count, stream, PublishedVersionsDays)
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
// Roll Back would take it.
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

		subtitle := "Published as " + build.Tag
		runningNow := hasRunning && build.Date.Equal(runningDay)
		switch {
		case runningNow:
			subtitle = "Running now · " + subtitle
		case hasPrevious && build.Date.Equal(previousDay):
			subtitle = "Your previous version · " + subtitle
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
	title = fmt.Sprintf("Pin to %s?", date)
	body = fmt.Sprintf("This stages a switch to the build from %s. Automatic updates will stay at this version until you return to the stream. The change applies the next time you restart.", date)
	return title, body
}

// UnpinConfirmation returns the title and body of the confirmation dialog
// shown before returning to the regular release stream.
func UnpinConfirmation(stream string) (title, body string) {
	title = "Return to Stream?"
	body = fmt.Sprintf("This stages a switch back to regular updates on the “%s” stream. The change applies the next time you restart.", stream)
	return title, body
}

// UnpinRow returns the title and subtitle for the Return to stream row.
// explanation is UnpinOffer's: non-empty when the row's button cannot do
// anything, in which case it replaces the description.
func UnpinRow(stream, explanation string) Row {
	subtitle := fmt.Sprintf("Switch back to the newest updates on the “%s” stream", stream)
	if explanation != "" {
		subtitle = explanation
	}
	return Row{
		Title:    "Return to stream",
		Subtitle: subtitle,
	}
}

// PinSupport is what the GUI knows, before anyone authenticates, about
// whether the privileged helper can carry out a pin or a return to the
// stream on this host.
type PinSupport struct {
	// Helper reports that the installed helper and its PolicyKit action
	// provide the command.
	Helper bool
	// TableBroken reports that the authoritative channel table exists but
	// could not be applied; the helper refuses every pin and unpin then.
	TableBroken bool
	// KnownStream reports that the running stream belongs to this image's
	// channel table (imageinfo.KnownStream); the helper derives no target
	// for any other stream.
	KnownStream bool
}

// PinOffer reports whether a Published versions row's Pin button is offered,
// and otherwise why not. Every refusal here is one the helper would make only
// after the administrator password, so the button must not invite it.
func PinOffer(stream string, support PinSupport) (offered bool, explanation string) {
	return switchOffer("Pinning", stream, support)
}

// UnpinOffer is PinOffer for the Return to stream button.
func UnpinOffer(stream string, support PinSupport) (offered bool, explanation string) {
	return switchOffer("Returning to the stream", stream, support)
}

func switchOffer(action, stream string, support PinSupport) (bool, string) {
	switch {
	case !support.Helper:
		return false, action + " is not supported on this system"
	case support.TableBroken:
		return false, action + " is unavailable until this system's update settings are repaired"
	case !support.KnownStream:
		return false, fmt.Sprintf("%s is not available for the “%s” stream of this image", action, stream)
	default:
		return true, ""
	}
}

// PinButtonState is one Published versions row's Pin button.
type PinButtonState struct {
	Label     string
	Sensitive bool
}

// PinButton derives a Pin button from everything that decides it, so a list
// re-rendered while a pin is running (Check Again during a minutes-long
// `bootc switch`) shows the pin in flight instead of a fresh, inert button.
// pinning is the day of the pin in flight, "" when none; busy reports that
// another recovery switch — a pin, Return to stream, or Roll Back — holds the
// page, because two bootc transactions would race for the next boot.
func PinButton(offered, alreadyPinned bool, day, pinning string, busy bool) PinButtonState {
	switch {
	case pinning != "" && day == pinning:
		return PinButtonState{Label: "Pinning…"}
	case alreadyPinned:
		return PinButtonState{Label: "Pinned"}
	default:
		return PinButtonState{Label: "Pin", Sensitive: offered && !busy}
	}
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
