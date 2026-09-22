package navigation

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// completeEnv is the environment a fully constructed, fully enabled window
// would present: every canonical route has a widget, and every configuration
// group is on.
func completeEnv(visible []Route) Env {
	return Env{
		Visible:   visible,
		Available: func(string) bool { return true },
		Enabled:   func(string, string) bool { return true },
	}
}

// refPage returns the single configuration page a primary route's refs come
// from. Primaries map to one page builder; details may span pages.
func refPage(route Route) string {
	if len(route.Refs) == 0 {
		return ""
	}
	return route.Refs[0].Page
}

// refGroups returns the groups a route consumes from one configuration page,
// in declaration order.
func refGroups(route Route, page string) []string {
	var groups []string
	for _, ref := range route.Refs {
		if ref.Page == page {
			groups = append(groups, ref.Group)
		}
	}
	return groups
}

func TestResolveCoversEveryNavigationMutation(t *testing.T) {
	items := PrimaryRoutes()
	for index, item := range items {
		t.Run(item.Name, func(t *testing.T) {
			got, ok := Resolve(item.Name, Env{
				Visible:   items,
				Available: func(name string) bool { return name == item.Name },
				Enabled:   func(string, string) bool { return true },
			})
			if !ok {
				t.Fatalf("Resolve(%q) rejected an available canonical route", item.Name)
			}
			want := Transition{
				Name:          item.Name,
				SelectedIndex: index,
				VisibleChild:  item.Name,
				Title:         item.Title,
				ShowContent:   true,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Resolve(%q) = %#v, want %#v", item.Name, got, want)
			}
		})
	}
}

func TestResolveRejectsUnavailableAndUnknownRoutes(t *testing.T) {
	tests := []struct {
		name      string
		route     string
		available func(string) bool
	}{
		{name: "unavailable fallback route", route: "help", available: func(string) bool { return false }},
		{name: "unknown", route: "not-a-route", available: func(string) bool { return true }},
		{name: "nil availability predicate", route: "help"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if transition, ok := Resolve(tt.route, Env{
				Visible:   PrimaryRoutes(),
				Available: tt.available,
				Enabled:   func(string, string) bool { return true },
			}); ok {
				t.Fatalf("Resolve(%q) = %#v, true; want rejection", tt.route, transition)
			}
		})
	}
}

func TestResolveDetailSelectsItsVisibleAncestor(t *testing.T) {
	visible := PrimaryRoutes()
	env := completeEnv(visible)
	env.Available = func(name string) bool { return name == "recovery" || name == "system" }

	got, ok := Resolve("recovery", env)
	if !ok {
		t.Fatal("Resolve(recovery) rejected a constructed detail with a visible ancestor")
	}
	want := Transition{
		Name:          "recovery",
		SelectedIndex: indexOf(t, visible, "system"),
		VisibleChild:  "recovery",
		Title:         "Recovery",
		ShowContent:   true,
		Back:          "system",
		Detail:        true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(recovery) = %#v, want %#v", got, want)
	}
}

// A detail whose widget the window did not build must land on its ancestor
// rather than opening an empty screen.
func TestResolveUnconstructedDetailFallsBackToItsAncestor(t *testing.T) {
	visible := PrimaryRoutes()
	env := completeEnv(visible)
	env.Available = func(name string) bool { return name == "system" }

	got, ok := Resolve("storage", env)
	if !ok {
		t.Fatal("Resolve(storage) rejected a declared detail with a visible ancestor")
	}
	if got.Name != "system" || got.Detail || got.Back != "" {
		t.Fatalf("Resolve(storage) = %#v, want the System primary transition", got)
	}
	if got.SelectedIndex != indexOf(t, visible, "system") {
		t.Fatalf("Resolve(storage).SelectedIndex = %d, want the System row", got.SelectedIndex)
	}
}

// A detail whose every configuration group is disabled has no content to
// show, so it falls back instead of rendering an empty page.
func TestResolveDisabledDetailFallsBackToItsAncestor(t *testing.T) {
	enabled := func(page, group string) bool { return page != "maintenance_page" }
	visible := VisibleRoutes(enabled)
	if containsPage(visible, "maintenance") {
		t.Fatal("Maintenance should be omitted when none of its groups are enabled")
	}
	env := Env{
		Visible:   visible,
		Available: func(string) bool { return true },
		Enabled:   enabled,
	}

	got, ok := Resolve("storage", env)
	if !ok {
		t.Fatal("Resolve(storage) rejected a detail whose groups were all disabled")
	}
	if got.Name != "system" || got.Detail {
		t.Fatalf("Resolve(storage) = %#v, want the System primary transition", got)
	}
}

