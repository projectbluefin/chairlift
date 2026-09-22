package registrytags

import (
	"testing"
	"time"
)

// verifiedTags is a sample of the tag list this package read from
// ghcr.io/ublue-os/bluefin on 2026-09-22, kept verbatim. It carries both
// populations of each kind the grammar has to separate, so the table below
// pins the parse against tags the registry actually publishes rather than
// against shapes invented to make the parser look right: the dated builds in
// both separator spellings, the moving stream tags, the architecture-suffixed
// stream tags, and the signature tags.
var verifiedTags = []string{
	// Dated builds.
	"44.20260623",
	"stable-20260623",
	"stable-44.20260623",
	"stable-daily-20260623",
	"stable-daily-44.20260623",
	"gts-20260623",
	"gts-44.20260623",
	"latest-44.20260623",
	"lts-testing.20260621",
	"lts-testing-20260621",
	"lts-hwe-testing-50.20260624",
	"stream10-testing.20260621",
	"10-hwe-testing-20260621",
	// Moving stream tags: no date at all.
	"latest",
	"stable",
	"stable-daily",
	"gts",
	"lts",
	"stream10",
	"10",
	"44",
	"lts-testing",
	"10-testing-50",
	// Architecture-suffixed stream tags: a word after the last separator.
	"lts-amd64",
	"lts-testing-amd64",
	"10-testing-amd64",
	"lts-hwe-testing-50-amd64",
	// Signature tags: a hex digest, never a date.
	"sha256-9f0201d21641133b15c5e58e6cf85008259e8ffce1c7169a063f7474f2f56c41.sig",
	"sha256-053bda05572dc8f45f3612a4dbd2a243e9f7deb33dd5666195016df08be9d12c",
}

func TestParseBuildAcceptsEveryDatedShapeTheRegistryPublishes(t *testing.T) {
	// The stream is the tag with its date and separating character removed,
	// which is the registry's own spelling: stable-20260623 and
	// stable-44.20260623 are two spellings of one build, and this parser
	// reports each under its own stream rather than guessing they are equal.
	tests := []struct {
		tag    string
		stream string
		date   string
	}{
		{tag: "44.20260623", stream: "44", date: "2026-06-23"},
		{tag: "stable-20260623", stream: "stable", date: "2026-06-23"},
		{tag: "stable-44.20260623", stream: "stable-44", date: "2026-06-23"},
		{tag: "stable-daily-20260623", stream: "stable-daily", date: "2026-06-23"},
		{tag: "stable-daily-44.20260623", stream: "stable-daily-44", date: "2026-06-23"},
		{tag: "gts-20260623", stream: "gts", date: "2026-06-23"},
		{tag: "gts-44.20260623", stream: "gts-44", date: "2026-06-23"},
		{tag: "latest-44.20260623", stream: "latest-44", date: "2026-06-23"},
		// Both separators are in use, and both name the same build.
		{tag: "lts-testing.20260621", stream: "lts-testing", date: "2026-06-21"},
		{tag: "lts-testing-20260621", stream: "lts-testing", date: "2026-06-21"},
		{tag: "lts-hwe-testing-50.20260624", stream: "lts-hwe-testing-50", date: "2026-06-24"},
		{tag: "stream10-testing.20260621", stream: "stream10-testing", date: "2026-06-21"},
		{tag: "10-hwe-testing-20260621", stream: "10-hwe-testing", date: "2026-06-21"},
	}

	for _, tt := range tests {
		build, ok := ParseBuild(tt.tag)
		if !ok {
			t.Errorf("ParseBuild(%q) did not recognize a dated build", tt.tag)
			continue
		}
		if build.Tag != tt.tag {
			t.Errorf("ParseBuild(%q).Tag = %q", tt.tag, build.Tag)
		}
		if build.Stream != tt.stream {
			t.Errorf("ParseBuild(%q).Stream = %q, want %q", tt.tag, build.Stream, tt.stream)
		}
		want, err := time.Parse(dateLayout, tt.date[:4]+tt.date[5:7]+tt.date[8:])
		if err != nil {
			t.Fatalf("bad test date %q: %v", tt.date, err)
		}
		if !build.Date.Equal(want) {
			t.Errorf("ParseBuild(%q).Date = %s, want %s", tt.tag, build.Date, want)
		}
	}
}

