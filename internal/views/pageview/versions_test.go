package pageview

import (
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/registrytags"
)

func TestCatalogStreamFollowsTheStreamOfAPinnedBuild(t *testing.T) {
	tests := []struct{ tag, want string }{
		{"stable", "stable"},
		{"lts-testing", "lts-testing"},
		{"stable-20260908", "stable"},
		{"lts-testing.20260621", "lts-testing"},
		{"", ""},
	}
	for _, test := range tests {
		if got := CatalogStream(test.tag); got != test.want {
			t.Errorf("CatalogStream(%q) = %q, want %q", test.tag, got, test.want)
		}
	}
}

// Tags as GHCR lists them for ghcr.io/ublue-os/bluefin-dx on 2026-09-24:
// one build per day is reachable under several streams' spellings.
func TestPublishedVersionsListsOneRowPerDayOfTheRunningStream(t *testing.T) {
	builds := registrytags.Builds([]string{
		"stable", "latest", "sha256-0f3c.sig",
		"gts-20260922", "stable-20260922", "stable-daily-20260922", "stable-44.20260922",
		"gts-20260915", "stable-20260915", "stable-daily-20260915",
		"latest-20260908", "stable-20260908", "stable-daily-20260908",
		"stable-20260901",
	}, time.Time{})

	got := PublishedVersions(builds, "stable", "44.20260908", "44.20260901")
	want := []Row{
		{Title: "22 September 2026", Subtitle: "Published as stable-20260922"},
		{Title: "15 September 2026", Subtitle: "Published as stable-20260915"},
		{Title: "8 September 2026", Subtitle: "Running now · Published as stable-20260908"},
		{Title: "1 September 2026", Subtitle: "Your previous version · Published as stable-20260901"},
	}
	if len(got) != len(want) {
		t.Fatalf("PublishedVersions() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("PublishedVersions()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestPublishedVersionsMarksNothingForAnUnreadableVersion(t *testing.T) {
	builds := registrytags.Builds([]string{"stable-20260908"}, time.Time{})
	got := PublishedVersions(builds, "stable", "", "not-a-build")
	if len(got) != 1 || got[0].Subtitle != "Published as stable-20260908" {
		t.Errorf("PublishedVersions() = %#v, want one unmarked row", got)
	}
}
