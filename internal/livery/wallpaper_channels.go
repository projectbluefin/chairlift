package livery

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WallpaperImage is one selectable wallpaper inside a channel.
type WallpaperImage struct {
	// ID is the stable identifier persisted in settings. It is the file path
	// relative to the collection root, so it is unique and survives a rename
	// of the channel it lives in.
	ID string
	// Name is the wallpaper's title, derived from the file name.
	Name string
	// Path is the file's location relative to WallpaperBackgroundsRoot.
	// ResolveImage turns it into an absolute path before gsettings sees it.
	Path string
}

// WallpaperChannel is a curated set of wallpapers sharing a theme. The order
// of Images is the order rotation walks, so it is the observable cycle.
type WallpaperChannel struct {
	ID     string
	Name   string
	Images []WallpaperImage
}

// WallpaperBackgroundsRoot is where the installed wallpaper channels live:
// each channel is a subdirectory of wallpaper files under it. It defaults to
// the system wallpaper directory the wallpaper casks install into — Bluefin,
// Bazzite and Framework each ship their own — and the views layer can override
// it (tests point it at a fixture tree). When the root is absent the whole
// catalog is empty, so no fabricated channel or path is ever offered.
var WallpaperBackgroundsRoot = "/usr/share/backgrounds"

// curatedChannelOrder names the channels in the order the issue lists them.
// Channels not named here — any the casks add later, or a local directory —
// sort after, alphabetically, so discovery never drops a real channel.
var curatedChannelOrder = []string{"bluefin", "bazzite", "framework"}

// wallpaperImageExtensions are the file names a channel entry counts as a
// wallpaper. GNOME's background schema accepts images and the XML animated
// background format both; both apply through ApplyNativeBackground.
var wallpaperImageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".gif": true, ".bmp": true, ".tiff": true, ".svg": true,
	".xml": true,
}

// WallpaperChannels discovers the wallpaper channels installed under
// WallpaperBackgroundsRoot. Each channel is a subdirectory of wallpaper files;
// each file is one entry. A channel with no wallpaper files is skipped, and
// when the root is absent the catalog is empty — there are no fabricated
// channels or paths, matching "no fabricated artwork when a pack is absent."
func WallpaperChannels() []WallpaperChannel {
	entries, err := os.ReadDir(WallpaperBackgroundsRoot)
	if err != nil {
		return nil
	}
	channels := make([]WallpaperChannel, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if channel := channelFromDir(entry.Name()); channel != nil {
			channels = append(channels, *channel)
		}
	}
	sort.SliceStable(channels, func(i, j int) bool {
		return channelIndex(channels[i].ID) < channelIndex(channels[j].ID)
	})
	return channels
}

// channelFromDir builds one channel from a subdirectory of the collection
// root, or nil when the directory has no wallpaper files.
func channelFromDir(dirID string) *WallpaperChannel {
	files, err := os.ReadDir(filepath.Join(WallpaperBackgroundsRoot, dirID))
	if err != nil {
		return nil
	}
	channel := &WallpaperChannel{ID: dirID, Name: titleize(dirID)}
	for _, file := range files {
		if file.IsDir() || !wallpaperImageExtensions[strings.ToLower(filepath.Ext(file.Name()))] {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(file.Name()), filepath.Ext(file.Name()))
		channel.Images = append(channel.Images, WallpaperImage{
			ID:   filepath.Join(dirID, file.Name()),
			Name: titleize(name),
			Path: filepath.Join(dirID, file.Name()),
		})
	}
	if len(channel.Images) == 0 {
		return nil
	}
	return channel
}

// channelIndex returns the sort position of a channel id: its place in
// curatedChannelOrder, or len(order) for an unlisted channel. SliceStable
// keeps discovery order for equal keys.
func channelIndex(id string) int {
	for i, named := range curatedChannelOrder {
		if named == id {
			return i
		}
	}
	return len(curatedChannelOrder)
}

// titleize renders a directory-style id as a display name: "bluefin" becomes
// "Bluefin", "01-bluefin" becomes "01 Bluefin". The channels are named by
// their directory, so this only needs to be readable, not perfect.
func titleize(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, "-", " ")
	id = strings.ReplaceAll(id, "_", " ")
	if id == "" {
		return id
	}
	return strings.ToUpper(id[:1]) + id[1:]
}

// ResolveImage returns the absolute path of one channel image, or false when
// the file is not on disk. A selection is never written until it resolves, so
// a missing file reports as absent rather than applying a path that does not
// exist.
func ResolveImage(rel string) (string, bool) {
	path := filepath.Join(WallpaperBackgroundsRoot, rel)
	info, err := os.Stat(path)
	return path, err == nil && !info.IsDir()
}

// ChannelByID returns the channel with the given id.
func ChannelByID(id string) (WallpaperChannel, bool) {
	for _, channel := range WallpaperChannels() {
		if channel.ID == id {
			return channel, true
		}
	}
	return WallpaperChannel{}, false
}

// PreviewImage returns the channel's first raster image, for its thumbnail.
// GNOME's XML animated-background files are selectable wallpapers but not
// pictures GdkPixbuf can decode, so they are skipped; a channel holding only
// XML entries has no preview and keeps its placeholder.
func PreviewImage(channel WallpaperChannel) (WallpaperImage, bool) {
	for _, image := range channel.Images {
		if strings.ToLower(filepath.Ext(image.Path)) != ".xml" {
			return image, true
		}
	}
	return WallpaperImage{}, false
}

// ImageByID returns the image with the given id inside one channel.
func ImageByID(channel WallpaperChannel, id string) (WallpaperImage, bool) {
	for _, image := range channel.Images {
		if image.ID == id {
			return image, true
		}
	}
	return WallpaperImage{}, false
}

// NextImage returns the id of the wallpaper that follows current in the
// channel, wrapping from the last back to the first. It is total: an unknown
// current selects the first image, so a stale or hand-edited value still lands
// on a real entry rather than cycling to nothing.
func NextImage(channelID, current string) string {
	channel, ok := ChannelByID(channelID)
	if !ok || len(channel.Images) == 0 {
		return ""
	}
	for i, image := range channel.Images {
		if image.ID == current {
			next := i + 1
			if next >= len(channel.Images) {
				next = 0
			}
			return channel.Images[next].ID
		}
	}
	return channel.Images[0].ID
}
