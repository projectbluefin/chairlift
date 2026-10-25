package bundleview

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/homebrew"
)

func TestWallpaperDiscoveryUsesExternalContents(t *testing.T) {
	dir := t.TempDir()
	bundles, err := homebrew.AvailableBundles([]string{dir})
	if err != nil || len(bundles) != 0 {
		t.Fatalf("missing manifest: %v, %v", bundles, err)
	}
	path := filepath.Join(dir, "artwork.Brewfile")
	if err := os.WriteFile(path, []byte("# The host's wallpapers\ncask \"host-wallpaper\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bundles, err = homebrew.AvailableBundles([]string{dir})
	if err != nil || len(bundles) != 1 {
		t.Fatalf("discovery: %v, %v", bundles, err)
	}
	if !WallpaperCollection(bundles[0].Name) || bundles[0].Path != path {
		t.Fatalf("wrong collection: %+v", bundles[0])
	}
	items, err := homebrew.BundleContents(path)
	if err != nil || len(items) != 1 {
		t.Fatalf("actual contents: %v, %v", items, err)
	}
	for _, name := range []string{"cli", "fonts", "wallpaper-slideshow", "custom"} {
		if WallpaperCollection(name) {
			t.Errorf("claimed unrelated collection %q", name)
		}
	}
}

func TestWallpaperStatusKeepsAllObservedStates(t *testing.T) {
	for _, state := range []homebrew.BundleStatus{homebrew.BundleInstalled, homebrew.BundleUpdateAvailable, homebrew.BundleNotInstalled, homebrew.BundleIndeterminate} {
		text, action := WallpaperStatus("Host collection", state, nil)
		if !strings.Contains(text, state.DisplayName()) || action != (state != homebrew.BundleInstalled) {
			t.Fatalf("%s: %s, %v", state, text, action)
		}
	}
	text, action := WallpaperStatus("Host collection", homebrew.BundleInstalled, errors.New("check failed"))
	if !strings.Contains(text, "Indeterminate") || !action {
		t.Fatalf("failed check: %s, %v", text, action)
	}
}
