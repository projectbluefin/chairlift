// Package pageview derives widget-independent page content for the views package.
package pageview

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/projectbluefin/chairlift/internal/pkexec"
)

// Row is the text displayed by a view row.
type Row struct {
	Title    string
	Subtitle string
}

// HelpResource is one configured link on the Help page.
type HelpResource struct {
	Title string
	URL   string
}

// Command is the executable and arguments for a configured maintenance action.
type Command struct {
	Name string
	Args []string
}

// OSReleaseEntry is one parsed field from an os-release file.
type OSReleaseEntry struct {
	Title string
	Value string
	IsURL bool
}

// FlatpakApplication returns the row text for an installed Flatpak application.
func FlatpakApplication(name, applicationID, version string) Row {
	subtitle := applicationID
	if version != "" {
		subtitle = fmt.Sprintf("%s (%s)", applicationID, version)
	}
	return Row{Title: name, Subtitle: subtitle}
}

// HomebrewPackage returns the row text for an installed Homebrew package.
func HomebrewPackage(name, version string, pinned bool) Row {
	subtitle := version
	if pinned {
		subtitle += " • Pinned"
	}
	return Row{Title: name, Subtitle: subtitle}
}

// BrewBundle returns the row text for a configured Homebrew bundle.
func BrewBundle(name, description, path string) Row {
	subtitle := path
	if description != "" {
		subtitle = fmt.Sprintf("%s — %s", description, path)
	}
	return Row{Title: name, Subtitle: subtitle}
}

// SearchResult returns the row text for a Homebrew search result.
func SearchResult(name, kind string) Row {
	return Row{Title: name, Subtitle: kind}
}

// UntrustedTap returns the row text for an untrusted Homebrew tap.
func UntrustedTap(name string, formulae, casks []string) Row {
	packages := make([]string, 0, len(formulae)+len(casks))
	for _, names := range [][]string{formulae, casks} {
		for _, packageName := range names {
			if i := strings.LastIndex(packageName, "/"); i >= 0 {
				packageName = packageName[i+1:]
			}
			packages = append(packages, packageName)
		}
	}
	return Row{
		Title:    name,
		Subtitle: fmt.Sprintf("%d installed: %s", len(packages), strings.Join(packages, ", ")),
	}
}

// FlatpakUpdate returns the row text for an available Flatpak update.
func FlatpakUpdate(name, applicationID, newVersion, installation string) Row {
	subtitle := applicationID
	if newVersion != "" {
		subtitle = fmt.Sprintf("%s → %s", applicationID, newVersion)
	}
	if installation == "user" {
		subtitle += " (user)"
	}
	return Row{Title: name, Subtitle: subtitle}
}

// BootcUpdateSubtitle returns the system-update expander subtitle.
func BootcUpdateSubtitle(staged bool, version string) string {
	if !staged {
		return "Check for and download the latest system image"
	}
	if version == "" {
		return "Update staged — restart to apply"
	}
	return fmt.Sprintf("Update %s staged — restart to apply", version)
}

// BootcStageResultSubtitle returns the subtitle after a staging action completes.
func BootcStageResultSubtitle(staged bool, version, lastMessage string) string {
	if staged {
		return BootcUpdateSubtitle(true, version)
	}
	if lastMessage != "" {
		return lastMessage
	}
	return "System is up to date"
}

// StagingLogSubtitle returns the "Details" expander subtitle for a staging
// run whose output is rendered through a bounded rolling window: shown is how
// many lines the expander currently holds and total is how many the stage
// helper has printed.
//
// It names the cap whenever one applied, because a truncated log that reads
// like a complete one is worse than no log at all: someone diagnosing a failed
// stage would otherwise keep looking for a line the view had silently dropped.
func StagingLogSubtitle(shown, total int) string {
	switch {
	case total <= 0:
		return "View output"
	case shown < total:
		return fmt.Sprintf("Showing the last %d of %d lines", shown, total)
	case total == 1:
		return "View output (1 line)"
	default:
		return fmt.Sprintf("View output (%d lines)", total)
	}
}

