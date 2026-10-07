// Package updatepresent owns the pure presentation decisions for the unified
// update shell.
package updatepresent

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/leonelquinteros/gotext"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/userprefs"
)

// Presentation is the aggregate status shown by the update shell's header:
// the wordmark, then the primary action, then one line of supporting text.
type Presentation struct {
	// Status is the one line of supporting text under the primary action.
	// Every phase sets it, and it is also what a screen reader hears when
	// the phase changes.
	Status string
	// Detail is an optional second line naming what went wrong. Only the
	// failure phases set it; every normal state is the single Status line.
	Detail      string
	ActionLabel string
	ShowAction  bool
	ActionStyle string
}

// ShowStatus keeps active status controls visible but collapses an empty
// header, so an empty box adds no spacing between the wordmark and the
// source groups.
func (p Presentation) ShowStatus(phase updateflow.Phase) bool {
	return p.Status != "" || p.Detail != "" || p.ShowAction || ShowProgress(phase)
}

// Snapshot maps one coordinator snapshot to aggregate widget text and action
// metadata.
func Snapshot(state updateflow.Snapshot) Presentation {
	switch state.Phase {
	case updateflow.PhaseChecking:
		return Presentation{Status: gotext.Get("Checking for updates…")}
	case updateflow.PhaseReady:
		return readyPresentation(state)
	case updateflow.PhaseCheckFailed:
		presentation := Presentation{Status: gotext.Get("Couldn't check for updates"),
			Detail: checkErrorDetail(state)}
		addAction(&presentation, state.Action, gotext.Get("Try again"))
		return presentation
	case updateflow.PhaseUpdating:
		return Presentation{Status: updatingStatus(state)}
	case updateflow.PhasePartialFailure:
		presentation := partialFailurePresentation(state)
		addAction(&presentation, state.Action, gotext.Get("Retry failed"))
		return presentation
	case updateflow.PhaseRestartRequired:
		// The restart action lives on the Operating system row ("Deployment
		// staged" with a "Restart now" suffix, #439), so the header offers
		// no primary action here: its one line says what is left to do.
		return Presentation{Status: gotext.Get("Restart to finish updating")}
	default:
		return Presentation{Status: gotext.Get("Checking for updates…")}
	}
}

// SourceLockReason explains why no user preference can turn a source on: the
// administrator's configuration first, then the host capability floor. It is
// empty for an operable source. The Updates page's source rows and the
// Preferences dialog's source switches both use it, so the two surfaces say
// the same thing about the same source.
func SourceLockReason(state updateflow.SourceState) string {
	switch {
	case !state.Configured:
		return gotext.Get("Disabled by administrator")
	case !state.Available:
		return gotext.Get("Not available on this computer")
	default:
		return ""
	}
}

// Source maps one source state to its row title and subtitle. A failure is
// said in plain words; the raw error is logged where it is produced
// (updateflow.Coordinator), never shown.
func Source(state updateflow.SourceState) (title, subtitle string) {
	title = sourceTitle(state.ID)

	if reason := SourceLockReason(state); reason != "" {
		return title, reason
	}
	switch {
	case !state.Enabled:
		return title, gotext.Get("Disabled in preferences")
	case state.Checking:
		return title, gotext.Get("Checking for updates…")
	case state.Updating:
		return title, gotext.Get("Installing updates…")
	case state.ApplyErr != nil:
		return title, gotext.Get("Couldn't update. %s", FailureHint(state.ApplyErr))
	case state.CheckErr != nil:
		return title, gotext.Get("Couldn't check for updates. %s", FailureHint(state.CheckErr))
	case state.RestartRequired:
		return title, gotext.Get("Deployment staged")
	case len(state.Items) > 0:
		if item, ok := soleOSItem(state); ok {
			return title, soleItemSubtitle(item)
		}
		return title, gotext.GetN("%d update available", "%d updates available", len(state.Items), len(state.Items))
	case state.Completed:
		return title, gotext.Get("Updated")
	default:
		return title, gotext.Get("Up to date")
	}
}

// ItemRows returns the pending items that get their own row beneath a
// source row. The Operating system source's one pending item is the
// deployment itself, so its versions go on the source row (see Source) and
// no child row repeats the source's name for one fact. Every other source
// keeps one row per item: Applications and Developer tools carry each item's
// own Update button on that row, so folding one away would remove its only
// control.
func ItemRows(state updateflow.SourceState) []updateflow.Item {
	if _, ok := soleOSItem(state); ok {
		return nil
	}
	return state.Items
}

// soleOSItem reports the Operating system source's only pending item. The
// decision is keyed on the source ID, never on the item's name: a Flatpak or
// formula that happens to be called "Applications" is still its own item.
func soleOSItem(state updateflow.SourceState) (updateflow.Item, bool) {
	if state.ID != updateflow.OperatingSystem || len(state.Items) != 1 {
		return updateflow.Item{}, false
	}
	return state.Items[0], true
}

