package views

import (
	"context"
	"errors"
	"log"
	"time"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gdkpixbuf"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
)

// wallpaperCommandTimeout bounds each gsettings round trip, so a hung settings
// tool reads as a failed check instead of a spinner that never stops.
const wallpaperCommandTimeout = 10 * time.Second

// Thumbnails are decoded at this size: a 16:9 tile, twice its drawn size for
// HiDPI. Decoding a 4K picture whole costs ~33 MB, and a channel holds dozens.
const (
	wallpaperThumbWidth  = 320
	wallpaperThumbHeight = 180
)

// wallpaperObservation is one off-thread read of the desktop and the catalog.
type wallpaperObservation struct {
	supported    bool
	currentURI   string
	channels     []livery.WallpaperChannel
	probeErr     error
	discoveryErr error
	rotation     livery.WallpaperRotation
	rotationErr  error
}

func (o wallpaperObservation) state() pageview.WallpaperChannelsState {
	return pageview.WallpaperChannelsOffer(o.supported, o.probeErr != nil, len(o.channels), o.discoveryErr != nil)
}

// observeWallpaperChannels reads only; it never writes a setting. Off GNOME it
// does not even read the catalog, because no picture could be applied there.
func observeWallpaperChannels() wallpaperObservation {
	ctx, cancel := context.WithTimeout(context.Background(), wallpaperCommandTimeout)
	defer cancel()
	var o wallpaperObservation
	o.supported, o.currentURI, o.probeErr = livery.ProbeNativeBackground(ctx)
	if o.probeErr != nil {
		log.Printf("wallpapers: %v", o.probeErr)
	}
	if !o.supported && o.probeErr == nil {
		return o
	}
	o.channels, o.discoveryErr = livery.WallpaperChannels(livery.WallpaperDataDirs())
	if o.discoveryErr != nil {
		log.Printf("wallpapers: %v", o.discoveryErr)
	}
	if o.supported {
		o.rotation, o.rotationErr = livery.ObserveWallpaperRotation(ctx)
		if o.rotationErr != nil {
			log.Printf("wallpapers: %v", o.rotationErr)
		}
	}
	return o
}

// wallpaperChannels is the channel selector inside the Wallpapers group. Its
// widgets and signal handlers are built once; a refresh only replaces the
// combo's model and the grid's children, so no callback slot is spent per
// refresh (see AGENTS.md, "Connect GTK signals once").
type wallpaperChannels struct {
	page *adw.PreferencesPage

	unknownRow *adw.ActionRow
	retry      *gtk.Button
	onRetry    func(gtk.Button)

	row      *adw.ComboRow
	spinner  *gtk.Spinner
	onSelect func(gobject.Object, uintptr)
	grid     *gtk.FlowBox
	onPick   func(gtk.FlowBox, uintptr)

	channels   []livery.WallpaperChannel
	shown      int
	currentURI string
	incomplete bool
	checks     []*gtk.Image
	pictures   []*gtk.Picture

	gate    actionstate.Gate
	observe actionstate.RefreshGate
	thumbs  actionstate.RefreshGate

	// The opt-in rotation switch. rotation is the last observed state, kept
	// when a later check fails; rotateSuppress silences the switch's
	// state-set while the page restores that state.
	rotateRow      *adw.ActionRow
	rotateSwitch   *gtk.Switch
	rotateSpinner  *gtk.Spinner
	rotateSuppress bool
	rotation       livery.WallpaperRotation
	rotationErr    error
	rotateGate     actionstate.Gate
}

// discoverWallpaperChannels runs off the main thread from buildWallpapersGroup.
// It reads the desktop and the catalog, then builds the selector once and
// hooks it into the group's refresh, so a collection installed later appears.
func (uh *UserHome) discoverWallpaperChannels(page *adw.PreferencesPage) {
	observation := observeWallpaperChannels()
	sgtk.RunOnMainThread(func() {
		if uh.wallpaperChannels != nil {
			return
		}
		wc := &wallpaperChannels{page: page}
		uh.wallpaperChannels = wc
		previous := uh.wallpaperRefresh
		uh.wallpaperRefresh = func() {
			if previous != nil {
				previous()
			}
			uh.refreshWallpaperChannels()
		}
		uh.showWallpaperChannels(observation)
	})
}