// SysupdateUpdateSubtitle returns the native A/B system-update expander
// subtitle from the /run/snosi state-file presentation (the outcome grammar
// is internal/sysupdate.Status.Presentation's): "staged" shows the pending
// version, "current" shows the last check time, "failed" prompts a retry,
// and anything else — including the fresh-boot no-files state — is the
// neutral idle prompt.
func SysupdateUpdateSubtitle(outcome, version, checkedAt string) string {
	switch outcome {
	case "staged":
		if version == "" {
			return "Update staged — restart to apply"
		}
		return fmt.Sprintf("Update %s staged — restart to apply", version)
	case "current":
		if formatted := formatCheckedAt(checkedAt); formatted != "" {
			return fmt.Sprintf("System is up to date (checked %s)", formatted)
		}
		return "System is up to date"
	case "failed":
		return "Last update check failed — use Check for Updates to retry"
	default:
		return "Check for and download the latest system image"
	}
}

// formatCheckedAt renders a stager ISO-8601 timestamp as a local wall-clock
// time, or "" when unparseable.
func formatCheckedAt(checkedAt string) string {
	parsed, err := time.Parse(time.RFC3339, checkedAt)
	if err != nil {
		return ""
	}
	return parsed.Local().Format("15:04")
}

// SysupdateStageResultSubtitle returns the subtitle after a native A/B
// staging action completes.
func SysupdateStageResultSubtitle(staged bool, version, lastMessage string) string {
	if staged {
		return SysupdateUpdateSubtitle("staged", version, "")
	}
	if lastMessage != "" {
		return lastMessage
	}
	return "System is up to date"
}

// SysupdateRollbackSubtitle returns the read-only rollback row subtitle.
// version is the inactive slot's version only when it is older than the
// running one (internal/sysupdate.RollbackCandidate); a staged-but-newer
// slot or an empty slot both present as no rollback.
func SysupdateRollbackSubtitle(version string) string {
	if version == "" {
		return "No previous version on disk"
	}
	return fmt.Sprintf("Version %s is on the inactive slot — choose it in the boot menu at restart to roll back", version)
}

// Feature returns the initial row text for an updex feature.
func Feature(name, description string) Row {
	return Row{Title: description, Subtitle: name}
}

// FeatureGroupDescription returns the description after features are loaded.
func FeatureGroupDescription(count int) string {
	return fmt.Sprintf("%d features available", count)
}

// HelpResources returns configured Help links in their display order.
func HelpResources(website, issues, chat string) []HelpResource {
	candidates := []HelpResource{
		{Title: "Website", URL: website},
		{Title: "Report Issues", URL: issues},
		{Title: "Community Discussions", URL: chat},
	}
	resources := make([]HelpResource, 0, len(candidates))
	for _, resource := range candidates {
		if resource.URL != "" {
			resources = append(resources, resource)
		}
	}
	return resources
}

// MaintenanceCommand returns the invocation for a configured maintenance action.
func MaintenanceCommand(script string, sudo bool) Command {
	if sudo {
		return Command{Name: pkexec.Command, Args: []string{script}}
	}
	return Command{Name: script}
}

// ParseOSRelease parses displayable fields from an os-release stream.
func ParseOSRelease(reader io.Reader) ([]OSReleaseEntry, error) {
	var entries []OSReleaseEntry
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		key := parts[0]
		value := strings.Trim(parts[1], "\"'")
		readableKey := strings.ReplaceAll(key, "_", " ")
		readableKey = cases.Title(language.English).String(strings.ToLower(readableKey))

		entries = append(entries, OSReleaseEntry{
			Title: readableKey,
			Value: value,
			IsURL: strings.HasSuffix(key, "URL"),
		})
	}
	return entries, scanner.Err()
}

// ShortDigest returns a compact bootc digest for display.
func ShortDigest(digest string) string {
	if len(digest) > 19 {
		return digest[:19] + "..."
	}
	return digest
}

// BluefinGroupDescription returns the Bluefin-family group description for a
// detected variant and running tag — e.g. "Bluefin LTS · lts". An empty tag
// falls back to the product name alone.
func BluefinGroupDescription(variantName, tag string) string {
	if variantName == "" {
		return "Bluefin-family features"
	}
	if tag == "" {
		return variantName
	}
	return fmt.Sprintf("%s · %s", variantName, tag)
}

// ChannelRow returns the release-channel switch row text. onTesting is the
// running channel; switchable is false when the running tag has no
// counterpart to switch to, in which case the subtitle explains the row is
// inert rather than leaving the user to guess.
func ChannelRow(onTesting, switchable bool, tag string) Row {
	row := Row{Title: "Testing Channel"}
	switch {
	case !switchable && tag != "":
		row.Subtitle = fmt.Sprintf("This image publishes no testing channel for the %s tag", tag)
	case !switchable:
		row.Subtitle = "This image does not publish a testing channel"
	case onTesting:
		row.Subtitle = "Tracking testing — turn off to return to stable, then restart"
	default:
		row.Subtitle = "Track pre-release images — unstable, restart to apply"
	}
	return row
}