// soleItemSubtitle is the source row's subtitle when its one pending item is
// folded into it.
func soleItemSubtitle(item updateflow.Item) string {
	switch {
	case item.CurrentVersion != "" && item.AvailableVersion != "":
		return gotext.Get("Update available: %s → %s", item.CurrentVersion, item.AvailableVersion)
	case item.AvailableVersion != "":
		return gotext.Get("Update available: %s", item.AvailableVersion)
	default:
		return gotext.GetN("%d update available", "%d updates available", 1, 1)
	}
}

// SourceIcon returns the symbolic icon for a source row.
func SourceIcon(id updateflow.SourceID) string {
	switch id {
	case updateflow.Applications:
		return "applications-system-symbolic"
	case updateflow.DeveloperTools:
		return "package-x-generic-symbolic"
	case updateflow.SystemComponents:
		return "applications-system-symbolic"
	case updateflow.OperatingSystem:
		return "drive-harddisk-system-symbolic"
	default:
		return "software-update-available-symbolic"
	}
}

// ItemTitle falls back to the execution identity when a provider has no name.
func ItemTitle(item updateflow.Item) string {
	if item.Name != "" {
		return item.Name
	}
	return item.ID
}

// ItemSubtitle returns the version detail shown beneath one pending item.
func ItemSubtitle(item updateflow.Item) string {
	var subtitle string
	switch {
	case item.CurrentVersion != "" && item.AvailableVersion != "":
		subtitle = gotext.Get("%s → %s", item.CurrentVersion, item.AvailableVersion)
	case item.AvailableVersion != "":
		subtitle = gotext.Get("Available: %s", item.AvailableVersion)
	case item.CurrentVersion != "":
		subtitle = gotext.Get("Installed: %s", item.CurrentVersion)
	default:
		subtitle = gotext.Get("Update available")
	}
	switch item.Scope {
	case "user":
		subtitle += gotext.Get(", for you only")
	case "system":
		subtitle += gotext.Get(", for everyone")
	}
	return subtitle
}

// ShowProgress reports whether the aggregate progress bar belongs on screen.
func ShowProgress(phase updateflow.Phase) bool {
	return phase == updateflow.PhaseChecking || phase == updateflow.PhaseUpdating
}

// CanStartOperation reports whether a shell operation may be launched.
func CanStartOperation(busy, closed bool) bool {
	return !busy && !closed
}

// RecheckForPreferences reports whether a changed update-source preference
// needs a new coordinator check for the shell to stay truthful: the snapshot
// on screen enables a source the preferences no longer do (or the reverse),
// or a check is in flight with the preferences it started from. Only a check
// recomputes enablement — the coordinator owns it — and a source the user
// just turned on has not been checked at all.
func RecheckForPreferences(snapshot updateflow.Snapshot, preferences userprefs.Values) bool {
	return snapshot.Phase == updateflow.PhaseChecking || snapshot.StaleFor(preferences)
}

// RecheckAfterCheck reports whether a check that has just returned left the
// shell describing preferences other than the current ones, so the shell
// must check again. A preference changed while that check was in flight is
// otherwise only noticed by a manual Refresh. A snapshot still in
// PhaseChecking belongs to a newer check, which already started from the
// current preferences, so it never asks for another: asking would supersede
// that check and loop for as long as checks overlap.
func RecheckAfterCheck(snapshot updateflow.Snapshot, preferences userprefs.Values) bool {
	return snapshot.Phase != updateflow.PhaseChecking && snapshot.StaleFor(preferences)
}

// PrimaryActionEnabled reports whether the page-level primary action button
// (check, update all, or retry failed) should be sensitive. Restarting is not
// a primary action: it lives on the Operating system row (#439). The action
// must be shown, the shell must not be busy or closed, and no restart may be
// in flight: while StartRestart's pkexec prompt is pending, starting another
// operation from the primary would race the reboot, and every Render
// recomputes sensitivity from shell state, so the in-flight window has to be
// part of the decision (issue #447).
func PrimaryActionEnabled(showAction, busy, closed, restartInFlight bool) bool {
	return showAction && CanStartOperation(busy, closed) && !restartInFlight
}

// ShouldPublish reports whether a queued snapshot may still reach the shell.
func ShouldPublish(closed bool) bool {
	return !closed
}

// SourceSubtitleLines returns the detail line limit for a source row.
func SourceSubtitleLines(compact bool) int32 {
	if compact {
		return 1
	}
	return 2
}

func readyPresentation(state updateflow.Snapshot) Presentation {
	var presentation Presentation
	switch {
	case state.Action == updateflow.ActionNone && state.TotalUpdates == 0:
		presentation.Status = gotext.Get("Nothing to update on this computer")
	case state.TotalUpdates > 0:
		presentation.Status = gotext.GetN(
			"%d update available",
			"%d updates available",
			state.TotalUpdates,
			state.TotalUpdates,
		)
		if state.RestartRequired() {
			presentation.Status = gotext.Get("%s · restart needed", presentation.Status)
		}
	case !state.LastChecked.IsZero():
		presentation.Status = gotext.Get("Up to date · checked at %s", state.LastChecked.Format("15:04"))
	default:
		presentation.Status = gotext.Get("Up to date")
	}
	addAction(&presentation, state.Action, gotext.Get("Check again"))
	if state.Action == updateflow.ActionUpdateAll {
		presentation.ActionLabel = gotext.Get("Update all")
	}
	return presentation
}

