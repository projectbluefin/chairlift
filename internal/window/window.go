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
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/updateproviders"
	"github.com/projectbluefin/chairlift/internal/version"
	"github.com/projectbluefin/chairlift/internal/views"

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

	splitView    *adw.NavigationSplitView
	sidebarList  *gtk.ListBox
	shownRow     int // Sidebar index of the shown page; the only row the selection may rest on
	contentStack *gtk.Stack
	contentPage  *adw.NavigationPage // Content navigation page for dynamic title
	toasts       *adw.ToastOverlay

	pages             map[string]*adw.ToolbarView
	navRows           map[string]*adw.ActionRow // Store references to nav rows for badges
	config            *config.Config
	capabilities      capability.Set // Resolved once; the policy floor for every entry path
	configError       *config.LoadError
	views             *views.UserHome
	updateShell       *views.UpdateShell
	firstRunSteps     []string
	firstRunIndex     int
	firstRunActive    bool
	firstRunCollapsed bool
	firstRunFooter    *gtk.Box
	firstRunBack      *gtk.Button
	firstRunNext      *gtk.Button
	updateBadge       *gtk.Label        // Noninteractive badge for the updates count
	navItems          []navigation.Item // Visible primaries: the sidebar rows, actions, and shortcuts
	navRoutes         []navigation.Item // navItems plus the details they offer; the Resolve inventory
	backRoute         string            // The primary the shown detail's Back returns to; "" on a primary
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
					navRows:           make(map[string]*adw.ActionRow),
					config:            cfg,
					configError:       configErr,
				}

				reg.Pin(o, unsafe.Pointer(w))

				w.SetDefaultSize(900, 700)
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

	// Create views manager
	w.views = views.New(w.config, w.capabilities, w)
	// Wire the Recovery detail navigation before any page can open it. Both
	// directions go through navigateToPage: the Maintenance row activates the
	// detail route and Recovery's back button returns to the primary the
	// transition named, so neither callback derives a route of its own.
	w.views.SetOpenRecoveryDetail(w.showRecoveryDetail)
	w.views.SetCloseRecoveryDetail(w.navigateBack)
	log.Printf("window: views built in %s", time.Since(start))

	// Initialize unified updates engine
	providers := []updateflow.Provider{
		updateproviders.NewFlatpak(),
		updateproviders.NewHomebrew(),
		updateproviders.NewSystemComponents(),
		updateproviders.NewOperatingSystem(),
	}
	coordinator := updateflow.New(providers, updateproviders.NewMaintenance(w.config))
	store := settings.New()
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
	w.updateShell.SetOnUpdateFinished(w.views.OnUpdateFinished)
	w.views.AttachUpdateShell(w.updateShell)
	// Create the navigation split view
	w.splitView = adw.NewNavigationSplitView()

	// Create sidebar
	sidebarPage := w.buildSidebar()
	w.splitView.SetSidebar(sidebarPage)

	// Create content area
	contentPage := w.buildContentArea()
	w.splitView.SetContent(contentPage)

	// Create toast overlay for notifications
	w.toasts = adw.NewToastOverlay()
	root := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	w.splitView.SetVexpand(true)
	root.Append(&w.splitView.Widget)
	w.buildFirstRunFooter(root)
	w.toasts.SetChild(&root.Widget)

	// Set window content
	w.SetContent(&w.toasts.Widget)
}

// buildSidebar creates the sidebar navigation
func (w *Window) buildSidebar() *adw.NavigationPage {
	// Create toolbar view for sidebar
	toolbarView := adw.NewToolbarView()

	// Add header bar with menu button
	headerBar := adw.NewHeaderBar()
	headerBar.SetShowEndTitleButtons(false)

	// Create hamburger menu button
	menuButton := w.buildMenuButton()
	headerBar.PackEnd(&menuButton.Widget)

	toolbarView.AddTopBar(&headerBar.Widget)

	// Create scrolled window for the list
	scrolled := gtk.NewScrolledWindow()
	scrolled.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scrolled.SetVexpand(true)

	// Create list box for navigation
	w.sidebarList = gtk.NewListBox()
	views.SetAccessibleLabel(w.sidebarList, "Navigation")
	w.sidebarList.SetSelectionMode(gtk.SelectionSingleValue)
	w.sidebarList.AddCssClass("navigation-sidebar")

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

	scrolled.SetChild(&w.sidebarList.Widget)
	toolbarView.SetContent(&scrolled.Widget)

	// Create navigation page
	navPage := adw.NewNavigationPage(&toolbarView.Widget, branding.AppName)

	return navPage
}

