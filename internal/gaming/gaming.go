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
// The stack is not uniformly applications. MangoHud is published as a
// Vulkan-layer extension of the freedesktop Platform runtime, so the
// inventory below queries applications and runtimes separately: `flatpak
// list --app` and `flatpak list --runtime` are mutually exclusive filters,
// and a component looked for under the wrong one reads as permanently
// missing.
//
// This has no bluefinctl counterpart. bluefinctl ships no gaming feature at
// all, so the component list below is ChairLift's own choice rather than a
// port of an upstream definition.
package gaming

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/projectbluefin/chairlift/internal/flatpak"
)

// Component is one Flatpak ref in the gaming stack. Not every one of them is
// an application: MangoHud ships as a Vulkan-layer extension of the
// freedesktop Platform runtime, so Kind is part of the definition rather than
// an assumption the inventory makes.
type Component struct {
	// ID is the Flathub ref ID installed and removed.
	ID string
	// Kind is the ref kind the component is published as, which decides
	// the `flatpak list` filter that can see it.
	Kind flatpak.Kind
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

// Ref identifies one installed ref: an ID together with its kind. The kind is
// part of the key because the two kinds come from separate `flatpak list`
// queries, so an inventory keyed on the ID alone cannot say which query is
// supposed to have answered for a given component.
type Ref struct {
	Kind flatpak.Kind
	ID   string
}

// Ref returns the component's inventory key.
func (c Component) Ref() Ref {
	return Ref{Kind: c.Kind, ID: c.ID}
}

// components is the canonical gaming stack, in install order. Steam and
// Proton-compatibility management are core; the overlay and controller
// tooling are additive.
var components = []Component{
	{
		ID:          "com.valvesoftware.Steam",
		Kind:        flatpak.KindApplication,
		Name:        "Steam",
		Description: "Valve's game client, with Proton for Windows titles",
		Core:        true,
	},
	{
		ID:          "net.davidotek.pupgui2",
		Kind:        flatpak.KindApplication,
		Name:        "ProtonUp-Qt",
		Description: "Installs and manages Proton-GE and Wine-GE compatibility tools",
		Core:        true,
	},
	{
		ID:          "com.github.Matoking.protontricks",
		Kind:        flatpak.KindApplication,
		Name:        "Protontricks",
		Description: "Per-game Winetricks workarounds for Proton prefixes",
	},
	{
		ID:          "io.github.benjamimgois.goverlay",
		Kind:        flatpak.KindApplication,
		Name:        "GOverlay",
		Description: "Configures the MangoHud performance overlay",
	},
	{
		// The one runtime component: MangoHud is a Vulkan layer extending
		// org.freedesktop.Platform, not an application, and `flatpak list
		// --app` never reports it.
		ID:          "org.freedesktop.Platform.VulkanLayer.MangoHud",
		Kind:        flatpak.KindRuntime,
		Name:        "MangoHud",
		Description: "In-game FPS, frametime, and hardware overlay",
	},
	{
		ID:          "com.github.tchx84.Flatseal",
		Kind:        flatpak.KindApplication,
		Name:        "Flatseal",
		Description: "Grants games access to controllers and external drives",
	},
}

// kinds returns the ref kinds the stack actually contains, in Components
// order and without duplicates. It is what decides which listings the
// inventory must be able to answer with.
func kinds() []flatpak.Kind {
	var result []flatpak.Kind
	for _, component := range components {
		if !slices.Contains(result, component.Kind) {
			result = append(result, component.Kind)
		}
	}
	return result
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
	// Installed is every installed ref, either scope.
	Installed map[Ref]bool
	// User is the subset installed in the user scope — the only components
	// ChairLift can remove, because removing a system-wide Flatpak needs
	// privilege ChairLift deliberately does not take for gaming mode.
	User map[Ref]bool
}

// State is the derived status of gaming mode on this host.
type State struct {
	// Enabled reports whether every core component is installed.
	Enabled bool
	// Installed lists the installed component IDs, in Components order.
	Installed []string
	// UserInstalled lists the installed component IDs ChairLift can
	// remove, in Components order.
	UserInstalled []string
	// SystemOnly lists installed component IDs that exist only in the
	// system scope. They count toward Enabled — the components are present
	// and usable — but Disable skips them rather than failing on an
	// uninstall it has no standing to perform.
	SystemOnly []string
	// Missing lists the not-yet-installed component IDs, in Components
	// order.
	Missing []string
	// MissingCore lists only the missing core component IDs. A non-empty
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
		ref := component.Ref()
		if scope.Installed[ref] {
			state.Installed = append(state.Installed, ref.ID)
			if scope.User[ref] {
				state.UserInstalled = append(state.UserInstalled, ref.ID)
			} else {
				state.SystemOnly = append(state.SystemOnly, ref.ID)
			}
			continue
		}
		state.Missing = append(state.Missing, ref.ID)
		if component.Core {
			state.MissingCore = append(state.MissingCore, ref.ID)
			state.Enabled = false
		}
	}
	return state
}

