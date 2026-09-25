package views

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/commands"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/notify"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/userprefs"
	"github.com/projectbluefin/chairlift/internal/views/updatepresent"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// UpdateShell is the reusable status-first view for unified updates.
type UpdateShell struct {
	coordinator   *updateflow.Coordinator
	preferences   func() userprefs.Values
	configured    func() map[updateflow.SourceID]bool
	toasts        ToastAdder
	snapshot      updateflow.Snapshot
	sources       []updateflow.SourceState
	lastPhase     updateflow.Phase
	havePhase     bool
	sourceRows    map[updateflow.SourceID]*sourceRow
	compactMode   bool
	lifecycle     context.Context
	cancel        context.CancelFunc
	closed        atomic.Bool
	operationMu   sync.Mutex
	mutation      atomic.Bool
	sourcesReady  bool
	closeBlocked  bool
	toolbarView   *adw.ToolbarView
	toastOverlay  *adw.ToastOverlay
	statusPage    *adw.StatusPage
	refresh       *gtk.Button
	primary       *gtk.Button
	progress      *gtk.ProgressBar
	banner        *adw.Banner
	sourceGroup   *adw.PreferencesGroup
	breakpointBin *adw.BreakpointBin
	// content is the vertical box inside the shell's clamp and scroller.
	// SetSecondaryContent appends to it rather than building a second
	// scroller, so the whole page scrolls as one.
	content *gtk.Box
	// secondary is whatever SetSecondaryContent last parented, kept so a
	// repeat call replaces it instead of adding a second copy.
	secondary        *gtk.Widget
	onUpdateFinished func(updateflow.Snapshot)
}

// SetOnUpdateFinished registers a callback invoked on the GTK main thread
// after an update run finishes.
func (s *UpdateShell) SetOnUpdateFinished(fn func(updateflow.Snapshot)) {
	if s != nil {
		s.onUpdateFinished = fn
	}
}

// NewUpdateShell builds a status-first update surface and starts its initial
// check before returning.
func NewUpdateShell(
	coordinator *updateflow.Coordinator,
	preferences func() userprefs.Values,
	configured func() map[updateflow.SourceID]bool,
	toasts ToastAdder,
) *UpdateShell {
	lifecycle, cancel := context.WithCancel(context.Background())
	s := &UpdateShell{
		coordinator: coordinator,
		preferences: preferences,
		configured:  configured,
		toasts:      toasts,
		sourceRows:  make(map[updateflow.SourceID]*sourceRow),
		lifecycle:   lifecycle,
		cancel:      cancel,
	}
	s.build()
	s.Render(updateflow.Snapshot{Phase: updateflow.PhaseChecking})
	s.StartCheck()
	return s
}

// Widget returns the shell's top-level toast overlay.
func (s *UpdateShell) Widget() *gtk.Widget {
	if s == nil || s.toastOverlay == nil {
		return nil
	}
	return &s.toastOverlay.Widget
}

// ToolbarView returns the shell's inner toolbar view.
func (s *UpdateShell) ToolbarView() *adw.ToolbarView {
	if s == nil {
		return nil
	}
	return s.toolbarView
}

// SetSecondaryContent mounts a preferences page below the update sources,
// inside the shell's own clamp and scroller. It exists because the Updates
// destination is this shell: everything the Updates page still owns — the
// system-version readout, and the controls that replace the operating
// system — is built by buildUpdatesPage and would otherwise never be
// mounted (issue #250).
//
// A nil page is a no-op, and mounting the same page twice does nothing the
// second time: an AdwPreferencesPage that is already parented misbehaves
// when it is added again. Nothing is connected here; the page's own handlers
// were connected once, at build time.
func (s *UpdateShell) SetSecondaryContent(page *adw.PreferencesPage) {
	if s == nil || s.content == nil || page == nil {
		return
	}
	widget := &page.Widget
	if s.secondary == widget {
		return
	}
	if s.secondary != nil {
		s.content.Remove(s.secondary)
		s.secondary = nil
	}
	// views.createPage builds every preferences page inside a scroller of
	// its own, and GtkScrolledWindow wraps a child that is not GtkScrollable
	// — an AdwPreferencesPage is not — in a GtkViewport. So the page's
	// parent here is that viewport, and clearing the viewport's child is the
	// detach that also clears the viewport's own child pointer; unparenting
	// the page directly would leave that pointer dangling.
	if parent := widget.GetParent(); parent != nil {
		gtk.ViewportNewFromInternalPtr(parent.GoPointer()).SetChild(nil)
	}
	s.content.Append(widget)
	s.secondary = widget
}

