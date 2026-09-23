// Package actionmsg builds the toast text (and, where the action itself is
// gated by dry-run, the execution decision) for maintenance-page,
// applications-page, updates-page, and features-page actions: Homebrew
// Brewfile dumps and installs, Homebrew/Flatpak cleanup, Homebrew package
// installs/uninstalls/pins/upgrades/self-updates, Flatpak application
// uninstalls/updates, Homebrew tap trust, bootc system update staging,
// configured custom maintenance scripts, and system feature toggles/updates.
//
// It is deliberately free of any puregotk/GTK import, following the
// internal/views/trustmsg pattern, so its logic can be unit-tested on a
// headless host. A test binary for a package that imports puregotk panics
// while resolving GTK/graphene shared libraries at package init — before any
// test function runs — so logic that must be tested cannot live in the view
// packages. See docs/agents/skills/gtk-headless-tests.md.
//
// Functions whose result only selects display text (BundleDump, Cleanup,
// Install, Uninstall, Pin, Upgrade, Update, SelfUpdate, SystemStage,
// FeatureUpdate) return a plain string: the state-changing/no-op decision for
// those actions is already made and already tested inside their wrapper
// package (internal/homebrew, internal/flatpak, internal/bootc,
// internal/updex).
// Functions whose result gates a further decision return a decision struct,
// precisely so the gated decision — not just the wording of the toast that
// follows it — is what a table-driven test in actionmsg_test.go asserts.
// MaintenanceScript decides whether a configured custom script executes at
// all. BundleInstall decides whether its row becomes permanently installed
// or resets after a dry-run preview. TapTrust decides whether the Untrusted
// Homebrew Taps view removes a row, changes group visibility, and refreshes.
// FeatureToggle decides whether a feature switch confirms its new visual
// state. Those latter three UI-side decisions have no wrapper-package
// equivalent even though the wrappers already gate their underlying
// command.
package actionmsg

import "fmt"

// BundleDump returns the toast text for exporting the installed package list.
// When dryRun is true, homebrew.BundleDump itself never runs `brew bundle
// dump` (bundle is one of homebrew's stateChangingCommands, skipped entirely
// under dry-run) so nothing is written, and the toast must say so rather than
// unconditionally claiming the list was saved. The destination is not named:
// it is a fixed file in the person's home folder, and a path in a toast is
// something they can neither act on nor change.
func BundleDump(dryRun bool) string {
	if dryRun {
		return "[DRY-RUN] Preview: your package list would be exported to your home folder — no changes made"
	}
	return "Package list exported to your home folder"
}

// BundleInstallDecision is the result of installing one app collection.
// Complete is false under dry-run because homebrew.BundleInstall skipped the
// command; the row must become clickable again instead of claiming the
// collection is installed.
type BundleInstallDecision struct {
	Complete bool
	Toast    string
}

// BundleInstall decides whether a successful wrapper return represents a real
// completed install and supplies the corresponding toast. The caller must use
// Complete for both its InstallGate transition and button state. name is the
// collection's display title, never its identifier on disk.
func BundleInstall(dryRun bool, name string) BundleInstallDecision {
	if dryRun {
		return BundleInstallDecision{
			Complete: false,
			Toast:    fmt.Sprintf("[DRY-RUN] Preview: %s would be installed — no changes made", name),
		}
	}
	return BundleInstallDecision{
		Complete: true,
		Toast:    fmt.Sprintf("%s installed", name),
	}
}

// Install returns the toast text for a Homebrew package install. The
// wrapper package (internal/homebrew) already skips the state-changing
// `brew install` command under dry-run — install is one of homebrew's
// stateChangingCommands — so this function only selects which string to
// show: a preview when dryRun is true, or a fixed completion message when
// the install actually ran.
func Install(dryRun bool, pkgName string) string {
	if dryRun {
		return fmt.Sprintf("[DRY-RUN] Preview: %s would be installed — no changes made", pkgName)
	}
	return fmt.Sprintf("%s installed", pkgName)
}

