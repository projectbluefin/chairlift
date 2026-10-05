package config

import (
	"io/fs"
	"reflect"
	"testing"
)

func TestLegacySystemPageLoadsFromInstalledCandidates(t *testing.T) {
	for _, path := range trustedConfigPaths {
		t.Run(path, func(t *testing.T) {
			withConfigPaths(t, trustedConfigPaths)
			var reads []string
			withReadFile(t, func(p string) ([]byte, error) {
				reads = append(reads, p)
				if p != path {
					return nil, fs.ErrNotExist
				}
				return []byte(`system_page:
  system_info_group: {enabled: true}
  health_group: {enabled: true, app_id: io.missioncenter.MissionCenter}
  bootc_status_group: {enabled: false}
  channel_group: {enabled: false}
applications_page:
  brew_group: {enabled: false}
`), nil
			})
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			want := defaultConfig()
			for _, group := range []string{"bootc_status_group", "channel_group"} {
				g := want.UpdatesPage[group]
				g.Enabled = false
				want.UpdatesPage[group] = g
			}
			g := want.ApplicationsPage["brew_group"]
			g.Enabled = false
			want.ApplicationsPage["brew_group"] = g
			if !reflect.DeepEqual(cfg, want) {
				t.Fatalf("loaded config = %+v, want %+v", cfg, want)
			}
			if reads[len(reads)-1] != path {
				t.Fatalf("continued past authoritative file: %v", reads)
			}
		})
	}
}

func TestLegacySystemPageCurrentFieldsWin(t *testing.T) {
	for _, current := range []string{
		"updates_page: null",
		"updates_page: {channel_group: null}",
		"updates_page: {channel_group: {enabled: null}}",
		"updates_page: {channel_group: {enabled: false}}",
		"updates_page: {channel_group: {enabled: true}}",
		"updates_page: {channel_group: {website: current}}",
	} {
		t.Run(current, func(t *testing.T) {
			data := "system_page: {channel_group: {enabled: false, website: legacy}}\n" + current
			cfg, err := loadFromPath(writeConfigFile(t, data))
			if err != nil {
				t.Fatal(err)
			}
			group := cfg.UpdatesPage["channel_group"]
			wantEnabled := current == "updates_page: {channel_group: {enabled: true}}"
			wantWebsite := "legacy"
			if current == "updates_page: {channel_group: {website: current}}" {
				wantWebsite = "current"
			}
			if group.Enabled != wantEnabled || group.Website != wantWebsite {
				t.Fatalf("migrated group = %+v", group)
			}
		})
	}
}

func TestLegacySystemPageCurrentDisableWinsRegardlessOfOrder(t *testing.T) {
	legacy := "system_page: {channel_group: {enabled: true}}\n"
	current := "updates_page: {channel_group: {enabled: false}}\n"
	for _, data := range []string{legacy + current, current + legacy} {
		cfg, err := loadFromPath(writeConfigFile(t, data))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.UpdatesPage["channel_group"].Enabled {
			t.Fatal("legacy enabled value overrode the current explicit disable")
		}
	}
}

func TestLegacySystemPageAliasesAndNull(t *testing.T) {
	for _, data := range []string{
		"system_page: null",
		"system_page: {channel_group: &group {enabled: false}, bootc_status_group: {<<: *group}}",
		"system_page: {channel_group: null}",
	} {
		cfg, err := loadFromPath(writeConfigFile(t, data))
		if err != nil {
			t.Fatal(err)
		}
		want := data != "system_page: {channel_group: &group {enabled: false}, bootc_status_group: {<<: *group}}"
		if cfg.UpdatesPage["channel_group"].Enabled != want || cfg.UpdatesPage["bootc_status_group"].Enabled != want {
			t.Fatalf("unexpected migration for %s: %+v", data, cfg.UpdatesPage)
		}
	}
}

