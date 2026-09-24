package views

import (
	"log"

	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// buildLiveryPage builds the Livery page.
//
// The three sections are the sentence in pageview.LiveryPageDescription, in
// order: who you are (the app-grid mark), who you stand with (the panel
// mark), what you roll with (the Files mark). The two that rotate are
// adjacent at the bottom so their shared behaviour reads as one idea; the
// personal mark sits alone at the top because it is set once.
//
// Every control starts insensitive. Reading the current state means calling
// `gsettings`, which is a subprocess and must not run on the GTK main thread,
// so the page is drawn first and populated by refreshLiveryState.
func (uh *UserHome) buildLiveryPage() {
	page := uh.liveryPrefsPage
	if page == nil {
		return
	}
	page.SetDescription(pageview.LiveryPageDescription)

	if uh.groupEnabled("livery_page", "livery_app_grid_group") {
		uh.buildLiveryAppGridGroup(page)
	}
	if uh.groupEnabled("livery_page", "livery_foundation_group") {
		uh.buildLiveryPanelGroup(page)
	}
	if uh.groupEnabled("livery_page", "livery_dock_group") {
		uh.buildLiveryDockGroup(page)
	}

	go uh.refreshLiveryState()
}

// buildLiveryAppGridGroup builds "who you are": a brand mark fetched from
// simpleicons.org, or the user's own SVG. It has no rotation switch —
// a personal mark that changed on its own would stop being personal.
func (uh *UserHome) buildLiveryAppGridGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.LiveryAppGridTitle)
	group.SetDescription(pageview.LiveryAppGridFragment)

	enableRow, enableSwitch := newSwitchRow(pageview.LiveryAppGridRow(), func(state bool) {
		uh.onLiveryAppGridToggled(state)
	})
	group.Add(&enableRow.Widget)

	// The same chooser the dock uses. Typing a slug blind into a text field
	// meant knowing simpleicons.org's answer in advance — and its slugs are
	// not guessable (".NET" is `dotnet`, "Write.as" is `writedotas`), so the
	// field could only ever have worked for someone who had already looked it
	// up on the website.
	brandRow := adw.NewActionRow()
	brandRow.SetUseMarkup(false)
	brand := pageview.LiverySelectedBrandRow("")
	brandRow.SetTitle(brand.Title)
	brandRow.SetSubtitle(brand.Subtitle)
	brandRow.AddSuffix(&gtk.NewImageFromIconName("go-next-symbolic").Widget)
	brandRow.SetActivatable(true)
	brandRow.SetSensitive(false)
	group.Add(&brandRow.Widget)

	openBrands := func(_ adw.ActionRow) { uh.presentLiveryPicker(liveryPickerBrand) }
	brandRow.ConnectActivated(&openBrands)

	page.Add(group)
	uh.liveryAppGridGroup = group
	uh.liveryAppGridSwitch = enableSwitch
	uh.liveryAppGridRow = brandRow

}

// buildLiveryPanelGroup builds "who you stand with": a foundation mark in the
// top bar, optionally advancing at each login.
func (uh *UserHome) buildLiveryPanelGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.LiveryPanelTitle)
	group.SetDescription(pageview.LiveryPanelFragment)

	enableRow, enableSwitch := newSwitchRow(pageview.LiveryPanelRow(), func(state bool) {
		uh.onLiverySurfaceToggled(livery.Panel, state)
	})
	group.Add(&enableRow.Widget)

	markRow := adw.NewActionRow()
	markRow.SetUseMarkup(false)
	mark := pageview.LiverySelectedFoundationRow(livery.DefaultID, "")
	markRow.SetTitle(mark.Title)
	markRow.SetSubtitle(mark.Subtitle)
	markRow.AddSuffix(&gtk.NewImageFromIconName("go-next-symbolic").Widget)
	markRow.SetActivatable(true)
	markRow.SetSensitive(false)
	group.Add(&markRow.Widget)

	openMarks := func(_ adw.ActionRow) { uh.presentLiveryPicker(liveryPickerFoundation) }
	markRow.ConnectActivated(&openMarks)
	uh.liveryPanelMarkRow = markRow

	rotateRow, rotateSwitch := newSwitchRow(pageview.LiveryRotationRow(livery.RunsFromSystemPrefix()), func(state bool) {
		uh.onLiveryRotateToggled(livery.Panel, state)
	})
	group.Add(&rotateRow.Widget)

	page.Add(group)
	uh.liveryPanelGroup = group
	uh.liveryPanelSwitch = enableSwitch
	uh.liveryPanelRotate = rotateSwitch

}

