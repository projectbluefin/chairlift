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
	AgentsPage       PageConfig `yaml:"agents_page"`
	UpdatesPage      PageConfig `yaml:"updates_page"`
	ApplicationsPage PageConfig `yaml:"applications_page"`
	MaintenancePage  PageConfig `yaml:"maintenance_page"`
	FeaturesPage     PageConfig `yaml:"features_page"`
	LiveryPage       PageConfig `yaml:"livery_page"`
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
	// InstallPulp and StageFeeds are the optional developer feed onboarding
	// steps `dx_group` may run after a confirmed Developer Mode enable. Both
	// default to false: they have side effects (a user-scope Flatpak install
	// and a file written into the user's data directory), so the same
	// conservative posture as reset_group applies — an administrator opts in
	// explicitly rather than every enable performing them. Neither is
	// consulted when Developer Mode is switched off, which is a clean no-op
	// for Pulp, the staged file, and any subscriptions already imported from
	// it.
	InstallPulp bool `yaml:"install_pulp,omitempty"`
	StageFeeds  bool `yaml:"stage_feeds,omitempty"`
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
	AgentsPage       rawPageConfig `yaml:"agents_page"`
	UpdatesPage      rawPageConfig `yaml:"updates_page"`
	ApplicationsPage rawPageConfig `yaml:"applications_page"`
	MaintenancePage  rawPageConfig `yaml:"maintenance_page"`
	FeaturesPage     rawPageConfig `yaml:"features_page"`
	LiveryPage       rawPageConfig `yaml:"livery_page"`
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
	InstallPulp  *bool              `yaml:"install_pulp"`
	StageFeeds   *bool              `yaml:"stage_feeds"`
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
		cfg.AgentsPage,
		cfg.UpdatesPage,
		cfg.ApplicationsPage,
		cfg.MaintenancePage,
		cfg.FeaturesPage,
		cfg.LiveryPage,
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
		AgentsPage:       mergePage(def.AgentsPage, raw.AgentsPage),
		UpdatesPage:      mergePage(def.UpdatesPage, raw.UpdatesPage),
		ApplicationsPage: mergePage(def.ApplicationsPage, raw.ApplicationsPage),
		MaintenancePage:  mergePage(def.MaintenancePage, raw.MaintenancePage),
		FeaturesPage:     mergePage(def.FeaturesPage, raw.FeaturesPage),
		LiveryPage:       mergePage(def.LiveryPage, raw.LiveryPage),
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
	if raw.InstallPulp != nil {
		result.InstallPulp = *raw.InstallPulp
	}
	if raw.StageFeeds != nil {
		result.StageFeeds = *raw.StageFeeds
	}

	return result
}

// defaultConfig returns the default configuration
func defaultConfig() *Config {
	return &Config{
		// Local AI runs rootless in the invoking account, so it crosses no
		// privilege boundary and needs no administrator route.
		AgentsPage: PageConfig{
			"agents_group": GroupConfig{Enabled: true},
		},
		UpdatesPage: PageConfig{
			// Whether this system updates itself on a schedule. Updating
			// now is the Updates page's own action and needs no key.
			"automatic_updates_group": GroupConfig{Enabled: true},
			"bootc_updates_group":     GroupConfig{Enabled: true},
			"flatpak_updates_group":   GroupConfig{Enabled: true},
			"brew_updates_group":      GroupConfig{Enabled: true},
			"brew_trust_group":        GroupConfig{Enabled: true},
			// Which release stream this machine follows, and the graphics
			// driver the image carries. Both replace the operating system
			// and need a restart, so they sit with updates rather than in a
			// separate place describing the machine.
			"channel_group": GroupConfig{Enabled: true},
			// The compact system-version readout. Technical identity stays
			// behind a details row.
			"bootc_status_group": GroupConfig{Enabled: true},
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
				BundlesPaths: []string{"/usr/share/ublue-os/homebrew", "/usr/share/chairlift/bundles", "/etc/chairlift/bundles"},
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
			// One routine cleanup action, composing the existing typed
			// post-update maintenance runner. Enabled because it is safe:
			// it removes cached downloads and unused supporting software,
			// never installed apps, documents or containers.
			"maintenance_freespace_group": GroupConfig{Enabled: true},
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
			// which is every non-Bluefin host.
			//
			// dx_group's optional feed onboarding (InstallPulp, StageFeeds)
			// is left at its zero value here on purpose: enabling developer
			// access and installing a feed reader for the account are
			// separate decisions, and only the first one is what the switch
			// says it does.
			"dx_group":     GroupConfig{Enabled: true},
			"gaming_group": GroupConfig{Enabled: true},
		},
		// The panel mark and the Files application mark. Both write only
		// into the user's own icon theme and dconf, so neither needs a
		// privileged route; both ship enabled because neither changes
		// anything until a selection is made.
		LiveryPage: PageConfig{
			"livery_app_grid_group":   GroupConfig{Enabled: true},
			"livery_foundation_group": GroupConfig{Enabled: true},
			"livery_dock_group":       GroupConfig{Enabled: true},
		},
		HelpPage: PageConfig{
			// Enhanced Troubleshooting lives on Help (issue #249): one
			// task-oriented destination for help and support, with the AI
			// assistant leading. It keeps the Homebrew-backed self-hiding
			// behavior, so it is safe to enable everywhere Homebrew exists.
			"troubleshooting_group": GroupConfig{Enabled: true},
			"help_resources_group": GroupConfig{
				Enabled: true,
				Website: "https://projectbluefin.io",
				Issues:  "https://github.com/projectbluefin/dakota/issues",
				Chat:    "https://docs.projectbluefin.io/",
			},
		},
	}
}

// IsGroupEnabled checks if a preference group is enabled
func (c *Config) IsGroupEnabled(pageName, groupName string) bool {
	var page PageConfig
	switch pageName {
	case "agents_page":
		page = c.AgentsPage
	case "updates_page":
		page = c.UpdatesPage
	case "applications_page":
		page = c.ApplicationsPage
	case "maintenance_page":
		page = c.MaintenancePage
	case "features_page":
		page = c.FeaturesPage
	case "livery_page":
		page = c.LiveryPage
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
	case "agents_page":
		page = c.AgentsPage
	case "updates_page":
		page = c.UpdatesPage
	case "applications_page":
		page = c.ApplicationsPage
	case "maintenance_page":
		page = c.MaintenancePage
	case "features_page":
		page = c.FeaturesPage
	case "livery_page":
		page = c.LiveryPage
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
