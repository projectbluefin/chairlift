package installcheck

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/navigation"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/updexhelper"
)

// cutoverDoc is the reviewed destination and ownership matrix. It is the
// human-readable half of the route table, and these tests are the forcing
// function that keeps the two in step: a route, a destination, or a
// privileged action that the table gains but the document never names fails
// here, in the same pull request.
const cutoverDoc = "docs/design/navigation-routes.md"

// TestDestinationInventoryIsComplete holds the five-destination contract:
// every destination has a surviving primary route, every route names a
// destination it belongs to, and a detail belongs to exactly one.
func TestDestinationInventoryIsComplete(t *testing.T) {
	destinations := navigation.Destinations()
	if len(destinations) == 0 {
		t.Fatal("navigation.Destinations() is empty: the gate would be vacuous")
	}

	names := make([]string, 0, len(destinations))
	for _, destination := range destinations {
		if destination.Name == "" || destination.Title == "" || destination.Icon == "" {
			t.Errorf("destination %#v is incomplete; the sidebar needs a name, title and icon", destination)
		}
		names = append(names, destination.Name)
	}

	served := make(map[string]bool)
	for _, route := range navigation.Routes() {
		if len(route.Serves) == 0 {
			t.Errorf("route %q belongs to no destination", route.Name)
		}
		for _, destination := range route.Serves {
			if !containsString(names, destination) {
				t.Errorf("route %q names unknown destination %q", route.Name, destination)
			}
		}
		if route.Kind != navigation.Detail {
			continue
		}
		if len(route.Serves) != 1 {
			t.Errorf("detail route %q names %d destinations, want exactly 1", route.Name, len(route.Serves))
		}
		// A detail is not a sidebar entry: it must not claim primary-only
		// metadata, and Resolve never assigns it an accelerator.
		if route.Icon != "" || route.AlwaysShow || route.Fallback || route.Temporary {
			t.Errorf("detail route %q declares primary-only metadata", route.Name)
		}
		if len(route.Refs) == 0 {
			t.Errorf("detail route %q declares no refs, so nothing gates it", route.Name)
		}
	}

	for _, route := range navigation.Routes() {
		if route.Temporary || route.Kind != navigation.Primary {
			continue
		}
		for _, destination := range route.Serves {
			served[destination] = true
		}
	}
	for _, destination := range names {
		if !served[destination] {
			t.Errorf("destination %q has no surviving primary route", destination)
		}
	}
}

// TestCanonicalRouteTableIsWellFormed holds the invariants the transition
// machinery assumes: unique, complete routes with exactly one fallback.
func TestCanonicalRouteTableIsWellFormed(t *testing.T) {
	routes := navigation.Routes()
	if len(routes) == 0 {
		t.Fatal("navigation.Routes() is empty: the gate would be vacuous")
	}

	seen := make(map[string]bool, len(routes))
	fallbacks := 0
	for _, route := range routes {
		if route.Name == "" || route.Title == "" {
			t.Errorf("route %#v is incomplete", route)
		}
		if seen[route.Name] {
			t.Errorf("duplicate route name %q", route.Name)
		}
		seen[route.Name] = true
		if route.Kind != navigation.Primary && route.Kind != navigation.Detail {
			t.Errorf("route %q has unknown kind %q", route.Name, route.Kind)
		}
		if !route.Fallback {
			continue
		}
		fallbacks++
		if route.Kind != navigation.Primary || !route.AlwaysShow {
			t.Errorf("fallback route %q must be an always-visible primary", route.Name)
		}
	}
	if fallbacks != 1 {
		t.Errorf("route table has %d fallback routes, want exactly 1", fallbacks)
	}

	if fallbacks == 1 {
		// The fallback route is where a disabled destination lands, so it
		// has to be reachable with nothing enabled at all.
		visible := navigation.VisibleRoutes(func(string, string) bool { return false })
		if len(visible) != 1 || !visible[0].Fallback {
			t.Errorf("VisibleRoutes(all disabled) = %v, want the fallback route alone", visible)
		}
	}
}

// TestRouteTableIsDocumented holds the reviewed matrix to the code table in
// both directions, and keeps the cutover list a real, non-empty list.
func TestRouteTableIsDocumented(t *testing.T) {
	doc := readRepoFile(t, cutoverDoc)

	routes := navigation.Routes()
	if len(routes) == 0 {
		t.Fatal("navigation.Routes() is empty: the gate would be vacuous")
	}

	temporary := 0
	for _, route := range routes {
		if !strings.Contains(doc, "`"+route.Name+"`") {
			t.Errorf("%s does not name route %q", cutoverDoc, route.Name)
		}
		if route.Temporary {
			temporary++
		}
	}
	if temporary == 0 {
		t.Fatal("no route is marked Temporary; the cutover list would be empty and the gate vacuous")
	}

	for _, destination := range navigation.Destinations() {
		if !strings.Contains(doc, "`"+destination.Name+"`") {
			t.Errorf("%s does not name destination %q", cutoverDoc, destination.Name)
		}
	}
}

// TestPrivilegedActionsHaveADocumentedOwner holds the ownership half of the
// matrix to the two inventories that already enumerate the privileged
// surface. A helper subcommand with no named owner is an action the cutover
// could relocate and forget — which is exactly how a privileged control ends
// up with no route that owns it.
func TestPrivilegedActionsHaveADocumentedOwner(t *testing.T) {
	doc := readRepoFile(t, cutoverDoc)

	commands := append(ubluehelper.SupportedCommands(), updexhelper.SupportedCommands()...)
	if len(commands) == 0 {
		t.Fatal("the helper inventories are empty: the gate would be vacuous")
	}
	for _, command := range commands {
		if !strings.Contains(doc, "`"+command+"`") {
			t.Errorf("%s does not name privileged action %q", cutoverDoc, command)
		}
	}
}