// buildLiveryDockGroup builds "what you roll with": the Files mark.
func (uh *UserHome) buildLiveryDockGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.LiveryDockTitle)
	group.SetDescription(pageview.LiveryDockFragment)

	enableRow, enableSwitch := newSwitchRow(pageview.LiveryDockRow(), func(state bool) {
		uh.onLiverySurfaceToggled(livery.Dock, state)
	})
	group.Add(&enableRow.Widget)

	// One row that opens a picker, not a search box wired into the page.
	//
	// GNOME puts a choice from a large set behind a row you activate: the row
	// states the current value, and the searching happens in a dialog. Settings
	// does this for default applications, wallpapers, and input sources. An
	// inline search entry with a live result list belongs in a search *page*,
	// not in a preferences group, and it pushes everything below it off the
	// screen besides.
	projectRow := adw.NewActionRow()
	projectRow.SetUseMarkup(false)
	selected := pageview.LiverySelectedProjectRow(livery.DefaultCNCFID)
	projectRow.SetTitle(selected.Title)
	projectRow.SetSubtitle(selected.Subtitle)
	projectRow.AddSuffix(&gtk.NewImageFromIconName("go-next-symbolic").Widget)
	projectRow.SetActivatable(true)
	projectRow.SetSensitive(false)
	group.Add(&projectRow.Widget)

	openPicker := func(_ adw.ActionRow) { uh.presentLiveryPicker(liveryPickerProject) }
	projectRow.ConnectActivated(&openPicker)

	uh.liveryDockSelectedRow = projectRow

	rotateRow, rotateSwitch := newSwitchRow(pageview.LiveryRotationRow(livery.RunsFromSystemPrefix()), func(state bool) {
		uh.onLiveryRotateToggled(livery.Dock, state)
	})
	group.Add(&rotateRow.Widget)

	// Where the artwork comes from.
	artworkRow := adw.NewActionRow()
	artwork := pageview.LiveryCNCFArtworkRow()
	artworkRow.SetTitle(artwork.Title)
	artworkRow.SetSubtitle(artwork.Subtitle)
	artworkRow.AddSuffix(&gtk.NewImageFromIconName("adw-external-link-symbolic").Widget)
	artworkRow.SetActivatable(true)
	artworkActivated := func(_ adw.ActionRow) { uh.openURL(pageview.CNCFArtworkURL) }
	artworkRow.ConnectActivated(&artworkActivated)
	group.Add(&artworkRow.Widget)

	page.Add(group)
	uh.liveryDockGroup = group
	uh.liveryDockSwitch = enableSwitch
	uh.liveryDockRotate = rotateSwitch

}

