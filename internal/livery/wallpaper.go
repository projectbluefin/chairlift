package livery

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// Wallpaper channels are the wallpaper collections already installed on this
// computer, read from the same catalog GNOME Settings' Appearance panel reads:
// every gnome-background-properties/*.xml list in the XDG data directories.
// The ublue-os wallpaper casks install their pictures into
// $XDG_DATA_HOME/backgrounds/<collection>/ and link one catalog entry per
// picture there, so a collection's directory is its channel. Nothing here
// downloads, installs, or invents a picture: a channel exists only while its
// files are on disk.

// NativeBackgroundSchema is GNOME's desktop-background schema. ChairLift does
// not ship it, so a host without it cannot take a wallpaper from here.
const NativeBackgroundSchema = "org.gnome.desktop.background"

const (
	pictureURIKey     = "picture-uri"
	pictureURIDarkKey = "picture-uri-dark"
	pictureOptionsKey = "picture-options"
	// backgroundPropertiesDir is the catalog directory GNOME Settings scans
	// under the user data directory and each system data directory.
	backgroundPropertiesDir = "gnome-background-properties"
	// backgroundsDir is the parent of every collection directory.
	backgroundsDir = "backgrounds"
	// noPicture is the catalog's spelling of an entry without a file
	// (cc-background-xml.c's NONE).
	noPicture = "(none)"
)

// pictureOptions is GDesktopBackgroundStyle without "none": the closed set a
// catalog <options> value may restore picture-options to.
var pictureOptions = map[string]bool{
	"wallpaper": true, "centered": true, "scaled": true,
	"stretched": true, "zoom": true, "spanned": true,
}

// defaultPictureOption is the schema default for picture-options.
const defaultPictureOption = "zoom"

// Wallpaper is one picture a channel offers.
type Wallpaper struct {
	// Name is the catalog's display name.
	Name string
	// File is the absolute path written to picture-uri: a still picture or a
	// GNOME day/night slideshow XML.
	File string
	// DarkFile is the catalog's filename-dark, or empty when it names none.
	DarkFile string
	// Preview is a raster picture for the thumbnail: File itself, or a
	// slideshow's first still. Empty when there is nothing to draw.
	Preview string
	// Options is the catalog's placement, such as "zoom".
	Options string
}

// WallpaperChannel is one installed collection, such as Bluefin or Bazzite.
type WallpaperChannel struct {
	// ID is the collection's directory name under backgrounds/.
	ID string
	// Name is the display name.
	Name string
	// Wallpapers is ordered by name, which for the monthly Bluefin set is
	// January to December.
	Wallpapers []Wallpaper
}

// channelOrder puts the curated collections first, in the order the issue
// names them; any other installed collection follows alphabetically.
var channelOrder = []string{"bluefin", "bluefin-extra", "bazzite", "framework"}

// channelNames spells collections whose directory name is not their name.
var channelNames = map[string]string{
	"bluefin-extra": "Bluefin Extra",
	"gnome":         "GNOME",
}

