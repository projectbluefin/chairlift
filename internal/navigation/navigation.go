// Package navigation owns ChairLift's canonical route inventory — the five
// task destinations, the primary sidebar mounts, and the detail views reached
// from them — plus the widget-free state transition for selecting a route.
package navigation

import "strconv"

// Shortcut groups used by the shortcuts dialog.
const (
	GroupNavigation = "navigation"
	GroupGeneral    = "general"
)

// Kind classifies a route.
type Kind string

const (
	// Primary is a destination that appears in the sidebar.
	Primary Kind = "primary"
	// Detail is a focused screen reached from a primary. A detail route is
	// not a sidebar entry and carries no Alt+number accelerator: detail
	// views are reached from the primary that owns them, never from their
	// own numeric slot (#233, #201 step 3).
	Detail Kind = "detail"
)

// Destination is one of the five canonical task destinations the product is
// organized around (#233). A destination is not itself navigable — routes
// are — but every route names the destination it belongs to, which is what
// makes a detail's Back target and its sidebar ancestor unambiguous.
type Destination struct {
	Name  string
	Title string
	Icon  string
}

// Destinations returns the canonical task destinations in the order #201
// mounts them. The slice is freshly allocated on every call.
func Destinations() []Destination {
	return append([]Destination(nil), destinations...)
}

// Ref names one configuration namespace a rendered route consumes. A route
// holds a slice of them because a destination may be composed from several
// pages' groups — Updates consumes both updates_page and system_page — and
// configuration identity is never inferred from a route's display name. This
// is the retired assumption: a route is no longer one config page.
type Ref struct {
	Page  string
	Group string
}

// Route is one navigable destination: a primary sidebar mount or a detail
// view reached from one.
type Route struct {
	Name   string
	Title  string
	Icon   string
	Kind   Kind
	Serves []string // canonical destinations this route belongs to
	Refs   []Ref    // configuration namespaces the rendered route consumes

	Accelerator string
	Display     string

	// AlwaysShow keeps a route in the sidebar even when every one of its
	// groups is disabled. Help carries it so the window always has a valid
	// destination.
	AlwaysShow bool

	// Fallback marks the route a disabled or unsupported destination lands
	// on. Exactly one route carries it.
	Fallback bool

	// Temporary marks a working mount the #201 cutover deletes rather than
	// carries forward. Every temporary route is named in the cutover
	// section of docs/design/navigation-routes.md.
	Temporary bool
}

// Transition is the complete UI state change for navigating to one route.
type Transition struct {
	// Name is the route actually selected. It differs from the requested
	// route when a disabled or unconstructed one fell back to a safe
	// ancestor.
	Name          string
	SelectedIndex int
	VisibleChild  string
	Title         string
	ShowContent   bool

	// Back is the primary route a detail view returns to. It is empty for a
	// primary transition.
	Back   string
	Detail bool
}

// Env is the pure environment a resolution consults. Visible holds the
// constructed, configuration-enabled primary routes in sidebar order;
// Available reports whether the caller built a widget for a route; Enabled
// reports whether a configuration group is on. Every field is a seam, so a
// test drives the same resolution the window does without GTK.
type Env struct {
	Visible   []Route
	Available func(name string) bool
	Enabled   func(page, group string) bool
}

var destinations = []Destination{
	{Name: "updates", Title: "Updates", Icon: "software-update-available-symbolic"},
	{Name: "apps", Title: "Apps", Icon: "application-x-executable-symbolic"},
	{Name: "appearance", Title: "Appearance", Icon: "preferences-desktop-appearance-symbolic"},
	{Name: "system", Title: "System", Icon: "computer-symbolic"},
	{Name: "help", Title: "Help", Icon: "help-browser-symbolic"},
}

