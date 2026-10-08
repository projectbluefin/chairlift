// Package pkexec owns the name of the program ChairLift escalates through.
//
// Every privileged action in ChairLift — the fixed-path helper invocations
// routed by internal/helperexec and the staging scripts routed by
// internal/stageexec — reaches root by the same route: pkexec, which is what
// binds the invocation to a PolicyKit action in data/*.policy. Both executors
// deliberately take that program name as a parameter so tests can substitute
// a stand-in without invoking the real pkexec/polkit stack, but neither
// executor supplied the production value. Each provider package
// (internal/bootc, internal/ublue, internal/updex) therefore declared its own
// private copy, leaving the escalation entrypoint stated repeatedly with no owner and no gate.
//
// This package is that owner. Providers name Command; the executors keep their
// injection seam. internal/installcheck's TestPkexecCommandHasOneOwner keeps a
// future provider from reintroducing a private copy.
package pkexec

import (
	"errors"
	"os/exec"
	"strings"
)

// Command is the privilege-escalation program ChairLift invokes in
// production. It is a bare name resolved through $PATH on purpose: PolicyKit
// binds an action to the *helper* path via the
// org.freedesktop.policykit.exec.path annotation, not to pkexec's own path.
const Command = "pkexec"

// DismissedExitCode is the status pkexec exits with when the user dismissed
// the authentication dialog (pkexec(1)). The helper never ran, so nothing
// changed. Status 127 — not authorized, or no authentication agent — is a
// genuine failure and is deliberately not treated as a dismissal.
const DismissedExitCode = 126

// dismissedMarker is the text pkexec prints to stderr for a dismissed
// dialog ("Error executing command as another user: Request dismissed").
// Callers fold that stderr into the message they show, so a message-only
// surface such as the window's error toast can still recognise it.
const dismissedMarker = "Request dismissed"

// CancelledMessage is the brief, non-error toast that replaces a dismissed
// PolicyKit prompt's error. The program never ran, so there is nothing to
// diagnose and no raw pkexec stderr or exit status worth pinning to the
// window (#492).
const CancelledMessage = "Authentication cancelled"

// IsAuthDismissed reports whether err is the user cancelling the PolicyKit
// authentication prompt rather than a failure: pkexec's own exit status 126
// anywhere in the chain, or its dismissal text in the message. Apply it only
// to errors from a pkexec invocation; another program's 126 means something
// else.
func IsAuthDismissed(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == DismissedExitCode {
		return true
	}
	return MessageIsAuthDismissed(err.Error())
}

// MessageIsAuthDismissed reports whether an already-formatted error message
// carries pkexec's dismissal text. It is the form the window's error toast
// uses, because every view hands it a string rather than the error.
func MessageIsAuthDismissed(message string) bool {
	return strings.Contains(message, dismissedMarker)
}

// UserMessage returns the text a privileged view hands the window's error
// toast for err: plain, a sentence written for the person, for a genuine
// failure; or a message the toast recognises as a dismissed password prompt.
// The raw error belongs in the log, which the caller writes.
func UserMessage(err error, plain string) string {
	if IsAuthDismissed(err) {
		return dismissedMarker
	}
	return plain
}
