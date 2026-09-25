package installcheck

import (
	"reflect"
	"sort"
	"testing"

	"github.com/projectbluefin/chairlift/internal/config"
	"github.com/projectbluefin/chairlift/internal/navigation"
)

// The page/group grammar has one declared owner: internal/config. Its
// exported schema surface (config.SchemaPages, config.SchemaGroups) is
// derived by reflection from Config's yaml tags and defaultConfig(), and the
// repository documentation gates (documentation_test.go, walkthrough_test.go)
// already hold CONFIG.md and docs/walkthrough.md to it.
//
// internal/navigation restates that same grammar a second time, as the
// configuration refs of its canonical route inventory, and the views builders
// are held to *navigation's* copy by
// navigation.TestPageMetadataMatchesViewBuilders. Nothing held navigation's
// copy to the owner, so the two halves of the contract could diverge in
// either direction with every gate still green:
//
//   - a group added to config but not to navigation is configurable and
//     documented but can never make its page appear in the sidebar, because
//     navigation.VisibleItems only consults the groups it knows about;
//   - a group named by navigation but absent from config is silently
//     un-disableable: config.IsGroupEnabled returns true for any group a
//     known page's map does not declare, so the guard around it is always
//     taken, and an administrator who names that group in config.yml to
//     turn it off instead trips the schema validator, which rejects a group
//     name unknown to config.SchemaGroups and fails closed — disabling
//     every configurable group until the file is fixed.
//
// These tests close that edge. A route's refs are page-qualified — the shape
// that lets one destination consume several namespaces, which is how the
// Recovery detail draws rollback controls from updates_page and reset
// controls from maintenance_page — so the page comparison and the group
// comparison are made per (page, group) pair rather than through one page
// field per route. The two drift directions above are still checked in both
// directions; what the inventory no longer claims is that each route maps to
// exactly one configuration page.
//
// These tests assert set equality, not order: config.SchemaGroups sorts its
// result, while navigation's slices are in sidebar presentation order, which
// is navigation's own concern.

