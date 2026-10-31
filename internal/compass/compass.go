// Package compass installs and removes Compass, TunaOS's keyboard-first
// launcher (https://github.com/tuna-os/compass), as a system-scope Flatpak.
//
// Compass is published on FlatPark, not Flathub. Bluefin-family images ship
// FlatPark as a system remote (/usr/share/flatpak/remotes.d), so ChairLift
// installs from that remote and never adds one: a host without it reports
// Compass as unavailable. Like gaming mode, installation and removal are
// authorized by Flatpak's own PolicyKit actions; ChairLift adds no pkexec
// route. Removal keeps the user's Compass data, because `flatpak uninstall`
// without --delete-data leaves ~/.var/app in place.
package compass

import (
	"errors"
	"fmt"
	"runtime"
	"slices"

	"github.com/projectbluefin/chairlift/internal/flatpak"
)

const (
	// AppID is Compass's Flatpak application ID.
	AppID = "org.tunaos.compass"
	// Remote is the system remote Compass is installed from.
	Remote = "flatpark"
)

// ErrRemoteMissing reports that this computer has no enabled FlatPark
// system remote, so Compass cannot be installed.
var ErrRemoteMissing = errors.New("the FlatPark Flatpak remote is not configured on this computer")

// ErrUnsupported reports that Compass is not published for this architecture:
// FlatPark publishes only app/org.tunaos.compass/x86_64/stable.
var ErrUnsupported = errors.New("compass is only published for x86_64")

// State is Compass's observed installation state.
type State struct {
	// User and System report where a copy is installed.
	User, System bool
	// RemoteConfigured reports whether the system FlatPark remote exists.
	RemoteConfigured bool
	// Supported reports whether Compass is published for this architecture.
	Supported bool
}

// Installed reports whether any copy is installed.
func (s State) Installed() bool { return s.User || s.System }

// Injection seams so the decisions below are testable without Flatpak.
var (
	goarch     = runtime.GOARCH
	listUser   = flatpak.ListUserApplications
	listSystem = flatpak.ListSystemApplications
	remotes    = func() ([]string, error) { return flatpak.EnabledRemotes(false) }
	install    = func() error { return flatpak.InstallFromRemote(AppID, Remote, false) }
	uninstall  = flatpak.Uninstall
)

// Status reads Compass's state from both installations and the system remote
// list. Every query must answer: a failed read cannot be told apart from an
// absent copy.
func Status() (State, error) {
	state := State{Supported: goarch == "amd64"}
	for _, query := range []struct {
		list  func() ([]flatpak.Application, error)
		found *bool
	}{{listUser, &state.User}, {listSystem, &state.System}} {
		apps, err := query.list()
		if err != nil {
			return State{}, fmt.Errorf("listing installed Flatpaks: %w", err)
		}
		*query.found = slices.ContainsFunc(apps, func(app flatpak.Application) bool { return app.ApplicationID == AppID })
	}
	names, err := remotes()
	if err != nil {
		return State{}, fmt.Errorf("listing Flatpak remotes: %w", err)
	}
	state.RemoteConfigured = slices.Contains(names, Remote)
	return state, nil
}

// Enable installs Compass system-wide from FlatPark unless a copy is already
// installed in either scope. It returns whether it ran an install.
func Enable() (bool, error) {
	state, err := Status()
	if err != nil {
		return false, err
	}
	if state.Installed() {
		return false, nil
	}
	if !state.Supported {
		return false, ErrUnsupported
	}
	if !state.RemoteConfigured {
		return false, ErrRemoteMissing
	}
	if err := install(); err != nil {
		return false, fmt.Errorf("installing %s: %w", AppID, err)
	}
	return true, nil
}

// Disable removes Compass from every scope it is installed in, attempting
// both even after one fails. It returns whether it ran any removal.
func Disable() (bool, error) {
	state, err := Status()
	if err != nil {
		return false, err
	}
	var errs []error
	if state.User {
		if err := uninstall(AppID, true); err != nil {
			errs = append(errs, fmt.Errorf("user scope: %w", err))
		}
	}
	if state.System {
		if err := uninstall(AppID, false); err != nil {
			errs = append(errs, fmt.Errorf("system scope: %w", err))
		}
	}
	if len(errs) > 0 {
		return true, fmt.Errorf("removing %s: %w", AppID, errors.Join(errs...))
	}
	return state.Installed(), nil
}
