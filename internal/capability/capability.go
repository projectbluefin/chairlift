// Package capability resolves which host facilities ChairLift's pages and
// groups depend on, and answers whether this host provides them.
//
// ChairLift hides a group whose backing tool is absent rather than rendering
// it inert — the policy internal/autoupdate states in source. This package is
// the read-only, display-side classification that policy needs: Set.Supports
// has exactly the shape internal/navigation's VisibleItems already accepts as
// its `enabled` parameter, and Compose is how an administrator's group
// configuration is combined with it.
//
// Three rules shape everything here.
//
// A capability is the presence of a backing tool or asset, never a runtime
// state. That `bootc` is installed is a capability; whether this machine is
// booted from a bootc deployment is not, because answering it runs
// `bootc status`. Probes are restricted to the non-blocking operations —
// exec.LookPath, os.Stat, and environment reads — which is the constraint
// page-level resolution inherits when it is threaded through `buildUI`. A
// group whose gate needs a query rather than a presence check keeps its own
// asynchronous gate, builds a hidden shell, and is revealed once the query
// answers; it is deliberately left unclassified in the prerequisites table
// rather than given a capability that would answer a different question.
//
// Capability is a floor, not a ceiling. A group renders only when its
// configuration enables it and the host supports it, so configuration may
// subtract from the capability set and never add to it; Compose implements
// that direction, and a nil configuration predicate composes to false
// everywhere, which is the repository's fail-closed rule for a caller holding
// no configuration. Within the table the same rule holds — a group whose
// capabilities are all absent is unsupported — with one deliberate exception:
// an *unclassified* pair is reported as supported, so that a missing entry
// cannot silently hide a group at runtime. That exception is only safe because
// the table's totality is enforced against the configuration schema by a gate;
// see Prerequisites.
//
// The package is pure: it imports no puregotk, directly or transitively, so
// its table-driven tests run on a GTK-less host like every other decidable
// leaf under internal/ (ADR-0007).
package capability

import (
	"os"
	"os/exec"
	"sort"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/sysupdate"
)

// Capability names one host facility a group's backing tool or asset
// provides. The string values are stable identifiers for diagnostics, not
// user-visible text.
type Capability string

const (
	// Flatpak is the flatpak command on $PATH.
	Flatpak Capability = "flatpak"
	// Homebrew is the brew command on $PATH. Its resolution is the
	// visibility half of the unified Homebrew path resolution; the execution
	// half stays with internal/homebrew.
	Homebrew Capability = "brew"
	// Podman is the podman command on $PATH. Quadlet is a Podman feature, so
	// it is the prerequisite of the rootless local-AI container.
	Podman Capability = "podman"
	// Distrobox is the distrobox command on $PATH.
	Distrobox Capability = "distrobox"
	// BootcStage is the installed OS staging script internal/bootc invokes
	// through pkexec. It is the prerequisite of the bootc updates group: a
	// host with the bootc command but no stage script has nothing for that
	// group to run.
	BootcStage Capability = "bootc-stage"
	// Sysupdate is a native A/B install — snosi's marker file — with the
	// staging script installed. Both halves are required, matching the gate
	// the sysupdate updates group already applies.
	Sysupdate Capability = "sysupdate"
	// ImageDescriptor is the read-only ublue-os image descriptor. It is the
	// prerequisite of every Bluefin-family group, which has nothing to render
	// on a host that runs a different OS.
	ImageDescriptor Capability = "image-descriptor"
)

// pathCapabilities pairs every capability provided by an executable with the
// name resolved on $PATH. Iterating one table keeps DetectWith's two probe
// kinds — one LookPath per binary, one Stat per asset — from drifting apart.
var pathCapabilities = []struct {
	capability Capability
	binary     string
}{
	{Flatpak, "flatpak"},
	{Homebrew, "brew"},
	{Podman, "podman"},
	{Distrobox, "distrobox"},
}