// Uninstall returns toast text for a Homebrew package or Flatpak application
// uninstall. Both wrappers skip their state-changing uninstall command under
// dry-run.
func Uninstall(dryRun bool, name string) string {
	if dryRun {
		return fmt.Sprintf("[DRY-RUN] Preview: %s would be uninstalled — no changes made", name)
	}
	return fmt.Sprintf("%s uninstalled", name)
}

// Pin returns toast text for a formula pin or unpin.
func Pin(dryRun bool, name string, pin bool) string {
	action := "unpinned"
	if pin {
		action = "pinned"
	}
	if dryRun {
		return fmt.Sprintf("[DRY-RUN] Preview: %s would be %s — no changes made", name, action)
	}
	return fmt.Sprintf("%s %s", name, action)
}

// Upgrade returns the toast text for a per-package Homebrew upgrade. The
// wrapper package (internal/homebrew) already skips the state-changing
// `brew upgrade` command under dry-run — upgrade is one of homebrew's
// stateChangingCommands — so this function only selects which string to
// show: a preview when dryRun is true, or a fixed completion message when
// the upgrade actually ran.
func Upgrade(dryRun bool, pkgName string) string {
	if dryRun {
		return fmt.Sprintf("[DRY-RUN] Preview: %s would be upgraded — no changes made", pkgName)
	}
	return fmt.Sprintf("%s upgraded", pkgName)
}

// Update returns the toast text for a per-app Flatpak update. The wrapper
// package (internal/flatpak) already skips the state-changing
// `flatpak update` command under dry-run, so this function only selects
// which string to show: a preview when dryRun is true, or a fixed completion
// message when the update actually ran.
func Update(dryRun bool, appID string) string {
	if dryRun {
		return fmt.Sprintf("[DRY-RUN] Preview: %s would be updated — no changes made", appID)
	}
	return fmt.Sprintf("%s updated", appID)
}

// SelfUpdate returns the toast text for a package manager self-update (e.g.
// Homebrew's own `brew update`). The wrapper package already skips the
// state-changing update command under dry-run, so this function only selects
// which string to show: a preview when dryRun is true, or a fixed completion
// message when the update actually ran.
func SelfUpdate(dryRun bool, tool string) string {
	if dryRun {
		return fmt.Sprintf("[DRY-RUN] Preview: %s would be updated — no changes made", tool)
	}
	return fmt.Sprintf("%s updated successfully", tool)
}

// SystemStage returns the completion toast for either bootc or native A/B
// staging. Both providers skip pkexec under dry-run, so a status re-read then
// describes existing system state, not work done by this click. Always report
// a preview under dry-run; retain the staged and current live messages.
func SystemStage(dryRun bool, staged bool) string {
	if dryRun {
		return "[DRY-RUN] Preview: no changes made — system state was not checked or modified by this click"
	}
	if staged {
		return "System update staged. Restart to apply."
	}
	return "System is up to date"
}

// TapTrustDecision is the result of deciding whether trusting a Homebrew tap
// should mutate the Untrusted Homebrew Taps UI (remove the tap's row, hide
// the group when empty, refresh outdated packages), and what toast to show
// for that decision.
type TapTrustDecision struct {
	// MutateUI is true when the tap was actually trusted (homebrew.
	// TrustPackages ran `brew trust` for real) and the Untrusted Homebrew
	// Taps UI should reflect that: remove the row, hide the group if empty,
	// and refresh outdated packages. It is exactly !dryRun — under dry-run,
	// TrustPackages's underlying `brew trust` call never runs (trust is one
	// of homebrew's stateChangingCommands), so the tap is not actually
	// trusted and the UI must not act as though it were.
	MutateUI bool
	// Toast is the completion message to show immediately.
	Toast string
}

