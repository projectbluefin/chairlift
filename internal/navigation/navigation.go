// Package navigation owns ChairLift's canonical route inventory — the seven
// sidebar primaries and the detail screens reached from them — plus the
// widget-free state transition for selecting a route.
package navigation

import "strconv"

// Shortcut groups used by the shortcuts dialog.
const (
	GroupNavigation = "navigation"
	GroupGeneral    = "general"
)

// helpRouteName is the destination a resolution falls back to when the
// requested route cannot be entered and its own ancestor cannot either. Help
// carries AlwaysShow, so a window that builds its inventory through
// VisibleItems always has it.
const helpRouteName = "help"

// Kind classifies a route by the way a user enters it.
type Kind string

const (
	// KindPrimary is a sidebar destination: it has a row, an advertised
	// Alt+number accelerator, and an entry in the shortcuts dialog.
	KindPrimary Kind = "primary"
	// KindDetail is a focused screen reached from the primary that owns it.
	// It has no sidebar row and no Alt+number accelerator: it is entered
	// from its ancestor's content, from its own Back control, or from a
	// deep link, and those three agree only because its identity lives
	// here.
	KindDetail Kind = "detail"
)

// Ref names one configuration namespace a rendered route consumes: the
// configuration page the group is declared on, and the group itself.
//
// A route holds a slice of them because a rendered destination may consume
// several namespaces at once. The Recovery detail is the live example: its
// rollback controls are gated by bootc_updates_group on updates_page, while
// its reset controls are gated by reset_group on maintenance_page — which is
// why one ConfigPage field per route could not express it, and why a route's
// configuration identity is never inferred from its display name.
type Ref struct {
	Page  string
	Group string
}

// Item describes one route in the canonical inventory.
type Item struct {
	Name  string
	Title string
	// Icon is the sidebar glyph. A detail leaves it empty: it has no row,
	// so its entry point carries its own affordance in the page that opens
	// it.
	Icon string

	// Kind is KindPrimary for a sidebar destination and KindDetail for a
	// screen reached from one. The empty value reads as KindPrimary.
	Kind Kind

	// Parent names the primary route a detail belongs to: the sidebar
	// ancestor that stays selected while the detail is shown, and the
	// destination the detail's Back control returns to. It is empty on a
	// primary, and always names a primary.
	Parent string

	// Refs are the configuration namespaces this route consumes. A primary
	// consumes the groups that make it worth showing; a detail consumes
	// the groups that build its controls, which may span pages.
	Refs []Ref

	// Accelerator and Display are the compacted Alt+number binding, set on
	// a primary and empty on a detail.
	Accelerator string
	Display     string

	// AlwaysShow keeps a primary in the sidebar even when every one of its
	// groups is disabled, so the window always has a destination. A detail
	// never carries it: a detail is not a row.
	AlwaysShow bool
}

// Shortcut describes one advertised and registered keyboard shortcut.
type Shortcut struct {
	Action      string
	Accelerator string
	Display     string
	Title       string
	Group       string
}

// Binding groups every accelerator registered for one application action.
type Binding struct {
	Action       string
	Accelerators []string
}

// Transition is the complete UI state change for navigating to one route.
type Transition struct {
	// Name is the route actually entered. It differs from the requested
	// route when a detail fell back to its ancestor or to Help, so a
	// caller can tell what it landed on instead of inferring it from a
	// title.
	Name          string
	SelectedIndex int
	VisibleChild  string
	Title         string
	ShowContent   bool

	// Detail is true when the entered route is a detail screen, and Back
	// names the primary that screen returns to. Both are zero on a primary
	// transition, so a window offers a Back control only where one leads
	// somewhere.
	Detail bool
	Back   string
}