// newSwitchRow builds an action row carrying a switch, wired to state-set.
//
// This is the pattern features_page.go already uses, and it is the reason
// this page stopped writing settings on its own. AdwSwitchRow exposes no
// change-specific signal in these bindings, so an earlier version listened to
// the generic `notify` — which fires for sensitivity, title, and subtitle
// too. Restoring saved state therefore looked like a user toggling the
// switch, and the page wrote to dconf on load. GtkSwitch::state-set fires
// only when the active state actually changes, and gtk_switch_set_active is a
// no-op when the value is unchanged, so programmatic restore is silent.
func newSwitchRow(presentation pageview.Row, onToggle func(bool)) (*adw.ActionRow, *gtk.Switch) {
	row := adw.NewActionRow()
	row.SetTitle(presentation.Title)
	row.SetSubtitle(presentation.Subtitle)

	toggle := gtk.NewSwitch()
	toggle.SetValign(gtk.AlignCenterValue)
	toggle.SetSensitive(false)

	sw := toggle
	stateSet := func(_ gtk.Switch, state bool) bool {
		onToggle(state)
		return false
	}
	toggle.ConnectStateSet(&stateSet)

	row.AddSuffix(&sw.Widget)
	row.SetActivatableWidget(&sw.Widget)
	return row, toggle
}

// refreshLiveryState reads persisted settings off the main thread and applies
// them to the page.
//
// A missing schema is the one failure the user cannot fix from this page, so
// it leaves every control insensitive and says why. Everything else has
// already been repaired by livery.Load.
func (uh *UserHome) refreshLiveryState() {
	ctx, cancel := livery.DefaultContext()
	defer cancel()

	state, err := livery.Load(ctx)
	panelAvailable := livery.PanelAvailable(ctx)

	sgtk.RunOnMainThread(func() {
		if err != nil {
			// The page keeps its controls insensitive and says why. The
			// gate is still armed: without it, every later notify would be
			// treated as a user edit against zero-valued state.
			log.Printf("livery: loading settings: %v", err)
			uh.liveryLoaded = true
			uh.toastAdder.ShowErrorToast(pageview.LiverySchemaMissingMessage)
			return
		}
		uh.applyLiveryState(state, panelAvailable)
	})
}

// applyLiveryState populates every control. It runs on the main thread.
//
// Each group is nil-guarded: any of the three can be disabled in config, in
// which case its widgets were never constructed.
func (uh *UserHome) applyLiveryState(state livery.State, panelAvailable bool) {
	// Two guards, because one is not enough on its own.
	//
	// liveryState is the value every handler compares against, so assigning
	// it *before* touching a widget is what makes the resulting notify
	// storm a no-op rather than a replay of the user's whole configuration.
	// Leaving it unassigned would have each restore look like a change from
	// the zero value and write icons, spawn gsettings, install the systemd
	// unit, and hit the Simple Icons CDN on page load.
	//
	// liverySuppress covers the same window explicitly, because these
	// bindings expose only the generic `notify` signal: it fires for
	// sensitivity and subtitle changes too, not just the property being
	// restored, and relying on value comparison alone to absorb all of them
	// is a subtler invariant than it needs to be.
	uh.liverySuppress = true
	defer func() {
		uh.liverySuppress = false
		// Only now may a handler act. Until the first load completes there
		// is no state to compare against, and a notify arriving in that
		// window — including one GTK delivers after this function returns —
		// would look like a user edit and write to dconf on page load.
		uh.liveryLoaded = true
	}()
	uh.liveryState = state

	// Seed the selections with the ids the combos will actually hold, not
	// the raw stored values. The combo carries an index, and the handler maps
	// that index back to an id; an empty or stale stored value resolves to
	// index 0, which maps back to the default id. Comparing the handler's
	// resolved "cncf" against a raw "" would read as a change and write.
	panelChoice := pageview.LiveryChoices(state.PanelID)

	uh.liveryState.PanelID = pageview.LiveryIDForIndex(panelChoice.Selected)

	if uh.liveryAppGridSwitch != nil {
		uh.liveryAppGridSwitch.SetSensitive(true)
		uh.liveryAppGridSwitch.SetActive(state.AppGridEnabled)
	}
	if uh.liveryAppGridRow != nil {
		uh.liveryAppGridRow.SetSensitive(state.AppGridEnabled)
		uh.liveryAppGridRow.SetSubtitle(pageview.LiverySelectedBrandRow(state.AppGridSlug).Subtitle)
	}

	// Without the Custom Command Menu extension there is no panel mark to
	// set, so the section is hidden rather than left as an inert switch.
	if uh.liveryPanelGroup != nil {
		uh.liveryPanelGroup.SetVisible(panelAvailable)
	}
	if uh.liveryPanelSwitch != nil {
		uh.liveryPanelSwitch.SetSensitive(panelAvailable)
		uh.liveryPanelSwitch.SetActive(state.PanelEnabled && panelAvailable)
	}
	if uh.liveryPanelMarkRow != nil {
		uh.liveryPanelMarkRow.SetSubtitle(pageview.LiverySelectedFoundationRow(state.PanelID, state.PanelCustom).Subtitle)
		uh.liveryPanelMarkRow.SetSensitive(panelAvailable && state.PanelEnabled)
	}
	if uh.liveryPanelRotate != nil {
		uh.liveryPanelRotate.SetSensitive(
			pageview.LiveryRotationAvailable(panelAvailable && state.PanelEnabled, uh.liveryState.PanelID))
		uh.liveryPanelRotate.SetActive(state.PanelRotate)
	}

	if uh.liveryDockSwitch != nil {
		uh.liveryDockSwitch.SetSensitive(true)
		uh.liveryDockSwitch.SetActive(state.DockEnabled)
	}
	if uh.liveryDockSelectedRow != nil {
		selected := pageview.LiverySelectedProjectRow(state.DockID)
		uh.liveryDockSelectedRow.SetTitle(selected.Title)
		uh.liveryDockSelectedRow.SetSubtitle(selected.Subtitle)
	}
	if uh.liveryDockSelectedRow != nil {
		uh.liveryDockSelectedRow.SetSensitive(state.DockEnabled)
	}
	if uh.liveryDockRotate != nil {
		uh.liveryDockRotate.SetSensitive(
			pageview.LiveryRotationAvailable(state.DockEnabled, uh.liveryState.DockID))
		uh.liveryDockRotate.SetActive(state.DockRotate)
	}
}