// createNavRow creates a navigation row for the sidebar
func (w *Window) createNavRow(item navigation.Item) *adw.ActionRow {
	row := adw.NewActionRow()
	row.SetTitle(item.Title)
	row.SetActivatable(true)

	// Add icon
	icon := gtk.NewImageFromIconName(item.Icon)
	row.AddPrefix(&icon.Widget)

	// Add badge for updates row (hidden by default)
	if item.Name == "updates" {
		w.updateBadge = gtk.NewLabel("")
		w.updateBadge.AddCssClass("circular")
		w.updateBadge.AddCssClass("warning")
		w.updateBadge.SetVisible(false)
		row.AddSuffix(&w.updateBadge.Widget)
	}

	// Store the page name in the row (using SetName for identification)
	row.SetName(item.Name)

	// Store reference to the row
	w.navRows[item.Name] = row

	return row
}

// buildContentArea creates the content stack
func (w *Window) buildContentArea() *adw.NavigationPage {
	// Create stack for content pages
	w.contentStack = gtk.NewStack()
	w.contentStack.SetTransitionType(gtk.StackTransitionTypeCrossfadeValue)

	// Add pages to the stack
	items := w.navItems
	for _, item := range items {
		page := w.views.GetPage(item.Name)
		if page != nil {
			w.pages[item.Name] = page
		}
		if item.Name == "updates" && w.updateShell != nil && w.updateShell.Widget() != nil {
			// The Updates destination is the status-first shell; everything
			// else the Updates page owns — automatic updates, the system
			// version, per-source groups, and the Advanced controls — mounts
			// beneath its sources. See UpdateShell.SetSecondaryContent.
			w.updateShell.SetSecondaryContent(w.views.UpdatesPreferencesPage())
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

	// Create navigation page with initial title from first nav item
	initialTitle := "Content"
	if len(items) > 0 {
		initialTitle = items[0].Title
	}
	w.contentPage = adw.NewNavigationPage(&w.contentStack.Widget, initialTitle)

	// Select first item by default
	if len(items) > 0 {
		firstRow := w.sidebarList.GetRowAtIndex(0)
		if firstRow != nil {
			w.sidebarList.SelectRow(firstRow)
			w.contentStack.SetVisibleChildName(items[0].Name)
		}
	}

	return w.contentPage
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

// buildMenuButton creates the hamburger menu button
func (w *Window) buildMenuButton() *gtk.MenuButton {
	// Create menu model
	menu := gio.NewMenu()

	// Add menu items
	menu.Append("Preferences", "win.show-preferences")
	menu.Append("Setup Assistant…", "win.show-setup")
	menu.Append("Keyboard Shortcuts", "win.show-shortcuts")
	menu.Append("About "+branding.AppName, "win.show-about")
	// Create menu button
	menuButton := gtk.NewMenuButton()
	menuButton.SetIconName("open-menu-symbolic")
	menuButton.SetMenuModel(&menu.MenuModel)
	menuButton.SetTooltipText("Main Menu")
	views.SetAccessibleLabel(menuButton, "Main Menu")

	return menuButton
}

// setupActions sets up window actions
func (w *Window) setupActions() {
	// Preferences action (win.preferences and win.show-preferences)
	prefsAction := gio.NewSimpleAction("show-preferences", nil)
	prefsActivateCb := func(action gio.SimpleAction, param uintptr) {
		w.onShowPreferences()
	}
	prefsAction.ConnectActivate(&prefsActivateCb)
	w.AddAction(prefsAction)

	winPrefsAction := gio.NewSimpleAction("preferences", nil)
	winPrefsAction.ConnectActivate(&prefsActivateCb)
	w.AddAction(winPrefsAction)

	// Check action (win.check)
	checkAction := gio.NewSimpleAction("check", nil)
	checkActivateCb := func(action gio.SimpleAction, param uintptr) {
		if w.updateShell != nil {
			w.updateShell.StartCheck()
		}
	}
	checkAction.ConnectActivate(&checkActivateCb)
	w.AddAction(checkAction)

	// Help action (win.help)
	helpAction := gio.NewSimpleAction("help", nil)
	helpActivateCb := func(action gio.SimpleAction, param uintptr) {
		group := w.config.GetGroupConfig("help_page", "help_resources_group")
		if group != nil && group.Website != "" {
			website := group.Website
			callback := gio.AsyncReadyCallback(func(_, resultPtr, _ uintptr) {
				_, _ = gio.AppInfoLaunchDefaultForUriFinish(&gio.AsyncResultBase{Ptr: resultPtr})
			})
			gio.AppInfoLaunchDefaultForUriAsync(website, nil, nil, &callback, 0)
		}
	}
	helpAction.ConnectActivate(&helpActivateCb)
	w.AddAction(helpAction)
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

	closeRequestCb := func(_ gtk.Window) bool {
		if w.updateShell == nil || !w.updateShell.Busy() {
			return false
		}
		w.updateShell.RevealBusyBanner()
		return true
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
	if w.firstRunActive && pageName != w.firstRunSteps[w.firstRunIndex] {
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

// onShowShortcuts shows the keyboard shortcuts window
func (w *Window) onShowShortcuts() {
	// Create a dialog to show shortcuts since GtkShortcutsWindow isn't available in puregotk
	dialog := adw.NewWindow()
	dialog.SetTransientFor(&w.Window)
	dialog.SetModal(true)
	dialog.SetTitle("Keyboard Shortcuts")
	dialog.SetDefaultSize(400, 450)

	// Create toolbar view
	toolbarView := adw.NewToolbarView()

	// Add header bar
	headerBar := adw.NewHeaderBar()
	toolbarView.AddTopBar(&headerBar.Widget)

	// Create scrolled window
	scrolled := gtk.NewScrolledWindow()
	scrolled.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scrolled.SetVexpand(true)

	// Create main box
	mainBox := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	mainBox.SetMarginTop(12)
	mainBox.SetMarginBottom(12)
	mainBox.SetMarginStart(12)
	mainBox.SetMarginEnd(12)

	// Create clamp for content width
	clamp := adw.NewClamp()
	clamp.SetMaximumSize(400)

	// Navigation shortcuts group
	navGroup := adw.NewPreferencesGroup()
	navGroup.SetTitle("Navigation")

	for _, shortcut := range navigation.Shortcuts(w.navItems) {
		if shortcut.Group != navigation.GroupNavigation {
			continue
		}
		row := adw.NewActionRow()
		row.SetTitle(shortcut.Title)

		label := gtk.NewLabel(shortcut.Display)
		label.AddCssClass("dim-label")
		row.AddSuffix(&label.Widget)

		navGroup.Add(&row.Widget)
	}

	mainBox.Append(&navGroup.Widget)

	// General shortcuts group
	generalGroup := adw.NewPreferencesGroup()
	generalGroup.SetTitle("General")

	for _, shortcut := range navigation.Shortcuts(w.navItems) {
		if shortcut.Group != navigation.GroupGeneral {
			continue
		}
		row := adw.NewActionRow()
		row.SetTitle(shortcut.Title)

		label := gtk.NewLabel(shortcut.Display)
		label.AddCssClass("dim-label")
		row.AddSuffix(&label.Widget)

		generalGroup.Add(&row.Widget)
	}

	mainBox.Append(&generalGroup.Widget)

	clamp.SetChild(&mainBox.Widget)
	scrolled.SetChild(&clamp.Widget)
	toolbarView.SetContent(&scrolled.Widget)

	dialog.SetContent(&toolbarView.Widget)
	dialog.Present()
}

// NavigationItems returns the visible, compacted page inventory used by this
// window. The application uses the same inventory to register accelerators.
func (w *Window) NavigationItems() []navigation.Item {
	return append([]navigation.Item(nil), w.navItems...)
}

// onShowAbout shows the about dialog
func (w *Window) onShowAbout() {
	about := adw.NewAboutWindow()
	about.SetTransientFor(&w.Window)
	about.SetApplicationName(branding.AppName)
	about.SetApplicationIcon("io.projectbluefin.chairlift")
	about.SetVersion(version.Version)
	about.SetDeveloperName("Project Bluefin")
	about.SetWebsite("https://github.com/projectbluefin/chairlift")
	about.SetIssueUrl("https://github.com/projectbluefin/chairlift/issues")
	about.SetLicenseType(gtk.LicenseGpl30Value)
	about.SetCopyright("© 2024-2026 Project Bluefin")
	about.SetDevelopers([]string{"Brian Ketelsen", "ChairLift Contributors"})
	about.Present()
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

// authCancelledToast replaces a dismissed PolicyKit prompt's error. The
// helper never ran, so there is nothing to diagnose and no raw pkexec
// stderr worth pinning to the window (#492).
const authCancelledToast = "Authentication cancelled"

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
		w.ShowToast(authCancelledToast)
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
	w.firstRunIndex = 0
	w.firstRunActive = true
	w.firstRunCollapsed = w.splitView.GetCollapsed()
	w.sidebarList.SetSensitive(false)
	// The collapsed split view's native Back goes to the sidebar, not the
	// previous setup step. Disable that route while the wizard owns navigation.
	w.contentPage.SetCanPop(false)
	w.splitView.SetCollapsed(true)
	w.splitView.SetShowContent(true)
	w.firstRunFooter.SetVisible(true)
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

func (w *Window) buildFirstRunFooter(root *gtk.Box) {
	w.firstRunFooter = gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	w.firstRunFooter.SetMarginTop(12)
	w.firstRunFooter.SetMarginBottom(12)
	w.firstRunFooter.SetMarginStart(12)
	w.firstRunFooter.SetMarginEnd(12)
	cancel := gtk.NewButtonWithLabel("Dismiss setup")
	w.firstRunBack = gtk.NewButtonWithLabel("Back")
	w.firstRunNext = gtk.NewButtonWithLabel("Next")
	w.firstRunNext.AddCssClass("suggested-action")
	spacer := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	spacer.SetHexpand(true)
	w.firstRunFooter.Append(&cancel.Widget)
	w.firstRunFooter.Append(&spacer.Widget)
	w.firstRunFooter.Append(&w.firstRunBack.Widget)
	w.firstRunFooter.Append(&w.firstRunNext.Widget)
	back := func(gtk.Button) {
		if w.firstRunActive && w.firstRunIndex > 0 {
			w.firstRunIndex--
			w.showFirstRunStep()
		}
	}
	next := func(gtk.Button) {
		if !w.firstRunActive {
			return
		}
		if w.firstRunIndex+1 == len(w.firstRunSteps) {
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
	cancel.ConnectClicked(&dismiss)
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
	w.firstRunFooter.SetVisible(false)
	root.Append(&w.firstRunFooter.Widget)
}

func (w *Window) showFirstRunStep() {
	w.navigateToPage(w.firstRunSteps[w.firstRunIndex])
	w.firstRunBack.SetSensitive(w.firstRunIndex > 0)
	if w.firstRunIndex+1 == len(w.firstRunSteps) {
		w.firstRunNext.SetLabel("Finish")
	} else {
		w.firstRunNext.SetLabel("Next")
	}
}

func (w *Window) finishFirstRun(completed bool) {
	if !w.firstRunActive {
		return
	}
	w.firstRunActive = false
	w.firstRunFooter.SetVisible(false)
	w.sidebarList.SetSensitive(true)
	w.contentPage.SetCanPop(true)
	w.splitView.SetCollapsed(w.firstRunCollapsed)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), firstrun.WriteTimeout)
		defer cancel()
		store := firstrun.NewGSettingsStore()
		var err error
		if completed {
			err = store.SetDisposition(ctx, firstrun.DispositionCompleted)
		} else {
			_, _, err = firstrun.RecordSkip(ctx, store)
		}
		if err != nil {
			sgtk.RunOnMainThread(func() { w.ShowErrorToast(err.Error()) })
		}
	}()
}