// refreshWallpaperChannels re-reads the desktop and catalog. A newer refresh,
// or an apply in flight, wins over an older observation.
func (uh *UserHome) refreshWallpaperChannels() {
	wc := uh.wallpaperChannels
	if wc == nil {
		return
	}
	ticket := wc.observe.Begin()
	go func() {
		observation := observeWallpaperChannels()
		sgtk.RunOnMainThread(func() {
			if !wc.observe.IsCurrent(ticket) || wc.gate.Running() || wc.rotateGate.Running() {
				// A pick's completion re-reads, and a newer refresh lands on
				// its own; only the retry control needs restoring here.
				if wc.retry != nil {
					wc.retry.SetSensitive(true)
				}
				return
			}
			uh.showWallpaperChannels(observation)
		})
	}()
}

// showWallpaperChannels renders one observation. Main thread only.
func (uh *UserHome) showWallpaperChannels(o wallpaperObservation) {
	wc := uh.wallpaperChannels
	state := o.state()
	switch state {
	case pageview.WallpaperChannelsUnknown:
		uh.ensureWallpaperUnknownRow(wc)
	case pageview.WallpaperChannelsAvailable:
		uh.ensureWallpaperSelector(wc)
		uh.setWallpaperChannels(wc, o.channels, o.currentURI, o.discoveryErr != nil)
		uh.setWallpaperRotation(wc, o.rotation, o.rotationErr)
	}
	if wc.unknownRow != nil {
		wc.unknownRow.SetVisible(state == pageview.WallpaperChannelsUnknown)
		wc.retry.SetSensitive(true)
	}
	if wc.row != nil {
		wc.row.SetVisible(state == pageview.WallpaperChannelsAvailable)
		wc.rotateRow.SetVisible(state == pageview.WallpaperChannelsAvailable)
		wc.grid.SetVisible(state == pageview.WallpaperChannelsAvailable)
	}
}

func (uh *UserHome) ensureWallpaperUnknownRow(wc *wallpaperChannels) {
	if wc.unknownRow != nil {
		return
	}
	group := uh.ensureWallpapersGroup(wc.page)
	text := pageview.WallpaperChannelsRow(pageview.WallpaperChannelsUnknown)
	row := adw.NewActionRow()
	row.SetUseMarkup(false)
	row.SetTitle(text.Title)
	row.SetSubtitle(text.Subtitle)
	retry := gtk.NewButtonWithLabel(pageview.WallpaperRetryLabel)
	retry.SetValign(gtk.AlignCenterValue)
	row.AddSuffix(&retry.Widget)
	wc.onRetry = func(_ gtk.Button) {
		retry.SetSensitive(false)
		uh.refreshWallpaperChannels()
	}
	retry.ConnectClicked(&wc.onRetry)
	wc.unknownRow, wc.retry = row, retry
	group.Add(&row.Widget)
}

// ensureWallpaperSelector builds the channel combo and the thumbnail grid once.
//
// The grid keeps SelectionNone for the foundation grid's reason: GtkFlowBox's
// single-selection mode follows the keyboard cursor, so arrowing across tiles
// would highlight pictures that were never applied. The current picture is
// drawn as a badge plus the accessible selected state instead.
func (uh *UserHome) ensureWallpaperSelector(wc *wallpaperChannels) {
	if wc.row != nil {
		return
	}
	group := uh.ensureWallpapersGroup(wc.page)
	text := pageview.WallpaperChannelsRow(pageview.WallpaperChannelsAvailable)
	row := adw.NewComboRow()
	row.SetUseMarkup(false)
	row.SetTitle(text.Title)
	row.SetSubtitle(text.Subtitle)
	wc.spinner = newActivitySpinner()
	row.AddSuffix(&wc.spinner.Widget)
	wc.onSelect = func(_ gobject.Object, _ uintptr) { uh.onWallpaperChannelSelected() }
	row.ConnectNotify(&wc.onSelect)
	group.Add(&row.Widget)

	// Rotation is opt-in: the switch starts insensitive and off, and only a
	// user's toggle (never a restore, which is suppressed) writes a timer.
	wc.rotateSpinner = newActivitySpinner()
	wc.rotateRow, wc.rotateSwitch = newSwitchRow(
		pageview.Row{Title: pageview.WallpaperRotationTitle},
		uh.onWallpaperRotationToggled,
		wc.rotateSpinner,
	)
	wc.rotateRow.SetUseMarkup(false)
	group.Add(&wc.rotateRow.Widget)

	grid := gtk.NewFlowBox()
	grid.SetSelectionMode(gtk.SelectionNoneValue)
	grid.SetHomogeneous(true)
	grid.SetMinChildrenPerLine(2)
	grid.SetMaxChildrenPerLine(4)
	grid.SetRowSpacing(6)
	grid.SetColumnSpacing(6)
	grid.SetMarginTop(6)
	wc.onPick = func(_ gtk.FlowBox, ptr uintptr) {
		uh.onWallpaperPicked(int(gtk.FlowBoxChildNewFromInternalPtr(ptr).GetIndex()))
	}
	grid.ConnectChildActivated(&wc.onPick)
	group.Add(&grid.Widget)
	wc.row, wc.grid = row, grid
}

