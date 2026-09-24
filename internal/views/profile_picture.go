package views

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/user"
	"path/filepath"

	"github.com/projectbluefin/chairlift/internal/avatar"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// avatarPicker is the Profile Picture section and its chooser.
//
// Every field is read and written on the GTK main thread only; workers get
// copies of what they need and hand results back through
// sgtk.RunOnMainThread.
type avatarPicker struct {
	row    *adw.ActionRow
	avatar *adw.Avatar

	// The chooser is built once, on first open, and re-presented. Its rows
	// are the ten catalog entries and never change, so the list is filled
	// once and one row-activated handler maps a row index into catalog.
	dialog  *adw.Dialog
	banner  *adw.Banner
	preview *adw.Avatar
	label   *gtk.Label
	apply   *gtk.Button
	checks  []*gtk.Image
	catalog []avatar.Avatar

	// selected is the entry being previewed; png is its transcoded artwork
	// once downloaded, nil until then or after a failure.
	selected *avatar.Avatar
	png      []byte
	texture  *gdk.Texture
	fetches  actionstate.RefreshGate
	applying actionstate.Gate
}

// buildAccountGroup builds the Profile Picture section. Opening the page
// reads only the local picture file; the catalog is embedded and no artwork
// is downloaded until the user picks an entry.
func (uh *UserHome) buildAccountGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.AvatarGroupTitle)
	group.SetDescription(pageview.AvatarGroupDescription)

	p := &avatarPicker{catalog: avatar.Catalog()}

	p.row = adw.NewActionRow()
	p.row.SetUseMarkup(false)
	current := pageview.AvatarCurrentRow(false, nil)
	p.row.SetTitle(current.Title)
	p.row.SetSubtitle(current.Subtitle)

	p.avatar = adw.NewAvatar(48, "", false)
	p.row.AddPrefix(&p.avatar.Widget)

	choose := gtk.NewButtonWithLabel(pageview.AvatarChooseLabel)
	choose.SetValign(gtk.AlignCenterValue)
	p.row.AddSuffix(&choose.Widget)
	p.row.SetActivatableWidget(&choose.Widget)
	clicked := func(_ gtk.Button) { uh.presentAvatarPicker() }
	choose.ConnectClicked(&clicked)

	group.Add(&p.row.Widget)
	page.Add(group)
	uh.profilePicture = p

	go uh.loadCurrentAvatar()
}

// loadCurrentAvatar shows the picture the account already has. The home
// directory is os.UserHomeDir, the one the applier's face-file fallback
// writes into, so the page reads back what a fallback apply wrote.
func (uh *UserHome) loadCurrentAvatar() {
	var path string
	u, userErr := user.Current()
	home, homeErr := os.UserHomeDir()
	if userErr == nil && homeErr == nil {
		path = avatar.CurrentPicture(u.Username, home)
	}
	var texture *gdk.Texture
	if path != "" {
		// GdkTexture loading is threadsafe, so the decode stays off the
		// main thread with the stat.
		t, err := gdk.NewTextureFromFilename(path)
		if err != nil {
			log.Printf("avatar: reading current picture %s: %v", path, err)
		} else {
			texture = t
		}
	}
	sgtk.RunOnMainThread(func() {
		p := uh.profilePicture
		if p == nil || p.row == nil {
			return
		}
		row := pageview.AvatarCurrentRow(texture != nil, nil)
		p.row.SetTitle(row.Title)
		p.row.SetSubtitle(row.Subtitle)
		if texture != nil {
			p.avatar.SetCustomImage(texture)
		}
	})
}

// presentAvatarPicker opens the chooser, building it on first use.
func (uh *UserHome) presentAvatarPicker() {
	p := uh.profilePicture
	if p == nil {
		return
	}
	if p.dialog == nil {
		uh.buildAvatarPicker(p)
	}
	p.banner.SetRevealed(false)
	p.dialog.Present(&uh.liveryPrefsPage.Widget)
}

// buildAvatarPicker constructs the chooser and connects its two signals. It
// runs at most once per session, for the callback-table reason documented
// on presentLiveryPicker.
func (uh *UserHome) buildAvatarPicker(p *avatarPicker) {
	dialog := adw.NewDialog()
	dialog.SetTitle(pageview.AvatarPickerTitle)
	dialog.SetContentWidth(420)
	dialog.SetContentHeight(620)

	apply := gtk.NewButtonWithLabel(pageview.AvatarApplyLabel)
	apply.AddCssClass("suggested-action")
	apply.SetSensitive(false)

	header := adw.NewHeaderBar()
	header.PackEnd(&apply.Widget)

	banner := adw.NewBanner("")
	banner.SetRevealed(false)

	preview := adw.NewAvatar(128, "", false)
	preview.SetMarginTop(12)
	label := gtk.NewLabel(pageview.AvatarPickerPrompt)
	label.AddCssClass("dim-label")

	list := gtk.NewListBox()
	list.SetSelectionMode(gtk.SelectionNoneValue)
	list.AddCssClass("boxed-list")
	list.SetMarginTop(12)
	list.SetMarginBottom(12)
	list.SetMarginStart(12)
	list.SetMarginEnd(12)
	p.checks = make([]*gtk.Image, len(p.catalog))
	for i, entry := range p.catalog {
		text := pageview.AvatarEntryRow(entry)
		row := adw.NewActionRow()
		row.SetUseMarkup(false)
		row.SetTitle(text.Title)
		row.SetSubtitle(text.Subtitle)
		row.SetActivatable(true)
		check := gtk.NewImageFromIconName("object-select-symbolic")
		check.SetVisible(false)
		row.AddSuffix(&check.Widget)
		p.checks[i] = check
		list.Append(&row.Widget)
	}

	body := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	body.Append(&banner.Widget)
	body.Append(&preview.Widget)
	body.Append(&label.Widget)
	body.Append(&list.Widget)

	scrolled := gtk.NewScrolledWindow()
	scrolled.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scrolled.SetVexpand(true)
	scrolled.SetChild(&body.Widget)

	content := adw.NewToolbarView()
	content.AddTopBar(&header.Widget)
	content.SetContent(&scrolled.Widget)
	dialog.SetChild(&content.Widget)

	p.dialog, p.banner, p.preview, p.label, p.apply = dialog, banner, preview, label, apply

	rowActivated := func(_ gtk.ListBox, rowPtr uintptr) {
		index := int(gtk.ListBoxRowNewFromInternalPtr(rowPtr).GetIndex())
		if index >= 0 && index < len(p.catalog) {
			uh.previewAvatar(p, index)
		}
	}
	list.ConnectRowActivated(&rowActivated)

	applyClicked := func(_ gtk.Button) { uh.applyAvatar(p) }
	apply.ConnectClicked(&applyClicked)
}