func TestLegacySystemPageInvalidInputStillFailsClosed(t *testing.T) {
	for _, data := range []string{
		"system_pag: {}",
		"system_page: []",
		"system_page: {health_group: []}",
		"system_page: {system_info_group: {unknown: true}}",
		"system_page: {health_group: {actions: [{unknown: true}]}}",
		"system_page: {channel_group: {enabled: false}}\nupdates_page: {channel_group: {enabled: []}}",
		"system_page: {unknown_group: {enabled: true}}",
		"system_page: {channel_group: {enabeld: false}}",
		"system_page: {health_group: {enabled: []}}",
		"system_page: {channel_group: {enabled: false}, channel_group: {enabled: true}}",
		"system_page: {channel_group: {enabled: []}}\nupdates_page: {channel_group: {enabled: true}}",
		"system_page: {health_group: {actions: [{title: test, script: /bin/test, sudo: true}]}}",
		"system_page: {channel_group: {actions: [{title: test, script: /bin/test, sudo: true}]}}",
		"system_page: {channel_group: &cycle {<<: *cycle}}",
	} {
		t.Run(data, func(t *testing.T) {
			path := writeConfigFile(t, data)
			withConfigPaths(t, []string{path, "must-not-read.yml"})
			cfg, err := Load()
			if err == nil || err.Path != path {
				t.Fatalf("error = %v, want authoritative failure", err)
			}
			assertAllKnownGroupsDisabled(t, cfg)
		})
	}
}

func TestLegacyMaintenanceGroupsIgnoredWithoutSchemaError(t *testing.T) {
	data := `maintenance_page:
  maintenance_brew_group:
    enabled: true
  maintenance_flatpak_group:
    enabled: true
  maintenance_optimization_group:
    enabled: true
  maintenance_freespace_group:
    enabled: false
`
	cfg, err := loadFromPath(writeConfigFile(t, data))
	if err != nil {
		t.Fatalf("loadFromPath failed: %v", err)
	}
	for _, retired := range legacyMaintenanceGroups {
		if _, present := cfg.MaintenancePage[retired]; present {
			t.Errorf("legacy maintenance group %q reached runtime Config", retired)
		}
	}
	if cfg.MaintenancePage["maintenance_freespace_group"].Enabled {
		t.Fatal("maintenance_freespace_group was not set to false")
	}
}

// TestLegacyMaintenanceGroupsUnknownFieldsStillFailClosed mirrors the
// TestLegacySystemPageInvalidInputStillFailsClosed coverage: an undeclared
// field under a retired group name must not be silently accepted, and a
// typo of a retired name must still be rejected so compatibility cannot
// conceal broken configurations.
func TestLegacyMaintenanceGroupsUnknownFieldsStillFailClosed(t *testing.T) {
	for _, data := range []string{
		"maintenance_page: {maintenance_brew_grup: {enabled: true}}",
		"maintenance_page: {maintenance_brew_group: {unknown: true}}",
		"maintenance_page: {maintenance_brew_group: []}",
		"maintenance_page: {maintenance_flatpak_group: {enabled: []}}",
		"maintenance_page: {maintenance_optimization_group: {actions: [{title: t, script: /bin/true, sudo: true}]}}",
		"maintenance_page: {maintenance_brew_group: 42}",
		"maintenance_page: {maintenance_brew_group: {enabled: false}, maintenance_brew_group: {enabled: true}}",
		"maintenance_page: {maintenance_brew_group: &cycle {<<: *cycle}}",
	} {
		t.Run(data, func(t *testing.T) {
			path := writeConfigFile(t, data)
			withConfigPaths(t, []string{path, "must-not-read.yml"})
			cfg, err := Load()
			if err == nil || err.Path != path {
				t.Fatalf("error = %v, want authoritative failure", err)
			}
			assertAllKnownGroupsDisabled(t, cfg)
		})
	}
}

// TestLegacyMaintenanceGroupsOnlyAreStripped ensures retired groups are
// removed but canonical neighbors (e.g. maintenance_freespace_group) keep
// their enabled state and order.
func TestLegacyMaintenanceGroupsOnlyAreStripped(t *testing.T) {
	data := `maintenance_page:
  maintenance_cleanup_group:
    enabled: true
  maintenance_brew_group:
    enabled: true
  maintenance_freespace_group:
    enabled: true
`
	cfg, err := loadFromPath(writeConfigFile(t, data))
	if err != nil {
		t.Fatalf("loadFromPath failed: %v", err)
	}
	if !cfg.MaintenancePage["maintenance_cleanup_group"].Enabled {
		t.Fatal("maintenance_cleanup_group should remain enabled")
	}
	if !cfg.MaintenancePage["maintenance_freespace_group"].Enabled {
		t.Fatal("maintenance_freespace_group should remain enabled")
	}
	for _, retired := range legacyMaintenanceGroups {
		if _, present := cfg.MaintenancePage[retired]; present {
			t.Errorf("legacy maintenance group %q reached runtime Config", retired)
		}
	}
}

