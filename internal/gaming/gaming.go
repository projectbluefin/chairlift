// Package gaming implements gaming mode for the Bluefin family (Bluefin,
// Bluefin LTS, and Dakota): a single toggle for the gaming tools stack.
//
// Every component is a system-scope Flatpak, installed with
// `flatpak install --system` from the Flathub remote the images configure
// system-wide (#503). Bluefin's policy is that Flatpaks are installed
// system-wide, so runtimes are shared and the system update services keep
// them current for every account; an image that ships Flathub only as a
// system remote also cannot resolve a `--user` install at all (#501). The
// flatpak CLI authorizes a system install itself through Flatpak's own
// PolicyKit actions (org.freedesktop.Flatpak.app-install and its runtime and
// uninstall counterparts), so ChairLift adds no pkexec route, no PolicyKit
// action of its own, and no privileged helper for gaming mode. Nothing is
// layered onto the image either, so a bootc update never has to reconcile it.
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
	// Core marks the components required for gaming mode to report as "on".
	Core bool
	// BranchOf names the application whose runtime branch this component
	// must be installed in. Flathub publishes a Platform extension such as
	// MangoHud in one branch per Platform release, so a bare ID is
	// ambiguous: `flatpak install -y` and `flatpak uninstall -y` stop at a
	// "Which do you want to use?" prompt and fail with "No ref chosen". A
	// component with BranchOf set is therefore installed as ID//BRANCH, the
	// branch the named application loads, and removed by one qualified ref
	// per installed branch. Empty for single-branch components.
	BranchOf string
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

// components is the canonical gaming stack, in install order.
var components = []Component{
	{
		ID:          "com.vysp3r.ProtonPlus",
		Kind:        flatpak.KindApplication,
		Name:        "ProtonPlus",
		Description: "Adds newer versions of the tools that run Windows games",
		Core:        true,
	},
	{
		ID:          "com.github.Matoking.protontricks",
		Kind:        flatpak.KindApplication,
		Name:        "Protontricks",
		Description: "Applies fixes for Windows games that need extra help",
		Core:        true,
	},
	{
		ID:          "io.github.benjamimgois.goverlay",
		Kind:        flatpak.KindApplication,
		Name:        "GOverlay",
		Description: "Sets up the in-game performance display",
		Core:        true,
	},
	{
		// The one runtime component: MangoHud is a Vulkan layer extending
		// org.freedesktop.Platform, not an application, and `flatpak list
		// --app` never reports it. The layer must match the Platform branch
		// of the app that loads it: an installed Flatpak Steam when there is
		// one (see layerBranch), otherwise the branch Protontricks uses.
		ID:          "org.freedesktop.Platform.VulkanLayer.MangoHud",
		Kind:        flatpak.KindRuntime,
		Name:        "MangoHud",
		Description: "Shows frame rate and hardware use while you play",
		Core:        true,
		BranchOf:    "com.github.Matoking.protontricks",
	},
}

// kinds returns the ref kinds the stack actually contains, in install
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

// Components returns the ordered inventory without exposing mutable state.
func Components() []Component {
	return slices.Clone(components)
}

// Scope records where each installed component lives. ChairLift installs
// into the system scope; an earlier ChairLift release installed per-user, and
// a user may have installed a component for their own account, so both
// scopes are inventoried.
type Scope struct {
	// Installed is every installed ref, either scope.
	Installed map[Ref]bool
	// User is the subset installed in the user scope.
	User map[Ref]bool
	// System is the subset installed in the system scope.
	System map[Ref]bool
	// UserBranches and SystemBranches record each ref's installed branches
	// per scope, so a component installed in several branches can be
	// removed by qualified ref. A ref listed without a branch has none.
	UserBranches   map[Ref][]string
	SystemBranches map[Ref][]string
}

