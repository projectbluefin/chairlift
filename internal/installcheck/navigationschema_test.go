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
// ConfigPage and Groups fields of its canonical sidebar inventory, and the
// views builders are held to *navigation's* copy by
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
// These tests close that edge. They assert set equality, not order:
// config.SchemaGroups sorts its result, while navigation's slices are in
// sidebar presentation order, which is navigation's own concern.

// sortedNames returns a freshly allocated, lexicographically sorted copy of
// values, so comparing two inventories never mutates either one.
func sortedNames(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

// TestNavigationPagesMatchConfigSchema holds navigation's ConfigPage values
// and config.SchemaPages() to a bijection: every configurable page is
// reachable from the sidebar, and every sidebar entry names a page the
// schema actually declares.
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

	navPages := make([]string, 0, len(items))
	seen := make(map[string]string, len(items))
	for _, item := range items {
		if item.ConfigPage == "" {
			t.Errorf("navigation page %q declares no ConfigPage", item.Name)
			continue
		}
		if previous, dup := seen[item.ConfigPage]; dup {
			t.Errorf(
				"navigation pages %q and %q both claim config page %q",
				previous, item.Name, item.ConfigPage,
			)
			continue
		}
		seen[item.ConfigPage] = item.Name
		navPages = append(navPages, item.ConfigPage)
	}

	want := sortedNames(schemaPages)
	got := sortedNames(navPages)
	if !reflect.DeepEqual(got, want) {
		t.Errorf(
			"navigation ConfigPage inventory = %v, config.SchemaPages() = %v",
			got, want,
		)
	}
}

// TestNavigationGroupsMatchConfigSchema holds every navigation page's Groups
// slice to the group names config declares for that page, so a group cannot
// be added to or removed from one side alone.
func TestNavigationGroupsMatchConfigSchema(t *testing.T) {
	items := navigation.Items()
	if len(items) == 0 {
		t.Fatal("navigation.Items() is empty: the gate would be vacuous")
	}

	for _, item := range items {
		t.Run(item.Name, func(t *testing.T) {
			schemaGroups, err := config.SchemaGroups(item.ConfigPage)
			if err != nil {
				t.Fatalf("config.SchemaGroups(%q): %v", item.ConfigPage, err)
			}
			if len(schemaGroups) == 0 {
				t.Fatalf(
					"config declares no groups for %q: the gate would be vacuous",
					item.ConfigPage,
				)
			}

			want := sortedNames(schemaGroups)
			got := sortedNames(item.Groups)
			if !reflect.DeepEqual(got, want) {
				t.Errorf(
					"navigation groups for %q = %v, config.SchemaGroups(%q) = %v",
					item.Name, got, item.ConfigPage, want,
				)
			}
		})
	}
}