// assetCapabilities pairs every capability provided by a fixed path with the
// presence test that decides it. Sysupdate needs both of its halves, which is
// why each entry holds a whole predicate rather than a path list.
var assetCapabilities = []struct {
	capability Capability
	present    func(Probe) bool
}{
	{BootcStage, func(p Probe) bool { return exists(p, bootc.StageScriptPath) }},
	{Sysupdate, func(p Probe) bool {
		return exists(p, sysupdate.MarkerPath) && exists(p, sysupdate.StageScriptPath)
	}},
	{ImageDescriptor, func(p Probe) bool { return exists(p, imageinfo.DescriptorPath) }},
}

// providedCapabilities returns every capability either probe table can
// resolve, so a caller — and the tests — can enumerate the set from its
// single source of truth instead of restating it.
func providedCapabilities() []Capability {
	result := make([]Capability, 0, len(pathCapabilities)+len(assetCapabilities))
	for _, entry := range pathCapabilities {
		result = append(result, entry.capability)
	}
	for _, entry := range assetCapabilities {
		result = append(result, entry.capability)
	}
	return result
}

// Probe is the non-blocking host inspection surface. Both fields are function
// seams so resolution is testable without installing anything or touching the
// real host, and so the screenshot walkthrough can substitute a host shape
// under the chairlift_e2e build tag. There is no field for a command
// execution: a capability that needed one would be a runtime state, not a
// capability.
type Probe struct {
	// LookPath resolves a bare command name, as exec.LookPath does.
	LookPath func(name string) (string, error)
	// Stat reports one fixed path's existence, as os.Stat does.
	Stat func(name string) (os.FileInfo, error)
}

// systemProbe is the production probe: the two non-blocking operations the
// package is restricted to.
func systemProbe() Probe {
	return Probe{LookPath: exec.LookPath, Stat: os.Stat}
}

// probe is the injection seam for Detect, so a build that must render a host
// shape it is not running on can replace it before the first resolution.
var probe = systemProbe()

// SetProbe replaces the probe Detect resolves with. Call it before the window
// resolves capabilities, never after: page-level capabilities are resolved
// once during `buildUI` and stay immutable for the session, so a sidebar whose
// items shift under the user's cursor is not a state this package can produce.
func SetProbe(p Probe) {
	probe = p
}

// Set is a resolved capability inventory. The zero Set has no capabilities,
// which is the correct resolution for a host that has none of them and the
// fail-closed value for a caller that could not resolve.
type Set map[Capability]bool

// Has reports whether the host provides one capability.
func (s Set) Has(c Capability) bool {
	return s[c]
}

// Supports reports whether this host satisfies one page's group. A group is
// satisfied by any one of its capabilities — see Prerequisites — and a group
// the table does not classify requires nothing, so it is reported as
// supported.
//
// That last case is a runtime safety net rather than a licence to leave the
// table incomplete: internal/installcheck holds the table to
// config.SchemaGroups in both directions, so an unclassified group fails a
// gate rather than silently rendering on a host that cannot back it.
func (s Set) Supports(page, group string) bool {
	required, classified := Required(page, group)
	if !classified || len(required) == 0 {
		return true
	}
	for _, c := range required {
		if s[c] {
			return true
		}
	}
	return false
}

// Compose composes an administrator's group configuration with a resolved
// capability set, producing the one predicate the sidebar navigation, the view
// builders, and the update coordinator are meant to share rather than each
// deriving its own answer. Capability is a floor: a group is enabled only when
// the configuration enables it and the host supports it, so configuration may
// subtract from the set and never add to it.
//
// A nil configured predicate composes to false for every pair. That matches
// navigation.VisibleItems' treatment of a nil predicate and keeps the
// repository's fail-closed rule: a caller with no configuration has no
// evidence that anything should render.
func Compose(configured func(page, group string) bool, set Set) func(page, group string) bool {
	return func(page, group string) bool {
		if configured == nil || !configured(page, group) {
			return false
		}
		return set.Supports(page, group)
	}
}

// Prerequisite is one classified page/group pair and the capabilities that
// can satisfy it. An empty AnyOf requires nothing of the host.
type Prerequisite struct {
	Page  string
	Group string
	AnyOf []Capability
}