// State is the derived status of gaming mode on this host.
type State struct {
	// Enabled reports whether every core component is installed.
	Enabled bool
	// Installed lists the installed component IDs, in stack order.
	Installed []string
	// UserInstalled lists the installed component IDs with a copy in the
	// user scope, in stack order.
	UserInstalled []string
	// SystemInstalled lists the installed component IDs with a copy in the
	// system scope, in stack order. A component may appear in both lists.
	SystemInstalled []string
	// Missing lists the not-yet-installed component IDs, in stack order.
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
		return fmt.Sprintf("Install ProtonPlus, Protontricks, and %d more", total-2)
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
			}
			if scope.System[ref] {
				state.SystemInstalled = append(state.SystemInstalled, ref.ID)
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

// listInstalled is an injection seam for the Flatpak query, so Status's
// error handling and derivation can be tested without a Flatpak
// installation. Its production value queries both scopes: ChairLift installs
// to the system scope, but a copy an earlier release or the user put in the
// user scope still counts as present, so it is neither reinstalled nor
// hidden from removal.
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
	scope := Scope{
		Installed:      map[Ref]bool{},
		User:           map[Ref]bool{},
		System:         map[Ref]bool{},
		UserBranches:   map[Ref][]string{},
		SystemBranches: map[Ref][]string{},
	}

	failures := map[flatpak.Kind][]error{}
	for _, query := range inventoryQueries {
		refs, err := query.list()
		if err != nil {
			failures[query.kind] = append(failures[query.kind], err)
			continue
		}
		// Both scopes must answer; otherwise absent/user/system classification
		// could claim missing for an installed component or conceal a user copy.
		for _, installed := range refs {
			ref := Ref{Kind: query.kind, ID: installed.ApplicationID}
			scope.Installed[ref] = true
			scopeRefs, branches := scope.System, scope.SystemBranches
			if query.user {
				scopeRefs, branches = scope.User, scope.UserBranches
			}
			scopeRefs[ref] = true
			if installed.Branch != "" && !slices.Contains(branches[ref], installed.Branch) {
				branches[ref] = append(branches[ref], installed.Branch)
			}
		}
	}

	for _, kind := range kinds() {
		if len(failures[kind]) != 0 {
			return Scope{}, fmt.Errorf("listing installed Flatpak %s refs: %w", kind, errors.Join(failures[kind]...))
		}
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

// ProgressCallback reports progress for gaming component install or removal.
type ProgressCallback func(step, total int, componentName string)

// Enable installs every missing component into the system scope.
// A component already present in either scope is not reinstalled, and a
// component failure does not abort the remaining entries.
func Enable() (installed []string, failures []error) {
	return EnableWithProgress(nil)
}

// EnableWithProgress installs missing components while reporting progress per component.
func EnableWithProgress(progress ProgressCallback) (installed []string, failures []error) {
	state, err := Status()
	if err != nil {
		return nil, []error{err}
	}

	var toInstall []Component
	for _, component := range components {
		if slices.Contains(state.Missing, component.ID) {
			toInstall = append(toInstall, component)
		}
	}

	for idx, component := range toInstall {
		id := component.ID
		if progress != nil {
			progress(idx, len(toInstall), component.Name)
		}
		ref := id
		if component.BranchOf != "" {
			branch, err := layerBranch(component)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", id, err))
				continue
			}
			ref = id + "//" + branch
		}
		if err := flatpak.Install(ref, false); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", id, err))
			continue
		}
		installed = append(installed, id)
	}
	return installed, failures
}

// flathubRemote is the system remote the Bluefin family configures and gaming
// installs from.
const flathubRemote = "flathub"

// steamAppID is the Flatpak Steam client. It is not part of the stack, but
// when it is installed it is what loads the MangoHud layer into games.
const steamAppID = "com.valvesoftware.Steam"

// layerBranch returns the branch a multi-branch component is installed in:
// the runtime branch of an installed Flatpak Steam, since that is the app
// whose games load the layer, and otherwise the branch of
// component.BranchOf.
func layerBranch(component Component) (string, error) {
	if runtime, err := flatpak.AppRuntime(steamAppID); err == nil {
		if branch, err := flatpak.RefBranch(runtime); err == nil {
			return branch, nil
		}
	}
	return runtimeBranch(component.BranchOf)
}

// runtimeBranch returns the branch of the runtime appID runs on: the
// installed copy's when there is one (Protontricks installs ahead of
// MangoHud, so enabling answers from the fresh install), otherwise the one
// Flathub currently publishes it against, which is what a later install
// would get.
func runtimeBranch(appID string) (string, error) {
	runtime, installedErr := flatpak.AppRuntime(appID)
	if installedErr != nil {
		var remoteErr error
		runtime, remoteErr = flatpak.RemoteAppRuntime(flathubRemote, appID, false)
		if remoteErr != nil {
			return "", fmt.Errorf("reading the runtime %s uses: %w", appID, errors.Join(installedErr, remoteErr))
		}
	}
	branch, err := flatpak.RefBranch(runtime)
	if err != nil {
		return "", fmt.Errorf("reading the runtime %s uses: %w", appID, err)
	}
	return branch, nil
}

