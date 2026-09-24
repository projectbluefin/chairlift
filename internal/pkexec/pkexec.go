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

// Command is the privilege-escalation program ChairLift invokes in
// production. It is a bare name resolved through $PATH on purpose: PolicyKit
// binds an action to the *helper* path via the
// org.freedesktop.policykit.exec.path annotation, not to pkexec's own path.
const Command = "pkexec"
