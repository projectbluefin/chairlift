package livery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// WallpaperRotateFlag is the command-line flag that runs one wallpaper
// rotation pass. It is the service unit's entire ExecStart, mirroring
// RotateFlag for the icon rotation.
const WallpaperRotateFlag = "--rotate-wallpaper"

// WallpaperServiceName is the systemd user unit that performs rotation. It
// is a oneshot enabled under graphical-session.target, mirroring the icon
// rotation unit (UnitName), so it runs one pass each time a graphical session
// starts and sessionToken resolves to that session rather than the boot.
// RotateWallpaper's per-session token makes a repeated run within one session
// a no-op, so the picture advances once per login session.
//
// WallpaperTimerName is a timer earlier builds of this feature installed. A
// timer fires on the user manager's clock, before graphical-session.target is
// active, so it is no longer written; RemoveWallpaperRotation still cleans a
// leftover one up.
const (
	WallpaperServiceName = "chairlift-livery-wallpaper-rotate.service"
	WallpaperTimerName   = "chairlift-livery-wallpaper-rotate.timer"
)

const wallpaperServiceTemplate = `[Unit]
Description=%s Wallpaper rotation
PartOf=graphical-session.target
After=graphical-session.target

[Service]
Type=oneshot
%sExecStart=%s %s

[Install]
WantedBy=graphical-session.target
`

// restorePrior restores one previously-existing unit file, or removes the path
// when there was nothing there to begin with. It is the inverse of the write
// an install or remove performed, so a failed step leaves the disk as the
// user had it.
func restorePrior(path string, existed bool, data []byte) error {
	if !existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeFileAtomically(path, data)
}

// wallpaperUnitPath returns the absolute path of the wallpaper service unit.
func wallpaperUnitPath() (string, error) {
	dir, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", WallpaperServiceName), nil
}

// wallpaperTimerPath returns the absolute path of the wallpaper timer unit.
func wallpaperTimerPath() (string, error) {
	dir, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", WallpaperTimerName), nil
}

