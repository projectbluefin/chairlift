package livery

import (
	"errors"
	"fmt"
	"log"
	"os/exec"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/launcher"
)

// DamaskID is the Flathub application ID of Damask, an external wallpaper
// manager for feeds such as Bing, Wallhaven, NASA APOD and Unsplash. ChairLift
// only installs and opens it on an explicit action: Damask owns its sources,
// rotation and settings, so nothing here reads or writes them.
const DamaskID = "app.drey.Damask"

// damaskRemote is the origin Damask is installed from, named explicitly so the
// install never depends on which remote flatpak would pick by default.
const damaskRemote = "flathub"

// damaskScopes are the installations DamaskInstalled consults, user first.
// Either copy means there is nothing to install. A variable so tests can
// substitute fixture inventories.
var damaskScopes = []struct {
	name string
	list func() ([]flatpak.Application, error)
}{
	{name: "user", list: flatpak.ListUserApplications},
	{name: "system", list: flatpak.ListSystemApplications},
}

// installDamask and launchDamask are the two mutations, variables so tests can
// observe them without a real Flatpak.
var (
	// Bluefin and Dakota configure Flathub as a system remote only, so a
	// user-scope install cannot resolve it (#501, #503). Flatpak authorizes the
	// system install through its own PolicyKit action; ChairLift adds no pkexec.
	installDamask = func() error {
		return flatpak.InstallFromRemote(DamaskID, damaskRemote, false)
	}
	launchDamask = func() error {
		return launcher.Start(damaskCommand(), func(err error) {
			log.Printf("livery: %s exited: %v", DamaskID, err)
		})
	}
)

// DamaskInstalled reports whether Damask is installed in either Flatpak scope.
//
// Flatpak itself being absent (*flatpak.NotFoundError) is a genuine "not
// installed": the caller decides whether Flatpak can install it. Any other
// failed query, when no scope found Damask, is returned as an error, so the
// caller shows an unknown state rather than offering to install an app that
// may already be there.
func DamaskInstalled() (bool, error) {
	var probeErr error
	for _, scope := range damaskScopes {
		apps, err := scope.list()
		if err != nil {
			var notFound *flatpak.NotFoundError
			if !errors.As(err, &notFound) {
				probeErr = errors.Join(probeErr, fmt.Errorf("listing %s flatpaks: %w", scope.name, err))
			}
			continue
		}
		for _, app := range apps {
			if app.ApplicationID == DamaskID {
				return true, nil
			}
		}
	}
	if probeErr != nil {
		return false, fmt.Errorf("livery: checking for %s: %w", DamaskID, probeErr)
	}
	return false, nil
}

// DamaskInstall installs Damask system-wide from Flathub unless it is already
// present. A failed probe is returned rather than installing over an unknown
// state. Under dry-run nothing is installed.
func DamaskInstall() error {
	installed, err := DamaskInstalled()
	if err != nil {
		return err
	}
	if installed {
		log.Printf("livery: %s already installed", DamaskID)
		return nil
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would install %s from %s (system scope)", DamaskID, damaskRemote)
		return nil
	}
	if err := installDamask(); err != nil {
		return fmt.Errorf("livery: installing %s: %w", DamaskID, err)
	}
	return nil
}

// damaskCommand launches the installed Damask. Desktops register no handler
// for an app ID, so xdg-open cannot start it; `flatpak run` is the launcher
// every Flatpak installation provides, and it runs the app's own desktop
// entry command inside its sandbox.
func damaskCommand() *exec.Cmd {
	return exec.Command("flatpak", "run", DamaskID)
}

// DamaskOpen starts the installed Damask without waiting for it: the manager is
// a long-lived window. A later unsuccessful exit is only logged. Under dry-run
// nothing is launched.
func DamaskOpen() error {
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would launch %s", DamaskID)
		return nil
	}
	if err := launchDamask(); err != nil {
		return fmt.Errorf("livery: launching %s: %w", DamaskID, err)
	}
	return nil
}