// kindOf returns the ref kind the stack declares for id. An ID that is not
// part of the stack is treated as an application, which is what an
// unrelated Flatpak in an inventory almost always is and, either way, cannot
// match a component.
func kindOf(id string) flatpak.Kind {
	for _, component := range components {
		if component.ID == id {
			return component.Kind
		}
	}
	return flatpak.KindApplication
}

// UserScope builds a Scope in which every listed ID is user-installed, each
// under the kind the stack declares for it. It is the convenience form for
// callers and tests that do not care about the user/system split.
func UserScope(ids []string) Scope {
	refs := make([]Ref, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, Ref{Kind: kindOf(id), ID: id})
	}
	return UserRefScope(refs)
}

// UserRefScope builds a Scope in which every listed ref is user-installed.
func UserRefScope(refs []Ref) Scope {
	scope := Scope{
		Installed: make(map[Ref]bool, len(refs)),
		User:      make(map[Ref]bool, len(refs)),
	}
	for _, ref := range refs {
		scope.Installed[ref] = true
		scope.User[ref] = true
	}
	return scope
}

// listInstalled is an injection seam for the Flatpak query, so Status's
// error handling and derivation can be tested without a Flatpak
// installation. Its production value queries both scopes: ChairLift installs
// to the user scope, but a component preinstalled system-wide by the image
// still counts as present — and must not be removed.
var listInstalled = installedComponents

// inventoryQuery is one `flatpak list` call: one installation scope, one ref
// kind. Both axes are needed because the two kinds are reported by mutually
// exclusive filters, so a stack containing a runtime extension cannot be
// inventoried by the application listing alone.
type inventoryQuery struct {
	kind flatpak.Kind
	user bool
	list func() ([]flatpak.Application, error)
}

var inventoryQueries = []inventoryQuery{
	{kind: flatpak.KindApplication, user: true, list: flatpak.ListUserApplications},
	{kind: flatpak.KindApplication, user: false, list: flatpak.ListSystemApplications},
	{kind: flatpak.KindRuntime, user: true, list: flatpak.ListUserRuntimes},
	{kind: flatpak.KindRuntime, user: false, list: flatpak.ListSystemRuntimes},
}

func installedComponents() (Scope, error) {
	scope := Scope{Installed: map[Ref]bool{}, User: map[Ref]bool{}}

	answered := map[flatpak.Kind]bool{}
	failures := map[flatpak.Kind][]error{}
	for _, query := range inventoryQueries {
		refs, err := query.list()
		if err != nil {
			failures[query.kind] = append(failures[query.kind], err)
			continue
		}
		answered[query.kind] = true
		for _, installed := range refs {
			ref := Ref{Kind: query.kind, ID: installed.ApplicationID}
			scope.Installed[ref] = true
			if query.user {
				scope.User[ref] = true
			}
		}
	}

	// One scope being unavailable (no system remote configured, for
	// instance) still yields a usable answer. A kind that answered in
	// neither scope does not: its components would be reported missing on
	// every refresh, which is Enable reinstalling them forever and Disable
	// never removing what ChairLift installed. Only the kinds the stack
	// actually contains are required.
	for _, kind := range kinds() {
		if answered[kind] {
			continue
		}
		return Scope{}, fmt.Errorf("listing installed Flatpak %s refs: %w", kind, errors.Join(failures[kind]...))
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
