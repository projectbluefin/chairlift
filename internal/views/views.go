// Package views provides the page content for the ChairLift application
package views

import (
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/config"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/sysupdate"
	"github.com/projectbluefin/chairlift/internal/troubleshoot"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/badgestate"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
	"github.com/projectbluefin/chairlift/internal/views/rowset"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// ToastAdder is an interface for adding toasts and notifying about updates
type ToastAdder interface {
	ShowToast(message string)
	ShowErrorToast(message string)
	SetUpdateBadge(count int)
	// NotifyBackground sends a desktop notification, for the one operation
	// (Update All) long enough that the user might not be watching. See
	// internal/notify for what gets sent and why it is only that one
	// operation.
	NotifyBackground(title, body string, urgent bool)
}

// UserHome manages all content pages
type UserHome struct {
	config     *config.Config
	toastAdder ToastAdder

	// Pages (ToolbarViews)
	agentsPage       *adw.ToolbarView
	updatesPage      *adw.ToolbarView
	applicationsPage *adw.ToolbarView
	maintenancePage  *adw.ToolbarView
	featuresPage     *adw.ToolbarView
	liveryPage       *adw.ToolbarView
	helpPage         *adw.ToolbarView
	recoveryPage     *adw.ToolbarView // Recovery detail, reached from System, not a sidebar page

	// PreferencesPages inside each ToolbarView - keep references to prevent GC
	agentsPrefsPage       *adw.PreferencesPage
	updatesPrefsPage      *adw.PreferencesPage
	applicationsPrefsPage *adw.PreferencesPage
	maintenancePrefsPage  *adw.PreferencesPage
	featuresPrefsPage     *adw.PreferencesPage
	liveryPrefsPage       *adw.PreferencesPage
	helpPrefsPage         *adw.PreferencesPage
	recoveryPrefsPage     *adw.PreferencesPage

	// References for dynamic updates
	formulaeExpander       *adw.ExpanderRow
	casksExpander          *adw.ExpanderRow
	outdatedExpander       *adw.ExpanderRow
	searchResultsExpander  *adw.ExpanderRow
	searchEntry            *gtk.SearchEntry
	flatpakUserExpander    *adw.ExpanderRow
	flatpakSystemExpander  *adw.ExpanderRow
	flatpakUpdatesExpander *adw.ExpanderRow
	flatpakUpdateRows      []*adw.ActionRow               // Store references for cleanup
	flatpakUserRows        rowset.Tracker[*adw.ActionRow] // Store references for cleanup
	flatpakSystemRows      rowset.Tracker[*adw.ActionRow] // Store references for cleanup
	formulaeRows           rowset.Tracker[*adw.ActionRow]
	caskRows               rowset.Tracker[*adw.ActionRow]
	searchResultRows       rowset.Tracker[*adw.ActionRow]
	brewBundlesGroup       *adw.PreferencesGroup
	brewTrustGroup         *adw.PreferencesGroup
	brewTrustRows          map[string]*adw.ActionRow
	outdatedRows           rowset.Tracker[*adw.ActionRow]

	// Livery references. liveryState is the last state the page loaded and
	// is what every handler compares against, so a programmatic widget
	// update during restore is recognized as "no change" instead of being
	// replayed as a user action; liverySuppress closes the same window
	// explicitly. See applyLiveryState.
	liveryAppGridGroup    *adw.PreferencesGroup
	liveryAppGridSwitch   *gtk.Switch
	liveryAppGridRow      *adw.ActionRow
	liveryPickerMode      liveryPickerMode
	liveryPanelGroup      *adw.PreferencesGroup
	liveryPanelRow        *adw.ActionRow
	liveryPanelSwitch     *gtk.Switch
	liveryPanelMarkRow    *adw.ActionRow
	liveryPanelRotate     *gtk.Switch
	liveryDockGroup       *adw.PreferencesGroup
	liveryDockSwitch      *gtk.Switch
	liveryPickerDialog    *adw.Dialog
	liveryPickerSearch    *gtk.SearchEntry
	liveryPickerList      *gtk.ListBox
	liveryDockSelectedRow *adw.ActionRow
	liveryDockRotate      *gtk.Switch
	// liveryDockVisible is the result set currently drawn, so the list's one
	// row-activated handler can map a row index back to a project without
	// allocating a callback per row. See refreshLiveryPickerRows.
	liveryDockVisible []pageview.LiveryProjectResult
	liveryState       livery.State
	liverySuppress    bool
	liveryLoaded      bool
	// One gate per section serializes that section's toggle work. Every
	// section's Apply and Clear touch the same mark file, so an off-then-on
	// flip without a gate can land Clear after Apply and leave the switch
	// showing enabled with no mark installed. See liveryToggleGate.
	liveryAppGridGate actionstate.Gate
	liveryPanelGate   actionstate.Gate
	liveryDockGate    actionstate.Gate
	// One serializer per section orders that section's selection work. A
	// selection carries a value, so refusing the second pick would discard
	// it; these queue instead, and a pick that a newer one has already
	// overtaken drops out. Without them two rapid picks can interleave and
	// leave the persisted id naming one mark while the installed icon is
	// another. See liverySelectionWork.
	liveryAppGridWork actionstate.Serializer
	liveryPanelWork   actionstate.Serializer
	liveryDockWork    actionstate.Serializer
	// One serializer covers rotation for both sections, because both rotate
	// switches write the same systemd user unit. Without it a rapid on/off
	// flip can land RemoveRotation before the earlier InstallRotation and
	// leave the unit's presence disagreeing with the persisted keys. See
	// onLiveryRotateToggled.
	liveryRotateWork actionstate.Serializer

	// Automatic background updates
	autoUpdatesRow    *adw.ActionRow
	autoUpdatesSwitch *gtk.Switch

	// bootc update references
	bootcStageExpander *adw.ExpanderRow
	bootcStageBtn      *gtk.Button
	bootcActivityRow   *adw.ActionRow
	bootcLogExpander   *adw.ExpanderRow
	bootcRollbackRow   *adw.ActionRow
	bootcRollbackBtn   *gtk.Button
	bootcRollbackGate  actionstate.Gate

	// native A/B (sysupdate) update references
	sysupdateStageExpander *adw.ExpanderRow
	sysupdateStageBtn      *gtk.Button
	sysupdateActivityRow   *adw.ActionRow
	sysupdateLogExpander   *adw.ExpanderRow
	sysupdateRollbackRow   *adw.ActionRow

	// Bluefin-family (channel / developer mode / gaming) references
	channelGroup    *adw.PreferencesGroup
	channelRow      *adw.ActionRow
	channelSwitch   *gtk.Switch
	developerGroup  *adw.PreferencesGroup
	developerRow    *adw.ActionRow
	developerSwitch *gtk.Switch
	developerGate   actionstate.Gate
	gamingGroup     *adw.PreferencesGroup
	gamingRow       *adw.ActionRow
	gamingSwitch    *gtk.Switch
	driverRow       *adw.ActionRow
	driverButton    *gtk.Button
	driverGate      actionstate.Gate

	// Staged-update changelog (SBOM diff), a drill-down inside
	// bootcStageExpander rather than a page of its own.
	changelogRow      *adw.ActionRow
	changelogButton   *gtk.Button
	changelogSections []*adw.ExpanderRow
	changelogBooted   string
	changelogStaged   string
	changelogGate     actionstate.Gate

	// Enhanced Troubleshooting (help_page troubleshooting_group)
	troubleshootGroup  *adw.PreferencesGroup
	troubleshootRow    *adw.ActionRow
	troubleshootButton *gtk.Button
	troubleshootState  troubleshoot.State
	troubleshootGate   actionstate.Gate

	// Local AI (features_page ai_group)
	aiStackGroup  *adw.PreferencesGroup
	aiStackRow    *adw.ActionRow
	aiStackSwitch *gtk.Switch
	aiStackGate   actionstate.Gate

	// Powerwash / Factory Reset (maintenance_page reset_group)
	powerwashGate    actionstate.Gate
	factoryResetGate actionstate.Gate

	// Features page references
	featuresGroup            *adw.PreferencesGroup
	featuresUnavailableGroup *adw.PreferencesGroup
	featureRows              map[string]*adw.ActionRow

	// Groups with deferred visibility

	// Recovery detail navigation, wired by the window after construction.
	// The System page opens Recovery; Recovery's back button returns to
	// System. Recovery is a detail, not a sidebar page, so it is not part of
	// navigation.Items() — see #241 for the canonical route.
	openRecoveryDetail  func()
	closeRecoveryDetail func()

	// Update badge tracking
	updateCounts badgestate.Counts

	brewRefresh         actionstate.RefreshGate
	searchRefresh       actionstate.RefreshGate
	brewPackagesRefresh actionstate.RefreshGate
	// flatpakPackagesRefresh bounds overlapping Flatpak inventory reloads so
	// only the newest reload may publish. Two uninstalls finishing close
	// together each trigger a reload; without a generation guard an older,
	// slower reload can complete last and re-add a removed row or overwrite
	// a newer status. See chairlift#69.
	flatpakPackagesRefresh actionstate.RefreshGate
	// flatpakUpdatesRefresh bounds overlapping Flatpak *update* inventory
	// reloads. Every completed update and uninstall kicks a reload, so two
	// finishing close together race; without a generation guard the older,
	// slower reload publishes last and re-adds a row that was already updated
	// or overwrites a newer badge count. See chairlift#69.
	flatpakUpdatesRefresh actionstate.RefreshGate
}

// New creates a new UserHome views manager
func New(cfg *config.Config, toastAdder ToastAdder) *UserHome {
	start := time.Now()

	uh := &UserHome{
		config:     cfg,
		toastAdder: toastAdder,
	}

	// Create pages - createPage returns both ToolbarView and PreferencesPage
	uh.agentsPage, uh.agentsPrefsPage = uh.createPage()
	uh.updatesPage, uh.updatesPrefsPage = uh.createPage()
	uh.applicationsPage, uh.applicationsPrefsPage = uh.createPage()
	uh.maintenancePage, uh.maintenancePrefsPage = uh.createPage()
	uh.featuresPage, uh.featuresPrefsPage = uh.createPage()
	uh.liveryPage, uh.liveryPrefsPage = uh.createPage()
	uh.helpPage, uh.helpPrefsPage = uh.createPage()
	uh.recoveryPage, uh.recoveryPrefsPage = uh.createRecoveryPage()

	// Build page content
	uh.buildAgentsPage()
	uh.buildUpdatesPage()
	uh.buildApplicationsPage()
	uh.buildMaintenancePage()
	uh.buildFeaturesPage()
	uh.buildLiveryPage()
	uh.buildHelpPage()
	uh.buildRecoveryPage()

	log.Printf("views: all pages built in %s", time.Since(start))
	return uh
}

// OnUpdateFinished refreshes inventories and changelog state after a non-preview update run.
func (uh *UserHome) OnUpdateFinished(final updateflow.Snapshot) {
	if final.Preview {
		return
	}
	uh.loadFlatpakUpdates()
	uh.loadOutdatedPackages()
	for _, source := range final.CompletedSources {
		if source == updateflow.OperatingSystem {
			go func() {
				if sysupdate.IsNativeABCached() {
					status, err := sysupdate.GetStatus()
					count := 0
					if status.IsStaged() {
						count = 1
					}
					uh.updateCounts.SetObserved(badgestate.Sysupdate, count, err == nil)
					uh.updateBadgeCount()
					return
				}
				ctx, cancel := bootc.DefaultContext()
				defer cancel()
				status, err := bootc.GetStatus(ctx)
				if err == nil {
					sgtk.RunOnMainThread(func() {
						uh.refreshChangelogAvailability(status)
					})
				}
				count := 0
				if status != nil && status.Status.Staged != nil {
					count = 1
				}
				uh.updateCounts.SetObserved(badgestate.Bootc, count, err == nil)
				uh.updateBadgeCount()
			}()
			break
		}
	}
}

// updateBadgeCount updates the total update count and notifies the window
func (uh *UserHome) updateBadgeCount() {
	total := uh.updateCounts.Total()

	sgtk.RunOnMainThread(func() {
		uh.toastAdder.SetUpdateBadge(total)
	})
}

// GetPage returns a page by name
func (uh *UserHome) GetPage(name string) *adw.ToolbarView {
	switch name {
	case "agents":
		return uh.agentsPage
	case "updates":
		return uh.updatesPage
	case "applications":
		return uh.applicationsPage
	case "maintenance":
		return uh.maintenancePage
	case "features":
		return uh.featuresPage
	case "livery":
		return uh.liveryPage
	case "help":
		return uh.helpPage
	default:
		return nil
	}
}

// createPage creates a page with toolbar view and scrolled content
func (uh *UserHome) createPage() (*adw.ToolbarView, *adw.PreferencesPage) {
	toolbarView := adw.NewToolbarView()

	// Add header bar
	headerBar := adw.NewHeaderBar()
	toolbarView.AddTopBar(&headerBar.Widget)

	// Create scrolled window with preferences page
	scrolled := gtk.NewScrolledWindow()
	scrolled.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scrolled.SetVexpand(true)

	prefsPage := adw.NewPreferencesPage()
	scrolled.SetChild(&prefsPage.Widget)

	toolbarView.SetContent(&scrolled.Widget)

	return toolbarView, prefsPage
}