// WallpaperDataDirs returns the directories whose gnome-background-properties
// lists GNOME Settings reads, user directory first, following the XDG Base
// Directory defaults when the variables are unset.
func WallpaperDataDirs() []string {
	user := os.Getenv("XDG_DATA_HOME")
	if user == "" {
		if home, err := os.UserHomeDir(); err == nil {
			user = filepath.Join(home, ".local", "share")
		}
	}
	system := os.Getenv("XDG_DATA_DIRS")
	if system == "" {
		system = "/usr/local/share:/usr/share"
	}
	var dirs []string
	if user != "" {
		dirs = append(dirs, user)
	}
	for _, dir := range strings.Split(system, ":") {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// WallpaperChannels reads every catalog under dataDirs and groups the entries
// whose pictures exist by collection. An absent catalog directory is no
// channel, not an error; a catalog that cannot be read or parsed is reported,
// while every other catalog still contributes, so one broken file never hides
// the rest. The same picture listed twice keeps its first (user) entry.
func WallpaperChannels(dataDirs []string) ([]WallpaperChannel, error) {
	byID := map[string]*WallpaperChannel{}
	seen := map[string]bool{}
	var errs []error
	for _, dataDir := range dataDirs {
		dir := filepath.Join(dataDir, backgroundPropertiesDir)
		files, err := os.ReadDir(dir)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("livery: reading %s: %w", dir, err))
			}
			continue
		}
		for _, file := range files {
			if file.IsDir() || !strings.EqualFold(filepath.Ext(file.Name()), ".xml") {
				continue
			}
			path := filepath.Join(dir, file.Name())
			wallpapers, deleted, err := readBackgroundProperties(path)
			if err != nil {
				errs = append(errs, err)
			}
			// A deleted entry hides the same picture from every later
			// (lower-priority) catalog, which is how GNOME Settings lets a
			// user remove a system wallpaper.
			for _, file := range deleted {
				seen[file] = true
			}
			for _, w := range wallpapers {
				id, ok := collectionOf(w.File)
				if !ok || seen[w.File] {
					continue
				}
				seen[w.File] = true
				channel := byID[id]
				if channel == nil {
					channel = &WallpaperChannel{ID: id, Name: channelName(id)}
					byID[id] = channel
				}
				channel.Wallpapers = append(channel.Wallpapers, w)
			}
		}
	}
	return sortedChannels(byID), errors.Join(errs...)
}

func sortedChannels(byID map[string]*WallpaperChannel) []WallpaperChannel {
	channels := make([]WallpaperChannel, 0, len(byID))
	for _, channel := range byID {
		sort.SliceStable(channel.Wallpapers, func(i, j int) bool {
			return channel.Wallpapers[i].Name < channel.Wallpapers[j].Name
		})
		channels = append(channels, *channel)
	}
	sort.Slice(channels, func(i, j int) bool {
		ri, rj := channelRank(channels[i].ID), channelRank(channels[j].ID)
		if ri != rj {
			return ri < rj
		}
		return channels[i].ID < channels[j].ID
	})
	return channels
}

func channelRank(id string) int {
	for i, curated := range channelOrder {
		if curated == id {
			return i
		}
	}
	return len(channelOrder)
}

func channelName(id string) string {
	if name, ok := channelNames[id]; ok {
		return name
	}
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' })
	for i, word := range words {
		first, size := utf8.DecodeRuneInString(word)
		words[i] = string(unicode.ToUpper(first)) + word[size:]
	}
	return strings.Join(words, " ")
}

// collectionOf names the collection a picture belongs to: the directory
// directly under a backgrounds/ directory. A picture anywhere else has no
// collection and is not offered as part of a channel.
func collectionOf(file string) (string, bool) {
	parent := filepath.Dir(file)
	if filepath.Base(filepath.Dir(parent)) != backgroundsDir {
		return "", false
	}
	return filepath.Base(parent), true
}

// backgroundList is the gnome-wp-list.dtd shape of one catalog file.
type backgroundList struct {
	Wallpapers []struct {
		Deleted  string `xml:"deleted,attr"`
		Name     string `xml:"name"`
		Filename string `xml:"filename"`
		Dark     string `xml:"filename-dark"`
		Options  string `xml:"options"`
	} `xml:"wallpaper"`
}