// prerequisites classifies every configurable group. The list is total over
// config.SchemaGroups, and internal/installcheck's
// TestCapabilityPrerequisitesMatchConfigSchema holds both directions, so a
// group added to the schema fails that gate until it is classified here.
//
// A group with no capabilities requires nothing of the host, and is listed
// anyway so that "no prerequisite" is a recorded decision rather than an
// omission. Three of those are worth naming, because they read like
// omissions:
//
//   - bootc_status_group renders whether this machine is booted from a bootc
//     deployment, which only `bootc status` can answer.
//   - features_group renders whether updex has features configured, which is
//     a query against the feature store rather than the presence of an asset.
//   - maintenance_optimization_group is a placeholder row with no backing
//     tool at all.
//
// Those three keep their existing asynchronous gates in the view layer. Every
// other entry names the presence that makes its group meaningful, and the
// group is satisfied by any one of them.
var prerequisites = []Prerequisite{
	// System. The image-identity group has nothing to show without the
	// descriptor; the rest are local reads.
	{Page: "system_page", Group: "system_info_group"},
	{Page: "system_page", Group: "bootc_status_group"},
	{Page: "system_page", Group: "channel_group", AnyOf: []Capability{ImageDescriptor}},
	{Page: "system_page", Group: "health_group"},

	// Updates. Update All is the union of the per-provider groups: its plan
	// only contains phases whose own group is present, so a host with no
	// provider at all has an Update All group with nothing to offer and must
	// not construct it. The automatic-updates switch inside that group is a
	// row, not a group — its uupd.timer prerequisite keeps its own
	// asynchronous gate in internal/autoupdate.
	{Page: "updates_page", Group: "update_all_group", AnyOf: []Capability{BootcStage, Sysupdate, Flatpak, Homebrew}},
	{Page: "updates_page", Group: "bootc_updates_group", AnyOf: []Capability{BootcStage}},
	{Page: "updates_page", Group: "sysupdate_updates_group", AnyOf: []Capability{Sysupdate}},
	{Page: "updates_page", Group: "flatpak_updates_group", AnyOf: []Capability{Flatpak}},
	{Page: "updates_page", Group: "brew_updates_group", AnyOf: []Capability{Homebrew}},
	{Page: "updates_page", Group: "brew_trust_group", AnyOf: []Capability{Homebrew}},

	// Applications. applications_installed_group only launches a Flatpak
	// application manager by ID, which a host without Flatpak can still fail
	// to launch the same way it does today; the group advertises an external
	// tool rather than driving one, so it is not gated on Flatpak.
	{Page: "applications_page", Group: "applications_installed_group"},
	{Page: "applications_page", Group: "flatpak_user_group", AnyOf: []Capability{Flatpak}},
	{Page: "applications_page", Group: "flatpak_system_group", AnyOf: []Capability{Flatpak}},
	{Page: "applications_page", Group: "brew_group", AnyOf: []Capability{Homebrew}},
	{Page: "applications_page", Group: "brew_search_group", AnyOf: []Capability{Homebrew}},
	{Page: "applications_page", Group: "brew_bundles_group", AnyOf: []Capability{Homebrew}},

	// Maintenance. Powerwash removes every user Flatpak and Distrobox
	// container, and each of its two steps already reports OutcomeSkipped
	// when its tool is absent, so the group needs either one to do anything.
	{Page: "maintenance_page", Group: "maintenance_cleanup_group"},
	{Page: "maintenance_page", Group: "maintenance_brew_group", AnyOf: []Capability{Homebrew}},
	{Page: "maintenance_page", Group: "maintenance_flatpak_group", AnyOf: []Capability{Flatpak}},
	{Page: "maintenance_page", Group: "maintenance_optimization_group"},
	{Page: "maintenance_page", Group: "reset_group", AnyOf: []Capability{Flatpak, Distrobox}},

	// Features. The developer and gaming groups both render Bluefin-family
	// state from the image descriptor. Troubleshooting installs everything it
	// needs from Homebrew. The AI stack needs Podman, whose Quadlet support
	// its rootless container is built on.
	{Page: "features_page", Group: "features_group"},
	{Page: "features_page", Group: "dx_group", AnyOf: []Capability{ImageDescriptor}},
	{Page: "features_page", Group: "gaming_group", AnyOf: []Capability{ImageDescriptor}},
	{Page: "features_page", Group: "ai_group", AnyOf: []Capability{Podman}},
	{Page: "features_page", Group: "troubleshooting_group", AnyOf: []Capability{Homebrew}},

	// Livery. The app-grid mark, dock, and panel foundation marks write
	// icons into the user's theme and preferences into dconf, with no
	// external host tool prerequisites.
	{Page: "livery_page", Group: "livery_app_grid_group"},
	{Page: "livery_page", Group: "livery_dock_group"},
	{Page: "livery_page", Group: "livery_foundation_group"},

	// Help is always retained in the sidebar, so its group is never the
	// reason a page disappears.
	{Page: "help_page", Group: "help_resources_group"},
}