// routes is the one canonical route inventory: the seven sidebar primaries in
// order, followed by the detail screens they own. Items, Details, VisibleItems
// and Resolve all read this table and nothing else.
var routes = []Item{
	{
		Name:  "updates",
		Title: "Updates",
		Icon:  "software-update-available-symbolic",
		Kind:  KindPrimary,
		Refs: refsOn("updates_page",
			"automatic_updates_group",
			"bootc_updates_group",
			"flatpak_updates_group",
			"brew_updates_group",
			"brew_trust_group",
			"channel_group",
			"bootc_status_group",
		),
	},
	{
		Name:  "applications",
		Title: "Apps",
		Icon:  "application-x-executable-symbolic",
		Kind:  KindPrimary,
		Refs: refsOn("applications_page",
			"applications_installed_group",
			"flatpak_user_group",
			"flatpak_system_group",
			"brew_group",
			"brew_search_group",
			"brew_bundles_group",
		),
	},
	{
		Name:  "agents",
		Title: "Agents",
		Icon:  "starred-symbolic",
		Kind:  KindPrimary,
		Refs:  refsOn("agents_page", "agents_group"),
	},
	{
		Name:  "features",
		Title: "Features",
		Icon:  "application-x-addon-symbolic",
		Kind:  KindPrimary,
		Refs: refsOn("features_page",
			"features_group",
			"dx_group",
			"gaming_group",
		),
	},
	{
		Name:  "livery",
		Title: "Livery",
		Icon:  "preferences-desktop-appearance-symbolic",
		Kind:  KindPrimary,
		Refs: refsOn("livery_page",
			"account_group",
			"livery_app_grid_group",
			"livery_foundation_group",
			"livery_dock_group",
		),
	},
	{
		Name:  "maintenance",
		Title: "Maintenance",
		Icon:  "emblem-system-symbolic",
		Kind:  KindPrimary,
		Refs: refsOn("maintenance_page",
			"maintenance_cleanup_group",
			"maintenance_freespace_group",
			"reset_group",
		),
	},
	{
		Name:  "help",
		Title: "Help",
		Icon:  "help-browser-symbolic",
		Kind:  KindPrimary,
		// Troubleshooting leads Help (issue #249); resources follow it.
		Refs: refsOn("help_page",
			"troubleshooting_group",
			"help_resources_group",
		),
		AlwaysShow: true,
	},

	// Detail routes. A detail is not a sidebar entry: it is a content-stack
	// sibling of the primary named by Parent, which stays selected while the
	// detail is shown, and Back returns there. Recovery is the detail the
	// window already opens today from Maintenance; until the window consumes
	// this transition its child name, title and Back target are spelled in
	// internal/window, which is the duplication this table removes.
	{
		Name:   "recovery",
		Title:  "Recovery",
		Kind:   KindDetail,
		Parent: "maintenance",
		Refs: []Ref{
			// The reset controls are configuration on the Maintenance
			// page; the rollback controls are configuration on Updates.
			// Keeping both refs is what lets Recovery be gated on the
			// same two namespaces maintenance_page.go's entry row and
			// recovery.go's builders already consult.
			{Page: "maintenance_page", Group: "reset_group"},
			{Page: "updates_page", Group: "bootc_updates_group"},
		},
	},
}

var generalShortcuts = []Shortcut{
	{Action: "win.show-shortcuts", Accelerator: "<Primary>question", Display: "Ctrl+?", Title: "Keyboard Shortcuts", Group: GroupGeneral},
	{Action: "app.quit", Accelerator: "<Primary>q", Display: "Ctrl+Q", Title: "Quit", Group: GroupGeneral},
	{Action: "win.navigate-help", Accelerator: "F1", Display: "F1", Title: "Help", Group: GroupGeneral},
}

// refsOn pairs one configuration page with the groups a route consumes on it.
func refsOn(page string, groups ...string) []Ref {
	refs := make([]Ref, 0, len(groups))
	for _, group := range groups {
		refs = append(refs, Ref{Page: page, Group: group})
	}
	return refs
}

// Items returns a copy of the complete canonical sidebar inventory — the
// primaries, with accelerators assigned as though every one were visible. It
// never returns a detail: a detail has no row and no Alt+number.
func Items() []Item {
	return compact(primaryRoutes())
}

// Details returns a copy of the canonical detail routes: the screens reached
// from a primary, with no accelerator and no sidebar position.
func Details() []Item {
	source := detailRoutes()
	result := make([]Item, len(source))
	for index, route := range source {
		result[index] = clone(route)
	}
	return result
}

