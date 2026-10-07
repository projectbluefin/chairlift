// Package views provides the page content for the ChairLift application
package views

import (
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/agentmode"
	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/capability"
	"github.com/projectbluefin/chairlift/internal/config"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/liverystate"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
	"github.com/projectbluefin/chairlift/internal/views/rowset"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gobject"
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
	config       *config.Config
	capabilities capability.Set
	toastAdder   ToastAdder
	updateShell  *UpdateShell

	// Pages (ToolbarViews)
	agentsPage       *adw.ToolbarView
	updatesPage      *adw.ToolbarView
	applicationsPage *adw.ToolbarView
	maintenancePage  *adw.ToolbarView
	featuresPage     *adw.ToolbarView
	liveryPage       *adw.ToolbarView
	helpPage         *adw.ToolbarView
	recoveryPage     *adw.ToolbarView // Powerwash detail, reached from Maintenance, not a sidebar page

	// PreferencesPages inside each ToolbarView - keep references to prevent GC
	agentsPrefsPage       *adw.PreferencesPage
	updatesPrefsPage      *adw.PreferencesPage
	applicationsPrefsPage *adw.PreferencesPage
	maintenancePrefsPage  *adw.PreferencesPage
	featuresPrefsPage     *adw.PreferencesPage
	liveryPrefsPage       *adw.PreferencesPage
	helpPrefsPage         *adw.PreferencesPage
	recoveryPrefsPage     *adw.PreferencesPage
	// The preferences page inside each createPage ToolbarView; it owns the
	// scrolling, so it is what ScrollPageToTop resets.
	pageContents map[*adw.ToolbarView]*adw.PreferencesPage

	// References for dynamic updates
	installedFormulae  *adw.PreferencesGroup
	installedCasks     *adw.PreferencesGroup
	formulaeRows       rowset.Tracker[*adw.ActionRow]
	caskRows           rowset.Tracker[*adw.ActionRow]
	brewBundlesGroup   *adw.PreferencesGroup
	appInstallProgress installProgress
	brewTrustGroup     *adw.PreferencesGroup
	brewTrustRows      map[string]*adw.ActionRow
	// Apps collections share one install gate per collection.
	bundleInstalls map[string]*bundleInstall
	bundleButtons  buttonRoute

	// One shared callback per rebuilt list; see buttonRoute. Each is cleared
	// alongside its row tracker, so reloads allocate no new trampolines.
	formulaButtons buttonRoute
	caskButtons    buttonRoute
	trustButtons   buttonRoute
	// confirmations routes every confirmation dialog the Apps and Updates
	// pages present, one response each.
	confirmations dialogRoute

	// Native activity indicators; operation state remains in the existing gates.
	liveryAppGridSpinner     *gtk.Spinner
	liveryPanelSpinner       *gtk.Spinner
	liveryPanelRotateSpinner *gtk.Spinner
	liveryDockSpinner        *gtk.Spinner
	liveryDockRotateSpinner  *gtk.Spinner
	developerSpinner         *gtk.Spinner
	gamingSpinner            *gtk.Spinner
	agentModeSpinner         *gtk.Spinner

	// Livery references. liveryState is the last state the page loaded and
	// is what every handler compares against, so a programmatic widget
	// update during restore is recognized as "no change" instead of being
	// replayed as a user action; liverySuppress closes the same window
	// explicitly. See applyLiveryState.
	liveryAppGridGroup     *adw.PreferencesGroup
	liveryAppGridSwitch    *gtk.Switch
	liveryAppGridRow       *adw.ActionRow
	liveryPickerMode       liveryPickerMode
	liveryPanelGroup       *adw.PreferencesGroup
	liveryPanelSwitch      *gtk.Switch
	liveryPanelMarkRow     *adw.ActionRow
	liveryPanelRotate      *gtk.Switch
	liveryDockGroup        *adw.PreferencesGroup
	liveryDockSwitch       *gtk.Switch
	liveryPickerDialog     *adw.Dialog
	liveryPickerSearch     *gtk.SearchEntry
	liveryPickerList       *gtk.ListBox
	liveryPickerCancel     func()
	liveryPickerGeneration uint64
	liveryFoundationGrid   *gtk.FlowBox
	liveryFoundationImages map[string]*gtk.Image
	liveryDockSelectedRow  *adw.ActionRow
	liveryDockRotate       *gtk.Switch
	// liveryDockVisible is the result set currently drawn, so the list's one
	// row-activated handler can map a row index back to a project without
	// allocating a callback per row. See refreshLiveryPickerRows.
	liveryDockVisible []pageview.LiveryProjectResult
	liveryState       livery.State
	liverySuppress    bool
	liveryLoaded      bool
	// What the first load found this desktop can apply: the app-grid mark
	// needs its icon-theme directory, the panel mark needs the Custom
	// Command Menu extension. The setup assistant reads these to leave an
	// inapplicable choice insensitive rather than offering a switch that
	// would apply nothing.
	liveryAppGridAvailable bool
	liveryPanelAvailable   bool
	// liverySchemaMissing records the one load failure the page cannot
	// recover from in-session; every choice is then reported unavailable.
	liverySchemaMissing bool
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
	liveryRotateWork    actionstate.Serializer
	liveryRotatePending liverystate.RotationCandidates

	// Profile Picture section of the Livery page; nil when account_group is
	// disabled. See profile_picture.go.
	profilePicture *avatarPicker

	// Automatic background updates
	autoUpdatesRow    *adw.ActionRow
	autoUpdatesSwitch *gtk.Switch

	// bootc update references
	bootcStageExpander *adw.ExpanderRow
	bootcStageBtn      *gtk.Button
	bootcActivityRow   *adw.ActionRow
	bootcLogExpander   *adw.ExpanderRow
	// The Roll Back group holds only the rollback row and is hidden with
	// it, so a host with no previous deployment shows no orphaned heading.
	bootcRollbackGroup *adw.PreferencesGroup
	bootcRollbackRow   *adw.ActionRow
	bootcRollbackBtn   *gtk.Button
	// Shown in place of Roll Back once a live rollback is queued, because
	// the rollback only takes effect at the next boot (#520).
	bootcRestartBtn   *gtk.Button
	bootcRollbackGate actionstate.Gate

	// Free up space admits one cleanup run at a time.
	freeUpSpaceGate actionstate.Gate

	// updateFeaturesGate admits one updex feature update at a time (#488).
	updateFeaturesGate actionstate.Gate

	// Bluefin-family (channel / developer mode / gaming) references
	channelGroup       *adw.PreferencesGroup
	channelRow         *adw.ActionRow
	channelSwitch      *gtk.Switch
	developerGroup     *adw.PreferencesGroup
	developerRow       *adw.ActionRow
	developerSwitch    *gtk.Switch
	developerGate      actionstate.Gate
	developerCanToggle bool
	// developerFeedGate admits the optional Pulp/feed-staging work that
	// follows a confirmed enable. It is a second gate rather than a reuse of
	// developerGate because its lifetime is different: the switch is
	// released as soon as the helper returns, while a Flatpak install keeps
	// running off the main thread. Overlapping installs are
	// refused by this gate, not by holding the switch insensitive.
	developerFeedGate  actionstate.Gate
	gamingGroup        *adw.PreferencesGroup
	gamingRow          *adw.ActionRow
	gamingComponents   []*gamingComponentRow
	gamingInstall      *gtk.Button
	gamingRemove       *gtk.Button
	gamingGate         actionstate.Gate
	gamingButtons      buttonRoute
	gamingDialogs      dialogRoute
	developerOptions   []*developerOptionRow
	developerButtons   buttonRoute
	wslBackend         string
	wslCombo           *adw.ComboRow
	wslSuppress        bool
	wslBackendResolved bool
	wslBackendNotify   func(gobject.Object, uintptr)
	driverRow          *adw.ActionRow
	driverButton       *gtk.Button
	driverGate         actionstate.Gate

	// Staged-update changelog (SBOM diff), with visible Compare and optional
	// result details in the system-update secondary group.
	changelogRow      *adw.ActionRow
	changelogGroup    *adw.PreferencesGroup
	changelogButton   *gtk.Button
	changelogSections []*adw.ExpanderRow
	changelogBooted   string
	changelogStaged   string
	changelogGate     actionstate.Gate

	// Published versions (the dated-build catalog, ADR-0013), listed on
	// the Powerwash page below Roll Back, in its own group. runningVersion and
	// previousVersion are the bootc versions of the booted and rollback
	// deployments, recorded by loadBootcRollbackStatus.
	publishedVersionsRow    *adw.ExpanderRow
	publishedVersionsButton *gtk.Button
	publishedVersionRows    []*adw.ActionRow
	publishedVersionButtons buttonRoute
	recoveryDialogs         dialogRoute
	pinGate                 actionstate.Gate
	unpinGate               actionstate.Gate
	unpinRow                *adw.ActionRow
	unpinBtn                *gtk.Button
	publishedVersionsRepo   string
	publishedVersionsStream string
	publishedVersionsGate   actionstate.Gate
	runningVersion          string
	previousVersion         string

	// Agent Mode (agents_page agents_group)
	agentModeRow       *adw.ActionRow
	agentModelRow      *adw.ActionRow
	agentPresetRow     *adw.ActionRow
	agentModeToggle    *guardedSwitch
	agentPresetSpinner *gtk.Spinner
	agentModeState     aistack.State
	agentModeGate      actionstate.Gate
	agentPresetGate    actionstate.Gate
	agentRefresh       actionstate.RefreshGate
	agentPresetDialogs dialogRoute
	// agentManageRow opens llmman's own web UI, where models are managed.
	agentManageRow *adw.ActionRow

	// Troubleshooting (agents_page troubleshooting_group): the Goose row, which
	// sets Goose up and launches it, and the menu-entry switch. Main-thread
	// only. gooseGate admits one setup or launch at a time and gooseBusy
	// keeps a background readiness read off the row while one runs;
	// gooseRefresh drops a read a newer one superseded.
	gooseRow       *adw.ActionRow
	gooseLaunchBtn *gtk.Button
	gooseSpinner   *gtk.Spinner
	gooseGate      actionstate.Gate
	gooseBusy      bool
	gooseRefresh   actionstate.RefreshGate
	gooseFacts     agentmode.ReadinessFacts
	gooseState     agentmode.State

	askBluefinMenuRow *adw.ActionRow
	askBluefinToggle  *guardedSwitch
	askBluefinGate    actionstate.Gate
	askBluefinMapped  func(gtk.Widget)
	askBluefinProbed  bool

	// Contribute to Bluefin (agents_page)
	contributeRow     *adw.ActionRow
	contributeButton  *gtk.Button
	contributeHelp    *gtk.Button
	contributeSpinner *gtk.Spinner
	contributeGate    actionstate.Gate

	// Powerwash / Factory Reset (maintenance_page reset_group)
	powerwashGate    actionstate.Gate
	factoryResetGate actionstate.Gate

	// Features page references
	featuresGroup *adw.PreferencesGroup
	featureRows   map[string]*adw.ActionRow
	// Printer applications (features_page printers_group)
	printersGroup *adw.PreferencesGroup
	printerRows   []*printerRow

	// Groups with deferred visibility

	// Powerwash detail navigation, wired by the window after construction.
	// Maintenance opens the detail; its back button returns to Maintenance.
	// The stable recovery route is a detail, not a sidebar page.
	openRecoveryDetail  func()
	closeRecoveryDetail func()

	brewPackagesRefresh actionstate.RefreshGate
}

