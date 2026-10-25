// Package window provides the main application window
package window

import (
	"context"
	"fmt"
	"log"
	"time"
	"unsafe"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/capability"
	"github.com/projectbluefin/chairlift/internal/config"
	"github.com/projectbluefin/chairlift/internal/firstrun"
	"github.com/projectbluefin/chairlift/internal/navigation"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/settings"
	"github.com/projectbluefin/chairlift/internal/ui"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/updateproviders"
	"github.com/projectbluefin/chairlift/internal/version"
	"github.com/projectbluefin/chairlift/internal/views"
	"github.com/projectbluefin/chairlift/internal/views/updatepresent"

	"github.com/frostyard/snowkit/gobj"
	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

var (
	gTypeWindow    gobject.Type
	windowRegistry *gobj.InstanceRegistry
)

// notificationID identifies ChairLift's one notification stream. Reusing a
// fixed ID (rather than a random one per send) means a later background run
// replaces the desktop's still-visible notification from an earlier one
// instead of stacking a second, matching how a single Updates row already
// represents "the current state," not a log of past runs.
const notificationID = "io.projectbluefin.chairlift.background-task"

// Window represents the main application window
type Window struct {
	adw.ApplicationWindow
	ui windowUI // The bound internal/ui/window.blp tree

	// Built on first use and re-presented; see onShowShortcuts.
	shortcutsDialog *adw.Dialog
	aboutDialog     *adw.Dialog

	splitView    *adw.NavigationSplitView
	sidebarList  *gtk.ListBox
	shownRow     int // Sidebar index of the shown page; the only row the selection may rest on
	contentStack *gtk.Stack
	contentPage  *adw.NavigationPage // Content navigation page for dynamic title
	toasts       *adw.ToastOverlay

	pages           map[string]*adw.ToolbarView
	navRows         map[string]*gtk.ListBoxRow // Store references to nav rows for badges
	config          *config.Config
	capabilities    capability.Set // Resolved once; the policy floor for every entry path
	configError     *config.LoadError
	views           *views.UserHome
	updateShell     *views.UpdateShell
	updateSettings  *settings.Store // The one updates-preference store: the shell reads it, Preferences binds to it
	firstRunSteps   []string
	firstRunIndex   int
	firstRunActive  bool
	narrow          bool // The narrow breakpoint is applied; see syncCollapsed
	firstRunContent *gtk.Stack
	firstRunFooter  *adw.ToolbarView // Reveals the setup flow's bottom bar
	firstRunBack    *gtk.Button
	firstRunNext    *gtk.Button
	updateBadge     *gtk.Label        // Noninteractive badge for the updates count
	navItems        []navigation.Item // Visible primaries: the sidebar rows, actions, and shortcuts
	navRoutes       []navigation.Item // navItems plus the details they offer; the Resolve inventory
	backRoute       string            // The primary the shown detail's Back returns to; "" on a primary
}

func init() {
	gTypeWindow, windowRegistry = gobj.RegisterType(gobj.TypeDef{
		ParentGLibType: adw.ApplicationWindowGLibType,
		ClassName:      "ChairLiftWindow",
		ClassInit: func(tc *gobject.TypeClass, reg *gobj.InstanceRegistry) {
			objClass := (*gobject.ObjectClass)(unsafe.Pointer(tc))
			objClass.OverrideConstructed(func(o *gobject.Object) {
				windowStart := time.Now()

				parentObjClass := (*gobject.ObjectClass)(unsafe.Pointer(tc.PeekParent()))
				parentObjClass.GetConstructed()(o)

				var parent adw.ApplicationWindow
				o.Cast(&parent)

				cfgStart := time.Now()
				cfg, configErr := config.Load()
				log.Printf("window: config loaded in %s", time.Since(cfgStart))

				w := &Window{
					ApplicationWindow: parent,
					pages:             make(map[string]*adw.ToolbarView),
					navRows:           make(map[string]*gtk.ListBoxRow),
					config:            cfg,
					configError:       configErr,
				}

				reg.Pin(o, unsafe.Pointer(w))

				w.SetDefaultSize(900, 700)
				// The HIG's smallest supported window: the narrow
				// breakpoint collapses the split view well above it.
				w.SetSizeRequest(360, 294)
				w.SetTitle(branding.AppName)
				w.buildUI()
				if w.configError != nil {
					// OverrideConstructed and buildUI both run on GTK's main
					// thread; the toast overlay now exists and timeout 0 makes
					// this diagnostic persist until dismissed.
					w.ShowErrorToast(w.configError.ToastMessage())
				}
				w.setupActions()

				log.Printf("window: constructed in %s", time.Since(windowStart))
			})
		},
	})
}

// New creates a new main window
func New(app adw.Application) *Window {
	obj := gobject.NewObject(gTypeWindow, "application", app.Application.GoPointer(), uintptr(0))
	if obj == nil {
		log.Fatal("Failed to create window")
	}
	return (*Window)(windowRegistry.Get(obj.GoPointer()))
}

// effectiveEnabled is the one policy floor shared by every entry path: the
// navigation sidebar and the first-run assistant route their group predicate
// through it, and the view builders compose the same predicate from the same
// capability set (UserHome.groupEnabled). A group is enabled only when its
// configuration enables it AND the host supports it; the capability set was
// resolved once during window construction and is immutable for the session.
// The update coordinator's source map uses sourcePolicy in buildUI instead,
// which reports the same two predicates separately so a source the host
// cannot back reads as unavailable rather than disabled by the administrator;
// their conjunction is exactly this function. A nil capability set satisfies
// no group that has a prerequisite, matching the repository's fail-closed
// rule. See internal/capability.
func (w *Window) effectiveEnabled(page, group string) bool {
	return capability.Compose(w.config.IsGroupEnabled, w.capabilities)(page, group)
}

// buildUI constructs the window UI
func (w *Window) buildUI() {
	start := time.Now()

	// Resolve the capability set once, before any surface reads it. Detection
	// is cheap (LookPath/Stat/env) and safe on the main thread; the result is
	// immutable for the session and is the single policy floor every entry
	// path routes through. See internal/capability.
	w.capabilities = capability.Detect()

	w.navItems = navigation.VisibleItems(w.effectiveEnabled)
	w.navRoutes = navigation.VisibleRoutes(w.effectiveEnabled)

	// Initialize unified updates engine. The shell is built before the views
	// because its preferences page is the Updates destination's only page:
	// views.New appends the Updates page's own groups to it.
	providers := []updateflow.Provider{
		updateproviders.NewFlatpak(),
		updateproviders.NewHomebrew(),
		updateproviders.NewSystemComponents(),
		updateproviders.NewOperatingSystem(),
	}
	coordinator := updateflow.New(providers, updateproviders.NewMaintenance(w.config))
	store := settings.New()
	w.updateSettings = store
	// Each source's policy keeps the administrator's configuration and the
	// capability floor apart, so the shell can tell "disabled by
	// administrator" from "not available on this system". Their conjunction
	// is exactly effectiveEnabled.
	sourcePolicy := func(page, group string) updateflow.Policy {
		return updateflow.Policy{
			Configured: w.config.IsGroupEnabled(page, group),
			Supported:  w.capabilities.Supports(page, group),
		}
	}
	w.updateShell = views.NewUpdateShell(
		coordinator,
		store.Values,
		func() map[updateflow.SourceID]updateflow.Policy {
			return map[updateflow.SourceID]updateflow.Policy{
				updateflow.OperatingSystem:  sourcePolicy("updates_page", "bootc_updates_group"),
				updateflow.Applications:     sourcePolicy("updates_page", "flatpak_updates_group"),
				updateflow.DeveloperTools:   sourcePolicy("updates_page", "brew_updates_group"),
				updateflow.SystemComponents: sourcePolicy("features_page", "features_group"),
			}
		},
		w,
	)

	// Create views manager
	w.views = views.New(w.config, w.capabilities, w, w.updateShell.PreferencesPage())
	// Wire the Recovery detail navigation before any page can open it. Both
	// directions go through navigateToPage: the Maintenance row activates the
	// detail route and Recovery's back button returns to the primary the
	// transition named, so neither callback derives a route of its own.
	w.views.SetOpenRecoveryDetail(w.showRecoveryDetail)
	w.views.SetCloseRecoveryDetail(w.navigateBack)
	log.Printf("window: views built in %s", time.Since(start))

	w.updateShell.SetOnUpdateFinished(w.views.OnUpdateFinished)
	// Connected once, here: a source toggled in Preferences (or by
	// gsettings) re-checks the shell instead of waiting for Refresh.
	store.OnSourceChanged(w.updateShell.PreferencesChanged)
	w.views.AttachUpdateShell(w.updateShell)

	// The window's widget tree is internal/ui/window.blp.
	ui.Bind(&w.ui)
	w.splitView = w.ui.SplitView
	w.toasts = w.ui.Toasts
	w.firstRunContent = w.ui.SetupStack
	w.bindSidebar()
	w.bindContentArea()
	w.ui.SetupStack.AddNamed(w.buildFirstRunBookend(
		"Welcome to",
		"Your system is set up and ready to go. Customize your developer tools, set up troubleshooting, and change other system defaults.",
		"Be the one who moves, not the one who is moved.", "Zavala"), "welcome")
	w.ui.SetupStack.AddNamed(w.buildFirstRunBookend(
		"You Are Ready",
		"You can return to this app at any time by choosing "+branding.AppName+" in your app menu.",
		"There are two ways of spreading light: to be the candle or the mirror that reflects it.", "Edith Wharton"), "conclusion")
	w.bindFirstRunFooter()

	// The product name has one owner, so the About item is added here rather
	// than spelled in the blueprint.
	w.ui.AppSection.Append("_About "+branding.AppName, "win.show-about")

	// Collapse the split view on a narrow window, as GNOME Settings does.
	// The breakpoint only reports the width; syncCollapsed owns the
	// property, because the setup flow collapses the split view too and a
	// breakpoint setter would snapshot and later restore the setup flow's
	// value. Connected once, here.
	narrowApplied := func(adw.Breakpoint) {
		w.narrow = true
		w.syncCollapsed()
	}
	narrowUnapplied := func(adw.Breakpoint) {
		w.narrow = false
		w.syncCollapsed()
	}
	w.ui.NarrowBreakpoint.ConnectApply(&narrowApplied)
	w.ui.NarrowBreakpoint.ConnectUnapply(&narrowUnapplied)
	w.AddBreakpoint(w.ui.NarrowBreakpoint)

	// Set window content
	w.SetContent(&w.toasts.Widget)
}

// syncCollapsed is the one writer of the split view's collapsed property:
// collapsed on a narrow window (internal/ui/window.blp's narrow_breakpoint)
// and while the setup flow, which hides the sidebar, runs.
func (w *Window) syncCollapsed() {
	w.splitView.SetCollapsed(w.narrow || w.firstRunActive)
}

// bindSidebar fills the blueprint's navigation list and connects its two
// handlers, once.
func (w *Window) bindSidebar() {
	w.sidebarList = w.ui.SidebarList
	w.ui.SidebarPage.SetTitle(branding.AppName)

	// Add navigation items
	for _, item := range w.navItems {
		row := w.createNavRow(item)
		w.sidebarList.Append(&row.Widget)
	}

	// Connect row activation
	rowActivatedCb := func(listbox gtk.ListBox, rowPtr uintptr) {
		// Convert uintptr to ListBoxRow
		row := gtk.ListBoxRowNewFromInternalPtr(rowPtr)
		w.onSidebarRowActivated(*row)
	}
	w.sidebarList.ConnectRowActivated(&rowActivatedCb)

	// GtkListBox selects every row that receives keyboard focus: Tab and the
	// arrow keys move the selection row by row without activating anything,
	// so the highlight would name a page the content does not show. The
	// selection belongs to navigateToPage alone; any other change is undone
	// here, which leaves the focus ring where the user moved it and lets
	// Return (row-activated) navigate as before.
	rowSelectedCb := func(_ gtk.ListBox, rowPtr uintptr) {
		if rowPtr != 0 && gtk.ListBoxRowNewFromInternalPtr(rowPtr).GetIndex() == int32(w.shownRow) {
			return
		}
		if row := w.sidebarList.GetRowAtIndex(int32(w.shownRow)); row != nil {
			w.sidebarList.SelectRow(row)
		}
	}
	w.sidebarList.ConnectRowSelected(&rowSelectedCb)
}

// createNavRow binds one sidebar row from internal/ui/nav-row.blp, the
// shape GNOME Settings gives its panel rows.
func (w *Window) createNavRow(item navigation.Item) *gtk.ListBoxRow {
	var view navRowUI
	ui.Bind(&view)
	view.Icon.SetFromIconName(item.Icon)
	view.Title.SetLabel(item.Title)
	row := view.Row

	// Add badge for updates row (hidden by default)
	if item.Name == "updates" {
		w.updateBadge = gtk.NewLabel("")
		w.updateBadge.AddCssClass("circular")
		w.updateBadge.AddCssClass("warning")
		w.updateBadge.SetValign(gtk.AlignCenterValue)
		w.updateBadge.SetVisible(false)
		view.Content.Append(&w.updateBadge.Widget)
	}

	// Store the page name in the row (using SetName for identification)
	row.SetName(item.Name)

	// Store reference to the row
	w.navRows[item.Name] = row

	return row
}

// bindContentArea adds every built page to the blueprint's content stack.
func (w *Window) bindContentArea() {
	w.contentStack = w.ui.ContentStack
	w.contentPage = w.ui.ContentPage
	// Add pages to the stack
	items := w.navItems
	for _, item := range items {
		page := w.views.GetPage(item.Name)
		if page != nil {
			w.pages[item.Name] = page
		}
		if item.Name == "updates" && w.updateShell != nil && w.updateShell.Widget() != nil {
			// The Updates destination is the status-first shell, whose one
			// preferences page also holds everything else the Updates page
			// owns. See UpdateShell.PreferencesPage.
			w.pages[item.Name] = w.updateShell.ToolbarView()
			w.contentStack.AddNamed(w.updateShell.Widget(), item.Name)
			continue
		}
		if page != nil {
			w.contentStack.AddNamed(&page.Widget, item.Name)
		}
	}

	// Recovery is a detail reached from Maintenance, not a sidebar page, so it
	// is not in navItems. It is a content-stack sibling of the primaries and
	// is registered as constructed so navigateToPage can enter it whenever
	// navRoutes offers it; Resolve keeps the Maintenance row selected while it
	// is shown. See #241.
	if recovery := w.views.RecoveryPage(); recovery != nil {
		w.pages["recovery"] = recovery
		w.contentStack.AddNamed(&recovery.Widget, "recovery")
	}

	// The content page takes its title from the first nav item.
	if len(items) > 0 {
		w.contentPage.SetTitle(items[0].Title)
	}

	// Select first item by default
	if len(items) > 0 {
		firstRow := w.sidebarList.GetRowAtIndex(0)
		if firstRow != nil {
			w.sidebarList.SelectRow(firstRow)
			w.contentStack.SetVisibleChildName(items[0].Name)
		}
	}
}

// onSidebarRowActivated handles sidebar row activation
func (w *Window) onSidebarRowActivated(row gtk.ListBoxRow) {
	// Get the ActionRow from the ListBoxRow
	widget := row.GetChild()
	if widget == nil {
		return
	}

	// Get the name we stored
	name := row.GetName()
	if name == "" {
		return
	}

	w.navigateToPage(name)
}

// setupActions sets up window actions
func (w *Window) setupActions() {
	// Preferences action (win.show-preferences)
	prefsAction := gio.NewSimpleAction("show-preferences", nil)
	prefsActivateCb := func(action gio.SimpleAction, param uintptr) {
		w.onShowPreferences()
	}
	prefsAction.ConnectActivate(&prefsActivateCb)
	w.AddAction(prefsAction)

	// Check action (win.check)
	checkAction := gio.NewSimpleAction("check", nil)
	checkActivateCb := func(action gio.SimpleAction, param uintptr) {
		if w.updateShell != nil {
			w.updateShell.StartCheck()
		}
	}
	checkAction.ConnectActivate(&checkActivateCb)
	w.AddAction(checkAction)

	// Back action (win.go-back): Alt+Left returns from a detail to
	// the primary it was opened from, as from any libadwaita navigation
	// page. It moves nothing while a primary is shown.
	backAction := gio.NewSimpleAction("go-back", nil)
	backActivateCb := func(action gio.SimpleAction, param uintptr) {
		w.navigateBack()
	}
	backAction.ConnectActivate(&backActivateCb)
	w.AddAction(backAction)

	// Close action (win.close): Ctrl+W, through close-request, so the
	// update guard and the setup flow's dismissal still apply.
	closeAction := gio.NewSimpleAction("close", nil)
	closeActivateCb := func(action gio.SimpleAction, param uintptr) {
		w.Close()
	}
	closeAction.ConnectActivate(&closeActivateCb)
	w.AddAction(closeAction)

	// Setup assistant action
	setupAction := gio.NewSimpleAction("show-setup", nil)
	setupActivateCb := func(action gio.SimpleAction, param uintptr) {
		w.PresentFirstRun()
	}
	setupAction.ConnectActivate(&setupActivateCb)
	w.AddAction(setupAction)

	// Show shortcuts action
	shortcutsAction := gio.NewSimpleAction("show-shortcuts", nil)
	shortcutsActivateCb := func(action gio.SimpleAction, param uintptr) {
		w.onShowShortcuts()
	}
	shortcutsAction.ConnectActivate(&shortcutsActivateCb)
	w.AddAction(shortcutsAction)

	// About action
	aboutAction := gio.NewSimpleAction("show-about", nil)
	aboutActivateCb := func(action gio.SimpleAction, param uintptr) {
		w.onShowAbout()
	}
	aboutAction.ConnectActivate(&aboutActivateCb)
	w.AddAction(aboutAction)

	// The setup flow's own close-request handler is connected earlier
	// (buildUI) and stops emission while setup is active, so this
	// navigation is never refused by setup.
	closeRequestCb := func(_ gtk.Window) bool {
		return w.RefuseCloseWhileUpdating()
	}
	w.ConnectCloseRequest(&closeRequestCb)

	// Navigation actions
	for _, item := range w.navItems {
		itemName := item.Name // Capture for closure
		action := gio.NewSimpleAction("navigate-"+itemName, nil)
		navActivateCb := func(action gio.SimpleAction, param uintptr) {
			w.navigateToPage(itemName)
		}
		action.ConnectActivate(&navActivateCb)
		w.AddAction(action)
	}
}

// navigateToPage applies the complete navigation.Resolve transition for a
// route: the visible sidebar row, the content child, the title, and the
// collapsed-layout reveal. It is the only method that moves the sidebar
// selection, and the only one that shows a detail: a detail keeps its
// ancestor's row selected and records the primary its Back control returns to.
func (w *Window) navigateToPage(pageName string) {
	if w.firstRunActive && (w.firstRunIndex < 0 || w.firstRunIndex >= len(w.firstRunSteps) || pageName != w.firstRunSteps[w.firstRunIndex]) {
		return
	}
	transition, ok := navigation.Resolve(pageName, w.navRoutes, func(name string) bool {
		_, exists := w.pages[name]
		return exists
	})
	if !ok {
		return
	}

	w.shownRow = transition.SelectedIndex
	w.backRoute = transition.Back
	row := w.sidebarList.GetRowAtIndex(int32(transition.SelectedIndex))
	if row != nil {
		w.sidebarList.SelectRow(row)
	}
	w.contentStack.SetVisibleChildName(transition.VisibleChild)
	w.contentPage.SetTitle(transition.Title)
	w.splitView.SetShowContent(transition.ShowContent)
}

// showRecoveryDetail opens the Recovery detail from Maintenance through the
// canonical transition. Resolve enters the detail only when navRoutes offers
// it and the page was built; otherwise it lands on Maintenance itself.
func (w *Window) showRecoveryDetail() {
	w.navigateToPage("recovery")
}

// navigateBack returns from the shown detail to the primary its transition
// named, through the same route as any other navigation. It moves nothing
// while a primary is shown, and it runs no action: a transition carries only
// the state the window applies.
func (w *Window) navigateBack() {
	if w.backRoute == "" {
		return
	}
	w.navigateToPage(w.backRoute)
}

// onShowShortcuts presents the keyboard shortcuts dialog, filled from
// internal/navigation's one shortcut inventory. libadwaita 1.8 brings
// AdwShortcutsDialog, which draws real keycaps and searches; older releases
// cannot parse it, so they get an AdwDialog of the same entries instead.
// The inventory is fixed for the session, so the dialog is built on first
// use and re-presented after: a dialog built per open would be one more
// that nothing frees.
func (w *Window) onShowShortcuts() {
	if w.shortcutsDialog == nil {
		w.shortcutsDialog = w.buildShortcutsDialog()
	}
	w.shortcutsDialog.Present(&w.Widget)
}

func (w *Window) buildShortcutsDialog() *adw.Dialog {
	if ui.SupportsShortcutsDialog() {
		var view shortcutsUI
		ui.Bind(&view)
		for _, shortcut := range navigation.Shortcuts(w.navItems) {
			item := adw.NewShortcutsItem(shortcut.Title, shortcut.Accelerator)
			if shortcut.Group == navigation.GroupNavigation {
				view.Navigation.Add(item)
			} else {
				view.General.Add(item)
			}
		}
		return &view.Dialog.Dialog
	}

	var view legacyShortcutsUI
	ui.Bind(&view)
	for _, shortcut := range navigation.Shortcuts(w.navItems) {
		row := adw.NewActionRow()
		row.SetTitle(shortcut.Title)
		label := gtk.NewLabel(shortcut.Display)
		label.AddCssClass("dim-label")
		row.AddSuffix(&label.Widget)
		if shortcut.Group == navigation.GroupNavigation {
			view.Navigation.Add(&row.Widget)
		} else {
			view.General.Add(&row.Widget)
		}
	}
	return view.Dialog
}

// NavigationItems returns the visible, compacted page inventory used by this
// window. The application uses the same inventory to register accelerators.
func (w *Window) NavigationItems() []navigation.Item {
	return append([]navigation.Item(nil), w.navItems...)
}

// onShowAbout presents the About dialog. Its static credits live in
// internal/ui/about.blp; the name and version are set here.
// Built on first use and re-presented, like the shortcuts dialog.
func (w *Window) onShowAbout() {
	if w.aboutDialog == nil {
		var view aboutUI
		ui.Bind(&view)
		view.Dialog.SetApplicationName(branding.AppName)
		view.Dialog.SetVersion(version.Version)
		w.aboutDialog = &view.Dialog.Dialog
	}
	w.aboutDialog.Present(&w.Widget)
}

// AddToast adds a toast notification
func (w *Window) AddToast(toast *adw.Toast) {
	w.toasts.AddToast(toast)
}

// ShowToast shows a simple toast message
func (w *Window) ShowToast(message string) {
	toast := adw.NewToast(message)
	toast.SetUseMarkup(false)
	toast.SetTimeout(3)
	toast.SetPriority(adw.ToastPriorityHighValue)
	w.AddToast(toast)
}

// errorToastWidthChars caps how wide the error toast's wrapping label asks to
// be. AdwToast's own title is a single ellipsized line, which is fine for the
// short confirmations ShowToast carries but not for a failure message that
// quotes a command's own error text: issue #140 reported a bundle install
// failure whose toast read "Brew command failed: Installing io.podman_des…",
// hiding the actual cause. A wrapping custom title shows the whole message,
// and the message itself is length-bounded at the source.
const errorToastWidthChars = 48

// ShowErrorToast shows an error toast immediately, keeping older errors queued
// until dismissed. It wraps the failing command's diagnosis instead of hiding
// subsequent failures behind an indefinitely displayed earlier toast.
//
// A message carrying pkexec's dismissal text is not an error: the user
// cancelled the authentication prompt. Every privileged view reports through
// here, so this is the one place that turns it into a brief, self-expiring
// toast; the caller has already restored its control.
//
// The toast is still constructed with the message as its plain title.
// adw_toast_set_custom_title clears that title itself, so this costs nothing,
// and it keeps the message visible rather than blank should the custom title
// ever fail to apply.
func (w *Window) ShowErrorToast(message string) {
	if pkexec.MessageIsAuthDismissed(message) {
		log.Printf("window: authentication dismissed: %s", message)
		w.ShowToast(pkexec.CancelledMessage)
		return
	}
	toast := adw.NewToast(message)
	toast.SetUseMarkup(false)
	toast.SetTimeout(0) // Persist until dismissed
	toast.SetPriority(adw.ToastPriorityHighValue)
	title := gtk.NewLabel(message)
	title.SetWrap(true)
	title.SetMaxWidthChars(errorToastWidthChars)
	title.SetXalign(0)
	toast.SetCustomTitle(&title.Widget)
	w.AddToast(toast)
}

// NotifyBackground sends a desktop GNotification, for background work long
// enough that a user plausibly stepped away before it finished. See
// internal/notify for what gets sent and why it is deliberately rare.
//
// It requires the window to be attached to a *gtk.Application, which is
// always true once the window is shown — GetApplication returns nil only
// before that, so a nil result is silently skipped rather than treated as an
// error worth surfacing: a missed notification is not worth interrupting the
// operation whose result it was reporting.
func (w *Window) NotifyBackground(title, body string, urgent bool) {
	application := w.GetApplication()
	if application == nil {
		log.Printf("NotifyBackground: no application attached yet, dropping %q", title)
		return
	}

	notification := gio.NewNotification(title)
	notification.SetBody(body)
	if urgent {
		notification.SetPriority(gio.GNotificationPriorityHighValue)
	}
	application.SendNotification(notificationID, notification)
}

// SetUpdateBadge updates the badge on the Updates navigation row
func (w *Window) SetUpdateBadge(count int) {
	if w.updateBadge == nil {
		return
	}

	if count > 0 {
		w.updateBadge.SetLabel(fmt.Sprintf("%d", count))
		// A bare number means nothing read aloud: the badge keeps the
		// number as its name and says what it counts as its description.
		w.updateBadge.UpdateProperty(gtk.AccessiblePropertyDescriptionValue, updatepresent.BadgeDescription(count), -1)
		w.updateBadge.SetVisible(true)
	} else {
		w.updateBadge.SetVisible(false)
	}
}

// PresentFirstRun starts the explicit wizard on the existing page controls.
// A second request presents the current step without resetting its progress.
func (w *Window) PresentFirstRun() {
	w.Present()
	if w.firstRunActive {
		return
	}
	w.firstRunSteps = nil
	for _, name := range firstrun.Pages(w.navItems) {
		if w.pages[name] != nil {
			w.firstRunSteps = append(w.firstRunSteps, name)
		}
	}
	if len(w.firstRunSteps) == 0 {
		return
	}
	w.firstRunIndex = -1
	w.firstRunActive = true
	w.sidebarList.SetSensitive(false)
	// The collapsed split view's native Back goes to the sidebar, not the
	// previous setup step. Disable that route while the wizard owns navigation.
	w.contentPage.SetCanPop(false)
	w.syncCollapsed()
	w.splitView.SetShowContent(true)
	w.firstRunFooter.SetRevealBottomBars(true)
	w.showFirstRunStep()
}

// NavigateToAgentsPage opens the Agent Mode page. It is the GTK half of the
// --agent-mode launch intent: internal/app resolves the decision and calls
// this from either the running-instance (onCommandLine) or cold-start
// (onActivate) path, and the route is the canonical Agents page, so the
// launch intent and the sidebar row navigate the same surface.
func (w *Window) NavigateToAgentsPage() {
	w.navigateToPage(navigation.AgentModeRoute)
}

// NavigateToDXModePage opens the Features page (where the Developer Mode toggle lives).
// It is the GTK half of the --dx-mode launch intent: internal/app resolves the
// decision and calls this from either the running-instance (onCommandLine) or
// cold-start (onActivate) path, and the route is the canonical Features page.
func (w *Window) NavigateToDXModePage() {
	w.navigateToPage(navigation.DXModeRoute)
}

// ShowMissingPrerequisite shows an error toast naming the prerequisite preventing
// Goose Desktop / Ask Bluefin from launching.
func (w *Window) ShowMissingPrerequisite(reason string) {
	if reason != "" {
		w.ShowErrorToast(reason)
	}
}

// CheckFirstRun never probes disposition or presents on ordinary activation.
func (w *Window) CheckFirstRun(explicitSetup bool) {
	if firstrun.ShouldPresent(explicitSetup) {
		w.PresentFirstRun()
	}
}

// bindFirstRunFooter connects the setup flow's bottom bar, once. The bar is
// internal/ui/window.blp's setup_footer: an action bar beneath the whole
// window, revealed only while the flow runs.
func (w *Window) bindFirstRunFooter() {
	w.firstRunFooter = w.ui.SetupToolbar
	w.firstRunBack = w.ui.SetupBack
	w.firstRunNext = w.ui.SetupNext
	back := func(gtk.Button) {
		if w.firstRunActive && w.firstRunIndex >= 0 {
			w.firstRunIndex--
			w.showFirstRunStep()
		}
	}
	next := func(gtk.Button) {
		if !w.firstRunActive {
			return
		}
		if w.firstRunIndex == len(w.firstRunSteps) {
			w.finishFirstRun(true)
			return
		}
		w.firstRunIndex++
		w.showFirstRunStep()
	}
	dismiss := func(gtk.Button) { w.finishFirstRun(false) }
	closeRequest := func(gtk.Window) bool {
		if w.firstRunActive {
			w.finishFirstRun(false)
			return true
		}
		return false
	}
	w.firstRunBack.ConnectClicked(&back)
	w.firstRunNext.ConnectClicked(&next)
	w.ui.SetupDismiss.ConnectClicked(&dismiss)
	w.ConnectCloseRequest(&closeRequest)
	keys := gtk.NewEventControllerKey()
	keyPressed := func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if w.firstRunActive && keyval == uint32(gdk.KEY_Escape) {
			w.finishFirstRun(false)
			return true
		}
		return false
	}
	keys.ConnectKeyPressed(&keyPressed)
	w.AddController(&keys.EventController)
}

