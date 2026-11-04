package pageview

import (
	"fmt"
	"strings"

	"github.com/projectbluefin/chairlift/internal/valhalla"
)

const LiveryWallpapersTitle = "Wallpapers"
const LiveryWallpapersDescription = "Set up a slideshow with extra Bluefin backgrounds. Open Valhalla after setup to use it."

// WallpaperSetupRow reports only observed setup, never Flatpak presence alone.
func WallpaperSetupRow(installed bool, err error) Row {
	row := Row{Title: "More Dinosaurs", Subtitle: "Valhalla slideshow and extra Bluefin wallpapers."}
	if err != nil {
		row.Subtitle = fmt.Sprintf("Couldn't check setup: %v", err)
	} else if installed {
		row.Subtitle += " (Installed)"
	}
	return row
}

// WallpaperPackages discloses the backend's fixed payload, not a legacy Brewfile.
func WallpaperPackages() []Row {
	return []Row{
		{Title: "Valhalla", Subtitle: "Wallpaper slideshow app"},
		{Title: strings.TrimPrefix(valhalla.ExtraWallpapersCask, "ublue-os/tap/"), Subtitle: "Homebrew cask · Extra Bluefin backgrounds"},
	}
}

// WallpaperSetupResult keeps failures and previews distinct from live success.
func WallpaperSetupResult(err error, preview bool) string {
	if err != nil {
		return fmt.Sprintf("Couldn't set up More Dinosaurs: %v. Try again.", err)
	}
	if preview {
		return "[DRY-RUN] Preview: More Dinosaurs would be installed and configured — no changes made"
	}
	return "More Dinosaurs installed and configured. Open Valhalla to use it."
}