// previewAvatar selects one catalog entry and downloads its artwork for the
// preview. Downloading changes nothing on the account; Apply does that.
func (uh *UserHome) previewAvatar(p *avatarPicker, index int) {
	entry := p.catalog[index]
	p.selected = &entry
	p.png, p.texture = nil, nil
	for i, check := range p.checks {
		check.SetVisible(i == index)
	}
	text := pageview.AvatarEntryRow(entry)
	p.label.SetText(text.Title + " · " + text.Subtitle)
	// A nil *PaintableBase passes NULL, clearing the previous preview; a
	// bare nil interface would panic inside the binding.
	p.preview.SetCustomImage((*gdk.PaintableBase)(nil))
	p.banner.SetRevealed(false)
	p.apply.SetSensitive(false)

	generation := p.fetches.Begin()
	go func() {
		png, texture, err := fetchAvatarPreview(entry.ID)
		sgtk.RunOnMainThread(func() {
			// A later pick supersedes this one; its result must not
			// replace the newer preview.
			if !p.fetches.IsCurrent(generation) {
				return
			}
			if err != nil {
				log.Printf("avatar: previewing %s: %v", entry.ID, err)
				p.banner.SetTitle(pageview.AvatarPreviewFailed(entry))
				p.banner.SetRevealed(true)
				return
			}
			p.png, p.texture = png, texture
			p.preview.SetCustomImage(texture)
			p.apply.SetSensitive(true)
		})
	}()
}

// fetchAvatarPreview downloads and transcodes one entry. It runs off the
// main thread.
func fetchAvatarPreview(id string) ([]byte, *gdk.Texture, error) {
	webp, err := avatar.FetchAvatar(context.Background(), id)
	if err != nil {
		return nil, nil, err
	}
	png, err := avatar.TranscodeWebPToPNG(bytes.NewReader(webp))
	if err != nil {
		return nil, nil, err
	}
	texture, err := gdk.NewTextureFromBytes(glib.NewBytes(png, uint(len(png))))
	if err != nil {
		return nil, nil, err
	}
	return png, texture, nil
}

// applyAvatar sets the previewed entry on the account. A failure keeps the
// dialog open with the old picture untouched; a dry run writes nothing.
func (uh *UserHome) applyAvatar(p *avatarPicker) {
	if p.selected == nil || p.png == nil || !p.applying.TryStart() {
		return
	}
	entry, png, texture := *p.selected, p.png, p.texture
	p.apply.SetSensitive(false)

	go func() {
		route, err := dispatchAvatar(entry, png)
		sgtk.RunOnMainThread(func() {
			p.applying.Reset()
			p.apply.SetSensitive(true)
			if err != nil {
				log.Printf("avatar: applying %s: %v", entry.ID, err)
				p.banner.SetTitle(pageview.AvatarApplyFailed(entry))
				p.banner.SetRevealed(true)
				return
			}
			if route != avatar.RouteDryRun {
				row := pageview.AvatarCurrentRow(true, &entry)
				p.row.SetTitle(row.Title)
				p.row.SetSubtitle(row.Subtitle)
				p.avatar.SetCustomImage(texture)
			}
			p.dialog.Close()
			uh.toastAdder.ShowToast(pageview.AvatarApplied(route, entry))
		})
	}()
}

// dispatchAvatar stages the PNG where AccountsService can read it and hands
// it to the applier. Under --dry-run nothing is written: Dispatch returns
// before reading IconPath, so the file is not created either.
func dispatchAvatar(entry avatar.Avatar, png []byte) (avatar.Route, error) {
	req := avatar.Request{UID: os.Getuid(), ID: entry.ID}
	if !dryrun.Enabled() {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir := filepath.Join(cache, "chairlift")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		req.IconPath = filepath.Join(dir, "avatar.png")
		if err := os.WriteFile(req.IconPath, png, 0o644); err != nil {
			return "", err
		}
	}
	return avatar.NewApplier().Dispatch(context.Background(), req)
}
