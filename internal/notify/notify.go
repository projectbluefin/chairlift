// Package notify decides the title and body of the desktop notifications
// ChairLift sends for long-running background work — the operations a user
// is unlikely to be watching when they finish.
//
// It is deliberately narrow. finupdate sends a GNotification on every update
// completion or failure; bluefinctl routes all feedback through its OpsBar
// instead and sends nothing to the desktop. ChairLift follows finupdate here
// for exactly one operation — Update All — because it is the one action long
// enough that a user plausibly switches away before it finishes. Every other
// action (a switch, a toggle) completes in view, and a toast already covers
// it; a second notification for the same instant event would be noise.
//
// The package only decides text. Sending the GNotification happens in
// internal/window, the one place that holds a *gtk.Application handle;
// internal/views cannot host a test binary at all (see
// docs/agents/skills/gtk-headless-tests.md), so the decision logic lives
// here instead, where it is a plain table test.
package notify

import (
	"fmt"

	"github.com/projectbluefin/chairlift/internal/branding"
)

// Urgency maps to GLib's NotificationPriority.
type Urgency int

const (
	UrgencyNormal Urgency = iota
	UrgencyHigh
)

// Notification is the content ChairLift asks the desktop to show.
type Notification struct {
	Title   string
	Body    string
	Urgency Urgency
}

// UpdateAllComplete returns the notification for a finished Update All run.
// succeeded and failed count the run's sources whose updates were verified
// as applied and that failed; sources the run never targeted — disabled,
// unconfigured, or not available on this host — are not counted, because a
// skipped source neither softens a total failure nor makes a clean run
// partial. maintenanceFailed reports a failed post-update cleanup and
// restartRequired a pending restart.
//
// The outcomes: every source failed (high urgency — nothing happened and the
// user should know), a mix of success and failure (high urgency, since a
// silent partial failure is the case most likely to leave a machine out of
// date without anyone noticing), updates installed but cleanup failed, a
// clean run that staged a restart, and a clean run. The run starts only with
// updates pending, so a clean run installed them; it never says the system
// was "already" current.
func UpdateAllComplete(succeeded, failed int, restartRequired, maintenanceFailed bool) Notification {
	restartSuffix := ""
	if restartRequired {
		restartSuffix = " Restart to apply the system image."
	}
	switch {
	case failed > 0 && succeeded == 0:
		return Notification{
			Title:   "Update failed",
			Body:    branding.AppName + " couldn't update this computer. Open it to see what went wrong.",
			Urgency: UrgencyHigh,
		}
	case failed > 0:
		body := fmt.Sprintf("%d part(s) updated, %d failed.", succeeded, failed) + restartSuffix
		return Notification{Title: "Update finished with problems", Body: body, Urgency: UrgencyHigh}
	case succeeded == 0 && !maintenanceFailed:
		return Notification{Title: "Nothing to update", Body: "No updates were installed."}
	case maintenanceFailed:
		return Notification{
			Title: "Update finished with problems",
			Body:  "Updates were installed, but cleaning up afterwards failed. Open the app for details." + restartSuffix,
		}
	case restartRequired:
		return Notification{Title: "Update complete", Body: "Restart to finish updating."}
	default:
		return Notification{Title: "Update complete", Body: "All updates were installed."}
	}
}
