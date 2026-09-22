package registrytags

import (
	"sort"
	"strings"
	"time"
)

// dateLayout is the form every dated build tag ends in.
const dateLayout = "20060102"

// Build is a tag recognized as a dated build of one stream.
type Build struct {
	// Tag is the full tag, e.g. "stable-44.20260623".
	Tag string
	// Stream is the tag with its date and separating character removed —
	// "stable", "stable-44", "lts-testing-50". It is the registry's own
	// spelling, not a normalized stream name: this package does not know
	// which streams an image publishes, and internal/imageinfo's tables are
	// the authority on that (ADR-0011).
	Stream string
	// Date is the day the tag names, in UTC, taken from the tag itself
	// rather than from the registry. Every dated build sampled on
	// 2026-09-22 agreed with its own
	// org.opencontainers.image.created annotation to the day; the
	// annotation's precise time is available from Client.Tag when a caller
	// needs it, and reading it costs one request per tag.
	Date time.Time
}

// ParseBuild reports whether tag names a dated build, and if so which stream
// and which day.
//
// Verified against the complete tag list of ghcr.io/ublue-os/bluefin on
// 2026-09-22 — 1907 tags, read from the registry through this package's own
// pagination. Every dated tag ends in a separator followed by exactly eight
// digits, and both separators are in use for the same build:
//
//	44.20260623   stable-20260623   stable-44.20260623
//	stable-daily-20260623   gts-44.20260623
//	lts-testing.20260621   lts-testing-20260621
//	lts-hwe-testing-50.20260624
//
// The eight digits must form a real calendar date. That is what keeps the
// two large non-build populations out: the signature tags
// (sha256-<hex>.sig, 746 of the 1907) never end in a separator followed by
// eight digits, and the architecture-suffixed stream tags
// (lts-amd64, 10-testing-amd64) end in a word rather than a date.
//
// The grammar is exact rather than greedy on purpose. A tag ending in a
// longer digit run — a future YYYYMMDDHHMM form, say — is not recognized as
// a build, so it resolves to "no date" and the caller shows nothing for it
// rather than a day it inferred. Growing the grammar is a deliberate,
// verified change; guessing at one from an unvalidated shape is how a
// rollback list ends up offering a build that does not exist.
func ParseBuild(tag string) (Build, bool) {
	separator := strings.LastIndexAny(tag, ".-")
	if separator <= 0 {
		return Build{}, false
	}

	stream, digits := tag[:separator], tag[separator+1:]
	if len(digits) != len(dateLayout) || !isDigits(digits) {
		return Build{}, false
	}

	date, err := time.Parse(dateLayout, digits)
	if err != nil {
		return Build{}, false
	}
	return Build{Tag: tag, Stream: stream, Date: date.UTC()}, true
}

// isDigits reports whether every byte of value is an ASCII digit.
func isDigits(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

// Builds returns the dated builds among tags at or after since, newest
// first and, within one day, in tag order.
//
// The order is total, so a caller that renders the slice renders the same
// list on every pass and a test can assert it without sorting first.
//
// One build is reachable by several tags. Verified 2026-09-22:
// stable-20260623, 44.20260623, stable-44.20260623, stable-daily-20260623,
// stable-daily-44.20260623, gts-20260623 and gts-44.20260623 all resolve to
// sha256:9f0201d2… and the same creation time. Every alias is returned here
// rather than collapsed, because which of them is the right one to show
// depends on the stream the machine is booted into — a decision this package
// has no standing to make. A caller that wants one entry per build resolves
// the candidates through Client.Tag and groups by Digest, or groups by Date
// to accept the aliases.
func Builds(tags []string, since time.Time) []Build {
	builds := make([]Build, 0, len(tags))
	for _, tag := range tags {
		build, ok := ParseBuild(tag)
		if !ok || build.Date.Before(since) {
			continue
		}
		builds = append(builds, build)
	}

	sort.Slice(builds, func(i, j int) bool {
		if !builds[i].Date.Equal(builds[j].Date) {
			return builds[i].Date.After(builds[j].Date)
		}
		return builds[i].Tag < builds[j].Tag
	})
	return builds
}
