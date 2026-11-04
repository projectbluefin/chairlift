package gaming

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/flatpak"
)

const (
	protonPlus  = "com.vysp3r.ProtonPlus"
	protontrick = "com.github.Matoking.protontricks"
	goverlay    = "io.github.benjamimgois.goverlay"
	mangohud    = "org.freedesktop.Platform.VulkanLayer.MangoHud"

	steam      = "com.valvesoftware.Steam"
	protonUpQt = "net.davidotek.pupgui2"
	flatseal   = "com.github.tchx84.Flatseal"
)

func allComponentIDs() []string {
	ids := make([]string, 0, len(components))
	for _, component := range components {
		ids = append(ids, component.ID)
	}
	return ids
}

// kindOf returns the ref kind the stack declares for id. An ID that is not
// part of the stack is treated as an application, which is what an unrelated
// Flatpak in an inventory almost always is.
func kindOf(id string) flatpak.Kind {
	for _, component := range components {
		if component.ID == id {
			return component.Kind
		}
	}
	return flatpak.KindApplication
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

// systemScope builds a Scope in which every listed ID is installed in the
// system scope only, where Enable puts it.
func systemScope(ids []string) Scope {
	return Scope{Installed: refsOf(ids...), User: refsOf(), System: refsOf(ids...)}
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
	wantCore := []string{protonPlus, protontrick, goverlay, mangohud}
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
			wantMissingCore: []string{protonPlus, protontrick, goverlay, mangohud},
		},
		{
			name:            "everything installed",
			installed:       allComponentIDs(),
			wantEnabled:     true,
			wantInstalled:   allComponentIDs(),
			wantMissingCore: nil,
		},
		{
			name:            "core missing one",
			installed:       []string{protonPlus, protontrick, goverlay},
			wantEnabled:     false,
			wantInstalled:   []string{protonPlus, protontrick, goverlay},
			wantMissingCore: []string{mangohud},
		},
		{
			name:            "unrelated apps are ignored",
			installed:       append([]string{"org.mozilla.firefox"}, allComponentIDs()...),
			wantEnabled:     true,
			wantInstalled:   allComponentIDs(),
			wantMissingCore: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := Derive(systemScope(test.installed))
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
		{name: "off", installed: nil, wantHas: "Install ProtonPlus"},
		{name: "partial", installed: []string{protonPlus}, wantHas: "Partly installed"},
		{name: "complete", installed: allComponentIDs(), wantHas: "All 4 gaming components"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Derive(systemScope(test.installed)).Summary()
			if !strings.Contains(got, test.wantHas) {
				t.Errorf("Summary() = %q, want it to contain %q", got, test.wantHas)
			}
		})
	}
}

func stubInstalled(t *testing.T, ids []string, err error) {
	t.Helper()
	stubScope(t, systemScope(ids), err)
}

func stubScope(t *testing.T, scope Scope, err error) {
	t.Helper()

	previous := listInstalled
	listInstalled = func() (Scope, error) { return scope, err }
	t.Cleanup(func() { listInstalled = previous })
}