var routes = []Route{
	// Working primary mounts. #233's five destination inventory replaces
	// this list at the #201 cutover: applications becomes Apps, livery
	// becomes Appearance, and maintenance and features are deleted with
	// their content redistributed to the detail routes below. Until then
	// these mounts keep the window usable, which is the whole point of
	// landing the seam before the cutover.
	{
		Name:   "applications",
		Title:  "Applications",
		Icon:   "application-x-executable-symbolic",
		Kind:   Primary,
		Serves: []string{"apps"},
		Refs: on("applications_page",
			"applications_installed_group",
			"flatpak_user_group",
			"flatpak_system_group",
			"brew_group",
			"brew_search_group",
			"brew_bundles_group",
		),
	},
	{
		Name:      "maintenance",
		Title:     "Maintenance",
		Icon:      "emblem-system-symbolic",
		Kind:      Primary,
		Serves:    []string{"system"},
		Refs:      on("maintenance_page", "maintenance_cleanup_group", "maintenance_brew_group", "maintenance_flatpak_group", "maintenance_optimization_group", "reset_group"),
		Temporary: true,
	},
	{
		Name:   "updates",
		Title:  "Updates",
		Icon:   "software-update-available-symbolic",
		Kind:   Primary,
		Serves: []string{"updates"},
		Refs: on("updates_page",
			"update_all_group",
			"bootc_updates_group",
			"sysupdate_updates_group",
			"flatpak_updates_group",
			"brew_updates_group",
			"brew_trust_group",
		),
	},
	{
		Name:   "system",
		Title:  "System",
		Icon:   "computer-symbolic",
		Kind:   Primary,
		Serves: []string{"system"},
		Refs:   on("system_page", "system_info_group", "bootc_status_group", "channel_group", "health_group"),
	},
	{
		Name:      "features",
		Title:     "Features",
		Icon:      "application-x-addon-symbolic",
		Kind:      Primary,
		Serves:    []string{"apps", "system", "help"},
		Refs:      on("features_page", "features_group", "dx_group", "gaming_group", "ai_group", "troubleshooting_group"),
		Temporary: true,
	},
	{
		Name:   "livery",
		Title:  "Livery",
		Icon:   "preferences-desktop-appearance-symbolic",
		Kind:   Primary,
		Serves: []string{"appearance"},
		Refs:   on("livery_page", "livery_app_grid_group", "livery_foundation_group", "livery_dock_group"),
	},
	{
		Name:       "help",
		Title:      "Help",
		Icon:       "help-browser-symbolic",
		Kind:       Primary,
		Serves:     []string{"help"},
		Refs:       on("help_page", "help_resources_group"),
		AlwaysShow: true,
		Fallback:   true,
	},

	// Detail routes. Each one owns content that exists today under a
	// primary mount, and none is reachable until the ticket that builds its
	// widget mounts it: Resolve falls back to the visible ancestor, so a
	// declared-but-unconstructed detail is never an empty screen. Titles
	// follow #233's information architecture rather than inventing a
	// parallel vocabulary.
	{
		Name:   "update-settings",
		Title:  "Update Settings",
		Kind:   Detail,
		Serves: []string{"updates"},
		// The one cross-page detail, and the reason refs exist: the
		// Automatic Updates switch is drawn inside update_all_group, while
		// early-release selection is the channel control that configures
		// the system's image. Naming either page after this route's title
		// would be exactly the inference Refs replace.
		Refs: []Ref{
			{Page: "updates_page", Group: "update_all_group"},
			{Page: "system_page", Group: "channel_group"},
		},
	},
	{
		Name:   "update-sources",
		Title:  "Update Sources",
		Kind:   Detail,
		Serves: []string{"updates"},
		Refs: on("updates_page",
			"bootc_updates_group",
			"sysupdate_updates_group",
			"flatpak_updates_group",
			"brew_updates_group",
		),
	},
	{
		Name:   "update-changes",
		Title:  "What's Changing",
		Kind:   Detail,
		Serves: []string{"updates"},
		Refs:   on("updates_page", "bootc_updates_group", "sysupdate_updates_group"),
	},
	{
		Name:   "trust",
		Title:  "Trust Decisions",
		Kind:   Detail,
		Serves: []string{"updates"},
		Refs:   on("updates_page", "brew_trust_group"),
	},
	{
		Name:   "gaming",
		Title:  "Gaming Setup",
		Kind:   Detail,
		Serves: []string{"apps"},
		Refs:   on("features_page", "gaming_group"),
	},
	{
		Name:   "developer-tools",
		Title:  "Developer Tools",
		Kind:   Detail,
		Serves: []string{"apps"},
		// The second cross-page detail: the specialist Homebrew controls
		// live on applications_page while developer access is a
		// features_page group. #233 places both here rather than inventing
		// a Developer primary.
		Refs: []Ref{
			{Page: "applications_page", Group: "brew_group"},
			{Page: "applications_page", Group: "brew_search_group"},
			{Page: "applications_page", Group: "brew_bundles_group"},
			{Page: "features_page", Group: "dx_group"},
		},
	},
	{
		Name:   "ai-tools",
		Title:  "AI Tools",
		Kind:   Detail,
		Serves: []string{"apps"},
		Refs:   on("features_page", "ai_group"),
	},
	{
		Name:   "icons",
		Title:  "Icons",
		Kind:   Detail,
		Serves: []string{"appearance"},
		Refs:   on("livery_page", "livery_app_grid_group", "livery_foundation_group", "livery_dock_group"),
	},
	{
		Name:   "about",
		Title:  "About This Computer",
		Kind:   Detail,
		Serves: []string{"system"},
		Refs:   on("system_page", "system_info_group", "bootc_status_group", "health_group"),
	},
	{
		Name:   "storage",
		Title:  "Storage",
		Kind:   Detail,
		Serves: []string{"system"},
		Refs:   on("maintenance_page", "maintenance_brew_group", "maintenance_flatpak_group"),
	},
	{
		Name:   "graphics",
		Title:  "Graphics",
		Kind:   Detail,
		Serves: []string{"system"},
		Refs:   on("system_page", "channel_group"),
	},
	{
		Name:   "distribution-features",
		Title:  "Distribution Features",
		Kind:   Detail,
		Serves: []string{"system"},
		Refs:   on("features_page", "features_group"),
	},
	{
		Name:   "administrator-maintenance",
		Title:  "Administrator Maintenance",
		Kind:   Detail,
		Serves: []string{"system"},
		Refs:   on("maintenance_page", "maintenance_cleanup_group"),
	},
	{
		Name:   "recovery",
		Title:  "Recovery",
		Kind:   Detail,
		Serves: []string{"system"},
		Refs:   on("maintenance_page", "reset_group"),
	},
	{
		Name:   "troubleshooting",
		Title:  "Troubleshooting",
		Kind:   Detail,
		Serves: []string{"help"},
		Refs:   on("features_page", "troubleshooting_group"),
	},
	{
		Name:   "capability-explanations",
		Title:  "Feature Availability",
		Kind:   Detail,
		Serves: []string{"help"},
		Refs:   on("help_page", "help_resources_group"),
	},
}