// setWallpaperChannels replaces the combo's model, keeping the channel on
// screen when it still exists, else the one holding the current picture.
func (uh *UserHome) setWallpaperChannels(wc *wallpaperChannels, channels []livery.WallpaperChannel, currentURI string, incomplete bool) {
	if sameWallpaperChannels(wc.channels, channels) && wc.incomplete == incomplete {
		// Nothing to redraw: keep the tiles (and keyboard focus), move only
		// the badge to what the desktop reports now.
		wc.currentURI = currentURI
		uh.markCurrentWallpaper(wc)
		return
	}
	wc.incomplete = incomplete
	keep := ""
	if wc.shown >= 0 && wc.shown < len(wc.channels) {
		keep = wc.channels[wc.shown].ID
	}
	wc.channels, wc.currentURI = channels, currentURI
	shown := 0
	if selected, _, ok := livery.SelectedWallpaper(channels, currentURI); ok {
		shown = selected
	}
	names := make([]string, len(channels))
	for i, channel := range channels {
		names[i] = channel.Name
		if channel.ID == keep {
			shown = i
		}
	}
	// The model swap notifies "selected"; marking nothing shown makes that
	// handler a no-op and leaves the one redraw below.
	wc.shown = len(channels)
	wc.row.SetModel(gtk.NewStringList(names))
	wc.row.SetSelected(uint32(shown))
	uh.drawWallpaperChannel(wc, shown)
}

// sameWallpaperChannels reports whether two observations list the same
// collections with the same pictures in the same order.
func sameWallpaperChannels(a, b []livery.WallpaperChannel) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || len(a[i].Wallpapers) != len(b[i].Wallpapers) {
			return false
		}
		for j := range a[i].Wallpapers {
			if a[i].Wallpapers[j] != b[i].Wallpapers[j] {
				return false
			}
		}
	}
	return true
}

// onWallpaperChannelSelected redraws the grid for the picked channel. It
// writes nothing: the generic notify also fires for unrelated properties, so
// it only acts when the selected channel actually changed.
func (uh *UserHome) onWallpaperChannelSelected() {
	wc := uh.wallpaperChannels
	if wc == nil || wc.row == nil || wc.shown >= len(wc.channels) {
		return
	}
	selected := int(wc.row.GetSelected())
	if selected == wc.shown || selected < 0 || selected >= len(wc.channels) {
		return
	}
	uh.drawWallpaperChannel(wc, selected)
}

// drawWallpaperChannel puts one channel's tiles in the grid and loads their
// thumbnails off the main thread. Main thread only.
func (uh *UserHome) drawWallpaperChannel(wc *wallpaperChannels, index int) {
	wc.grid.RemoveAll()
	wc.checks, wc.pictures = nil, nil
	wc.shown = index
	if index < 0 || index >= len(wc.channels) {
		return
	}
	channel := wc.channels[index]
	wc.row.SetSubtitle(pageview.WallpaperChannelSubtitle(len(channel.Wallpapers), wc.incomplete))
	uh.renderWallpaperRotation(wc)
	previews := make([]string, len(channel.Wallpapers))
	for i, wallpaper := range channel.Wallpapers {
		picture, check, tile := newWallpaperTile(wallpaper.Name)
		wc.grid.Insert(&tile.Widget, -1)
		wc.pictures = append(wc.pictures, picture)
		wc.checks = append(wc.checks, check)
		previews[i] = wallpaper.Preview
	}
	uh.markCurrentWallpaper(wc)
	ticket := wc.thumbs.Begin()
	go loadWallpaperThumbnails(wc, ticket, previews)
}

