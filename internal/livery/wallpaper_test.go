package livery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// writeFile creates path (and its parents) with body, failing the test on error.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// properties renders one gnome-background-properties list in the shape the
// ublue-os wallpaper casks ship (after their preflight replaced "~").
func properties(entries ...string) string {
	return `<?xml version="1.0"?>
<!DOCTYPE wallpapers SYSTEM "gnome-wp-list.dtd">
<wallpapers>` + strings.Join(entries, "") + `</wallpapers>`
}

func entry(name, file, dark string) string {
	s := `<wallpaper deleted="false"><name>` + name + `</name><filename>` + file + `</filename>`
	if dark != "" {
		s += `<filename-dark>` + dark + `</filename-dark>`
	}
	return s + `<options>zoom</options></wallpaper>`
}

// installedChannels lays out what bluefin-wallpapers and bazzite-wallpapers
// install under one data directory: pictures in backgrounds/<collection> and
// the catalog GNOME Settings reads in gnome-background-properties.
func installedChannels(t *testing.T) (dataDir string) {
	t.Helper()
	dataDir = t.TempDir()
	bluefin := filepath.Join(dataDir, "backgrounds", "bluefin")
	bazzite := filepath.Join(dataDir, "backgrounds", "bazzite")
	day := filepath.Join(bluefin, "01-bluefin-day.jxl")
	night := filepath.Join(bluefin, "01-bluefin-night.jxl")
	writeFile(t, day, "day")
	writeFile(t, night, "night")
	writeFile(t, filepath.Join(bluefin, "01-bluefin.xml"), `<background><starttime><hour>6</hour></starttime>
<static><duration>37200.0</duration><file>`+day+`</file></static>
<transition type="overlay"><duration>1800.0</duration><from>`+day+`</from><to>`+night+`</to></transition>
<static><duration>45600.0</duration><file>`+night+`</file></static>
</background>`)
	writeFile(t, filepath.Join(bluefin, "02-bluefin.png"), "png")
	writeFile(t, filepath.Join(bazzite, "Giants.jpg"), "jpg")
	props := filepath.Join(dataDir, "gnome-background-properties")
	writeFile(t, filepath.Join(props, "02-bluefin.xml"), properties(entry("Bluefin 02 - February", filepath.Join(bluefin, "02-bluefin.png"), "")))
	writeFile(t, filepath.Join(props, "01-bluefin.xml"), properties(entry("Bluefin 01 - January", filepath.Join(bluefin, "01-bluefin.xml"), "")))
	writeFile(t, filepath.Join(props, "bazzite.xml"), properties(entry("Bazzite Giants", filepath.Join(bazzite, "Giants.jpg"), "")))
	return dataDir
}

func TestWallpaperChannelsGroupsTheInstalledCatalogByCollection(t *testing.T) {
	dataDir := installedChannels(t)

	channels, err := WallpaperChannels([]string{dataDir})

	if err != nil {
		t.Fatalf("WallpaperChannels() error = %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("got %d channels, want 2: %+v", len(channels), channels)
	}
	bluefin, bazzite := channels[0], channels[1]
	if bluefin.ID != "bluefin" || bluefin.Name != "Bluefin" || bazzite.ID != "bazzite" || bazzite.Name != "Bazzite" {
		t.Fatalf("channels = %q/%q, %q/%q; want Bluefin first, then Bazzite", bluefin.ID, bluefin.Name, bazzite.ID, bazzite.Name)
	}
	if len(bluefin.Wallpapers) != 2 || bluefin.Wallpapers[0].Name != "Bluefin 01 - January" || bluefin.Wallpapers[1].Name != "Bluefin 02 - February" {
		t.Fatalf("bluefin wallpapers = %+v; want January then February", bluefin.Wallpapers)
	}
}

func TestWallpaperChannelsPreviewsADayNightSlideshowByItsFirstPicture(t *testing.T) {
	dataDir := installedChannels(t)

	channels, err := WallpaperChannels([]string{dataDir})

	if err != nil {
		t.Fatal(err)
	}
	january := channels[0].Wallpapers[0]
	wantFile := filepath.Join(dataDir, "backgrounds", "bluefin", "01-bluefin.xml")
	wantPreview := filepath.Join(dataDir, "backgrounds", "bluefin", "01-bluefin-day.jxl")
	if january.File != wantFile || january.Preview != wantPreview {
		t.Fatalf("january = %+v; want File %s and Preview %s", january, wantFile, wantPreview)
	}
	february := channels[0].Wallpapers[1]
	if february.Preview != february.File {
		t.Fatalf("a still picture previews as itself; got %+v", february)
	}
}

func TestWallpaperChannelsResolvesARelativeFileAgainstItsCatalog(t *testing.T) {
	// GNOME Settings resolves <filename> with
	// g_file_new_for_commandline_arg_and_cwd(content, dirname(catalog)).
	dataDir := t.TempDir()
	writeFile(t, filepath.Join(dataDir, "backgrounds", "gnome", "blobs-l.svg"), "svg")
	writeFile(t, filepath.Join(dataDir, "gnome-background-properties", "gnome.xml"),
		properties(entry("Blobs", "../backgrounds/gnome/blobs-l.svg", "")))

	channels, err := WallpaperChannels([]string{dataDir})

	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataDir, "backgrounds", "gnome", "blobs-l.svg")
	if len(channels) != 1 || channels[0].Name != "GNOME" || channels[0].Wallpapers[0].File != want {
		t.Fatalf("channels = %+v; want the GNOME collection with %s", channels, want)
	}
}

