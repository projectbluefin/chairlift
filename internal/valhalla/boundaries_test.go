package valhalla

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/devsetup"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestSlideshowMissingRemoteDoesNotInstall(t *testing.T) {
	for _, remotes := range []string{"flathub\tsystem\n", "flatpark\tdisabled,no-enumerate\n"} {
		t.Run(remotes, func(t *testing.T) {
			f := setup(t)
			baseRun := runCommand
			runCommand = func(ctx context.Context, exe string, args ...string) (string, error) {
				if args[0] == "remotes" {
					return remotes, nil
				}
				return baseRun(ctx, exe, args...)
			}
			if err := Install(context.Background(), nil); err == nil {
				t.Fatalf("missing remote: %v", err)
			}
			for _, call := range f.calls {
				if strings.HasPrefix(call, "install") || strings.HasPrefix(call, "tap") {
					t.Fatalf("unexpected install without remote: %s", call)
				}
			}
		})
	}
}

func TestSlideshowInstallsOnlyRequiredMissingConverters(t *testing.T) {
	f := setup(t)
	f.artwork(t, "day.svg")
	f.artwork(t, "night.jxl")
	f.artwork(t, "plasma.avif")
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	brewExecutable = func() string { return filepath.Join(bin, "brew") }
	lookPath = func(string) (string, error) { return "", errors.New("not on PATH") }
	baseInstall := installPackage
	var installed []string
	installPackage = func(name string, cask bool) error {
		if cask {
			return baseInstall(name, cask)
		}
		installed = append(installed, name)
		tool := "rsvg-convert"
		switch name {
		case "jpeg-xl":
			tool = "djxl"
		case "libavif":
			tool = "avifdec"
		}
		return os.WriteFile(filepath.Join(bin, tool), []byte("fixture executable"), 0755)
	}
	if err := Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(installed, []string{"librsvg", "jpeg-xl", "libavif"}) {
		t.Fatalf("converter dependencies = %q", installed)
	}
	if err := Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(installed) != 3 {
		t.Fatalf("reinstalled resolved converters: %q", installed)
	}
}

func TestSlideshowCannotResolveInstalledConverterFails(t *testing.T) {
	f := setup(t)
	f.artwork(t, "day.svg")
	lookPath = func(string) (string, error) { return "", errors.New("missing") }
	if err := Install(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("unusable converter = %v", err)
	}
	if f.active != "'wallhaven'" {
		t.Fatal("activated without PNGs")
	}
}

func TestSlideshowReadbackRefusesFalseSuccess(t *testing.T) {
	for _, key := range []string{ManualFoldersKey, ActiveSourceKey} {
		t.Run(key, func(t *testing.T) {
			f := setup(t)
			f.artwork(t, "day.svg")
			baseRun := runCommand
			runCommand = func(ctx context.Context, exe string, args ...string) (string, error) {
				if filepath.Base(exe) == "flatpak" && len(args) == 9 && args[5] == "set" && args[7] == key {
					return "", nil
				}
				return baseRun(ctx, exe, args...)
			}
			if err := Install(context.Background(), nil); err == nil {
				t.Fatal("ignored settings write counted as successful setup")
			}
			if ok, _ := Installed(context.Background()); ok {
				t.Fatal("ignored settings write reports installed")
			}
			if key == ManualFoldersKey && f.active != "'wallhaven'" {
				t.Fatal("activated source before folders were verified")
			}
		})
	}
}

func TestSlideshowStatusObservesSourceAndAccess(t *testing.T) {
	for _, kind := range []string{"flatpak only", "source changed", "sandbox access lost", "PNG truncated"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			f.artwork(t, "day.png")
			if kind == "flatpak only" {
				f.installed = true
			} else {
				if err := Install(context.Background(), nil); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "source changed":
				f.active = "'wallhaven'"
			case "sandbox access lost":
				f.access = false
			case "PNG truncated":
				dir := filepath.Join(f.data, "chairlift", "wallpaper-slideshow", "png")
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, entries[0].Name())
				st, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Truncate(path, st.Size()-12); err != nil {
					t.Fatal(err)
				}
			}
			if ok, _ := Installed(context.Background()); ok {
				t.Fatal("incomplete observed state reports installed")
			}
		})
	}
}

func TestSlideshowRejectsCaskMetadataErrors(t *testing.T) {
	for _, metadata := range []string{`{"casks":[{"token":"bluefin-wallpapers-extra","installed":"../outside"}]}`, `{"casks":`, `{"casks":[]}`} {
		t.Run(metadata, func(t *testing.T) {
			f := setup(t)
			f.artwork(t, "day.png")
			baseRun := runCommand
			runCommand = func(ctx context.Context, exe string, args ...string) (string, error) {
				if filepath.Base(exe) == "brew" && args[0] == "info" {
					return metadata, nil
				}
				return baseRun(ctx, exe, args...)
			}
			if err := Install(context.Background(), nil); err == nil {
				t.Fatal("invalid installed cask metadata accepted")
			}
			if ok, _ := Installed(context.Background()); ok {
				t.Fatal("invalid installed cask metadata reports installed")
			}
		})
	}
}