// newWallpaperTile is one picture: a cover-fitted thumbnail, its name, and a
// badge for the current picture. An overlay keeps the badge from reflowing.
func newWallpaperTile(name string) (*gtk.Picture, *gtk.Image, *gtk.Overlay) {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	box.SetMarginTop(6)
	box.SetMarginBottom(6)
	picture := gtk.NewPicture()
	picture.SetContentFit(gtk.ContentFitCoverValue)
	picture.SetCanShrink(true)
	picture.SetSizeRequest(wallpaperThumbWidth/2, wallpaperThumbHeight/2)
	picture.AddCssClass("card")
	box.Append(&picture.Widget)
	label := gtk.NewLabel(name)
	label.SetWrap(true)
	label.SetMaxWidthChars(18)
	label.SetJustify(gtk.JustifyCenterValue)
	box.Append(&label.Widget)

	check := gtk.NewImageFromIconName("object-select-symbolic")
	check.AddCssClass("accent")
	check.SetHalign(gtk.AlignEndValue)
	check.SetValign(gtk.AlignStartValue)
	check.SetMarginTop(12)
	check.SetMarginEnd(6)
	check.SetVisible(false)
	SetAccessibleLabel(check, pageview.WallpaperSelectedLabel)
	tile := gtk.NewOverlay()
	tile.SetChild(&box.Widget)
	tile.AddOverlay(&check.Widget)
	return picture, check, tile
}

// loadWallpaperThumbnails decodes each preview at tile size. GdkPixbuf's file
// loaders and gdk_texture_new_for_pixbuf are safe off the main thread; a
// picture no loader can read keeps its empty card.
func loadWallpaperThumbnails(wc *wallpaperChannels, ticket uint64, previews []string) {
	for i, path := range previews {
		if !wc.thumbs.IsCurrent(ticket) {
			return // A newer channel is on screen; stop decoding this one.
		}
		if path == "" {
			continue
		}
		pixbuf, err := gdkpixbuf.NewPixbufFromFileAtScale(path, wallpaperThumbWidth, wallpaperThumbHeight, true)
		if err != nil || pixbuf == nil {
			log.Printf("wallpapers: thumbnail %s: %v", path, err)
			continue
		}
		texture := gdk.NewTextureForPixbuf(pixbuf)
		pixbuf.Unref()
		if texture == nil {
			continue
		}
		index := i
		sgtk.RunOnMainThread(func() {
			// puregotk releases nothing on its own: SetPaintable takes its
			// own reference, so this one is dropped on every path.
			defer texture.Unref()
			if !wc.thumbs.IsCurrent(ticket) || index >= len(wc.pictures) {
				return
			}
			wc.pictures[index].SetPaintable(texture)
		})
	}
}

// markCurrentWallpaper badges the confirmed current picture. It changes
// presentation only and emits no child-activated, so it is safe on load.
func (uh *UserHome) markCurrentWallpaper(wc *wallpaperChannels) {
	channel, current, ok := livery.SelectedWallpaper(wc.channels, wc.currentURI)
	for i, check := range wc.checks {
		selected := ok && channel == wc.shown && current == i
		check.SetVisible(selected)
		if child := wc.grid.GetChildAtIndex(int32(i)); child != nil {
			state := 0
			if selected {
				state = 1
			}
			child.UpdateState(gtk.AccessibleStateSelectedValue, state, -1)
		}
	}
}

// onWallpaperPicked applies the picked picture. Only an explicit pick writes;
// the grid stays pickable afterwards, so a failure is retried by picking
// again. The badge moves only when the desktop confirms the new picture.
func (uh *UserHome) onWallpaperPicked(index int) {
	wc := uh.wallpaperChannels
	if wc == nil || wc.shown < 0 || wc.shown >= len(wc.channels) {
		return
	}
	wallpapers := wc.channels[wc.shown].Wallpapers
	if index < 0 || index >= len(wallpapers) || !wc.gate.TryStart() {
		return
	}
	wallpaper := wallpapers[index]
	wc.observe.Begin() // An observation that predates this pick must not land.
	wc.grid.SetSensitive(false)
	setActivitySpinner(wc.spinner, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), wallpaperCommandTimeout)
		defer cancel()
		preview := dryrun.Enabled()
		err := livery.ApplyBackground(ctx, wallpaper)
		sgtk.RunOnMainThread(func() {
			wc.observe.Begin() // An observation read during the write must not land.
			wc.gate.Reset()
			setActivitySpinner(wc.spinner, false)
			wc.grid.SetSensitive(true)
			uh.finishWallpaperPick(wc, wallpaper, preview, err)
			// Re-read what the desktop now holds. After a failure that is the
			// only truthful badge (an earlier key may have been written, or
			// the schema may be gone), and it also delivers any refresh that
			// was dropped while the pick held the gate.
			uh.refreshWallpaperChannels()
		})
	}()
}

