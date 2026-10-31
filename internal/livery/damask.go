package livery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/launcher"
)

// DamaskID is the third-party Flatpak application id of the Damask wallpaper
// manager. ChairLift ships no artwork and no rotation for it: it only offers
// to install and launch the app, never installing software on the user's
// behalf without an explicit action. The id follows the Bluefin convention of
// reverse-domain app ids.
const DamaskID = "app.drey.Damask"

// damaskScopes are the Flatpak scopes DamaskInstalled consults, user first.
// It is a variable so a test can point it at a fixture list.
var damaskScopes = []func() ([]flatpak.Application, error){
	flatpak.ListUserApplications,
	flatpak.ListSystemApplications,
}

// DamaskInstalled reports whether the external Damask Flatpak is present in
// either the user or system scope. Flatpak itself being absent
// (*flatpak.NotFoundError) is a genuine "not installed". Any other failed
// scope query, when no scope found Damask, is returned as an error: the probe
// could not tell, so the caller must show an unknown state rather than offer
// to install an app that may already be there.
func DamaskInstalled(ctx context.Context) (bool, error) {
	var probeErr error
	for _, list := range damaskScopes {
		apps, err := list()
		if err != nil {
			var notFound *flatpak.NotFoundError
			if !errors.As(err, &notFound) {
				probeErr = errors.Join(probeErr, err)
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

// damaskRemote is the Flatpak remote Damask is installed from. It is named
// explicitly so the install never depends on which remote flatpak picks by
// default.
const damaskRemote = "flathub"

// DamaskInstall installs Damask into the user scope from Flathub. It is a no-op when Damask is already installed, and it respects
// the dry-run gate.
func DamaskInstall(ctx context.Context) error {
	// A failed probe falls through to the install, which reports its own
	// error if Damask turns out to be present after all.
	if installed, _ := DamaskInstalled(ctx); installed {
		log.Printf("livery: %s already installed", DamaskID)
		return nil
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would install %s from Flathub (user scope)", DamaskID)
		return nil
	}
	return flatpak.InstallFromRemote(DamaskID, damaskRemote, true)
}

// damaskCommand is the launch for the installed Damask Flatpak. Desktops
// register no handler for an app:// URI, so xdg-open cannot start it;
// `flatpak run` is the launcher every Flatpak install provides.
func damaskCommand() *exec.Cmd {
	return exec.Command("flatpak", "run", DamaskID)
}

// launchDamask starts Damask without waiting for it: the manager is a
// long-lived window, and it must outlive the action's context. A later
// unsuccessful exit is only logged, because the click has already returned.
// It is a variable so a test can observe the launch without a real Flatpak.
var launchDamask = func(context.Context) error {
	if err := launcher.Start(damaskCommand(), func(err error) {
		log.Printf("livery: %s exited: %v", DamaskID, err)
	}); err != nil {
		return fmt.Errorf("livery: launching %s: %w", DamaskID, err)
	}
	return nil
}

// DamaskOpen launches the installed Damask Flatpak with `flatpak run`. It is
// a no-op under the dry-run gate.
func DamaskOpen(ctx context.Context) error {
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would launch %s", DamaskID)
		return nil
	}
	return launchDamask(ctx)
}