// Back returns to the mount that draws the detail's content today, which is
// the one whose configuration namespaces the detail shares. The old
// Maintenance mount still draws Storage and Recovery; System draws About.
func TestResolveAncestorIsTheMountThatDrawsTheContent(t *testing.T) {
	visible := PrimaryRoutes()
	env := completeEnv(visible)

	tests := map[string]string{
		"storage":                   "maintenance",
		"recovery":                  "maintenance",
		"administrator-maintenance": "maintenance",
		"about":                     "system",
		"graphics":                  "system",
		"developer-tools":           "applications",
		"trust":                     "updates",
		"update-settings":           "updates",
		"gaming":                    "features",
		"troubleshooting":           "features",
		"icons":                     "livery",
		"capability-explanations":   "help",
	}
	for detail, ancestor := range tests {
		t.Run(detail, func(t *testing.T) {
			got, ok := Resolve(detail, env)
			if !ok {
				t.Fatalf("Resolve(%q) rejected a constructed detail", detail)
			}
			if got.Name != detail || got.Back != ancestor || !got.Detail {
				t.Fatalf("Resolve(%q) = %#v, want the %q ancestor", detail, got, ancestor)
			}
			if got.SelectedIndex != indexOf(t, visible, ancestor) {
				t.Errorf("Resolve(%q).SelectedIndex = %d, want the %q row", detail, got.SelectedIndex, ancestor)
			}
		})
	}
}

// A detail whose destination has no visible mount at all falls back to Help,
// which is always retained, rather than opening an unreachable screen.
func TestResolveHiddenAncestorFallsBackToHelp(t *testing.T) {
	visible := VisibleRoutes(func(page, group string) bool { return page == "maintenance_page" })
	if containsPage(visible, "livery") {
		t.Fatal("Livery should be omitted when none of its own groups are enabled")
	}
	env := completeEnv(visible)

	got, ok := Resolve("icons", env)
	if !ok {
		t.Fatal("Resolve(icons) rejected a detail whose destination has no visible mount")
	}
	if got.Name != "help" || got.Detail {
		t.Fatalf("Resolve(icons) = %#v, want the Help fallback transition", got)
	}
	if !containsPage(visible, "help") {
		t.Fatal("the fallback transition landed on a route that is not visible")
	}
}

// A detail route is reached from its primary, never from its own numeric
// slot (#233, #201 step 3).
func TestDetailRoutesCarryNoAccelerator(t *testing.T) {
	for _, route := range Routes() {
		if route.Kind != Detail {
			continue
		}
		t.Run(route.Name, func(t *testing.T) {
			if route.Accelerator != "" || route.Display != "" {
				t.Errorf("detail route %q advertises %q/%q", route.Name, route.Accelerator, route.Display)
			}
		})
	}

	advertised := make(map[string]bool)
	for _, shortcut := range Shortcuts(PrimaryRoutes()) {
		advertised[shortcut.Action] = true
	}
	for _, route := range Routes() {
		if route.Kind == Detail && advertised["win.navigate-"+route.Name] {
			t.Errorf("detail route %q is advertised as a navigation shortcut", route.Name)
		}
	}
}

// A cross-page detail resolves through the destination it serves, not through
// its display name: update-settings consumes both updates_page and
// system_page, and either destination's primary is a valid ancestor.
func TestResolveCrossNamespaceDetailUsesItsDestination(t *testing.T) {
	visible := PrimaryRoutes()
	env := completeEnv(visible)
	env.Available = func(string) bool { return true }

	got, ok := Resolve("update-settings", env)
	if !ok {
		t.Fatal("Resolve(update-settings) rejected a constructed cross-page detail")
	}
	if got.Name != "update-settings" || got.Back != "updates" {
		t.Fatalf("Resolve(update-settings) = %#v, want the Updates-owned detail", got)
	}

	pages := make(map[string]bool)
	for _, ref := range mustRoute(t, "update-settings").Refs {
		pages[ref.Page] = true
	}
	if !pages["updates_page"] || !pages["system_page"] {
		t.Fatalf("update-settings refs span %v, want both updates_page and system_page", pages)
	}
}

