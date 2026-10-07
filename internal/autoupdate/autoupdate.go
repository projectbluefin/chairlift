// Package autoupdate reads and reports the state of automatic background
// system updates — the `uupd.timer` systemd unit that Universal Blue images
// ship to apply updates without the user asking.
//
// It is ChairLift's replacement for bluefinctl's update-strategy surface.
// bluefinctl models this as a strategy enum (automatic / manual / focus mode)
// spread across a masked-timer check, an enabled-timer check, per-layer
// switches, and a schedule picker. ChairLift reduces it to the single
// question a user actually has — should the system update itself in the
// background, yes or no — because the rest of that surface is option sprawl
// rather than capability.
//
// Collapsing three systemd states into one switch means the mapping has to be
// explicit, which is what State does: masked and disabled both read as "off",
// and only an enabled *and* active timer reads as "on". The privileged writes
// live in the ublue helper; this package only classifies.
package autoupdate

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// TimerUnit is the systemd unit Universal Blue images use for unattended
// updates. It is the unit whose state the switch reports.
const TimerUnit = "uupd.timer"

// ResumeTimerUnit is the second trigger of the same uupd.service: Universal
// Blue images enable it (WantedBy suspend, hibernate, and
// suspend-then-hibernate) so an update runs 20 minutes after every resume.
// Turning automatic updates off must silence it too, or the switch claims
// "off" while background updates still run after each resume.
const ResumeTimerUnit = "uupd-resume.timer"

const probeTimeout = 5 * time.Second

// State is the classified state of automatic background updates.
type State string

const (
	// StateOn means the timer is enabled and running.
	StateOn State = "on"
	// StateOff means the timer exists but is disabled or masked. Masking is
	// how bluefinctl's "focus mode" and "manual" strategy are both expressed
	// on disk, and neither is distinguishable to a user who only wants to
	// know whether their machine updates itself.
	StateOff State = "off"
	// StateUnavailable means the unit is not installed. The switch is hidden
	// entirely rather than shown inert, matching how ChairLift hides every
	// group whose backing tool is absent.
	StateUnavailable State = "unavailable"
)

// Available reports whether automatic updates can be controlled at all.
func (s State) Available() bool {
	return s == StateOn || s == StateOff
}

// Enabled reports whether automatic updates are currently on.
func (s State) Enabled() bool {
	return s == StateOn
}

// Classify maps systemd's answers to a State. isEnabled is the output of
// `systemctl is-enabled uupd.timer` and isActive of `systemctl is-active`;
// each is the trimmed first word, with the empty string standing for a
// failed or missing unit.
//
// The distinct outcomes are:
//
//   - "" or "not-found" from is-enabled — the unit is not installed, so
//     StateUnavailable.
//   - "masked" — StateOff. A masked timer cannot run, however is-active
//     answers.
//   - "enabled" or "enabled-runtime" with an active timer — StateOn.
//   - "enabled" with an inactive timer — StateOff. An enabled-but-dead timer
//     does not update anything, and reporting it as on would be a lie the
//     user discovers only by not receiving updates.
//   - anything else ("disabled", "static", "indirect") — StateOff.
func Classify(isEnabled, isActive string) State {
	enabled := strings.TrimSpace(isEnabled)
	active := strings.TrimSpace(isActive)

	switch enabled {
	case "", "not-found":
		return StateUnavailable
	case "masked", "masked-runtime":
		return StateOff
	case "enabled", "enabled-runtime":
		if active == "active" {
			return StateOn
		}
		return StateOff
	default:
		return StateOff
	}
}

// probe is an injection seam for the systemctl queries, so Detect's
// classification is testable without systemd. Its production value runs the
// real unprivileged `systemctl show`-class queries.
var probe = systemctlProbe

// SetProbe replaces the systemctl queries Detect runs.
//
// It exists so the screenshot walkthrough can render the automatic-updates
// switch on a runner where uupd.timer is not installed — otherwise the
// walkthrough could only ever capture the switch hidden, and could not verify
// it at all. It is called exclusively from ChairLift's chairlift_e2e-tagged
// build (internal/app/imageinfo_override_e2e.go); no released binary contains
// a call site, which internal/installcheck asserts.
//
// It is safe in a way the image-descriptor override is not even in principle:
// this classification is read-only and feeds a switch whose writes go through
// the privileged helper, which queries systemd itself.
func SetProbe(replacement func(context.Context) (isEnabled, isActive string)) {
	if replacement == nil {
		return
	}
	probe = replacement
}

// Detect classifies this host's automatic-update state.
func Detect(ctx context.Context) State {
	isEnabled, isActive := probe(ctx)
	return Classify(isEnabled, isActive)
}

// systemctlProbe runs the two unprivileged queries. Both `is-enabled` and
// `is-active` exit non-zero for perfectly ordinary answers ("disabled",
// "inactive"), so the exit status is ignored and only stdout is read.
func systemctlProbe(ctx context.Context) (isEnabled, isActive string) {
	return systemctlOutput(ctx, "is-enabled"), systemctlOutput(ctx, "is-active")
}

func systemctlOutput(ctx context.Context, verb string) string {
	queryCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	output, err := exec.CommandContext(queryCtx, "systemctl", verb, TimerUnit).Output()
	if err != nil && len(output) == 0 {
		return ""
	}
	return strings.TrimSpace(string(output))
}