// ChannelSwitchResultSubtitle returns the subtitle after a channel switch
// completes. It never claims the running system changed: bootc stages the
// new image, so the restart is the part the user still has to do.
func ChannelSwitchResultSubtitle(toTesting bool) string {
	if toTesting {
		return "Switched to testing — restart to apply"
	}
	return "Switched to stable — restart to apply"
}

// DeveloperRow returns the developer-mode switch row text. groups are the
// developer groups the user currently belongs to.
func DeveloperRow(active bool, groups []string) Row {
	row := Row{Title: "Developer Mode"}
	if active {
		row.Subtitle = fmt.Sprintf("Active — member of %s", strings.Join(groups, ", "))
		return row
	}
	row.Subtitle = "Join the container, VM, and serial-device groups"
	return row
}

// DeveloperResultSubtitle returns the subtitle after a developer-mode toggle
// completes. Group membership is only applied to new sessions, so both
// outcomes say so rather than implying an immediate effect.
func DeveloperResultSubtitle(enabled bool) string {
	if enabled {
		return "Developer mode enabled — log out and back in to take effect"
	}
	return "Developer mode disabled — log out and back in to take effect"
}

// GamingRow returns the gaming-mode switch row text. summary comes from
// internal/gaming.State.Summary.
func GamingRow(summary string) Row {
	return Row{Title: "Gaming Mode", Subtitle: summary}
}

// GamingResultSubtitle returns the subtitle after a gaming-mode toggle
// completes. changed is the number of Flatpaks installed or removed, and
// failed the number that could not be.
func GamingResultSubtitle(enabled bool, changed, failed int) string {
	verb := "removed"
	if enabled {
		verb = "installed"
	}
	if failed > 0 {
		return fmt.Sprintf("%d component(s) %s, %d failed", changed, verb, failed)
	}
	if changed == 0 {
		return "No components needed changing"
	}
	return fmt.Sprintf("%d component(s) %s", changed, verb)
}

// UpdateAllRow returns the Update All hero row text before a run has started.
// planned is the number of phases that will run on this host.
func UpdateAllRow(planned int) Row {
	row := Row{Title: "Update All"}
	switch planned {
	case 0:
		row.Subtitle = "Nothing on this system can be updated from here"
	case 1:
		row.Subtitle = "Update the one available source"
	default:
		row.Subtitle = "Update the system image, applications, and packages in one step"
	}
	return row
}

// UpdateAllPhaseSubtitle returns the per-phase row subtitle. running is true
// while the phase is in flight; detail is the phase's Result detail once it
// has finished, and empty before it starts.
func UpdateAllPhaseSubtitle(running bool, detail string) string {
	switch {
	case running:
		return "Working…"
	case detail == "":
		return "Waiting"
	default:
		return detail
	}
}

// RestartRow returns the restart prompt row text. It is only shown when an OS
// image is actually staged, so it always names the restart as the thing that
// applies it rather than as a generic suggestion.
func RestartRow(version string) Row {
	if version == "" {
		return Row{
			Title:    "Restart to Apply",
			Subtitle: "A system update is staged and takes effect after a restart",
		}
	}
	return Row{
		Title:    "Restart to Apply",
		Subtitle: fmt.Sprintf("Version %s is staged and takes effect after a restart", version),
	}
}

// BootcRollbackRow returns the bootc rollback row text. version and timestamp
// describe the deployment the host would return to; either may be empty.
//
// It is deliberately a single row naming one destination, not a history
// browser: `bootc rollback` has exactly one target — the deployment the host
// already records — so offering a choice would imply a capability the
// operation does not have.
func BootcRollbackRow(version, timestamp string) Row {
	row := Row{Title: "Roll Back"}
	switch {
	case version == "" && timestamp == "":
		row.Subtitle = "No previous system image is available"
	case version == "":
		row.Subtitle = fmt.Sprintf("Return to the image from %s at the next restart", timestamp)
	case timestamp == "":
		row.Subtitle = fmt.Sprintf("Return to version %s at the next restart", version)
	default:
		row.Subtitle = fmt.Sprintf("Return to version %s (%s) at the next restart", version, timestamp)
	}
	return row
}