// TestLegacyUpdatesGroupsIgnoredWithoutSchemaError mirrors the maintenance
// coverage: pre-26.09 host files carry update_all_group and
// sysupdate_updates_group on updates_page; the validator treats them as
// known names, the strip pass removes them before runtime decoding.
func TestLegacyUpdatesGroupsIgnoredWithoutSchemaError(t *testing.T) {
	data := `updates_page:
  update_all_group:
    enabled: true
  sysupdate_updates_group:
    enabled: true
  automatic_updates_group:
    enabled: false
`
	cfg, err := loadFromPath(writeConfigFile(t, data))
	if err != nil {
		t.Fatalf("loadFromPath failed: %v", err)
	}
	for _, retired := range legacyUpdatesGroups {
		if _, present := cfg.UpdatesPage[retired]; present {
			t.Errorf("legacy updates group %q reached runtime Config", retired)
		}
	}
	if cfg.UpdatesPage["automatic_updates_group"].Enabled {
		t.Fatal("automatic_updates_group was not set to false")
	}
}

// TestLegacyUpdatesGroupsUnknownFieldsStillFailClosed verifies that
// unknown fields under a retired updates_page group, and a typo of a
// retired name, still fail closed — the compatibility rule must not
// conceal broken configurations.
func TestLegacyUpdatesGroupsUnknownFieldsStillFailClosed(t *testing.T) {
	for _, data := range []string{
		"updates_page: {update_all_grup: {enabled: true}}",
		"updates_page: {update_all_group: {unknown: true}}",
		"updates_page: {update_all_group: []}",
		"updates_page: {sysupdate_updates_group: {enabled: []}}",
		"updates_page: {update_all_group: 42}",
	} {
		t.Run(data, func(t *testing.T) {
			path := writeConfigFile(t, data)
			withConfigPaths(t, []string{path, "must-not-read.yml"})
			cfg, err := Load()
			if err == nil || err.Path != path {
				t.Fatalf("error = %v, want authoritative failure", err)
			}
			assertAllKnownGroupsDisabled(t, cfg)
		})
	}
}

// TestLegacyUpdatesGroupsOnlyAreStripped ensures retired groups are
// removed but canonical neighbors (e.g. automatic_updates_group) keep
// their enabled state and order.
func TestLegacyUpdatesGroupsOnlyAreStripped(t *testing.T) {
	data := `updates_page:
  automatic_updates_group:
    enabled: true
  update_all_group:
    enabled: true
  bootc_updates_group:
    enabled: true
`
	cfg, err := loadFromPath(writeConfigFile(t, data))
	if err != nil {
		t.Fatalf("loadFromPath failed: %v", err)
	}
	if !cfg.UpdatesPage["automatic_updates_group"].Enabled {
		t.Fatal("automatic_updates_group should remain enabled")
	}
	if !cfg.UpdatesPage["bootc_updates_group"].Enabled {
		t.Fatal("bootc_updates_group should remain enabled")
	}
	for _, retired := range legacyUpdatesGroups {
		if _, present := cfg.UpdatesPage[retired]; present {
			t.Errorf("legacy updates group %q reached runtime Config", retired)
		}
	}
}

// TestLegacyFeaturesGroupsIgnoredWithoutSchemaError mirrors the maintenance
// coverage for the pre-26.09 ai_group under features_page: known name,
// stripped before runtime, canonical neighbors preserved.
func TestLegacyFeaturesGroupsIgnoredWithoutSchemaError(t *testing.T) {
	data := `features_page:
  ai_group:
    enabled: true
  dx_group:
    enabled: false
`
	cfg, err := loadFromPath(writeConfigFile(t, data))
	if err != nil {
		t.Fatalf("loadFromPath failed: %v", err)
	}
	for _, retired := range legacyFeaturesGroups {
		if _, present := cfg.FeaturesPage[retired]; present {
			t.Errorf("legacy features group %q reached runtime Config", retired)
		}
	}
	if cfg.FeaturesPage["dx_group"].Enabled {
		t.Fatal("dx_group was not set to false")
	}
}