// sortedNames returns a freshly allocated, lexicographically sorted copy of
// values, so comparing two inventories never mutates either one.
func sortedNames(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

// refKey renders one configuration ref for messages.
func refKey(ref navigation.Ref) string {
	return ref.Page + "/" + ref.Group
}

// TestNavigationPagesMatchConfigSchema holds the pages navigation's sidebar
// inventory consumes and config.SchemaPages() to a bijection: every
// configurable page is reachable from the sidebar, and every sidebar entry
// names pages the schema actually declares.
func TestNavigationPagesMatchConfigSchema(t *testing.T) {
	schemaPages, err := config.SchemaPages()
	if err != nil {
		t.Fatalf("config.SchemaPages(): %v", err)
	}
	if len(schemaPages) == 0 {
		t.Fatal("config.SchemaPages() is empty: the gate would be vacuous")
	}

	items := navigation.Items()
	if len(items) == 0 {
		t.Fatal("navigation.Items() is empty: the gate would be vacuous")
	}

	declared := make(map[string]bool, len(schemaPages))
	for _, page := range schemaPages {
		declared[page] = true
	}

	navPages := make([]string, 0, len(items))
	seen := make(map[string]string, len(items))
	for _, item := range items {
		if len(item.Refs) == 0 {
			t.Errorf("navigation page %q consumes no configuration ref", item.Name)
			continue
		}
		// One route consumes several groups on one page, so its own refs
		// repeat that page. Deduplicate within the route before comparing
		// routes: two *different* pages claiming one configuration page is
		// the drift this gate exists to catch.
		pages := make([]string, 0, len(item.Refs))
		own := make(map[string]bool, len(item.Refs))
		for _, ref := range item.Refs {
			if ref.Page == "" {
				t.Errorf("navigation page %q declares an unqualified ref %q", item.Name, ref.Group)
				continue
			}
			if !declared[ref.Page] {
				t.Errorf(
					"navigation page %q consumes %q, which config.SchemaPages() does not declare",
					item.Name, ref.Page,
				)
				continue
			}
			if own[ref.Page] {
				continue
			}
			own[ref.Page] = true
			pages = append(pages, ref.Page)
		}
		for _, page := range pages {
			if previous, dup := seen[page]; dup {
				t.Errorf(
					"navigation pages %q and %q both consume config page %q",
					previous, item.Name, page,
				)
				continue
			}
			seen[page] = item.Name
			navPages = append(navPages, page)
		}
	}

	want := sortedNames(schemaPages)
	got := sortedNames(navPages)
	if !reflect.DeepEqual(got, want) {
		t.Errorf(
			"navigation consumes config pages %v, config.SchemaPages() = %v",
			got, want,
		)
	}
}

// TestNavigationGroupsMatchConfigSchema holds the groups navigation's sidebar
// inventory consumes to the group names config declares for those pages, so a
// group cannot be added to or removed from one side alone. Every group of
// every page the inventory consumes must be claimed by exactly one entry: a
// group no route names is a feature that cannot make its page appear, and a
// group no page declares is one an administrator cannot switch off.
func TestNavigationGroupsMatchConfigSchema(t *testing.T) {
	items := navigation.Items()
	if len(items) == 0 {
		t.Fatal("navigation.Items() is empty: the gate would be vacuous")
	}

	claimed := make(map[string]map[string]bool)
	for _, item := range items {
		for _, ref := range item.Refs {
			groups, ok := claimed[ref.Page]
			if !ok {
				groups = make(map[string]bool)
				claimed[ref.Page] = groups
			}
			if groups[ref.Group] {
				t.Errorf("navigation claims %s twice", refKey(ref))
			}
			groups[ref.Group] = true
		}
	}

	for _, page := range sortedNames(pagesOf(claimed)) {
		t.Run(page, func(t *testing.T) {
			schemaGroups, err := config.SchemaGroups(page)
			if err != nil {
				t.Fatalf("config.SchemaGroups(%q): %v", page, err)
			}
			if len(schemaGroups) == 0 {
				t.Fatalf("config declares no groups for %q: the gate would be vacuous", page)
			}

			want := sortedNames(schemaGroups)
			got := sortedNames(keysOf(claimed[page]))
			if !reflect.DeepEqual(got, want) {
				t.Errorf(
					"navigation groups for %q = %v, config.SchemaGroups(%q) = %v",
					page, got, page, want,
				)
			}
		})
	}
}

// TestEveryNavigationRefNamesADeclaredGroup holds every ref of every route —
// primaries and details alike — to the schema. It is the cross-namespace
// edge: a detail's ref must name the page the group is actually declared on,
// so a route that drew a group from the wrong namespace fails here instead of
// silently gating itself on a pair nothing else recognizes.
func TestEveryNavigationRefNamesADeclaredGroup(t *testing.T) {
	routes := append(navigation.Items(), navigation.Details()...)
	if len(routes) == 0 {
		t.Fatal("navigation declares no routes: the gate would be vacuous")
	}
	if len(navigation.Details()) == 0 {
		t.Fatal("navigation declares no detail routes: the cross-namespace case is untested")
	}

	for _, route := range routes {
		t.Run(route.Name, func(t *testing.T) {
			if len(route.Refs) == 0 {
				t.Fatalf("route %q consumes no configuration ref", route.Name)
			}
			for _, ref := range route.Refs {
				schemaGroups, err := config.SchemaGroups(ref.Page)
				if err != nil {
					t.Errorf("route %q consumes %s: %v", route.Name, refKey(ref), err)
					continue
				}
				found := false
				for _, group := range schemaGroups {
					if group == ref.Group {
						found = true
						break
					}
				}
				if !found {
					t.Errorf(
						"route %q consumes %s, which config does not declare on %q",
						route.Name, refKey(ref), ref.Page,
					)
				}
			}
		})
	}
}

func pagesOf(claimed map[string]map[string]bool) []string {
	pages := make([]string, 0, len(claimed))
	for page := range claimed {
		pages = append(pages, page)
	}
	return pages
}

func keysOf(groups map[string]bool) []string {
	names := make([]string, 0, len(groups))
	for group := range groups {
		names = append(names, group)
	}
	return names
}