func TestStatusUsesTheFlatpakQuery(t *testing.T) {
	stubInstalled(t, allComponentIDs(), nil)

	state, err := Status()
	if err != nil {
		t.Fatalf("Status() error = %v, want nil", err)
	}
	if !state.Enabled {
		t.Errorf("Status().Enabled = false, want true with all core components installed")
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
	if removed, kept, failures := Disable(); removed != nil || kept != nil || len(failures) != 1 {
		t.Errorf("Disable() = (%v, %v, %v), want (nil, nil, one error)", removed, kept, failures)
	}
}

// Every present component counts toward gaming mode being on, whichever
// scope holds it, and each scope a component is installed in is reported
// separately — a component can be in both.
func TestDeriveSplitsUserAndSystemInstallations(t *testing.T) {
	scope := Scope{
		Installed: refsOf(protonPlus, protontrick, mangohud),
		User:      refsOf(protontrick, mangohud),
		System:    refsOf(protonPlus, mangohud),
	}

	state := Derive(scope)
	if state.Enabled {
		t.Error("Derive().Enabled = true, want false — goverlay is missing")
	}
	if !reflect.DeepEqual(state.Installed, []string{protonPlus, protontrick, mangohud}) {
		t.Errorf("Derive().Installed = %v, want every present component", state.Installed)
	}
	if !reflect.DeepEqual(state.UserInstalled, []string{protontrick, mangohud}) {
		t.Errorf("Derive().UserInstalled = %v, want the user-scope components", state.UserInstalled)
	}
	if !reflect.DeepEqual(state.SystemInstalled, []string{protonPlus, mangohud}) {
		t.Errorf("Derive().SystemInstalled = %v, want the system-scope components", state.SystemInstalled)
	}
}

// Retired components — Flatpaks a previous Gaming Mode installed that this
// build no longer manages — are reported by ID in the retired list's order,
// from either scope, without switching gaming mode on or off.
func TestDeriveReportsInstalledRetiredComponents(t *testing.T) {
	// The full current stack is installed, so gaming mode is on; the retired
	// copies are added on top, from different scopes.
	scope := systemScope(allComponentIDs())
	scope.Installed[refOf(steam)] = true
	scope.User[refOf(steam)] = true
	scope.Installed[refOf(flatseal)] = true
	scope.System[refOf(flatseal)] = true

	state := Derive(scope)
	if !state.Enabled {
		t.Error("Derive().Enabled = false, want true — retired components do not switch gaming mode off")
	}
	if !reflect.DeepEqual(state.Retired, []string{steam, flatseal}) {
		t.Errorf("Derive().Retired = %v, want %v in retired-list order from both scopes", state.Retired, []string{steam, flatseal})
	}
}

// A host that upgraded from a release with more components keeps those
// installs. They do not change the switch that picks the summary, so they are
// added on — an upgrade must not hide them behind an otherwise clean message.
func TestSummaryMentionsRetiredComponents(t *testing.T) {
	got := Derive(systemScope(append(allComponentIDs(), steam))).Summary()
	if !strings.Contains(got, "1 previously-installed app") {
		t.Errorf("Summary() = %q, want it to mention the still-present retired component", got)
	}
}

// Turning Gaming Mode off removes a retired component's user copy just as it
// removes a current component's, because the user copy needs no authorization.
func TestDisableRemovesARetiredUserScopeCopy(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam),
		User:      refsOf(steam),
	}, nil)
	log := fakeFlatpak(t, "exit 0")

	removed, kept, failures := Disable()
	if len(failures) != 0 {
		t.Fatalf("Disable() failures = %v, want none", failures)
	}
	if !reflect.DeepEqual(removed, []string{steam}) {
		t.Errorf("Disable() removed = %v, want only the retired user-scope copy", removed)
	}
	if len(kept) != 0 {
		t.Errorf("Disable() kept = %v, want none", kept)
	}
	want := []string{"uninstall -y --user " + steam}
	if calls := invocations(t, log); !reflect.DeepEqual(calls, want) {
		t.Errorf("Disable() ran %q, want %q", calls, want)
	}
}

// A system copy of a retired component the image ships is left in place,
// exactly like a current component's: undoing gaming mode never takes a
// distro default away from every account.
func TestDisableKeepsAnImageShippedRetiredSystemCopy(t *testing.T) {
	declareShipped(t, map[int]map[string]string{
		0: {"flatseal.preinstall": "[Flatpak Preinstall " + flatseal + "]\n"},
	}, "")
	stubScope(t, Scope{
		Installed: refsOf(flatseal),
		System:    refsOf(flatseal),
	}, nil)
	log := fakeFlatpak(t, "exit 0")

	removed, kept, failures := Disable()
	if len(failures) != 0 {
		t.Fatalf("Disable() failures = %v, want none", failures)
	}
	if !reflect.DeepEqual(kept, []string{flatseal}) {
		t.Errorf("Disable() kept = %v, want the image-shipped retired system copy", kept)
	}
	if removed != nil {
		t.Errorf("Disable() removed = %v, want none", removed)
	}
	if calls := invocations(t, log); len(calls) != 0 {
		t.Errorf("Disable() ran %q, want nothing", calls)
	}
}

// A system copy of a retired component the image does not ship is removed
// under Flatpak's PolicyKit, and the ship declaration is read even though no
// current component is installed system-wide.
func TestDisableRemovesANonShippedRetiredSystemCopy(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam),
		System:    refsOf(steam),
	}, nil)
	log := fakeFlatpak(t, "exit 0")

	removed, kept, failures := Disable()
	if len(failures) != 0 {
		t.Fatalf("Disable() failures = %v, want none", failures)
	}
	if !reflect.DeepEqual(removed, []string{steam}) {
		t.Errorf("Disable() removed = %v, want the non-shipped retired system copy", removed)
	}
	if len(kept) != 0 {
		t.Errorf("Disable() kept = %v, want none", kept)
	}
	want := []string{"uninstall -y --system " + steam}
	if calls := invocations(t, log); !reflect.DeepEqual(calls, want) {
		t.Errorf("Disable() ran %q, want %q", calls, want)
	}
}
