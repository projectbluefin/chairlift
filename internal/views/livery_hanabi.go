package views

import (
	"errors"
	"log"
	"path/filepath"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
)

// buildHanabiControl owns one capability-dependent row. All widget and local
// state access is on the main thread; workers receive immutable snapshots.
func (uh *UserHome) buildHanabiControl(page *adw.PreferencesPage) {
	var row *adw.ActionRow
	var choose, apply, retry *gtk.Button
	var confirmed livery.HanabiState
	var pending string
	var generation actionstate.RefreshGate
	busy := false
	active := func() bool { return uh.liveryPrefsPage == page && uh.groupEnabled("livery_page", "wallpapers_group") }
	subtitle := func() string {
		if confirmed.Path == "" {
			return "Choose a video, then Apply to save its path for Hanabi. Playback is managed by the extension."
		}
		return "Saved video: " + confirmed.Path + ". Playback is managed by Hanabi."
	}
	report := func(err error) {
		log.Printf("hanabi: %v", err)
		uh.toastAdder.ShowErrorToast(err.Error())
	}
	var refresh func()
	build := func() {
		if row != nil {
			return
		}
		row = adw.NewActionRow()
		row.SetUseMarkup(false)
		choose = gtk.NewButtonWithLabel("Choose Video…")
		apply = gtk.NewButtonWithLabel("Apply Video")
		retry = gtk.NewButtonWithLabel("Retry")
		for _, button := range []*gtk.Button{choose, apply, retry} {
			button.SetValign(gtk.AlignCenterValue)
			row.AddSuffix(&button.Widget)
		}
		uh.ensureWallpapersGroup(page).Add(&row.Widget)

		retryClicked := func(_ gtk.Button) {
			if active() && !busy {
				refresh()
			}
		}
		retry.ConnectClicked(&retryClicked)
		chooseClicked := func(_ gtk.Button) {
			if !active() || busy || !confirmed.Available {
				return
			}
			busy = true
			choose.SetSensitive(false)
			apply.SetSensitive(false)
			ticket := generation.Begin()
			dialog := gtk.NewFileDialog()
			dialog.SetTitle("Choose a Video Wallpaper")
			filter := gtk.NewFileFilter()
			filter.SetName("Video files (MP4, MOV, M4V, WebM, OGV)")
			for _, pattern := range []string{"*.mp4", "*.mov", "*.m4v", "*.webm", "*.ogv"} {
				filter.AddPattern(pattern)
			}
			filters := gio.NewListStore(gtk.FileFilterGLibType())
			filters.Append(gobject.ObjectNewFromInternalPtr(filter.GoPointer()))
			dialog.SetFilters(filters)
			dialog.SetDefaultFilter(filter)
			var done gio.AsyncReadyCallback = func(_, result, _ uintptr) {
				file, err := dialog.OpenFinish(&gio.AsyncResultBase{Ptr: result})
				if !active() || !generation.IsCurrent(ticket) {
					return
				}
				busy = false
				choose.SetSensitive(true)
				apply.SetSensitive(pending != "")
				if err != nil {
					var chooserError *glib.Error
					if !errors.As(err, &chooserError) || chooserError.Domain != gtk.DialogErrorQuark() || chooserError.Code != int32(gtk.DialogErrorDismissedValue) {
						report(err)
					}
					return // Dismissal never changes the pending or confirmed path.
				}
				path := file.GetPath()
				busy = true
				choose.SetSensitive(false)
				apply.SetSensitive(false)
				go func() {
					err := livery.ValidateHanabiVideo(path)
					sgtk.RunOnMainThread(func() {
						if !active() || !generation.IsCurrent(ticket) {
							return
						}
						busy = false
						choose.SetSensitive(true)
						if err != nil {
							report(err)
							apply.SetSensitive(pending != "")
							return
						}
						pending = path
						row.SetSubtitle("Selected: " + filepath.Base(path) + " — not applied. Choose Apply to save the path.")
						apply.SetSensitive(true)
					})
				}()
			}
			dialog.Open(uh.window(), nil, &done, 0)
		}
		choose.ConnectClicked(&chooseClicked)
		applyClicked := func(_ gtk.Button) {
			if !active() || busy || !confirmed.Available || pending == "" {
				return
			}
			busy = true
			choose.SetSensitive(false)
			apply.SetSensitive(false)
			ticket := generation.Begin()
			path, previous := pending, confirmed
			go func() {
				ctx, cancel := livery.DefaultContext()
				defer cancel()
				state, preview, err := livery.ApplyHanabi(ctx, path, previous)
				sgtk.RunOnMainThread(func() {
					if !active() || !generation.IsCurrent(ticket) {
						return
					}
					busy = false
					choose.SetSensitive(true)
					apply.SetSensitive(true)
					if err != nil {
						last := previous.Path
						if last == "" {
							last = "none"
						}
						row.SetSubtitle("Could not confirm the change. Last confirmed video: " + last + ". Retry Apply or choose another video.")
						report(err)
						return
					}
					if preview {
						uh.toastAdder.ShowToast("Preview only: the video wallpaper setting was not changed.")
						return
					}
					confirmed = state
					pending = ""
					apply.SetSensitive(false)
					row.SetSubtitle(subtitle())
					uh.toastAdder.ShowToast("Video path saved. Hanabi manages wallpaper playback.")
				})
			}()
		}
		apply.ConnectClicked(&applyClicked)
	}
	refresh = func() {
		if !active() || busy {
			return
		}
		ticket := generation.Begin()
		go func() {
			ctx, cancel := livery.DefaultContext()
			defer cancel()
			state, err := livery.LoadHanabi(ctx)
			sgtk.RunOnMainThread(func() {
				if !active() || !generation.IsCurrent(ticket) {
					return
				}
				if err == nil && !state.Available {
					if row != nil {
						row.SetVisible(false)
					}
					return
				}
				build()
				row.SetVisible(true)
				pending = ""
				choose.SetVisible(err == nil)
				apply.SetVisible(err == nil)
				retry.SetVisible(err != nil)
				apply.SetSensitive(false)
				if err != nil {
					confirmed.Available = false
					log.Printf("hanabi: %v", err)
					row.SetTitle("Check Video Wallpaper Support")
					row.SetSubtitle("Could not check Hanabi settings. Retry to check support; no setting was changed.")
					return
				}
				confirmed = state
				row.SetTitle("Video Wallpaper")
				row.SetSubtitle(subtitle())
			})
		}()
	}
	previous := uh.wallpaperRefresh
	uh.wallpaperRefresh = func() {
		if previous != nil {
			previous()
		}
		refresh()
	}
	refresh()
}
