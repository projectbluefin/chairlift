package pageview

import (
	"strings"
	"testing"
)

func TestDamaskOfferNeverTreatsAFailedProbeAsAbsent(t *testing.T) {
	tests := []struct {
		name                                   string
		installed, probeFailed, flatpakPresent bool
		want                                   DamaskState
	}{
		{name: "installed opens", installed: true, flatpakPresent: true, want: DamaskInstalled},
		{name: "absent with Flatpak installs", flatpakPresent: true, want: DamaskInstallable},
		{name: "absent without Flatpak is hidden", want: DamaskHidden},
		{name: "a failed probe is unknown", probeFailed: true, flatpakPresent: true, want: DamaskUnknown},
		{name: "a failed probe is unknown even when Flatpak looked missing", probeFailed: true, want: DamaskUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DamaskOffer(tt.installed, tt.probeFailed, tt.flatpakPresent); got != tt.want {
				t.Fatalf("DamaskOffer(%v, %v, %v) = %v, want %v", tt.installed, tt.probeFailed, tt.flatpakPresent, got, tt.want)
			}
		})
	}
}

// TestDamaskControlIsOneStatefulAction holds every visible state to one
// distinct button label and a subtitle that agrees with it, so install, open
// and retry can never be offered side by side or mislabelled.
func TestDamaskControlIsOneStatefulAction(t *testing.T) {
	tests := []struct {
		state        DamaskState
		action       string
		subtitleHas  string
		failureHas   string
		subtitleLack string
	}{
		{state: DamaskInstallable, action: "Install", subtitleHas: "Install", failureHas: "install", subtitleLack: "Couldn't"},
		{state: DamaskInstalled, action: "Open", subtitleHas: "feeds", failureHas: "open", subtitleLack: "Install"},
		{state: DamaskUnknown, action: "Try Again", subtitleHas: "Couldn't check", failureHas: "check", subtitleLack: "Install a"},
	}
	seen := map[string]bool{}
	for _, tt := range tests {
		row := DamaskRow(tt.state)
		if row.Title != "Damask" {
			t.Errorf("state %v: title %q, want Damask", tt.state, row.Title)
		}
		if !strings.Contains(row.Subtitle, tt.subtitleHas) || strings.Contains(row.Subtitle, tt.subtitleLack) {
			t.Errorf("state %v: subtitle %q should contain %q and not %q", tt.state, row.Subtitle, tt.subtitleHas, tt.subtitleLack)
		}
		action := DamaskActionName(tt.state)
		if action != tt.action {
			t.Errorf("state %v: action %q, want %q", tt.state, action, tt.action)
		}
		if seen[action] {
			t.Errorf("state %v reuses action label %q", tt.state, action)
		}
		seen[action] = true
		failure := DamaskFailure(tt.state)
		if !strings.Contains(failure, tt.failureHas) || !strings.Contains(failure, "Try again") {
			t.Errorf("state %v: failure %q should name %q and offer a retry", tt.state, failure, tt.failureHas)
		}
	}
}

func TestWallpaperGroupFragmentNamesBothSources(t *testing.T) {
	for _, want := range []string{"collections", "apps"} {
		if !strings.Contains(WallpaperGroupFragment, want) {
			t.Errorf("WallpaperGroupFragment %q does not mention %q", WallpaperGroupFragment, want)
		}
	}
}

// TestWallpaperChannelsOfferNeverReadsAFailureAsUnsupported holds the channel
// selector to #230: only GNOME takes a picture, a failed check is unknown
// (with a retry), and no channel is offered that is not installed.
func TestWallpaperChannelsOfferNeverReadsAFailureAsUnsupported(t *testing.T) {
	tests := []struct {
		name                                  string
		supported, probeFailed, discoveryFail bool
		channels                              int
		want                                  WallpaperChannelsState
	}{
		{name: "GNOME with installed channels offers them", supported: true, channels: 2, want: WallpaperChannelsAvailable},
		{name: "GNOME without any collection offers nothing", supported: true, want: WallpaperChannelsHidden},
		{name: "another desktop never offers a GNOME write", channels: 2, want: WallpaperChannelsHidden},
		{name: "a failed desktop check is unknown", probeFailed: true, channels: 2, want: WallpaperChannelsUnknown},
		{name: "a failed desktop check with nothing installed offers nothing", probeFailed: true, want: WallpaperChannelsHidden},
		{name: "an unreadable catalog with nothing found is unknown", supported: true, discoveryFail: true, want: WallpaperChannelsUnknown},
		{name: "an unreadable catalog does not hide the channels it found", supported: true, discoveryFail: true, channels: 1, want: WallpaperChannelsAvailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WallpaperChannelsOffer(tt.supported, tt.probeFailed, tt.channels, tt.discoveryFail)
			if got != tt.want {
				t.Fatalf("WallpaperChannelsOffer(%v, %v, %d, %v) = %v, want %v",
					tt.supported, tt.probeFailed, tt.channels, tt.discoveryFail, got, tt.want)
			}
		})
	}
}