func (w *Window) showFirstRunStep() {
	if w.firstRunIndex < 0 {
		w.firstRunContent.SetVisibleChildName("welcome")
	} else if w.firstRunIndex == len(w.firstRunSteps) {
		w.firstRunContent.SetVisibleChildName("conclusion")
	} else {
		step := w.firstRunSteps[w.firstRunIndex]
		w.navigateToPage(step)
		w.views.ScrollPageToTop(step)
		w.firstRunContent.SetVisibleChildName("pages")
	}
	w.firstRunBack.SetSensitive(w.firstRunIndex >= 0)
	if w.firstRunIndex == len(w.firstRunSteps) {
		w.firstRunNext.SetLabel("Finish")
	} else {
		w.firstRunNext.SetLabel("Next")
	}
}

func (w *Window) finishFirstRun(completed bool) {
	if !w.firstRunActive {
		return
	}
	if completed {
		w.firstRunActive = false
		w.firstRunFooter.SetRevealBottomBars(false)
		w.firstRunContent.SetVisibleChildName("pages")
		w.sidebarList.SetSensitive(true)
		w.contentPage.SetCanPop(true)
		w.syncCollapsed()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), firstrun.WriteTimeout)
			defer cancel()
			store := firstrun.NewGSettingsStore()
			if err := store.SetDisposition(ctx, firstrun.DispositionCompleted); err != nil {
				log.Printf("window: recording setup completion failed: %v", err)
				sgtk.RunOnMainThread(func() { w.ShowErrorToast("Couldn't save your setup progress.") })
			}
		}()
		return
	}
	w.firstRunActive = false
	w.firstRunFooter.SetRevealBottomBars(false)
	w.firstRunContent.SetVisibleChildName("pages")
	w.sidebarList.SetSensitive(true)
	w.contentPage.SetCanPop(true)
	w.syncCollapsed()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), firstrun.WriteTimeout)
		defer cancel()
		store := firstrun.NewGSettingsStore()
		_, _, err := firstrun.RecordSkip(ctx, store)
		if err != nil {
			log.Printf("window: recording setup disposition failed: %v", err)
			sgtk.RunOnMainThread(func() { w.ShowErrorToast("Couldn't save your setup progress. Setup may open again next time.") })
		}
	}()
}