// TapTrust decides whether trusting a Homebrew tap (trustTap in
// internal/views/updates_page.go, following a successful
// homebrew.TrustPackages call) should mutate the Untrusted Homebrew Taps UI,
// and what toast to show. MutateUI is exactly !dryRun; the caller must not
// independently recompute that condition. Under dry-run, TrustPackages's
// `brew trust` call is skipped entirely by homebrew's stateChangingCommands
// gate, so nothing was actually trusted — removing the row, hiding the
// group, or refreshing outdated packages would make the tap disappear from
// the untrusted list as if it were now trusted, with no way to undo it from
// the UI. This function is what actionmsg_test.go asserts on, precisely so
// that decision — not just the wording of the toast that follows it — is
// tested.
func TapTrust(dryRun bool, tapName string) TapTrustDecision {
	if dryRun {
		return TapTrustDecision{
			MutateUI: false,
			Toast:    fmt.Sprintf("[DRY-RUN] Preview: %s would be trusted — no changes made", tapName),
		}
	}
	return TapTrustDecision{
		MutateUI: true,
		Toast:    fmt.Sprintf("Trusted %s. Its packages can update again.", tapName),
	}
}

// ScriptDecision is the result of deciding whether a configured custom
// maintenance script should actually execute, and what toast to show for
// that decision.
type ScriptDecision struct {
	// Execute is true when the script should actually be run (cmd.Run()
	// invoked, whether direct or via pkexec). It is false under dry-run, in
	// which case no exec.Cmd may be constructed or run at all.
	Execute bool
	// Toast is the completion message to show immediately (dry-run) or once
	// the script's cmd.Run() returns successfully (live run).
	Toast string
}

// MaintenanceScript decides whether a configured custom maintenance script
// (config.yml's `actions` entries, run by runMaintenanceAction in
// internal/views/maintenance_page.go) should execute. Custom scripts have no
// wrapper package of their own to gate their execution the way homebrew,
// flatpak, bootc, and updex do, so this is the one place that decision is
// made and tested. Execute is exactly !dryRun; the caller must not
// independently recompute that condition.
func MaintenanceScript(dryRun bool, title string) ScriptDecision {
	if dryRun {
		return ScriptDecision{
			Execute: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: %s would run — no changes made", title),
		}
	}
	return ScriptDecision{
		Execute: true,
		Toast:   fmt.Sprintf("%s completed", title),
	}
}

// FeatureToggleDecision is the result of deciding whether toggling a system
// feature's switch (onFeatureToggled in internal/views/features_page.go,
// following a successful updex.EnableFeature/DisableFeature call) should
// confirm the switch's new visual state, and what toast to show for that
// decision.
type FeatureToggleDecision struct {
	// Confirm is true when the feature was actually enabled/disabled
	// (updex.runHelper invoked pkexec for real) and the switch should
	// confirm the flip the user just made. It is exactly !dryRun — under
	// dry-run, updex.runHelper returns nil before ever invoking pkexec, so
	// nothing was actually toggled and the switch must not visually confirm
	// a change that did not happen.
	Confirm bool
	// Toast is the completion message to show immediately.
	Toast string
}

// FeatureToggle decides whether toggling a system feature's switch should
// confirm its new state, and what toast to show. Confirm is exactly
// !dryRun; the caller must not independently recompute that condition.
// Under dry-run, updex.EnableFeature/DisableFeature's underlying pkexec call
// is skipped entirely by updex.runHelper's own dry-run short-circuit, so
// nothing was actually enabled or disabled — confirming the switch would
// make it look toggled with no way to tell the user it did not really
// change. This function is what actionmsg_test.go asserts on, precisely so
// that decision — not just the wording of the toast that follows it — is
// tested.
func FeatureToggle(dryRun bool, enable bool, name string) FeatureToggleDecision {
	if dryRun {
		verb := "enabled"
		if !enable {
			verb = "disabled"
		}
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: %s would be %s — no changes made", name, verb),
		}
	}
	if enable {
		return FeatureToggleDecision{
			Confirm: true,
			Toast:   fmt.Sprintf("%s enabled. Update to download, reboot to apply.", name),
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   fmt.Sprintf("%s disabled. Update to apply, reboot to complete.", name),
	}
}

