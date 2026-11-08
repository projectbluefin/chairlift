package window

import (
	"github.com/projectbluefin/chairlift/internal/ui"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// The window's blueprint bindings. Each struct names one internal/ui/*.blp;
// internal/installcheck holds every tagged field to an object of the same
// class in that blueprint.

// windowUI is the main window's content tree: internal/ui/window.blp.
type windowUI struct {
	_                ui.File                  `ui:"window"`
	Toasts           *adw.ToastOverlay        `ui:"toasts"`
	SetupToolbar     *adw.ToolbarView         `ui:"setup_toolbar"`
	SetupStack       *gtk.Stack               `ui:"setup_stack"`
	SplitView        *adw.NavigationSplitView `ui:"split_view"`
	SidebarPage      *adw.NavigationPage      `ui:"sidebar_page"`
	SidebarList      *gtk.ListBox             `ui:"sidebar_list"`
	ContentPage      *adw.NavigationPage      `ui:"content_page"`
	ContentStack     *gtk.Stack               `ui:"content_stack"`
	SetupDismiss     *gtk.Button              `ui:"setup_dismiss"`
	SetupBack        *gtk.Button              `ui:"setup_back"`
	SetupNext        *gtk.Button              `ui:"setup_next"`
	NarrowBreakpoint *adw.Breakpoint          `ui:"narrow_breakpoint"`
	AppSection       *gio.Menu                `ui:"app_section"`
	ActivityButton   *gtk.Button              `ui:"activity_button"`
	ActivitySpinner  *gtk.Spinner             `ui:"activity_spinner"`
}

// bookendUI is one first-run bookend: internal/ui/bookend.blp.
type bookendUI struct {
	_           ui.File          `ui:"bookend"`
	Bookend     *adw.ToolbarView `ui:"bookend"`
	Eyebrow     *gtk.Label       `ui:"eyebrow"`
	Wordmark    *gtk.Picture     `ui:"wordmark"`
	Message     *gtk.Label       `ui:"message"`
	Quote       *gtk.Label       `ui:"quote"`
	Attribution *gtk.Label       `ui:"attribution"`
}

// shortcutsUI is the libadwaita 1.8 shortcuts dialog: internal/ui/shortcuts.blp.
type shortcutsUI struct {
	_          ui.File               `ui:"shortcuts"`
	Dialog     *adw.ShortcutsDialog  `ui:"shortcuts_dialog"`
	Navigation *adw.ShortcutsSection `ui:"navigation_section"`
	General    *adw.ShortcutsSection `ui:"general_section"`
}

// legacyShortcutsUI is the shortcuts dialog for older libadwaita:
// internal/ui/shortcuts-legacy.blp.
type legacyShortcutsUI struct {
	_          ui.File               `ui:"shortcuts-legacy"`
	Dialog     *adw.Dialog           `ui:"shortcuts_dialog"`
	Navigation *adw.PreferencesGroup `ui:"navigation_group"`
	General    *adw.PreferencesGroup `ui:"general_group"`
}

// aboutUI is the About dialog: internal/ui/about.blp.
type aboutUI struct {
	_      ui.File          `ui:"about"`
	Dialog *adw.AboutDialog `ui:"about_dialog"`
}

// activityUI is the Activity dialog: internal/ui/activity.blp.
type activityUI struct {
	_         ui.File               `ui:"activity"`
	Dialog    *adw.Dialog           `ui:"dialog"`
	Stack     *gtk.Stack            `ui:"stack"`
	TaskGroup *adw.PreferencesGroup `ui:"task_group"`
}

// navRowUI is one sidebar row: internal/ui/nav-row.blp.
type navRowUI struct {
	_       ui.File         `ui:"nav-row"`
	Row     *gtk.ListBoxRow `ui:"row"`
	Content *gtk.Box        `ui:"content"`
	Icon    *gtk.Image      `ui:"icon"`
	Title   *gtk.Label      `ui:"title"`
}