func TestWallpaperChannelsRowSaysWhatTheGroupCanDo(t *testing.T) {
	unknown := WallpaperChannelsRow(WallpaperChannelsUnknown)
	if !strings.Contains(unknown.Subtitle, "Couldn't check") {
		t.Fatalf("unknown row = %+v; it must say the check failed", unknown)
	}
	available := WallpaperChannelsRow(WallpaperChannelsAvailable)
	if available.Title == unknown.Title && available.Subtitle == unknown.Subtitle {
		t.Fatal("the available and unknown rows read the same")
	}
	if strings.Contains(available.Subtitle, "Couldn't") {
		t.Fatalf("available row = %+v; it must not read as a failure", available)
	}
}

func TestWallpaperCountIsPluralized(t *testing.T) {
	for count, want := range map[int]string{1: "1 wallpaper", 2: "2 wallpapers", 12: "12 wallpapers"} {
		if got := WallpaperCount(count); got != want {
			t.Fatalf("WallpaperCount(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestWallpaperApplyFailureIsRetryable(t *testing.T) {
	if !strings.Contains(WallpaperApplyFailure, "Try again") {
		t.Fatalf("WallpaperApplyFailure = %q; a failed apply keeps the tiles as the retry", WallpaperApplyFailure)
	}
}

func TestWallpaperChannelSubtitleSaysWhenSomeCollectionsWereUnreadable(t *testing.T) {
	if got := WallpaperChannelSubtitle(12, false); got != "12 wallpapers" {
		t.Fatalf("complete subtitle = %q", got)
	}
	got := WallpaperChannelSubtitle(1, true)
	if !strings.Contains(got, "1 wallpaper") || !strings.Contains(got, "couldn't be read") {
		t.Fatalf("incomplete subtitle = %q; an unreadable catalog must not pass for a complete list", got)
	}
}

func TestWallpaperRotationSubtitleReportsOnlyTheObservedSchedule(t *testing.T) {
	for _, tc := range []struct {
		name string
		view WallpaperRotationView
		want string
	}{
		{"off names the channel on screen", WallpaperRotationView{}, "Change to the next picture in Bluefin once a day."},
		{"scheduled names its channel and next change", WallpaperRotationView{On: true, ChannelName: "Bazzite", Scheduled: true, Next: "Mon 2 Nov 00:00"}, "Rotating through Bazzite daily · Next change Mon 2 Nov 00:00"},
		{"scheduled without a next time", WallpaperRotationView{On: true, ChannelName: "Bazzite", Scheduled: true}, "Rotating through Bazzite daily."},
		{"installed but not running", WallpaperRotationView{On: true, ChannelName: "Bazzite"}, "isn't scheduled"},
		{"last change failed", WallpaperRotationView{On: true, Scheduled: true, LastRunFailed: true}, "last wallpaper change failed"},
		{"check failed", WallpaperRotationView{On: true, Scheduled: true, CheckFailed: true}, "Couldn't check wallpaper rotation"},
	} {
		got := WallpaperRotationSubtitle(tc.view, "Bluefin")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: subtitle %q lacks %q", tc.name, got, tc.want)
		}
	}
}

func TestWallpaperRotationFailureIsARetry(t *testing.T) {
	for _, enable := range []bool{true, false} {
		if msg := WallpaperRotationFailure(enable); !strings.Contains(msg, "Try again") {
			t.Errorf("failure toast %q must offer a retry", msg)
		}
	}
}
