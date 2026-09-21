// Package updatepresent owns the pure presentation decisions for the unified
// update shell.
package updatepresent

import (
	"fmt"

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
			Title:       "Checking for Updates",
			Description: checkingDescription(state),
		}
	case updateflow.PhaseReady:
		return readyPresentation(state)
	case updateflow.PhaseCheckFailed:
		presentation := Presentation{
			Icon:        "network-error-symbolic",
			Title:       "Unable to Check for Updates",
			Description: checkErrorDescription(state),
			Banner:      "Unable to Check for Updates",
		}
		addAction(&presentation, state.Action, "Try Again")
		return presentation
	case updateflow.PhaseUpdating:
		return Presentation{
			Icon:        "content-loading-symbolic",
			Title:       "Installing Updates",
			Description: updatingDescription(state),
		}
	case updateflow.PhasePartialFailure:
		banner := "Some Updates Could Not Be Installed"
		if state.MaintenanceErr != nil && len(state.FailedSources) == 0 {
			banner = "Maintenance Failed"
		}
		presentation := Presentation{
			Icon:        "dialog-warning-symbolic",
			Title:       "Some Updates Could Not Be Installed",
			Description: partialFailureDescription(state),
			Banner:      banner,
		}
		addAction(&presentation, state.Action, "Retry Failed")
		return presentation
	case updateflow.PhaseRestartRequired:
		return Presentation{
			Icon:        "system-reboot-symbolic",
			Title:       "Restart Required",
			Description: "Restart to finish installing updates.",
			Banner:      "Restart Required",
		}
	default:
		return Presentation{
			Icon:        "view-refresh-symbolic",
			Title:       "Checking for Updates",
			Description: "Preparing to check for updates…",
		}
	}
}

// Source maps one source state to its row title and subtitle.
func Source(state updateflow.SourceState) (title, subtitle string) {
	title = sourceTitle(state.ID)

	switch {
	case !state.Configured:
		return title, "Disabled by administrator"
	case !state.Available:
		return title, "Not available on this system"
	case !state.Enabled:
		return title, "Disabled in Preferences"
	case state.Checking:
		return title, "Checking for updates…"
	case state.Updating:
		return title, "Installing updates…"
	case state.ApplyErr != nil:
		return title, fmt.Sprintf("Update failed: %s", state.ApplyErr.Error())
	case state.CheckErr != nil:
		return title, fmt.Sprintf("Check failed: %s", state.CheckErr.Error())
	case state.RestartRequired:
		return title, "Restart required"
	case len(state.Items) > 0:
		if len(state.Items) == 1 {
			return title, "1 update available"
		}
		return title, fmt.Sprintf("%d updates available", len(state.Items))
	case state.Completed:
		return title, "Updated"
	default:
		return title, "Up to date"
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
		return fmt.Sprintf("%s → %s", item.CurrentVersion, item.AvailableVersion)
	case item.AvailableVersion != "":
		return fmt.Sprintf("Available: %s", item.AvailableVersion)
	case item.CurrentVersion != "":
		return fmt.Sprintf("Installed: %s", item.CurrentVersion)
	default:
		return "Update available"
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
		Title: "System Is Up to Date",
	}
	switch {
	case state.Action == updateflow.ActionNone && state.TotalUpdates == 0:
		presentation.Description = "No update sources are available."
	case state.TotalUpdates > 0:
		presentation.Icon = "software-update-available-symbolic"
		presentation.Title = "Updates Available"
		if state.TotalUpdates == 1 {
			presentation.Description = "1 update is available."
		} else {
			presentation.Description = fmt.Sprintf("%d updates are available.", state.TotalUpdates)
		}
		if state.RestartRequired() {
			presentation.Description += " Restart is required to finish installing updates."
		}
	default:
		presentation.Description = upToDateDescription(state)
	}
	addAction(&presentation, state.Action, "Check Again")
	if state.Action == updateflow.ActionUpdateAll {
		presentation.ActionLabel = "Update All"
	}
	return presentation
}

func upToDateDescription(state updateflow.Snapshot) string {
	if !state.LastChecked.IsZero() {
		return fmt.Sprintf(
			"No updates are available. Last checked at %s.",
			state.LastChecked.Format("15:04"),
		)
	}
	return "No updates are available."
}

func checkingDescription(state updateflow.Snapshot) string {
	if state.Current != "" && state.Progress != "" {
		return fmt.Sprintf("%s: %s", sourceTitle(state.Current), state.Progress)
	}
	if state.Progress != "" {
		return state.Progress
	}
	if state.Current != "" {
		return fmt.Sprintf("Checking %s…", sourceTitle(state.Current))
	}
	return "Checking available sources…"
}

func updatingDescription(state updateflow.Snapshot) string {
	if state.Current != "" && state.Progress != "" {
		return fmt.Sprintf("%s: %s", sourceTitle(state.Current), state.Progress)
	}
	if state.Progress != "" {
		return state.Progress
	}
	if state.Current != "" {
		return fmt.Sprintf("Installing %s…", sourceTitle(state.Current))
	}
	return "Installing updates…"
}

func checkErrorDescription(state updateflow.Snapshot) string {
	for _, source := range state.Sources {
		if source.CheckErr != nil {
			return fmt.Sprintf("%s: %s", sourceTitle(source.ID), source.CheckErr.Error())
		}
	}
	return "The update check could not be completed."
}

func partialFailureDescription(state updateflow.Snapshot) string {
	if state.MaintenanceErr != nil && len(state.FailedSources) == 0 {
		return fmt.Sprintf("Updates completed, but maintenance failed: %s", state.MaintenanceErr.Error())
	}
	completedNoun := "sources completed"
	if len(state.CompletedSources) == 1 {
		completedNoun = "source completed"
	}
	failedNoun := "sources failed"
	if len(state.FailedSources) == 1 {
		failedNoun = "source failed"
	}
	completed := fmt.Sprintf("%d %s", len(state.CompletedSources), completedNoun)
	failed := fmt.Sprintf("%d %s", len(state.FailedSources), failedNoun)
	return fmt.Sprintf("%s; %s.", completed, failed)
}

func addAction(presentation *Presentation, action updateflow.Action, checkLabel string) {
	switch action {
	case updateflow.ActionCheck:
		presentation.ActionLabel = checkLabel
		presentation.ShowAction = true
		presentation.ActionStyle = "suggested-action"
	case updateflow.ActionUpdateAll:
		presentation.ActionLabel = "Update All"
		presentation.ShowAction = true
		presentation.ActionStyle = "suggested-action"
	case updateflow.ActionRetryFailed:
		presentation.ActionLabel = "Retry Failed"
		presentation.ShowAction = true
		presentation.ActionStyle = "suggested-action"
	}
}

func sourceTitle(id updateflow.SourceID) string {
	switch id {
	case updateflow.Applications:
		return "Applications"
	case updateflow.DeveloperTools:
		return "Developer Tools"
	case updateflow.SystemComponents:
		return "System Components"
	case updateflow.OperatingSystem:
		return "Operating System"
	default:
		return "Updates"
	}
}
