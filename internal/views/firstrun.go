package views

import (
	"context"
	"errors"
	"log"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/firstrun"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/settings"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/userprefs"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// FirstRunAssistant is the setup assistant: an AdwDialog over the main window
// with a welcome hero screen and at most three optional steps, each carrying
// real controls that act through SetupHost — the pages' own handlers — so
// nothing here is a second implementation of a setting.
//
// Every widget and every signal is built once, in buildUI; Present only
// rebuilds the pure model and refreshes row state, so reopening the assistant
// allocates no new puregotk callback (AGENTS.md's fixed callback table).
type FirstRunAssistant struct {
	dialog          *adw.Dialog
	stack           *gtk.Stack
	toasts          *adw.ToastOverlay
	model           *firstrun.AssistantModel
	store           firstrun.Store
	host            SetupHost
	toastAdder      ToastAdder
	filter          func(page, group string) bool
	configStepTitle *gtk.Label
	configStepDesc  *gtk.Label
	configStepInfo  *adw.PreferencesGroup
	stepStack       *gtk.Stack
	wordmarkPic     *gtk.Picture
	primaryBtn      *gtk.Button
	secondaryBtn    *gtk.Button
	backBtn         *gtk.Button
	forwardBtn      *gtk.Button
	open            bool
	// decided is set when this presentation reaches a decision — Get Moving,
	// a welcome-only completion, or Finish — each of which persists its own
	// disposition. The closed handler reads it to tell an intentional
	// dismissal (Escape, the close button, a click outside) from a close
	// that already recorded something: a dismissal records a skip, so the
	// assistant does not return uninvited on every launch.
	decided bool

	// Appearance step: one switch row per surface the floor admits, driven
	// through the Livery page's own switch.
	liveryRows map[livery.Surface]*setupSwitchRow
	// suppress covers every programmatic switch write — a restore from the
	// page's state, a revert of a flip the page refused, a dry-run seed —
	// each of which emits state-set exactly as a click does.
	suppress bool

	// Apps step: one row per discovered collection, connected through the
	// Apps page's shared per-collection install gate.
	bundlesGroup       *adw.PreferencesGroup
	bundlesPlaceholder *adw.ActionRow
	bundleRowsBuilt    bool

	// Update Preferences step: one switch row per source the floor admits,
	// bound to the same GSettings key the Preferences dialog binds.
	updateRows map[updateflow.SourceID]*setupSwitchRow
}

// setupSwitchRow is one choice row and the switch it carries.
type setupSwitchRow struct {
	row      *adw.ActionRow
	toggle   *gtk.Switch
	subtitle string // the row's own subtitle, restored once the choice is ready
}

// NewFirstRunAssistant constructs the assistant and wires all signals once.
// host supplies the pages' handlers the step controls act through; the
// controls stay insensitive without it.
func NewFirstRunAssistant(filter func(page, group string) bool, toastAdder ToastAdder, host SetupHost) *FirstRunAssistant {
	assistant := &FirstRunAssistant{
		model:      firstrun.NewAssistantModel(filter),
		store:      firstrun.NewGSettingsStore(),
		host:       host,
		toastAdder: toastAdder,
		filter:     filter,
		liveryRows: make(map[livery.Surface]*setupSwitchRow),
		updateRows: make(map[updateflow.SourceID]*setupSwitchRow),
	}
	assistant.buildUI()
	return assistant
}

func (a *FirstRunAssistant) buildUI() {
	dialog := adw.NewDialog()
	dialog.SetTitle(pageview.WelcomeTitle)
	dialog.SetContentWidth(620)
	dialog.SetContentHeight(600)
	dialog.SetCanClose(true)

	toolbarView := adw.NewToolbarView()
	headerBar := adw.NewHeaderBar()
	toolbarView.AddTopBar(&headerBar.Widget)

	stack := gtk.NewStack()
	stack.SetTransitionType(gtk.StackTransitionTypeSlideLeftRightValue)
	toolbarView.SetContent(&stack.Widget)

	// The assistant's own toast overlay: the window's overlay sits beneath
	// the dialog's backdrop, where a "still loading" answer would be easy to
	// miss.
	toasts := adw.NewToastOverlay()
	toasts.SetChild(&toolbarView.Widget)
	dialog.SetChild(&toasts.Widget)

	// Welcome Hero Screen
	clamp := adw.NewClamp()
	clamp.SetMaximumSize(520)
	clamp.SetTighteningThreshold(400)

	welcomeBox := gtk.NewBox(gtk.OrientationVerticalValue, 14)
	welcomeBox.SetMarginTop(24)
	welcomeBox.SetMarginBottom(24)
	welcomeBox.SetMarginStart(24)
	welcomeBox.SetMarginEnd(24)
	welcomeBox.SetVexpand(true)
	welcomeBox.SetHalign(gtk.AlignCenterValue)
	welcomeBox.SetValign(gtk.AlignCenterValue)

	heroBox := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	heroBox.SetHalign(gtk.AlignCenterValue)

	// Wordmark illustration supporting light/dark theme palettes. The file is
	// chosen in applyWordmark rather than here because the assistant is cached
	// on the window and presented again later, possibly after a theme change.
	wordmarkPic := gtk.NewPicture()
	wordmarkPic.SetCanShrink(true)
	wordmarkPic.SetKeepAspectRatio(true)
	wordmarkPic.SetContentFit(gtk.ContentFitContainValue)
	wordmarkPic.SetHalign(gtk.AlignCenterValue)
	wordmarkPic.SetSizeRequest(280, 80)
	SetAccessibleLabel(wordmarkPic, pageview.WordmarkAccessibleName)
	heroBox.Append(&wordmarkPic.Widget)
	welcomeBox.Append(&heroBox.Widget)

	titleLabel := gtk.NewLabel(pageview.WelcomeTitle)
	titleLabel.AddCssClass("title-1")
	titleLabel.SetHalign(gtk.AlignCenterValue)
	welcomeBox.Append(&titleLabel.Widget)

	subtitleLabel := gtk.NewLabel(pageview.WelcomeSubtitle)
	subtitleLabel.AddCssClass("body")
	subtitleLabel.AddCssClass("dim-label")
	subtitleLabel.SetWrap(true)
	subtitleLabel.SetJustify(gtk.JustifyCenterValue)
	subtitleLabel.SetMaxWidthChars(44)
	subtitleLabel.SetHalign(gtk.AlignCenterValue)
	welcomeBox.Append(&subtitleLabel.Widget)

	actionBox := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	actionBox.SetMarginTop(16)
	actionBox.SetHalign(gtk.AlignCenterValue)

	primaryBtn := gtk.NewButtonWithLabel(pageview.ConfigureEverythingAction)
	primaryBtn.AddCssClass("suggested-action")
	primaryBtn.AddCssClass("pill")
	primaryBtn.SetSizeRequest(240, 42)
	primaryBtn.SetTooltipText(pageview.ConfigureEverythingDescription)
	actionBox.Append(&primaryBtn.Widget)

	secondaryBtn := gtk.NewButtonWithLabel(pageview.GetMovingAction)
	secondaryBtn.AddCssClass("flat")
	secondaryBtn.AddCssClass("pill")
	secondaryBtn.SetSizeRequest(240, 38)
	secondaryBtn.SetTooltipText(pageview.GetMovingDescription())
	actionBox.Append(&secondaryBtn.Widget)

	welcomeBox.Append(&actionBox.Widget)
	clamp.SetChild(&welcomeBox.Widget)
	stack.AddNamed(&clamp.Widget, "welcome")

	// Keyboard focus and default activation set to "Configure Everything"
	dialog.SetDefaultWidget(&primaryBtn.Widget)
	dialog.SetFocus(&primaryBtn.Widget)

	// Configuration Step Screen: a scrolling column of the step's title,
	// description, its controls and one reassurance group, with the
	// Back/Next buttons pinned beneath as a bottom bar so they are reachable
	// without scrolling however many rows a step carries.
	configClamp := adw.NewClamp()
	configClamp.SetMaximumSize(560)
	configBox := gtk.NewBox(gtk.OrientationVerticalValue, 16)
	configBox.SetMarginTop(24)
	configBox.SetMarginBottom(24)
	configBox.SetMarginStart(24)
	configBox.SetMarginEnd(24)

	configStepTitle := gtk.NewLabel("")
	configStepTitle.AddCssClass("title-2")
	configStepTitle.SetHalign(gtk.AlignStartValue)
	configBox.Append(&configStepTitle.Widget)

	configStepDesc := gtk.NewLabel("")
	configStepDesc.AddCssClass("dim-label")
	configStepDesc.SetWrap(true)
	configStepDesc.SetHalign(gtk.AlignStartValue)
	configBox.Append(&configStepDesc.Widget)

	// One stack child per step the floor admits, built once from the same
	// model Present rebuilds: the floor is fixed for the session, so the
	// steps and their choices are too. Not homogeneous: each step takes its
	// own height, so a three-row step does not reserve a longer step's room.
	stepStack := gtk.NewStack()
	stepStack.SetTransitionType(gtk.StackTransitionTypeCrossfadeValue)
	stepStack.SetVhomogeneous(false)
	configBox.Append(&stepStack.Widget)

	infoGroup := adw.NewPreferencesGroup()
	infoRow := adw.NewActionRow()
	infoRow.SetTitle(pageview.ConfigStepInfoTitle)
	infoRow.SetSubtitle(pageview.ConfigStepInfoSubtitle())
	infoGroup.Add(&infoRow.Widget)
	configBox.Append(&infoGroup.Widget)
	configClamp.SetChild(&configBox.Widget)

	scrolled := gtk.NewScrolledWindow()
	scrolled.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scrolled.SetVexpand(true)
	scrolled.SetChild(&configClamp.Widget)

	navBox := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	navBox.SetMarginTop(12)
	navBox.SetMarginBottom(18)
	navBox.SetMarginEnd(24)
	navBox.SetHalign(gtk.AlignEndValue)

	backBtn := gtk.NewButtonWithLabel(pageview.BackAction)
	backBtn.AddCssClass("pill")
	navBox.Append(&backBtn.Widget)

	forwardBtn := gtk.NewButtonWithLabel(pageview.FinishAction)
	forwardBtn.AddCssClass("suggested-action")
	forwardBtn.AddCssClass("pill")
	navBox.Append(&forwardBtn.Widget)

	configView := adw.NewToolbarView()
	configView.SetContent(&scrolled.Widget)
	configView.AddBottomBar(&navBox.Widget)
	stack.AddNamed(&configView.Widget, "config")

	a.dialog = dialog
	a.stack = stack
	a.toasts = toasts
	a.configStepTitle = configStepTitle
	a.configStepDesc = configStepDesc
	a.configStepInfo = infoGroup
	a.stepStack = stepStack
	a.wordmarkPic = wordmarkPic
	a.primaryBtn = primaryBtn
	a.secondaryBtn = secondaryBtn
	a.backBtn = backBtn
	a.forwardBtn = forwardBtn

	a.applyWordmark()
	for _, step := range a.model.Steps() {
		if step.ID == firstrun.StepIDWelcome {
			continue
		}
		a.buildStep(step)
	}

	// Wire signal callbacks once at build time
	primaryClicked := func(_ gtk.Button) {
		a.onConfigure()
	}
	primaryBtn.ConnectClicked(&primaryClicked)

	secondaryClicked := func(_ gtk.Button) {
		a.onGetMoving()
	}
	secondaryBtn.ConnectClicked(&secondaryClicked)

	backClicked := func(_ gtk.Button) {
		a.onBack()
	}
	backBtn.ConnectClicked(&backClicked)

	finishClicked := func(_ gtk.Button) {
		a.onFinish()
	}
	forwardBtn.ConnectClicked(&finishClicked)

	// A close that reached no decision is an intentional dismissal — Escape,
	// the close button, a click outside — and records a skip, preserving a
	// completion an earlier run recorded. A crash records nothing: this
	// handler never runs for one.
	dialogClosed := func(_ adw.Dialog) {
		a.open = false
		if !a.decided {
			a.recordSkip()
		}
	}
	dialog.ConnectClosed(&dialogClosed)
}

// buildStep constructs one step's controls into the step stack, keyed on the
// step ID. Each step is a preferences group of the step's own choices.
func (a *FirstRunAssistant) buildStep(step firstrun.Step) {
	// The step's title and description head the screen above the stack, so
	// the group shows no title of its own: three "Appearance" headings in a
	// 600-pixel dialog read as a defect. The AT-SPI suite finds the showing
	// step by that heading and the dialog's title, both of which showStep
	// sets.
	group := adw.NewPreferencesGroup()
	switch step.ID {
	case firstrun.StepIDTheme:
		for _, choice := range step.Choices {
			surface, ok := liverySurfaceForChoice(choice.ID)
			if !ok {
				continue
			}
			a.buildLiveryChoice(group, surface, choice)
		}
		if a.host != nil {
			a.host.OnLiveryLoaded(a.refreshLiveryRows)
		}
	case firstrun.StepIDApps:
		a.buildBundleChoices(group)
	case firstrun.StepIDUpdates:
		for _, choice := range step.Choices {
			a.buildUpdateChoice(group, choice)
		}
		if a.host != nil {
			a.host.OnUpdateSourcesRendered(a.refreshUpdateRows)
		}
	default:
		return
	}
	a.stepStack.AddNamed(&group.Widget, step.ID)
}

// liverySurfaceForChoice maps an Appearance choice to its Livery surface.
func liverySurfaceForChoice(choiceID string) (livery.Surface, bool) {
	switch choiceID {
	case firstrun.ChoiceIDAppGrid:
		return livery.AppGrid, true
	case firstrun.ChoiceIDFoundation:
		return livery.Panel, true
	case firstrun.ChoiceIDDock:
		return livery.Dock, true
	}
	return 0, false
}

// buildLiveryChoice adds one Appearance switch row. The row reads the Livery
// page's own copy for the surface, so both surfaces describe one setting in
// one voice, and its switch drives the page's switch through the host.
func (a *FirstRunAssistant) buildLiveryChoice(group *adw.PreferencesGroup, surface livery.Surface, choice firstrun.Choice) {
	presentation, _ := pageview.SetupChoiceRow(choice.ID)
	entry := &setupSwitchRow{subtitle: presentation.Subtitle}
	entry.row, entry.toggle = newSwitchRow(presentation, func(state bool) {
		a.onLiveryChoiceToggled(surface, state)
	}, nil)
	SetAccessibleLabel(entry.toggle, presentation.Title)
	entry.row.SetSubtitle(pageview.SetupChoicesLoadingSubtitle)
	group.Add(&entry.row.Widget)
	a.liveryRows[surface] = entry
}

// onLiveryChoiceToggled flips the Livery page's switch for surface. When the
// page cannot take the flip — it has not loaded, or its gate holds a run —
// the assistant's switch is restored and the toast says why.
func (a *FirstRunAssistant) onLiveryChoiceToggled(surface livery.Surface, state bool) {
	if a.suppress {
		return
	}
	if a.host != nil && a.host.SetLiveryEnabled(surface, state) {
		return
	}
	entry := a.liveryRows[surface]
	// The restore runs after this state-set handler returns: setting the
	// switch from inside its own state-set emission re-enters GTK's switch
	// mid-transition.
	sgtk.RunOnMainThread(func() {
		a.suppress = true
		entry.toggle.SetActive(!state)
		a.suppress = false
	})
	a.showToast(pageview.SetupChoiceBusyMessage)
}

// refreshLiveryRows reads every Appearance row's state from the Livery page.
// It runs on the main thread, from OnLiveryLoaded and each time the step is
// shown, and its programmatic restores are suppressed.
func (a *FirstRunAssistant) refreshLiveryRows() {
	if a.host == nil {
		return
	}
	a.suppress = true
	defer func() { a.suppress = false }()
	for surface, entry := range a.liveryRows {
		enabled, available, ready := a.host.LiveryChoice(surface)
		switch {
		case !ready:
			entry.row.SetSubtitle(pageview.SetupChoicesLoadingSubtitle)
		case !available:
			entry.row.SetSubtitle(pageview.SetupChoiceUnavailableSubtitle)
		default:
			entry.row.SetSubtitle(entry.subtitle)
		}
		entry.toggle.SetActive(enabled && available)
		entry.toggle.SetSensitive(ready && available)
	}
}

// buildBundleChoices adds the Apps step's collection group. Discovery is the
// Apps page's, asynchronous, so the group opens with a placeholder row and
// fills in once the host reports the inventory.
func (a *FirstRunAssistant) buildBundleChoices(group *adw.PreferencesGroup) {
	group.SetDescription(bundleview.GroupDescription)
	placeholder := adw.NewActionRow()
	placeholder.SetTitle(pageview.SetupBundlesLoadingTitle)
	group.Add(&placeholder.Widget)
	a.bundlesGroup = group
	a.bundlesPlaceholder = placeholder

	if a.host != nil {
		a.host.OnBundlesLoaded(a.populateBundleRows)
	}
}

// populateBundleRows builds the collection rows once, on the main thread,
// each Install button connected through the shared per-collection gate.
func (a *FirstRunAssistant) populateBundleRows() {
	if a.bundleRowsBuilt || a.host == nil {
		return
	}
	bundles, loaded := a.host.Bundles()
	if !loaded {
		return
	}
	a.bundleRowsBuilt = true
	a.bundlesGroup.Remove(&a.bundlesPlaceholder.Widget)
	presentation := bundleview.Present(len(bundles), "")
	a.bundlesGroup.SetDescription(presentation.Description)
	if len(bundles) == 0 {
		row := adw.NewActionRow()
		row.SetTitle(presentation.PlaceholderTitle)
		row.SetSubtitle(presentation.PlaceholderSubtitle)
		a.bundlesGroup.Add(&row.Widget)
		return
	}
	for _, bundle := range bundles {
		row, installBtn := newBundleRow(bundle)
		a.host.ConnectBundleInstall(bundle, installBtn)
		a.bundlesGroup.Add(&row.Widget)
	}
}

// buildUpdateChoice adds one Update Preferences switch row, bound to the
// source's GSettings key the way the Preferences dialog binds it. Under
// --dry-run nothing is bound: the switch shows the stored value and a toggle
// logs the write it would make, so a preview run persists nothing.
func (a *FirstRunAssistant) buildUpdateChoice(group *adw.PreferencesGroup, choice firstrun.Choice) {
	preference, ok := pageview.UpdateSourcePreferenceByKey(choice.ID)
	if !ok {
		return
	}
	presentation, _ := pageview.SetupChoiceRow(choice.ID)
	entry := &setupSwitchRow{subtitle: presentation.Subtitle}
	entry.row, entry.toggle = newSwitchRow(presentation, func(state bool) {
		if a.suppress {
			return
		}
		if dryrun.Enabled() {
			log.Printf("[DRY-RUN] would set %s %s=%t", settings.SchemaID, preference.Key, state)
		}
	}, nil)
	SetAccessibleLabel(entry.toggle, presentation.Title)
	group.Add(&entry.row.Widget)
	a.updateRows[preference.ID] = entry

	var store *settings.Store
	if a.host != nil {
		store = a.host.UpdatePreferences()
	}
	if store != nil {
		if dryrun.Enabled() {
			// Seeding the switch emits state-set exactly as a click does,
			// and a seed is not a write the dry run would make.
			a.suppress = true
			entry.toggle.SetActive(preferenceValue(store.Values(), preference.ID))
			a.suppress = false
		} else {
			store.BindBoolean(preference.Key, &entry.toggle.Object)
		}
	}
}

// preferenceValue reads one source's stored preference.
func preferenceValue(values userprefs.Values, id updateflow.SourceID) bool {
	switch id {
	case updateflow.OperatingSystem:
		return values.OperatingSystem
	case updateflow.Applications:
		return values.Applications
	case updateflow.DeveloperTools:
		return values.DeveloperTools
	case updateflow.SystemComponents:
		return values.SystemComponents
	}
	return false
}

// refreshUpdateRows applies the update shell's latest availability to every
// Update Preferences row: a source the administrator disabled or the host
// cannot back stays insensitive and says so, exactly as in Preferences.
func (a *FirstRunAssistant) refreshUpdateRows() {
	if a.host == nil {
		return
	}
	for id, entry := range a.updateRows {
		state, ready := a.host.UpdateSource(id)
		var states []updateflow.SourceState
		if ready {
			states = []updateflow.SourceState{state}
		}
		subtitle := pageview.UpdateSourcePreferenceSubtitle(states, ready, id)
		if subtitle == "" {
			subtitle = entry.subtitle
		}
		entry.row.SetSubtitle(subtitle)
		entry.toggle.SetSensitive(pageview.UpdateSourcePreferenceSensitive(states, ready, id))
	}
}

// showToast shows a short message inside the dialog.
func (a *FirstRunAssistant) showToast(message string) {
	if a.toasts == nil {
		return
	}
	toast := adw.NewToast(message)
	toast.SetUseMarkup(false)
	toast.SetTimeout(3)
	a.toasts.AddToast(toast)
}

// applyWordmark points the wordmark picture at the variant matching the
// current light/dark palette.
//
// The assistant is cached on the window and presented repeatedly, so the
// variant cannot be decided once at build time: a theme change between two
// presentations would otherwise leave dark lettering on a dark background.
func (a *FirstRunAssistant) applyWordmark() {
	if a.wordmarkPic == nil {
		return
	}
	isDark := false
	if sm := adw.StyleManagerGetDefault(); sm != nil {
		isDark = sm.GetDark()
	}
	vm := pageview.NewWelcomeViewModel(isDark)
	path, err := firstrun.AssetPath(vm.WordmarkAsset)
	if err != nil {
		log.Printf("firstrun: resolving wordmark asset: %v", err)
		return
	}
	a.wordmarkPic.SetFilename(path)
}

// showStep displays a configuration step: its title becomes the dialog's,
// its controls are refreshed from the pages that own them, and the forward
// button is labeled for what clicking it actually does next.
func (a *FirstRunAssistant) showStep(step firstrun.Step) {
	a.dialog.SetTitle(step.Title)
	a.configStepTitle.SetText(step.Title)
	a.configStepDesc.SetText(step.Description)
	a.forwardBtn.SetLabel(pageview.StepForwardAction(a.model.ForwardFinishes()))
	a.backBtn.SetLabel(pageview.BackAction)
	switch step.ID {
	case firstrun.StepIDTheme:
		a.refreshLiveryRows()
	case firstrun.StepIDApps:
		a.populateBundleRows()
	case firstrun.StepIDUpdates:
		a.refreshUpdateRows()
	}
	a.stepStack.SetVisibleChildName(step.ID)
	// Return advances: the forward button is both the default widget and,
	// on entering a step, the focus. Without moving focus it stays on the
	// welcome screen's hidden button, and Return re-activates that.
	a.dialog.SetDefaultWidget(&a.forwardBtn.Widget)
	a.dialog.SetFocus(&a.forwardBtn.Widget)
}

// showWelcome returns the dialog to the hero screen.
func (a *FirstRunAssistant) showWelcome() {
	a.dialog.SetTitle(pageview.WelcomeTitle)
	a.stack.SetVisibleChildName("welcome")
	a.dialog.SetDefaultWidget(&a.primaryBtn.Widget)
	a.dialog.SetFocus(&a.primaryBtn.Widget)
}

func (a *FirstRunAssistant) onConfigure() {
	next, dismissed, disp := a.model.SelectFlow(firstrun.FlowChoiceConfigure)
	if dismissed {
		if disp == firstrun.DispositionCompleted {
			a.decided = true
			a.recordCompletion()
		}
		a.dialog.Close()
		return
	}
	if next != nil {
		a.showStep(*next)
		a.stack.SetVisibleChildName("config")
	}
}

func (a *FirstRunAssistant) onGetMoving() {
	a.model.SelectFlow(firstrun.FlowChoiceGetMoving)
	a.decided = true
	a.recordSkip()
	a.dialog.Close()

	if a.toastAdder != nil {
		a.toastAdder.ShowToast(pageview.GetMovingToastMessage())
	}
}

// recordCompletion persists a finished setup off the GTK main thread.
//
// The write spawns `gsettings`, and a click handler runs on the GTK main
// thread, which internal/livery's convention keeps subprocess work off.
// Nothing on screen depends on the result, so the dialog closes without
// waiting for it.
func (a *FirstRunAssistant) recordCompletion() {
	store := a.store
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), firstrun.WriteTimeout)
		defer cancel()
		if err := store.SetDisposition(ctx, firstrun.DispositionCompleted); err != nil {
			logDispositionError("completed", err)
		}
	}()
}

