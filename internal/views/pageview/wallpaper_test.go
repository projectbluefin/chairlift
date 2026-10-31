package pageview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/livery"
)

// withChannels points the wallpaper catalog at a fixture tree with one
// directory per channel, each holding a single image.
func withChannels(t *testing.T, ids ...string) {
	t.Helper()
	root := t.TempDir()
	for _, id := range ids {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "one.png"), []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	original := livery.WallpaperBackgroundsRoot
	livery.WallpaperBackgroundsRoot = root
	t.Cleanup(func() { livery.WallpaperBackgroundsRoot = original })
}

func TestWallpaperSelectorRowsMarksTheConfirmedChannel(t *testing.T) {
	withChannels(t, "framework", "bluefin")

	rows := WallpaperSelectorRows("framework")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].ID != "bluefin" || rows[1].ID != "framework" {
		t.Errorf("rows not in curated order: %+v", rows)
	}
	if rows[0].Selected || !rows[1].Selected {
		t.Errorf("only framework should be selected: %+v", rows)
	}
}

func TestWallpaperSelectorIsEmptyWithoutChannels(t *testing.T) {
	original := livery.WallpaperBackgroundsRoot
	livery.WallpaperBackgroundsRoot = filepath.Join(t.TempDir(), "absent")
	t.Cleanup(func() { livery.WallpaperBackgroundsRoot = original })

	if rows := WallpaperSelectorRows(""); len(rows) != 0 {
		t.Errorf("fabricated rows with no channel installed: %+v", rows)
	}
}

func TestWallpaperRotationAvailableNeedsGNOMEAndAChannel(t *testing.T) {
	for _, tc := range []struct {
		gnome, channel, want bool
	}{
		{true, true, true},
		{true, false, false},
		{false, true, false},
		{false, false, false},
	} {
		if got := WallpaperRotationAvailable(tc.gnome, tc.channel); got != tc.want {
			t.Errorf("WallpaperRotationAvailable(%v, %v) = %v, want %v", tc.gnome, tc.channel, got, tc.want)
		}
	}
}

func TestWallpaperRotationRowWarnsForASourceBuild(t *testing.T) {
	if got := WallpaperRotationRow(false).Subtitle; !strings.Contains(got, "moved or reinstalled") {
		t.Errorf("source build subtitle does not warn: %q", got)
	}
	if got := WallpaperRotationRow(true).Subtitle; strings.Contains(got, "moved or reinstalled") {
		t.Errorf("installed build subtitle warns: %q", got)
	}
}

func TestDamaskOffer(t *testing.T) {
	for _, tc := range []struct {
		installed, failed, flatpak bool
		want                       DamaskState
	}{
		{true, false, true, DamaskInstalled},
		{true, false, false, DamaskInstalled},
		{false, false, true, DamaskInstallable},
		{false, false, false, DamaskHidden},
		{false, true, true, DamaskUnknown},
		{false, true, false, DamaskHidden},
	} {
		if got := DamaskOffer(tc.installed, tc.failed, tc.flatpak); got != tc.want {
			t.Errorf("DamaskOffer(%v, %v, %v) = %v, want %v", tc.installed, tc.failed, tc.flatpak, got, tc.want)
		}
	}
}

func TestDamaskControlIsOneActionWithTwoStates(t *testing.T) {
	if DamaskActionName(true) != "Open" || DamaskActionName(false) != "Install" {
		t.Errorf("action names = %q/%q", DamaskActionName(true), DamaskActionName(false))
	}
	if DamaskRow(true).Title != DamaskRow(false).Title {
		t.Error("Damask row title changes with install state")
	}
	if DamaskRow(true).Subtitle == DamaskRow(false).Subtitle {
		t.Error("Damask row subtitle does not say which action applies")
	}
	if unknown := DamaskUnknownRow(); unknown.Title != DamaskRow(true).Title ||
		unknown.Subtitle == DamaskRow(true).Subtitle || unknown.Subtitle == DamaskRow(false).Subtitle {
		t.Errorf("unknown Damask row does not read as unknown: %+v", unknown)
	}
}

// TestWallpaperGroupVisibleOnlyWithACollectionOrDamask pins the gating: a
// host with neither the collection nor an offerable Damask never builds an
// empty group, and an installable Damask surfaces it without Homebrew.
func TestWallpaperGroupVisibleOnlyWithACollectionOrDamask(t *testing.T) {
	for _, tc := range []struct {
		collection bool
		damask     DamaskState
		want       bool
	}{
		{false, DamaskHidden, false},
		{true, DamaskHidden, true},
		{false, DamaskInstallable, true},
		{false, DamaskInstalled, true},
		{true, DamaskInstalled, true},
		{false, DamaskUnknown, true},
	} {
		if got := WallpaperGroupVisible(tc.collection, tc.damask); got != tc.want {
			t.Errorf("WallpaperGroupVisible(%v, %v) = %v, want %v", tc.collection, tc.damask, got, tc.want)
		}
	}
}
