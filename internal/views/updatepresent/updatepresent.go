// Package updatepresent owns the pure presentation decisions for the unified
// update shell.
package updatepresent

import (
	"github.com/leonelquinteros/gotext"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// Presentation is the aggregate status shown by the update shell.
type Presentation struct {
	Icon        string
	Title       string
	Description string
	ActionLabel string
	ShowAction  bool
	ActionStyle string
	Banner      string
}

// Snapshot maps one coordinator snapshot to aggregate widget text and action
// metadata.
func Snapshot(state updateflow.Snapshot) Presentation {
	switch state.Phase {
	case updateflow.PhaseChecking:
		return Presentation{
			Icon:        "view-refresh-symbolic",
			Title:       gotext.Get("Checking for updates"),
			Description: checkingDescription(state),
		}
	case updateflow.PhaseReady:
		return readyPresentation(state)
	case updateflow.PhaseCheckFailed:
		presentation := Presentation{
			Icon:        "network-error-symbolic",
			Title:       gotext.Get("Unable to check for updates"),
			Description: checkErrorDescription(state),
			Banner:      gotext.Get("Unable to check for updates"),
		}
		addAction(&presentation, state.Action, gotext.Get("Try again"))
		return presentation
	case updateflow.PhaseUpdating:
		return Presentation{
			Icon:        "content-loading-symbolic",
			Title:       gotext.Get("Installing updates"),
			Description: updatingDescription(state),
		}
	case updateflow.PhasePartialFailure:
		banner := gotext.Get("Some updates could not be installed")
		if state.MaintenanceErr != nil && len(state.FailedSources) == 0 {
			banner = gotext.Get("Maintenance failed")
		}
		presentation := Presentation{
			Icon:        "dialog-warning-symbolic",
			Title:       gotext.Get("Some updates could not be installed"),
			Description: partialFailureDescription(state),
			Banner:      banner,
		}
		addAction(&presentation, state.Action, gotext.Get("Retry failed"))
		return presentation
	case updateflow.PhaseRestartRequired:
		presentation := Presentation{
			Icon:        "system-reboot-symbolic",
			Title:       gotext.Get("Restart required"),
			Description: gotext.Get("Restart to finish installing updates."),
			Banner:      gotext.Get("Restart required"),
		}
		addAction(&presentation, state.Action, gotext.Get("Restart now"))
		return presentation
	default:
		return Presentation{
			Icon:        "view-refresh-symbolic",
			Title:       gotext.Get("Checking for updates"),
			Description: gotext.Get("Preparing to check for updates…"),
		}
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
		return title, gotext.Get("Restart required")
	case len(state.Items) > 0:
		return title, gotext.GetN("%d update available", "%d updates available", len(state.Items), len(state.Items))
	case state.Completed:
		return title, gotext.Get("Updated")
	default:
		return title, gotext.Get("Up to date")
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

// ItemSubtitle returns the version detail shown beneath one pending item.
func ItemSubtitle(item updateflow.Item) string {
	switch {
	case item.CurrentVersion != "" && item.AvailableVersion != "":
		return gotext.Get("%s → %s", item.CurrentVersion, item.AvailableVersion)
	case item.AvailableVersion != "":
		return gotext.Get("Available: %s", item.AvailableVersion)
	case item.CurrentVersion != "":
		return gotext.Get("Installed: %s", item.CurrentVersion)
	default:
		return gotext.Get("Update available")
	}
}

// ShowProgress reports whether the aggregate progress bar belongs on screen.
func ShowProgress(phase updateflow.Phase) bool {
	return phase == updateflow.PhaseChecking || phase == updateflow.PhaseUpdating
}

// SourceHasDetails reports whether a source row should expose its detail rows.
func SourceHasDetails(state updateflow.SourceState) bool {
	return len(state.Items) > 0
}

// CanStartOperation reports whether a shell operation may be launched.
func CanStartOperation(busy, closed bool) bool {
	return !busy && !closed
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
	presentation := Presentation{
		Icon:  "emblem-system-symbolic",
		Title: gotext.Get("System is up to date"),
	}
	switch {
	case state.Action == updateflow.ActionNone && state.TotalUpdates == 0:
		presentation.Description = gotext.Get("No update sources are available.")
	case state.TotalUpdates > 0:
		presentation.Icon = "software-update-available-symbolic"
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
	case updateflow.ActionRestart:
		presentation.ActionLabel = gotext.Get("Restart now")
		presentation.ShowAction = true
		// Destructive rather than suggested: this ends the user's session
		// and closes whatever they have open.
		presentation.ActionStyle = "destructive-action"
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