// updatingStatus names the source being updated. The provider's own
// progress text, when it has any, follows it on the same line.
func updatingStatus(state updateflow.Snapshot) string {
	switch {
	case state.Current != "" && state.Progress != "":
		return gotext.Get("%s: %s", sourceTitle(state.Current), state.Progress)
	case state.Progress != "":
		return state.Progress
	case state.Current != "":
		return gotext.Get("%s: installing updates…", sourceTitle(state.Current))
	case state.Maintaining:
		return gotext.Get("Cleaning up after updates…")
	default:
		return gotext.Get("Installing updates…")
	}
}

func checkErrorDetail(state updateflow.Snapshot) string {
	var failed []string
	hint := ""
	for _, source := range state.Sources {
		if source.CheckErr == nil {
			continue
		}
		failed = append(failed, sourceTitle(source.ID))
		// One hint covers the line only when every failure earns it;
		// otherwise the rows carry their own and the line points at the log.
		sourceHint := FailureHint(source.CheckErr)
		if hint == "" {
			hint = sourceHint
		} else if hint != sourceHint {
			hint = logHint()
		}
	}
	if len(failed) == 0 {
		return ""
	}
	return gotext.Get("Not checked: %s. %s", strings.Join(failed, ", "), hint)
}

// FailureHint is the sentence that follows a failure's plain description. It
// names a cause only when the error shows one: a request that could not
// reach the network, or one that ran out of time. Any other failure — a
// local Flatpak, Homebrew, or bootc error — is pointed at the log, where the
// raw error is kept, rather than told a remedy it may not need.
func FailureHint(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return gotext.Get("It took too long. Try again later.")
	case NetworkFailure(err):
		return gotext.Get("Check your internet connection.")
	default:
		return logHint()
	}
}

func logHint() string {
	return gotext.Get("Details are in the log.")
}

// networkPhrases are what the tools behind the update sources print when a
// request never reached its server: libcurl through Flatpak/OSTree and
// Homebrew, glibc's resolver, Go's net package, and bootc's HTTP client.
// Each is specific to a failed connection; a tool's generic "unable to load"
// or "failed" is deliberately absent, because it says nothing about why.
var networkPhrases = []string{
	"could not resolve",
	"temporary failure in name resolution",
	"name or service not known",
	"no such host",
	"failed to lookup address",
	"dns error",
	"network is unreachable",
	"network is down",
	"no route to host",
	"connection refused",
	"connection timed out",
	"connection reset",
	"failed to connect",
	"could not connect",
	"couldn't connect",
	"timeout was reached",
	"i/o timeout",
	"tls handshake timeout",
	"error sending request",
}

// NetworkFailure reports whether err shows that a request could not reach
// the network: a Go network error in its chain, or a tool's own message
// naming a failed connection. A cancellation or an exhausted deadline is not
// one (FailureHint names the deadline itself).
func NetworkFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, phrase := range networkPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

func partialFailurePresentation(state updateflow.Snapshot) Presentation {
	if state.MaintenanceErr != nil && len(state.FailedSources) == 0 {
		return Presentation{Status: gotext.Get("Updates installed, but couldn't clean up"),
			Detail: gotext.Get("Old files are still on this computer. %s", FailureHint(state.MaintenanceErr))}
	}
	presentation := Presentation{Status: gotext.Get("Couldn't install some updates")}
	if len(state.FailedSources) > 0 {
		failed := make([]string, 0, len(state.FailedSources))
		for _, id := range state.FailedSources {
			failed = append(failed, sourceTitle(id))
		}
		presentation.Detail = gotext.Get("Not updated: %s", strings.Join(failed, ", "))
	}
	return presentation
}

func addAction(presentation *Presentation, action updateflow.Action, checkLabel string) {
	switch action {
	case updateflow.ActionCheck:
		presentation.ActionLabel = checkLabel
		presentation.ShowAction = true
		presentation.ActionStyle = "suggested-action"
	case updateflow.ActionUpdateAll:
		presentation.ActionLabel = gotext.Get("Update all")
		presentation.ShowAction = true
		presentation.ActionStyle = "suggested-action"
	case updateflow.ActionRetryFailed:
		presentation.ActionLabel = gotext.Get("Retry failed")
		presentation.ShowAction = true
		presentation.ActionStyle = "suggested-action"
	}
}

func sourceTitle(id updateflow.SourceID) string {
	switch id {
	case updateflow.Applications:
		return gotext.Get("Applications")
	case updateflow.DeveloperTools:
		return gotext.Get("Developer tools")
	case updateflow.SystemComponents:
		return gotext.Get("System components")
	case updateflow.OperatingSystem:
		return gotext.Get("Operating system")
	default:
		return gotext.Get("Updates")
	}
}