// New creates a new UserHome views manager.
//
// caps is the capability set resolved once during window construction; every
// view builder routes its group predicate through it, so a group renders only
// when its configuration enables it and the host supports it. A nil caps is
// treated as an empty set, which lets callers that cannot resolve capabilities
// still construct views (they simply render nothing capability-gated).
func New(cfg *config.Config, caps capability.Set, toastAdder ToastAdder) *UserHome {
	start := time.Now()

	uh := &UserHome{
		config:       cfg,
		capabilities: caps,
		toastAdder:   toastAdder,
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

// groupEnabled is the one policy floor every view builder shares: a page's
// group renders only when its configuration enables it AND the host supports
// it. It composes the administrator's configuration with the capability set
// resolved once during window construction, so the views never derive their
// own answer and the floor stays identical across every page. See
// internal/capability. A nil capability set composes to false everywhere,
// matching the repository's fail-closed rule.
func (uh *UserHome) groupEnabled(page, group string) bool {
	return capability.Compose(uh.config.IsGroupEnabled, uh.capabilities)(page, group)
}

// OnUpdateFinished refreshes the installed app inventories and Compare pair
// after verified live updates. The shell remains the only badge owner.
func (uh *UserHome) OnUpdateFinished(final updateflow.Snapshot) {
	if final.Preview {
		return
	}
	for _, source := range final.CompletedSources {
		switch source {
		case updateflow.DeveloperTools:
			go uh.loadHomebrewPackages()
		case updateflow.OperatingSystem:
			go func() {
				ctx, cancel := bootc.DefaultContext()
				defer cancel()
				status, err := bootc.GetStatus(ctx)
				if err != nil {
					log.Printf("views: could not refresh staged system state: %v", err)
					return
				}
				sgtk.RunOnMainThread(func() {
					uh.refreshChangelogAvailability(status)
				})
			}()
		}
	}
}

// UpdatesPreferencesPage returns the preferences page buildUpdatesPage
// filled, for the window to mount as the update shell's secondary content.
// It is always constructed by New; a group configuration or the capability
// floor disables leaves it without that group, never nil.
func (uh *UserHome) UpdatesPreferencesPage() *adw.PreferencesPage {
	if uh == nil {
		return nil
	}
	return uh.updatesPrefsPage
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

// ScrollPageToTop returns a page built by createPage to its top. A page keeps
// its scroll position across navigation, so a step of the setup flow could
// otherwise open mid-page with its heading out of view.
func (uh *UserHome) ScrollPageToTop(name string) {
	if page := uh.pageContents[uh.GetPage(name)]; page != nil {
		page.ScrollToTop()
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
	if uh.pageContents == nil {
		uh.pageContents = map[*adw.ToolbarView]*adw.PreferencesPage{}
	}
	uh.pageContents[toolbarView] = prefsPage

	return toolbarView, prefsPage
}

// AttachUpdateShell shares the unified mutation owner with secondary actions.
func (uh *UserHome) AttachUpdateShell(shell *UpdateShell) {
	uh.updateShell = shell
	if shell != nil {
		shell.trustGroupAvailable = uh.brewTrustGroup != nil
	}
}