// TestLegacyFeaturesGroupsUnknownFieldsStillFailClosed verifies that
// unknown fields under ai_group, and a typo of the retired name, still
// fail closed.
func TestLegacyFeaturesGroupsUnknownFieldsStillFailClosed(t *testing.T) {
	for _, data := range []string{
		"features_page: {ai_grup: {enabled: true}}",
		"features_page: {ai_group: {unknown: true}}",
		"features_page: {ai_group: []}",
		"features_page: {ai_group: {enabled: []}}",
		"features_page: {ai_group: 42}",
	} {
		t.Run(data, func(t *testing.T) {
			path := writeConfigFile(t, data)
			withConfigPaths(t, []string{path, "must-not-read.yml"})
			cfg, err := Load()
			if err == nil || err.Path != path {
				t.Fatalf("error = %v, want authoritative failure", err)
			}
			assertAllKnownGroupsDisabled(t, cfg)
		})
	}
}

// TestLegacyFeaturesGroupsOnlyAreStripped ensures the retired ai_group
// is removed but canonical neighbors keep their enabled state and order.
func TestLegacyFeaturesGroupsOnlyAreStripped(t *testing.T) {
	data := `features_page:
  features_group:
    enabled: true
  ai_group:
    enabled: true
  dx_group:
    enabled: true
`
	cfg, err := loadFromPath(writeConfigFile(t, data))
	if err != nil {
		t.Fatalf("loadFromPath failed: %v", err)
	}
	if !cfg.FeaturesPage["features_group"].Enabled {
		t.Fatal("features_group should remain enabled")
	}
	if !cfg.FeaturesPage["dx_group"].Enabled {
		t.Fatal("dx_group should remain enabled")
	}
	for _, retired := range legacyFeaturesGroups {
		if _, present := cfg.FeaturesPage[retired]; present {
			t.Errorf("legacy features group %q reached runtime Config", retired)
		}
	}
}

// TestLegacyGroupsAcrossPagesDoNotInterfere ensures the per-page allowlist
// does not let a retired name leak into a different page: e.g.
// update_all_group inside updates_page is accepted, but update_all_group
// inside features_page is unknown and fails closed. The reverse holds for
// the other retired names too. This guards the per-page scoping against
// accidental relaxation.
func TestLegacyGroupsAcrossPagesDoNotInterfere(t *testing.T) {
	for _, data := range []string{
		"features_page: {update_all_group: {enabled: true}}",
		"features_page: {sysupdate_updates_group: {enabled: true}}",
		"features_page: {maintenance_brew_group: {enabled: true}}",
		"updates_page: {maintenance_brew_group: {enabled: true}}",
		"updates_page: {ai_group: {enabled: true}}",
		"maintenance_page: {update_all_group: {enabled: true}}",
		"maintenance_page: {ai_group: {enabled: true}}",
		"updates_page: {troubleshooting_group: {enabled: true}}",
	} {
		t.Run(data, func(t *testing.T) {
			path := writeConfigFile(t, data)
			withConfigPaths(t, []string{path, "must-not-read.yml"})
			cfg, err := Load()
			if err == nil || err.Path != path {
				t.Fatalf("error = %v, want authoritative failure", err)
			}
			assertAllKnownGroupsDisabled(t, cfg)
		})
	}
}

// TestLegacyFeaturesPageV012Shape loads the features_page layout shipped by
// v0.12.x: ai_group with its retired ai_images/ai_model fields, and
// troubleshooting_group before it moved to help_page.
func TestLegacyFeaturesPageV012Shape(t *testing.T) {
	data := `features_page:
  features_group:
    enabled: true
  ai_group:
    enabled: true
    ai_images:
      nvidia: registry.example.internal/ramalama/cuda:latest
    ai_model: ollama://qwen2.5:7b
  troubleshooting_group:
    enabled: false
help_page:
  help_resources_group:
    enabled: true
`
	cfg, err := loadFromPath(writeConfigFile(t, data))
	if err != nil {
		t.Fatalf("loadFromPath failed: %v", err)
	}
	for _, retired := range legacyFeaturesGroups {
		if _, present := cfg.FeaturesPage[retired]; present {
			t.Errorf("legacy features group %q reached runtime Config", retired)
		}
	}
	group, present := cfg.AgentsPage["troubleshooting_group"]
	if !present {
		t.Fatal("troubleshooting_group missing from agents_page")
	}
	if group.Enabled {
		t.Fatal("features_page troubleshooting_group opt-out was not migrated to agents_page")
	}
	if _, stale := cfg.HelpPage["troubleshooting_group"]; stale {
		t.Error("the migrated group stopped at help_page")
	}
	if !cfg.HelpPage["help_resources_group"].Enabled {
		t.Fatal("help_resources_group should remain enabled")
	}
}

