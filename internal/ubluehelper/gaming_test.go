package ubluehelper

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
)

func TestGamingSwitchParsesOnlyEnableDisableWords(t *testing.T) {
	for _, test := range []struct {
		args []string
		want Invocation
	}{
		{[]string{"gaming-switch", "enable"}, Invocation{Command: CommandGamingSwitch, Gaming: true}},
		{[]string{"gaming-switch", "disable"}, Invocation{Command: CommandGamingSwitch}},
		{[]string{"gaming-switch", "enable", "--dry-run"}, Invocation{Command: CommandGamingSwitch, Gaming: true, DryRun: true}},
		{[]string{"gaming-switch", "disable", "--dry-run"}, Invocation{Command: CommandGamingSwitch, DryRun: true}},
	} {
		t.Run(test.args[1], func(t *testing.T) {
			got, err := ParseInvocation(test.args)
			if err != nil || got != test.want {
				t.Fatalf("ParseInvocation(%v) = %+v, %v; want %+v, nil", test.args, got, err, test.want)
			}
			if !got.UsesChannelTable() {
				t.Fatal("gaming switch does not require the authoritative channel table")
			}
		})
	}
}

func TestGamingSwitchRejectsCallerControlledTargetsAndFlags(t *testing.T) {
	for _, args := range [][]string{
		{"gaming-switch"},
		{"gaming-switch", ""},
		{"gaming-switch", "true"},
		{"gaming-switch", "false"},
		{"gaming-switch", "ENABLE"},
		{"gaming-switch", "gaming"},
		{"gaming-switch", "ghcr.io/evil/image:stable"},
		{"gaming-switch", "--dry-run"},
		{"gaming-switch", "--dry-run", "enable"},
		{"gaming-switch", "enable", "--apply"},
		{"gaming-switch", "disable", "--force"},
		{"gaming-switch", "enable", "--dry-run", "extra"},
		{"gaming-switch", "disable", "--dry-run", "--dry-run"},
	} {
		if got, err := ParseInvocation(args); err == nil {
			t.Errorf("ParseInvocation(%v) = %+v, nil; want rejection", args, got)
		}
	}
}

func TestGamingSwitchDerivesEnforcedImageWithoutRestart(t *testing.T) {
	for _, test := range []struct {
		name    string
		info    imageinfo.Info
		enabled bool
		want    string
	}{
		{
			name:    "enable standard stable",
			info:    imageinfo.Info{Name: "dakota", Ref: "docker://ghcr.io/projectbluefin/dakota:stable", Tag: "stable"},
			enabled: true,
			want:    "ghcr.io/projectbluefin/dakota-gaming:stable",
		},
		{
			name:    "enable nvidia testing",
			info:    imageinfo.Info{Name: "dakota-nvidia", Ref: "docker://ghcr.io/projectbluefin/dakota-nvidia:testing", Tag: "testing"},
			enabled: true,
			want:    "ghcr.io/projectbluefin/dakota-nvidia-gaming:testing",
		},
		{
			name: "disable standard next",
			info: imageinfo.Info{Name: "dakota-gaming", Ref: "docker://ghcr.io/projectbluefin/dakota-gaming:next", Tag: "next", Flavor: "gaming"},
			want: "ghcr.io/projectbluefin/dakota:next",
		},
		{
			name: "disable nvidia btw",
			info: imageinfo.Info{Name: "dakota-nvidia-gaming", Ref: "docker://ghcr.io/projectbluefin/dakota-nvidia-gaming:btw", Tag: "btw", Flavor: "gaming"},
			want: "ghcr.io/projectbluefin/dakota-nvidia:btw",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := GamingSwitchArgs(test.info, test.enabled)
			want := []string{"switch", "--enforce-container-sigpolicy", test.want}
			if !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("GamingSwitchArgs(%+v, %t) = %v, %t; want %v, true", test.info, test.enabled, got, ok, want)
			}
		})
	}
}

func TestGamingSwitchRefusesUnknownPinnedAndUnpublishedImages(t *testing.T) {
	for _, test := range []struct {
		ref     string
		tag     string
		enabled bool
	}{
		{"ghcr.io/evil/dakota", "stable", true},
		{"ghcr.io/projectbluefin/dakota", "latest", true},
		{"ghcr.io/projectbluefin/dakota", "stable-20261101", true},
		{"ghcr.io/projectbluefin/dakota", "stable.20261101", true},
		{"ghcr.io/projectbluefin/dakota", "unknown", true},
		{"ghcr.io/projectbluefin/dakota-handheld", "stable", true},
		{"ghcr.io/projectbluefin/dakota-nvidia-open", "stable", true},
		{"ghcr.io/projectbluefin/dakota-nvidia-open-gaming", "stable", false},
		{"ghcr.io/projectbluefin/dakota-gaming", "stable", true},
		{"ghcr.io/projectbluefin/dakota", "stable", false},
		{"ghcr.io/ublue-os/bluefin", "stable", true},
		{"", "stable", true},
	} {
		info := imageinfo.Info{Ref: "docker://" + test.ref, Tag: test.tag}
		if args, ok := GamingSwitchArgs(info, test.enabled); ok || args != nil {
			t.Errorf("GamingSwitchArgs(%+v, %t) = %v, %t; want nil, false", info, test.enabled, args, ok)
		}
	}
}

func TestGamingSwitchGetsImageDownloadBudget(t *testing.T) {
	if !slices.Contains(SupportedCommands(), "gaming-switch") {
		t.Fatal("gaming-switch is absent from the accepted helper command surface")
	}
	if got := Timeout("gaming-switch"); got < 30*time.Minute {
		t.Errorf("gaming-switch timeout = %v; want at least the 30-minute image download budget", got)
	}
}
