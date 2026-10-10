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

import (
	"fmt"
	"strings"

	"github.com/projectbluefin/chairlift/internal/pkexec"
)

// BundleDump returns the toast text for exporting the installed package list.
// When dryRun is true, homebrew.BundleDump itself never runs `brew bundle
// dump` (bundle is one of homebrew's stateChangingCommands, skipped entirely
// under dry-run) so nothing is written, and the toast must say so rather than
// unconditionally claiming the list was saved. The destination is not named:
// it is a fixed file in the person's home folder, and a path in a toast is
// something they can neither act on nor change.
func BundleDump(dryRun bool) string {
	if dryRun {
		return "[DRY-RUN] Preview: your app list would be saved to your home folder — no changes made"
	}
	return "Saved your app list to your home folder."
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

// Uninstall returns toast text for a Homebrew package uninstall.
// The wrapper skips the state-changing command under dry-run.
func Uninstall(dryRun bool, name string) string {
	if dryRun {
		return fmt.Sprintf("[DRY-RUN] Preview: %s would be uninstalled — no changes made", name)
	}
	return fmt.Sprintf("%s uninstalled", name)
}

// UninstallFailure returns the error toast for a failed Homebrew uninstall.
// When Homebrew refused because installed packages still need it,
// dependents names them: a bare "Could not uninstall" left a retry that
// fails the same way as the only thing to try. Any other failure keeps its
// detail in the log.
func UninstallFailure(name string, dependents []string) string {
	switch len(dependents) {
	case 0:
		return fmt.Sprintf("Could not uninstall %s", name)
	case 1:
		return fmt.Sprintf("Could not uninstall %s because %s needs it", name, dependents[0])
	default:
		last := len(dependents) - 1
		return fmt.Sprintf("Could not uninstall %s because %s and %s need it",
			name, strings.Join(dependents[:last], ", "), dependents[last])
	}
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

// SystemStage returns the completion toast for bootc staging. Staging skips
// pkexec under dry-run, so a status re-read then describes existing system
// state, not work done by this click. Always report a preview under dry-run;
// retain the staged and current live messages.
func SystemStage(dryRun bool, staged bool) string {
	if dryRun {
		return "[DRY-RUN] Preview: no changes made — system state was not checked or modified by this click"
	}
	if staged {
		return "Update downloaded. Restart to install it."
	}
	return "System is up to date"
}

// UpdateAllPreview is the toast for an Update all run that only previewed
// its sources (dry-run). The panel returns to "Updates available" because
// nothing was installed, which without this toast reads like a failed or
// silently ignored run; a live run reports through its completion
// notification and the panel instead.
const UpdateAllPreview = "[DRY-RUN] Preview: updates would be installed — no changes made"

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
		Toast:    fmt.Sprintf("Trusted %s. Its software can update again.", tapName),
	}
}

// PackageTrustDecision is the result of trusting one package from an
// untrusted tap (the per-package Trust button next to each formula/cask
// inside the Manage source trust group). MutateUI is true when the package
// was actually trusted (homebrew.TrustFormula/TrustCask ran `brew trust` for
// real) and the per-package row should disappear; it is exactly !dryRun,
// following the same shape TapTrust uses for the broader trust path.
type PackageTrustDecision struct {
	MutateUI bool
	Toast    string
}

