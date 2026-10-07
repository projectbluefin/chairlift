package pageview

import (
	"fmt"
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
		{Row: Row{Title: "22 September 2026", Subtitle: "Published as stable-20260922"}, Day: "20260922"},
		{Row: Row{Title: "15 September 2026", Subtitle: "Published as stable-20260915"}, Day: "20260915"},
		{Row: Row{Title: "8 September 2026", Subtitle: "Running now · Published as stable-20260908"}, Day: "20260908"},
		{Row: Row{Title: "1 September 2026", Subtitle: "Your previous version · Published as stable-20260901"}, Day: "20260901"},
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
	if len(got) != 1 || got[0].Subtitle != "Published as stable-20260908" || got[0].Day != "20260908" {
		t.Errorf("PublishedVersions() = %#v, want one unmarked row", got)
	}
}

func TestPinConfirmationStatesTheTargetAndRestart(t *testing.T) {
	title, body := PinConfirmation("20 September 2026")
	if title != "Pin to 20 September 2026?" {
		t.Errorf("PinConfirmation() title = %q, want %q", title, "Pin to 20 September 2026?")
	}
	for _, fragment := range []string{"20 September 2026", "restart", "return to the stream"} {
		if !strings.Contains(body, fragment) {
			t.Errorf("PinConfirmation() body = %q, want it to contain %q", body, fragment)
		}
	}
}

func TestUnpinConfirmationStatesStreamAndRestart(t *testing.T) {
	title, body := UnpinConfirmation("latest")
	if title != "Return to Stream?" {
		t.Errorf("UnpinConfirmation() title = %q, want %q", title, "Return to Stream?")
	}
	for _, fragment := range []string{"latest", "restart", "regular updates"} {
		if !strings.Contains(body, fragment) {
			t.Errorf("UnpinConfirmation() body = %q, want it to contain %q", body, fragment)
		}
	}
}

func TestUnpinRowReflectsTheOffer(t *testing.T) {
	offered := UnpinRow("latest", "")
	if offered.Title != "Return to stream" {
		t.Errorf("UnpinRow(offered).Title = %q, want %q", offered.Title, "Return to stream")
	}
	if !strings.Contains(offered.Subtitle, "latest") {
		t.Errorf("UnpinRow(offered).Subtitle = %q, want it to name the stream", offered.Subtitle)
	}

	_, explanation := UnpinOffer("latest", PinSupport{})
	if got := UnpinRow("latest", explanation).Subtitle; got != explanation {
		t.Errorf("UnpinRow(refused).Subtitle = %q, want the refusal %q", got, explanation)
	}
}

// Every refusal the helper makes before deriving a pin or unpin target —
// no helper command, a broken channel table, a stream outside the image's
// channel table — must be decided here, before the password prompt, rather
// than reached as an error after authentication.
func TestPinAndUnpinAreOfferedOnlyWhenTheHelperCanDeriveATarget(t *testing.T) {
	ready := PinSupport{Helper: true, KnownStream: true}
	tests := []struct {
		name    string
		support PinSupport
		offered bool
		pin     string
		unpin   string
	}{
		{name: "ready", support: ready, offered: true},
		{
			name:    "no helper command",
			support: PinSupport{KnownStream: true},
			pin:     "Pinning is not supported on this system",
			unpin:   "Returning to the stream is not supported on this system",
		},
		{
			name:    "broken channel table",
			support: PinSupport{Helper: true, TableBroken: true, KnownStream: true},
			pin:     "Pinning is unavailable until this system's update settings are repaired",
			unpin:   "Returning to the stream is unavailable until this system's update settings are repaired",
		},
		{
			name:    "stream outside the channel table",
			support: PinSupport{Helper: true},
			pin:     "Pinning is not available for the “stable” stream of this image",
			unpin:   "Returning to the stream is not available for the “stable” stream of this image",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offered, explanation := PinOffer("stable", tt.support)
			if offered != tt.offered || explanation != tt.pin {
				t.Errorf("PinOffer() = %v, %q; want %v, %q", offered, explanation, tt.offered, tt.pin)
			}
			offered, explanation = UnpinOffer("stable", tt.support)
			if offered != tt.offered || explanation != tt.unpin {
				t.Errorf("UnpinOffer() = %v, %q; want %v, %q", offered, explanation, tt.offered, tt.unpin)
			}
		})
	}
}

// A list re-rendered during a pin (Check Again while `bootc switch` pulls for
// minutes) must show the pin in flight, and no Pin may start while another
// recovery switch holds the page.
func TestPinButtonFollowsTheRecoverySwitchInFlight(t *testing.T) {
	tests := []struct {
		name          string
		offered       bool
		alreadyPinned bool
		day, pinning  string
		busy          bool
		want          PinButtonState
	}{
		{name: "idle", offered: true, day: "20260913", want: PinButtonState{Label: "Pin", Sensitive: true}},
		{name: "not offered", day: "20260913", want: PinButtonState{Label: "Pin"}},
		{name: "booted build", offered: true, alreadyPinned: true, day: "20260920", want: PinButtonState{Label: "Pinned"}},
		{name: "this pin in flight", offered: true, day: "20260913", pinning: "20260913", busy: true, want: PinButtonState{Label: "Pinning…"}},
		{name: "another pin in flight", offered: true, day: "20260906", pinning: "20260913", busy: true, want: PinButtonState{Label: "Pin"}},
		{name: "return to stream or roll back running", offered: true, day: "20260913", busy: true, want: PinButtonState{Label: "Pin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PinButton(tt.offered, tt.alreadyPinned, tt.day, tt.pinning, tt.busy); got != tt.want {
				t.Errorf("PinButton() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// The catalog serves a repeat read from cache for registrytags.DefaultTTL, so
// the row must not promise a fresh registry request on every press.
func TestPublishedVersionsRowStatesTheReuseWindow(t *testing.T) {
	subtitle := PublishedVersionsRow("latest").Subtitle
	if strings.Contains(subtitle, "each time") {
		t.Errorf("PublishedVersionsRow().Subtitle = %q, promises a registry request on every press", subtitle)
	}
	window := fmt.Sprintf("within %d minutes", int(registrytags.DefaultTTL/time.Minute))
	if !strings.Contains(subtitle, window) || !strings.Contains(subtitle, "“latest” stream") {
		t.Errorf("PublishedVersionsRow().Subtitle = %q, want it to name the stream and %q", subtitle, window)
	}
}