// BootcRollbackResultSubtitle returns the subtitle after a rollback is
// staged. Rolling back only changes which deployment boots next, so it never
// claims the running system changed.
func BootcRollbackResultSubtitle() string {
	return "Rolled back — restart to boot the previous image"
}

// AutomaticUpdatesRow returns the automatic-background-updates switch row
// text. It is one row rather than bluefinctl's strategy picker, schedule
// rows, and per-layer switches, so its subtitle has to carry what the switch
// actually governs.
func AutomaticUpdatesRow(enabled bool) Row {
	row := Row{Title: "Automatic Updates"}
	if enabled {
		row.Subtitle = "This system installs updates in the background and applies them at restart"
		return row
	}
	row.Subtitle = "Update this system only when you ask"
	return row
}

// AutomaticUpdatesResultSubtitle returns the subtitle after the switch is
// toggled. Turning automatic updates on does not update anything right now,
// so neither outcome implies an immediate change.
func AutomaticUpdatesResultSubtitle(enabled bool) string {
	if enabled {
		return "Automatic updates are on — the next check runs on the system's schedule"
	}
	return "Automatic updates are off — use Update All when you want to update"
}

// GraphicsDriverRow returns the graphics-driver row text. current is the
// driver flavour of the running image, hardware describes the detected GPU,
// and recommended is non-empty only when a switch is both possible and
// worthwhile.
//
// The row is informational whenever there is nothing to offer, which is the
// common case: an AMD or Intel machine is already correct, and an LTS host
// has no driver image published at all.
func GraphicsDriverRow(current, hardware, recommended string) Row {
	row := Row{Title: "Graphics Driver"}
	switch {
	case recommended != "":
		row.Subtitle = fmt.Sprintf("%s detected — switch to the %s image, then restart", hardware, recommended)
	case current != "" && hardware != "":
		row.Subtitle = fmt.Sprintf("%s · running the %s image", hardware, current)
	case hardware != "":
		row.Subtitle = hardware
	default:
		row.Subtitle = "No graphics hardware detected"
	}
	return row
}

// GraphicsDriverResultSubtitle returns the subtitle after a driver switch is
// staged. Like a channel switch it only stages the image, so it never claims
// the running system changed.
func GraphicsDriverResultSubtitle(driver string) string {
	return fmt.Sprintf("Switched to the %s image — restart to apply", driver)
}

// PowerwashRow returns the Powerwash row text. summary comes from
// internal/powerwash.Summarize's Headline once a run has completed, or "" if
// no run has happened yet.
func PowerwashRow(summary string) Row {
	if summary == "" {
		return Row{
			Title:    "Remove Everything I Installed",
			Subtitle: "Removes every user Flatpak and Distrobox container. Does not touch the system image.",
		}
	}
	return Row{Title: "Remove Everything I Installed", Subtitle: summary}
}

// PowerwashConfirmation returns the title and body of the confirmation
// dialog Powerwash must show before it runs, since removing every installed
// application and container cannot be undone from within ChairLift.
func PowerwashConfirmation() (title, body string) {
	return "Remove Everything I Installed?",
		"This removes every Flatpak application and every Distrobox container " +
			"for your account. It does not touch the system image or your files. " +
			"This cannot be undone."
}

// FactoryResetRow returns the Factory Reset row text.
func FactoryResetRow() Row {
	return Row{
		Title:    "Factory Reset",
		Subtitle: "Replaces the system with a fresh install of the current image, discarding local changes",
	}
}

// FactoryResetConfirmation returns the title and body of the confirmation
// dialog Factory Reset must show before it runs. The body names
// --experimental explicitly — bootc's own reset path is not stabilized
// upstream, and a confirmation that omitted that fact would be hiding the
// one piece of information most likely to change a user's mind.
func FactoryResetConfirmation() (title, body string) {
	return "Factory Reset This System?",
		"This discards every local change and reinstalls the current system " +
			"image from scratch, using bootc's --experimental reset path. " +
			"Your applications and home directory are not touched, but this " +
			"cannot be undone. The reset applies at the next restart."
}

// FactoryResetResultSubtitle returns the subtitle after a factory reset is
// applied. Like a channel switch it only stages the change.
func FactoryResetResultSubtitle() string {
	return "Factory reset applied — restart to complete it"
}