// StartCheck starts a generation-guarded check away from the GTK thread.
func (s *UpdateShell) StartCheck() {
	if s == nil {
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.coordinator == nil || !updatepresent.CanStartOperation(s.Busy(), s.closed.Load()) {
		if !s.closed.Load() && s.refresh != nil && s.Busy() {
			s.refresh.SetSensitive(false)
		}
		return
	}
	ctx, cancel, ok := s.operationContext()
	if !ok {
		return
	}
	s.sourcesReady = false
	preferences := s.currentPreferences()
	configured := s.currentConfiguration()
	go func() {
		defer cancel()
		s.coordinator.Check(ctx, preferences, configured, s.publish)
	}()
}

// StartUpdate starts the current snapshot's serial mutation away from the GTK
// thread.
func (s *UpdateShell) StartUpdate() {
	if s == nil {
		return
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.coordinator == nil || !updatepresent.CanStartOperation(s.Busy(), s.closed.Load()) {
		if !s.closed.Load() && s.refresh != nil && s.Busy() {
			s.refresh.SetSensitive(false)
		}
		return
	}
	if !s.mutation.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel, ok := s.operationContext()
	if !ok {
		s.mutation.Store(false)
		return
	}
	current := cloneSnapshot(s.snapshot)
	preferences := s.currentPreferences()
	if s.refresh != nil {
		s.refresh.SetSensitive(false)
	}
	if s.primary != nil {
		s.primary.SetSensitive(false)
	}
	go func() {
		defer cancel()
		defer s.mutation.Store(false)
		// The coordinator publishes synchronously from this goroutine, so
		// recording the last snapshot here needs no additional lock.
		var final updateflow.Snapshot
		s.coordinator.UpdateAll(ctx, current, preferences, func(snapshot updateflow.Snapshot) {
			final = snapshot
			s.publish(snapshot)
		})
		s.notifyUpdateComplete(final)
		if s.onUpdateFinished != nil {
			sgtk.RunOnMainThread(func() {
				if s.onUpdateFinished != nil {
					s.onUpdateFinished(final)
				}
			})
		}
	}()
}

// StartRestart restarts the machine so a staged update takes effect. It is
// the only privileged action this shell performs directly; everything else
// it drives goes through the coordinator's own providers.
func (s *UpdateShell) StartRestart() {
	if s == nil {
		return
	}
	// On a live run this process is about to go away, and a second press
	// would only queue a redundant PolicyKit prompt.
	if s.primary != nil {
		s.primary.SetSensitive(false)
	}
	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		err := ublue.Restart(ctx)

		sgtk.RunOnMainThread(func() {
			if s.primary != nil {
				s.primary.SetSensitive(true)
			}
			if s.toasts == nil {
				return
			}
			if err != nil {
				s.toasts.ShowErrorToast(fmt.Sprintf("Restart failed: %v", err))
				return
			}
			if dryrun.Enabled() {
				s.toasts.ShowToast("[DRY-RUN] Preview: the system would restart now — no changes made")
			}
		})
	}()
}

// notifyUpdateComplete sends the single desktop notification ChairLift
// emits. An update run is the one operation long enough that the user may
// have stepped away; every other action completes in view and already has a
// toast. See internal/notify.
func (s *UpdateShell) notifyUpdateComplete(final updateflow.Snapshot) {
	if s == nil || s.toasts == nil || final.Preview || !updatepresent.ShouldPublish(s.closed.Load()) {
		return
	}

	skipped := 0
	for _, source := range final.Sources {
		if !source.Enabled || !source.Configured || !source.Available {
			skipped++
		}
	}
	notification := notify.UpdateAllComplete(
		len(final.CompletedSources),
		len(final.FailedSources),
		skipped,
		final.RestartRequired(),
	)

	sgtk.RunOnMainThread(func() {
		if !updatepresent.ShouldPublish(s.closed.Load()) {
			return
		}
		s.toasts.NotifyBackground(
			notification.Title,
			notification.Body,
			notification.Urgency == notify.UrgencyHigh,
		)
	})
}

// Busy reports whether the coordinator is currently mutating updates.
func (s *UpdateShell) Busy() bool {
	return s != nil &&
		(s.mutation.Load() || (s.coordinator != nil && s.coordinator.Busy()))
}

// RevealBusyBanner keeps the user informed when a close request is rejected
// while an update mutation is still running.
func (s *UpdateShell) RevealBusyBanner() {
	if s == nil || s.banner == nil {
		return
	}
	s.closeBlocked = true
	s.banner.SetTitle("Updates are still in progress…")
	s.banner.SetRevealed(true)
}

// Render applies one immutable coordinator snapshot on the GTK main thread.
func (s *UpdateShell) Render(snapshot updateflow.Snapshot) {
	if s == nil || !updatepresent.ShouldPublish(s.closed.Load()) {
		return
	}
	snapshot = cloneSnapshot(snapshot)
	s.snapshot = snapshot
	s.sources = cloneSourceStates(snapshot.Sources)
	if snapshot.Phase != updateflow.PhaseChecking {
		s.sourcesReady = true
	}

	presentation := updatepresent.Snapshot(snapshot)
	s.statusPage.SetIconName(presentation.Icon)
	s.statusPage.SetTitle(presentation.Title)
	s.statusPage.SetDescription(presentation.Description)
	s.renderPrimaryAction(presentation)
	s.renderProgress(snapshot)
	if s.refresh != nil {
		s.refresh.SetSensitive(updatepresent.CanStartOperation(s.Busy(), s.closed.Load()))
	}
	if s.closeBlocked && s.Busy() {
		s.banner.SetTitle("Updates are still in progress…")
		s.banner.SetRevealed(true)
	} else {
		s.closeBlocked = false
		s.banner.SetTitle(presentation.Banner)
		s.banner.SetRevealed(presentation.Banner != "")
	}
	s.renderSources(snapshot.Sources)
	if s.toasts != nil {
		s.toasts.SetUpdateBadge(snapshot.TotalUpdates)
	}
	if !s.havePhase || s.lastPhase != snapshot.Phase {
		s.statusPage.Announce(presentation.Title, gtk.AccessibleAnnouncementPriorityMediumValue)
		s.lastPhase = snapshot.Phase
		s.havePhase = true
	}
}

// Sources returns a defensive copy of the latest source inventory.
func (s *UpdateShell) Sources() []updateflow.SourceState {
	if s == nil {
		return nil
	}
	return cloneSourceStates(s.sources)
}

// SourcesReady reports whether the initial availability check has completed.
func (s *UpdateShell) SourcesReady() bool {
	return s != nil && s.sourcesReady
}

func (s *UpdateShell) build() {
	s.toolbarView = adw.NewToolbarView()
	header := adw.NewHeaderBar()
	header.SetTitleWidget(&gtk.NewLabel(branding.AppName).Widget)
	s.refresh = newIconButton("view-refresh-symbolic", "Refresh")
	s.refresh.SetActionName(commands.CheckAction)
	header.PackStart(&s.refresh.Widget)

	menu := gio.NewMenu()
	menu.Append("Preferences", commands.PreferencesAction)
	menu.Append("Keyboard shortcuts", commands.ShowShortcutsAction)
	menu.Append("Help", commands.HelpAction)
	menu.Append("About "+branding.AppName, commands.ShowAboutAction)
	menu.Append("Quit", commands.QuitAction)
	menuButton := gtk.NewMenuButton()
	menuButton.SetIconName("open-menu-symbolic")
	menuButton.SetTooltipText("Main menu")
	SetAccessibleLabel(menuButton, "Main menu")
	menuButton.SetMenuModel(&menu.MenuModel)
	header.PackEnd(&menuButton.Widget)
	s.toolbarView.AddTopBar(&header.Widget)

	content := gtk.NewBox(gtk.OrientationVerticalValue, 24)
	content.SetMarginTop(24)
	content.SetMarginBottom(24)
	content.SetMarginStart(12)
	content.SetMarginEnd(12)
	s.content = content

	s.statusPage = adw.NewStatusPage()
	s.statusPage.SetVexpand(false)
	controls := gtk.NewBox(gtk.OrientationVerticalValue, 12)
	controls.SetHalign(gtk.AlignCenterValue)

	s.primary = gtk.NewButtonWithLabel("")
	s.primary.AddCssClass("pill")
	s.primary.SetVisible(false)
	primaryClicked := func(_ gtk.Button) {
		switch s.snapshot.Action {
		case updateflow.ActionCheck:
			s.StartCheck()
		case updateflow.ActionUpdateAll, updateflow.ActionRetryFailed:
			s.StartUpdate()
		case updateflow.ActionRestart:
			s.StartRestart()
		}
	}
	s.primary.ConnectClicked(&primaryClicked)
	controls.Append(&s.primary.Widget)

	s.progress = gtk.NewProgressBar()
	s.progress.SetShowText(false)
	s.progress.SetVisible(false)
	s.progress.SetHexpand(true)
	controls.Append(&s.progress.Widget)
	s.statusPage.SetChild(&controls.Widget)
	content.Append(&s.statusPage.Widget)

	s.banner = adw.NewBanner("")
	s.banner.SetRevealed(false)
	content.Append(&s.banner.Widget)

	s.sourceGroup = adw.NewPreferencesGroup()
	s.sourceGroup.SetTitle("Update sources")
	s.sourceGroup.SetVisible(false)
	content.Append(&s.sourceGroup.Widget)

	s.breakpointBin = adw.NewBreakpointBin()
	// AdwBreakpointBin requires a minimum size: without one libadwaita warns
	// on every allocation and the max-width condition below cannot resolve,
	// so the compact layout never applies reliably. Both values sit under
	// the 600px condition so the breakpoint can actually be reached.
	s.breakpointBin.SetSizeRequest(360, 200)
	s.breakpointBin.SetChild(&content.Widget)
	compactBreakpoint := adw.NewBreakpoint(adw.BreakpointConditionParse("max-width: 600px"))
	applyCompact := func(_ adw.Breakpoint) {
		s.setCompactRows(true)
	}
	unapplyCompact := func(_ adw.Breakpoint) {
		s.setCompactRows(false)
	}
	compactBreakpoint.ConnectApply(&applyCompact)
	compactBreakpoint.ConnectUnapply(&unapplyCompact)
	s.breakpointBin.AddBreakpoint(compactBreakpoint)

	scrolled := gtk.NewScrolledWindow()
	scrolled.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scrolled.SetMinContentWidth(360)
	scrolled.SetVexpand(true)
	clamp := adw.NewClamp()
	clamp.SetMaximumSize(720)
	clamp.SetChild(&s.breakpointBin.Widget)
	scrolled.SetChild(&clamp.Widget)
	s.toolbarView.SetContent(&scrolled.Widget)

	s.toastOverlay = adw.NewToastOverlay()
	s.toastOverlay.SetChild(&s.toolbarView.Widget)
	destroyed := func(_ gtk.Widget) {
		s.dispose()
	}
	s.toastOverlay.ConnectDestroy(&destroyed)
}

func (s *UpdateShell) publish(snapshot updateflow.Snapshot) {
	if s == nil || !updatepresent.ShouldPublish(s.closed.Load()) {
		return
	}
	snapshot = cloneSnapshot(snapshot)
	sgtk.RunOnMainThread(func() {
		if !updatepresent.ShouldPublish(s.closed.Load()) {
			return
		}
		s.Render(snapshot)
	})
}

func (s *UpdateShell) renderPrimaryAction(presentation updatepresent.Presentation) {
	s.primary.SetVisible(presentation.ShowAction)
	s.primary.SetLabel(presentation.ActionLabel)
	s.primary.RemoveCssClass("suggested-action")
	s.primary.RemoveCssClass("destructive-action")
	if presentation.ActionStyle != "" {
		s.primary.AddCssClass(presentation.ActionStyle)
	}
	s.primary.SetSensitive(presentation.ShowAction &&
		updatepresent.CanStartOperation(s.Busy(), s.closed.Load()))
}

func (s *UpdateShell) renderProgress(snapshot updateflow.Snapshot) {
	visible := updatepresent.ShowProgress(snapshot.Phase)
	s.progress.SetVisible(visible)
	if visible {
		s.progress.Pulse()
	}
}

func (s *UpdateShell) renderSources(states []updateflow.SourceState) {
	seen := make(map[updateflow.SourceID]bool, len(states))
	for _, state := range states {
		seen[state.ID] = true
		row, ok := s.sourceRows[state.ID]
		if !ok {
			row = newSourceRow(state)
			row.setCompact(s.compactMode)
			s.sourceRows[state.ID] = row
			s.sourceGroup.Add(&row.row.Widget)
		} else {
			row.render(state)
		}
	}
	for id, row := range s.sourceRows {
		if !seen[id] {
			s.sourceGroup.Remove(&row.row.Widget)
			delete(s.sourceRows, id)
		}
	}
	s.sourceGroup.SetVisible(len(s.sourceRows) > 0)
}

func (s *UpdateShell) setCompactRows(compact bool) {
	s.compactMode = compact
	for _, row := range s.sourceRows {
		row.setCompact(compact)
	}
}

func (s *UpdateShell) operationContext() (context.Context, context.CancelFunc, bool) {
	if s.closed.Load() || s.lifecycle == nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(s.lifecycle)
	return ctx, cancel, true
}

func (s *UpdateShell) dispose() {
	if s == nil || s.closed.Swap(true) {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *UpdateShell) currentPreferences() userprefs.Values {
	if s.preferences == nil {
		return userprefs.Values{}
	}
	return s.preferences()
}

func (s *UpdateShell) currentConfiguration() map[updateflow.SourceID]bool {
	if s.configured == nil {
		return nil
	}
	values := s.configured()
	if values == nil {
		return nil
	}
	cloned := make(map[updateflow.SourceID]bool, len(values))
	for id, enabled := range values {
		cloned[id] = enabled
	}
	return cloned
}
