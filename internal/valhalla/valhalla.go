// Package valhalla prepares More Dinosaurs without launching Valhalla.
// Artwork comes from the installed extra-wallpaper cask, not its desktop
// integration symlinks.
package valhalla

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/projectbluefin/chairlift/internal/devsetup"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/outputtail"
	"github.com/projectbluefin/chairlift/internal/pkexec"
)

const (
	// AppID is Valhalla's Flatpak application ID.
	AppID = "io.github.tunix.valhalla"
	// Schema is Valhalla's GSettings schema identifier.
	Schema = AppID
	// ActiveSourceKey selects the wallpaper source.
	ActiveSourceKey = "active-source"
	// ManualFoldersKey stores the user's wallpaper folder paths.
	ManualFoldersKey = "manual-folders"
	// SchemaDir is the schema directory in the FlatPark application.
	SchemaDir = "/app/extra/valhalla"
	// ExtraWallpapersCask is the collection's fixed Homebrew wallpaper payload.
	ExtraWallpapersCask = wallpaperTap + "/bluefin-wallpapers-extra"
	wallpaperTap        = "ublue-os/tap"

	readTimeout       = 30 * time.Second
	conversionTimeout = 2 * time.Minute
)

const (
	AppStage        devsetup.Stage = "Install Valhalla"
	WallpapersStage devsetup.Stage = "Install extra wallpapers"
	ImagesStage     devsetup.Stage = "Prepare wallpaper images"
	ConfigureStage  devsetup.Stage = "Configure slideshow"
	VerifyStage     devsetup.Stage = "Verify setup"
)

// Stages is the ordered, bounded activity inventory for slideshow setup.
var Stages = [...]devsetup.Stage{AppStage, WallpapersStage, ImagesStage, ConfigureStage, VerifyStage}

var errPayloadMissing = errors.New("extra wallpaper artwork is not installed")

// Only the external boundaries are replaced by tests. Package installation uses
// the existing dry-run, bounded process-group and per-package trust wrappers.
var (
	runCommand     = execCommand
	brewExecutable = homebrew.ExecutablePath
	lookPath       = exec.LookPath
	tap            = homebrew.Tap
	trustCask      = homebrew.TrustCask
	installPackage = homebrew.Install
	installFlatpak = func() error { return flatpak.InstallFromRemote(AppID, "flatpark", false) }
)