// readBackgroundProperties returns the usable entries of one catalog file and
// the pictures it marks deleted, applying GNOME Settings' own rules
// (cc-background-xml.c): "(none)" is skipped, a relative filename is resolved
// against the catalog's directory, and an entry whose picture is not a
// regular file on disk is dropped. A picture that cannot be checked (a stat
// error other than "does not exist") is reported rather than read as
// uninstalled. A "~/" path is not expanded, because GNOME does not expand it
// either; the casks replace it before linking their catalog.
func readBackgroundProperties(path string) (wallpapers []Wallpaper, deleted []string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("livery: reading %s: %w", path, err)
	}
	var list backgroundList
	if err := xml.Unmarshal(data, &list); err != nil {
		return nil, nil, fmt.Errorf("livery: parsing %s: %w", path, err)
	}
	base := filepath.Dir(path)
	var errs []error
	for _, e := range list.Wallpapers {
		if isDeleted, _ := strconv.ParseBool(strings.TrimSpace(e.Deleted)); isDeleted {
			if file, ok := catalogPath(base, e.Filename); ok {
				deleted = append(deleted, file)
			}
			continue
		}
		file, ok, statErr := catalogFile(base, e.Filename)
		if statErr != nil {
			errs = append(errs, fmt.Errorf("livery: checking %s from %s: %w", file, path, statErr))
		}
		if !ok {
			continue
		}
		dark, darkOK, _ := catalogFile(base, e.Dark)
		if !darkOK {
			dark = ""
		}
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		}
		wallpapers = append(wallpapers, Wallpaper{
			Name:     name,
			File:     file,
			DarkFile: dark,
			Preview:  previewOf(file),
			Options:  strings.TrimSpace(e.Options),
		})
	}
	return wallpapers, deleted, errors.Join(errs...)
}

// catalogPath resolves one <filename> value to a clean absolute path.
func catalogPath(base, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == noPicture {
		return "", false
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	return filepath.Clean(value), true
}

// catalogFile resolves one <filename> value to an existing regular file. A
// missing file is simply absent; any other stat failure is returned.
func catalogFile(base, value string) (string, bool, error) {
	path, ok := catalogPath(base, value)
	if !ok {
		return "", false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return path, false, err
	}
	return path, info.Mode().IsRegular(), nil
}

// slideshow is the part of GNOME's animated background format a thumbnail
// needs: its still frames, in order.
type slideshow struct {
	Static []struct {
		File string `xml:"file"`
	} `xml:"static"`
}

// previewOf returns a picture a thumbnail can draw for file: file itself, or
// for a slideshow XML its first still that exists. Empty means none.
func previewOf(file string) string {
	if !strings.EqualFold(filepath.Ext(file), ".xml") {
		return file
	}
	data, err := os.ReadFile(file)
	if err != nil {
		log.Printf("livery: reading slideshow %s: %v", file, err)
		return ""
	}
	var show slideshow
	if err := xml.Unmarshal(data, &show); err != nil {
		log.Printf("livery: parsing slideshow %s: %v", file, err)
		return ""
	}
	for _, still := range show.Static {
		if path, ok, _ := catalogFile(filepath.Dir(file), still.File); ok {
			return path
		}
	}
	return ""
}

// SelectedWallpaper finds the channel and wallpaper whose file is the current
// picture-uri. Paths are compared decoded, because GLib and Go escape a few
// characters differently in a file:// URI.
func SelectedWallpaper(channels []WallpaperChannel, currentURI string) (channel, index int, ok bool) {
	current, ok := uriPath(currentURI)
	if !ok {
		return 0, 0, false
	}
	for c, ch := range channels {
		for i, w := range ch.Wallpapers {
			if w.File == current {
				return c, i, true
			}
		}
	}
	return 0, 0, false
}

func uriPath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return "", false
	}
	return filepath.Clean(u.Path), true
}

// BackgroundURI returns the file:// URI GNOME's background keys hold for an
// absolute path; the schema accepts only local file URIs.
func BackgroundURI(file string) string {
	return (&url.URL{Scheme: "file", Path: filepath.Clean(file)}).String()
}

