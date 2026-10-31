package livery

import (
	"context"
	"errors"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestBackgroundURIIsAFileURI(t *testing.T) {
	for path, want := range map[string]string{
		"/usr/share/backgrounds/bluefin/aurora.png": "file:///usr/share/backgrounds/bluefin/aurora.png",
		"/home/a b/100% done/moon.jxl":              "file:///home/a%20b/100%25%20done/moon.jxl",
	} {
		if got := BackgroundURI(path); got != want {
			t.Errorf("BackgroundURI(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestApplyNativeBackgroundWritesFileURIs pins the value, not only the key:
// GNOME resolves picture-uri with g_filename_from_uri, so a bare path would
// leave the desktop picture unchanged.
func TestApplyNativeBackgroundWritesFileURIs(t *testing.T) {
	f := newFakeCommands(t)
	file := "/usr/share/backgrounds/bluefin/aurora.png"

	if err := ApplyNativeBackground(context.Background(), file); err != nil {
		t.Fatalf("ApplyNativeBackground: %v", err)
	}
	for _, key := range []string{pictureURIKey, pictureURIDarkKey} {
		want := "gsettings set " + NativeBackgroundSchema + " " + key + ` "file://` + file + `"`
		if !f.sawPrefix(want) {
			t.Errorf("missing %q in %v", want, f.calls)
		}
	}
}

// TestSelectWallpaperPersistsTheChannel covers what the rotation pass needs:
// without the stored channel RotateWallpaper has nothing to advance.
func TestSelectWallpaperPersistsTheChannel(t *testing.T) {
	f := newFakeCommands(t)

	if err := SelectWallpaper(context.Background(), "bluefin", "bluefin/aurora.png", "/b/bluefin/aurora.png"); err != nil {
		t.Fatalf("SelectWallpaper: %v", err)
	}
	if !f.sawPrefix("gsettings set " + NativeBackgroundSchema + " " + pictureURIKey + ` "file:///b/bluefin/aurora.png"`) {
		t.Errorf("background not applied: %v", f.calls)
	}
	for _, want := range []string{setCall(KeyWallpaperChannel, "bluefin"), setCall(KeyWallpaperImage, "bluefin/aurora.png")} {
		if !f.sawPrefix(want) {
			t.Errorf("missing %q in %v", want, f.calls)
		}
	}
}

// TestApplyNativeBackgroundRenamesMissingSchemaError pins that a write failing
// because GNOME's background schema is absent surfaces ErrBackgroundSchemaMissing
// rather than gsettingsSet's ErrExtensionMissing, whose text blames an unrelated
// GNOME Shell extension, so the journal stays truthful.
func TestApplyNativeBackgroundRenamesMissingSchemaError(t *testing.T) {
	f := newFakeCommands(t)
	// A missing schema: the tool exits nonzero with "No such schema" output, so
	// gsettingsSet maps it to ErrExtensionMissing.
	f.reply["gsettings set "+NativeBackgroundSchema+" "+pictureURIKey] = "No such schema"
	f.fail["gsettings set "+NativeBackgroundSchema+" "+pictureURIKey] = errors.New("gsettings: schema missing")
	err := ApplyNativeBackground(context.Background(), "/usr/share/backgrounds/bluefin/aurora.png")
	if !errors.Is(err, ErrBackgroundSchemaMissing) {
		t.Fatalf("ApplyNativeBackground: err = %v, want %v", err, ErrBackgroundSchemaMissing)
	}
	if errors.Is(err, ErrExtensionMissing) {
		t.Fatalf("ApplyNativeBackground: err still names ErrExtensionMissing: %v", err)
	}
}

func TestSelectWallpaperRespectsDryRun(t *testing.T) {
	f := newFakeCommands(t)
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	if err := SelectWallpaper(context.Background(), "bluefin", "bluefin/aurora.png", "/b/bluefin/aurora.png"); err != nil {
		t.Fatalf("SelectWallpaper: %v", err)
	}
	if f.sawPrefix("gsettings set") {
		t.Errorf("dry-run selection wrote settings: %v", f.calls)
	}
}