func TestParseBuildRejectsEveryTagThatIsNotADatedBuild(t *testing.T) {
	tests := []struct {
		tag string
		why string
	}{
		{tag: "latest", why: "a moving stream tag carries no date"},
		{tag: "stable", why: "a moving stream tag carries no date"},
		{tag: "stable-daily", why: "a moving stream tag carries no date"},
		{tag: "lts-testing", why: "a moving stream tag carries no date"},
		{tag: "10-testing-50", why: "a major version is not a date"},
		{tag: "lts-amd64", why: "an architecture suffix is a word, not a date"},
		{tag: "lts-testing-amd64", why: "an architecture suffix is a word, not a date"},
		{tag: "lts-hwe-testing-50-amd64", why: "an architecture suffix is a word, not a date"},
		{
			tag: "sha256-9f0201d21641133b15c5e58e6cf85008259e8ffce1c7169a063f7474f2f56c41.sig",
			why: "a signature tag ends in .sig",
		},
		{
			tag: "sha256-053bda05572dc8f45f3612a4dbd2a243e9f7deb33dd5666195016df08be9d12c",
			why: "a signature tag ends in hex, not a date",
		},
		// The remaining shapes are synthetic, and pin the two properties the
		// grammar relies on: the digit run is exactly eight, and the digits
		// have to be a real calendar date.
		{tag: "20260623", why: "a bare date has no stream to belong to"},
		{tag: ".20260623", why: "a date with no stream has nothing to name"},
		{tag: "stable-", why: "a separator with no date is not a build"},
		{tag: "stable-2026062", why: "seven digits is not a date"},
		{tag: "stable-202606231", why: "nine digits is not a date"},
		{tag: "stable-20260623T0000", why: "a longer digit run is not the verified grammar"},
		{tag: "stable-20261301", why: "month 13 is not a real date"},
		{tag: "stable-20260632", why: "day 32 is not a real date"},
		{tag: "stable-20260230", why: "February the 30th is not a real date"},
		{tag: "stable-2026.0623", why: "the digits must be contiguous"},
		{tag: "", why: "an empty tag names nothing"},
	}

	for _, tt := range tests {
		if build, ok := ParseBuild(tt.tag); ok {
			t.Errorf("ParseBuild(%q) accepted %s, returning %+v", tt.tag, tt.why, build)
		}
	}
}

func TestParseBuildAgreesWithTheVerifiedTagList(t *testing.T) {
	// Every tag in the sample resolves to exactly one of the two answers,
	// and the dated ones are the ones this test names. A grammar change that
	// started accepting stream tags, or that stopped accepting one of the
	// dated spellings, changes this count.
	dated := 0
	for _, tag := range verifiedTags {
		if _, ok := ParseBuild(tag); ok {
			dated++
		}
	}

	const wantDated = 13
	if dated != wantDated {
		t.Errorf("ParseBuild recognized %d of the %d verified tags as builds, want %d",
			dated, len(verifiedTags), wantDated)
	}
}

func TestBuildsOrdersNewestFirstAndFiltersByDay(t *testing.T) {
	builds := Builds([]string{
		"stable-20260621",
		"lts-amd64",
		"stable-20260623",
		"stable-20260622",
		"sha256-abc.sig",
		"44.20260623",
	}, time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC))

	want := []string{"44.20260623", "stable-20260623", "stable-20260622"}
	if len(builds) != len(want) {
		t.Fatalf("Builds returned %d builds, want %d: %+v", len(builds), len(want), builds)
	}
	for index := range want {
		if builds[index].Tag != want[index] {
			t.Errorf("Builds[%d].Tag = %q, want %q", index, builds[index].Tag, want[index])
		}
	}
}

func TestBuildsIncludesTheBoundaryDay(t *testing.T) {
	// `since` is a day boundary, not a moment: a calendar asked for "the last
	// ninety days" includes the ninetieth.
	builds := Builds([]string{"stable-20260601"}, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if len(builds) != 1 {
		t.Fatalf("Builds excluded the boundary day: %+v", builds)
	}
}

func TestBuildsIsEmptyWhenNoTagIsDated(t *testing.T) {
	builds := Builds([]string{"latest", "stable", "lts-amd64"}, time.Time{})
	if len(builds) != 0 {
		t.Errorf("Builds returned %+v, want no builds", builds)
	}
}

func TestBuildsBreaksTiesByTagSoTheOrderIsTotal(t *testing.T) {
	// One build is reachable by several tags, and several tags can name the
	// same day. The order within a day is fixed so a re-render does not
	// shuffle the list under the user.
	builds := Builds([]string{"gts-20260623", "44.20260623", "stable-20260623"}, time.Time{})
	want := []string{"44.20260623", "gts-20260623", "stable-20260623"}

	if len(builds) != len(want) {
		t.Fatalf("Builds returned %d builds, want %d", len(builds), len(want))
	}
	for index := range want {
		if builds[index].Tag != want[index] {
			t.Errorf("Builds[%d].Tag = %q, want %q", index, builds[index].Tag, want[index])
		}
	}
}