// PackageTrust decides whether trusting one Homebrew package (the
// per-package button inside a tap's expander row) should mutate the Manage
// source trust UI: remove the now-trusted package's row, and remove the tap
// itself once its last package is trusted. It mirrors TapTrust but for the
// finer-grained action; MutateUI is exactly !dryRun, and under dry-run the
// package is not actually trusted so the row must not vanish.
func PackageTrust(dryRun bool, packageName string) PackageTrustDecision {
	if dryRun {
		return PackageTrustDecision{
			MutateUI: false,
			Toast:    fmt.Sprintf("[DRY-RUN] Preview: %s would be trusted — no changes made", packageName),
		}
	}
	return PackageTrustDecision{
		MutateUI: true,
		Toast:    fmt.Sprintf("Trusted %s. It can update again.", packageName),
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

// MaintenanceScriptFailure words a failed configured maintenance script.
// A sudo script runs through pkexec, and pkexec exits 126 when the user
// dismissed the authentication prompt; the script never ran, so that is the
// brief "Authentication cancelled" toast (isError false) every other
// privileged view shows, not a pinned "failed: exit status 126". The script
// runner captures no stderr, so the exit status is the only signal. A script
// run without pkexec owns its own exit statuses, so its 126 stays a failure.
func MaintenanceScriptFailure(title string, sudo bool, err error) (text string, isError bool) {
	if sudo && pkexec.IsAuthDismissed(err) {
		return pkexec.CancelledMessage, false
	}
	return fmt.Sprintf("%s failed: %v", title, err), true
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
			Toast:   fmt.Sprintf("%s turned on. Update, then restart to use it.", name),
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   fmt.Sprintf("%s turned off. Update, then restart to finish.", name),
	}
}

// LiveryDecision is the result of deciding whether a finished Livery action —
// a section master switch (LiveryToggle), a mark selection (LiverySelection),
// or a rotate-at-login switch (LiveryRotation), all in
// internal/views/livery_actions.go — may advance the page's own view of that
// section, and what toast to show for the decision.
type LiveryDecision struct {
	// MutateUI is true when the new value was really persisted and the page
	// may mirror it: advance the section's in-memory state, change its
	// sub-rows, and leave the control where the user put it. It is exactly
	// !dryRun — under dry-run livery.SetBool and livery.SetString return
	// before touching gsettings (internal/livery/settings.go), so the stored
	// value never moved and the page must neither mirror nor display a
	// change that did not happen. The caller must not independently
	// recompute that condition: the same decision value drives the mirror,
	// the control restore, and the toast below.
	MutateUI bool
	// Toast is the message to show once the action's work has finished. It
	// is empty on a live run, where the changed control is its own
	// confirmation and the page has never shown a toast for one; only the
	// preview needs words, because the control visibly springs back.
	Toast string
}

// LiveryToggle decides whether a Livery section master switch may advance the
// page's visible and in-memory state, and supplies the preview toast when it
// may not. MutateUI is exactly !dryRun. name is the section's display name
// (pageview.LiverySectionName), never a livery.Surface value or a gsettings
// key.
func LiveryToggle(dryRun bool, enable bool, name string) LiveryDecision {
	if dryRun {
		return LiveryDecision{Toast: liveryPreview(name + " would be " + liveryTurned(enable))}
	}
	return LiveryDecision{MutateUI: true}
}

// LiverySelection decides whether a brand, project, foundation mark, or
// custom-file pick may replace the section's confirmed selection. Under
// dry-run the chooser closes and the row keeps its old value, so the preview
// toast is what says the pick was understood. name is as for LiveryToggle.
func LiverySelection(dryRun bool, name string) LiveryDecision {
	if dryRun {
		return LiveryDecision{Toast: liveryPreview(name + " would change")}
	}
	return LiveryDecision{MutateUI: true}
}

// LiveryRotation decides whether a rotate-at-login switch may advance the
// confirmed rotation pair. Under dry-run the switch springs back after its
// spinner, so the preview toast says what would have been scheduled. name is
// as for LiveryToggle.
func LiveryRotation(dryRun bool, enable bool, name string) LiveryDecision {
	if dryRun {
		return LiveryDecision{Toast: liveryPreview("rotating " + name + " at login would be " + liveryTurned(enable))}
	}
	return LiveryDecision{MutateUI: true}
}

func liveryTurned(enable bool) string {
	if enable {
		return "turned on"
	}
	return "turned off"
}

func liveryPreview(what string) string {
	return fmt.Sprintf("[DRY-RUN] Preview: %s — no changes made", what)
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
	return "Features updated. Restart to apply."
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
	state := "off"
	if toTesting {
		state = "on"
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   fmt.Sprintf("Early updates are %s. Restart to finish.", state),
	}
}

// DeveloperMode decides whether the Developer Mode switch should confirm its
// new state, and what toast to show. The live toasts name the re-login
// requirement because supplementary group membership only takes effect in a
// new session — the switch flipping is not the whole story.
//
// skipped lists the developer groups the helper reported it could not
// change. An enable that skipped some is a partial grant: it still confirms
// (the account did join the others) and its toast names each group not
// granted, so the user does not assume access to Docker or Incus they lack
// (#495). The toast is an ordinary one, not a persistent error: on images
// that ship neither group (Dakota) every enable skips them, and a persistent
// "enabled" banner outlived a later disable. An ordinary toast's title is a
// single ellipsized line, so the note stays short enough that the group
// names are not cut off.
func DeveloperMode(dryRun bool, enable bool, skipped []string) FeatureToggleDecision {
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
	if enable && len(skipped) > 0 {
		return FeatureToggleDecision{
			Confirm: true,
			Toast:   fmt.Sprintf("Developer Mode is on (no %s). Log out and back in.", strings.Join(skipped, ", ")),
		}
	}
	if enable {
		return FeatureToggleDecision{Confirm: true, Toast: "Developer Mode turned on. Log out and back in to use it."}
	}
	return FeatureToggleDecision{Confirm: true, Toast: "Developer Mode turned off. Log out and back in to finish."}
}

// GamingMode decides whether the Gaming Mode switch should confirm its new
// state, and what toast to show.
//
// Gaming mode differs from the other two toggles in one way that matters
// here: it installs or removes Flatpaks one at a time, so a live run can
// partly succeed. Confirm therefore is not simply !dryRun — a live run that
// changed nothing, or whose every component failed, must not confirm either.
//
// kept counts selected components whose system copy came with the OS image,
// which removal leaves in place. Those are neither a change nor a failure, so
// they are reported separately: "0 removed" with no explanation, while the
// components are still visibly installed, is the confusing outcome.
func GamingMode(dryRun bool, enable bool, changed, failed, kept int) FeatureToggleDecision {
	verb := "removed"
	if enable {
		verb = "installed"
	}

	if dryRun {
		// A preview confirms nothing, but it still reports what the run
		// would do: a removal whose every component came with the image
		// removes nothing, and saying "would be removed" there promises a
		// change the live run will not make.
		var toast string
		switch {
		case changed == 0 && failed > 0:
			toast = fmt.Sprintf("[DRY-RUN] Preview: no gaming components could be %s (%d failed) — no changes made", verb, failed)
		case changed == 0 && kept > 0:
			toast = fmt.Sprintf("[DRY-RUN] Preview: nothing to remove — %d component(s) that came with the system would be left in place", kept)
		case changed == 0:
			toast = "[DRY-RUN] Preview: gaming mode is already in the requested state — no changes made"
		default:
			toast = fmt.Sprintf("[DRY-RUN] Preview: gaming components would be %s — no changes made", verb)
			if kept > 0 {
				toast += fmt.Sprintf(". %d component(s) that came with the system would be left in place", kept)
			}
		}
		return FeatureToggleDecision{Confirm: false, Toast: toast}
	}

	suffix := ""
	if kept > 0 {
		suffix = " Apps installed for everyone were kept."
	}
	done, base := "Removed", "remove"
	if enable {
		done, base = "Installed", "install"
	}

	switch {
	case changed == 0 && failed > 0:
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("Couldn't %s any gaming apps. Try again.%s", base, suffix),
		}
	case changed == 0 && kept > 0:
		// Nothing was removed, only because everything selected came with
		// the image. The switch must not claim gaming mode is off.
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("Nothing to remove.%s", suffix),
		}
	case changed == 0:
		return FeatureToggleDecision{
			Confirm: true,
			Toast:   "Nothing needed changing.",
		}
	case failed > 0:
		return FeatureToggleDecision{
			Confirm: true,
			Toast:   fmt.Sprintf("%s %s. %d couldn't be %s.%s", done, gamingAppCount(changed), failed, verb, suffix),
		}
	default:
		return FeatureToggleDecision{
			Confirm: true,
			Toast:   fmt.Sprintf("%s %s.%s", done, gamingAppCount(changed), suffix),
		}
	}
}

