package installcheck

import (
	"strings"
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
// internal/navigation restates that grammar a second time, as the Refs of its
// canonical route inventory, and the views builders are held to *navigation's*
// copy by navigation.TestPageMetadataMatchesViewBuilders. These tests close
// the remaining edge in the only direction that is still meaningful.
//
// The former gate here asserted a *bijection* between navigation's pages and
// config.SchemaPages(): one sidebar entry, one configuration page. That
// assumption is retired. #233 organizes the product around five task
// destinations whose content is composed from several configuration
// namespaces — Updates owns the Automatic Updates switch drawn inside
// updates_page's update_all_group *and* the early-release channel control from
// system_page — so a route holds a set of refs and one group may be claimed by
// more than one route. What survives is the edge the bijection existed to
// catch, restated without the one-to-one premise:
//
//   - a group added to config but claimed by no surviving route is
//     configurable and documented but belongs to no destination, which is how
//     a capability ends up with no owner at the cutover;
//   - a ref named by navigation but absent from config is silently
//     un-disableable: config.IsGroupEnabled returns true for any group a
//     known page's map does not declare, so the guard around it is always
//     taken, and an administrator who names that group in config.yml to turn
//     it off instead trips the schema validator, which rejects a group name
//     unknown to config.SchemaGroups and fails closed — disabling every
//     configurable group until the file is fixed.
//
// The coverage tests assert set membership, not order: config.SchemaGroups
// sorts its result, while navigation's slices are in presentation order,
// which is navigation's own concern.
//
// The cutover half of the contract lives in
// navigationroutes_test.go: the route table, the destination inventory, and
// the reviewed matrix in docs/design/navigation-routes.md.

// groupRef is one configuration namespace, keyed for set comparison.
type groupRef struct {
	page  string
	group string
}

// schemaGroupRefs returns every configuration namespace config declares,
// keyed by page, with each page's groups in the schema's sorted order.
func schemaGroupRefs(t *testing.T) map[string][]string {
	t.Helper()
	pages, err := config.SchemaPages()
	if err != nil {
		t.Fatalf("config.SchemaPages(): %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("config.SchemaPages() is empty: the gates would be vacuous")
	}

	result := make(map[string][]string, len(pages))
	for _, page := range pages {
		groups, err := config.SchemaGroups(page)
		if err != nil {
			t.Fatalf("config.SchemaGroups(%q): %v", page, err)
		}
		if len(groups) == 0 {
			t.Fatalf("config declares no groups for %q: the gates would be vacuous", page)
		}
		result[page] = groups
	}
	return result
}

// routeGroupExemptions names configuration groups that deliberately belong to
// no surviving route. Every entry carries a reason, and
// TestRouteGroupExemptionsAreAllLive rejects one that has gone stale.
var routeGroupExemptions = map[groupRef]string{
	{"maintenance_page", "maintenance_optimization_group"}: "an empty placeholder with no controls; #233 retires it at the cutover, so it owns no destination and this entry goes with it",
}

func TestRouteGroupExemptionsAreAllLive(t *testing.T) {
	schema := schemaGroupRefs(t)
	for ref, reason := range routeGroupExemptions {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("exemption %s/%s has no reason", ref.page, ref.group)
		}
		groups, ok := schema[ref.page]
		if !ok {
			t.Errorf("exemption %s/%s names a page config does not declare", ref.page, ref.group)
			continue
		}
		if !containsString(groups, ref.group) {
			t.Errorf(
				"exemption %s/%s names a group config does not declare; the exemption is stale",
				ref.page, ref.group,
			)
		}
	}
}

// TestEveryConfigGroupHasASurvivingRoute is the replacement for the retired
// bijection. A group no surviving route claims has no destination after the
// #201 cutover, which is the state the destination matrix exists to prevent.
func TestEveryConfigGroupHasASurvivingRoute(t *testing.T) {
	schema := schemaGroupRefs(t)

	claimed := make(map[groupRef][]string)
	routes := navigation.Routes()
	if len(routes) == 0 {
		t.Fatal("navigation.Routes() is empty: the gate would be vacuous")
	}
	for _, route := range routes {
		if route.Temporary {
			// A route the cutover deletes cannot be why a group survives it.
			continue
		}
		for _, ref := range route.Refs {
			key := groupRef{ref.Page, ref.Group}
			claimed[key] = append(claimed[key], route.Name)
		}
	}

	for page, groups := range schema {
		for _, group := range groups {
			key := groupRef{page, group}
			if _, ok := claimed[key]; ok {
				continue
			}
			if reason, exempt := routeGroupExemptions[key]; exempt {
				t.Logf("group %s/%s is exempt: %s", page, group, reason)
				continue
			}
			t.Errorf(
				"config declares group %s/%s but no surviving navigation route claims it; "+
					"give it a destination in internal/navigation or record an exemption with a reason",
				page, group,
			)
		}
	}
}

// TestEveryRouteRefNamesARealConfigGroup is the invalid-mapping half: a ref
// config does not declare is silently un-disableable, so the group it guards
// can never be turned off by an administrator.
func TestEveryRouteRefNamesARealConfigGroup(t *testing.T) {
	schema := schemaGroupRefs(t)

	routes := navigation.Routes()
	if len(routes) == 0 {
		t.Fatal("navigation.Routes() is empty: the gate would be vacuous")
	}

	for _, route := range routes {
		t.Run(route.Name, func(t *testing.T) {
			for _, ref := range route.Refs {
				groups, ok := schema[ref.Page]
				if !ok {
					t.Errorf("ref %s/%s names a page config does not declare", ref.Page, ref.Group)
					continue
				}
				if !containsString(groups, ref.Group) {
					t.Errorf("ref %s/%s names a group config does not declare", ref.Page, ref.Group)
				}
			}
		})
	}
}

// containsString reports whether values holds want.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
