// Package updatepresent owns the pure presentation decisions for the unified
// update shell.
package updatepresent

import (
	"github.com/leonelquinteros/gotext"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// Presentation is the aggregate status shown by the update shell.
type Presentation struct {
	Title       string
	Description string
	ActionLabel string
	ShowAction  bool
	ActionStyle string
	Banner      string
	// Announcement is what a screen reader hears when the phase changes.
	// It is empty for every phase whose Title already says it; only
	// PhaseRestartRequired, which clears the panel, sets it on its own.
	Announcement string
}

// Announce returns the text to announce on a phase change: the explicit
// Announcement when the panel carries no title, the Title otherwise.
func (p Presentation) Announce() string {
	if p.Announcement != "" {
		return p.Announcement
	}
	return p.Title
}

// ShowStatus keeps active status controls visible but collapses an empty panel.
// Announcements and banners do not need the status page to occupy space.
func (p Presentation) ShowStatus(phase updateflow.Phase) bool {
	return p.Title != "" || p.Description != "" || p.ShowAction || ShowProgress(phase)
}

// Snapshot maps one coordinator snapshot to aggregate widget text and action
// metadata.
func Snapshot(state updateflow.Snapshot) Presentation {
	switch state.Phase {
	case updateflow.PhaseChecking:
		return Presentation{Title: gotext.Get("Checking for updates"),
			Description: checkingDescription(state)}
	case updateflow.PhaseReady:
		return readyPresentation(state)
	case updateflow.PhaseCheckFailed:
		presentation := Presentation{Title: gotext.Get("Unable to check for updates"),
			Description: checkErrorDescription(state),
			Banner:      gotext.Get("Unable to check for updates")}
		addAction(&presentation, state.Action, gotext.Get("Try again"))
		return presentation
	case updateflow.PhaseUpdating:
		return Presentation{Title: gotext.Get("Installing updates"),
			Description: updatingDescription(state)}
	case updateflow.PhasePartialFailure:
		banner := gotext.Get("Some updates could not be installed")
		if state.MaintenanceErr != nil && len(state.FailedSources) == 0 {
			banner = gotext.Get("Maintenance failed")
		}
		presentation := Presentation{Title: gotext.Get("Some updates could not be installed"),
			Description: partialFailureDescription(state),
			Banner:      banner}
		addAction(&presentation, state.Action, gotext.Get("Retry failed"))
		return presentation
	case updateflow.PhaseRestartRequired:
		// The restart action lives on the Operating system row, not on the
		// page-level status panel: an in-progress row that says "Deployment
		// staged" with a "Restart now" suffix tells the user both halves of
		// the story without a banner above the wordmark. The status page is
		// left with empty text so the Bluefin logo leads straight into the
		// "System updates" group, and the announcement repeats the row's
		// subtitle so a screen reader still reports the pending reboot.
		return Presentation{Announcement: gotext.Get("Deployment staged")}
	default:
		return Presentation{Title: gotext.Get("Checking for updates"),
			Description: gotext.Get("Preparing to check for updates…")}
	}
}

// Source maps one source state to its row title and subtitle.
func Source(state updateflow.SourceState) (title, subtitle string) {
	title = sourceTitle(state.ID)

	switch {
	case !state.Configured:
		return title, gotext.Get("Disabled by administrator")
	case !state.Available:
		return title, gotext.Get("Not available on this system")
	case !state.Enabled:
		return title, gotext.Get("Disabled in preferences")
	case state.Checking:
		return title, gotext.Get("Checking for updates…")
	case state.Updating:
		return title, gotext.Get("Installing updates…")
	case state.ApplyErr != nil:
		return title, gotext.Get("Update failed: %s", state.ApplyErr.Error())
	case state.CheckErr != nil:
		return title, gotext.Get("Check failed: %s", state.CheckErr.Error())
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
	presentation := Presentation{Title: gotext.Get("System is up to date")}
	switch {
	case state.Action == updateflow.ActionNone && state.TotalUpdates == 0:
		presentation.Description = gotext.Get("No update sources are available.")
	case state.TotalUpdates > 0:

		presentation.Title = gotext.Get("Updates available")
		presentation.Description = gotext.GetN(
			"%d update is available.",
			"%d updates are available.",
			state.TotalUpdates,
			state.TotalUpdates,
		)
		if state.RestartRequired() {
			presentation.Description += " " + gotext.Get("Restart is required to finish installing updates.")
		}
	default:
		presentation.Description = upToDateDescription(state)
	}
	addAction(&presentation, state.Action, gotext.Get("Check again"))
	if state.Action == updateflow.ActionUpdateAll {
		presentation.ActionLabel = gotext.Get("Update all")
	}
	return presentation
}

func upToDateDescription(state updateflow.Snapshot) string {
	if !state.LastChecked.IsZero() {
		return gotext.Get(
			"No updates are available. Last checked at %s.",
			state.LastChecked.Format("15:04"),
		)
	}
	return gotext.Get("No updates are available.")
}

func checkingDescription(state updateflow.Snapshot) string {
	if state.Current != "" && state.Progress != "" {
		return gotext.Get("%s: %s", sourceTitle(state.Current), state.Progress)
	}
	if state.Progress != "" {
		return state.Progress
	}
	if state.Current != "" {
		return gotext.Get("Checking %s…", sourceTitle(state.Current))
	}
	return gotext.Get("Checking available sources…")
}

func updatingDescription(state updateflow.Snapshot) string {
	if state.Current != "" && state.Progress != "" {
		return gotext.Get("%s: %s", sourceTitle(state.Current), state.Progress)
	}
	if state.Progress != "" {
		return state.Progress
	}
	if state.Current != "" {
		return gotext.Get("Installing %s…", sourceTitle(state.Current))
	}
	return gotext.Get("Installing updates…")
}

func checkErrorDescription(state updateflow.Snapshot) string {
	for _, source := range state.Sources {
		if source.CheckErr != nil {
			return gotext.Get("%s: %s", sourceTitle(source.ID), source.CheckErr.Error())
		}
	}
	return gotext.Get("The update check could not be completed.")
}

func partialFailureDescription(state updateflow.Snapshot) string {
	if state.MaintenanceErr != nil && len(state.FailedSources) == 0 {
		return gotext.Get("Updates completed, but maintenance failed: %s", state.MaintenanceErr.Error())
	}
	completed := gotext.GetN("%d source completed", "%d sources completed", len(state.CompletedSources), len(state.CompletedSources))
	failed := gotext.GetN("%d source failed", "%d sources failed", len(state.FailedSources), len(state.FailedSources))
	return gotext.Get("%s; %s.", completed, failed)
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