func gamingAppCount(n int) string {
	if n == 1 {
		return "1 gaming app"
	}
	return fmt.Sprintf("%d gaming apps", n)
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
			Toast:   "[DRY-RUN] Preview: would go back to the previous version — no changes made",
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   "Restart to use the previous version.",
	}
}

// FactoryReset decides whether the factory-reset row should adopt its
// applied subtitle, and what toast to show. Confirm is exactly !dryRun, the
// same reasoning as Rollback: under dry-run nothing was staged.
func FactoryReset(dryRun bool) FeatureToggleDecision {
	if dryRun {
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   "[DRY-RUN] Preview: would reset this computer — no changes made",
		}
	}
	return FeatureToggleDecision{
		Confirm: true,
		Toast:   "Restart to finish resetting this computer.",
	}
}

// Powerwash decides whether the Powerwash row should show its outcome, and
// what toast to display. Unlike Rollback and FactoryReset, Powerwash runs
// two independent, unprivileged steps that can each succeed, fail, or be
// skipped (a tool not installed, or nothing in the account), so Confirm is
// not simply !dryRun — a live run in which nothing was actually removed must
// not claim success, the same distinction internal/views/actionmsg.GamingMode
// already makes. A preview reads the same inventory, so it previews only a
// removal that would happen.
func Powerwash(dryRun bool, succeeded, failed int) FeatureToggleDecision {
	if dryRun {
		toast := "[DRY-RUN] Preview: would remove your Flatpak apps and containers — no changes made"
		switch {
		case failed > 0:
			toast = "[DRY-RUN] Preview: could not read everything Powerwash would remove — no changes made"
		case succeeded == 0:
			toast = "[DRY-RUN] Preview: nothing is installed to remove — no changes made"
		}
		return FeatureToggleDecision{Confirm: false, Toast: toast}
	}
	switch {
	case succeeded == 0 && failed == 0:
		return FeatureToggleDecision{Confirm: true, Toast: "There was nothing to remove."}
	case failed > 0 && succeeded == 0:
		return FeatureToggleDecision{Confirm: false, Toast: "Couldn't remove anything. Try again."}
	case failed > 0:
		return FeatureToggleDecision{Confirm: true, Toast: "Some apps or containers couldn't be removed. Try again."}
	default:
		return FeatureToggleDecision{Confirm: true, Toast: "Removed your Flatpak apps and containers."}
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
		Toast:   fmt.Sprintf("Switched to the %s driver. Restart to finish.", driver),
	}
}