// ProbeNativeBackground reports whether this session can take a wallpaper
// from ChairLift, and the current picture-uri when it can. Only GNOME draws
// org.gnome.desktop.background, so any other desktop is unsupported without
// running anything: Plasma and other desktops often ship the schema too, and
// writing it there would change nothing on screen. On GNOME a missing schema
// is unsupported; any other failed read is returned as an error, so the page
// can say it could not check instead of hiding the channels as unsupported.
func ProbeNativeBackground(ctx context.Context) (supported bool, currentURI string, err error) {
	if detectDesktop() != deskenv.GNOME {
		return false, "", nil
	}
	current, err := gsettingsGet(ctx, NativeBackgroundSchema, pictureURIKey)
	if errors.Is(err, ErrExtensionMissing) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("livery: checking the desktop background: %w", err)
	}
	return true, current, nil
}

// ErrBackgroundUnavailable means GNOME's background schema is not installed,
// so no pick can work until the page checks again.
var ErrBackgroundUnavailable = errors.New("livery: GNOME's desktop-background settings are not installed")

// ErrBackgroundNotApplied means the desktop did not keep the background that
// was written, so the selection must not be reported as applied.
var ErrBackgroundNotApplied = errors.New("livery: the desktop did not keep the new background")

// ApplyBackground sets w as the desktop background the way GNOME Settings'
// set_background does: picture-uri, then picture-uri-dark from the catalog's
// dark picture or the same picture, and a placement when picture-options is
// "none" (which would draw no picture at all). Success is confirmed by
// reading both keys back, not by gsettings exiting 0. Under dry-run nothing is
// run.
func ApplyBackground(ctx context.Context, w Wallpaper) error {
	light := BackgroundURI(w.File)
	dark := light
	if w.DarkFile != "" {
		dark = BackgroundURI(w.DarkFile)
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would set %s %s=%s and %s=%s", NativeBackgroundSchema, pictureURIKey, light, pictureURIDarkKey, dark)
		return nil
	}
	// Every read that can fail runs before the first write, so a failure
	// here leaves the desktop exactly as it was.
	placement, err := getBackgroundKey(ctx, pictureOptionsKey)
	if err != nil {
		return err
	}
	if err := setBackgroundKey(ctx, pictureURIKey, strconv.Quote(light)); err != nil {
		return err
	}
	if err := setBackgroundKey(ctx, pictureURIDarkKey, strconv.Quote(dark)); err != nil {
		return err
	}
	if placement == "none" {
		if err := setBackgroundKey(ctx, pictureOptionsKey, "'"+pictureOption(w.Options)+"'"); err != nil {
			return err
		}
	}
	return confirmBackground(ctx, light, dark)
}

// pictureOption is the catalog's placement when it is one GNOME draws a
// picture with, else the schema default.
func pictureOption(option string) string {
	if pictureOptions[option] {
		return option
	}
	return defaultPictureOption
}

// backgroundError renames the shared gsettings helpers' schema-missing error,
// whose text names the Custom Command Menu extension, to the background's own.
func backgroundError(action, key string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrExtensionMissing) {
		return fmt.Errorf("%w (%s %s)", ErrBackgroundUnavailable, action, key)
	}
	return fmt.Errorf("livery: %s %s: %w", action, key, err)
}

func getBackgroundKey(ctx context.Context, key string) (string, error) {
	value, err := gsettingsGet(ctx, NativeBackgroundSchema, key)
	return value, backgroundError("reading", key, err)
}

func setBackgroundKey(ctx context.Context, key, value string) error {
	return backgroundError("writing", key, gsettingsSet(ctx, NativeBackgroundSchema, key, value))
}

// confirmBackground reads both keys back. Paths are compared decoded, so the
// readback is not tripped by GLib escaping a character Go left alone.
func confirmBackground(ctx context.Context, light, dark string) error {
	for _, check := range []struct{ key, want string }{
		{pictureURIKey, light},
		{pictureURIDarkKey, dark},
	} {
		got, err := getBackgroundKey(ctx, check.key)
		if err != nil {
			return err
		}
		gotPath, okGot := uriPath(got)
		wantPath, _ := uriPath(check.want)
		if !okGot || gotPath != wantPath {
			return fmt.Errorf("%w: %s is %q", ErrBackgroundNotApplied, check.key, got)
		}
	}
	return nil
}