// removalRefs returns the refs one scope's copy of component is uninstalled
// by: one branch-qualified ref per installed branch for a multi-branch
// component, otherwise the bare ID.
func removalRefs(component Component, branches []string) []string {
	if component.BranchOf == "" || len(branches) == 0 {
		return []string{component.ID}
	}
	refs := make([]string, 0, len(branches))
	for _, branch := range branches {
		refs = append(refs, component.ID+"//"+branch)
	}
	return refs
}

// uninstallAll removes every ref from one scope, attempting each one even
// after a failure.
func uninstallAll(refs []string, user bool) error {
	var errs []error
	for _, ref := range refs {
		if err := flatpak.Uninstall(ref, user); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Disable removes each installed component from the scopes the inventory
// observed it in: a user-scope copy (an earlier ChairLift release installed
// per-user) is removed with `--user` and needs no authorization, a
// system-scope copy with `--system` under Flatpak's own PolicyKit. Scopes a
// component is not installed in are never touched.
//
// A system copy of a component the OS image declares it ships (imageShipped)
// is left in place: it predates gaming mode, and removing it would take a
// distro default away from every account. Such a component is reported in
// kept — after its user copy, if any, is removed — rather than as removed,
// because it is still installed. A component counts as removed only when
// every one of its copies was; otherwise it is one failure naming each scope
// that could not be removed. A multi-branch component (BranchOf) is removed
// from each scope by one qualified ref per branch observed there, because a
// bare ID matching several installed branches fails at flatpak's prompt.
func Disable() (removed, kept []string, failures []error) {
	return DisableWithProgress(nil)
}

// DisableWithProgress removes installed components while reporting progress per component.
func DisableWithProgress(progress ProgressCallback) (removed, kept []string, failures []error) {
	scope, err := listInstalled()
	if err != nil {
		return nil, nil, []error{err}
	}
	state := Derive(scope)
	var shipped map[string]bool
	if len(state.SystemInstalled) > 0 {
		if shipped, err = imageShipped(); err != nil {
			return nil, nil, []error{fmt.Errorf("reading the Flatpaks the system image ships: %w", err)}
		}
	}

	type disableItem struct {
		component          Component
		hasUser            bool
		hasUnshippedSystem bool
		keep               bool
	}
	var toProcess []disableItem
	var toRemoveCount int
	for _, component := range components {
		id := component.ID
		if !slices.Contains(state.Installed, id) {
			continue
		}
		hasUser := slices.Contains(state.UserInstalled, id)
		inSystem := slices.Contains(state.SystemInstalled, id)
		hasUnshippedSystem := inSystem && !shipped[id]
		keep := inSystem && shipped[id]
		toProcess = append(toProcess, disableItem{
			component:          component,
			hasUser:            hasUser,
			hasUnshippedSystem: hasUnshippedSystem,
			keep:               keep,
		})
		if hasUser || hasUnshippedSystem {
			toRemoveCount++
		}
	}

	step := 0
	for _, item := range toProcess {
		id := item.component.ID
		ref := item.component.Ref()
		var errs []error
		if (item.hasUser || item.hasUnshippedSystem) && progress != nil {
			progress(step, toRemoveCount, item.component.Name)
			step++
		}
		if item.hasUser {
			if err := uninstallAll(removalRefs(item.component, scope.UserBranches[ref]), true); err != nil {
				errs = append(errs, fmt.Errorf("user scope: %w", err))
			}
		}
		if item.hasUnshippedSystem {
			if err := uninstallAll(removalRefs(item.component, scope.SystemBranches[ref]), false); err != nil {
				errs = append(errs, fmt.Errorf("system scope: %w", err))
			}
		}
		switch {
		case len(errs) != 0:
			failures = append(failures, fmt.Errorf("%s: %w", id, errors.Join(errs...)))
		case item.keep:
			kept = append(kept, id)
		default:
			removed = append(removed, id)
		}
	}
	return removed, kept, failures
}
