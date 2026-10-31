package livery

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// withWallpaperRoot points WallpaperBackgroundsRoot at a fixture tree built
// from write, and restores it afterwards. It returns the root so a test can
// also create files directly.
func withWallpaperRoot(t *testing.T, write map[string]string) string {
	t.Helper()
	root := t.TempDir()
	original := WallpaperBackgroundsRoot
	WallpaperBackgroundsRoot = root
	t.Cleanup(func() { WallpaperBackgroundsRoot = original })
	for rel, content := range write {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestWallpaperChannelsEmptyWhenRootAbsent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist")
	original := WallpaperBackgroundsRoot
	WallpaperBackgroundsRoot = root
	t.Cleanup(func() { WallpaperBackgroundsRoot = original })

	if got := WallpaperChannels(); got != nil {
		t.Fatalf("expected nil catalog for an absent root, got %+v", got)
	}
}

func TestWallpaperChannelsDiscoversSubdirectoriesInCuratedOrder(t *testing.T) {
	withWallpaperRoot(t, map[string]string{
		"bazzite/cosmic.png":     "b",
		"bazzite/forest.jpg":     "b",
		"bluefin/01-bluefin.xml": "b",
		"bluefin/aurora.png":     "b",
		"framework/mountain.png": "f",
	})

	got := WallpaperChannels()
	want := []WallpaperChannel{
		{ID: "bluefin", Name: "Bluefin", Images: []WallpaperImage{
			{ID: "bluefin/01-bluefin.xml", Name: "01 bluefin", Path: "bluefin/01-bluefin.xml"},
			{ID: "bluefin/aurora.png", Name: "Aurora", Path: "bluefin/aurora.png"},
		}},
		{ID: "bazzite", Name: "Bazzite", Images: []WallpaperImage{
			{ID: "bazzite/cosmic.png", Name: "Cosmic", Path: "bazzite/cosmic.png"},
			{ID: "bazzite/forest.jpg", Name: "Forest", Path: "bazzite/forest.jpg"},
		}},
		{ID: "framework", Name: "Framework", Images: []WallpaperImage{
			{ID: "framework/mountain.png", Name: "Mountain", Path: "framework/mountain.png"},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("channels mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestWallpaperChannelsSkipsChannelsWithoutImages(t *testing.T) {
	withWallpaperRoot(t, map[string]string{
		"bluefin/aurora.png": "b",
		"empty/readme.txt":   "not a wallpaper",
		"notes.txt":          "a stray file at the root is not a channel",
	})

	got := WallpaperChannels()
	if len(got) != 1 || got[0].ID != "bluefin" {
		t.Fatalf("expected only the bluefin channel, got %+v", got)
	}
}

func TestResolveImageRejectsMissingFile(t *testing.T) {
	withWallpaperRoot(t, map[string]string{
		"bluefin/aurora.png": "b",
	})
	if path, ok := ResolveImage("bluefin/aurora.png"); !ok || path != filepath.Join(WallpaperBackgroundsRoot, "bluefin/aurora.png") {
		t.Fatalf("ResolveImage should locate an existing file, got (%q, %t)", path, ok)
	}
	if _, ok := ResolveImage("bluefin/missing.png"); ok {
		t.Fatal("ResolveImage accepted a file that is not on disk")
	}
}

func TestNextImageWrapsAtChannelEnd(t *testing.T) {
	withWallpaperRoot(t, map[string]string{
		"bluefin/a.png": "b",
		"bluefin/b.png": "b",
		"bluefin/c.png": "b",
	})
	if got := NextImage("bluefin", "bluefin/a.png"); got != "bluefin/b.png" {
		t.Fatalf("next after a.png = %q, want bluefin/b.png", got)
	}
	if got := NextImage("bluefin", "bluefin/c.png"); got != "bluefin/a.png" {
		t.Fatalf("next after c.png = %q, want wrap to bluefin/a.png", got)
	}
	if got := NextImage("bluefin", "bluefin/unknown.png"); got != "bluefin/a.png" {
		t.Fatalf("unknown current = %q, want first image bluefin/a.png", got)
	}
}

func TestChannelAndImageByID(t *testing.T) {
	withWallpaperRoot(t, map[string]string{
		"bluefin/aurora.png": "b",
	})
	channel, ok := ChannelByID("bluefin")
	if !ok || len(channel.Images) != 1 {
		t.Fatalf("ChannelByID(bluefin) = %+v, %t", channel, ok)
	}
	image, ok := ImageByID(channel, "bluefin/aurora.png")
	if !ok || image.Name != "Aurora" {
		t.Fatalf("ImageByID = %+v, %t", image, ok)
	}
	if _, ok := ChannelByID("nope"); ok {
		t.Fatal("ChannelByID accepted an unknown id")
	}
}

func TestPreviewImageSkipsSlideshowXML(t *testing.T) {
	withWallpaperRoot(t, map[string]string{
		"bluefin/01-bluefin.xml": "b",
		"bluefin/aurora.png":     "b",
		"slides/only.XML":        "s",
	})
	channel, _ := ChannelByID("bluefin")
	image, ok := PreviewImage(channel)
	if !ok || image.ID != "bluefin/aurora.png" {
		t.Fatalf("PreviewImage(bluefin) = %+v, %t; want the first raster image", image, ok)
	}
	slides, _ := ChannelByID("slides")
	if image, ok := PreviewImage(slides); ok {
		t.Fatalf("PreviewImage(slides) = %+v; an XML-only channel has no preview", image)
	}
}
