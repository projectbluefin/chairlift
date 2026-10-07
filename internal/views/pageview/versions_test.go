package pageview

import (
	"strings"
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
	want := []PublishedVersion{
		{Row: Row{Title: "22 September 2026"}, Day: "20260922"},
		{Row: Row{Title: "15 September 2026"}, Day: "20260915"},
		{Row: Row{Title: "8 September 2026", Subtitle: "Running now"}, Day: "20260908"},
		{Row: Row{Title: "1 September 2026", Subtitle: "Your previous version"}, Day: "20260901"},
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
	if len(got) != 1 || got[0].Subtitle != "" || got[0].Day != "20260908" {
		t.Errorf("PublishedVersions() = %#v, want one unmarked row", got)
	}
}

// The versions copy is read by people choosing a day to go back to, so it
// names no stream, registry, tag, or build.
func TestVersionsCopyNamesNoRegistryJargon(t *testing.T) {
	pinTitle, pinBody := PinConfirmation("20 September 2026")
	unpinTitle, unpinBody := UnpinConfirmation()
	texts := []string{
		PublishedVersionsRow().Subtitle,
		PublishedVersionsSummary(0), PublishedVersionsSummary(1), PublishedVersionsSummary(5),
		pinTitle, pinBody, unpinTitle, unpinBody,
		UnpinRow(true).Title, UnpinRow(true).Subtitle, UnpinRow(false).Subtitle,
		PinUnsupportedExplanation(), UnpinUnsupportedExplanation(),
	}
	for _, text := range texts {
		for _, jargon := range []string{"stream", "Stream", "registry", "tag", "build", "image", "system"} {
			if strings.Contains(text, jargon) {
				t.Errorf("%q contains %q", text, jargon)
			}
		}
	}
}

func TestPublishedVersionsSummaryCountsVersions(t *testing.T) {
	if got := PublishedVersionsSummary(5); !strings.HasPrefix(got, "5 versions") {
		t.Errorf("PublishedVersionsSummary(5) = %q, want the count", got)
	}
	if got := PublishedVersionsSummary(1); !strings.HasPrefix(got, "1 version ") {
		t.Errorf("PublishedVersionsSummary(1) = %q, want a singular count", got)
	}
	if got := PublishedVersionsSummary(0); !strings.HasPrefix(got, "No versions") {
		t.Errorf("PublishedVersionsSummary(0) = %q, want it to say there are none", got)
	}
}

func TestPinConfirmationStatesTheTargetAndRestart(t *testing.T) {
	title, body := PinConfirmation("20 September 2026")
	if !strings.Contains(title, "20 September 2026") {
		t.Errorf("PinConfirmation() title = %q, want it to name the day", title)
	}
	for _, fragment := range []string{"Updates stop", "restart", "regular updates"} {
		if !strings.Contains(body, fragment) {
			t.Errorf("PinConfirmation() body = %q, want it to contain %q", body, fragment)
		}
	}
}

func TestUnpinConfirmationStatesRestart(t *testing.T) {
	title, body := UnpinConfirmation()
	if !strings.Contains(title, "Regular Updates") {
		t.Errorf("UnpinConfirmation() title = %q, want it to name regular updates", title)
	}
	for _, fragment := range []string{"newest version", "restart"} {
		if !strings.Contains(body, fragment) {
			t.Errorf("UnpinConfirmation() body = %q, want it to contain %q", body, fragment)
		}
	}
}

func TestUnpinRowReflectsSupport(t *testing.T) {
	supported := UnpinRow(true)
	unsupported := UnpinRow(false)
	if supported.Title != unsupported.Title {
		t.Errorf("titles differ: %q vs %q", supported.Title, unsupported.Title)
	}
	if unsupported.Subtitle != UnpinUnsupportedExplanation() || supported.Subtitle == unsupported.Subtitle {
		t.Errorf("UnpinRow subtitles = %q / %q, want the unsupported one to explain", supported.Subtitle, unsupported.Subtitle)
	}
}

func TestPinAndUnpinUnsupportedExplanations(t *testing.T) {
	for _, got := range []string{PinUnsupportedExplanation(), UnpinUnsupportedExplanation()} {
		if !strings.Contains(got, "isn't available on this computer") {
			t.Errorf("unsupported explanation = %q, want it to say it isn't available", got)
		}
	}
}
