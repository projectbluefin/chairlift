package pageview

import (
	"errors"
	"strings"
	"testing"
)

func TestWallpaperSetupDistinguishesReadinessAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		installed bool
		err       error
		want      string
	}{
		{"not configured", false, nil, "Valhalla slideshow and extra Bluefin wallpapers."},
		{"verified ready", true, nil, "Valhalla slideshow and extra Bluefin wallpapers. (Installed)"},
		{"read failed", false, errors.New("sandbox read denied"), "Couldn't check setup: sandbox read denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WallpaperSetupRow(tc.installed, tc.err); got.Title != "More Dinosaurs" || got.Subtitle != tc.want {
				t.Fatalf("WallpaperSetupRow() = %+v, want subtitle %q", got, tc.want)
			}
		})
	}
}

func TestWallpaperSetupResultNeverTurnsFailureOrPreviewIntoSuccess(t *testing.T) {
	for _, preview := range []bool{false, true} {
		got := WallpaperSetupResult(errors.New("writing manual-folders: access denied"), preview)
		if !strings.Contains(got, "writing manual-folders: access denied") || strings.Contains(got, "installed") {
			t.Fatalf("failure result = %q, want cause and no success", got)
		}
	}
	if got := WallpaperSetupResult(nil, true); !strings.Contains(got, "[DRY-RUN]") || !strings.Contains(got, "no changes made") {
		t.Fatalf("preview result = %q", got)
	}
	if got := WallpaperSetupResult(nil, false); got != "More Dinosaurs installed and configured. Open Valhalla to use it." {
		t.Fatalf("live result = %q", got)
	}
}

func TestWallpaperPackagesDescribeTheAppWithoutSourceJargon(t *testing.T) {
	rows := WallpaperPackages()
	if len(rows) != 2 || rows[0].Title != "Valhalla" || rows[0].Subtitle != "Wallpaper slideshow app" {
		t.Fatalf("WallpaperPackages() = %+v", rows)
	}
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Title+row.Subtitle), "flatpark") {
			t.Fatalf("wallpaper disclosure contains source jargon: %+v", row)
		}
	}
}
