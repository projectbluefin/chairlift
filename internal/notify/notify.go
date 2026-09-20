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
// succeeded/failed/skipped are the phase counts from updateall.Summary, and
// restartRequired is Summary.RestartRequired.
//
// The four distinct outcomes: every phase failed (high urgency — nothing
// happened and the user should know), a mix of success and failure (high
// urgency, since a silent partial failure is the case most likely to leave a
// machine out of date without anyone noticing), a clean run that staged a
// restart, and a clean run that found nothing to do — the last case still
// gets a notification, since a user who stepped away wants to know the
// system is current before deciding to restart.
func UpdateAllComplete(succeeded, failed, skipped int, restartRequired bool) Notification {
	total := succeeded + failed + skipped

	switch {
	case total == 0:
		return Notification{Title: "Nothing to update", Body: "No update sources are available on this system."}
	case failed == total:
		return Notification{
			Title:   "Update failed",
			Body:    branding.AppName + " could not update this system. Open the app for details.",
			Urgency: UrgencyHigh,
		}
	case failed > 0:
		body := fmt.Sprintf("%d part(s) updated, %d failed.", succeeded, failed)
		if restartRequired {
			body += " Restart to apply the system image."
		}
		return Notification{Title: "Update finished with problems", Body: body, Urgency: UrgencyHigh}
	case restartRequired:
		return Notification{Title: "Update complete", Body: "Restart to apply the new system image."}
	default:
		return Notification{Title: "Update complete", Body: "This system is already up to date."}
	}
}