// Install installs the collection's fixed payload, converts the installed cask's
// SVG/JXL/AVIF artwork to PNG, grants read-only access, and selects the manual source.
// report receives bounded phase transitions on the calling worker and may be nil.
// A preview reports Preview for each stage without commands or writes. Call this
// from a worker, not GTK.
func Install(ctx context.Context, report func(devsetup.Stage, devsetup.Status)) (err error) {
	emit := func(stage devsetup.Stage, status devsetup.Status) {
		if report != nil {
			report(stage, status)
		}
	}
	if dryrun.Enabled() {
		log.Print("[DRY-RUN] Would install More Dinosaurs (Valhalla and extra wallpapers), convert artwork to PNG, and configure its manual source")
		for _, stage := range Stages {
			emit(stage, devsetup.Preview)
		}
		return nil
	}
	stage := AppStage
	emit(stage, devsetup.Working)
	defer func() {
		if err != nil {
			status := devsetup.Failed
			if errors.Is(err, context.Canceled) || pkexec.IsAuthDismissed(err) {
				status = devsetup.Cancelled
			}
			emit(stage, status)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	brew := brewExecutable()
	if brew == "" {
		return errors.New("wallpaper slideshow requires Homebrew")
	}
	present, err := appInstalled(ctx)
	if err != nil {
		return err
	}
	if !present {
		out, err := command(ctx, readTimeout, "flatpak", "remotes", "--system", "--columns=name,options")
		if err != nil {
			return err
		}
		found := false
		for _, line := range strings.Split(out, "\n") {
			name, options, _ := strings.Cut(strings.TrimSpace(line), "\t")
			if name == "flatpark" && !slices.Contains(strings.Split(options, ","), "disabled") {
				found = true
			}
		}
		if !found {
			return errors.New("wallpaper slideshow requires its system application source; no source was added")
		}
		if err := installFlatpak(); err != nil {
			return fmt.Errorf("installing Valhalla: %w", err)
		}
	}
	emit(stage, devsetup.Complete)
	stage = WallpapersStage
	emit(stage, devsetup.Working)
	for _, step := range []struct {
		name string
		run  func() error
	}{
		{"opening the wallpaper tap", func() error { return tap(wallpaperTap) }},
		{"trusting the extra wallpaper cask", func() error { return trustCask(ExtraWallpapersCask) }},
		{"installing extra wallpapers", func() error { return installPackage(ExtraWallpapersCask, true) }},
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := step.run(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	emit(stage, devsetup.Complete)
	stage = ImagesStage
	emit(stage, devsetup.Working)
	art, err := installedArtwork(ctx, brew)
	if err != nil {
		return err
	}
	dir, err := pngDirectory()
	if err != nil {
		return err
	}
	if err := preparePNGs(ctx, brew, dir, art); err != nil {
		return err
	}
	emit(stage, devsetup.Complete)
	stage = ConfigureStage
	emit(stage, devsetup.Working)
	if err := configure(ctx, dir); err != nil {
		return err
	}
	emit(stage, devsetup.Complete)
	stage = VerifyStage
	emit(stage, devsetup.Working)
	ok, err := Installed(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("wallpaper slideshow setup could not be verified; install again to retry")
	}
	emit(stage, devsetup.Complete)
	return nil
}

// Installed requires observable, usable PNGs for every installed source artwork,
// the manual folder/source settings, and sandbox readability. Flatpak presence
// alone is not successful setup. Reads never install converters or repair state.
func Installed(ctx context.Context) (bool, error) {
	present, err := appInstalled(ctx)
	if err != nil || !present {
		return false, err
	}
	brew := brewExecutable()
	if brew == "" {
		return false, nil
	}
	taps, err := command(ctx, readTimeout, brew, "tap")
	if err != nil {
		return false, err
	}
	if !slices.Contains(strings.Fields(taps), wallpaperTap) {
		return false, nil
	}
	art, err := installedArtwork(ctx, brew)
	if errors.Is(err, errPayloadMissing) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	dir, err := pngDirectory()
	if err != nil {
		return false, err
	}
	for _, item := range art {
		ok, err := validPNG(filepath.Join(dir, item.output))
		if err != nil || !ok {
			return false, err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && obsoletePNG(entry.Name(), art) {
			return false, nil
		}
	}
	folders, err := readFolders(ctx)
	if err != nil {
		return false, err
	}
	if !slices.Contains(folders, dir) {
		return false, nil
	}
	raw, err := settings(ctx, "get", ActiveSourceKey)
	if err != nil {
		return false, err
	}
	source, rest, err := parseString(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(rest) != "" {
		return false, fmt.Errorf("reading Valhalla active source: invalid GVariant string %q", raw)
	}
	if source != "manual" {
		return false, nil
	}
	_, err = command(ctx, readTimeout, "flatpak", "run", "--system", "--command=test", AppID, "-r", filepath.Join(dir, art[0].output))
	if err != nil {
		return false, fmt.Errorf("checking Valhalla PNG source access: %w", err)
	}
	return true, nil
}

func appInstalled(ctx context.Context) (bool, error) {
	out, err := command(ctx, readTimeout, "flatpak", "list", "--system", "--app", "--columns=application")
	if err != nil {
		return false, err
	}
	return slices.Contains(strings.Fields(out), AppID), nil
}

func pngDirectory() (string, error) {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		data = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(data) {
		return "", errors.New("wallpaper data directory must be absolute")
	}
	return filepath.Join(data, "chairlift", "wallpaper-slideshow", "png"), nil
}

type artwork struct{ source, output string }

func installedArtwork(ctx context.Context, brew string) ([]artwork, error) {
	out, err := command(ctx, readTimeout, brew, "info", "--json=v2", "--cask", ExtraWallpapersCask)
	if err != nil {
		return nil, err
	}
	var info struct {
		Casks []struct{ Token, Installed string }
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return nil, fmt.Errorf("reading installed wallpaper cask: %w", err)
	}
	version := ""
	for _, cask := range info.Casks {
		if cask.Token == "bluefin-wallpapers-extra" {
			version = cask.Installed
		}
	}
	if version == "" {
		return nil, errPayloadMissing
	}
	if filepath.Base(version) != version || version == "." || version == ".." {
		return nil, errors.New("invalid installed wallpaper cask version")
	}
	out, err = command(ctx, readTimeout, brew, "--caskroom", ExtraWallpapersCask)
	if err != nil {
		return nil, err
	}
	root := strings.TrimSpace(out)
	if !filepath.IsAbs(root) || strings.ContainsAny(root, "\r\n") {
		return nil, errors.New("invalid wallpaper Caskroom path")
	}
	root = filepath.Join(root, version)
	var result []artwork
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		// Do not follow .user-home or any artwork symlink out of the cask.
		if !entry.Type().IsRegular() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".svg", ".jxl", ".avif", ".png":
		default:
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(filepath.Join(version, rel)))
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		result = append(result, artwork{path, fmt.Sprintf("%x-%s.png", sum[:8], name)})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, errPayloadMissing
	}
	if err != nil {
		return nil, fmt.Errorf("finding installed extra wallpaper artwork: %w", err)
	}
	if len(result) == 0 {
		return nil, errPayloadMissing
	}
	return result, nil
}

func converter(ctx context.Context, brew, name, formula string) (string, error) {
	resolve := func() string {
		if path, err := lookPath(name); err == nil {
			return path
		}
		// A direct GUI launch may lack Homebrew on PATH. WithBrewPath is also
		// used for every child below, but LookPath itself reads the parent PATH.
		candidate := filepath.Join(filepath.Dir(brew), name)
		if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
			return candidate
		}
		return ""
	}
	if path := resolve(); path != "" {
		return path, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := installPackage(formula, false); err != nil {
		return "", fmt.Errorf("installing %s for wallpaper conversion: %w", formula, err)
	}
	if path := resolve(); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("%s was installed but %s is not executable", formula, name)
}

func preparePNGs(ctx context.Context, brew, dir string, art []artwork) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0755); err != nil {
		return err
	}
	if st, err := os.Lstat(dir); err == nil && !st.IsDir() {
		return errors.New("wallpaper PNG destination is not a directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(dir), ".png-stage-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(stage); err != nil {
			log.Printf("Could not remove wallpaper conversion staging directory: %v", err)
		}
	}()
	converters := map[string]string{}
	for _, item := range art {
		if err := ctx.Err(); err != nil {
			return err
		}
		dest := filepath.Join(stage, item.output)
		ext := strings.ToLower(filepath.Ext(item.source))
		if ext == ".png" {
			if err := copyPNG(item.source, dest); err != nil {
				return err
			}
		} else {
			name, formula := "rsvg-convert", "librsvg"
			switch ext {
			case ".jxl":
				name, formula = "djxl", "jpeg-xl"
			case ".avif":
				name, formula = "avifdec", "libavif"
			}
			tool := converters[name]
			if tool == "" {
				tool, err = converter(ctx, brew, name, formula)
				if err != nil {
					return err
				}
				converters[name] = tool
			}
			var args []string
			if ext == ".svg" {
				args = []string{"--format=png", "--output", dest, item.source}
			} else {
				args = []string{item.source, dest}
			}
			if _, err := command(ctx, conversionTimeout, tool, args...); err != nil {
				return fmt.Errorf("converting %s: %w", filepath.Base(item.source), err)
			}
		}
		if ok, err := validPNG(dest); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("converter produced no usable PNG for %s", filepath.Base(item.source))
		}
		if err := os.Chmod(dest, 0644); err != nil {
			return err
		}
	}
	// Convert and validate the entire batch before touching prior output. Each
	// rename replaces one complete PNG atomically; failed converters never
	// leave a half-written image in the source Valhalla reads.
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, item := range art {
		if err := os.Rename(filepath.Join(stage, item.output), filepath.Join(dir, item.output)); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !obsoletePNG(name, art) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("removing obsolete generated wallpaper: %w", err)
		}
	}
	return nil
}

func obsoletePNG(name string, art []artwork) bool {
	prefix, _, _ := strings.Cut(name, "-")
	return len(prefix) == 16 && strings.Trim(prefix, "0123456789abcdef") == "" &&
		strings.HasSuffix(name, ".png") &&
		!slices.ContainsFunc(art, func(item artwork) bool { return item.output == name })
}

func copyPNG(source, dest string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Close())
}