// presentLiveryProjectPicker opens the project chooser, building it once.
//
// The dialog and its signals are created on first use and then reused. A
// fresh dialog per open would connect a new search handler and a new
// row-activated handler every time, and puregotk draws every callback from a
// fixed table that never releases a slot — so rebuilding per open burns two
// slots per open and ends in the same panic as a handler per row, just more
// slowly. This is the rule in AGENTS.md: connect once, never in a path that
// reruns.
func (uh *UserHome) presentLiveryPicker(mode liveryPickerMode) {
	if uh.liveryPickerDialog == nil {
		uh.buildLiveryProjectPicker()
	}
	uh.liveryPickerMode = mode
	uh.liveryPickerDialog.SetTitle(mode.title())
	if uh.liveryPickerSearch != nil {
		uh.liveryPickerSearch.SetPlaceholderText(mode.placeholder())
		uh.liveryPickerSearch.SetText("")
	}
	uh.refreshLiveryPickerRows("")
	uh.liveryPickerDialog.Present(&uh.liveryPrefsPage.Widget)
}

// liveryPickerMode selects which catalog the shared chooser searches.
//
// One dialog serves both, because they are the same interaction over
// different lists — and because building a second would connect a second set
// of signals, which is the callback-table trap AGENTS.md documents.
type liveryPickerMode int

const (
	liveryPickerProject liveryPickerMode = iota
	liveryPickerBrand
	liveryPickerFoundation
)

// surface maps a picker mode to the surface it configures.
func (m liveryPickerMode) surface() livery.Surface {
	switch m {
	case liveryPickerBrand:
		return livery.AppGrid
	case liveryPickerFoundation:
		return livery.Panel
	default:
		return livery.Dock
	}
}

func (m liveryPickerMode) title() string {
	switch m {
	case liveryPickerBrand:
		return pageview.LiveryBrandPickerTitle
	case liveryPickerFoundation:
		return pageview.LiveryFoundationPickerTitle
	default:
		return pageview.LiveryProjectPickerTitle
	}
}