func TestWallpaperChannelsOffersNothingThatIsNotOnDisk(t *testing.T) {
	dataDir := t.TempDir()
	images := filepath.Join(dataDir, "backgrounds", "bluefin")
	kept := filepath.Join(images, "kept.png")
	writeFile(t, kept, "png")
	writeFile(t, filepath.Join(images, "deleted.png"), "png")
	writeFile(t, filepath.Join(dataDir, "loose.png"), "png")
	writeFile(t, filepath.Join(dataDir, "gnome-background-properties", "list.xml"), properties(
		entry("Kept", kept, ""),
		entry("Missing", filepath.Join(images, "gone.png"), ""),
		entry("A directory", images, ""),
		entry("No picture", "(none)", ""),
		`<wallpaper deleted="true"><name>Deleted</name><filename>`+filepath.Join(images, "deleted.png")+`</filename></wallpaper>`,
		entry("No collection", filepath.Join(dataDir, "loose.png"), ""),
	))

	channels, err := WallpaperChannels([]string{dataDir})

	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || len(channels[0].Wallpapers) != 1 || channels[0].Wallpapers[0].Name != "Kept" {
		t.Fatalf("channels = %+v; want only the one entry whose file exists in a collection", channels)
	}
}

func TestWallpaperChannelsDropsADarkVariantThatIsNotOnDisk(t *testing.T) {
	dataDir := t.TempDir()
	light := filepath.Join(dataDir, "backgrounds", "gnome", "l.png")
	dark := filepath.Join(dataDir, "backgrounds", "gnome", "d.png")
	writeFile(t, light, "l")
	writeFile(t, filepath.Join(dataDir, "gnome-background-properties", "a.xml"), properties(
		entry("Missing dark", light, dark),
	))

	channels, err := WallpaperChannels([]string{dataDir})

	if err != nil {
		t.Fatal(err)
	}
	if got := channels[0].Wallpapers[0].DarkFile; got != "" {
		t.Fatalf("DarkFile = %q; a missing dark picture must not be written", got)
	}
}

func TestWallpaperChannelsIsEmptyWhenNoCollectionIsInstalled(t *testing.T) {
	channels, err := WallpaperChannels([]string{filepath.Join(t.TempDir(), "absent")})

	if err != nil || len(channels) != 0 {
		t.Fatalf("WallpaperChannels(absent) = %+v, %v; want no channels and no error", channels, err)
	}
}

func TestWallpaperChannelsReportsAnUnreadableCatalogButKeepsTheRest(t *testing.T) {
	dataDir := installedChannels(t)
	writeFile(t, filepath.Join(dataDir, "gnome-background-properties", "broken.xml"), "<wallpapers><wallpaper>")

	channels, err := WallpaperChannels([]string{dataDir})

	if err == nil {
		t.Fatal("a malformed catalog file must be reported, not read as absence")
	}
	if len(channels) != 2 {
		t.Fatalf("one bad file hid the other collections: %+v", channels)
	}
}

func TestWallpaperChannelsPrefersTheUserCopyOfTheSameFile(t *testing.T) {
	user := installedChannels(t)
	system := t.TempDir()
	same := filepath.Join(user, "backgrounds", "bazzite", "Giants.jpg")
	writeFile(t, filepath.Join(system, "gnome-background-properties", "dup.xml"), properties(entry("System Giants", same, "")))

	channels, err := WallpaperChannels([]string{user, system})

	if err != nil {
		t.Fatal(err)
	}
	bazzite := channels[1]
	if len(bazzite.Wallpapers) != 1 || bazzite.Wallpapers[0].Name != "Bazzite Giants" {
		t.Fatalf("bazzite = %+v; want the user catalog's single entry", bazzite)
	}
}

