// Package config provides YAML configuration loading for ChairLift
package config

import (
	"errors"
	"io/fs"
	"log"
	"os"
)

// Config represents the application configuration
type Config struct {
	SystemPage       PageConfig `yaml:"system_page"`
	UpdatesPage      PageConfig `yaml:"updates_page"`
	ApplicationsPage PageConfig `yaml:"applications_page"`
	MaintenancePage  PageConfig `yaml:"maintenance_page"`
	FeaturesPage     PageConfig `yaml:"features_page"`
	HelpPage         PageConfig `yaml:"help_page"`
}

// PageConfig represents configuration for a single page
type PageConfig map[string]GroupConfig

// GroupConfig represents configuration for a preference group
type GroupConfig struct {
	Enabled      bool           `yaml:"enabled"`
	AppID        string         `yaml:"app_id,omitempty"`
	Actions      []ActionConfig `yaml:"actions,omitempty"`
	Website      string         `yaml:"website,omitempty"`
	Issues       string         `yaml:"issues,omitempty"`
	Chat         string         `yaml:"chat,omitempty"`
	BundlesPaths []string       `yaml:"bundles_paths,omitempty"`
	// AIImages overrides the local-AI container image per GPU vendor
	// ("nvidia", "amd", "intel", "none"). It lives in the ordinary config
	// file rather than the root-only channels.yml because the AI stack
	// crosses no privilege boundary: the container runs rootless in the
	// invoking account, so a user pointing it at their own image can do
	// nothing they could not do by running podman directly.
	AIImages map[string]string `yaml:"ai_images,omitempty"`
	// AIModel overrides the model the stack serves.
	AIModel string `yaml:"ai_model,omitempty"`
}

// ActionConfig represents a configurable action
type ActionConfig struct {
	Title  string `yaml:"title"`
	Script string `yaml:"script"`
	Sudo   bool   `yaml:"sudo"`
}

// rawConfig mirrors Config for YAML parsing, but every optional field is a
// pointer so yaml.v3 can distinguish "key omitted" (nil) from "key present,
// possibly with the zero value" (non-nil). It is never exposed outside this
// file; loadFromPath merges it onto defaultConfig() to produce the *Config
// callers see.
type rawConfig struct {
	SystemPage       rawPageConfig `yaml:"system_page"`
	UpdatesPage      rawPageConfig `yaml:"updates_page"`
	ApplicationsPage rawPageConfig `yaml:"applications_page"`
	MaintenancePage  rawPageConfig `yaml:"maintenance_page"`
	FeaturesPage     rawPageConfig `yaml:"features_page"`
	HelpPage         rawPageConfig `yaml:"help_page"`
}

// rawPageConfig mirrors PageConfig for YAML parsing.
type rawPageConfig map[string]rawGroupConfig

// rawGroupConfig mirrors GroupConfig for YAML parsing. A nil field means the
// key was absent (or explicitly null) in the source file and the merge keeps
// defaultConfig()'s value; a non-nil field, including a pointer to an empty
// string/slice, means the file set that field explicitly and it replaces the
// default outright.
type rawGroupConfig struct {
	Enabled      *bool              `yaml:"enabled"`
	AppID        *string            `yaml:"app_id"`
	Actions      *[]ActionConfig    `yaml:"actions"`
	Website      *string            `yaml:"website"`
	Issues       *string            `yaml:"issues"`
	Chat         *string            `yaml:"chat"`
	BundlesPaths *[]string          `yaml:"bundles_paths"`
	AIImages     *map[string]string `yaml:"ai_images"`
	AIModel      *string            `yaml:"ai_model"`
}

// trustedConfigPaths are the fixed administrator- and package-owned candidates
// permitted to define sudo actions. Membership in this list — not a filesystem
// lookup performed after the file has been read — decides a search candidate's
// provenance.
var trustedConfigPaths = []string{
	"/etc/chairlift/config.yml",
	"/usr/share/chairlift/config.yml",
}

