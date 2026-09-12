// Package ublue is ChairLift's client for the Bluefin-family system
// features — release-channel switching and developer mode — on Bluefin,
// Bluefin LTS, and Dakota.
//
// It splits cleanly along the privilege boundary. Reads are unprivileged and
// local: the image descriptor at internal/imageinfo.DescriptorPath and the
// invoking user's own group membership. Writes are delegated in full to the
// fixed-path privileged helper via pkexec, exactly as internal/updex does —
// this package never assembles a `bootc` or `usermod` command line of its
// own, and never passes an image reference or a username across the pkexec
// boundary.
package ublue

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os/user"
	"sync"
	"time"

	"github.com/projectbluefin/chairlift/internal/gpu"
	"github.com/projectbluefin/chairlift/internal/helperexec"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
)

const (
	// HelperPath is the fixed, absolute installed path of the privileged
	// ublue helper binary. It must match the
	// org.freedesktop.policykit.exec.path annotation on all three actions in
	// data/io.projectbluefin.chairlift.ublue.policy exactly, and each action
	// additionally selects one supported helper command through the
	// exec.argv1 annotation. A path mismatch (for example a bare,
	// $PATH-resolved name) makes pkexec fall back to the generic, more
	// restrictive org.freedesktop.policykit.pkexec.run-program action
	// instead. The Makefile installs the binary here whenever PREFIX is /usr
	// (the default).
	HelperPath = "/usr/bin/chairlift-ublue-helper"

	pkexecCommand = "pkexec"

	// DefaultTimeout bounds a helper invocation. Channel switching only
	// stages a bootc transaction, but that transaction contacts a registry,
	// so it gets the same generous ceiling the updex helper uses.
	DefaultTimeout = 10 * time.Minute
)

// DefaultContext returns a context with the default timeout.
func DefaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), DefaultTimeout)
}

// Error represents a ublue helper error. It aliases
// internal/helperexec.Error: all privileged helper invocations share one
// plumbing implementation and one failure taxonomy.
type Error = helperexec.Error

// NotFoundError is returned when pkexec or the privileged helper is absent.
type NotFoundError = helperexec.NotFoundError

// Status is the complete unprivileged view of this host's Bluefin-family
// state. A zero Status with Available false is what every non-ublue host
// produces, and is the signal for the views layer to hide these rows rather
// than render an error.
type Status struct {
	Available   bool
	Variant     imageinfo.Variant
	Channel     imageinfo.Channel
	Tag         string
	Ref         string
	Developer   bool
	DevGroups   []string
	CanSwitchTo imageinfo.Channel
	// Driver is the graphics-driver flavour of the running image.
	Driver imageinfo.Driver
	// RecommendedDriver is the driver this machine's hardware wants, set
	// only when switching to it is both possible and worthwhile.
	RecommendedDriver imageinfo.Driver
	// GPU describes the detected graphics hardware.
	GPU string
}

// Detect returns the current Bluefin-family status. A host with no image
// descriptor is reported as unavailable with a nil error: that is the
// expected outcome on Snow Linux and every other non-ublue system, not a
// failure worth surfacing.
// detectInfo is an injection seam for the image descriptor read, so
// Detect's variant handling can be exercised for Bluefin, Bluefin LTS, and
// Dakota on a host that is none of them. Its production value is always
// imageinfo.Detect.
var detectInfo = imageinfo.Detect

// descriptorOverride, when non-empty, is the path Detect reads the image
// descriptor from instead of imageinfo.DescriptorPath.
//
// It exists so the screenshot walkthrough can render the Bluefin-family rows
// on a host that is not a Bluefin system — otherwise the walkthrough would
// only ever capture those rows hidden, and could not verify them at all.
//
// Three properties keep it from being a privilege hole:
//
//   - It is set only by SetDescriptorOverride, which internal/app calls
//     exclusively inside the --dry-run branch. In dry-run every mutation
//     short-circuits before pkexec, so a spoofed descriptor can change only
//     what a session that is making no changes displays.
//   - It affects this unprivileged, read-only detection alone. The
//     privileged helper resolves its own descriptor from
//     imageinfo.DescriptorPath and never consults this value or the
//     environment it came from, so a spoofed descriptor cannot influence the
//     image reference handed to bootc.
//   - It carries no authority of its own: the channel table it resolves
//     against still comes from the root-owned system paths.
var descriptorOverride string