// readPrior reads one unit file, reporting whether it existed.
func readPrior(path string) (data []byte, existed bool, err error) {
	data, err = os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// renderWallpaperUnit returns the path and bytes InstallWallpaperRotation
// would write for the service unit.
func renderWallpaperUnit() (servicePath, serviceUnit string, err error) {
	exe, err := unitExecutable()
	if err != nil {
		return "", "", fmt.Errorf("livery: locating the application executable: %w", err)
	}
	servicePath, err = wallpaperUnitPath()
	if err != nil {
		return "", "", err
	}
	var env string
	if dir := os.Getenv("GSETTINGS_SCHEMA_DIR"); dir != "" {
		quoted, err := systemdQuote("GSETTINGS_SCHEMA_DIR=" + dir)
		if err != nil {
			return "", "", fmt.Errorf("livery: carrying GSETTINGS_SCHEMA_DIR into the rotation unit: %w", err)
		}
		env = "Environment=" + quoted + "\n"
	}
	quotedExe, err := systemdQuote(exe)
	if err != nil {
		return "", "", fmt.Errorf("livery: writing the rotation unit's ExecStart: %w", err)
	}
	serviceUnit = fmt.Sprintf(wallpaperServiceTemplate, branding.AppName, env, quotedExe, WallpaperRotateFlag)
	return servicePath, serviceUnit, nil
}

// RotateWallpaper advances the selected image within the active channel and
// writes it to GNOME's background schema.
//
// It is a no-op when rotation is off, when no channel has been chosen, or when
// GNOME does not own the background schema on this host — in each case there
// is nothing to rotate, and failing would only make the unit exit non-zero at
// every run. It advances the catalog only: a channel with no resolvable image
// is skipped, and the session token is recorded regardless so a permanently
// unavailable image cannot re-advance on every retry. The token lives in its
// own key (KeyWallpaperRotationToken), so this pass and the icon pass in
// Rotate never mark each other as already done for the session.
func RotateWallpaper(ctx context.Context) error {
	state, err := Load(ctx)
	if err != nil {
		return err
	}
	if !state.WallpaperRotate {
		return nil
	}
	available, err := NativeBackgroundAvailable(ctx)
	if err != nil {
		return err
	}
	if !available {
		log.Printf("livery: skipping wallpaper rotation: GNOME background schema is not available")
		return nil
	}
	if state.WallpaperChannel == "" {
		return nil
	}

	token := sessionToken(ctx)
	if token != "" && token == state.WallpaperRotationToken {
		return nil
	}

	channel, ok := ChannelByID(state.WallpaperChannel)
	if !ok || len(channel.Images) == 0 {
		return nil
	}

	var failures []string
	nextID := NextImage(channel.ID, state.WallpaperImage)
	image, ok := ImageByID(channel, nextID)
	if !ok {
		return nil
	}
	file, ok := ResolveImage(image.Path)
	if ok {
		if err := ApplyNativeBackground(ctx, file); err != nil {
			failures = append(failures, err.Error())
		} else if err := SetString(ctx, KeyWallpaperImage, nextID); err != nil {
			failures = append(failures, err.Error())
		}
	} else {
		log.Printf("livery: skipping wallpaper rotation: %q is not available", image.Path)
	}

	// The token is recorded even on failure, so a pass that already ran for
	// this session does not advance again.
	if token != "" {
		if err := SetString(ctx, KeyWallpaperRotationToken, token); err != nil {
			failures = append(failures, err.Error())
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("livery: wallpaper rotation: %s", strings.Join(failures, "; "))
	}
	return nil
}

// InstallWallpaperRotation writes and enables the rotation service under
// graphical-session.target, mirroring InstallRotation. It is idempotent:
// rewriting an identical unit and re-enabling an enabled service are both
// no-ops as far as the user can observe. A failure restores whatever was on
// disk before.
func InstallWallpaperRotation(ctx context.Context) (result error) {
	servicePath, serviceUnit, err := renderWallpaperUnit()
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would write %s and enable it", servicePath)
		return nil
	}

	priorData, priorExisted, _ := readPrior(servicePath)
	defer func() {
		if result != nil {
			if err := restorePrior(servicePath, priorExisted, priorData); err != nil {
				result = errors.Join(result, err)
			}
			_, _ = runCommand(ctx, "systemctl", "--user", "daemon-reload")
		}
	}()

	if err := writeFileAtomically(servicePath, []byte(serviceUnit)); err != nil {
		return fmt.Errorf("livery: installing %s: %w", filepath.Base(servicePath), err)
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("livery: reloading user units: %w: %s", err, strings.TrimSpace(out))
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "enable", WallpaperServiceName); err != nil {
		return fmt.Errorf("livery: enabling wallpaper rotation: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// RemoveWallpaperRotation disables and deletes the rotation service and any
// timer an earlier build left behind. A failure
// restores whatever was on disk before.
func RemoveWallpaperRotation(ctx context.Context) (result error) {
	servicePath, err := wallpaperUnitPath()
	if err != nil {
		return err
	}
	timerPath, err := wallpaperTimerPath()
	if err != nil {
		return err
	}

	serviceData, serviceExisted, serviceErr := readPrior(servicePath)
	timerData, timerExisted, timerErr := readPrior(timerPath)
	if serviceErr != nil {
		return serviceErr
	}
	if timerErr != nil {
		return timerErr
	}
	if !serviceExisted && !timerExisted {
		return nil
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would disable and remove %s and %s", servicePath, timerPath)
		return nil
	}

	defer func() {
		if result != nil {
			if err := restorePrior(servicePath, serviceExisted, serviceData); err != nil {
				result = errors.Join(result, err)
			}
			if err := restorePrior(timerPath, timerExisted, timerData); err != nil {
				result = errors.Join(result, err)
			}
			_, _ = runCommand(ctx, "systemctl", "--user", "daemon-reload")
		}
	}()

	units := []struct {
		name    string
		existed bool
	}{
		{WallpaperTimerName, timerExisted},
		{WallpaperServiceName, serviceExisted},
	}
	for _, unit := range units {
		if !unit.existed {
			continue
		}
		if out, err := runCommand(ctx, "systemctl", "--user", "disable", unit.name); err != nil {
			return fmt.Errorf("livery: disabling %s: %w: %s", unit.name, err, strings.TrimSpace(out))
		}
	}
	for _, path := range []string{servicePath, timerPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("livery: removing %s: %w", filepath.Base(path), err)
		}
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("livery: reloading user units: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// SyncWallpaperRotation installs the rotation service when rotation is desired
// and removes it when it is not, so the unit's presence always matches the
// switch.
func SyncWallpaperRotation(ctx context.Context, s State) error {
	if s.WallpaperRotate {
		return InstallWallpaperRotation(ctx)
	}
	return RemoveWallpaperRotation(ctx)
}

// ConfigureWallpaperRotation commits the rotation preference only after the
// user manager accepted the schedule. A failed operation preserves the last
// persisted choice.
func ConfigureWallpaperRotation(ctx context.Context, desired State) (State, error) {
	previous, err := Load(ctx)
	if err != nil {
		return State{}, err
	}
	if err := SyncWallpaperRotation(ctx, desired); err != nil {
		return previous, err
	}
	if err := SetBool(ctx, KeyWallpaperRotate, desired.WallpaperRotate); err != nil {
		return previous, errors.Join(err, SyncWallpaperRotation(ctx, previous))
	}
	return desired, nil
}
