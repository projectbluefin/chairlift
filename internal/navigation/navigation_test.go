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

	"github.com/projectbluefin/chairlift/internal/capability"
)

func TestResolveCoversEveryNavigationMutation(t *testing.T) {
	items := Items()
	for index, item := range items {
		t.Run(item.Name, func(t *testing.T) {
			got, ok := Resolve(item.Name, items, func(name string) bool {
				return name == item.Name
			})
			if !ok {
				t.Fatalf("Resolve(%q) rejected an available canonical page", item.Name)
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

func TestResolveRejectsUnavailableAndUnknownPages(t *testing.T) {
	tests := []struct {
		name      string
		page      string
		available func(string) bool
	}{
		{name: "unavailable", page: "help", available: func(string) bool { return false }},
		{name: "unknown", page: "not-a-page", available: func(string) bool { return true }},
		{name: "nil availability predicate", page: "help"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if transition, ok := Resolve(tt.page, Items(), tt.available); ok {
				t.Fatalf("Resolve(%q) = %#v, true; want rejection", tt.page, transition)
			}
		})
	}
}

func TestShortcutsRegisterEveryAdvertisedAccelerator(t *testing.T) {
	items := Items()
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
			t.Errorf("page %q has no advertised navigation shortcut", item.Name)
			continue
		}
		if shortcut.Display != item.Display ||
			shortcut.Title != "Go to "+item.Title ||
			shortcut.Group != GroupNavigation {
			t.Errorf("page %q shortcut = %#v, want canonical item metadata", item.Name, shortcut)
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
	gotItems := Items()
	gotItems[0].Name = "changed"
	gotItems[0].Refs[0] = Ref{Page: "changed", Group: "changed"}
	if Items()[0].Name == "changed" {
		t.Fatal("Items returned mutable canonical storage")
	}
	if Items()[0].Refs[0] == (Ref{Page: "changed", Group: "changed"}) {
		t.Fatal("Items returned mutable canonical ref storage")
	}

	gotDetails := Details()
	if len(gotDetails) == 0 {
		t.Fatal("Details() is empty; the copy check would be vacuous")
	}
	gotDetails[0].Title = "changed"
	gotDetails[0].Refs[0] = Ref{Page: "changed", Group: "changed"}
	if Details()[0].Title == "changed" || Details()[0].Refs[0].Page == "changed" {
		t.Fatal("Details returned mutable canonical storage")
	}

	gotRoutes := VisibleRoutes(func(string, string) bool { return true })
	if len(gotRoutes) == 0 {
		t.Fatal("VisibleRoutes() is empty; the copy check would be vacuous")
	}
	gotRoutes[len(gotRoutes)-1].Name = "changed"
	if last := VisibleRoutes(func(string, string) bool { return true }); last[len(last)-1].Name == "changed" {
		t.Fatal("VisibleRoutes returned mutable canonical storage")
	}

	gotShortcuts := Shortcuts(Items())
	gotShortcuts[0].Action = "changed"
	if Shortcuts(Items())[0].Action == "changed" {
		t.Fatal("Shortcuts returned mutable canonical storage")
	}

	gotBindings := Bindings(Items())
	gotBindings[0].Accelerators[0] = "changed"
	if Bindings(Items())[0].Accelerators[0] == "changed" {
		t.Fatal("Bindings returned mutable canonical storage")
	}
}

func TestVisiblePagesOmitEveryFullyDisabledFunctionalPage(t *testing.T) {
	for _, disabled := range Items() {
		if disabled.AlwaysShow {
			continue
		}
		t.Run(disabled.Name, func(t *testing.T) {
			got := VisibleItems(func(page, group string) bool {
				return !declares(disabled, Ref{Page: page, Group: group})
			})
			if containsPage(got, disabled.Name) {
				t.Fatalf("VisibleItems retained %q when all of its groups were disabled", disabled.Name)
			}
			if !containsPage(got, "help") {
				t.Fatal("VisibleItems omitted Help")
			}
		})
	}
}

// TestPageMetadataCoversEveryBuilderBackedGroup pins the canonical refs: the
// configuration namespace each route consumes, page-qualified. It is
// deliberately hand-written — the forcing function that keeps it complete is
// the config schema comparison in internal/installcheck — and it is the place
// a cross-namespace route is spelled out, because that shape is what a single
// page field per route could not express.
func TestPageMetadataCoversEveryBuilderBackedGroup(t *testing.T) {
	want := map[string][]Ref{
		"updates": refsOn("updates_page",
			"automatic_updates_group",
			"bootc_updates_group",
			"flatpak_updates_group",
			"brew_updates_group",
			"brew_trust_group",
			"channel_group",
			"bootc_status_group",
		),
		"applications": refsOn("applications_page",
			"applications_installed_group",
			"flatpak_user_group",
			"flatpak_system_group",
			"brew_group",
			"brew_search_group",
			"brew_bundles_group",
		),
		"agents":   refsOn("agents_page", "agents_group"),
		"features": refsOn("features_page", "features_group", "dx_group", "gaming_group"),
		"livery": refsOn("livery_page",
			"account_group",
			"livery_app_grid_group",
			"livery_foundation_group",
			"livery_dock_group",
		),
		"maintenance": refsOn("maintenance_page",
			"maintenance_cleanup_group",
			"maintenance_freespace_group",
			"reset_group",
		),
		"help": refsOn("help_page", "troubleshooting_group", "help_resources_group"),
	}

	items := Items()
	if len(items) != len(want) {
		t.Fatalf("Items() has %d pages, want %d", len(items), len(want))
	}
	for _, item := range items {
		wantRefs, ok := want[item.Name]
		if !ok {
			t.Errorf("unexpected page metadata %q", item.Name)
			continue
		}
		if !reflect.DeepEqual(item.Refs, wantRefs) {
			t.Errorf("%s refs = %v, want %v", item.Name, item.Refs, wantRefs)
		}
	}
}

// TestDetailMetadataCoversEveryBuilderBackedGroup does the same for the detail
// routes, pinning each one's ancestor and the namespaces that build its
// controls.
func TestDetailMetadataCoversEveryBuilderBackedGroup(t *testing.T) {
	// Recovery draws its rollback controls from updates_page and its reset
	// controls from maintenance_page, so its refs are the one cross-namespace
	// case in the inventory today.
	want := map[string]struct {
		parent string
		refs   []Ref
	}{
		"recovery": {
			parent: "maintenance",
			refs: []Ref{
				{Page: "maintenance_page", Group: "reset_group"},
				{Page: "updates_page", Group: "bootc_updates_group"},
			},
		},
	}

	details := Details()
	if len(details) != len(want) {
		t.Fatalf("Details() has %d routes, want %d", len(details), len(want))
	}
	for _, detail := range details {
		wantDetail, ok := want[detail.Name]
		if !ok {
			t.Errorf("unexpected detail metadata %q", detail.Name)
			continue
		}
		if detail.Parent != wantDetail.parent {
			t.Errorf("%s parent = %q, want %q", detail.Name, detail.Parent, wantDetail.parent)
		}
		if !reflect.DeepEqual(detail.Refs, wantDetail.refs) {
			t.Errorf("%s refs = %v, want %v", detail.Name, detail.Refs, wantDetail.refs)
		}
	}
}

func TestPageMetadataMatchesViewBuilders(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate navigation_test.go")
	}
	viewsDir := filepath.Join(filepath.Dir(filename), "..", "views")
	groupCall := regexp.MustCompile(
		`uh.groupEnabled\("([^"]+)",\s*"([^"]+)"\)`,
	)

	for _, item := range Items() {
		t.Run(item.Name, func(t *testing.T) {
			path := filepath.Join(viewsDir, item.Name+"_page.go")
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}

			found := make(map[Ref]bool)
			for _, match := range groupCall.FindAllStringSubmatch(string(source), -1) {
				found[Ref{Page: match[1], Group: match[2]}] = true
			}
			if len(found) != len(item.Refs) {
				t.Fatalf("builder guards = %v, navigation metadata = %v", found, item.Refs)
			}
			for _, ref := range item.Refs {
				if !found[ref] {
					t.Errorf("navigation ref %v has no builder guard in %s", ref, path)
				}
			}
		})
	}
}

// TestDetailRouteRefsAreGuardedByViewBuilders holds a detail's declared
// namespaces to the view code that actually gates its controls. A detail may
// be built across files — Recovery's rollback group is guarded in
// recovery.go while its reset controls are added from maintenance_page.go —
// so this checks the whole views tree rather than one file per route, and
// fails when a detail claims a namespace nothing consults.
func TestDetailRouteRefsAreGuardedByViewBuilders(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate navigation_test.go")
	}
	viewsDir := filepath.Join(filepath.Dir(filename), "..", "views")
	groupCall := regexp.MustCompile(
		`uh.groupEnabled\("([^"]+)",\s*"([^"]+)"\)`,
	)

	guarded := make(map[Ref]bool)
	err := filepath.WalkDir(viewsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range groupCall.FindAllStringSubmatch(string(source), -1) {
			guarded[Ref{Page: match[1], Group: match[2]}] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", viewsDir, err)
	}
	if len(guarded) == 0 {
		t.Fatal("no view builder guards a configuration group; this gate would be vacuous")
	}

	for _, detail := range Details() {
		t.Run(detail.Name, func(t *testing.T) {
			for _, ref := range detail.Refs {
				if !guarded[ref] {
					t.Errorf("detail %q names %v but no view builder gates on it", detail.Name, ref)
				}
			}
		})
	}
}

func TestVisiblePagesKeepEachBuilderBackedGroup(t *testing.T) {
	for _, page := range Items() {
		if page.AlwaysShow {
			continue
		}
		for _, builderRef := range page.Refs {
			t.Run(page.Name+"/"+builderRef.Group, func(t *testing.T) {
				got := VisibleItems(func(configPage, group string) bool {
					return configPage == builderRef.Page && group == builderRef.Group
				})
				if !containsPage(got, page.Name) {
					t.Fatalf("VisibleItems omitted %q when %q was enabled", page.Name, builderRef.Group)
				}
			})
		}
	}
}

func TestVisiblePagesAlwaysKeepHelp(t *testing.T) {
	got := VisibleItems(func(page, group string) bool { return false })
	if len(got) != 1 || got[0].Name != "help" {
		t.Fatalf("VisibleItems(all disabled) = %#v, want Help only", got)
	}
	if got[0].Accelerator != "<Alt>1" || got[0].Display != "Alt+1" {
		t.Fatalf("Help shortcut = %q/%q, want compacted Alt+1", got[0].Accelerator, got[0].Display)
	}
}

// The capability floor reaches the sidebar. VisibleItems is driven by the one
// composed predicate the window builds from config plus the host shape, so a
// page whose only group's backing tool is absent is hidden even when the
// administrator's configuration enables it. Without the floor a Homebrew-only
// page would leak onto a host that cannot run it. See chairlift#205.
func TestVisibleItemsAppliesTheCapabilityFloor(t *testing.T) {
	alwaysEnabled := func(page, group string) bool { return true }

	t.Run("agents hidden when Homebrew absent", func(t *testing.T) {
		predicate := capability.Compose(alwaysEnabled, capability.Set{capability.Flatpak: true})
		if got := VisibleItems(predicate); containsPage(got, "agents") {
			t.Errorf("VisibleItems showed agents_page on a host without Homebrew: %#v", got)
		}
	})

	t.Run("agents shown when Homebrew present", func(t *testing.T) {
		predicate := capability.Compose(
			alwaysEnabled,
			capability.Set{capability.Flatpak: true, capability.Homebrew: true},
		)
		if got := VisibleItems(predicate); !containsPage(got, "agents") {
			t.Errorf("VisibleItems omitted agents_page with Homebrew present: %#v", got)
		}
	})

	t.Run("help stays visible regardless of capability", func(t *testing.T) {
		got := VisibleItems(capability.Compose(alwaysEnabled, capability.Set{}))
		if !containsPage(got, "help") {
			t.Errorf("VisibleItems omitted help on an empty host: %#v", got)
		}
	})
}

func TestVisiblePagesCompactShortcutsAndTransitions(t *testing.T) {
	visible := VisibleItems(func(page, group string) bool {
		return page == "updates_page" || page == "features_page"
	})
	wantNames := []string{"updates", "features", "help"}
	if len(visible) != len(wantNames) {
		t.Fatalf("VisibleItems selected %d pages, want %d: %#v", len(visible), len(wantNames), visible)
	}
	for index, wantName := range wantNames {
		if visible[index].Name != wantName {
			t.Fatalf("visible[%d].Name = %q, want %q", index, visible[index].Name, wantName)
		}
		wantAccelerator := "<Alt>" + strconv.Itoa(index+1)
		if visible[index].Accelerator != wantAccelerator {
			t.Errorf("visible[%d].Accelerator = %q, want %q", index, visible[index].Accelerator, wantAccelerator)
		}
		transition, ok := Resolve(wantName, visible, func(string) bool { return true })
		if !ok {
			t.Fatalf("Resolve(%q) rejected a visible page", wantName)
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

	if transition, ok := Resolve("maintenance", visible, func(string) bool { return true }); ok {
		t.Fatalf("Resolve accepted omitted Maintenance page: %#v", transition)
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
			`w.capabilities = capability.Detect()`,
			`w.navItems = navigation.VisibleItems(w.effectiveEnabled)`,
			`func (w *Window) effectiveEnabled(page, group string) bool {`,
			`transition, ok := navigation.Resolve(pageName, w.navItems, func(name string) bool {`,
			`w.sidebarList.GetRowAtIndex(int32(transition.SelectedIndex))`,
			`w.contentStack.SetVisibleChildName(transition.VisibleChild)`,
			`w.contentPage.SetTitle(transition.Title)`,
			`w.splitView.SetShowContent(transition.ShowContent)`,
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

func containsPage(items []Item, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

// declares reports whether a route consumes one configuration namespace.
func declares(item Item, ref Ref) bool {
	for _, candidate := range item.Refs {
		if candidate == ref {
			return true
		}
	}
	return false
}

func routeByName(t *testing.T, name string) Item {
	t.Helper()
	route, ok := lookup(name)
	if !ok {
		t.Fatalf("canonical inventory declares no route %q", name)
	}
	return route
}

// ancestorPages returns the configuration pages a route's primary ancestor
// consumes: the namespaces whose groups keep that ancestor's sidebar row, and
// therefore the ones a detail shares with the row it returns to.
func ancestorPages(route Item, t *testing.T) map[string]bool {
	t.Helper()
	ancestor := routeByName(t, route.Parent)
	pages := make(map[string]bool, len(ancestor.Refs))
	for _, ref := range ancestor.Refs {
		pages[ref.Page] = true
	}
	return pages
}

// everythingEnabled is the predicate a host with every capability and an
// untouched configuration resolves to.
func everythingEnabled(string, string) bool { return true }

// alwaysConstructed vouches for every route, which is the caller half of the
// construction seam.
func alwaysConstructed(string) bool { return true }

// A caller holding no configuration at all must not be able to make a route
// reachable: a nil predicate vouches for nothing rather than for everything, so
// an un-wired window leaves only Help — the one primary that is always offered.
func TestNilConfigurationPredicateHidesEveryRoute(t *testing.T) {
	visible := VisibleItems(nil)
	if len(visible) != 1 || visible[0].Name != helpRouteName {
		t.Fatalf("VisibleItems(nil) = %#v, want only %q", visible, helpRouteName)
	}

	routes := VisibleRoutes(nil)
	if len(routes) != 1 || routes[0].Name != helpRouteName {
		t.Fatalf("VisibleRoutes(nil) = %#v, want only %q", routes, helpRouteName)
	}
	for _, route := range routes {
		if !isPrimary(route) {
			t.Errorf("VisibleRoutes(nil) offered the detail %q", route.Name)
		}
	}
}

// A detail is offered only while its own refs are enabled: a visible ancestor
// keeps the row, but a detail whose providers are all disabled would open an
// empty screen, so it stays closed and the row is what the user gets.
func TestVisibleRoutesOmitADetailWhoseOwnRefsAreDisabled(t *testing.T) {
	detail := routeByName(t, "recovery")

	// An ancestor-only ref: it keeps Maintenance's row without enabling
	// either namespace the detail draws its controls from.
	ancestorOnly := func(page, group string) bool {
		return page == "maintenance_page" && group == "maintenance_cleanup_group"
	}
	for _, ref := range detail.Refs {
		if ancestorOnly(ref.Page, ref.Group) {
			t.Fatalf("the ancestor-only predicate also enables the detail's own ref %v", ref)
		}
	}

	routes := VisibleRoutes(ancestorOnly)
	if !containsPage(routes, detail.Parent) {
		t.Fatalf("VisibleRoutes omitted the ancestor %q", detail.Parent)
	}
	if containsPage(routes, detail.Name) {
		t.Errorf("VisibleRoutes offered %q with none of its refs enabled: %#v", detail.Name, routes)
	}
	// A rejected detail falls back to the ancestor whose row is still there
	// rather than opening an empty screen.
	transition, ok := Resolve(detail.Name, routes, alwaysConstructed)
	if !ok {
		t.Fatalf("Resolve(%q) refused to fall back to the visible ancestor %q", detail.Name, detail.Parent)
	}
	if transition.Name != detail.Parent || transition.Detail || transition.Back != "" {
		t.Errorf(
			"Resolve(%q) = %#v, want the ancestor transition for %q",
			detail.Name, transition, detail.Parent,
		)
	}
}

// A detail contributes no sidebar row, so it must not consume an Alt+number
// slot or shift the index a window selects. A caller that builds its own
// inventory — the window hands Resolve whatever it holds — therefore gets the
// same row for the same page whether or not a detail is interleaved with the
// primaries.
func TestDetailEntriesDoNotShiftSidebarRowIndices(t *testing.T) {
	detail := routeByName(t, "recovery")
	primaries := []string{detail.Parent, helpRouteName}

	cases := []struct {
		name      string
		inventory []Item
	}{
		{"detail last", []Item{
			clone(routeByName(t, detail.Parent)),
			clone(routeByName(t, helpRouteName)),
			clone(detail),
		}},
		{"detail first", []Item{
			clone(detail),
			clone(routeByName(t, detail.Parent)),
			clone(routeByName(t, helpRouteName)),
		}},
		{"detail interleaved", []Item{
			clone(routeByName(t, detail.Parent)),
			clone(detail),
			clone(routeByName(t, helpRouteName)),
		}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			for wantIndex, wantName := range primaries {
				transition, ok := Resolve(wantName, testCase.inventory, alwaysConstructed)
				if !ok {
					t.Fatalf("Resolve(%q) rejected a primary present in the inventory", wantName)
				}
				if transition.SelectedIndex != wantIndex {
					t.Errorf(
						"Resolve(%q).SelectedIndex = %d, want %d: a detail consumed a row",
						wantName, transition.SelectedIndex, wantIndex,
					)
				}
				if transition.Name != wantName || transition.Detail {
					t.Errorf("Resolve(%q) = %#v, want a primary transition", wantName, transition)
				}
			}

			transition, ok := Resolve(detail.Name, testCase.inventory, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected a detail present in the inventory", detail.Name)
			}
			if !transition.Detail || transition.Back != detail.Parent {
				t.Errorf("Resolve(%q) = %#v, want a detail transition back to %q", detail.Name, transition, detail.Parent)
			}
			if transition.SelectedIndex != 0 {
				t.Errorf(
					"Resolve(%q).SelectedIndex = %d, want the ancestor's row 0",
					detail.Name, transition.SelectedIndex,
				)
			}
		})
	}
}

// Entering a detail must select the ancestor's row, keep the detail's own
// title, reveal content in a collapsed layout, and name the ancestor it
// returns to. Asserting the transition equals the ancestor's own transition
// where they overlap is the point: Back is not a second navigation path, it is
// the ordinary ancestor transition.
func TestDetailRoutesResolveToTheirAncestorTitleAndBack(t *testing.T) {
	routes := VisibleRoutes(everythingEnabled)
	if len(Details()) == 0 {
		t.Fatal("the inventory declares no detail route; this gate would be vacuous")
	}

	for _, detail := range Details() {
		t.Run(detail.Name, func(t *testing.T) {
			ancestor, ok := Resolve(detail.Parent, routes, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected the detail's own ancestor", detail.Parent)
			}
			transition, ok := Resolve(detail.Name, routes, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected an offered, constructed detail", detail.Name)
			}

			if !transition.Detail {
				t.Error("detail transition does not mark itself a detail")
			}
			if transition.Name != detail.Name || transition.VisibleChild != detail.Name {
				t.Errorf(
					"detail transition entered %q/%q, want the detail route %q",
					transition.Name, transition.VisibleChild, detail.Name,
				)
			}
			if transition.Back != detail.Parent {
				t.Errorf("Back = %q, want the primary ancestor %q", transition.Back, detail.Parent)
			}
			if transition.SelectedIndex != ancestor.SelectedIndex {
				t.Errorf(
					"SelectedIndex = %d, want the ancestor's row %d",
					transition.SelectedIndex, ancestor.SelectedIndex,
				)
			}
			if transition.Title != detail.Title || transition.Title == ancestor.Title {
				t.Errorf(
					"Title = %q, want the detail's own title %q rather than the ancestor's %q",
					transition.Title, detail.Title, ancestor.Title,
				)
			}
			if !transition.ShowContent {
				t.Error("detail transition does not reveal content in a collapsed layout")
			}

			back, ok := Resolve(transition.Back, routes, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected the detail's Back destination", transition.Back)
			}
			if !reflect.DeepEqual(back, ancestor) {
				t.Errorf("Back resolves to %#v, want the ancestor transition %#v", back, ancestor)
			}
			if back.Detail || back.Back != "" {
				t.Errorf("the ancestor transition carries detail state: %#v", back)
			}
		})
	}
}

// A caller that offers primaries only — the window before it consumes detail
// routes, or a host where the detail is switched off — must still land on the
// ancestor rather than on a screen it never constructed.
func TestDetailRoutesFallBackToTheirAncestorWhenNotOffered(t *testing.T) {
	primaries := VisibleItems(everythingEnabled)
	if len(Details()) == 0 {
		t.Fatal("the inventory declares no detail route; this gate would be vacuous")
	}

	for _, detail := range Details() {
		t.Run(detail.Name, func(t *testing.T) {
			transition, ok := Resolve(detail.Name, primaries, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected a detail the caller did not offer", detail.Name)
			}
			ancestor, ok := Resolve(detail.Parent, primaries, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected the detail's ancestor", detail.Parent)
			}
			if !reflect.DeepEqual(transition, ancestor) {
				t.Fatalf("fallback = %#v, want the ancestor transition %#v", transition, ancestor)
			}
		})
	}
}

// A detail whose ancestor has no row has nowhere to return to: it falls
// through to Help. That is the case where only a cross-namespace ref is
// enabled — Recovery's rollback group lives on updates_page, so Updates stays
// visible while Maintenance, which owns the detail's row, does not.
func TestDetailRoutesFallBackToHelpWhenTheirAncestorIsHidden(t *testing.T) {
	exercised := 0
	for _, detail := range Details() {
		ancestor := routeByName(t, detail.Parent)
		ancestorPages := make(map[string]bool)
		for _, ref := range ancestor.Refs {
			ancestorPages[ref.Page] = true
		}
		foreign := make([]Ref, 0, len(detail.Refs))
		for _, ref := range detail.Refs {
			if !ancestorPages[ref.Page] {
				foreign = append(foreign, ref)
			}
		}
		if len(foreign) == 0 {
			t.Run(detail.Name, func(t *testing.T) {
				t.Skip("every ref lives on the ancestor's own page, so its row cannot be hidden independently")
			})
			continue
		}

		t.Run(detail.Name, func(t *testing.T) {
			foreignOnly := func(page, group string) bool {
				for _, ref := range foreign {
					if ref == (Ref{Page: page, Group: group}) {
						return true
					}
				}
				return false
			}
			routes := VisibleRoutes(foreignOnly)
			if containsPage(routes, detail.Parent) {
				t.Fatalf("the ancestor %q is still visible: this case cannot be exercised", detail.Parent)
			}
			if containsPage(routes, detail.Name) {
				t.Errorf("VisibleRoutes offered %q while the ancestor that owns its row is hidden", detail.Name)
			}

			transition, ok := Resolve(detail.Name, routes, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected a detail with a hidden ancestor", detail.Name)
			}
			help, ok := Resolve(helpRouteName, routes, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected the fallback help route", helpRouteName)
			}
			if !reflect.DeepEqual(transition, help) {
				t.Fatalf("fallback = %#v, want the Help transition %#v", transition, help)
			}
			if transition.VisibleChild == detail.Name {
				t.Fatalf("fallback entered the hidden detail %q", detail.Name)
			}
			exercised++
		})
	}
	if exercised == 0 {
		t.Fatal("no detail route spans a namespace outside its ancestor, so this gate never ran")
	}
}

// A deep link into a detail the host cannot back must not reach it: the
// resolution lands on the ancestor, and carries none of the detail's state, so
// a window cannot reveal the screen or offer a Back control to it.
func TestUnsupportedDetailFallsBackWithoutEnteringTheHiddenRoute(t *testing.T) {
	routes := VisibleRoutes(everythingEnabled)
	if len(Details()) == 0 {
		t.Fatal("the inventory declares no detail route; this gate would be vacuous")
	}

	for _, detail := range Details() {
		t.Run(detail.Name+"/unsupported", func(t *testing.T) {
			unsupported := func(name string) bool { return name != detail.Name }
			transition, ok := Resolve(detail.Name, routes, unsupported)
			if !ok {
				t.Fatalf("Resolve(%q) rejected an unsupported detail", detail.Name)
			}
			if transition.Detail || transition.Back != "" {
				t.Errorf("unsupported detail produced detail state: %#v", transition)
			}
			if transition.VisibleChild == detail.Name || transition.Name == detail.Name {
				t.Errorf("unsupported detail was entered anyway: %#v", transition)
			}
			if !containsPage(routes, transition.VisibleChild) {
				t.Errorf("fallback child %q is not a route the caller offered", transition.VisibleChild)
			}
			if transition.Title == detail.Title {
				t.Errorf("fallback kept the detail's title %q", transition.Title)
			}
		})

		t.Run(detail.Name+"/ancestor unavailable too", func(t *testing.T) {
			nothingBacked := func(name string) bool {
				return name != detail.Name && name != detail.Parent
			}
			transition, ok := Resolve(detail.Name, routes, nothingBacked)
			if !ok {
				t.Fatalf("Resolve(%q) rejected a detail with nothing backed", detail.Name)
			}
			if transition.Name != helpRouteName {
				t.Errorf("fallback = %q, want the help route %q", transition.Name, helpRouteName)
			}
		})
	}
}

// A rendered destination may consume several configuration namespaces, so a
// group reference carries the page it is declared on. Enabling the same group
// name on a page the route does not name must not offer it: that is exactly
// what an implementation which inferred configuration identity from a display
// name would do.
func TestCrossNamespaceRefsKeepTheirConfigurationPage(t *testing.T) {
	crossed := 0
	for _, route := range append(Items(), Details()...) {
		pages := make(map[string]bool)
		for _, ref := range route.Refs {
			if ref.Page == "" || ref.Group == "" {
				t.Errorf("%s declares an unqualified ref %v", route.Name, ref)
			}
			pages[ref.Page] = true
		}
		if len(pages) < 2 {
			continue
		}
		crossed++
		t.Run(route.Name, func(t *testing.T) {
			asked := make(map[Ref]bool)
			VisibleRoutes(func(page, group string) bool {
				asked[Ref{Page: page, Group: group}] = true
				return false
			})
			for _, ref := range route.Refs {
				if !asked[ref] {
					t.Errorf("resolution never asked whether %v was enabled", ref)
				}
			}

			for _, ref := range route.Refs {
				oneNamespace := func(page, group string) bool {
					return page == ref.Page && group == ref.Group
				}
				if ancestorPages(route, t)[ref.Page] && !containsPage(VisibleRoutes(oneNamespace), route.Name) {
					t.Errorf("enabling the ancestor's own ref %v did not offer %q", ref, route.Name)
				}

				wrongPage := func(page, group string) bool {
					return page != ref.Page && group == ref.Group
				}
				if containsPage(VisibleRoutes(wrongPage), route.Name) {
					t.Errorf("%q was offered with %q enabled on a page it does not name", route.Name, ref.Group)
				}
			}

			declared := func(page, group string) bool {
				for _, ref := range route.Refs {
					if ref == (Ref{Page: page, Group: group}) {
						return true
					}
				}
				return false
			}
			if !containsPage(VisibleRoutes(declared), route.Name) {
				t.Errorf("enabling %q's own refs did not offer it", route.Name)
			}
		})
	}
	if crossed == 0 {
		t.Fatal("no route spans two configuration namespaces; this gate would be vacuous")
	}
}

// A detail is reached from its ancestor, never from a number: it must acquire
// no Alt+ accelerator, no advertised shortcut, and no registered binding, even
// when the caller hands the shortcut inventory a list that contains one.
func TestShortcutsAdvertisePrimariesOnly(t *testing.T) {
	routes := VisibleRoutes(everythingEnabled)
	details := Details()
	if len(details) == 0 {
		t.Fatal("the inventory declares no detail route; this gate would be vacuous")
	}

	primaries := 0
	for _, route := range routes {
		if isPrimary(route) {
			primaries++
		}
	}
	if primaries != len(Items()) {
		t.Fatalf("offered %d primaries, canonical inventory has %d", primaries, len(Items()))
	}

	alt := make(map[string]string)
	for _, binding := range Bindings(routes) {
		for _, accelerator := range binding.Accelerators {
			if strings.HasPrefix(accelerator, "<Alt>") {
				alt[accelerator] = binding.Action
			}
		}
	}
	if len(alt) != primaries {
		t.Fatalf("Alt binding count = %d, want one per primary (%d): %v", len(alt), primaries, alt)
	}

	shortcutActions := make(map[string]bool)
	for _, shortcut := range Shortcuts(routes) {
		shortcutActions[shortcut.Action] = true
	}
	for _, detail := range details {
		if detail.Accelerator != "" || detail.Display != "" {
			t.Errorf("detail %q carries accelerator %q/%q", detail.Name, detail.Accelerator, detail.Display)
		}
		action := "win.navigate-" + detail.Name
		if shortcutActions[action] {
			t.Errorf("detail %q is advertised as keyboard shortcut %q", detail.Name, action)
		}
		for accelerator, bound := range alt {
			if bound == action {
				t.Errorf("detail %q is bound to %s", detail.Name, accelerator)
			}
		}
	}
}

// Mouse entry reads a sidebar row's name; keyboard entry runs the
// win.navigate-<name> action. The two are the same route identity, so they
// must resolve the same transition — a divergence here is how a key and a
// click end up on different screens.
func TestRowEntryAndShortcutEntryResolveTheSameTransition(t *testing.T) {
	visible := VisibleItems(everythingEnabled)
	routes := VisibleRoutes(everythingEnabled)

	actions := make(map[string]string)
	for _, shortcut := range Shortcuts(visible) {
		actions[shortcut.Action] = strings.TrimPrefix(shortcut.Action, "win.navigate-")
	}

	for _, item := range visible {
		t.Run(item.Name, func(t *testing.T) {
			rowName := item.Name
			keyName, ok := actions["win.navigate-"+rowName]
			if !ok {
				t.Fatalf("no advertised navigate action names the row %q", rowName)
			}
			if keyName != rowName {
				t.Fatalf("keyboard names the route %q, the row names it %q", keyName, rowName)
			}

			fromRow, ok := Resolve(rowName, routes, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected the mouse path's route", rowName)
			}
			fromKey, ok := Resolve(keyName, routes, alwaysConstructed)
			if !ok {
				t.Fatalf("Resolve(%q) rejected the keyboard path's route", keyName)
			}
			if !reflect.DeepEqual(fromRow, fromKey) {
				t.Fatalf("mouse = %#v, keyboard = %#v", fromRow, fromKey)
			}
			if fromRow.Detail || fromRow.Back != "" {
				t.Errorf("primary transition carries detail state: %#v", fromRow)
			}
		})
	}
}

// The canonical inventory is one table, and every route in it states the
// parentage a resolution depends on. Without this, a detail with no parent
// resolves to nothing and an unstated Kind silently becomes a primary row.
func TestRouteInventoryHasOneWellFormedOwner(t *testing.T) {
	seen := make(map[string]bool, len(routes))
	for _, route := range routes {
		t.Run(route.Name, func(t *testing.T) {
			if route.Name == "" || route.Title == "" {
				t.Fatalf("route %q declares no name or title", route.Name)
			}
			if seen[route.Name] {
				t.Fatalf("route %q is declared twice", route.Name)
			}
			seen[route.Name] = true

			if route.Kind != KindPrimary && route.Kind != KindDetail {
				t.Errorf("route %q states kind %q", route.Name, route.Kind)
			}
			if len(route.Refs) == 0 {
				t.Error("route declares no configuration ref, so configuration would gate it off")
			}
			refSeen := make(map[Ref]bool, len(route.Refs))
			for _, ref := range route.Refs {
				if refSeen[ref] {
					t.Errorf("ref %v is declared twice", ref)
				}
				refSeen[ref] = true
			}

			if isPrimary(route) {
				if route.Parent != "" {
					t.Errorf("primary carries a parent %q", route.Parent)
				}
				if route.Icon == "" {
					t.Error("primary declares no sidebar icon")
				}
				return
			}

			if route.Parent == "" {
				t.Fatal("detail declares no parent")
			}
			ancestor, ok := lookup(route.Parent)
			if !ok || !isPrimary(ancestor) {
				t.Fatalf("detail parent %q is not a primary route", route.Parent)
			}
			if route.Accelerator != "" || route.Display != "" {
				t.Errorf("detail carries accelerator %q/%q", route.Accelerator, route.Display)
			}
			if route.AlwaysShow {
				t.Error("detail carries AlwaysShow, which is a sidebar rule")
			}
			if route.Icon != "" {
				t.Errorf("detail carries sidebar icon %q", route.Icon)
			}
		})
	}

	if len(Items())+len(Details()) != len(routes) {
		t.Fatalf(
			"the canonical table holds %d routes but Items() and Details() account for %d: a route is unclassified",
			len(routes), len(Items())+len(Details()),
		)
	}
}