func TestWallpaperDataDirsPutsTheUserDirectoryFirst(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/home/me/.local/share")
	t.Setenv("XDG_DATA_DIRS", "/usr/local/share:/usr/share")

	got := WallpaperDataDirs()

	want := []string{"/home/me/.local/share", "/usr/local/share", "/usr/share"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("WallpaperDataDirs() = %v, want %v", got, want)
	}
}

func TestWallpaperDataDirsFallsBackToTheXDGDefaults(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_DATA_DIRS", "")

	got := WallpaperDataDirs()

	want := []string{"/home/me/.local/share", "/usr/local/share", "/usr/share"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("WallpaperDataDirs() = %v, want %v", got, want)
	}
}

func TestSelectedWallpaperFindsTheCurrentPicture(t *testing.T) {
	channels := []WallpaperChannel{
		{ID: "bluefin", Wallpapers: []Wallpaper{{File: "/a/1.png"}, {File: "/a/2.png"}}},
		{ID: "bazzite", Wallpapers: []Wallpaper{{File: "/b/1.png"}}},
	}

	channel, index, ok := SelectedWallpaper(channels, "file:///a/2.png")
	if !ok || channel != 0 || index != 1 {
		t.Fatalf("SelectedWallpaper() = %d, %d, %v; want 0, 1, true", channel, index, ok)
	}
	if _, _, ok := SelectedWallpaper(channels, "file:///elsewhere.png"); ok {
		t.Fatal("a picture outside every channel selects nothing")
	}
}