// generalShortcuts are the shortcuts that belong to no single route.
var generalShortcuts = []Shortcut{
	{Action: "win.show-shortcuts", Accelerator: "<Primary>question", Display: "Ctrl+?", Title: "Keyboard Shortcuts", Group: GroupGeneral},
	{Action: "app.quit", Accelerator: "<Primary>q", Display: "Ctrl+Q", Title: "Quit", Group: GroupGeneral},
	{Action: "win.navigate-help", Accelerator: "F1", Display: "F1", Title: "Help", Group: GroupGeneral},
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

// Routes returns a copy of the complete canonical route inventory — primary
// mounts and detail routes alike.
func Routes() []Route {
	result := make([]Route, len(routes))
	for index, route := range routes {
		result[index] = cloneRoute(route)
	}
	return result
}

// PrimaryRoutes returns a copy of the primary sidebar inventory, with
// accelerators assigned as though every primary were visible.
func PrimaryRoutes() []Route {
	return compact(primaries())
}

// VisibleRoutes filters the primary inventory using static group
// configuration and compacts Alt+number accelerators over the result. The
// fallback route is always retained so the window always has a valid
// destination.
func VisibleRoutes(enabled func(page, group string) bool) []Route {
	visible := make([]Route, 0, len(routes))
	for _, route := range primaries() {
		if route.AlwaysShow || anyRefEnabled(route, enabled) {
			visible = append(visible, route)
		}
	}
	return compact(visible)
}

// Shortcuts returns the advertised shortcuts for the supplied visible routes
// plus the canonical general shortcuts. Detail routes advertise nothing:
// they are reached from their primary, not from a numeric slot.
func Shortcuts(visible []Route) []Shortcut {
	result := make([]Shortcut, 0, len(visible)+len(generalShortcuts))
	for _, route := range visible {
		result = append(result, Shortcut{
			Action:      "win.navigate-" + route.Name,
			Accelerator: route.Accelerator,
			Display:     route.Display,
			Title:       "Go to " + route.Title,
			Group:       GroupNavigation,
		})
	}
	return append(result, generalShortcuts...)
}

// Bindings groups the shortcuts for the supplied visible routes by action for
// GTK registration.
func Bindings(visible []Route) []Binding {
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
// A known route always lands somewhere safe. An unknown one is rejected
// outright, because there is nothing to guess at and navigating nowhere beats
// navigating wrong. A primary the caller did not construct, or whose groups
// are all disabled, falls back to the fallback route; a detail the caller did
// not construct, or whose every configuration group is disabled, falls back
// to the visible primary that owns its destination. Resolution is pure: it
// returns state to apply and never runs an action.
func Resolve(routeName string, env Env) (Transition, bool) {
	route, known := lookup(routeName)
	if !known {
		return Transition{}, false
	}

	if route.Kind == Primary {
		if transition, ok := primaryTransition(route.Name, env); ok {
			return transition, true
		}
		return fallbackTransition(route.Name, env)
	}

	ancestor, ok := ancestorFor(route, env)
	if !ok {
		// Nothing on screen draws this detail, so it stays unreachable: the
		// deep link lands on the fallback route instead of opening a screen
		// with no way back to what it belongs to.
		return fallbackTransition(route.Name, env)
	}
	transition, ok := primaryTransition(ancestor.Name, env)
	if !ok {
		return Transition{}, false
	}
	if !constructed(route, env) || !anyRefEnabled(route, env.Enabled) {
		// A declared detail with no widget, or one whose content is
		// entirely disabled, must not render an empty screen. Landing on
		// the ancestor keeps a stale deep link or a saved shortcut safe.
		return transition, true
	}
	transition.Name = route.Name
	transition.VisibleChild = route.Name
	transition.Title = route.Title
	transition.Back = ancestor.Name
	transition.Detail = true
	return transition, true
}

// primaryTransition selects a visible, constructed primary route by name.
func primaryTransition(name string, env Env) (Transition, bool) {
	route, known := lookup(name)
	if !known || route.Kind != Primary || !constructed(route, env) {
		return Transition{}, false
	}
	for index, visible := range env.Visible {
		if visible.Name == name {
			return Transition{
				Name:          name,
				SelectedIndex: index,
				VisibleChild:  name,
				Title:         visible.Title,
				ShowContent:   true,
			}, true
		}
	}
	return Transition{}, false
}

// fallbackTransition lands on the fallback route unless the rejected route is
// itself that route, which would be a transition to nowhere.
func fallbackTransition(rejected string, env Env) (Transition, bool) {
	route, ok := fallbackRoute(env)
	if !ok || route.Name == rejected {
		return Transition{}, false
	}
	return primaryTransition(route.Name, env)
}

// ancestorFor returns the visible primary a detail belongs to, and reports
// false when the detail has no reachable mount at all. The ancestor is the
// visible route that draws the detail's content today — the one whose refs
// overlap the detail's own — so Back returns to the screen the user came from
// and follows that content when the #201 cutover moves it. Ties go to sidebar
// order. A detail whose content no visible route draws falls back to the
// first visible primary serving one of its destinations; when there is none,
// the detail is unreachable rather than merely orphaned.
func ancestorFor(route Route, env Env) (Route, bool) {
	best := Route{}
	bestOverlap := 0
	for _, visible := range env.Visible {
		if visible.Kind != Primary || !constructed(visible, env) {
			continue
		}
		if overlap := refOverlap(visible, route); overlap > bestOverlap {
			best, bestOverlap = visible, overlap
		}
	}
	if bestOverlap > 0 {
		return best, true
	}
	for _, destination := range route.Serves {
		for _, visible := range env.Visible {
			if visible.Kind == Primary && serves(visible, destination) && constructed(visible, env) {
				return visible, true
			}
		}
	}
	return Route{}, false
}

// refOverlap counts the configuration namespaces two routes share.
func refOverlap(a, b Route) int {
	shared := make(map[Ref]bool, len(a.Refs))
	for _, ref := range a.Refs {
		shared[ref] = true
	}
	count := 0
	for _, ref := range b.Refs {
		if shared[ref] {
			count++
		}
	}
	return count
}

// fallbackRoute returns the visible route marked as the fallback destination.
func fallbackRoute(env Env) (Route, bool) {
	for _, visible := range env.Visible {
		if visible.Fallback {
			return visible, true
		}
	}
	return Route{}, false
}

// constructed reports whether the caller built a widget for the route. A nil
// seam fails closed: a caller that cannot say what it built has vouched for
// nothing, so an unset predicate can never make a route reachable.
func constructed(route Route, env Env) bool {
	if env.Available == nil {
		return false
	}
	return env.Available(route.Name)
}

func serves(route Route, destination string) bool {
	for _, name := range route.Serves {
		if name == destination {
			return true
		}
	}
	return false
}

func lookup(name string) (Route, bool) {
	for _, route := range routes {
		if route.Name == name {
			return route, true
		}
	}
	return Route{}, false
}

func primaries() []Route {
	result := make([]Route, 0, len(routes))
	for _, route := range routes {
		if route.Kind == Primary {
			result = append(result, route)
		}
	}
	return result
}

// anyRefEnabled reports whether any configuration group a route consumes is
// enabled. A route that declares no refs is not gated by configuration.
func anyRefEnabled(route Route, enabled func(page, group string) bool) bool {
	if enabled == nil {
		return false
	}
	for _, ref := range route.Refs {
		if enabled(ref.Page, ref.Group) {
			return true
		}
	}
	return false
}

// on builds the refs of one configuration page's groups, in the order given.
func on(page string, groups ...string) []Ref {
	result := make([]Ref, 0, len(groups))
	for _, group := range groups {
		result = append(result, Ref{Page: page, Group: group})
	}
	return result
}

func compact(source []Route) []Route {
	result := make([]Route, len(source))
	for index, route := range source {
		number := strconv.Itoa(index + 1)
		route = cloneRoute(route)
		route.Accelerator = "<Alt>" + number
		route.Display = "Alt+" + number
		result[index] = route
	}
	return result
}

func cloneRoute(route Route) Route {
	route.Refs = append([]Ref(nil), route.Refs...)
	route.Serves = append([]string(nil), route.Serves...)
	return route
}