func TestShortcutsRegisterEveryAdvertisedAccelerator(t *testing.T) {
	items := PrimaryRoutes()
	advertised := make(map[string]Shortcut)
	for _, shortcut := range Shortcuts(items) {
		key := shortcut.Action + "\x00" + shortcut.Accelerator
		if _, exists := advertised[key]; exists {
			t.Fatalf("duplicate shortcut for action %q accelerator %q", shortcut.Action, shortcut.Accelerator)
		}
		advertised[key] = shortcut
	}

	registered := make(map[string]bool)
	for _, binding := range Bindings(items) {
		for _, accelerator := range binding.Accelerators {
			registered[binding.Action+"\x00"+accelerator] = true
		}
	}
	if len(registered) != len(advertised) {
		t.Fatalf("registered shortcut count = %d, advertised count = %d", len(registered), len(advertised))
	}
	for key, shortcut := range advertised {
		if !registered[key] {
			t.Errorf("advertised shortcut %s (%s) is not registered", shortcut.Display, shortcut.Title)
		}
	}
	for _, item := range items {
		key := "win.navigate-" + item.Name + "\x00" + item.Accelerator
		shortcut, ok := advertised[key]
		if !ok {
			t.Errorf("route %q has no advertised navigation shortcut", item.Name)
			continue
		}
		if shortcut.Display != item.Display ||
			shortcut.Title != "Go to "+item.Title ||
			shortcut.Group != GroupNavigation {
			t.Errorf("route %q shortcut = %#v, want canonical route metadata", item.Name, shortcut)
		}
	}

	help, ok := advertised["win.navigate-help\x00F1"]
	if !ok {
		t.Fatal("F1 is not mapped to the Help navigation action")
	}
	if help.Display != "F1" || help.Title != "Help" || help.Group != GroupGeneral {
		t.Fatalf("F1 shortcut = %#v, want advertised General/Help entry", help)
	}
}

func TestReturnedInventoriesCannotMutateCanonicalState(t *testing.T) {
	gotItems := PrimaryRoutes()
	gotItems[0].Name = "changed"
	gotItems[0].Refs[0].Group = "changed"
	gotItems[0].Serves[0] = "changed"
	if PrimaryRoutes()[0].Name == "changed" {
		t.Fatal("PrimaryRoutes returned mutable canonical storage")
	}
	if PrimaryRoutes()[0].Refs[0].Group == "changed" {
		t.Fatal("PrimaryRoutes returned mutable canonical ref storage")
	}
	if PrimaryRoutes()[0].Serves[0] == "changed" {
		t.Fatal("PrimaryRoutes returned mutable canonical destination storage")
	}

	gotRoutes := Routes()
	gotRoutes[0].Name = "changed"
	gotRoutes[0].Refs[0].Page = "changed"
	if Routes()[0].Name == "changed" || Routes()[0].Refs[0].Page == "changed" {
		t.Fatal("Routes returned mutable canonical storage")
	}

	gotDestinations := Destinations()
	gotDestinations[0].Name = "changed"
	if Destinations()[0].Name == "changed" {
		t.Fatal("Destinations returned mutable canonical storage")
	}

	gotShortcuts := Shortcuts(PrimaryRoutes())
	gotShortcuts[0].Action = "changed"
	if Shortcuts(PrimaryRoutes())[0].Action == "changed" {
		t.Fatal("Shortcuts returned mutable canonical storage")
	}

	gotBindings := Bindings(PrimaryRoutes())
	gotBindings[0].Accelerators[0] = "changed"
	if Bindings(PrimaryRoutes())[0].Accelerators[0] == "changed" {
		t.Fatal("Bindings returned mutable canonical storage")
	}
}

func TestVisiblePagesOmitEveryFullyDisabledFunctionalPage(t *testing.T) {
	for _, disabled := range PrimaryRoutes() {
		if disabled.AlwaysShow {
			continue
		}
		t.Run(disabled.Name, func(t *testing.T) {
			got := VisibleRoutes(func(page, group string) bool {
				return page != refPage(disabled)
			})
			if containsPage(got, disabled.Name) {
				t.Fatalf("VisibleRoutes retained %q when all of its groups were disabled", disabled.Name)
			}
			if !containsPage(got, "help") {
				t.Fatal("VisibleRoutes omitted Help")
			}
		})
	}
}

