package gaming

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/flatpak"
)

const (
	steam       = "com.valvesoftware.Steam"
	protonUp    = "net.davidotek.pupgui2"
	protontrick = "com.github.Matoking.protontricks"
	goverlay    = "io.github.benjamimgois.goverlay"
	mangohud    = "org.freedesktop.Platform.VulkanLayer.MangoHud"
	flatseal    = "com.github.tchx84.Flatseal"
)

func allComponentIDs() []string {
	ids := make([]string, 0, len(components))
	for _, component := range components {
		ids = append(ids, component.ID)
	}
	return ids
}

// refOf is the inventory key production code builds for an ID: its declared
// kind plus the ID. Looking a component up under the other kind is the
// mistake the Ref key exists to make visible.
func refOf(id string) Ref {
	return Ref{Kind: kindOf(id), ID: id}
}

func refsOf(ids ...string) map[Ref]bool {
	refs := make(map[Ref]bool, len(ids))
	for _, id := range ids {
		refs[refOf(id)] = true
	}
	return refs
}

func TestComponentsHaveUniqueIDsAndDisplayText(t *testing.T) {
	got := components
	if len(got) == 0 {
		t.Fatal("gaming stack is empty")
	}

	seen := make(map[string]bool, len(got))
	for _, component := range got {
		if component.ID == "" {
			t.Errorf("component %q has no ref ID", component.Name)
		}
		if component.Name == "" || component.Description == "" {
			t.Errorf("component %q is missing display text", component.ID)
		}
		if seen[component.ID] {
			t.Errorf("component %q appears twice", component.ID)
		}
		seen[component.ID] = true
	}
}

// Kind is not a decoration: it selects the `flatpak list` filter a component
// is looked for under, and the zero value would silently be neither of the
// two valid kinds.
func TestEveryComponentDeclaresARefKind(t *testing.T) {
	for _, component := range components {
		switch component.Kind {
		case flatpak.KindApplication, flatpak.KindRuntime:
		default:
			t.Errorf("component %q has kind %q, want %q or %q",
				component.ID, component.Kind, flatpak.KindApplication, flatpak.KindRuntime)
		}
	}
}

// MangoHud ships as a Vulkan-layer extension of org.freedesktop.Platform, so
// classifying it as an application is what made it read as permanently
// missing: `flatpak list --app` cannot see it no matter how it was installed.
func TestMangoHudIsModelledAsARuntimeExtension(t *testing.T) {
	for _, component := range components {
		if component.ID != mangohud {
			continue
		}
		if component.Kind != flatpak.KindRuntime {
			t.Fatalf("MangoHud kind = %q, want %q", component.Kind, flatpak.KindRuntime)
		}
		return
	}
	t.Fatalf("MangoHud (%q) is not in the gaming stack", mangohud)
}

// The stack contains both kinds, which is why the inventory cannot collapse
// back into a single listing.
func TestStackKindsCoverApplicationsAndRuntimes(t *testing.T) {
	if got, want := kinds(), []flatpak.Kind{flatpak.KindApplication, flatpak.KindRuntime}; !reflect.DeepEqual(got, want) {
		t.Errorf("kinds() = %v, want %v", got, want)
	}
}

func TestCoreComponentsDefineTheOnOffThreshold(t *testing.T) {
	wantCore := []string{steam, protonUp}
	var got []string
	for _, component := range components {
		if component.Core {
			got = append(got, component.ID)
		}
	}
	if !reflect.DeepEqual(got, wantCore) {
		t.Fatalf("core component IDs = %v, want %v", got, wantCore)
	}
}

func TestDeriveClassifiesEveryInstallationShape(t *testing.T) {
	tests := []struct {
		name            string
		installed       []string
		wantEnabled     bool
		wantInstalled   []string
		wantMissingCore []string
	}{
		{
			name:          "nothing installed",
			installed:     nil,
			wantEnabled:   false,
			wantInstalled: nil,
			// Reported in stack order, not alphabetical.
			wantMissingCore: []string{steam, protonUp},
		},
		{
			name:            "everything installed",
			installed:       allComponentIDs(),
			wantEnabled:     true,
			wantInstalled:   allComponentIDs(),
			wantMissingCore: nil,
		},
		{
			name:            "core only is still on",
			installed:       []string{steam, protonUp},
			wantEnabled:     true,
			wantInstalled:   []string{steam, protonUp},
			wantMissingCore: nil,
		},
		{
			// The trap this guards: a host with the extras but no Steam must
			// not read as gaming mode on.
			name:            "extras without core is off",
			installed:       []string{protontrick, goverlay, mangohud, flatseal},
			wantEnabled:     false,
			wantInstalled:   []string{protontrick, goverlay, mangohud, flatseal},
			wantMissingCore: []string{steam, protonUp},
		},
		{
			name:            "one core component missing",
			installed:       []string{steam, mangohud},
			wantEnabled:     false,
			wantInstalled:   []string{steam, mangohud},
			wantMissingCore: []string{protonUp},
		},
		{
			name:            "unrelated apps are ignored",
			installed:       []string{"org.mozilla.firefox", steam, protonUp},
			wantEnabled:     true,
			wantInstalled:   []string{steam, protonUp},
			wantMissingCore: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := Derive(UserScope(test.installed))
			if state.Enabled != test.wantEnabled {
				t.Errorf("Derive(%v).Enabled = %v, want %v", test.installed, state.Enabled, test.wantEnabled)
			}
			if !reflect.DeepEqual(state.Installed, test.wantInstalled) {
				t.Errorf("Derive(%v).Installed = %v, want %v", test.installed, state.Installed, test.wantInstalled)
			}
			if !reflect.DeepEqual(state.MissingCore, test.wantMissingCore) {
				t.Errorf("Derive(%v).MissingCore = %v, want %v", test.installed, state.MissingCore, test.wantMissingCore)
			}
			if len(state.Installed)+len(state.Missing) != len(components) {
				t.Errorf("Derive(%v) accounted for %d of %d components",
					test.installed, len(state.Installed)+len(state.Missing), len(components))
			}
		})
	}
}

