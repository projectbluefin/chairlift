package config

import (
	"testing"
)

func TestWallpapersGroupRespectsConfiguration(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		value := "false"
		if enabled {
			value = "true"
		}
		t.Run(value, func(t *testing.T) {
			cfg, err := loadFromPath(writeConfigFile(t, "livery_page:\n  wallpapers_group:\n    enabled: "+value+"\n"))
			if err != nil {
				t.Fatalf("loadFromPath: %v", err)
			}
			if got := cfg.IsGroupEnabled("livery_page", "wallpapers_group"); got != enabled {
				t.Fatalf("wallpapers_group enabled = %t, want %t", got, enabled)
			}
			if !cfg.IsGroupEnabled("livery_page", "account_group") {
				t.Fatal("wallpaper configuration disabled Profile Picture")
			}
		})
	}
}

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