// recordSkip persists a skip off the GTK main thread, for the same reason as
// recordCompletion and more so, since the read and the write are two spawns.
// firstrun.RecordSkip keeps a recorded completion, so a Get Moving or a
// dismissal after setup finished demotes nothing.
func (a *FirstRunAssistant) recordSkip() {
	store := a.store
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), firstrun.WriteTimeout)
		defer cancel()
		if _, _, err := firstrun.RecordSkip(ctx, store); err != nil {
			logDispositionError("skipped", err)
		}
	}()
}

// logDispositionError reports a failed disposition write, naming the missing
// schema for what it is.
//
// firstrun.ShouldPresent answers false for that condition, so the assistant
// does not return uninvited on every launch of an install whose schema was
// never compiled; the log line says why nothing was recorded rather than
// leaving a bare gsettings failure behind.
func logDispositionError(state string, err error) {
	if errors.Is(err, firstrun.ErrSchemaMissing) {
		log.Printf("firstrun: not recording %s disposition: %v; reinstall the application so its settings schema is compiled (in a source checkout, run `make schemas`)", state, err)
		return
	}
	log.Printf("firstrun: saving %s disposition: %v", state, err)
}

func (a *FirstRunAssistant) onBack() {
	prev, ok := a.model.Previous()
	if ok && prev != nil {
		if a.model.IsWelcome() {
			a.showWelcome()
		} else {
			a.showStep(*prev)
		}
	}
}

func (a *FirstRunAssistant) onFinish() {
	step, done := a.model.Advance()
	if done {
		a.decided = true
		a.recordCompletion()
		a.dialog.Close()
		if a.toastAdder != nil {
			a.toastAdder.ShowToast(pageview.SetupCompletedMessage)
		}
		return
	}
	a.showStep(step)
}

// Present displays the assistant dialog attached to the given parent widget.
//
// A request that arrives while the assistant is already open re-presents it
// as it stands. Rebuilding the model there would discard the step the user is
// on, which is what a second Setup Assistant… activation, or a `chairlift
// --setup` aimed at the running instance, would otherwise do.
func (a *FirstRunAssistant) Present(parent *gtk.Widget) {
	if !a.open {
		a.model = firstrun.NewAssistantModel(a.filter)
		a.decided = false
		a.applyWordmark()
		a.showWelcome()
		a.open = true
	}
	a.dialog.Present(parent)
}
