package pageview

import (
	"strings"

	"github.com/projectbluefin/chairlift/internal/capability"
)

// featureTitles names every group the capability floor can hide. Groups with
// no prerequisite are never hidden by it, so they have no entry;
// TestEveryCapabilityGatedGroupHasATitle holds the table total over
// capability.Prerequisites.
var featureTitles = map[[2]string]string{
	{"livery_page", "wallpapers_group"}:         "Wallpapers",
	{"updates_page", "bootc_updates_group"}:     "System Updates",
	{"updates_page", "flatpak_updates_group"}:   "App Updates",
	{"updates_page", "brew_updates_group"}:      "Developer Tool Updates",
	{"updates_page", "brew_trust_group"}:        "Unverified Sources",
	{"updates_page", "channel_group"}:           "Early Updates",
	{"applications_page", "brew_group"}:         "Installed Apps and Tools",
	{"applications_page", "brew_bundles_group"}: "App Collections",
	{"agents_page", "agents_group"}:             "Agent Mode",
	{"agents_page", "troubleshooting_group"}:    "Troubleshooting",
	{"features_page", "dx_group"}:               "Developer Mode",
	{"features_page", "gaming_group"}:           "Gaming Mode",
	{"features_page", "printers_group"}:         "Printers",
	{"maintenance_page", "reset_group"}:         "Powerwash",
}

// capabilityNames is what a person would look for on their computer to
// supply one capability. Product names a person can install are fine here;
// file paths are not.
var capabilityNames = map[capability.Capability]string{
	capability.Flatpak:         "Flatpak",
	capability.Homebrew:        "Homebrew",
	capability.Distrobox:       "Distrobox",
	capability.Podman:          "Podman",
	capability.BootcStage:      "Bluefin's system updates",
	capability.ImageDescriptor: "Bluefin",
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