// requirementIndex is prerequisites keyed by page and then group, built once
// so a lookup does not scan the list per group per page build.
var requirementIndex = func() map[string]map[string][]Capability {
	index := make(map[string]map[string][]Capability, len(prerequisites))
	for _, p := range prerequisites {
		groups, ok := index[p.Page]
		if !ok {
			groups = make(map[string][]Capability)
			index[p.Page] = groups
		}
		groups[p.Group] = p.AnyOf
	}
	return index
}()

// Required returns the capabilities that can satisfy one page's group.
// classified is false for a pair the table does not classify.
//
// The returned slice is a copy: a caller may sort or truncate it without
// changing the resolved table.
func Required(page, group string) (required []Capability, classified bool) {
	groups, ok := requirementIndex[page]
	if !ok {
		return nil, false
	}
	anyOf, ok := groups[group]
	if !ok {
		return nil, false
	}
	return append([]Capability(nil), anyOf...), true
}

// Prerequisites returns every classified pair, ordered by page and then group,
// each with a copy of its capabilities. It exists so a gate can hold the table
// to the configuration schema in both directions; nothing in the running
// application needs the whole table.
func Prerequisites() []Prerequisite {
	result := make([]Prerequisite, len(prerequisites))
	for i, p := range prerequisites {
		result[i] = Prerequisite{
			Page:  p.Page,
			Group: p.Group,
			AnyOf: append([]Capability(nil), p.AnyOf...),
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Page != result[j].Page {
			return result[i].Page < result[j].Page
		}
		return result[i].Group < result[j].Group
	})
	return result
}

// Detect resolves this host's capability set through the package probe. Call
// it once during window construction and keep the result: capabilities are
// resolved once per session and never re-probed, so sidebar accelerators and
// items do not shift under the user's cursor.
//
// Every probe is a LookPath or a Stat, so the call is cheap enough to run on
// the GTK main thread. Nothing here is cached: the immutability is the
// caller's contract, and a package-level cache would make a test's probe
// substitution order-dependent.
func Detect() Set {
	return DetectWith(probe)
}

// DetectWith resolves a capability set through p. It is the pure core Detect
// wraps, and what the tests drive directly.
func DetectWith(p Probe) Set {
	set := make(Set, len(pathCapabilities)+len(assetCapabilities))
	for _, entry := range pathCapabilities {
		set[entry.capability] = onPath(p, entry.binary)
	}

	// The OS staging capabilities are asset presences, not command
	// presences: each group's action is a fixed script invoked through
	// pkexec, so the script is what the group needs.
	for _, entry := range assetCapabilities {
		set[entry.capability] = entry.present(p)
	}
	return set
}

// onPath reports whether one command resolves on $PATH. A probe that returns
// an error for any reason — absent, or not executable — is not present.
func onPath(p Probe, name string) bool {
	if p.LookPath == nil {
		return false
	}
	_, err := p.LookPath(name)
	return err == nil
}

// exists reports whether one fixed path is present. A probe that returns an
// error for any reason is not present.
func exists(p Probe, path string) bool {
	if p.Stat == nil {
		return false
	}
	_, err := p.Stat(path)
	return err == nil
}