// FeatureUpdate returns the toast text for the Features page's "Update"
// button (onUpdateFeaturesClicked). The wrapper package (internal/updex)
// already skips the state-changing update via updex.runHelper's own
// dry-run short-circuit before pkexec, so this function only selects which
// string to show: a preview when dryRun is true, or a fixed completion
// message when the update actually ran. The button's own SetSensitive/
// SetLabel reset is unconditional and unaffected by dryRun.
func FeatureUpdate(dryRun bool) string {
	if dryRun {
		return "[DRY-RUN] Preview: features would be updated — no changes made"
	}
	return "Features updated. Changes apply after reboot."
}

// ChannelSwitch decides whether the Testing Channel switch should confirm
// its new state, and what toast to show. Confirm is exactly !dryRun, for the
// same reason as FeatureToggle: under dry-run, ublue.runHelper short-circuits
// before pkexec, so nothing was staged and confirming the switch would show
// a channel change that did not happen.
func ChannelSwitch(dryRun bool, toTesting bool) FeatureToggleDecision {
	channel := "stable"
	if toTesting {
		channel = "testing"
	}
	if dryRun {
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: would switch to the %s channel — no changes made", channel),
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   fmt.Sprintf("Switched to the %s channel. Restart to apply.", channel),
	}
}

// DeveloperMode decides whether the Developer Mode switch should confirm its
// new state, and what toast to show. The live toasts name the re-login
// requirement because supplementary group membership only takes effect in a
// new session — the switch flipping is not the whole story.
func DeveloperMode(dryRun bool, enable bool) FeatureToggleDecision {
	verb := "disabled"
	if enable {
		verb = "enabled"
	}
	if dryRun {
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: developer mode would be %s — no changes made", verb),
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   fmt.Sprintf("Developer mode %s. Log out and back in to apply.", verb),
	}
}

// GamingMode decides whether the Gaming Mode switch should confirm its new
// state, and what toast to show.
//
// Gaming mode differs from the other two toggles in one way that matters
// here: it installs user-scope Flatpaks one at a time, so a live run can
// partly succeed. Confirm therefore is not simply !dryRun — a live run that
// changed nothing, or whose every component failed, must not confirm either.
//
// skipped counts components the image preinstalled system-wide, which
// removal leaves alone. Those are neither a change nor a failure, so they
// are reported separately: telling a user "0 removed" with no explanation
// when the components are still visibly installed is the confusing outcome.
func GamingMode(dryRun bool, enable bool, changed, failed, skipped int) FeatureToggleDecision {
	verb := "removed"
	if enable {
		verb = "installed"
	}

	if dryRun {
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: gaming components would be %s — no changes made", verb),
		}
	}

	suffix := ""
	if skipped > 0 {
		suffix = fmt.Sprintf(" %d component(s) installed system-wide were left in place.", skipped)
	}

	switch {
	case changed == 0 && failed > 0:
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("No gaming components could be %s (%d failed).%s", verb, failed, suffix),
		}
	case changed == 0 && skipped > 0:
		// Nothing was removed, but only because everything present belongs
		// to the system image. The switch must not claim gaming mode is off.
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("Nothing to remove.%s", suffix),
		}
	case changed == 0:
		return FeatureToggleDecision{
			Confirm: true,
			Toast:   "Gaming mode is already in the requested state.",
		}
	case failed > 0:
		return FeatureToggleDecision{
			Confirm: true,
			Toast:   fmt.Sprintf("%d gaming component(s) %s, %d failed.%s", changed, verb, failed, suffix),
		}
	default:
		return FeatureToggleDecision{
			Confirm: true,
			Toast:   fmt.Sprintf("%d gaming component(s) %s.%s", changed, verb, suffix),
		}
	}
}