func (m liveryPickerMode) placeholder() string {
	switch m {
	case liveryPickerBrand:
		return pageview.LiveryBrandSearchPlaceholder
	case liveryPickerFoundation:
		return pageview.LiveryFoundationSearchPlaceholder
	default:
		return pageview.LiveryProjectSearchPlaceholder
	}
}

// buildLiveryProjectPicker constructs the chooser and wires its signals. It
// runs at most once per session.
func (uh *UserHome) buildLiveryProjectPicker() {
	dialog := adw.NewDialog()
	dialog.SetTitle(pageview.LiveryProjectPickerTitle)
	dialog.SetContentWidth(420)
	dialog.SetContentHeight(520)

	search := gtk.NewSearchEntry()
	search.SetPlaceholderText(pageview.LiveryProjectSearchPlaceholder)
	search.SetHexpand(true)

	header := adw.NewHeaderBar()
	header.SetTitleWidget(&search.Widget)

	list := gtk.NewListBox()
	list.SetSelectionMode(gtk.SelectionNoneValue)
	list.AddCssClass("boxed-list")
	list.SetMarginTop(12)
	list.SetMarginBottom(12)
	list.SetMarginStart(12)
	list.SetMarginEnd(12)

	scrolled := gtk.NewScrolledWindow()
	scrolled.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scrolled.SetVexpand(true)
	scrolled.SetChild(&list.Widget)

	content := adw.NewToolbarView()
	content.AddTopBar(&header.Widget)
	content.SetContent(&scrolled.Widget)
	dialog.SetChild(&content.Widget)

	uh.liveryPickerDialog = dialog
	uh.liveryPickerSearch = search
	uh.liveryPickerList = list

	searchChanged := func(_ gtk.SearchEntry) {
		uh.refreshLiveryPickerRows(search.GetText())
	}
	search.ConnectSearchChanged(&searchChanged)

	rowActivated := func(_ gtk.ListBox, rowPtr uintptr) {
		uh.onLiveryPickerRowActivated(rowPtr)
	}
	list.ConnectRowActivated(&rowActivated)
}

// refreshLiveryPickerRows redraws the picker's results for a query.
func (uh *UserHome) refreshLiveryPickerRows(query string) {
	list := uh.liveryPickerList
	if list == nil {
		return
	}
	for {
		child := list.GetRowAtIndex(0)
		if child == nil {
			break
		}
		list.Remove(&child.Widget)
	}

	var results []pageview.LiveryPickerResult
	var selected string
	switch uh.liveryPickerMode {
	case liveryPickerBrand:
		selected = uh.liveryState.AppGridSlug
		results = pageview.LiveryBrandResults(query, selected)
	case liveryPickerFoundation:
		selected = uh.liveryState.PanelID
		results = pageview.LiveryFoundationResults(query, selected)
	default:
		selected = uh.liveryState.DockID
		results = pageview.LiveryProjectResults(query, selected)
	}
	// A query that matches nothing says so. The custom escape hatch is
	// appended below, after this check, because appending it first would make
	// the result set never empty — the no-results row could not be reached,
	// and a search for nonsense would answer with "Custom SVG…" alone. The
	// visible slice is cleared with it, since row-activated maps a row index
	// into that slice and this branch draws a row that is not in it.
	if len(results) == 0 {
		uh.liveryDockVisible = nil
		empty := adw.NewActionRow()
		empty.SetUseMarkup(false)
		row := pageview.LiveryNoResultsRow(query)
		empty.SetTitle(row.Title)
		empty.SetSubtitle(row.Subtitle)
		list.Append(&empty.Widget)
		return
	}

	// The escape hatch rides along with the catalog rather than sitting as a
	// fourth row in every section.
	results = append(results, pageview.LiveryCustomResult(selected == livery.CustomID))
	uh.liveryDockVisible = results

	for _, result := range results {
		row := adw.NewActionRow()
		// Catalog names are literal text, not markup. AdwPreferencesRow
		// parses titles as Pango markup by default, so a brand containing an
		// ampersand — "AT&T", "Dungeons & Dragons", "1&1" — fails to parse
		// and the row renders with no title at all. Eight of the 3,461 brands
		// hit this.
		row.SetUseMarkup(false)
		row.SetTitle(result.Name)
		row.SetActivatable(true)
		if result.Selected {
			row.AddSuffix(&gtk.NewImageFromIconName("object-select-symbolic").Widget)
		}
		list.Append(&row.Widget)
	}
}