// SetDescriptorOverride points the unprivileged image-descriptor read at
// path. Callers must only use it in dry-run mode; see descriptorOverride.
func SetDescriptorOverride(path string) {
	descriptorOverride = path
	log.Printf("ublue image descriptor override: %s", path)
}

func Detect() (Status, error) {
	detect := detectInfo
	if descriptorOverride != "" {
		path := descriptorOverride
		detect = func() (imageinfo.Info, error) { return imageinfo.Load(path) }
	}

	info, err := detect()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Status{}, nil
		}
		return Status{}, &Error{Message: fmt.Sprintf("reading image descriptor: %v", err)}
	}

	status := Status{
		Available: true,
		Variant:   info.Variant(),
		Channel:   info.Channel(),
		Tag:       info.EffectiveTag(),
		Ref:       info.CleanRef(),
	}

	groups, err := currentUserGroups()
	if err != nil {
		log.Printf("developer-mode group lookup failed: %v", err)
	}
	status.Developer, status.DevGroups = DeveloperState(groups)

	status.Driver = info.Driver()
	hardware := detectGPU()
	status.GPU = hardware.Describe()
	if recommended, offer := info.RecommendedDriver(hardware.NVIDIA); offer {
		status.RecommendedDriver = recommended
	}

	// Offer the switch only in the direction the running tag actually has a
	// counterpart for. A pinned or unrecognized tag offers neither.
	if _, ok := info.SwitchTarget(imageinfo.ChannelTesting); ok {
		status.CanSwitchTo = imageinfo.ChannelTesting
	} else if _, ok := info.SwitchTarget(imageinfo.ChannelStable); ok {
		status.CanSwitchTo = imageinfo.ChannelStable
	} else {
		status.CanSwitchTo = imageinfo.ChannelUnknown
	}

	return status, nil
}

// DeveloperState reports whether developer mode is active for a user with
// the supplied group names, and returns the matching developer groups in
// ubluehelper.DevGroups order. Membership in any one developer group counts
// as active, mirroring bluefinctl's _check_devmode_active.
func DeveloperState(groups []string) (bool, []string) {
	if len(groups) == 0 {
		return false, nil
	}

	present := make(map[string]bool, len(groups))
	for _, group := range groups {
		present[group] = true
	}

	matched := make([]string, 0, len(ubluehelper.DevGroups()))
	for _, group := range ubluehelper.DevGroups() {
		if present[group] {
			matched = append(matched, group)
		}
	}
	if len(matched) == 0 {
		return false, nil
	}
	return true, matched
}

// detectGPU is an injection seam for the graphics-hardware probe, so
// Detect's driver recommendation is testable on any machine.
var detectGPU = gpu.Detect

// lookupGroups is an injection seam so DeveloperState's caller can be tested
// without depending on the test host's real group membership. Its production
// value resolves the invoking user's supplementary groups.
var lookupGroups = osUserGroups

func currentUserGroups() ([]string, error) {
	return lookupGroups()
}

func osUserGroups() ([]string, error) {
	account, err := user.Current()
	if err != nil {
		return nil, err
	}
	ids, err := account.GroupIds()
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(ids))
	for _, id := range ids {
		group, err := user.LookupGroupId(id)
		if err != nil {
			continue
		}
		names = append(names, group.Name)
	}
	return names, nil
}

// SwitchChannel stages a switch of this host to the requested release
// channel. Only the channel word crosses the pkexec boundary; the helper
// resolves the image reference itself.
func SwitchChannel(ctx context.Context, channel imageinfo.Channel) error {
	if channel != imageinfo.ChannelStable && channel != imageinfo.ChannelTesting {
		return &Error{Message: fmt.Sprintf("unsupported channel %q", channel)}
	}
	_, _, err := runHelper(ctx, pkexecCommand, ubluehelper.CommandChannelSwitch, string(channel))
	return err
}

