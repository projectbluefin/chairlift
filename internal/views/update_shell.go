package views

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
	"sync"
	"sync/atomic"

	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/commands"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/firstrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/notify"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/updateproviders"
	"github.com/projectbluefin/chairlift/internal/userprefs"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/trustmsg"
	"github.com/projectbluefin/chairlift/internal/views/updatepresent"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// UpdateShell is the reusable status-first view for unified updates.
type UpdateShell struct {
	coordinator         *updateflow.Coordinator
	preferences         func() userprefs.Values
	policy              func() map[updateflow.SourceID]updateflow.Policy
	toasts              ToastAdder
	snapshot            updateflow.Snapshot
	sources             []updateflow.SourceState
	lastPhase           updateflow.Phase
	havePhase           bool
	sourceRows          map[updateflow.SourceID]*sourceRow
	compactMode         bool
	lifecycle           context.Context
	cancel              context.CancelFunc
	closed              atomic.Bool
	operationMu         sync.Mutex
	mutation            atomic.Bool
	restartInFlight     atomic.Bool
	sourcesReady        bool
	closeBlocked        bool
	toolbarView         *adw.ToolbarView
	toastOverlay        *adw.ToastOverlay
	header              *gtk.Box
	statusLine          *gtk.Label
	statusDetail        *gtk.Label
	refresh             *gtk.Button
	primary             *gtk.Button
	progress            *gtk.ProgressBar
	progressPulse       glib.SourceFunc
	progressTimer       uint32
	wordmark            *gtk.Picture
	styleManager        *adw.StyleManager
	themeHandler        uint32
	updateButtons       buttonRoute
	trustGroupAvailable bool
	banner              *adw.Banner
	systemGroup         *adw.PreferencesGroup
	appsGroup           *adw.PreferencesGroup
	breakpointBin       *adw.BreakpointBin
	// page holds the shell's inset sections and, beside them, the secondary
	// preferences page, inside the shell's clamp and scroller.
	// SetSecondaryContent appends to it rather than building a second
	// scroller, so the whole page scrolls as one.
	page *gtk.Box
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
// check before returning. policy reports, per source, the administrator's
// configuration and the host capability floor as separate facts, so a source
// the host cannot back is not reported as disabled by the administrator.
func NewUpdateShell(
	coordinator *updateflow.Coordinator,
	preferences func() userprefs.Values,
	policy func() map[updateflow.SourceID]updateflow.Policy,
	toasts ToastAdder,
) *UpdateShell {
	lifecycle, cancel := context.WithCancel(context.Background())
	s := &UpdateShell{
		coordinator: coordinator,
		preferences: preferences,
		policy:      policy,
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
	if s == nil || s.page == nil || page == nil {
		return
	}
	widget := &page.Widget
	if s.secondary == widget {
		return
	}
	if s.secondary != nil {
		s.page.Remove(s.secondary)
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
	growWithContent(widget)
	unclampPreferencesPage(widget)
	s.page.Append(widget)
	s.secondary = widget
}

// unclampPreferencesPage lets a mounted AdwPreferencesPage take the shell's
// width. The page clamps its groups again inside the shell's own clamp, so
// its rows came out narrower and further inset than the source rows above
// them. Its built-in 12px side padding is what the shell's content inset
// matches; only the second clamp is lifted. Any other child shape is left
// untouched.
func unclampPreferencesPage(widget *gtk.Widget) {
	scroller := widget.GetFirstChild()
	if scroller == nil || scroller.GetCssName() != "scrolledwindow" {
		return
	}
	viewport := scroller.GetFirstChild()
	if viewport == nil || viewport.GetCssName() != "viewport" {
		return
	}
	inner := viewport.GetFirstChild()
	if inner == nil || inner.GetCssName() != "clamp" {
		return
	}
	clamp := adw.ClampNewFromInternalPtr(inner.GoPointer())
	clamp.SetMaximumSize(math.MaxInt32)
	clamp.SetTighteningThreshold(math.MaxInt32)
}

// growWithContent stops a widget that scrolls internally from doing so, so
// it reports its content's full height instead of a scroller's small minimum.
//
// AdwPreferencesPage is built around a GtkScrolledWindow as its only child.
// Nested in this shell's own scroller, it was allocated that scroller-sized
// minimum once mounted below the sources, and the page scrolled inside the
// page. The outer scroller alone scrolls the destination. Any other child
// shape is left untouched.
func growWithContent(widget *gtk.Widget) {
	child := widget.GetFirstChild()
	if child == nil || child.GetCssName() != "scrolledwindow" {
		return
	}
	scroller := gtk.ScrolledWindowNewFromInternalPtr(child.GoPointer())
	scroller.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyNeverValue)
	scroller.SetPropagateNaturalHeight(true)
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
	policy := s.currentPolicy()
	go func() {
		defer cancel()
		s.coordinator.Check(ctx, preferences, policy, s.publish)
	}()
}

// StartUpdate starts the current snapshot's serial mutation away from the GTK
// thread.
func (s *UpdateShell) StartUpdate() {
	if !s.beginMutation() {
		return
	}
	ctx, cancel, ok := s.operationContext()
	if !ok {
		s.finishMutation()
		return
	}
	current := cloneSnapshot(s.snapshot)
	preferences := s.currentPreferences()
	go func() {
		defer cancel()
		defer sgtk.RunOnMainThread(s.finishMutation)
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

// beginMutation is the common admission point for unified, individual and
// dedicated staging actions. Every caller runs on GTK's main thread.
func (s *UpdateShell) beginMutation() bool {
	if s == nil {
		return false
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	// A pending restart owns the system until pkexec resolves; refuse any
	// mutation (Update all, per-row updates) started in that window.
	if s.coordinator == nil || !updatepresent.CanStartOperation(s.Busy(), s.closed.Load()) ||
		s.restartInFlight.Load() || !s.sourcesReady || updatepresent.ShowProgress(s.snapshot.Phase) || !s.mutation.CompareAndSwap(false, true) {
		return false
	}
	s.primary.SetSensitive(false)
	s.refresh.SetSensitive(false)
	for _, row := range s.sourceRows {
		row.setSensitive(false)
	}
	return true
}

func (s *UpdateShell) finishMutation() {
	s.mutation.Store(false)
	if s.closed.Load() {
		return
	}
	s.renderPrimaryAction(updatepresent.Snapshot(s.snapshot))
	s.refresh.SetSensitive(true)
	s.renderProgress(s.snapshot)
	// restartInFlight must override the per-row sensitivity flip below:
	// setSensitive iterates every suffix (including the new Restart now
	// button added in #439) and would otherwise re-enable a second restart
	// while the privileged action is still up. Re-applied here so the
	// row-owned restart path honours the in-flight guard at every snapshot
	// boundary, not just at the render() call site (#446 review).
	restartInFlight := s.restartInFlight.Load()
	for _, row := range s.sourceRows {
		row.setSensitive(!updatepresent.ShowProgress(s.snapshot.Phase))
		if restartInFlight {
			row.setRestartButtonSensitive(false)
		}
	}
}

func (s *UpdateShell) startItemUpdate(source updateflow.SourceID, item updateflow.Item, row *adw.ActionRow) {
	message := actionmsg.Update(dryrun.Enabled(), updatepresent.ItemTitle(item))
	if source == updateflow.DeveloperTools {
		message = actionmsg.Upgrade(dryrun.Enabled(), item.Name)
	}
	failed := func(err error) string {
		var trustErr *homebrew.UntrustedTapError
		if errors.As(err, &trustErr) {
			return trustmsg.UpgradeMessage(item.Name, s.trustGroupAvailable)
		}
		return fmt.Sprintf("Couldn't update %s. %s", updatepresent.ItemTitle(item), updatepresent.FailureHint(err))
	}
	s.startIndividualUpdate(source, row, func(ctx context.Context) (updateflow.ApplyResult, error) {
		return updateproviders.UpdateItem(ctx, source, item)
	}, message, failed)
}

func (s *UpdateShell) startToolRefresh() {
	row := s.sourceRows[updateflow.DeveloperTools]
	if row == nil {
		return
	}
	s.startIndividualUpdate(updateflow.DeveloperTools, row.row, updateproviders.RefreshDeveloperTools,
		actionmsg.SelfUpdate(dryrun.Enabled(), "Tool catalog"),
		func(err error) string {
			return "Couldn't check for new tool versions. " + updatepresent.FailureHint(err)
		})
}

// startIndividualUpdate runs one row's update. A live change reports source
// as completed through the same finished hook an Update All run uses, so the
// Apps inventory refreshes after a single Homebrew upgrade too. A failure is
// logged with its raw error and shown as failed(err), which is plain words.
func (s *UpdateShell) startIndividualUpdate(source updateflow.SourceID, row *adw.ActionRow, run func(context.Context) (updateflow.ApplyResult, error), result string, failed func(error) string) {
	if !s.beginMutation() {
		return
	}
	ctx, cancel, ok := s.operationContext()
	if !ok {
		s.finishMutation()
		return
	}
	row.SetSubtitle("Working…")
	s.renderProgress(updateflow.Snapshot{Phase: updateflow.PhaseUpdating})
	go func() {
		defer cancel()
		outcome, err := run(ctx)
		sgtk.RunOnMainThread(func() {
			s.finishMutation()
			if s.closed.Load() {
				return
			}
			if err != nil {
				log.Printf("updates: updating one item in %s: %v", source, err)
				message := failed(err)
				row.SetSubtitle(message)
				if s.toasts != nil {
					s.toasts.ShowErrorToast(message)
				}
				return
			}
			if !outcome.Changed && !outcome.Preview {
				row.SetSubtitle("No update was applied; the update is still available")
				if s.toasts != nil {
					s.toasts.ShowToast("No update was applied")
				}
				return
			}
			if s.toasts != nil {
				s.toasts.ShowToast(result)
			}
			if outcome.Preview {
				s.renderSources(s.snapshot.Sources)
			} else {
				s.StartCheck()
				if s.onUpdateFinished != nil {
					s.onUpdateFinished(updateflow.Snapshot{CompletedSources: []updateflow.SourceID{source}})
				}
			}
		})
	}()
}

// StartRestart restarts the machine so a staged update takes effect. It is
// the only privileged action this shell performs directly; everything else
// it drives goes through the coordinator's own providers.
func (s *UpdateShell) StartRestart() {
	if s == nil {
		return
	}
	// Block a second press while the privileged restart is in flight. A later
	// Render recomputes the button from shell state, so the flag — not just the
	// widget call — is what keeps it disabled across snapshots (issue #447).
	s.restartInFlight.Store(true)
	s.setRestartButtonSensitive(false)
	s.renderRestartGatedActions()
	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()

		// An image without the helper's restart command would only fail
		// after authentication; ask for a restart instead.
		if !ublue.StatusCached().Supports(ubluehelper.CommandRestart) {
			sgtk.RunOnMainThread(func() {
				s.restartInFlight.Store(false)
				// Re-run the row render so the restart suffix honours the
				// current Busy()/ShowProgress gates instead of being
				// unconditionally re-enabled while a check or mutation
				// is still in flight (#446 review).
				s.renderSources(s.snapshot.Sources)
				s.renderRestartGatedActions()
				if s.toasts != nil {
					s.toasts.ShowToast("Restart your computer to finish the update")
				}
			})
			return
		}

		err := ublue.Restart(ctx)

		sgtk.RunOnMainThread(func() {
			s.restartInFlight.Store(false)
			// Same gating concern as the no-restart-command fallback
			// above: re-route through renderSources so a failing restart
			// while a check is running cannot re-enable a second press
			// before the next snapshot's render() (#446 review).
			s.renderSources(s.snapshot.Sources)
			s.renderRestartGatedActions()
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

// renderRestartGatedActions re-applies the page-level primary action's
// sensitivity at each restartInFlight transition. renderPrimaryAction is the
// only consumer of PrimaryActionEnabled's restartInFlight gate, so without
// this Update all stays pressable while pkexec is pending, or stays disabled
// after a failed restart until the next snapshot.
func (s *UpdateShell) renderRestartGatedActions() {
	if s.closed.Load() {
		return
	}
	s.renderPrimaryAction(updatepresent.Snapshot(s.snapshot))
}

// setRestartButtonSensitive toggles the Operating system row's restart
// button, the only control that starts a restart (#439). The page-level
// primary never offers a restart; it is gated via renderRestartGatedActions.
func (s *UpdateShell) setRestartButtonSensitive(sensitive bool) {
	if row, ok := s.sourceRows[updateflow.OperatingSystem]; ok && row != nil {
		row.setRestartButtonSensitive(sensitive)
	}
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
	s.statusLine.SetLabel(presentation.Status)
	s.statusDetail.SetLabel(presentation.Detail)
	s.statusDetail.SetVisible(presentation.Detail != "")
	s.header.SetVisible(presentation.ShowStatus(snapshot.Phase))
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
		s.banner.SetRevealed(false)
	}
	s.renderSources(snapshot.Sources)
	if s.toasts != nil {
		s.toasts.SetUpdateBadge(snapshot.TotalUpdates)
	}
	if !s.havePhase || s.lastPhase != snapshot.Phase {
		s.toastOverlay.Announce(presentation.Status, gtk.AccessibleAnnouncementPriorityMediumValue)
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
	s.refresh = newIconButton("view-refresh-symbolic", "Refresh")
	s.refresh.SetActionName(commands.CheckAction)
	header.PackStart(&s.refresh.Widget)

	s.toolbarView.AddTopBar(&header.Widget)

	content := gtk.NewBox(gtk.OrientationVerticalValue, 12)
	content.SetMarginTop(12)
	content.SetMarginBottom(12)
	content.SetMarginStart(12)
	content.SetMarginEnd(12)
	// The secondary preferences page sits beside content, not inside it:
	// its own 12px side padding then lines its rows up with the sections
	// content insets by the same 12px.
	pageBox := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	pageBox.SetMarginBottom(12)
	pageBox.Append(&content.Widget)
	s.page = pageBox

	s.wordmark = gtk.NewPicture()
	s.wordmark.SetCanShrink(true)
	s.wordmark.SetKeepAspectRatio(true)
	s.wordmark.SetContentFit(gtk.ContentFitContainValue)
	s.wordmark.SetHalign(gtk.AlignCenterValue)
	s.wordmark.SetSizeRequest(200, 82)
	SetAccessibleLabel(s.wordmark, "Bluefin")
	wordmarkClamp := adw.NewClamp()
	wordmarkClamp.SetMaximumSize(200)
	wordmarkClamp.SetTighteningThreshold(200)
	wordmarkClamp.SetHalign(gtk.AlignCenterValue)
	wordmarkClamp.SetChild(&s.wordmark.Widget)
	content.Append(&wordmarkClamp.Widget)
	s.styleManager = adw.StyleManagerGetDefault()
	lightPath, lightErr := firstrun.AssetPath(firstrun.AssetWordmarkLight)
	darkPath, darkErr := firstrun.AssetPath(firstrun.AssetWordmarkDark)
	if lightErr != nil || darkErr != nil {
		log.Printf("updates: loading wordmark: %v, %v", lightErr, darkErr)
	}
	currentWordmark := ""
	applyWordmark := func() {
		path := lightPath
		if s.styleManager != nil && s.styleManager.GetDark() {
			path = darkPath
		}
		if path == currentWordmark {
			return
		}
		currentWordmark = path
		s.wordmark.SetFilename(path)
	}
	applyWordmark()
	if s.styleManager != nil {
		changed := func(gobject.Object, uintptr) { applyWordmark() }
		s.themeHandler = s.styleManager.ConnectNotify(&changed)
	}

	// The header reads wordmark → primary action → one line of status, so
	// the action sits directly under the wordmark and the supporting text
	// under the action. While checking or updating there is no action and
	// the progress bar takes its place.
	s.header = gtk.NewBox(gtk.OrientationVerticalValue, 6)
	s.header.SetHalign(gtk.AlignCenterValue)

	s.primary = gtk.NewButtonWithLabel("")
	s.primary.AddCssClass("pill")
	s.primary.SetHalign(gtk.AlignCenterValue)
	s.primary.SetVisible(false)
	primaryClicked := func(_ gtk.Button) {
		// The primary never starts a restart: the Operating system row owns
		// the "Restart now" suffix (#439).
		switch s.snapshot.Action {
		case updateflow.ActionCheck:
			s.StartCheck()
		case updateflow.ActionUpdateAll, updateflow.ActionRetryFailed:
			s.StartUpdate()
		}
	}
	s.primary.ConnectClicked(&primaryClicked)
	s.header.Append(&s.primary.Widget)

	s.progress = gtk.NewProgressBar()
	s.progress.SetShowText(false)
	s.progress.SetVisible(false)
	s.progress.SetSizeRequest(240, -1)
	s.header.Append(&s.progress.Widget)

	s.statusLine = gtk.NewLabel("")
	s.statusLine.SetWrap(true)
	s.statusLine.SetJustify(gtk.JustifyCenterValue)
	// A stable description lets assistive technology, and the AT-SPI
	// suite, tell the status line apart from a source row saying the same.
	s.statusLine.UpdateProperty(gtk.AccessiblePropertyDescriptionValue, "Update status", -1)
	s.header.Append(&s.statusLine.Widget)

	s.statusDetail = gtk.NewLabel("")
	s.statusDetail.SetWrap(true)
	s.statusDetail.SetJustify(gtk.JustifyCenterValue)
	s.statusDetail.AddCssClass("caption")
	s.statusDetail.AddCssClass("dim-label")
	s.statusDetail.SetVisible(false)
	s.header.Append(&s.statusDetail.Widget)
	content.Append(&s.header.Widget)

	s.banner = adw.NewBanner("")
	s.banner.SetRevealed(false)
	content.Append(&s.banner.Widget)

	s.systemGroup = adw.NewPreferencesGroup()
	s.systemGroup.SetTitle("System updates")
	s.systemGroup.SetDescription("Operating system updates take effect after a restart.")
	content.Append(&s.systemGroup.Widget)
	s.appsGroup = adw.NewPreferencesGroup()
	s.appsGroup.SetTitle("Apps and tools")
	s.appsGroup.SetDescription("Updates to the software you use.")
	content.Append(&s.appsGroup.Widget)

	s.breakpointBin = adw.NewBreakpointBin()
	// AdwBreakpointBin requires a minimum size: without one libadwaita warns
	// on every allocation and the max-width condition below cannot resolve,
	// so the compact layout never applies reliably. Both values sit under
	// the 600px condition so the breakpoint can actually be reached.
	s.breakpointBin.SetSizeRequest(360, 200)
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
	clamp.SetChild(&pageBox.Widget)
	scrolled.SetChild(&clamp.Widget)
	// The breakpoint bin wraps the scroller, not the other way round. An
	// AdwBreakpointBin never asks for more height than its size request, so
	// inside a GtkScrolledWindow it got one viewport of height and clipped
	// everything below ("GtkBox exceeds AdwBreakpointBin height: requested
	// 1844 px, 844 px available"): once the preferences page mounted,
	// automatic updates, the release channel, and the graphics driver could
	// not be scrolled to.
	s.breakpointBin.SetChild(&scrolled.Widget)
	s.toolbarView.SetContent(&s.breakpointBin.Widget)

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
	s.primary.SetSensitive(updatepresent.PrimaryActionEnabled(
		presentation.ShowAction,
		s.Busy(),
		s.closed.Load(),
		s.restartInFlight.Load(),
	))
}

func (s *UpdateShell) renderProgress(snapshot updateflow.Snapshot) {
	visible := updatepresent.ShowProgress(snapshot.Phase)
	s.progress.SetVisible(visible)
	if visible && s.progressTimer == 0 {
		// Keep one callback identity across checks; provider snapshots can be
		// silent for minutes while a command runs. GLib dispatches on GTK's thread.
		if s.progressPulse == nil {
			s.progressPulse = func(uintptr) bool {
				s.progress.Pulse()
				return true
			}
		}
		s.progress.Pulse()
		s.progressTimer = glib.TimeoutAdd(100, &s.progressPulse, 0)
	} else if !visible && s.progressTimer != 0 {
		glib.SourceRemove(s.progressTimer)
		s.progressTimer = 0
	}
}

func (s *UpdateShell) renderSources(states []updateflow.SourceState) {
	rebuild := len(states) != len(s.sourceRows)
	for _, state := range states {
		row := s.sourceRows[state.ID]
		if row == nil || !slices.Equal(row.items, state.Items) ||
			row.enabled != (state.Configured && state.Available && state.Enabled) {
			rebuild = true
			break
		}
	}
	if !rebuild {
		for _, state := range states {
			s.sourceRows[state.ID].render(state, !s.Busy() && !updatepresent.ShowProgress(s.snapshot.Phase), s.restartInFlight.Load())
		}
		return
	}
	for _, row := range s.sourceRows {
		row.remove()
	}
	s.updateButtons.clear()
	clear(s.sourceRows)
	for _, state := range states {
		group := s.appsGroup
		if state.ID == updateflow.OperatingSystem || state.ID == updateflow.SystemComponents {
			group = s.systemGroup
		}
		s.sourceRows[state.ID] = newSourceRow(state, group, s)
	}
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
	if s.progressTimer != 0 {
		glib.SourceRemove(s.progressTimer)
		s.progressTimer = 0
	}
	if s.styleManager != nil && s.themeHandler != 0 {
		gobject.SignalHandlerDisconnect(&s.styleManager.Object, s.themeHandler)
		s.themeHandler = 0
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

func (s *UpdateShell) currentPolicy() map[updateflow.SourceID]updateflow.Policy {
	if s.policy == nil {
		return nil
	}
	values := s.policy()
	if values == nil {
		return nil
	}
	cloned := make(map[updateflow.SourceID]updateflow.Policy, len(values))
	for id, policy := range values {
		cloned[id] = policy
	}
	return cloned
}