func validPNG(path string) (bool, error) {
	st, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !st.Mode().IsRegular() {
		return false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	config, err := png.DecodeConfig(file)
	if err != nil {
		return false, nil
	}
	// DecodeConfig deliberately avoids allocating a full 8K raster during every
	// inventory refresh. Require the complete PNG end marker as well, so an
	// interrupted conversion with only a valid header is not usable output.
	var end [12]byte
	if st.Size() < int64(len(end)) {
		return false, nil
	}
	if _, err := file.ReadAt(end[:], st.Size()-int64(len(end))); err != nil {
		return false, err
	}
	if end != [12]byte{0, 0, 0, 0, 'I', 'E', 'N', 'D', 174, 66, 96, 130} {
		return false, nil
	}
	return config.Width > 0 && config.Height > 0 && int64(config.Width)*int64(config.Height) <= 64*1024*1024, nil
}

func settings(ctx context.Context, verb, key string, values ...string) (string, error) {
	args := []string{"run", "--system", "--env=GSETTINGS_SCHEMA_DIR=" + SchemaDir, "--command=gsettings", AppID, verb, Schema, key}
	return command(ctx, readTimeout, "flatpak", append(args, values...)...)
}

func readFolders(ctx context.Context) ([]string, error) {
	out, err := settings(ctx, "get", ManualFoldersKey)
	if err != nil {
		return nil, err
	}
	folders, err := parseStrv(out)
	if err != nil {
		return nil, fmt.Errorf("reading Valhalla manual folders: %w", err)
	}
	return folders, nil
}

func configure(ctx context.Context, dir string) error {
	folders, err := readFolders(ctx)
	if err != nil {
		return err
	}
	if _, err := command(ctx, readTimeout, "flatpak", "override", "--user", "--filesystem="+dir+":ro", AppID); err != nil {
		return err
	}
	if !slices.Contains(folders, dir) {
		folders = append(folders, dir)
		if _, err := settings(ctx, "set", ManualFoldersKey, formatStrv(folders)); err != nil {
			return err
		}
	}
	observed, err := readFolders(ctx)
	if err != nil {
		return err
	}
	if !slices.Equal(observed, folders) {
		return errors.New("manual folders did not retain the requested value")
	}
	// Do not activate an empty or unverified source on a partial failure.
	if _, err := settings(ctx, "set", ActiveSourceKey, "'manual'"); err != nil {
		return err
	}
	return nil
}

func command(ctx context.Context, timeout time.Duration, exe string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	out, err := runCommand(ctx, exe, args...)
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", filepath.Base(exe), err, strings.TrimSpace(out))
	}
	return out, nil
}

type countedOutput struct {
	*outputtail.Writer
	size int
}

func (w *countedOutput) Write(p []byte) (int, error) {
	w.size += len(p)
	return w.Writer.Write(p)
}

func execCommand(ctx context.Context, exe string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = homebrew.WithBrewPath(os.Environ(), brewExecutable())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	const outputLimit = 64 * 1024
	stdout := &countedOutput{Writer: outputtail.New(outputLimit)}
	stderr := outputtail.New(outputLimit)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return stdout.String() + "\n" + stderr.String(), err
	}
	// A truncated GVariant/JSON response must never be treated as a complete
	// observation; in particular, it must not replace the user's folder list.
	if stdout.size > outputLimit {
		return "", errors.New("command response exceeded 64 KiB")
	}
	return stdout.String(), nil
}