// onLiveryPickerRowActivated applies the chosen project and closes the dialog.
func (uh *UserHome) onLiveryPickerRowActivated(rowPtr uintptr) {
	if uh.liveryPickerList == nil || len(uh.liveryDockVisible) == 0 {
		return
	}
	row := gtk.ListBoxRowNewFromInternalPtr(rowPtr)
	index := int(row.GetIndex())
	if index < 0 || index >= len(uh.liveryDockVisible) {
		return
	}
	id := uh.liveryDockVisible[index].ID
	mode := uh.liveryPickerMode
	if uh.liveryPickerDialog != nil {
		uh.liveryPickerDialog.Close()
	}
	if id == pageview.LiveryCustomResultID {
		uh.presentLiveryFileChooser(mode.surface())
		return
	}
	switch mode {
	case liveryPickerBrand:
		uh.onLiveryBrandChosen(id)
	case liveryPickerFoundation:
		uh.onLiverySelectionChangedByID(livery.Panel, id)
	default:
		uh.onLiveryProjectChosen(id)
	}
}

// presentLiveryFileChooser asks for an SVG and applies it to a surface.
//
// This is the escape hatch: the catalogs cover 3,461 brands, 214 projects,
// and ten foundations, and none of them covers a mark that is only yours.
// Without it the "Custom SVG…" entry in the foundation dropdown is a dead
// end that selects a source with no file behind it.
//
// The dialog is built per invocation rather than once, unlike the search
// picker. That is deliberate: GtkFileDialog is one-shot by design — its
// result arrives through a callback tied to this particular Open call — and
// a file chooser is opened rarely, by hand, so the callback-table cost is a
// handful of slots over a session rather than one per keystroke.
func (uh *UserHome) presentLiveryFileChooser(surface livery.Surface) {
	dialog := gtk.NewFileDialog()
	dialog.SetTitle(pageview.LiveryFileChooserTitle)

	filter := gtk.NewFileFilter()
	filter.SetName(pageview.LiveryFileChooserFilterName)
	filter.AddMimeType("image/svg+xml")
	filter.AddPattern("*.svg")
	filters := gio.NewListStore(gtk.FileFilterGLibType())
	filters.Append(gobject.ObjectNewFromInternalPtr(filter.GoPointer()))
	dialog.SetFilters(filters)

	var done gio.AsyncReadyCallback = func(source, result, _ uintptr) {
		file, err := dialog.OpenFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil {
			// Cancelling is the ordinary outcome here, not a failure worth
			// a toast; a real error still reaches the log.
			log.Printf("livery: choosing a file: %v", err)
			return
		}
		path := file.GetPath()
		if path == "" {
			return
		}
		uh.onLiveryCustomFileChosen(surface, path)
	}
	dialog.Open(uh.window(), nil, &done, 0)
}

// window returns the toplevel to anchor dialogs on, or nil when the page is
// not yet in one.
func (uh *UserHome) window() *gtk.Window {
	if uh.liveryPrefsPage == nil {
		return nil
	}
	root := uh.liveryPrefsPage.GetRoot()
	if root == nil {
		return nil
	}
	return gtk.WindowNewFromInternalPtr(root.GoPointer())
}
