package views

import (
	"context"
	"errors"
	"log"

	"github.com/projectbluefin/chairlift/internal/firstrun"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// FirstRunAssistant encapsulates the transient onboarding assistant dialog.
type FirstRunAssistant struct {
	dialog          *adw.Dialog
	stack           *gtk.Stack
	model           *firstrun.AssistantModel
	store           firstrun.Store
	toastAdder      ToastAdder
	filter          func(page, group string) bool
	configStepTitle *gtk.Label
	configStepDesc  *gtk.Label
	wordmarkPic     *gtk.Picture
	primaryBtn      *gtk.Button
	secondaryBtn    *gtk.Button
	backBtn         *gtk.Button
	forwardBtn      *gtk.Button
}

// NewFirstRunAssistant constructs the assistant and wires all signals once.
func NewFirstRunAssistant(filter func(page, group string) bool, toastAdder ToastAdder) *FirstRunAssistant {
	assistant := &FirstRunAssistant{
		model:      firstrun.NewAssistantModel(filter),
		store:      firstrun.NewGSettingsStore(),
		toastAdder: toastAdder,
		filter:     filter,
	}
	assistant.buildUI()
	return assistant
}

func (a *FirstRunAssistant) buildUI() {
	dialog := adw.NewDialog()
	dialog.SetTitle(pageview.WelcomeTitle)
	dialog.SetContentWidth(620)
	dialog.SetContentHeight(560)
	dialog.SetCanClose(true)

	toolbarView := adw.NewToolbarView()
	headerBar := adw.NewHeaderBar()
	toolbarView.AddTopBar(&headerBar.Widget)

	stack := gtk.NewStack()
	stack.SetTransitionType(gtk.StackTransitionTypeSlideLeftRightValue)
	toolbarView.SetContent(&stack.Widget)
	dialog.SetChild(&toolbarView.Widget)

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

	// Dinosaur mascot illustration
	dinoPath, _ := firstrun.AssetPath(firstrun.AssetDinosaur)
	if dinoPath != "" {
		dinoPic := gtk.NewPictureForFilename(dinoPath)
		dinoPic.SetCanShrink(true)
		dinoPic.SetKeepAspectRatio(true)
		dinoPic.SetContentFit(gtk.ContentFitContainValue)
		dinoPic.SetHalign(gtk.AlignCenterValue)
		dinoPic.SetSizeRequest(160, 110)
		heroBox.Append(&dinoPic.Widget)
	}

	// Wordmark illustration supporting light/dark theme palettes. The file is
	// chosen in applyWordmark rather than here because the assistant is cached
	// on the window and presented again later, possibly after a theme change.
	wordmarkPic := gtk.NewPicture()
	wordmarkPic.SetCanShrink(true)
	wordmarkPic.SetKeepAspectRatio(true)
	wordmarkPic.SetContentFit(gtk.ContentFitContainValue)
	wordmarkPic.SetHalign(gtk.AlignCenterValue)
	wordmarkPic.SetSizeRequest(260, 52)
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

	// Configuration Step Screen
	configClamp := adw.NewClamp()
	configClamp.SetMaximumSize(520)
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

	stepGroup := adw.NewPreferencesGroup()
	infoRow := adw.NewActionRow()
	infoRow.SetTitle(pageview.ConfigStepInfoTitle)
	infoRow.SetSubtitle(pageview.ConfigStepInfoSubtitle())
	stepGroup.Add(&infoRow.Widget)
	configBox.Append(&stepGroup.Widget)

	navBox := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	navBox.SetMarginTop(20)
	navBox.SetHalign(gtk.AlignEndValue)

	backBtn := gtk.NewButtonWithLabel(pageview.BackAction)
	backBtn.AddCssClass("pill")
	navBox.Append(&backBtn.Widget)

	forwardBtn := gtk.NewButtonWithLabel(pageview.FinishAction)
	forwardBtn.AddCssClass("suggested-action")
	forwardBtn.AddCssClass("pill")
	navBox.Append(&forwardBtn.Widget)

	configBox.Append(&navBox.Widget)
	configClamp.SetChild(&configBox.Widget)
	stack.AddNamed(&configClamp.Widget, "config")

	a.dialog = dialog
	a.stack = stack
	a.configStepTitle = configStepTitle
	a.configStepDesc = configStepDesc
	a.wordmarkPic = wordmarkPic
	a.primaryBtn = primaryBtn
	a.secondaryBtn = secondaryBtn
	a.backBtn = backBtn
	a.forwardBtn = forwardBtn

	a.applyWordmark()

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

// showStep displays a configuration step and labels the forward button for
// what clicking it actually does next.
func (a *FirstRunAssistant) showStep(step firstrun.Step) {
	a.configStepTitle.SetText(step.Title)
	a.configStepDesc.SetText(step.Description)
	a.forwardBtn.SetLabel(pageview.StepForwardAction(a.model.ForwardFinishes()))
}

func (a *FirstRunAssistant) onConfigure() {
	next, dismissed, disp := a.model.SelectFlow(firstrun.FlowChoiceConfigure)
	if dismissed {
		if disp == firstrun.DispositionCompleted {
			if err := a.store.SetDisposition(context.Background(), firstrun.DispositionCompleted); err != nil {
				logDispositionError("completed", err)
			}
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
	a.dialog.Close()

	// The assistant is reachable again after setup finished, so a skip here
	// must not overwrite a recorded completion with a weaker state.
	ctx := context.Background()
	current, err := a.store.GetDisposition(ctx)
	if err != nil {
		current = firstrun.DispositionNotAddressed
	}
	if next := firstrun.SkipPreserving(current); next != current {
		if err := a.store.SetDisposition(ctx, next); err != nil {
			logDispositionError("skipped", err)
		}
	}

	if a.toastAdder != nil {
		a.toastAdder.ShowToast(pageview.GetMovingToastMessage())
	}
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
		log.Printf("firstrun: not recording %s disposition: %v; run `make schemas` or install the application to compile the settings schema", state, err)
		return
	}
	log.Printf("firstrun: saving %s disposition: %v", state, err)
}

func (a *FirstRunAssistant) onBack() {
	prev, ok := a.model.Previous()
	if ok && prev != nil {
		if a.model.IsWelcome() {
			a.stack.SetVisibleChildName("welcome")
			a.dialog.SetDefaultWidget(&a.primaryBtn.Widget)
			a.dialog.SetFocus(&a.primaryBtn.Widget)
		} else {
			a.showStep(*prev)
		}
	}
}

func (a *FirstRunAssistant) onFinish() {
	step, done := a.model.Advance()
	if done {
		if err := a.store.SetDisposition(context.Background(), firstrun.DispositionCompleted); err != nil {
			logDispositionError("completed", err)
		}
		a.dialog.Close()
		if a.toastAdder != nil {
			a.toastAdder.ShowToast(pageview.SetupCompletedMessage)
		}
		return
	}
	a.showStep(step)
}

// Present displays the assistant dialog attached to the given parent widget.
func (a *FirstRunAssistant) Present(parent *gtk.Widget) {
	a.model = firstrun.NewAssistantModel(a.filter)
	a.applyWordmark()
	a.stack.SetVisibleChildName("welcome")
	a.dialog.SetDefaultWidget(&a.primaryBtn.Widget)
	a.dialog.SetFocus(&a.primaryBtn.Widget)
	a.dialog.Present(parent)
}
