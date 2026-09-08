// Package gaming implements ChairLift's gaming mode for the Bluefin family
// (Bluefin, Bluefin LTS, and Dakota): a one-switch install of the gaming
// stack those images do not ship by default.
//
// Unlike the release-channel switch and developer mode, gaming mode crosses
// no privilege boundary. Every component is a user-scope Flatpak, installed
// with `flatpak install --user`, so the whole feature runs as the invoking
// user with no pkexec, no PolicyKit action, and no privileged helper — the
// same reasoning that keeps Homebrew tap trust unprivileged. That is also
// why it is safe on a bootc host: nothing is layered onto the image, so a
// system update never has to reconcile it.
//
// This has no bluefinctl counterpart. bluefinctl ships no gaming feature at
// all, so the component list below is ChairLift's own choice rather than a
// port of an upstream definition.
package gaming

import (
	"fmt"
	"strings"

	"github.com/projectbluefin/chairlift/internal/flatpak"
)

// Component is one Flatpak in the gaming stack.
type Component struct {
	// ApplicationID is the Flathub app ID installed and removed.
	ApplicationID string
	// Name is the display name shown in the ChairLift row.
	Name string
	// Description explains what the component contributes.
	Description string
	// Core marks the components without which gaming mode is meaningless.
	// Gaming mode reports as "on" only when every core component is
	// present; the rest are conveniences that may be removed individually
	// without turning the feature off.
	Core bool
}

// components is the canonical gaming stack, in install order. Steam and
// Proton-compatibility management are core; the overlay and controller
// tooling are additive.
var components = []Component{
	{
		ApplicationID: "com.valvesoftware.Steam",
		Name:          "Steam",
		Description:   "Valve's game client, with Proton for Windows titles",
		Core:          true,
	},
	{
		ApplicationID: "net.davidotek.pupgui2",
		Name:          "ProtonUp-Qt",
		Description:   "Installs and manages Proton-GE and Wine-GE compatibility tools",
		Core:          true,
	},
	{
		ApplicationID: "com.github.Matoking.protontricks",
		Name:          "Protontricks",
		Description:   "Per-game Winetricks workarounds for Proton prefixes",
	},
	{
		ApplicationID: "io.github.benjamimgois.goverlay",
		Name:          "GOverlay",
		Description:   "Configures the MangoHud performance overlay",
	},
	{
		ApplicationID: "org.freedesktop.Platform.VulkanLayer.MangoHud",
		Name:          "MangoHud",
		Description:   "In-game FPS, frametime, and hardware overlay",
	},
	{
		ApplicationID: "com.github.tchx84.Flatseal",
		Name:          "Flatseal",
		Description:   "Grants games access to controllers and external drives",
	},
}

// Components returns the canonical gaming stack in install order. The
// returned slice is freshly allocated on every call, so callers cannot
// mutate the package's definition of the feature.
func Components() []Component {
	result := make([]Component, len(components))
	copy(result, components)
	return result
}

// CoreComponents returns only the components whose absence means gaming mode
// is off.
func CoreComponents() []Component {
	result := make([]Component, 0, len(components))
	for _, component := range components {
		if component.Core {
			result = append(result, component)
		}
	}
	return result
}

// Scope records where each installed component lives. ChairLift installs
// into the user scope; an image may preinstall a component system-wide.
type Scope struct {
	// Installed is every installed application ID, either scope.
	Installed map[string]bool
	// User is the subset installed in the user scope — the only components
	// ChairLift can remove, because removing a system-wide Flatpak needs
	// privilege ChairLift deliberately does not take for gaming mode.
	User map[string]bool
}

// State is the derived status of gaming mode on this host.
type State struct {
	// Enabled reports whether every core component is installed.
	Enabled bool
	// Installed lists the installed application IDs, in Components order.
	Installed []string
	// UserInstalled lists the installed application IDs ChairLift can
	// remove, in Components order.
	UserInstalled []string
	// SystemOnly lists installed application IDs that exist only in the
	// system scope. They count toward Enabled — the components are present
	// and usable — but Disable skips them rather than failing on an
	// uninstall it has no standing to perform.
	SystemOnly []string
	// Missing lists the not-yet-installed application IDs, in Components
	// order.
	Missing []string
	// MissingCore lists only the missing core application IDs. A non-empty
	// value with a non-empty Installed is the partial state — some of the
	// stack is present but the feature is not usable.
	MissingCore []string
}