func TestSummaryDescribesEachState(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		wantHas   string
	}{
		{name: "off", installed: nil, wantHas: "Install Steam"},
		{name: "partial", installed: []string{steam}, wantHas: "Partly installed"},
		{name: "on but incomplete", installed: []string{steam, protonUp}, wantHas: "of 6 gaming components"},
		{name: "complete", installed: allComponentIDs(), wantHas: "All 6 gaming components"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Derive(UserScope(test.installed)).Summary()
			if !strings.Contains(got, test.wantHas) {
				t.Errorf("Summary() = %q, want it to contain %q", got, test.wantHas)
			}
		})
	}
}

func stubInstalled(t *testing.T, ids []string, err error) {
	t.Helper()
	stubScope(t, UserScope(ids), err)
}

func stubScope(t *testing.T, scope Scope, err error) {
	t.Helper()

	previous := listInstalled
	listInstalled = func() (Scope, error) { return scope, err }
	t.Cleanup(func() { listInstalled = previous })
}

func TestStatusUsesTheFlatpakQuery(t *testing.T) {
	stubInstalled(t, []string{steam, protonUp, flatseal}, nil)

	state, err := Status()
	if err != nil {
		t.Fatalf("Status() error = %v, want nil", err)
	}
	if !state.Enabled {
		t.Errorf("Status().Enabled = false, want true with both core components installed")
	}
}

func TestStatusPropagatesQueryFailure(t *testing.T) {
	stubInstalled(t, nil, errors.New("flatpak not installed"))

	if _, err := Status(); err == nil {
		t.Fatal("Status() error = nil, want the query failure surfaced rather than reported as 'off'")
	}
}

// Enable and Disable both begin with Status, so a failing query must abort
// before any install or uninstall is attempted.
func TestEnableAndDisableAbortOnQueryFailure(t *testing.T) {
	stubInstalled(t, nil, errors.New("flatpak not installed"))

	if installed, failures := Enable(); installed != nil || len(failures) != 1 {
		t.Errorf("Enable() = (%v, %v), want (nil, one error)", installed, failures)
	}
	if removed, skipped, failures := Disable(); removed != nil || skipped != nil || len(failures) != 1 {
		t.Errorf("Disable() = (%v, %v, %v), want (nil, nil, one error)", removed, skipped, failures)
	}
}

// A component the image preinstalled system-wide is present — it counts
// toward gaming mode being on — but ChairLift did not install it and cannot
// remove it without privilege gaming mode deliberately does not take.
func TestDeriveSplitsUserAndSystemInstallations(t *testing.T) {
	scope := Scope{
		Installed: refsOf(steam, protonUp, mangohud),
		User:      refsOf(protonUp),
	}

	state := Derive(scope)
	if !state.Enabled {
		t.Error("Derive().Enabled = false, want true — both core components are present regardless of scope")
	}
	if !reflect.DeepEqual(state.Installed, []string{steam, protonUp, mangohud}) {
		t.Errorf("Derive().Installed = %v, want every present component", state.Installed)
	}
	if !reflect.DeepEqual(state.UserInstalled, []string{protonUp}) {
		t.Errorf("Derive().UserInstalled = %v, want only the user-scope component", state.UserInstalled)
	}
	if !reflect.DeepEqual(state.SystemOnly, []string{steam, mangohud}) {
		t.Errorf("Derive().SystemOnly = %v, want the system-scope components", state.SystemOnly)
	}
}

// The regression this guards: iterating Installed rather than UserInstalled
// makes Disable attempt a user-scope uninstall of a system-wide component,
// which fails and is reported to the user as an error for a component that
// was never ChairLift's to remove.
func TestDisableSkipsSystemScopeComponentsInsteadOfFailingOnThem(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam, protonUp),
		User:      refsOf(),
	}, nil)

	removed, skipped, failures := Disable()
	if len(failures) != 0 {
		t.Errorf("Disable() failures = %v, want none for components installed system-wide", failures)
	}
	if len(removed) != 0 {
		t.Errorf("Disable() removed = %v, want none", removed)
	}
	if !reflect.DeepEqual(skipped, []string{steam, protonUp}) {
		t.Errorf("Disable() skipped = %v, want both system-scope components", skipped)
	}
}