func TestPageMetadataCoversEveryBuilderBackedGroup(t *testing.T) {
	want := map[string][]string{
		"applications": {
			"applications_installed_group",
			"flatpak_user_group",
			"flatpak_system_group",
			"brew_group",
			"brew_search_group",
			"brew_bundles_group",
		},
		"maintenance": {
			"maintenance_cleanup_group",
			"maintenance_brew_group",
			"maintenance_flatpak_group",
			"maintenance_optimization_group",
			"reset_group",
		},
		"updates": {
			"update_all_group",
			"bootc_updates_group",
			"sysupdate_updates_group",
			"flatpak_updates_group",
			"brew_updates_group",
			"brew_trust_group",
		},
		"system": {
			"system_info_group",
			"bootc_status_group",
			"channel_group",
			"health_group",
		},
		"features": {
			"features_group",
			"dx_group",
			"gaming_group",
			"ai_group",
			"troubleshooting_group",
		},
		"livery": {
			"livery_app_grid_group",
			"livery_foundation_group",
			"livery_dock_group",
		},
		"help": {"help_resources_group"},
	}

	items := PrimaryRoutes()
	if len(items) != len(want) {
		t.Fatalf("PrimaryRoutes() has %d routes, want %d", len(items), len(want))
	}
	for _, item := range items {
		wantGroups, ok := want[item.Name]
		if !ok {
			t.Errorf("unexpected route metadata %q", item.Name)
			continue
		}
		if got := refGroups(item, refPage(item)); !reflect.DeepEqual(got, wantGroups) {
			t.Errorf("%s groups = %v, want %v", item.Name, got, wantGroups)
		}
	}
}

// A primary route is drawn by exactly one page builder, so its refs name
// exactly one configuration page. Detail routes are the many-to-many case and
// are deliberately not held to this.
func TestPrimaryRoutesConsumeExactlyOneConfigPage(t *testing.T) {
	for _, route := range PrimaryRoutes() {
		t.Run(route.Name, func(t *testing.T) {
			pages := make(map[string]bool)
			for _, ref := range route.Refs {
				pages[ref.Page] = true
			}
			if len(pages) != 1 {
				t.Fatalf("primary route %q consumes %d config pages, want 1", route.Name, len(pages))
			}
		})
	}
}

func TestPageMetadataMatchesViewBuilders(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate navigation_test.go")
	}
	viewsDir := filepath.Join(filepath.Dir(filename), "..", "views")
	groupCall := regexp.MustCompile(
		`IsGroupEnabled\("([^"]+)",\s*"([^"]+)"\)`,
	)

	for _, item := range PrimaryRoutes() {
		t.Run(item.Name, func(t *testing.T) {
			path := filepath.Join(viewsDir, item.Name+"_page.go")
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}

			configPage := refPage(item)
			found := make(map[string]bool)
			for _, match := range groupCall.FindAllStringSubmatch(string(source), -1) {
				if match[1] != configPage {
					t.Errorf("builder uses config page %q, want %q", match[1], configPage)
				}
				found[match[2]] = true
			}
			wantGroups := refGroups(item, configPage)
			if len(found) != len(wantGroups) {
				t.Fatalf("builder groups = %v, navigation metadata = %v", found, wantGroups)
			}
			for _, group := range wantGroups {
				if !found[group] {
					t.Errorf("route group %q has no builder guard in %s", group, path)
				}
			}
		})
	}
}

func TestVisiblePagesKeepEachBuilderBackedGroup(t *testing.T) {
	for _, page := range PrimaryRoutes() {
		if page.AlwaysShow {
			continue
		}
		for _, enabledGroup := range page.Refs {
			t.Run(page.Name+"/"+enabledGroup.Group, func(t *testing.T) {
				got := VisibleRoutes(func(configPage, group string) bool {
					return configPage == enabledGroup.Page && group == enabledGroup.Group
				})
				if !containsPage(got, page.Name) {
					t.Fatalf("VisibleRoutes omitted %q when %q was enabled", page.Name, enabledGroup.Group)
				}
			})
		}
	}
}

func TestVisiblePagesAlwaysKeepHelp(t *testing.T) {
	got := VisibleRoutes(func(page, group string) bool { return false })
	if len(got) != 1 || got[0].Name != "help" {
		t.Fatalf("VisibleRoutes(all disabled) = %#v, want Help only", got)
	}
	if got[0].Accelerator != "<Alt>1" || got[0].Display != "Alt+1" {
		t.Fatalf("Help shortcut = %q/%q, want compacted Alt+1", got[0].Accelerator, got[0].Display)
	}
}

