package valhalla

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

type fixture struct {
	root, data, folders, active string
	calls                       []string
	installed, access           bool
	fail, badOutput             string
	untapped                    bool
}

func setup(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{root: filepath.Join(base, "Caskroom", "bluefin-wallpapers-extra"), data: filepath.Join(base, "data"), folders: `['/existing, folder', '/quote\'s', '/back\\slash']`, active: "'wallhaven'"}
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_DATA_HOME", f.data)
	if err := os.MkdirAll(filepath.Join(f.root, "2026-05-09", "images"), 0755); err != nil {
		t.Fatal(err)
	}
	oldRun, oldBrew, oldLookup := runCommand, brewExecutable, lookPath
	oldTap, oldTrust, oldInstall, oldFlatpak := tap, trustCask, installPackage, installFlatpak
	t.Cleanup(func() {
		runCommand, brewExecutable, lookPath = oldRun, oldBrew, oldLookup
		tap, trustCask, installPackage, installFlatpak = oldTap, oldTrust, oldInstall, oldFlatpak
		dryrun.Set(false)
	})
	dryrun.Set(false)
	brewExecutable = func() string { return filepath.Join(base, "brew", "bin", "brew") }
	lookPath = func(name string) (string, error) { return "/tools/" + name, nil }
	tap = func(name string) error {
		f.calls = append(f.calls, "tap "+name)
		if err := f.failure("tap"); err != nil {
			return err
		}
		f.untapped = false
		return nil
	}
	trustCask = func(name string) error { f.calls = append(f.calls, "trust "+name); return f.failure("trust") }
	installPackage = func(name string, cask bool) error {
		f.calls = append(f.calls, "install-package "+name)
		if !cask && name != "librsvg" && name != "jpeg-xl" && name != "libavif" {
			t.Fatalf("unexpected converter %q", name)
		}
		return f.failure("install-package")
	}
	installFlatpak = func() error {
		f.calls = append(f.calls, "install-flatpak flatpark "+AppID)
		if err := f.failure("install-flatpak"); err != nil {
			return err
		}
		f.installed = true
		return nil
	}
	runCommand = func(ctx context.Context, exe string, args ...string) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("external command has no deadline")
		}
		line := filepath.Base(exe) + " " + strings.Join(args, " ")
		f.calls = append(f.calls, line)
		if f.fail != "" && strings.Contains(line, f.fail) {
			return "failure detail", errors.New("command failed")
		}
		switch filepath.Base(exe) {
		case "brew":
			if len(args) == 1 && args[0] == "tap" {
				if f.untapped {
					return "", nil
				}
				return "ublue-os/tap\n", nil
			}
			if args[0] == "info" {
				if f.untapped {
					return "", errors.New("tap is not installed")
				}
				if !slices.Equal(args, []string{"info", "--json=v2", "--cask", "ublue-os/tap/bluefin-wallpapers-extra"}) {
					return "multiple taps contain bluefin-wallpapers-extra", errors.New("ambiguous cask reference")
				}
				return `{"casks":[{"token":"bluefin-wallpapers-extra","version":"9999","installed":"2026-05-09"}]}`, nil
			}
			if args[0] == "--caskroom" {
				if !slices.Equal(args, []string{"--caskroom", "ublue-os/tap/bluefin-wallpapers-extra"}) {
					return "multiple taps contain bluefin-wallpapers-extra", errors.New("ambiguous cask reference")
				}
				return f.root + "\n", nil
			}
		case "rsvg-convert", "djxl", "avifdec":
			var dest string
			if filepath.Base(exe) == "rsvg-convert" {
				if len(args) != 4 || args[0] != "--format=png" || args[1] != "--output" {
					t.Fatalf("SVG argv: %q", args)
				}
				dest = args[2]
			} else {
				if len(args) != 2 || filepath.Ext(args[1]) != ".png" {
					t.Fatalf("raster converter argv: %q", args)
				}
				dest = args[1]
			}
			if f.badOutput != "" {
				return "", os.WriteFile(dest, []byte(f.badOutput), 0644)
			}
			writePNG(t, dest)
			return "", nil
		case "flatpak":
			switch args[0] {
			case "list":
				if !slices.Equal(args, []string{"list", "--system", "--app", "--columns=application"}) {
					t.Fatalf("inventory argv: %q", args)
				}
				if f.installed {
					return AppID + "\n", nil
				}
				return "", nil
			case "remotes":
				return "flatpark\tsystem\n", nil
			case "override":
				if len(args) != 4 || args[1] != "--user" || !strings.HasSuffix(args[2], ":ro") || args[3] != AppID {
					t.Fatalf("override argv: %q", args)
				}
				f.access = true
				return "", nil
			case "run":
				if slices.Contains(args, "--command=test") {
					if !f.access {
						return "", errors.New("sandbox cannot read source")
					}
					return "", nil
				}
				if !slices.Equal(args[:4], []string{"run", "--system", "--env=GSETTINGS_SCHEMA_DIR=/app/extra/valhalla", "--command=gsettings"}) {
					t.Fatalf("settings argv: %q", args)
				}
				verb, key := args[5], args[7]
				if verb == "get" {
					if key == ManualFoldersKey {
						return f.folders, nil
					}
					return f.active, nil
				}
				if verb == "set" {
					if key == ManualFoldersKey {
						f.folders = args[8]
					} else {
						f.active = args[8]
					}
					return "", nil
				}
			}
		}
		t.Fatalf("unexpected command %s", line)
		return "", nil
	}
	return f
}

