// Package pageview derives widget-independent page content for the views package.
package pageview

import (
	"fmt"
	"time"

	"github.com/projectbluefin/chairlift/internal/gpu"
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

// HomebrewPackage returns the row text for an installed Homebrew package.
func HomebrewPackage(name, version string, pinned bool) Row {
	subtitle := version
	if pinned {
		subtitle += " • Pinned"
	}
	return Row{Title: name, Subtitle: subtitle}
}

// UntrustedTap returns the row text for a software source whose updates
// Homebrew has paused. The source is named, because a person cannot decide
// to trust something they cannot identify, and the count stands in for the
// package list: what matters is how much is stuck, not its taxonomy.
func UntrustedTap(name string, formulae, casks []string) Row {
	count := len(formulae) + len(casks)
	row := Row{Title: name, Subtitle: "Updates are paused for software from this source"}
	switch {
	case count == 1:
		row.Subtitle = "Updates are paused for 1 program you installed from this source"
	case count > 1:
		row.Subtitle = fmt.Sprintf("Updates are paused for %d programs you installed from this source", count)
	}
	return row
}

// BootcStageButtonLabel is the label of the system-update row's action. The
// action runs the image's stage helper, which downloads the newest version
// and stages it for the next restart, so the button says "Download": a
// button labelled as a check that pulled an image and queued a deployment
// behind an administrator prompt was the shakedown's W3-01.
const BootcStageButtonLabel = "Download"

// BootcStageRunningSubtitle is the system-update expander subtitle while the
// stage helper runs. It names both halves, because the helper checks first
// and downloads only when a newer version exists.
const BootcStageRunningSubtitle = "Looking for a newer version and downloading it…"

// BootcUpdateSubtitle returns the system-update expander subtitle. A
// downloaded update changes nothing until the machine restarts, so the
// waiting state says when it takes effect rather than that it is "staged".
// The idle state describes what the Download action does — a download, an
// administrator password, and nothing until a restart — because those are
// the consequences a person cannot discover from a button label.
func BootcUpdateSubtitle(staged bool, version string) string {
	if !staged {
		return "Downloads the newest version of the operating system. Asks for an administrator password; the new version installs when you restart"
	}
	if version == "" {
		return "A new version is ready and installs when you restart"
	}
	return fmt.Sprintf("Version %s is ready and installs when you restart", version)
}

// BootcStageResultSubtitle returns the subtitle after a staging action
// completes. Nothing waiting after a run that reported no error means the
// system is current. The script's own last line is deliberately not shown:
// it is written for a terminal and can name paths a person has no use for.
func BootcStageResultSubtitle(staged bool, version string) string {
	if staged {
		return BootcUpdateSubtitle(true, version)
	}
	return "Your system is up to date"
}

// BootcStageFailureSubtitle returns the subtitle after a staging action
// failed. It points at Details only when the helper printed something: the
// view hides Details until a line arrives, and a stage can print nothing —
// on Dakota, bootc logged its progress to the journal, not to the pipe.
func BootcStageFailureSubtitle(hadOutput bool) string {
	if hadOutput {
		return "The update could not be downloaded. Open Details to see what happened."
	}
	return "The update could not be downloaded"
}

// UpdateBusyToast answers a Download, early-updates, or graphics-driver
// request the update shell refused because a check, an update run, or a
// restart already owns the system. Each of them replaces or stages the
// operating system, so running one beside an update run's own staging would
// race two privileged deployments; a refused click must still say why
// nothing happened.
const UpdateBusyToast = "Wait for the current update check or installation to finish"

// PrivilegedFailureToast returns the toast for a privileged action that
// returned err, and whether that toast is an error. failure is the view's
// own plain-language failure text; the views on the Updates page do not
// quote the helper's output, so the window's message-based dismissal check
// never sees pkexec's "Request dismissed". A dismissed authentication prompt
// is not a failure — the helper never ran — so it yields the brief
// pkexec.CancelledMessage instead of a persistent error.
func PrivilegedFailureToast(err error, failure string) (toast string, isError bool) {
	if pkexec.IsAuthDismissed(err) {
		return pkexec.CancelledMessage, false
	}
	return failure, true
}

// StagingLogSubtitle returns the "Details" expander subtitle for a staging
// run whose output is rendered through a bounded rolling window: shown is how
// many lines the expander currently holds and total is how many the stage
// helper has printed.
//
// It names the cap whenever one applied, because a truncated log that reads
// like a complete one is worse than no log at all: someone diagnosing a failed
// stage would otherwise keep looking for a line the view had silently dropped.
// The view hides the expander until the first line arrives, so total <= 0
// never names an empty log as something to view.
func StagingLogSubtitle(shown, total int) string {
	switch {
	case total <= 0:
		return "No output"
	case shown < total:
		return fmt.Sprintf("Showing the last %d of %d lines", shown, total)
	case total == 1:
		return "View output (1 line)"
	default:
		return fmt.Sprintf("View output (%d lines)", total)
	}
}

// ChangelogComparingLabel is the Compare button's label while a comparison
// runs. The view both sets it and tests for it, so it has one spelling; a
// three-dot copy of the ellipsis made the in-flight guard never match.
const ChangelogComparingLabel = "Comparing…"

// Feature returns the initial row text for an updex feature.
func Feature(name, description string) Row {
	return Row{Title: description, Subtitle: name}
}

// FeatureGroupDescription returns the description after features are loaded.
func FeatureGroupDescription(count int) string {
	return fmt.Sprintf("%d features available", count)
}

// FeaturesEmptyState decides whether the Features page must explain that it
// offers nothing, and with what text. The page is otherwise blank on a host
// with no ublue-os image descriptor (so no Developer or Gaming group), no
// Podman (so no Printers group), and no optional features for updex to list,
// which reads as a broken page rather than an answer. bluefinGroups is
// whether any Developer or Gaming group was built; printers is whether the
// Printers group was built — its rows may all be locked, but a locked switch
// that says why is an offering, not an empty page; optionalFeatures is
// whether the optional-features group is (or may still become) visible — a
// group still checking, or one that could not check, is itself an
// explanation.
func FeaturesEmptyState(bluefinGroups, printers, optionalFeatures bool) (Row, bool) {
	if bluefinGroups || printers || optionalFeatures {
		return Row{}, false
	}
	return Row{
		Title:    "Nothing to set up here",
		Subtitle: "This system does not offer developer tools, gaming apps, printers, or optional features that can be set up from this page.",
	}, true
}

// HelpResources returns configured Help links in their display order with clear action titles.
//
// Each title names what its key holds in the distribution's shipped
// configuration (projectbluefin/common's help_resources_group): website is
// the documentation site and chat is the Ask Bluefin assistant. Titling chat
// as documentation put "Browse documentation" on the assistant and "Visit
// project website" on the documentation. "Ask for help" rather than "Ask
// Bluefin" because the Ask Bluefin menu entry opens Troubleshooting on the
// Agents page, a different destination.
func HelpResources(website, issues, chat string) []HelpResource {
	candidates := []HelpResource{
		{Title: "Browse documentation", URL: website},
		{Title: "Report a problem", URL: issues},
		{Title: "Ask for help", URL: chat},
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

// SystemVersionRow returns the compact system-version row: the version a
// person can quote in a support request or compare against release notes,
// and whether a newer one is already waiting. The exact identifiers stay
// behind SystemVersionDetails, because nobody needs a digest to read their
// own version. staged reports that an update is waiting even when its
// version cannot be read, which a composefs host cannot do without root.
func SystemVersionRow(version, released string, staged bool, stagedVersion string) Row {
	row := Row{Title: "System version"}
	date := formatReleaseDate(released)
	switch {
	case version != "" && date != "":
		row.Subtitle = fmt.Sprintf("You are running version %s, released %s", version, date)
	case version != "":
		row.Subtitle = fmt.Sprintf("You are running version %s", version)
	case date != "":
		row.Subtitle = fmt.Sprintf("You are running the version released %s", date)
	default:
		row.Subtitle = "This system's version could not be read"
	}
	if staged {
		// Two sentences, so both end in a period.
		row.Subtitle = row.Subtitle + ". " + BootcUpdateSubtitle(true, stagedVersion) + "."
	}
	return row
}

// SystemVersionDetails returns the rows behind the System version "Details"
// expander: the identifiers a support request asks for, and the only place
// they appear. A row is omitted when its value is unknown, so the expander
// never shows an empty field. The digest is given whole: a shortened one
// cannot identify a build, and these rows exist to be quoted.
func SystemVersionDetails(version, released, source, digest string) []Row {
	candidates := []Row{
		{Title: "Version", Subtitle: version},
		{Title: "Released", Subtitle: formatReleaseDate(released)},
		{Title: "Source", Subtitle: source},
		{Title: "Build ID", Subtitle: digest},
	}
	rows := make([]Row, 0, len(candidates))
	for _, row := range candidates {
		if row.Subtitle != "" {
			rows = append(rows, row)
		}
	}
	return rows
}

// formatReleaseDate renders an image timestamp as a plain date, or "" when
// it is absent or unparseable.
func formatReleaseDate(timestamp string) string {
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return ""
	}
	return parsed.Local().Format("2 January 2006")
}

// ChannelRow returns the early-updates switch row text. onTesting is the
// running channel; switchable is false when this system publishes no
// counterpart to switch to, in which case the subtitle says the row is inert
// rather than leaving the user to guess.
//
// The subtitle names the two consequences a person cannot discover
// afterwards: the versions are less tested, and taking one replaces the
// operating system and needs a restart.
func ChannelRow(onTesting, switchable bool) Row {
	row := Row{Title: "Get updates early"}
	switch {
	case !switchable:
		row.Subtitle = "This system does not offer early updates"
	case onTesting:
		row.Subtitle = "You get new versions before they are fully tested. Turning this off replaces the operating system with the tested version and needs a restart."
	default:
		row.Subtitle = "Get new versions before they are fully tested. They can be unreliable. Replaces the operating system and needs a restart."
	}
	return row
}

// ChannelSwitchResultSubtitle returns the subtitle after a channel switch
// completes. It never claims the running system changed: the new version is
// only downloaded, so the restart is the part the user still has to do.
func ChannelSwitchResultSubtitle(toTesting bool) string {
	if toTesting {
		return "You will get updates early — restart to apply"
	}
	return "You will get tested updates only — restart to apply"
}

// DeveloperRow returns the developer-tools switch row text. It names the
// capability, never the supplementary groups that carry it: which groups an
// account is added to is an implementation detail of how access is granted,
// where what a person deciding needs is the consequence — an administrator
// password now, and a new login before anything works.
func DeveloperRow(active bool) Row {
	row := Row{Title: "Developer Mode"}
	if active {
		row.Subtitle = "Build projects with containers and virtual machines on your Bluefin workstation. Choose optional tools below."
		return row
	}
	row.Subtitle = "Set up this Bluefin workstation for project development, containers and virtual machines. Needs your administrator password; choose optional tools below."
	return row
}

// DeveloperResultSubtitle returns the subtitle after a developer-tools toggle
// completes. The change only reaches new login sessions, so both outcomes say
// so rather than implying an immediate effect.
func DeveloperResultSubtitle(enabled bool) string {
	if enabled {
		return "Turned on — log out and back in for it to take effect."
	}
	return "Turned off — log out and back in for it to take effect."
}

// GamingCheckingSubtitle is the gaming row's subtitle while ChairLift is still
// finding out what is installed. The switch cannot be used until that answer
// arrives, so the row has to say why.
const GamingCheckingSubtitle = "Checking what is installed…"

// GamingUnavailableSubtitle is shown when that check fails. The underlying
// error names commands and application ids, so it is logged rather than shown.
const GamingUnavailableSubtitle = "Could not check which gaming apps are installed."

// GamingRow returns the gaming switch row text. ready reports whether the
// apps gaming needs are all present (internal/gaming.State.Enabled), and
// installed of total counts the whole set.
func GamingRow(ready bool, installed, total int) Row {
	row := Row{Title: "Gaming Mode"}
	switch {
	case ready && installed >= total:
		row.Subtitle = fmt.Sprintf("All %d gaming components are installed. Select apps to manage below.", total)
	case ready:
		row.Subtitle = fmt.Sprintf("%d of %d gaming components installed. Select apps to manage below.", installed, total)
	case installed > 0:
		row.Subtitle = fmt.Sprintf("%d of %d gaming components installed. Choose which apps to add or remove.", installed, total)
	default:
		row.Subtitle = "Choose the gaming apps to install. Downloads can be large."
	}
	return row
}

// GamingWorkingSubtitle returns the subtitle shown while the gaming apps are
// being installed or removed.
func GamingWorkingSubtitle(enabled bool) string {
	if enabled {
		return "Installing…"
	}
	return "Removing…"
}

// GamingResultSubtitle returns the subtitle after a gaming toggle completes.
// changed is the number of apps installed or removed, and failed the number
// that could not be.
func GamingResultSubtitle(enabled bool, changed, failed int) string {
	if failed > 0 {
		if enabled {
			return fmt.Sprintf("Installed %s, but %d could not be installed.", gamingApps(changed), failed)
		}
		return fmt.Sprintf("Removed %s, but %d could not be removed.", gamingApps(changed), failed)
	}
	if changed == 0 {
		return "Nothing needed changing."
	}
	if enabled {
		return fmt.Sprintf("Installed %s.", gamingApps(changed))
	}
	return fmt.Sprintf("Removed %s.", gamingApps(changed))
}

// gamingApps counts apps in words a person reads, rather than "component(s)".
func gamingApps(count int) string {
	if count == 1 {
		return "1 gaming app"
	}
	return fmt.Sprintf("%d gaming apps", count)
}

// BootcRollbackRow returns the previous-version row text. version and
// timestamp describe the deployment the host would return to; either may be
// empty. The caller shows the row only once bootc reports that deployment,
// so empty arguments mean its details are unreadable, never that nothing is
// kept: a composefs host reads neither the rollback's version label nor its
// image creation time without root (#521).
//
// It is deliberately a single row naming one destination, not a history
// browser: going back has exactly one target — the version the host still
// keeps — so offering a choice would imply a capability the operation does
// not have.
func BootcRollbackRow(version, timestamp string) Row {
	row := Row{Title: "Go back to the previous version"}
	date := formatReleaseDate(timestamp)
	switch {
	case version == "" && date == "":
		row.Subtitle = "Return to the previous version the next time you restart"
	case version == "":
		row.Subtitle = fmt.Sprintf("Return to the version from %s the next time you restart", date)
	case date == "":
		row.Subtitle = fmt.Sprintf("Return to version %s the next time you restart", version)
	default:
		row.Subtitle = fmt.Sprintf("Return to version %s, released %s, the next time you restart", version, date)
	}
	return row
}

// BootcRollbackResultSubtitle returns the subtitle after going back is
// requested. It only changes which version starts next, so it never claims
// the running system changed.
func BootcRollbackResultSubtitle() string {
	return "The previous version starts the next time you restart"
}

// AutomaticUpdatesRow returns the automatic-background-updates switch row
// text. It is one row rather than bluefinctl's strategy picker, schedule
// rows, and per-layer switches, so its subtitle has to carry what the switch
// actually governs.
func AutomaticUpdatesRow(enabled bool) Row {
	row := Row{Title: "Automatic updates"}
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
	return "Automatic updates are off — use Update all when you want to update"
}

// GraphicsDriverRow returns the graphics-driver row text. current is the
// driver the running system carries, hardware describes the detected
// graphics chip, and recommended is non-empty only when a switch is both
// possible and worthwhile.
//
// The row is informational whenever there is nothing to offer, which is the
// common case. When there is something to offer it names the hardware the
// driver is for and the restart it costs, and promises nothing about speed:
// what a driver switch buys varies per machine, and this row cannot know.
func GraphicsDriverRow(current, hardware, recommended string) Row {
	row := Row{Title: "Graphics driver"}
	// gpu.Set.Describe answers with a sentence, not a name, when nothing
	// was detected (a virtual machine); interpolating it produced "for your
	// No graphics hardware detected graphics".
	if hardware == (gpu.Set{}).Describe() {
		hardware = ""
	}
	switch {
	case recommended != "" && hardware != "":
		row.Subtitle = fmt.Sprintf("Switch to the %s driver for your %s graphics. Replaces the operating system and needs a restart.", recommended, hardware)
	case recommended != "":
		row.Subtitle = fmt.Sprintf("Switch to the %s driver. Replaces the operating system and needs a restart.", recommended)
	case current != "" && hardware != "":
		row.Subtitle = fmt.Sprintf("Using the %s driver for your %s graphics", current, hardware)
	case current != "":
		row.Subtitle = fmt.Sprintf("Using the %s driver", current)
	case hardware != "":
		row.Subtitle = fmt.Sprintf("Your graphics hardware: %s", hardware)
	default:
		row.Subtitle = "No graphics hardware was detected"
	}
	return row
}

// GraphicsDriverResultSubtitle returns the subtitle after a driver switch is
// requested. Like a channel switch it only downloads the new version, so it
// never claims the running system changed.
func GraphicsDriverResultSubtitle(driver string) string {
	return fmt.Sprintf("Switched to the %s driver — restart to apply", driver)
}

// Recovery rows. These two are not maintenance — they are what a person
// reaches for when something has already gone wrong — so their text has one
// job: state the real scope. Neither removes "everything", and a row that
// implied it would be talking someone out of the action that would have
// helped them, or into one that does more than they wanted.
//
// The confirmation dialogs below are the authority on irreversibility and
// stay as they are; these rows are the resting text of the page.

// PowerwashRow returns the Powerwash row text.
func PowerwashRow() Row {
	return Row{
		Title:    "Remove Flatpak apps and containers",
		Subtitle: "Removes every Flatpak app and Distrobox container in your account. Homebrew packages, your files, settings, and the system itself stay as they are.",
	}
}

// PowerwashResultSubtitle returns the subtitle after a Powerwash run,
// counted from internal/powerwash.Summarize. A step whose tool is not
// installed had nothing to remove, which is neither success nor failure, so
// a run that skipped everything must not read as though it cleared the
// machine.
func PowerwashResultSubtitle(succeeded, failed int) string {
	switch {
	case failed > 0 && succeeded > 0:
		return "Some apps could not be removed — see the system logs for details"
	case failed > 0:
		return "Nothing could be removed — see the system logs for details"
	case succeeded > 0:
		return "Removed your Flatpak apps and containers"
	default:
		return "There was nothing installed to remove"
	}
}

// PowerwashConfirmation returns the title and body of the confirmation
// dialog Powerwash must show before it runs, since removing every installed
// application and container cannot be undone from within ChairLift.
func PowerwashConfirmation() (title, body string) {
	return "Remove Flatpak Apps and Containers?",
		"This removes every Flatpak application and every Distrobox container " +
			"for your account. It does not touch the system image or your files. " +
			"This cannot be undone."
}

// FactoryResetRow returns the Factory Reset row text.
func FactoryResetRow() Row {
	return Row{
		Title:    "Reset the system",
		Subtitle: "Reinstalls the system from scratch and discards changes made to it. Your files and apps stay where they are. Takes effect after a restart.",
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

// KVMAccessNeedsNewLogin is shown after virtualization access was granted to
// an account whose current session cannot use it yet. devtools.ErrNewLogin's
// own text is a Go error string, lower-case and phrased for a log.
const KVMAccessNeedsNewLogin = "Virtualization access granted. Log out and back in, then turn on WSL Mode again."
