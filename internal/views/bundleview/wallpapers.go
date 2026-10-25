package bundleview

import "github.com/projectbluefin/chairlift/internal/homebrew"

// WallpaperCollection identifies the external artwork Brewfile owned by Livery.
// Slideshow tools retain their existing Apps owner until their own cutover.
func WallpaperCollection(name string) bool { return name == "artwork" }

// WallpaperStatus keeps check failures explicit instead of inferring absence.
func WallpaperStatus(description string, status homebrew.BundleStatus, err error) (string, bool) {
	if err != nil {
		status = homebrew.BundleIndeterminate
	}
	return description + " · " + status.DisplayName(), status != homebrew.BundleInstalled
}