func (f *fixture) failure(stage string) error {
	if f.fail == stage {
		return errors.New(stage + " failed")
	}
	return nil
}

func (f *fixture) artwork(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(f.root, "2026-05-09", "images", name)
	if strings.HasSuffix(name, ".png") {
		writePNG(t, path)
	} else if err := os.WriteFile(path, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.White)
	err = png.Encode(file, img)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("writing PNG: %v / %v", err, closeErr)
	}
}

func TestSlideshowInstallConvertsAndPreservesPreferences(t *testing.T) {
	f := setup(t)
	f.artwork(t, "day.svg")
	f.artwork(t, "night.jxl")
	f.artwork(t, "already.png")
	if err := Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.data, "chairlift", "wallpaper-slideshow", "png")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 3 {
		t.Fatalf("PNG sources = %v, %v", entries, err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".png" {
			t.Errorf("non-PNG output %s", entry.Name())
		}
	}
	folders, err := parseStrv(f.folders)
	want := []string{"/existing, folder", "/quote's", `/back\slash`, dir}
	if err != nil || !reflect.DeepEqual(folders, want) {
		t.Fatalf("folders = %q, %v; want %q", folders, err, want)
	}
	folderWrite, sourceWrite := -1, -1
	for i, call := range f.calls {
		if strings.Contains(call, "set "+Schema+" "+ManualFoldersKey) {
			folderWrite = i
		}
		if strings.Contains(call, "set "+Schema+" "+ActiveSourceKey) {
			sourceWrite = i
		}
		if strings.Contains(strings.ToLower(call), "damask") || strings.Contains(call, "remote-add") {
			t.Errorf("forbidden payload/remote: %s", call)
		}
	}
	if folderWrite < 0 || sourceWrite <= folderWrite {
		t.Fatalf("configuration order = %q", f.calls)
	}
	if ok, err := Installed(context.Background()); !ok || err != nil {
		t.Fatalf("Installed = %v, %v", ok, err)
	}
	if err := Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	folders, err = parseStrv(f.folders)
	if err != nil || !reflect.DeepEqual(folders, want) {
		t.Fatalf("repeat folders = %q, %v", folders, err)
	}
}

func TestSlideshowFailedConversionKeepsPreviousPNGs(t *testing.T) {
	for _, conversion := range []struct{ file, tool string }{
		{"night.jxl", "djxl "},
		{"night.avif", "avifdec "},
	} {
		t.Run(conversion.file, func(t *testing.T) {
			f := setup(t)
			f.artwork(t, "day.svg")
			if err := Install(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(f.data, "chairlift", "wallpaper-slideshow", "png")
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			old, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
			if err != nil {
				t.Fatal(err)
			}
			f.artwork(t, conversion.file)
			f.fail = conversion.tool
			if err := Install(context.Background(), nil); err == nil {
				t.Fatal("conversion failure accepted")
			}
			got, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
			if err != nil || !slices.Equal(old, got) {
				t.Fatalf("previous PNG changed: %v", err)
			}
			if ok, _ := Installed(context.Background()); ok {
				t.Fatal("partial output reports installed")
			}
		})
	}
}

func TestSlideshowFailuresRemainRetryable(t *testing.T) {
	for _, stage := range []string{"tap", "trust", "install-package", "install-flatpak", "rsvg-convert ", "override ", "get " + Schema + " " + ManualFoldersKey, "set " + Schema + " " + ManualFoldersKey, "set " + Schema + " " + ActiveSourceKey} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t)
			f.artwork(t, "day.svg")
			f.fail = stage
			if err := Install(context.Background(), nil); err == nil {
				t.Fatal("failure accepted")
			}
			if ok, _ := Installed(context.Background()); ok {
				t.Fatal("failed setup reports installed")
			}
			f.fail = ""
			if err := Install(context.Background(), nil); err != nil {
				t.Fatalf("retry: %v", err)
			}
		})
	}
}