// untrustedConfigPaths are the relative candidates resolved against the
// executable directory or the working directory. config.dev.yml is a
// repository-only development override that keeps source checkouts usable
// while package/install paths ship the privileged default at
// /usr/share/chairlift/config.yml. Neither may define sudo actions.
var untrustedConfigPaths = []string{
	"config.dev.yml",
	"config.yml",
}

// configPaths are the locations to search for the config file, trusted
// candidates first.
var configPaths = append(append([]string{}, trustedConfigPaths...), untrustedConfigPaths...)

// readFile is an injection seam for deterministic read-failure tests. Its
// production value is always os.ReadFile.
var readFile = os.ReadFile

// Load loads the first existing configuration candidate. A missing candidate
// continues the documented search order; any other failure in the first
// existing candidate is authoritative and returns a fail-closed configuration
// together with the actionable error.
func Load() (*Config, *LoadError) {
	for _, candidate := range configPaths {
		// Provenance comes from which fixed candidate matched, decided
		// before the file is read, so nothing on disk can change the answer
		// afterwards.
		src := configSource{
			path:    resolveCandidatePath(candidate),
			trusted: isTrustedCandidate(candidate),
		}
		cfg, err := loadResolvedPath(src)
		if err == nil {
			log.Printf("Loaded config from %s", src.path)
			return cfg, nil
		}
		if err.Kind == KindRead && errors.Is(err, fs.ErrNotExist) && !danglingAuthoritativeSymlink(src.path) {
			continue
		}

		log.Print(err.LogMessage())
		return disabledConfig(), err
	}

	// Preserve built-in defaults only when every candidate is genuinely absent.
	log.Println("No config file found, using defaults")
	return defaultConfig(), nil
}

// loadFromPath attempts to load config from a specific path. The path is not
// one of Load()'s fixed candidates, so provenance is decided by inspecting it —
// but still before the read, so the decision cannot be swapped out underneath
// the bytes that were loaded.
func loadFromPath(path string) (*Config, *LoadError) {
	resolved := resolveCandidatePath(path)
	return loadResolvedPath(configSource{
		path:    resolved,
		trusted: isTrustedConfigPath(resolved),
	})
}

// loadResolvedPath reads and strictly validates one already-resolved
// candidate, then overlays it onto the built-in defaults. src carries the
// provenance decision made before the read; nothing here re-derives it.
func loadResolvedPath(src configSource) (*Config, *LoadError) {
	data, err := readFile(src.path)
	if err != nil {
		return nil, &LoadError{
			Path:   src.path,
			Kind:   KindRead,
			Detail: "reading configuration file",
			Err:    err,
		}
	}

	raw, loadErr := parseAndValidate(src, data)
	if loadErr != nil {
		return nil, loadErr
	}

	merged := mergeConfig(defaultConfig(), raw)
	if loadErr := validateEffectiveSudoProvenance(src, merged); loadErr != nil {
		return nil, loadErr
	}

	return merged, nil
}

// configPages returns cfg's pages in a fixed order so any whole-config walk
// visits the same pages the rest of the package knows about.
func configPages(cfg *Config) []PageConfig {
	return []PageConfig{
		cfg.SystemPage,
		cfg.UpdatesPage,
		cfg.ApplicationsPage,
		cfg.MaintenancePage,
		cfg.FeaturesPage,
		cfg.HelpPage,
	}
}

// danglingAuthoritativeSymlink reports whether path is a symlink whose target
// is missing. os.ReadFile follows the link and fails with ENOENT for such a
// link exactly as it does for an absent path, so an Lstat on the candidate is
// the only way to tell a present-but-unreadable authoritative symlink apart
// from a genuinely missing candidate. A missing directory entry returns false
// here (Lstat also fails ENOENT), so Load() still advances to the next
// candidate for it; a regular file that exists is never read as ENOENT, so
// the non-ENOENT branch already fails it closed.
func danglingAuthoritativeSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// disabledConfig retains the canonical pages, groups, and non-visibility
// defaults while forcing every known group off. It is the only configuration
// returned for an authoritative read, parse/type, or schema failure.
func disabledConfig() *Config {
	cfg := defaultConfig()
	pages := configPages(cfg)
	for _, page := range pages {
		for name, group := range page {
			group.Enabled = false
			page[name] = group
		}
	}
	return cfg
}