func TestVisiblePagesCompactShortcutsAndTransitions(t *testing.T) {
	visible := VisibleRoutes(func(page, group string) bool {
		return page == "updates_page" || page == "features_page"
	})
	wantNames := []string{"updates", "features", "help"}
	if len(visible) != len(wantNames) {
		t.Fatalf("VisibleRoutes selected %d routes, want %d: %#v", len(visible), len(wantNames), visible)
	}
	for index, wantName := range wantNames {
		if visible[index].Name != wantName {
			t.Fatalf("visible[%d].Name = %q, want %q", index, visible[index].Name, wantName)
		}
		wantAccelerator := "<Alt>" + strconv.Itoa(index+1)
		if visible[index].Accelerator != wantAccelerator {
			t.Errorf("visible[%d].Accelerator = %q, want %q", index, visible[index].Accelerator, wantAccelerator)
		}
		transition, ok := Resolve(wantName, completeEnv(visible))
		if !ok {
			t.Fatalf("Resolve(%q) rejected a visible route", wantName)
		}
		if transition.SelectedIndex != index {
			t.Errorf("Resolve(%q).SelectedIndex = %d, want %d", wantName, transition.SelectedIndex, index)
		}
	}

	shortcuts := Shortcuts(visible)
	for index, item := range visible {
		shortcut := shortcuts[index]
		if shortcut.Action != "win.navigate-"+item.Name ||
			shortcut.Accelerator != item.Accelerator ||
			shortcut.Display != item.Display {
			t.Errorf("shortcut[%d] = %#v, want compacted metadata for %#v", index, shortcut, item)
		}
	}

	// An omitted primary is known but not visible, so it lands on the
	// fallback route rather than nowhere: a stale deep link must not open a
	// page the window never built.
	got, ok := Resolve("maintenance", completeEnv(visible))
	if !ok {
		t.Fatal("Resolve(maintenance) rejected a known but omitted primary")
	}
	if got.Name != "help" || got.Detail {
		t.Fatalf("Resolve(maintenance) = %#v, want the Help fallback transition", got)
	}

	altBindings := make(map[string]string)
	for _, binding := range Bindings(visible) {
		if strings.HasPrefix(binding.Action, "win.navigate-") {
			for _, accelerator := range binding.Accelerators {
				if strings.HasPrefix(accelerator, "<Alt>") {
					altBindings[accelerator] = binding.Action
				}
			}
		}
	}
	if len(altBindings) != len(visible) {
		t.Fatalf("Alt binding count = %d, want %d: %v", len(altBindings), len(visible), altBindings)
	}
	for _, item := range visible {
		if got := altBindings[item.Accelerator]; got != "win.navigate-"+item.Name {
			t.Errorf("%s is bound to %q, want navigate action for %q", item.Accelerator, got, item.Name)
		}
	}
}

func TestWindowAndAppUseCanonicalNavigation(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate navigation_test.go")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))

	checks := map[string][]string{
		filepath.Join(repoRoot, "internal", "window", "window.go"): {
			`w.navigateToPage(name)`,
			`w.navItems = navigation.VisibleRoutes(w.config.IsGroupEnabled)`,
			`transition, ok := navigation.Resolve(routeName, navigation.Env{`,
			`Enabled: w.config.IsGroupEnabled,`,
			`w.sidebarList.GetRowAtIndex(int32(transition.SelectedIndex))`,
			`w.contentStack.SetVisibleChildName(transition.VisibleChild)`,
			`w.contentPage.SetTitle(transition.Title)`,
			`w.splitView.SetShowContent(transition.ShowContent)`,
			`w.backTarget = transition.Back`,
			`action := gio.NewSimpleAction("navigate-"+itemName, nil)`,
			`for _, shortcut := range navigation.Shortcuts(w.navItems)`,
		},
		filepath.Join(repoRoot, "internal", "app", "app.go"): {
			`a.setupKeyboardShortcuts(win.NavigationItems())`,
			`for _, binding := range navigation.Bindings(items)`,
			`a.SetAccelsForAction(binding.Action, binding.Accelerators)`,
		},
	}

	for path, required := range checks {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, fragment := range required {
			if !strings.Contains(string(source), fragment) {
				t.Errorf("%s does not contain canonical navigation wiring %q", path, fragment)
			}
		}
	}
}

func containsPage(items []Route, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func indexOf(t *testing.T, items []Route, name string) int {
	t.Helper()
	for index, item := range items {
		if item.Name == name {
			return index
		}
	}
	t.Fatalf("route %q not found in the visible inventory", name)
	return -1
}

func mustRoute(t *testing.T, name string) Route {
	t.Helper()
	route, ok := lookup(name)
	if !ok {
		t.Fatalf("route %q is not in the canonical inventory", name)
	}
	return route
}