// AgentMode returns the decision for the Agent Mode switch. A dry run never
// confirms: nothing was installed, written, or started.
func AgentMode(dryRun bool, enable bool) FeatureToggleDecision {
	if dryRun {
		verb := "turned off"
		if enable {
			verb = "turned on"
		}
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: Agent Mode would be %s — no changes made", verb),
		}
	}
	if enable {
		return FeatureToggleDecision{Confirm: true, Toast: "Agent Mode is on."}
	}
	return FeatureToggleDecision{Confirm: true, Toast: "Agent Mode is off. Your models were kept."}
}

// PrinterApp returns the decision for one printer application family's
// switch on the Features page, after printerapp.Enable or Disable returned
// nil. rowTitle is the row's own title so three families' toasts read apart.
// A dry run never confirms: no quadlet was written, started, stopped, or
// removed. A live disable says the driver image and the printer's settings
// were kept, because they were — printerapp.Disable removes only the unit.
func PrinterApp(dryRun bool, enable bool, rowTitle string) FeatureToggleDecision {
	if dryRun {
		verb := "turned off"
		if enable {
			verb = "turned on"
		}
		return FeatureToggleDecision{
			Confirm: false,
			Toast:   fmt.Sprintf("[DRY-RUN] Preview: %s would be %s — no changes made", rowTitle, verb),
		}
	}
	if enable {
		return FeatureToggleDecision{Confirm: true, Toast: rowTitle + " is on."}
	}
	return FeatureToggleDecision{Confirm: true, Toast: rowTitle + " is off. Your printer settings were kept."}
}

// AskBluefinMenuDecision is the outcome of flipping the Agents page's "Show
// Ask Bluefin in menu" switch.
type AskBluefinMenuDecision struct {
	// Active is the position the switch must show: the observed visibility
	// when it was read back, otherwise the last known one (!requested).
	Active bool
	// Toast is empty when the requested change was observed applied.
	Toast string
	// Error selects an error toast rather than an ordinary one.
	Error bool
}

// AskBluefinMenu decides what the Ask Bluefin menu switch shows after
// devmenu.SetAskBluefinVisible (err) and the read-back
// devmenu.AskBluefinState (observed, readErr). A preview says nothing
// changed, and a write that did not take effect is reported rather than
// silently snapping the switch back.
func AskBluefinMenu(dryRun, requested, observed bool, err, readErr error) AskBluefinMenuDecision {
	switch {
	case readErr != nil && err != nil:
		return AskBluefinMenuDecision{Active: !requested, Toast: "Could not update the Ask Bluefin menu entry.", Error: true}
	case readErr != nil:
		return AskBluefinMenuDecision{Active: !requested, Toast: "Could not verify the Ask Bluefin menu entry.", Error: true}
	case err != nil:
		return AskBluefinMenuDecision{Active: observed, Toast: "Could not update the Ask Bluefin menu entry.", Error: true}
	case dryRun:
		return AskBluefinMenuDecision{Active: observed, Toast: "Preview only — the menu entry was not changed."}
	case observed != requested:
		return AskBluefinMenuDecision{Active: observed, Toast: "The Ask Bluefin menu entry did not change.", Error: true}
	}
	return AskBluefinMenuDecision{Active: observed}
}