// mergeConfig overlays raw (a parsed config file) onto def (defaultConfig())
// page by page, returning a new *Config. Every optional field on every group
// follows the same rule: omitted in raw -> keep def's value; present in raw
// (including an explicit empty string/slice) -> use raw's value, replacing
// def's outright.
func mergeConfig(def *Config, raw *rawConfig) *Config {
	return &Config{
		SystemPage:       mergePage(def.SystemPage, raw.SystemPage),
		UpdatesPage:      mergePage(def.UpdatesPage, raw.UpdatesPage),
		ApplicationsPage: mergePage(def.ApplicationsPage, raw.ApplicationsPage),
		MaintenancePage:  mergePage(def.MaintenancePage, raw.MaintenancePage),
		FeaturesPage:     mergePage(def.FeaturesPage, raw.FeaturesPage),
		HelpPage:         mergePage(def.HelpPage, raw.HelpPage),
	}
}

// mergePage overlays raw onto def for a single page. Runtime inputs have
// already passed strict schema validation, so the unknown-group branch is
// defensive support for trusted package-internal callers only. Groups present
// in both maps are merged field by field.
func mergePage(def PageConfig, raw rawPageConfig) PageConfig {
	result := make(PageConfig, len(def))
	for name, group := range def {
		result[name] = group
	}

	for name, rawGroup := range raw {
		base, ok := def[name]
		if !ok {
			// Preserve the historical package-internal merge behavior.
			base = GroupConfig{Enabled: true}
		}
		result[name] = mergeGroup(base, rawGroup)
	}

	return result
}

// mergeGroup overlays raw onto def for a single group, field by field. Each
// assignment is guarded by the corresponding raw pointer's nil-check: nil
// means the file omitted (or explicitly nulled) that key, so def's value is
// kept; non-nil means the file set the key, so raw's value replaces def's,
// including an explicit empty string or empty slice.
func mergeGroup(def GroupConfig, raw rawGroupConfig) GroupConfig {
	result := def

	if raw.Enabled != nil {
		result.Enabled = *raw.Enabled
	}
	if raw.AppID != nil {
		result.AppID = *raw.AppID
	}
	if raw.Actions != nil {
		result.Actions = *raw.Actions
	}
	if raw.Website != nil {
		result.Website = *raw.Website
	}
	if raw.Issues != nil {
		result.Issues = *raw.Issues
	}
	if raw.Chat != nil {
		result.Chat = *raw.Chat
	}
	if raw.AIImages != nil {
		result.AIImages = *raw.AIImages
	}
	if raw.AIModel != nil {
		result.AIModel = *raw.AIModel
	}
	if raw.BundlesPaths != nil {
		result.BundlesPaths = *raw.BundlesPaths
	}

	return result
}