// VisibleItems filters the canonical primary inventory using static group
// configuration and compacts Alt+number accelerators over the result. Help is
// always retained so the window always has a valid destination.
func VisibleItems(enabled func(page, group string) bool) []Item {
	visible := make([]Item, 0, len(routes))
	for _, item := range primaryRoutes() {
		if item.AlwaysShow || anyRefEnabled(item, enabled) {
			visible = append(visible, item)
		}
	}
	return compact(visible)
}

// VisibleRoutes returns every route the caller can enter under the supplied
// configuration predicate: the visible primaries in sidebar order, followed by
// the details whose own refs are enabled and whose primary ancestor is also
// visible.
//
// A detail is offered last and only under those conditions because the sidebar
// keeps its ancestor selected while the detail is shown and Back returns
// there: a detail whose ancestor no longer has a row has nowhere to return to,
// so Resolve would fall back rather than enter it. Passing the result to
// Resolve is what lets a deep link reach a detail; passing VisibleItems, whose
// entries are all primaries, leaves every detail to the safe fallback.
func VisibleRoutes(enabled func(page, group string) bool) []Item {
	visible := VisibleItems(enabled)
	result := make([]Item, 0, len(visible)+len(routes))
	result = append(result, visible...)
	for _, detail := range detailRoutes() {
		if _, _, ancestorVisible := primaryAt(visible, detail.Parent); !ancestorVisible {
			continue
		}
		if !anyRefEnabled(detail, enabled) {
			continue
		}
		result = append(result, clone(detail))
	}
	return result
}

// Shortcuts returns the advertised shortcuts for the supplied visible routes
// plus the canonical general shortcuts. Only primaries are advertised: a
// detail is entered from its ancestor, so it acquires no Alt+number and no
// dialog entry even when a caller hands this function an inventory that
// contains one.
func Shortcuts(visible []Item) []Shortcut {
	result := make([]Shortcut, 0, len(visible)+len(generalShortcuts))
	for _, item := range visible {
		if !isPrimary(item) {
			continue
		}
		result = append(result, Shortcut{
			Action:      "win.navigate-" + item.Name,
			Accelerator: item.Accelerator,
			Display:     item.Display,
			Title:       "Go to " + item.Title,
			Group:       GroupNavigation,
		})
	}
	return append(result, generalShortcuts...)
}

// Bindings groups the shortcuts for the supplied visible routes by action for
// GTK registration. Like Shortcuts, it covers primaries only.
func Bindings(visible []Item) []Binding {
	shortcuts := Shortcuts(visible)
	indexes := make(map[string]int, len(shortcuts))
	bindings := make([]Binding, 0, len(shortcuts))
	for _, shortcut := range shortcuts {
		index, ok := indexes[shortcut.Action]
		if !ok {
			index = len(bindings)
			indexes[shortcut.Action] = index
			bindings = append(bindings, Binding{Action: shortcut.Action})
		}
		bindings[index].Accelerators = append(
			bindings[index].Accelerators,
			shortcut.Accelerator,
		)
	}
	return bindings
}

// Resolve derives every state mutation needed to navigate to routeName.
//
// It rejects a name the canonical inventory does not declare, and a primary
// the caller did not offer or vouch for. A detail resolves to its primary
// ancestor's row index, its own title, and the ancestor its Back control
// returns to.
//
// A detail the caller did not offer — unsupported on this host, never
// constructed, or disabled by configuration — resolves instead to its nearest
// visible ancestor and, failing that, to Help. Neither path can execute
// anything: a Transition carries only the state a window applies, and the
// rejected route's name never reaches it, so a fallback cannot reveal a screen
// or fire a control the user cannot see.
func Resolve(routeName string, visible []Item, available func(string) bool) (Transition, bool) {
	route, ok := lookup(routeName)
	if !ok {
		return Transition{}, false
	}
	if isPrimary(route) {
		return primaryTransition(route.Name, visible, available)
	}

	ancestor, index, ancestorVisible := primaryAt(visible, route.Parent)
	if ancestorVisible && offered(visible, routeName) && constructed(available, routeName) {
		return Transition{
			Name:          route.Name,
			SelectedIndex: index,
			VisibleChild:  route.Name,
			Title:         route.Title,
			ShowContent:   true,
			Detail:        true,
			Back:          route.Parent,
		}, true
	}

	// The detail is unusable here. Prefer the ancestor the user would have
	// arrived from; Help is the last resort, and carries AlwaysShow so a
	// caller that built its inventory through VisibleItems has it.
	if ancestorVisible {
		if transition, ok := primaryTransitionAt(ancestor, index, available); ok {
			return transition, true
		}
	}
	return primaryTransition(helpRouteName, visible, available)
}