// Rollback decides whether the rollback row should adopt its
// rolled-back subtitle, and what toast to show. Confirm is exactly !dryRun,
// for the same reason as FeatureToggle: under dry-run ublue.runHelper
// short-circuits before pkexec, so no deployment was reordered and claiming
// otherwise would misreport what will boot next.
func Rollback(dryRun bool) FeatureToggleDecision {
	if dryRun {
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   "[DRY-RUN] Preview: would roll back to the previous system image — no changes made",
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   "Rolled back. Restart to boot the previous image.",
	}
}

// FactoryReset decides whether the factory-reset row should adopt its
// applied subtitle, and what toast to show. Confirm is exactly !dryRun, the
// same reasoning as Rollback: under dry-run nothing was staged.
func FactoryReset(dryRun bool) FeatureToggleDecision {
	if dryRun {
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   "[DRY-RUN] Preview: would factory reset this system — no changes made",
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   "Factory reset applied. Restart to complete it.",
	}
}

// Powerwash decides whether the Powerwash row should show its outcome, and
// what toast to display. Unlike Rollback and FactoryReset, Powerwash runs
// two independent, unprivileged steps that can each succeed, fail, or be
// skipped (a tool not installed), so Confirm is not simply !dryRun — a live
// run in which nothing was actually removed must not claim success, the same
// distinction internal/views/actionmsg.GamingMode already makes.
func Powerwash(dryRun bool, succeeded, failed int) FeatureToggleDecision {
	if dryRun {
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   "[DRY-RUN] Preview: would remove your Flatpaks and Distrobox containers — no changes made",
		}
	}
	switch {
	case succeeded == 0 && failed == 0:
		return FeatureToggleDecision{Confirm: true, Toast: "Nothing was installed to remove."}
	case failed > 0 && succeeded == 0:
		return FeatureToggleDecision{Confirm: false, Toast: "Powerwash failed — nothing was removed."}
	case failed > 0:
		return FeatureToggleDecision{Confirm: true, Toast: "Powerwash finished with problems."}
	default:
		return FeatureToggleDecision{Confirm: true, Toast: "Powerwash complete."}
	}
}

// AutomaticUpdates decides whether the automatic-updates switch should adopt
// its new state, and what toast to show. Confirm is exactly !dryRun: under
// dry-run ublue.runHelper short-circuits before pkexec, so the timer was
// never touched and confirming would leave the switch disagreeing with
// systemd.
func AutomaticUpdates(dryRun bool, enable bool) FeatureToggleDecision {
	verb := "off"
	if enable {
		verb = "on"
	}
	if dryRun {
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: automatic updates would be turned %s — no changes made", verb),
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   fmt.Sprintf("Automatic updates turned %s.", verb),
	}
}

// DriverSwitch decides whether the graphics-driver row should adopt its
// switched subtitle, and what toast to show. Confirm is exactly !dryRun, for
// the same reason as ChannelSwitch: under dry-run ublue.runHelper
// short-circuits before pkexec, so no image was staged.
func DriverSwitch(dryRun bool, driver string) FeatureToggleDecision {
	if dryRun {
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: would switch to the %s image — no changes made", driver),
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   fmt.Sprintf("Switched to the %s image. Restart to apply.", driver),
	}
}

// AIStack returns the toast for the local-AI switch. It no longer names the
// compute stack the hardware selected: "CUDA" confirmed nothing a person
// could act on, and the selected stack is on the Agents page's Details row
// for anyone it does mean something to.
func AIStack(dryRun bool, enable bool) FeatureToggleDecision {
	if dryRun {
		verb := "stopped"
		if enable {
			verb = "started"
		}
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: local AI would be %s — no changes made", verb),
		}
	}

	if enable {
		return FeatureToggleDecision{
			Confirm: true,
			Toast:   "Local AI is starting. The first model download runs in the background.",
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   "Local AI stopped. The model it downloaded was kept.",
	}
}