// defaultConfig returns the default configuration
func defaultConfig() *Config {
	return &Config{
		SystemPage: PageConfig{
			"system_info_group":  GroupConfig{Enabled: true},
			"bootc_status_group": GroupConfig{Enabled: true},
			// Which image this machine runs, and the graphics driver that
			// image carries. Both describe the system's identity, so they
			// belong beside the rest of it rather than among the features
			// you switch on.
			"channel_group": GroupConfig{Enabled: true},
			"health_group": GroupConfig{
				Enabled: true,
				AppID:   "io.missioncenter.MissionCenter",
			},
		},
		UpdatesPage: PageConfig{
			// Update All leads the page: it is the one action most users
			// need, with the per-provider groups below it for the cases it
			// does not cover.
			"update_all_group":        GroupConfig{Enabled: true},
			"bootc_updates_group":     GroupConfig{Enabled: true},
			"sysupdate_updates_group": GroupConfig{Enabled: true},
			"flatpak_updates_group":   GroupConfig{Enabled: true},
			"brew_updates_group":      GroupConfig{Enabled: true},
			"brew_trust_group":        GroupConfig{Enabled: true},
		},
		ApplicationsPage: PageConfig{
			"applications_installed_group": GroupConfig{
				Enabled: true,
				AppID:   "io.github.kolunmi.Bazaar",
			},
			"flatpak_user_group":   GroupConfig{Enabled: true},
			"flatpak_system_group": GroupConfig{Enabled: true},
			"brew_group":           GroupConfig{Enabled: true},
			"brew_search_group":    GroupConfig{Enabled: true},
			"brew_bundles_group": GroupConfig{
				Enabled:      true,
				BundlesPaths: []string{"/usr/share/snow/bundles"},
			},
		},
		MaintenancePage: PageConfig{
			"maintenance_cleanup_group": GroupConfig{
				Enabled: false,
				Actions: []ActionConfig{
					{
						Title:  "Clean Up Boot Old Entries",
						Script: "/usr/libexec/bls-gc",
						Sudo:   true,
					},
				},
			},
			"maintenance_brew_group":         GroupConfig{Enabled: true},
			"maintenance_flatpak_group":      GroupConfig{Enabled: true},
			"maintenance_optimization_group": GroupConfig{Enabled: true},
			// Powerwash and Factory Reset. Disabled by default, the same as
			// maintenance_cleanup_group: both are irreversible, so an
			// administrator opts in explicitly rather than exposing them on
			// every install.
			"reset_group": GroupConfig{Enabled: false},
		},
		FeaturesPage: PageConfig{
			"features_group": GroupConfig{Enabled: true},
			// Capabilities you turn on. Both hide themselves when
			// internal/ublue reports no /usr/share/ublue-os/image-info.json,
			// which is every non-Bluefin host including Snow Linux.
			"dx_group":              GroupConfig{Enabled: true},
			"gaming_group":          GroupConfig{Enabled: true},
			"ai_group":              GroupConfig{Enabled: true},
			"troubleshooting_group": GroupConfig{Enabled: true},
		},
		HelpPage: PageConfig{
			"help_resources_group": GroupConfig{
				Enabled: true,
				Website: "https://github.com/frostyard/snosi",
				Issues:  "https://github.com/frostyard/snosi/issues",
				Chat:    "https://github.com/frostyard/snosi/discussions",
			},
		},
	}
}

// IsGroupEnabled checks if a preference group is enabled
func (c *Config) IsGroupEnabled(pageName, groupName string) bool {
	var page PageConfig
	switch pageName {
	case "system_page":
		page = c.SystemPage
	case "updates_page":
		page = c.UpdatesPage
	case "applications_page":
		page = c.ApplicationsPage
	case "maintenance_page":
		page = c.MaintenancePage
	case "features_page":
		page = c.FeaturesPage
	case "help_page":
		page = c.HelpPage
	default:
		return true
	}

	group, ok := page[groupName]
	if !ok {
		return true // Default to enabled if not specified
	}
	return group.Enabled
}

// GetGroupConfig returns the configuration for a specific group
func (c *Config) GetGroupConfig(pageName, groupName string) *GroupConfig {
	var page PageConfig
	switch pageName {
	case "system_page":
		page = c.SystemPage
	case "updates_page":
		page = c.UpdatesPage
	case "applications_page":
		page = c.ApplicationsPage
	case "maintenance_page":
		page = c.MaintenancePage
	case "features_page":
		page = c.FeaturesPage
	case "help_page":
		page = c.HelpPage
	default:
		return nil
	}

	group, ok := page[groupName]
	if !ok {
		return nil
	}
	return &group
}