// SetDeveloperMode adds or removes the invoking user's developer-group
// membership. No username crosses the pkexec boundary; the helper resolves
// it from PKEXEC_UID.
func SetDeveloperMode(ctx context.Context, enabled bool) error {
	command := ubluehelper.CommandDXDisable
	if enabled {
		command = ubluehelper.CommandDXEnable
	}
	_, _, err := runHelper(ctx, pkexecCommand, command)
	return err
}

// Restart restarts the machine. It is the only ChairLift action that ends the
// user's session, so callers must confirm before reaching it.
func Restart(ctx context.Context) error {
	_, _, err := runHelper(ctx, pkexecCommand, ubluehelper.CommandRestart)
	return err
}

// Rollback makes the previous deployment the default for the next boot. It
// does not restart the machine; Restart is a separate, separately confirmed
// action.
func Rollback(ctx context.Context) error {
	_, _, err := runHelper(ctx, pkexecCommand, ubluehelper.CommandRollback)
	return err
}

// FactoryReset replaces the running deployment with a fresh install of the
// same image, discarding every local change. It is irreversible: callers
// must confirm with the user before reaching this — see
// pageview.FactoryResetConfirmation — because there is nothing this function
// or the privileged helper behind it can undo once bootc applies the reset.
func FactoryReset(ctx context.Context) error {
	_, _, err := runHelper(ctx, pkexecCommand, ubluehelper.CommandFactoryReset)
	return err
}

// SetAutomaticUpdates turns unattended background updates on or off.
func SetAutomaticUpdates(ctx context.Context, enabled bool) error {
	command := ubluehelper.CommandAutoDisable
	if enabled {
		command = ubluehelper.CommandAutoEnable
	}
	_, _, err := runHelper(ctx, pkexecCommand, command)
	return err
}

// SwitchDriver moves this host to a different graphics-driver image on the
// same stream. Only the driver word crosses the pkexec boundary; the helper
// resolves the image reference itself.
func SwitchDriver(ctx context.Context, driver imageinfo.Driver) error {
	switch driver {
	case imageinfo.DriverStandard, imageinfo.DriverNVIDIA, imageinfo.DriverNVIDIAOpen:
	default:
		return &Error{Message: fmt.Sprintf("unsupported graphics driver %q", driver)}
	}
	_, _, err := runHelper(ctx, pkexecCommand, ubluehelper.CommandDriverSwitch, string(driver))
	return err
}

// runHelper executes HelperPath via pkexec for privileged operations. The
// plumbing — dry-run short-circuit, journaling, failure classification — is
// owned by internal/helperexec; this wrapper only binds the package's fixed
// HelperPath. pkexecPath stays an explicit parameter (mirroring
// internal/stageexec.Run's executable seam) so tests can substitute a fake
// pkexec stand-in without invoking the real pkexec/polkit stack or
// requiring root. HelperPath itself is never overridden: it is the fixed
// absolute path that must match the policy's exec.path annotation, so tests
// assert it by inspecting the fake pkexec's captured argv. Every invocation
// — dry-run or live — is journal.Record'd, so a test can assert the argv
// ChairLift assembled without granting privilege; see internal/journal.
func runHelper(ctx context.Context, pkexecPath string, args ...string) (string, string, error) {
	return helperexec.Run(ctx, pkexecPath, HelperPath, args...)
}

var (
	availableOnce   sync.Once
	availableResult Status
)

// StatusCached returns a cached Detect() result, running the detection at
// most once per process. The image descriptor cannot change without a
// reboot, so re-reading it on every page build would be pure overhead.
func StatusCached() Status {
	availableOnce.Do(func() {
		status, err := Detect()
		if err != nil {
			log.Printf("ublue detection failed: %v", err)
			return
		}
		availableResult = status
	})
	return availableResult
}