// TestLegacyTroubleshootingGroupCurrentFieldsWin verifies the newest home
// wins along features_page → help_page → agents_page.
func TestLegacyTroubleshootingGroupCurrentFieldsWin(t *testing.T) {
	for _, tt := range []struct {
		data string
		want bool
	}{
		{"features_page: {troubleshooting_group: {enabled: false}}\nhelp_page: {troubleshooting_group: {enabled: true}}", true},
		{"features_page: {troubleshooting_group: {enabled: true}}\nhelp_page: {troubleshooting_group: {enabled: false}}", false},
		{"features_page: {troubleshooting_group: {enabled: false}}\nhelp_page: {troubleshooting_group: {enabled: false}}\nagents_page: {troubleshooting_group: {enabled: true}}", true},
	} {
		t.Run(tt.data, func(t *testing.T) {
			cfg, err := loadFromPath(writeConfigFile(t, tt.data))
			if err != nil {
				t.Fatalf("loadFromPath failed: %v", err)
			}
			if got := cfg.IsGroupEnabled("agents_page", "troubleshooting_group"); got != tt.want {
				t.Errorf("agents_page.troubleshooting_group enabled = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLegacyAIGroupFieldsStillFailClosed verifies the retired ai_group
// fields are type-checked and are not accepted on any other group.
func TestLegacyAIGroupFieldsStillFailClosed(t *testing.T) {
	for _, data := range []string{
		"features_page: {ai_group: {ai_model: [a]}}",
		"features_page: {ai_group: {ai_images: [a]}}",
		"features_page: {ai_group: {ai_images: {nvidia: [a]}}}",
		"features_page: {dx_group: {ai_model: x}}",
		"features_page: {troubleshooting_group: {ai_model: x}}",
		"agents_page: {ai_group: {ai_model: x}}",
	} {
		t.Run(data, func(t *testing.T) {
			path := writeConfigFile(t, data)
			withConfigPaths(t, []string{path, "must-not-read.yml"})
			cfg, err := Load()
			if err == nil || err.Path != path {
				t.Fatalf("error = %v, want authoritative failure", err)
			}
			assertAllKnownGroupsDisabled(t, cfg)
		})
	}
}

// Troubleshooting moved from Help to the Agents page. An
// administrator file that disabled it at the old address must keep it
// disabled, and a current setting must win over the old one.
func TestTroubleshootingGroupMovedFromHelp(t *testing.T) {
	tests := []struct {
		data string
		want bool
	}{
		{"help_page: {troubleshooting_group: {enabled: false}}", false},
		{"help_page: {troubleshooting_group: {enabled: false}}\nagents_page: {troubleshooting_group: {enabled: true}}", true},
		{"agents_page: {troubleshooting_group: {enabled: false}}\nhelp_page: {troubleshooting_group: {enabled: true}}", false},
		{"help_page: {troubleshooting_group: null}", true},
	}
	for _, tt := range tests {
		t.Run(tt.data, func(t *testing.T) {
			cfg, err := loadFromPath(writeConfigFile(t, tt.data))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.IsGroupEnabled("agents_page", "troubleshooting_group"); got != tt.want {
				t.Errorf("agents_page.troubleshooting_group enabled = %v, want %v", got, tt.want)
			}
			if _, stale := cfg.HelpPage["troubleshooting_group"]; stale {
				t.Error("the moved group is still present under help_page")
			}
		})
	}
}

func TestTroubleshootingGroupAtItsOldAddressStillFailsClosed(t *testing.T) {
	for _, data := range []string{
		"help_page: {troubleshooting_group: {enabeld: false}}",
		"help_page: {troubleshooting_group: []}",
		"help_page: {troubleshooting_group: {actions: [{title: t, script: /bin/t, sudo: true}]}}",
	} {
		t.Run(data, func(t *testing.T) {
			path := writeConfigFile(t, data)
			withConfigPaths(t, []string{path, "must-not-read.yml"})
			cfg, err := Load()
			if err == nil || err.Path != path {
				t.Fatalf("error = %v, want authoritative failure", err)
			}
			assertAllKnownGroupsDisabled(t, cfg)
		})
	}
}