func (uh *UserHome) finishWallpaperPick(wc *wallpaperChannels, wallpaper livery.Wallpaper, preview bool, err error) {
	if err != nil {
		switch {
		case errors.Is(err, livery.ErrBackgroundNotApplied):
			log.Printf("wallpapers: %s was written but not kept: %v", wallpaper.File, err)
		case errors.Is(err, livery.ErrBackgroundUnavailable):
			log.Printf("wallpapers: %v; checking the desktop again", err)
		default:
			log.Printf("wallpapers: applying %s: %v", wallpaper.File, err)
		}
		uh.toastAdder.ShowErrorToast(pageview.WallpaperApplyFailure)
		return
	}
	uh.toastAdder.ShowToast(actionmsg.WallpaperApply(preview, wallpaper.Name))
	if preview {
		return
	}
	wc.currentURI = livery.BackgroundURI(wallpaper.File)
	uh.markCurrentWallpaper(wc)
}

// setWallpaperRotation records one rotation observation and redraws the
// switch. A failed check keeps the last observed state and says so, rather
// than reading as off. Main thread only.
func (uh *UserHome) setWallpaperRotation(wc *wallpaperChannels, rotation livery.WallpaperRotation, err error) {
	if err == nil {
		wc.rotation = rotation
	}
	wc.rotationErr = err
	uh.renderWallpaperRotation(wc)
}

// renderWallpaperRotation puts the observed rotation state on the switch and
// its subtitle. Main thread only.
func (uh *UserHome) renderWallpaperRotation(wc *wallpaperChannels) {
	if wc.rotateRow == nil {
		return
	}
	view := pageview.WallpaperRotationView{
		On:            wc.rotation.On,
		ChannelName:   wc.rotation.ChannelID,
		Scheduled:     wc.rotation.Scheduled,
		LastRunFailed: wc.rotation.LastRunFailed,
		CheckFailed:   wc.rotationErr != nil,
	}
	for _, channel := range wc.channels {
		if channel.ID == wc.rotation.ChannelID {
			view.ChannelName = channel.Name
		}
	}
	if !wc.rotation.Next.IsZero() {
		view.Next = wc.rotation.Next.Local().Format("Mon 2 Jan 15:04")
	}
	shown := ""
	if wc.shown >= 0 && wc.shown < len(wc.channels) {
		shown = wc.channels[wc.shown].Name
	}
	wc.rotateRow.SetSubtitle(pageview.WallpaperRotationSubtitle(view, shown))
	wc.rotateSuppress = true
	wc.rotateSwitch.SetActive(wc.rotation.On)
	wc.rotateSuppress = false
	wc.rotateSwitch.SetSensitive(!wc.rotateGate.Running())
}

// onWallpaperRotationToggled turns rotation on for the channel on screen, or
// off. It runs only for a user's toggle; the switch then shows the state the
// user manager reports afterwards, so a failure or a preview springs back to
// what is really scheduled and the switch stays retryable.
func (uh *UserHome) onWallpaperRotationToggled(on bool) {
	wc := uh.wallpaperChannels
	if wc == nil || wc.rotateSuppress || wc.rotateSwitch == nil {
		return
	}
	if on == wc.rotation.On && wc.rotationErr == nil {
		return
	}
	if on && (wc.shown < 0 || wc.shown >= len(wc.channels)) {
		return
	}
	if !wc.rotateGate.TryStart() {
		return
	}
	var channel livery.WallpaperChannel
	if on {
		channel = wc.channels[wc.shown]
	}
	wc.observe.Begin() // An observation that predates this toggle must not land.
	wc.rotateSwitch.SetSensitive(false)
	setActivitySpinner(wc.rotateSpinner, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), wallpaperCommandTimeout)
		defer cancel()
		preview := dryrun.Enabled()
		var err error
		if on {
			err = livery.EnableWallpaperRotation(ctx, channel.ID)
		} else {
			err = livery.DisableWallpaperRotation(ctx)
		}
		observed, observeErr := livery.ObserveWallpaperRotation(ctx)
		if observeErr != nil {
			log.Printf("wallpapers: %v", observeErr)
		}
		sgtk.RunOnMainThread(func() {
			wc.observe.Begin() // An observation read during the change must not land.
			wc.rotateGate.Reset()
			setActivitySpinner(wc.rotateSpinner, false)
			uh.setWallpaperRotation(wc, observed, observeErr)
			if err != nil {
				log.Printf("wallpapers: rotation: %v", err)
				uh.toastAdder.ShowErrorToast(pageview.WallpaperRotationFailure(on))
				return
			}
			uh.toastAdder.ShowToast(actionmsg.WallpaperRotation(preview, on, channel.Name))
		})
	}()
}