// primaryRoutes and detailRoutes split the one canonical table by kind. They
// are filtered views of it, not tables of their own.
func primaryRoutes() []Item {
	return filterRoutes(func(item Item) bool { return isPrimary(item) })
}

func detailRoutes() []Item {
	return filterRoutes(func(item Item) bool { return !isPrimary(item) })
}

func filterRoutes(keep func(Item) bool) []Item {
	result := make([]Item, 0, len(routes))
	for _, route := range routes {
		if keep(route) {
			result = append(result, route)
		}
	}
	return result
}

// lookup finds a canonical route by name.
func lookup(name string) (Item, bool) {
	for _, route := range routes {
		if route.Name == name {
			return route, true
		}
	}
	return Item{}, false
}

// isPrimary reports whether a route is a sidebar destination. The empty Kind
// reads as primary so a route built by a caller that predates the field keeps
// the sidebar behavior it had.
func isPrimary(item Item) bool {
	return item.Kind == "" || item.Kind == KindPrimary
}

// primaryTransition resolves a primary route that the caller offers.
func primaryTransition(name string, visible []Item, available func(string) bool) (Transition, bool) {
	item, index, ok := primaryAt(visible, name)
	if !ok {
		return Transition{}, false
	}
	return primaryTransitionAt(item, index, available)
}

// primaryTransitionAt builds the transition for a primary the caller has
// already located, refusing one the caller never constructed.
func primaryTransitionAt(item Item, index int, available func(string) bool) (Transition, bool) {
	if !constructed(available, item.Name) {
		return Transition{}, false
	}
	return Transition{
		Name:          item.Name,
		SelectedIndex: index,
		VisibleChild:  item.Name,
		Title:         item.Title,
		ShowContent:   true,
	}, true
}

// primaryAt returns the primary route named name from the caller's inventory,
// together with its sidebar row index.
//
// The row index is not the item's position in the inventory, and the two must
// not be conflated: a detail is not a row, so it is skipped rather than counted,
// and an inventory that contains one — which VisibleRoutes produces, and which
// a window may hand back — would otherwise select the wrong screen. The item is
// returned alongside the index so no caller has to subscript the inventory with
// a row number to recover it.
func primaryAt(visible []Item, name string) (Item, int, bool) {
	row := 0
	for _, item := range visible {
		if !isPrimary(item) {
			continue
		}
		if item.Name == name {
			return item, row, true
		}
		row++
	}
	return Item{}, 0, false
}

// offered reports whether the caller's inventory contains name at all.
func offered(visible []Item, name string) bool {
	for _, item := range visible {
		if item.Name == name {
			return true
		}
	}
	return false
}

// constructed is the caller's construction seam. A nil predicate vouches for
// nothing, so a caller holding no inventory cannot make a route reachable.
func constructed(available func(string) bool, name string) bool {
	return available != nil && available(name)
}

// anyRefEnabled reports whether at least one of a route's configuration refs
// is enabled. A route that declares no ref is gated off rather than on: the
// canonical inventory is checked by test to hold a ref for every route, so the
// empty case is a caller's construction error and fails closed.
func anyRefEnabled(item Item, enabled func(page, group string) bool) bool {
	if enabled == nil {
		return false
	}
	for _, ref := range item.Refs {
		if enabled(ref.Page, ref.Group) {
			return true
		}
	}
	return false
}

// compact assigns the Alt+number accelerator by sidebar position, over an
// inventory that is already primaries only.
func compact(source []Item) []Item {
	result := make([]Item, len(source))
	for index, item := range source {
		number := strconv.Itoa(index + 1)
		item = clone(item)
		item.Accelerator = "<Alt>" + number
		item.Display = "Alt+" + number
		result[index] = item
	}
	return result
}

// clone copies a route's slice so a returned inventory never aliases canonical
// storage.
func clone(item Item) Item {
	item.Refs = append([]Ref(nil), item.Refs...)
	return item
}