func TestProbeNativeBackgroundNeverAsksOffGNOME(t *testing.T) {
	for _, desktop := range []deskenv.Desktop{deskenv.KDE, deskenv.Unknown} {
		t.Run(desktop.String(), func(t *testing.T) {
			useDesktop(t, desktop)
			f := newFakeCommands(t)

			supported, current, err := ProbeNativeBackground(context.Background())

			if supported || current != "" || err != nil {
				t.Fatalf("ProbeNativeBackground() = %v, %q, %v; want unsupported", supported, current, err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("a non-GNOME desktop still ran %v", f.calls)
			}
		})
	}
}

func TestProbeNativeBackgroundReadsTheCurrentPictureOnGNOME(t *testing.T) {
	useDesktop(t, deskenv.GNOME)
	f := newFakeCommands(t)
	f.reply["gsettings get org.gnome.desktop.background picture-uri"] = "'file:///home/me/a.png'\n"

	supported, current, err := ProbeNativeBackground(context.Background())

	if !supported || current != "file:///home/me/a.png" || err != nil {
		t.Fatalf("ProbeNativeBackground() = %v, %q, %v", supported, current, err)
	}
}

func TestProbeNativeBackgroundTreatsAMissingSchemaAsUnsupported(t *testing.T) {
	useDesktop(t, deskenv.GNOME)
	f := newFakeCommands(t)
	f.reply["gsettings get org.gnome.desktop.background"] = `No such schema "org.gnome.desktop.background"`
	f.fail["gsettings get org.gnome.desktop.background"] = errors.New("exit status 1")

	supported, _, err := ProbeNativeBackground(context.Background())

	if supported || err != nil {
		t.Fatalf("missing schema = %v, %v; want unsupported without error", supported, err)
	}
}

func TestProbeNativeBackgroundReportsAFailedReadAsAnError(t *testing.T) {
	useDesktop(t, deskenv.GNOME)
	f := newFakeCommands(t)
	f.reply["gsettings get org.gnome.desktop.background"] = "dconf is unreachable"
	f.fail["gsettings get org.gnome.desktop.background"] = errors.New("exit status 1")

	supported, _, err := ProbeNativeBackground(context.Background())

	if supported || err == nil {
		t.Fatalf("a failed read = %v, %v; want an error, not 'unsupported'", supported, err)
	}
}

func TestBackgroundURIEncodesAnAbsolutePath(t *testing.T) {
	got := BackgroundURI("/home/o'brien/My Pictures/a#1.png")

	want := "file:///home/o%27brien/My%20Pictures/a%231.png"
	if got != want {
		t.Fatalf("BackgroundURI() = %q, want %q", got, want)
	}
}

// keepWrites makes the fake gsettings answer reads with the values written,
// the way a working dconf does.
func keepWrites(f *fakeCommands, picture, dark string) {
	f.reply["gsettings get org.gnome.desktop.background picture-uri-dark"] = "'" + dark + "'"
	f.reply["gsettings get org.gnome.desktop.background picture-uri"] = "'" + picture + "'"
	f.reply["gsettings get org.gnome.desktop.background picture-options"] = "'zoom'"
}

func TestApplyBackgroundWritesBothKeysAndConfirmsByReadback(t *testing.T) {
	f := newFakeCommands(t)
	keepWrites(f, "file:///home/me/a.png", "file:///home/me/a.png")

	err := ApplyBackground(context.Background(), Wallpaper{Name: "A", File: "/home/me/a.png"})

	if err != nil {
		t.Fatalf("ApplyBackground() = %v", err)
	}
	for _, want := range []string{
		`gsettings set org.gnome.desktop.background picture-uri "file:///home/me/a.png"`,
		`gsettings set org.gnome.desktop.background picture-uri-dark "file:///home/me/a.png"`,
	} {
		if !containsLine(f.calls, want) {
			t.Fatalf("calls %v lack %q", f.calls, want)
		}
	}
	if containsPrefix(f.calls, "gsettings set org.gnome.desktop.background picture-options") {
		t.Fatalf("picture-options was rewritten although it already shows a picture: %v", f.calls)
	}
}

func TestApplyBackgroundUsesTheDarkVariantWhenTheCatalogNamesOne(t *testing.T) {
	f := newFakeCommands(t)
	keepWrites(f, "file:///b/light.png", "file:///b/dark.png")

	err := ApplyBackground(context.Background(), Wallpaper{File: "/b/light.png", DarkFile: "/b/dark.png"})

	if err != nil {
		t.Fatal(err)
	}
	if !containsLine(f.calls, `gsettings set org.gnome.desktop.background picture-uri-dark "file:///b/dark.png"`) {
		t.Fatalf("dark key not written from filename-dark: %v", f.calls)
	}
}

func TestApplyBackgroundRestoresAPictureModeWhenTheDesktopShowsNone(t *testing.T) {
	// GNOME Settings' set_background does the same: a picture with
	// picture-options 'none' would never be drawn.
	f := newFakeCommands(t)
	keepWrites(f, "file:///a.png", "file:///a.png")
	f.reply["gsettings get org.gnome.desktop.background picture-options"] = "'none'"

	err := ApplyBackground(context.Background(), Wallpaper{File: "/a.png", Options: "spanned"})

	if err != nil {
		t.Fatal(err)
	}
	if !containsLine(f.calls, `gsettings set org.gnome.desktop.background picture-options 'spanned'`) {
		t.Fatalf("picture-options not restored from the catalog: %v", f.calls)
	}
}

func TestApplyBackgroundFallsBackToZoomForAnUnknownCatalogOption(t *testing.T) {
	f := newFakeCommands(t)
	keepWrites(f, "file:///a.png", "file:///a.png")
	f.reply["gsettings get org.gnome.desktop.background picture-options"] = "'none'"

	err := ApplyBackground(context.Background(), Wallpaper{File: "/a.png", Options: "'; rm -rf"})

	if err != nil {
		t.Fatal(err)
	}
	if !containsLine(f.calls, `gsettings set org.gnome.desktop.background picture-options 'zoom'`) {
		t.Fatalf("an unknown option must become GNOME's default, got %v", f.calls)
	}
}

func TestApplyBackgroundFailsWhenTheDesktopDidNotKeepTheValue(t *testing.T) {
	f := newFakeCommands(t)
	keepWrites(f, "file:///old.png", "file:///home/me/a.png")

	err := ApplyBackground(context.Background(), Wallpaper{File: "/home/me/a.png"})

	if err == nil {
		t.Fatal("a write the desktop did not keep must not read as applied")
	}
}

func TestApplyBackgroundReportsAFailedWrite(t *testing.T) {
	f := newFakeCommands(t)
	f.fail["gsettings set org.gnome.desktop.background picture-uri "] = errors.New("exit status 1")

	err := ApplyBackground(context.Background(), Wallpaper{File: "/home/me/a.png"})

	if err == nil {
		t.Fatal("a failed gsettings set must be an error")
	}
}

func TestApplyBackgroundWritesNothingUnderDryRun(t *testing.T) {
	f := newFakeCommands(t)
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	err := ApplyBackground(context.Background(), Wallpaper{File: "/home/me/a.png"})

	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("dry run ran %v", f.calls)
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func containsPrefix(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func TestWallpaperChannelsLetsAUserDeletionHideTheSystemEntry(t *testing.T) {
	// A deleted="true" entry in the user catalog is how GNOME Settings hides
	// a system wallpaper; the system copy must not reappear here.
	user, system := t.TempDir(), t.TempDir()
	picture := filepath.Join(system, "backgrounds", "gnome", "blobs.png")
	writeFile(t, picture, "png")
	writeFile(t, filepath.Join(system, "gnome-background-properties", "gnome.xml"), properties(entry("Blobs", picture, "")))
	writeFile(t, filepath.Join(user, "gnome-background-properties", "hidden.xml"), properties(
		`<wallpaper deleted="true"><name>Blobs</name><filename>`+picture+`</filename></wallpaper>`,
	))

	channels, err := WallpaperChannels([]string{user, system})

	if err != nil || len(channels) != 0 {
		t.Fatalf("WallpaperChannels() = %+v, %v; a user-deleted picture must stay hidden", channels, err)
	}
}

func TestWallpaperChannelsNamesAMultibyteCollectionWithoutBreakingIt(t *testing.T) {
	dataDir := t.TempDir()
	picture := filepath.Join(dataDir, "backgrounds", "été-sombre", "a.png")
	writeFile(t, picture, "png")
	writeFile(t, filepath.Join(dataDir, "gnome-background-properties", "a.xml"), properties(entry("A", picture, "")))

	channels, err := WallpaperChannels([]string{dataDir})

	if err != nil || len(channels) != 1 || channels[0].Name != "Été Sombre" {
		t.Fatalf("channels = %+v, %v; want the collection named \"Été Sombre\"", channels, err)
	}
}

func TestApplyBackgroundWritesNothingWhenThePlacementCannotBeRead(t *testing.T) {
	// picture-options is read before any write, so a failed read cannot
	// leave the light picture changed behind a failure toast.
	f := newFakeCommands(t)
	f.fail["gsettings get org.gnome.desktop.background picture-options"] = errors.New("exit status 1")

	err := ApplyBackground(context.Background(), Wallpaper{File: "/a.png"})

	if err == nil {
		t.Fatal("a failed placement read must fail the pick")
	}
	if containsPrefix(f.calls, "gsettings set") {
		t.Fatalf("a key was written before the pick could succeed: %v", f.calls)
	}
}

func TestApplyBackgroundNamesAMissingSchemaAsUnavailable(t *testing.T) {
	f := newFakeCommands(t)
	f.reply["gsettings get org.gnome.desktop.background"] = `No such schema "org.gnome.desktop.background"`
	f.fail["gsettings get org.gnome.desktop.background"] = errors.New("exit status 1")

	err := ApplyBackground(context.Background(), Wallpaper{File: "/a.png"})

	if !errors.Is(err, ErrBackgroundUnavailable) {
		t.Fatalf("ApplyBackground() = %v; want ErrBackgroundUnavailable, not the extension's error", err)
	}
	if errors.Is(err, ErrExtensionMissing) {
		t.Fatalf("a missing background schema must not read as the Custom Command Menu extension: %v", err)
	}
}

func TestGsettingsGetIgnoresAWarningPrintedBeforeTheValue(t *testing.T) {
	f := newFakeCommands(t)
	f.reply["gsettings get org.gnome.desktop.background picture-uri"] =
		"(process:42): GLib-GIO-WARNING **: Using the 'memory' GSettings backend.\n'file:///a.png'\n"

	got, err := gsettingsGet(context.Background(), NativeBackgroundSchema, "picture-uri")

	if err != nil || got != "file:///a.png" {
		t.Fatalf("gsettingsGet() = %q, %v; want the value line only", got, err)
	}
}

func TestWallpaperChannelsReportsAPictureItCannotCheck(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can stat through a mode-0 directory")
	}
	dataDir := t.TempDir()
	locked := filepath.Join(dataDir, "backgrounds", "locked")
	picture := filepath.Join(locked, "a.png")
	writeFile(t, picture, "png")
	writeFile(t, filepath.Join(dataDir, "gnome-background-properties", "a.xml"), properties(entry("A", picture, "")))
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := WallpaperChannels([]string{dataDir})

	if err == nil {
		t.Fatal("a picture that cannot be checked must be reported, not read as uninstalled")
	}
}