// Summary returns the one-line row subtitle for the state.
func (s State) Summary() string {
	total := len(components)
	switch {
	case s.Enabled && len(s.Missing) == 0:
		return fmt.Sprintf("All %d gaming components installed", total)
	case s.Enabled:
		return fmt.Sprintf("%d of %d gaming components installed", len(s.Installed), total)
	case len(s.Installed) > 0:
		return fmt.Sprintf("Partly installed — %s missing", strings.Join(s.MissingCore, ", "))
	default:
		return fmt.Sprintf("Install Steam, Proton tooling, and %d more", total-2)
	}
}

// Derive computes gaming mode's state from a scope report. It is pure so the
// whole partial/complete/absent matrix — and the user/system split — is
// covered without a Flatpak installation.
func Derive(scope Scope) State {
	state := State{Enabled: true}
	for _, component := range components {
		id := component.ApplicationID
		if scope.Installed[id] {
			state.Installed = append(state.Installed, id)
			if scope.User[id] {
				state.UserInstalled = append(state.UserInstalled, id)
			} else {
				state.SystemOnly = append(state.SystemOnly, id)
			}
			continue
		}
		state.Missing = append(state.Missing, id)
		if component.Core {
			state.MissingCore = append(state.MissingCore, id)
			state.Enabled = false
		}
	}
	return state
}

// UserScope builds a Scope in which every listed ID is user-installed. It is
// the convenience form for callers and tests that do not care about the
// split.
func UserScope(ids []string) Scope {
	scope := Scope{
		Installed: make(map[string]bool, len(ids)),
		User:      make(map[string]bool, len(ids)),
	}
	for _, id := range ids {
		scope.Installed[id] = true
		scope.User[id] = true
	}
	return scope
}

// listInstalled is an injection seam for the Flatpak query, so Status's
// error handling and derivation can be tested without a Flatpak
// installation. Its production value queries both scopes: ChairLift installs
// to the user scope, but a component preinstalled system-wide by the image
// still counts as present — and must not be removed.
var listInstalled = installedApplications

func installedApplications() (Scope, error) {
	scope := Scope{Installed: map[string]bool{}, User: map[string]bool{}}

	userApps, userErr := flatpak.ListUserApplications()
	for _, app := range userApps {
		scope.Installed[app.ApplicationID] = true
		scope.User[app.ApplicationID] = true
	}

	systemApps, systemErr := flatpak.ListSystemApplications()
	for _, app := range systemApps {
		scope.Installed[app.ApplicationID] = true
	}

	// Only a failure of both scopes is fatal; one scope being unavailable
	// (no system remote configured, for instance) still yields a usable
	// answer.
	if userErr != nil && systemErr != nil {
		return Scope{}, fmt.Errorf("listing installed Flatpaks: %w", userErr)
	}

	return scope, nil
}

// Status returns gaming mode's current state on this host.
func Status() (State, error) {
	scope, err := listInstalled()
	if err != nil {
		return State{}, err
	}
	return Derive(scope), nil
}

// Enable installs every missing component into the user scope. It reports
// the components it installed and, separately, the ones that failed, so a
// single unavailable Flathub app does not abort the rest of the stack.
func Enable() (installed []string, failures []error) {
	state, err := Status()
	if err != nil {
		return nil, []error{err}
	}

	for _, id := range state.Missing {
		if err := flatpak.Install(id, true); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", id, err))
			continue
		}
		installed = append(installed, id)
	}
	return installed, failures
}

// Disable removes every user-scope component. Components the image
// preinstalled system-wide are reported as skipped, not attempted: ChairLift
// did not install them, and removing them would need privilege gaming mode
// deliberately does not take. Attempting them anyway would fail and report
// as an error for something that was never ChairLift's to remove.
func Disable() (removed []string, skipped []string, failures []error) {
	state, err := Status()
	if err != nil {
		return nil, nil, []error{err}
	}

	for _, id := range state.UserInstalled {
		if err := flatpak.Uninstall(id, true); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", id, err))
			continue
		}
		removed = append(removed, id)
	}
	return removed, state.SystemOnly, failures
}