func (w *Window) buildFirstRunBookend(eyebrowText, message, quotation, author string) *gtk.Widget {
	var view bookendUI
	ui.Bind(&view)
	view.Eyebrow.SetLabel(eyebrowText)
	view.Message.SetLabel(message)
	view.Message.SetVisible(message != "")
	// The conclusion quote is from projectbluefin.io's Mission section.
	view.Quote.SetLabel("“" + quotation + "”")
	view.Attribution.SetLabel("— " + author)

	wordmark := view.Wordmark
	styleManager := adw.StyleManagerGetDefault()
	lightPath, lightErr := firstrun.AssetPath(firstrun.AssetWordmarkLight)
	darkPath, darkErr := firstrun.AssetPath(firstrun.AssetWordmarkDark)
	if lightErr != nil || darkErr != nil {
		log.Printf("firstrun: loading welcome wordmark: %v, %v", lightErr, darkErr)
	}
	currentPath := ""
	applyWordmark := func() {
		path := lightPath
		if styleManager != nil && styleManager.GetDark() {
			path = darkPath
		}
		if path == currentPath {
			return
		}
		currentPath = path
		wordmark.SetFilename(path)
	}
	applyWordmark()
	if styleManager != nil {
		changed := func(gobject.Object, uintptr) { applyWordmark() }
		styleManager.ConnectNotify(&changed)
	}
	return &view.Bookend.Widget
}

// RefuseCloseWhileUpdating reports whether an update run must keep the window
// open, and if so shows why: the busy banner lives inside the Updates shell,
// so a refusal from any other page used to look like a dead close button.
// The close-request handler and app.quit share it, because
// g_application_quit never emits close-request.
func (w *Window) RefuseCloseWhileUpdating() bool {
	if w.updateShell == nil || !w.updateShell.Busy() {
		return false
	}
	w.navigateToPage("updates")
	w.updateShell.RevealBusyBanner()
	return true
}
