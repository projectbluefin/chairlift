package pageview

import (
	"strings"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/capability"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
)

// featureTitles names every group the capability floor can hide. Groups with
// no prerequisite are never hidden by it, so they have no entry;
// TestEveryCapabilityGatedGroupHasATitle holds the table total over
// capability.Prerequisites.
var featureTitles = map[[2]string]string{
	{"updates_page", "bootc_updates_group"}:       "System updates",
	{"updates_page", "flatpak_updates_group"}:     "App updates",
	{"updates_page", "brew_updates_group"}:        "Developer tool updates",
	{"updates_page", "brew_trust_group"}:          "Unverified Homebrew sources",
	{"updates_page", "channel_group"}:             "Release channel",
	{"applications_page", "flatpak_user_group"}:   "Your apps",
	{"applications_page", "flatpak_system_group"}: "Shared apps",
	{"applications_page", "brew_group"}:           "Packages from Homebrew",
	{"applications_page", "brew_search_group"}:    "Find more apps and tools",
	{"applications_page", "brew_bundles_group"}:   "App collections",
	{"agents_page", "agents_group"}:               "Agent Mode",
	{"features_page", "dx_group"}:                 "Developer mode",
	{"features_page", "gaming_group"}:             "Gaming mode",
	{"help_page", "troubleshooting_group"}:        "Enhanced Troubleshooting",
	{"maintenance_page", "reset_group"}:           "Recovery",
}

// capabilityNames is what a person would look for on their system to supply
// one capability. Technical names are fine here: the rows sit behind an
// expander the user opened to ask why.
var capabilityNames = map[capability.Capability]string{
	capability.Flatpak:         "Flatpak",
	capability.Homebrew:        "Homebrew",
	capability.Distrobox:       "Distrobox",
	capability.BootcStage:      bootc.StageScriptPath,
	capability.ImageDescriptor: imageinfo.DescriptorPath,
}

// UnavailableFeatures lists the groups configuration enables but the host
// capability set hides, each titled for a person and subtitled with what is
// missing. It consumes the already-resolved set, so explaining never
// re-probes. A nil configured predicate lists nothing: a group configuration
// disabled is the administrator's choice, not a missing tool.
func UnavailableFeatures(set capability.Set, configured func(page, group string) bool) []Row {
	if configured == nil {
		return nil
	}
	var rows []Row
	for _, p := range capability.Prerequisites() {
		if !configured(p.Page, p.Group) || set.Supports(p.Page, p.Group) {
			continue
		}
		missing := make([]string, len(p.AnyOf))
		for i, c := range p.AnyOf {
			missing[i] = capabilityNames[c]
		}
		rows = append(rows, Row{
			Title:    featureTitles[[2]string{p.Page, p.Group}],
			Subtitle: "Needs " + strings.Join(missing, " or "),
		})
	}
	return rows
}
