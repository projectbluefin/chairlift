package livery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// Wallpaper rotation is opt-in: nothing here runs until the user turns the
// Wallpapers group's rotation switch on for an installed channel. It is a
// systemd *user* timer plus a oneshot service, written to the user's
// ~/.config/systemd/user like the Livery icon rotation unit, so nothing is
// privileged. The service's ExecStart names the channel, which makes the two
// unit files the whole of rotation's state: whether it is on, and for which
// collection, is read back from them and from the user manager, never from a
// separate preference that could disagree.

// WallpaperRotateFlag is the command-line flag that runs one wallpaper
// rotation pass. The service's ExecStart is the executable, this flag, and
// the channel ID.
const WallpaperRotateFlag = "--rotate-wallpaper"

const (
	// WallpaperRotateService is the oneshot unit that performs one pass.
	WallpaperRotateService = "chairlift-wallpaper-rotate.service"
	// WallpaperRotateTimer schedules WallpaperRotateService.
	WallpaperRotateTimer = "chairlift-wallpaper-rotate.timer"
)

// wallpaperServiceTemplate runs one pass inside the graphical session's user
// manager, where the session bus and dconf are reachable.
const wallpaperServiceTemplate = `[Unit]
Description=%s wallpaper rotation
After=graphical-session.target

[Service]
Type=oneshot
ExecStart=%s %s %s
`

// wallpaperTimerTemplate changes the picture once a day. Persistent catches
// up on a day the computer was off, so a machine used only in the evening
// still rotates.
const wallpaperTimerTemplate = `[Unit]
Description=%s daily wallpaper rotation

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
`

// channelIDPattern is the closed set a channel ID must match before it is
// written into a unit file or accepted on the command line. A collection is a
// directory name under backgrounds/, and the casks name theirs in this set.
var channelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ErrWallpaperChannelInvalid means a channel ID is outside channelIDPattern.
var ErrWallpaperChannelInvalid = errors.New("livery: not a wallpaper channel name")

// ErrWallpaperChannelMissing means the rotating channel is no longer
// installed, so the pass has nothing to show.
var ErrWallpaperChannelMissing = errors.New("livery: the rotating wallpaper collection is not installed")

// wallpaperUnitPaths returns the absolute paths of the service and timer.
func wallpaperUnitPaths() (service, timer string, err error) {
	dir, err := configHome()
	if err != nil {
		return "", "", err
	}
	unitDir := filepath.Join(dir, "systemd", "user")
	return filepath.Join(unitDir, WallpaperRotateService), filepath.Join(unitDir, WallpaperRotateTimer), nil
}

// renderWallpaperUnits returns the two unit files EnableWallpaperRotation
// writes for channelID.
func renderWallpaperUnits(channelID string) (service, timer string, err error) {
	if !channelIDPattern.MatchString(channelID) {
		return "", "", fmt.Errorf("%w: %q", ErrWallpaperChannelInvalid, channelID)
	}
	exe, err := unitExecutable()
	if err != nil {
		return "", "", fmt.Errorf("livery: locating the application executable: %w", err)
	}
	quotedExe, err := systemdQuote(exe)
	if err != nil {
		return "", "", fmt.Errorf("livery: writing the wallpaper rotation unit's ExecStart: %w", err)
	}
	quotedID, err := systemdQuote(channelID)
	if err != nil {
		return "", "", err
	}
	service = fmt.Sprintf(wallpaperServiceTemplate, branding.AppName, quotedExe, WallpaperRotateFlag, quotedID)
	timer = fmt.Sprintf(wallpaperTimerTemplate, branding.AppName)
	return service, timer, nil
}

// wallpaperUnitChannel recovers the channel ID from a service unit's
// ExecStart, or false when the line is not one this package wrote.
func wallpaperUnitChannel(unit string) (string, bool) {
	for _, line := range strings.Split(unit, "\n") {
		rest, ok := strings.CutPrefix(line, "ExecStart=")
		if !ok {
			continue
		}
		_, quoted, ok := strings.Cut(rest, " "+WallpaperRotateFlag+" ")
		if !ok {
			return "", false
		}
		id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(quoted), `"`), `"`)
		return id, channelIDPattern.MatchString(id)
	}
	return "", false
}

// unitBackup is one unit file's content before a change, so a failed change
// can put it back.
type unitBackup struct {
	path    string
	data    []byte
	existed bool
}

