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

// troubleshooting_group (Ask Bluefin's Goose row) moved from Help to the Agents page. An
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
