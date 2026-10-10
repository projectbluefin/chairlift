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

// ResumeState reports whether the resume-from-suspend timer that fires
// uupd.service is silenced. The switch's user-facing State is governed by
// TimerUnit, but the same service is also triggered by ResumeTimerUnit
// twenty minutes after every resume, so a "turn automatic updates off"
// action that silences only the first timer leaves background updates
// firing after every suspend — exactly the helper/GUI skew that left
// the resume timer armed in #558.
//
// The probe is the same unprivileged `systemctl is-enabled
// uupd-resume.timer` the helper itself would not have changed. Detecting
// "still armed" only needs the empty/masked/enabled taxonomy, not
// TimerUnit's full State enum, so the three-way answer is exposed as
// ResumeState rather than a new State constant.
//
//   - ResumeStateMasked — the unit exists and is masked; the helper's
//     disable path took effect.
//   - ResumeStateAvailable — the unit exists and is enabled (or in any
//     other non-masked state, which a default branch in classifyResume
//     folds into "armed"). This is the skew signal: the helper ran, the
//     switch reads "off", but the resume trigger is still live.
//   - ResumeStateAbsent — `systemctl is-enabled` answers "" or
//     "not-found". Either the image does not ship the unit, or it is
//     loaded as a transient unit that has since been removed. Either
//     way, there is no resume trigger to silence, and the post-check
//     has nothing to warn about.
type ResumeState int

const (
	ResumeStateMasked ResumeState = iota
	ResumeStateAvailable
	ResumeStateAbsent
)

// String reports the human-readable label of a ResumeState, so log lines
// and toast copy can read the value without a switch on every call site.
func (r ResumeState) String() string {
	switch r {
	case ResumeStateMasked:
		return "masked"
	case ResumeStateAvailable:
		return "available"
	case ResumeStateAbsent:
		return "absent"
	default:
		return "unknown"
	}
}

// classifyResume maps a single `systemctl is-enabled ResumeTimerUnit`
// answer to the three-way ResumeState. The vocabulary matches Classify's
// empty-string and masked branches, so a host whose helper masked the
// unit reports ResumeStateMasked and one whose helper predates the
// resume-timer work reports ResumeStateAvailable.
func classifyResume(isEnabled string) ResumeState {
	switch strings.TrimSpace(isEnabled) {
	case "", "not-found", "disabled":
		return ResumeStateAbsent
	case "masked", "masked-runtime":
		return ResumeStateMasked
	default:
		return ResumeStateAvailable
	}
}

// resumeProbe is the injection seam for the unprivileged
// `systemctl is-enabled ResumeTimerUnit` query DetectResume runs. The
// production value runs the real query; tests substitute a function that
// returns the canned is-enabled answer directly. No released or e2e
// binary replaces it, so it has no exported setter and no environment
// variable for internal/installcheck's stub-surface rule to track.
var resumeProbe = systemctlResumeOutput

// DetectResume classifies the resume-from-suspend timer's state. It is
// the post-action read that backs the helper-skew warning: after the
// helper has run auto-updates-disable, this answers whether it took
// effect on the resume trigger.
func DetectResume(ctx context.Context) ResumeState {
	return classifyResume(resumeProbe(ctx))
}

// systemctlResumeOutput runs the unprivileged is-enabled query. As with
// the main timer probe, exit status is ignored in favour of stdout
// because systemctl exits non-zero for perfectly ordinary answers
// ("disabled", "masked").
func systemctlResumeOutput(ctx context.Context) string {
	queryCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	output, err := exec.CommandContext(queryCtx, "systemctl", "is-enabled", ResumeTimerUnit).Output()
	if err != nil && len(output) == 0 {
		return ""
	}
	return strings.TrimSpace(string(output))
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