func backupUnit(path string) (unitBackup, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return unitBackup{path: path}, nil
	}
	if err != nil {
		return unitBackup{}, err
	}
	return unitBackup{path: path, data: data, existed: true}, nil
}

func (b unitBackup) restore() error {
	if b.existed {
		return writeFileAtomically(b.path, b.data)
	}
	if err := os.Remove(b.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// EnableWallpaperRotation writes the service and timer for channelID and
// enables and starts the timer. It is idempotent: enabling the same channel
// again rewrites identical files and re-enables an enabled timer, and enabling
// a different channel repoints the service. A failure restores the files that
// were there before, so the user is left with the rotation they had. Under
// dry-run nothing is written or run.
func EnableWallpaperRotation(ctx context.Context, channelID string) (result error) {
	service, timer, err := renderWallpaperUnits(channelID)
	if err != nil {
		return err
	}
	servicePath, timerPath, err := wallpaperUnitPaths()
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would write %s and %s for channel %s and enable %s", servicePath, timerPath, channelID, WallpaperRotateTimer)
		return nil
	}
	var backups []unitBackup
	for _, path := range []string{servicePath, timerPath} {
		b, err := backupUnit(path)
		if err != nil {
			return err
		}
		backups = append(backups, b)
	}
	defer func() {
		if result == nil {
			return
		}
		for _, b := range backups {
			result = errors.Join(result, b.restore())
		}
		_, reloadErr := runCommand(ctx, "systemctl", "--user", "daemon-reload")
		result = errors.Join(result, reloadErr)
	}()
	if err := writeFileAtomically(servicePath, []byte(service)); err != nil {
		return fmt.Errorf("livery: installing wallpaper rotation: %w", err)
	}
	if err := writeFileAtomically(timerPath, []byte(timer)); err != nil {
		return fmt.Errorf("livery: installing wallpaper rotation: %w", err)
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("livery: reloading user units: %w: %s", err, strings.TrimSpace(out))
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "enable", "--now", WallpaperRotateTimer); err != nil {
		return fmt.Errorf("livery: enabling wallpaper rotation: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// DisableWallpaperRotation stops and disables the timer and removes both
// units. With no units installed it does nothing, so it is idempotent. A
// failure to stop the timer leaves everything in place; a later failure puts
// the files back and re-enables the timer, so rotation never reads as off
// while it still runs. Under dry-run nothing is changed.
func DisableWallpaperRotation(ctx context.Context) (result error) {
	servicePath, timerPath, err := wallpaperUnitPaths()
	if err != nil {
		return err
	}
	var backups []unitBackup
	installed := false
	for _, path := range []string{servicePath, timerPath} {
		b, err := backupUnit(path)
		if err != nil {
			return err
		}
		installed = installed || b.existed
		backups = append(backups, b)
	}
	if !installed {
		return nil
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would disable %s and remove %s and %s", WallpaperRotateTimer, servicePath, timerPath)
		return nil
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "disable", "--now", WallpaperRotateTimer); err != nil {
		return fmt.Errorf("livery: disabling wallpaper rotation: %w: %s", err, strings.TrimSpace(out))
	}
	defer func() {
		if result == nil {
			return
		}
		for _, b := range backups {
			result = errors.Join(result, b.restore())
		}
		_, reloadErr := runCommand(ctx, "systemctl", "--user", "daemon-reload")
		_, enableErr := runCommand(ctx, "systemctl", "--user", "enable", "--now", WallpaperRotateTimer)
		result = errors.Join(result, reloadErr, enableErr)
	}()
	for _, b := range backups {
		if err := os.Remove(b.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("livery: removing wallpaper rotation: %w", err)
		}
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("livery: reloading user units: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// WallpaperRotation is the observed state of wallpaper rotation.
type WallpaperRotation struct {
	// On means the timer's unit file is installed: the user turned rotation
	// on and has not turned it off.
	On bool
	// ChannelID is the collection the service rotates through, or empty when
	// the service is missing or was not written by ChairLift.
	ChannelID string
	// Scheduled means the user manager reports the timer enabled and active.
	// On without Scheduled is a broken state the page reports as such.
	Scheduled bool
	// Next is the next time the timer fires, or zero when it is unknown.
	Next time.Time
	// LastRunFailed means the most recent pass exited unsuccessfully.
	LastRunFailed bool
}

// ObserveWallpaperRotation reads rotation's state from the unit files and the
// user manager. With no timer installed it runs nothing. A failed systemctl
// read is returned as an error rather than reported as off.
func ObserveWallpaperRotation(ctx context.Context) (WallpaperRotation, error) {
	servicePath, timerPath, err := wallpaperUnitPaths()
	if err != nil {
		return WallpaperRotation{}, err
	}
	var state WallpaperRotation
	if _, err := os.Stat(timerPath); err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, fmt.Errorf("livery: checking wallpaper rotation: %w", err)
	}
	state.On = true
	if service, err := os.ReadFile(servicePath); err == nil {
		state.ChannelID, _ = wallpaperUnitChannel(string(service))
	} else if !os.IsNotExist(err) {
		return state, fmt.Errorf("livery: checking wallpaper rotation: %w", err)
	}

	timer, err := systemctlShow(ctx, WallpaperRotateTimer, "UnitFileState", "ActiveState", "NextElapseUSecRealtime")
	if err != nil {
		return state, err
	}
	state.Scheduled = timer["UnitFileState"] == "enabled" && timer["ActiveState"] == "active"
	state.Next = parseSystemdTimestamp(timer["NextElapseUSecRealtime"])
	service, err := systemctlShow(ctx, WallpaperRotateService, "Result")
	if err != nil {
		return state, err
	}
	if result := service["Result"]; result != "" && result != "success" {
		state.LastRunFailed = true
	}
	return state, nil
}

// systemctlShow reads properties of one user unit.
func systemctlShow(ctx context.Context, unit string, properties ...string) (map[string]string, error) {
	args := []string{"--user", "show", "--timestamp=unix"}
	for _, p := range properties {
		args = append(args, "-p", p)
	}
	args = append(args, unit)
	out, err := runCommand(ctx, "systemctl", args...)
	if err != nil {
		return nil, fmt.Errorf("livery: reading %s: %w: %s", unit, err, strings.TrimSpace(out))
	}
	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values, nil
}

// parseSystemdTimestamp reads a `--timestamp=unix` value ("@1762041600").
// Anything else — an empty value or "n/a" for a timer with nothing queued —
// is the zero time.
func parseSystemdTimestamp(value string) time.Time {
	seconds, ok := strings.CutPrefix(value, "@")
	if !ok {
		return time.Time{}
	}
	n, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// NextWallpaper returns the picture after the current one in channel,
// wrapping at the end. When the current picture is not in the channel the
// rotation starts at its first picture. ok is false for an empty channel.
func NextWallpaper(channel WallpaperChannel, currentURI string) (Wallpaper, bool) {
	if len(channel.Wallpapers) == 0 {
		return Wallpaper{}, false
	}
	next := 0
	if _, index, found := SelectedWallpaper([]WallpaperChannel{channel}, currentURI); found {
		next = (index + 1) % len(channel.Wallpapers)
	}
	return channel.Wallpapers[next], true
}

// wallpaperDataDirs is an injection seam for the catalog directories.
var wallpaperDataDirs = WallpaperDataDirs

// RotateWallpaper is one headless pass: it moves the desktop background to
// the next picture of channelID through ApplyBackground, which writes
// picture-uri and picture-uri-dark and confirms both. Off GNOME, or where
// GNOME's background schema is absent, there is nothing to rotate and the
// pass succeeds without writing. A collection that is no longer installed is
// an error, so the timer's failure shows on the Livery page.
func RotateWallpaper(ctx context.Context, channelID string) error {
	if !channelIDPattern.MatchString(channelID) {
		return fmt.Errorf("%w: %q", ErrWallpaperChannelInvalid, channelID)
	}
	supported, currentURI, err := ProbeNativeBackground(ctx)
	if err != nil {
		return err
	}
	if !supported {
		log.Printf("livery: skipping wallpaper rotation: this desktop does not take a GNOME background")
		return nil
	}
	channels, discoveryErr := WallpaperChannels(wallpaperDataDirs())
	for _, channel := range channels {
		if channel.ID != channelID {
			continue
		}
		next, ok := NextWallpaper(channel, currentURI)
		if !ok {
			break
		}
		if current, found := uriPath(currentURI); found && current == next.File {
			return nil // A one-picture collection already showing it.
		}
		return ApplyBackground(ctx, next)
	}
	if discoveryErr != nil {
		return discoveryErr
	}
	return fmt.Errorf("%w: %s", ErrWallpaperChannelMissing, channelID)
}
