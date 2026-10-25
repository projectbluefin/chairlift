package config

import (
	"testing"
)

func TestBrewBundleGroupConfigControlsRuntimeWiring(t *testing.T) {
	if !defaultConfig().IsGroupEnabled("applications_page", "brew_bundles_group") {
		t.Fatal("default brew_bundles_group is disabled, want enabled")
	}

	path := writeConfigFile(t, "applications_page:\n  brew_bundles_group:\n    enabled: false\n")
	cfg, loadErr := loadFromPath(path)
	if loadErr != nil {
		t.Fatalf("loadFromPath(%q): %v", path, loadErr)
	}
	if cfg.IsGroupEnabled("applications_page", "brew_bundles_group") {
		t.Fatal("explicitly disabled brew_bundles_group remains enabled")
	}

}

func TestWallpapersPolicyIsIndependent(t *testing.T) {
	cfg, err := loadFromPath(writeConfigFile(t, "livery_page:\n  wallpapers_group:\n    enabled: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsGroupEnabled("livery_page", "wallpapers_group") {
		t.Fatal("disabled wallpaper policy ignored")
	}
	for _, group := range []string{"account_group", "livery_app_grid_group", "livery_foundation_group", "livery_dock_group"} {
		if !cfg.IsGroupEnabled("livery_page", group) {
			t.Errorf("wallpaper policy disabled %s", group)
		}
	}
}