func TestSlideshowRejectsInvalidOutputAndMalformedSettings(t *testing.T) {
	for _, kind := range []string{"invalid PNG", "malformed folders"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			f.artwork(t, "day.svg")
			if kind == "invalid PNG" {
				f.badOutput = "not a PNG"
			} else {
				f.folders = "['/keep', unquoted]"
			}
			original := f.folders
			if err := Install(context.Background(), nil); err == nil {
				t.Fatal("invalid setup accepted")
			}
			if f.active != "'wallhaven'" || f.folders != original {
				t.Fatal("invalid setup overwrote preferences")
			}
			if ok, _ := Installed(context.Background()); ok {
				t.Fatal("invalid setup reports installed")
			}
		})
	}
}

func TestSlideshowFilesystemFailureAndMissingArtwork(t *testing.T) {
	for _, kind := range []string{"missing artwork", "blocked destination", "missing source file"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			if kind != "missing artwork" {
				f.artwork(t, "day.png")
			}
			if kind == "blocked destination" {
				if err := os.MkdirAll(f.data, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(f.data, "chairlift"), []byte("blocked"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "missing source file" {
				if err := Install(context.Background(), nil); err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(filepath.Join(f.data, "chairlift", "wallpaper-slideshow", "png"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(f.data, "chairlift", "wallpaper-slideshow", "png", entries[0].Name())); err != nil {
					t.Fatal(err)
				}
			} else if err := Install(context.Background(), nil); err == nil {
				t.Fatal("filesystem failure accepted")
			}
			if ok, _ := Installed(context.Background()); ok {
				t.Fatal("missing output reports installed")
			}
		})
	}
}

func TestSlideshowDryRunDoesNothing(t *testing.T) {
	f := setup(t)
	dryrun.Set(true)
	if err := Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("dry-run invoked commands: %q", f.calls)
	}
	if _, err := os.Stat(f.data); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote output: %v", err)
	}
}

func TestSlideshowStatusDoesNotInstallConverters(t *testing.T) {
	f := setup(t)
	f.artwork(t, "day.svg")
	if err := Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	lookPath = func(string) (string, error) { return "", errors.New("missing") }
	if ok, err := Installed(context.Background()); !ok || err != nil {
		t.Fatalf("existing PNG source without converters = %v, %v", ok, err)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "install") {
			t.Errorf("status mutated: %s", call)
		}
	}
}

func TestGVariantFoldersPreservePathSyntax(t *testing.T) {
	for _, paths := range [][]string{nil, {"/a,b", `/quote's`, `/back\slash`, `/double"quote`, "/line\nbreak", "/tab\tpath", "/日本語"}} {
		got, err := parseStrv(formatStrv(paths))
		if err != nil || !slices.Equal(got, paths) {
			t.Fatalf("round trip %q = %q, %v", paths, got, err)
		}
	}
	got, err := parseStrv(`@as ["/a,b", '/it\'s', '/back\\slash', '\u65e5\u672c']`)
	if err != nil || !slices.Equal(got, []string{"/a,b", "/it's", `/back\slash`, "日本"}) {
		t.Fatalf("quoted array = %q, %v", got, err)
	}
	for _, raw := range []string{"", "garbage", "['/a'", "[/a]", "['/a' '/b']", "['/a',]junk", "['/a', 1]", `['bad\q']`, `['bad\u0000']`, "@ai []", "['unterminated]"} {
		if _, err := parseStrv(raw); err == nil {
			t.Errorf("malformed array accepted: %q", raw)
		}
	}
}

func TestSlideshowConvertsKDEAVIFPayload(t *testing.T) {
	f := setup(t)
	source := f.artwork(t, "day.avif")
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if ready, err := Installed(context.Background()); err != nil || !ready {
		t.Fatalf("AVIF setup ready = %v, %v", ready, err)
	}
	after, err := os.ReadFile(source)
	if err != nil || string(after) != string(before) {
		t.Fatalf("original AVIF changed: %q, %v", after, err)
	}
}

func TestSlideshowUntappedWallpaperSourceIsNotReady(t *testing.T) {
	f := setup(t)
	f.installed = true
	f.untapped = true
	if ready, err := Installed(context.Background()); ready || err != nil {
		t.Fatalf("untapped source ready = %v, %v; want false, nil", ready, err)
	}
	if _, err := os.Stat(f.data); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("readiness created or repaired wallpaper data: %v", err)
	}
}

func TestSlideshowFailedTapObservationIsNotAbsent(t *testing.T) {
	f := setup(t)
	f.installed = true
	f.fail = "brew tap"
	if ready, err := Installed(context.Background()); ready || err == nil {
		t.Fatalf("failed tap observation ready = %v, %v; want false with error", ready, err)
	}
}