func TestSlideshowUsesInstalledVersionAndIgnoresDesktopLinks(t *testing.T) {
	f := setup(t)
	f.artwork(t, "day.png")
	// The fixture's available version is 9999, but the installed one is 2026-05-09.
	// A broken user-home integration link must never become the artwork source.
	if err := os.Symlink("/missing/desktop-background", filepath.Join(f.root, "2026-05-09", ".user-home")); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if ok, err := Installed(context.Background()); !ok || err != nil {
		t.Fatalf("installed cask artwork = %v, %v", ok, err)
	}
}

func TestSlideshowFallbackAndQuotedTargetPath(t *testing.T) {
	for _, kind := range []string{"HOME fallback", "quoted XDG path"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			f.artwork(t, "day.png")
			var want string
			if kind == "HOME fallback" {
				t.Setenv("XDG_DATA_HOME", "")
				want = filepath.Join(os.Getenv("HOME"), ".local", "share", "chairlift", "wallpaper-slideshow", "png")
			} else {
				f.data = filepath.Join(t.TempDir(), "comma, quote's and back\\slash")
				t.Setenv("XDG_DATA_HOME", f.data)
				want = filepath.Join(f.data, "chairlift", "wallpaper-slideshow", "png")
			}
			if err := Install(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			folders, err := parseStrv(f.folders)
			if err != nil || !slices.Contains(folders, want) {
				t.Fatalf("target folders = %q, %v", folders, err)
			}
			if _, err := os.Stat(want); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSlideshowCanceledInstallDoesNothing(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Install(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled install = %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("canceled install ran commands: %q", f.calls)
	}
}

func TestSlideshowMissingConverterInstallFailure(t *testing.T) {
	f := setup(t)
	f.artwork(t, "day.svg")
	lookPath = func(string) (string, error) { return "", errors.New("missing") }
	baseInstall := installPackage
	installPackage = func(name string, cask bool) error {
		if cask {
			return baseInstall(name, cask)
		}
		return errors.New("converter download failed")
	}
	if err := Install(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "converter download failed") {
		t.Fatalf("dependency failure = %v", err)
	}
	if f.active != "'wallhaven'" {
		t.Fatal("activated without converted output")
	}
	if ok, _ := Installed(context.Background()); ok {
		t.Fatal("missing converter/output reports installed")
	}
}

func TestSlideshowFlattensDuplicateArtworkNames(t *testing.T) {
	f := setup(t)
	for _, variant := range []string{"first", "second"} {
		if err := os.MkdirAll(filepath.Join(f.root, "2026-05-09", "images", variant), 0755); err != nil {
			t.Fatal(err)
		}
		f.artwork(t, filepath.Join(variant, "wallpaper.svg"))
	}
	if err := Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(f.data, "chairlift", "wallpaper-slideshow", "png"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("flattened PNG outputs = %v, %v", entries, err)
	}
	if ok, err := Installed(context.Background()); !ok || err != nil {
		t.Fatalf("flattened source = %v, %v", ok, err)
	}
}

func TestSlideshowPackUpgradeRequiresFreshPNGs(t *testing.T) {
	f := setup(t)
	f.artwork(t, "day.png")
	f.artwork(t, "retired.png")
	ctx := context.Background()
	if err := Install(ctx, nil); err != nil {
		t.Fatal(err)
	}
	oldArtwork, err := installedArtwork(ctx, brewExecutable())
	if err != nil {
		t.Fatal(err)
	}
	dir, err := pngDirectory()
	if err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(dir, "personal.png")
	writePNG(t, personal)
	newRoot := filepath.Join(f.root, "2026-06-01", "images")
	if err := os.MkdirAll(newRoot, 0755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(newRoot, "day.png"))
	previous := runCommand
	runCommand = func(ctx context.Context, exe string, args ...string) (string, error) {
		out, err := previous(ctx, exe, args...)
		if filepath.Base(exe) == "brew" && args[0] == "info" {
			out = strings.ReplaceAll(out, `"installed":"2026-05-09"`, `"installed":"2026-06-01"`)
		}
		return out, err
	}
	if ok, err := Installed(ctx); ok || err != nil {
		t.Fatalf("new pack with old PNGs reports ready = %v, %v", ok, err)
	}
	if err := Install(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for _, item := range oldArtwork {
		if _, err := os.Stat(filepath.Join(dir, item.output)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("obsolete generated image %s remains: %v", item.output, err)
		}
	}
	if _, err := os.Stat(personal); err != nil {
		t.Fatalf("unmanaged image was removed: %v", err)
	}
	if ok, err := Installed(ctx); !ok || err != nil {
		t.Fatalf("reconverted pack not ready = %v, %v", ok, err)
	}
}

type stageEvent struct {
	stage  devsetup.Stage
	status devsetup.Status
}

func TestSlideshowReportsRealPhaseBoundaries(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprint(present), func(t *testing.T) {
			f := setup(t)
			f.installed = present
			f.artwork(t, "day.svg")
			var events []stageEvent
			assertWorking := func(stage devsetup.Stage) {
				t.Helper()
				if len(events) == 0 || events[len(events)-1] != (stageEvent{stage, devsetup.Working}) {
					t.Fatalf("dependency invoked before %s Working: %v", stage, events)
				}
			}
			baseRun, baseTap, baseTrust, baseInstall, baseFlatpak := runCommand, tap, trustCask, installPackage, installFlatpak
			runCommand = func(ctx context.Context, exe string, args ...string) (string, error) {
				switch filepath.Base(exe) {
				case "rsvg-convert":
					assertWorking(ImagesStage)
				case "flatpak":
					if args[0] == "remotes" {
						assertWorking(AppStage)
					} else if args[0] == "override" || (args[0] == "run" && slices.Contains(args, "set")) {
						assertWorking(ConfigureStage)
					}
				}
				return baseRun(ctx, exe, args...)
			}
			tap = func(name string) error { assertWorking(WallpapersStage); return baseTap(name) }
			trustCask = func(name string) error { assertWorking(WallpapersStage); return baseTrust(name) }
			installPackage = func(name string, cask bool) error { assertWorking(WallpapersStage); return baseInstall(name, cask) }
			installFlatpak = func() error { assertWorking(AppStage); return baseFlatpak() }
			if err := Install(context.Background(), func(stage devsetup.Stage, status devsetup.Status) {
				events = append(events, stageEvent{stage, status})
			}); err != nil {
				t.Fatal(err)
			}
			var want []stageEvent
			for _, stage := range Stages {
				want = append(want, stageEvent{stage, devsetup.Working}, stageEvent{stage, devsetup.Complete})
			}
			if !slices.Equal(events, want) {
				t.Fatalf("events = %v, want %v", events, want)
			}
		})
	}
}

func TestSlideshowReportsFailureAtOwningPhase(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stage   devsetup.Stage
		failure string
	}{
		{"app", AppStage, "install-flatpak"},
		{"wallpapers", WallpapersStage, "trust"},
		{"images", ImagesStage, "rsvg-convert"},
		{"configure", ConfigureStage, "override"},
		{"verify", VerifyStage, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.artwork(t, "day.svg")
			f.fail = tc.failure
			var events []stageEvent
			err := Install(context.Background(), func(stage devsetup.Stage, status devsetup.Status) {
				events = append(events, stageEvent{stage, status})
				if tc.stage == VerifyStage && stage == VerifyStage && status == devsetup.Working {
					f.access = false
				}
			})
			if err == nil {
				t.Fatal("phase failure reported success")
			}
			var want []stageEvent
			for _, stage := range Stages {
				want = append(want, stageEvent{stage, devsetup.Working})
				if stage == tc.stage {
					want = append(want, stageEvent{stage, devsetup.Failed})
					break
				}
				want = append(want, stageEvent{stage, devsetup.Complete})
			}
			if !slices.Equal(events, want) {
				t.Fatalf("events = %v, want %v", events, want)
			}
		})
	}
}

func TestSlideshowCancellationAndPreviewAreNotCompletion(t *testing.T) {
	for _, kind := range []string{"context", "authentication", "preview"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "context":
				cancel()
			case "authentication":
				installFlatpak = func() error { return errors.New("Request dismissed") }
			default:
				dryrun.Set(true)
			}
			var events []stageEvent
			err := Install(ctx, func(stage devsetup.Stage, status devsetup.Status) { events = append(events, stageEvent{stage, status}) })
			want := []stageEvent{{AppStage, devsetup.Working}, {AppStage, devsetup.Cancelled}}
			if kind == "preview" {
				want = nil
				for _, stage := range Stages {
					want = append(want, stageEvent{stage, devsetup.Preview})
				}
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("cancellation returned success")
			}
			if !slices.Equal(events, want) {
				t.Fatalf("events = %v, want %v", events, want)
			}
			if kind != "authentication" && len(f.calls) != 0 {
				t.Fatalf("preview/canceled context invoked commands: %v", f.calls)
			}
			if _, err := os.Stat(filepath.Join(f.data, "chairlift")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preview/cancellation wrote setup files: %v", err)
			}
		})
	}
}
