// Package developerapps owns the Developer page's curated, mixed-source IDE
// inventory and verified installation outcomes. It has no GTK dependency.
package developerapps

import (
	"errors"
	"fmt"
	"log"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/homebrew"
)

type Source string

const (
	Homebrew Source = "Homebrew"
	Flatpak  Source = "Flatpak"
)

// App identifies one native row and its explicitly selected package source.
type App struct {
	ID            string
	Name          string
	Description   string
	Source        Source
	Package       string
	Remote        string
	Architectures []string
}

// Status distinguishes verified absence, installed state, unavailable backend,
// and failed inventory reads. A read failure must never offer a fresh install.
type Status struct {
	Installed bool
	Available bool
	Reason    string
	Err       error
}

// Inspect batches reads by package manager. Either Flatpak scope proves an
// installed copy; a missing system remote does not invalidate an existing app.
func Inspect(apps []App) []Status {
	states := make([]Status, len(apps))
	var needBrew, needFlatpak bool
	for _, app := range apps {
		if len(app.Architectures) != 0 && !slices.Contains(app.Architectures, runtime.GOARCH) {
			continue
		}
		needBrew = needBrew || app.Source == Homebrew
		needFlatpak = needFlatpak || app.Source == Flatpak
	}
	var casks []homebrew.Package
	var brewErr, userErr, systemErr, remoteErr error
	var userApps, systemApps []flatpak.Application
	var remotes []string
	brewAvailable := homebrew.ExecutablePath() != ""
	_, flatpakErr := exec.LookPath("flatpak")
	if needBrew && brewAvailable {
		casks, brewErr = homebrew.ListInstalledCasks()
	}
	if needFlatpak && flatpakErr == nil {
		userApps, userErr = flatpak.ListUserApplications()
		systemApps, systemErr = flatpak.ListSystemApplications()
		remotes, remoteErr = flatpak.EnabledRemotes(false)
	}
	for i, app := range apps {
		state := &states[i]
		if len(app.Architectures) != 0 && !slices.Contains(app.Architectures, runtime.GOARCH) {
			state.Reason = "Not available for this computer's architecture."
			continue
		}
		switch app.Source {
		case Homebrew:
			if !brewAvailable {
				state.Reason = "Homebrew is not installed."
				continue
			}
			if !strings.HasPrefix(app.Package, "ublue-os/") || strings.Count(app.Package, "/") != 2 {
				state.Reason = "IDE packages must come from an ublue-os tap."
				continue
			}
			state.Err = brewErr
			for _, cask := range casks {
				if path.Base(cask.Name) == path.Base(app.Package) && cask.Version != "" {
					state.Installed = true
					break
				}
			}
			state.Available = brewErr == nil
		case Flatpak:
			if flatpakErr != nil {
				state.Reason = "Flatpak is not installed."
				continue
			}
			for _, inventory := range [][]flatpak.Application{userApps, systemApps} {
				for _, installed := range inventory {
					if installed.ApplicationID == app.Package {
						state.Installed = true
						break
					}
				}
			}
			if state.Installed {
				state.Available = true
				continue
			}
			state.Err = errors.Join(userErr, systemErr, remoteErr)
			state.Available = state.Err == nil && slices.Contains(remotes, app.Remote)
			if state.Err == nil && !state.Available {
				state.Reason = fmt.Sprintf("Enable the %s system remote to install.", app.Remote)
			}
		default:
			state.Reason = "Unsupported package source."
		}
	}
	return states
}

// Install runs the selected backend only after a successful installed-state
// check, and confirms the result against fresh inventory. Existing copies and
// dry-run previews never cause a duplicate installation or invented success.
func Install(app App) (Status, error) {
	before := Inspect([]App{app})[0]
	if before.Installed {
		return before, nil
	}
	if before.Err != nil {
		return before, fmt.Errorf("checking %s: %w", app.Name, before.Err)
	}
	if !before.Available {
		return before, errors.New(before.Reason)
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would install %s via %s: %s", app.Name, app.Source, app.Package)
		return before, nil
	}
	var err error
	switch app.Source {
	case Homebrew:
		if i := strings.LastIndex(app.Package, "/"); i > 0 {
			if err = homebrew.Tap(app.Package[:i]); err != nil {
				return before, err
			}
			if err = homebrew.TrustCask(app.Package); err != nil {
				return before, err
			}
		}
		err = homebrew.Install(app.Package, true)
	case Flatpak:
		err = flatpak.InstallFromRemote(app.Package, app.Remote, false)
	}
	if err != nil {
		return before, err
	}
	after := Inspect([]App{app})[0]
	if after.Err != nil {
		return after, fmt.Errorf("verifying %s: %w", app.Name, after.Err)
	}
	if !after.Installed {
		return after, fmt.Errorf("%s is not installed after setup", app.Name)
	}
	return after, nil
}
