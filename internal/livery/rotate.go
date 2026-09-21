package livery

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// UnitName is the systemd user unit that performs rotation.
const UnitName = "chairlift-livery-rotate.service"

// RotateFlag is the command-line flag that runs one rotation pass. It is the
// unit's entire ExecStart.
const RotateFlag = "--rotate-livery"

// unitTemplate rotates once per graphical session.
//
// Rotation runs at login, not at logout. The brief that prompted this feature
// asked for logout, but a logout hook is strictly worse here for a reason its
// own fallback clause exposes: when logout is abrupt — a crash, a force-quit,
// a power loss — the hook does not fire, so the fallback leaves the previous
// mark in place and the user sees no rotation at all. That is precisely the
// outcome the feature exists to avoid. Running at login needs one mechanism
// instead of two, is idempotent because the session either started or it did
// not, and produces the identical observable result: a different mark each
// time you sit down. The extension also reloads the panel icon live on
// `changed::menuicon-setting`, so nothing has to wait for a session boundary
// to take effect.
//
// The Description is built from branding.AppName rather than spelled here:
// `systemctl --user status` prints it, so a user reads it.
const unitTemplate = `[Unit]
Description=%s Livery rotation
PartOf=graphical-session.target
After=graphical-session.target

[Service]
Type=oneshot
%sExecStart=%s %s

[Install]
WantedBy=graphical-session.target
`

// configHome is an injection seam for $XDG_CONFIG_HOME.
var configHome = defaultConfigHome

func defaultConfigHome() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("livery: locating home directory: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}

// executablePath is an injection seam for the binary the unit invokes.
var executablePath = os.Executable

// RunsFromSystemPrefix reports whether this binary lives somewhere a login
// will still find it.
//
// The rotation unit records an absolute ExecStart. For an installed build
// that is /usr/bin/chairlift and stable. For a source build it is whatever
// build/ directory happened to produce it, which a later rebuild, clean, or
// move invalidates — and systemd reports that failure to the journal, where
// nobody is looking, while the UI keeps claiming rotation is on. The page
// says so instead of letting it fail quietly.
func RunsFromSystemPrefix() bool {
	exe, err := executablePath()
	if err != nil {
		return false
	}
	for _, prefix := range []string{"/usr/bin/", "/usr/local/bin/", "/usr/libexec/"} {
		if strings.HasPrefix(exe, prefix) {
			return true
		}
	}
	return false
}

// UnitPath returns the absolute path of the rotation unit.
func UnitPath() (string, error) {
	dir, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", UnitName), nil
}

// RotationInstalled reports whether the rotation unit is on disk.
func RotationInstalled() bool {
	path, err := UnitPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// InstallRotation writes and enables the rotation unit. It is idempotent:
// rewriting an identical unit and re-enabling an enabled unit are both no-ops
// as far as the user can observe.
func InstallRotation(ctx context.Context) error {
	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("livery: locating the application executable: %w", err)
	}
	path, err := UnitPath()
	if err != nil {
		return err
	}
	// An installed build finds its schema in /usr/share/glib-2.0/schemas. A
	// source build does not, and systemd starts the unit with none of the
	// developer's shell environment — so without carrying the override
	// forward, rotation would fail silently at the next login on exactly the
	// path a developer is testing from.
	var env string
	if dir := os.Getenv("GSETTINGS_SCHEMA_DIR"); dir != "" {
		env = "Environment=GSETTINGS_SCHEMA_DIR=" + dir + "\n"
	}
	unit := fmt.Sprintf(unitTemplate, branding.AppName, env, exe, RotateFlag)
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would write %s and enable it", path)
		_ = unit
		return nil
	}
	if err := writeFileAtomically(path, []byte(unit)); err != nil {
		return fmt.Errorf("livery: installing rotation unit: %w", err)
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("livery: reloading user units: %w: %s", err, strings.TrimSpace(out))
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "enable", UnitName); err != nil {
		return fmt.Errorf("livery: enabling rotation: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// RemoveRotation disables and deletes the rotation unit.
func RemoveRotation(ctx context.Context) error {
	path, err := UnitPath()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return nil
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would disable and remove %s", path)
		return nil
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "disable", UnitName); err != nil {
		return fmt.Errorf("livery: disabling rotation: %w: %s", err, strings.TrimSpace(out))
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("livery: removing rotation unit: %w", err)
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("livery: reloading user units: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// SyncRotationUnit installs the unit when either section rotates and removes
// it when neither does, so the unit's presence always matches the switches.
func SyncRotationUnit(ctx context.Context, s State) error {
	if s.PanelRotate || s.DockRotate {
		return InstallRotation(ctx)
	}
	return RemoveRotation(ctx)
}

// sessionToken identifies the current graphical session.
//
// It is the idempotence key for rotation: a pass that has already run for
// this token does nothing. systemd already runs the oneshot unit once per
// graphical session, so this guards the remaining case — the unit being
// restarted, or the flag being run by hand — from double-advancing the
// selection.
func sessionToken(ctx context.Context) string {
	out, err := runCommand(ctx, "systemctl", "--user", "show", "-p",
		"ActiveEnterTimestampMonotonic", "--value", "graphical-session.target")
	if token := strings.TrimSpace(out); err == nil && token != "" && token != "0" {
		return "session:" + token
	}
	// A host without that target still gets a stable per-boot token, which
	// keeps repeated manual runs from advancing more than once.
	if data, readErr := os.ReadFile("/proc/sys/kernel/random/boot_id"); readErr == nil {
		return "boot:" + strings.TrimSpace(string(data))
	}
	return ""
}

// Rotate advances every section whose rotation switch is on, then persists
// the new selections.
//
// Rotation advances catalog selections only. A section pinned to a custom
// file or, in the app grid's case, to a personal brand is left alone: those
// are choices, not a cycle. The app grid has no rotation switch at all.
//
// It is deterministic — NextID walks the catalog in order — and idempotent
// within a session via the rotation token. A section that is rotating but not
// enabled is skipped rather than turned on: rotation changes which mark is
// selected, it does not decide whether ChairLift manages that surface.
func Rotate(ctx context.Context) error {
	state, err := Load(ctx)
	if err != nil {
		return err
	}
	if !state.PanelRotate && !state.DockRotate {
		return nil
	}

	token := sessionToken(ctx)
	if token != "" && token == state.RotationToken {
		return nil
	}

	var failures []string
	if state.PanelRotate && state.PanelEnabled {
		next := NextID(state.PanelID)
		if err := Apply(ctx, Panel, Source{Kind: FromCatalog, Value: next}); err != nil {
			failures = append(failures, err.Error())
		} else if err := SetString(ctx, KeyPanelID, next); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if state.DockRotate && state.DockEnabled {
		next := NextCNCFID(state.DockID)
		if err := Apply(ctx, Dock, Source{Kind: FromCNCF, Value: next}); err != nil {
			failures = append(failures, err.Error())
		} else if err := SetString(ctx, KeyDockID, next); err != nil {
			failures = append(failures, err.Error())
		}
	}

	// The shell caches icon textures by name, and the rotation unit runs
	// after graphical-session.target — by which point the dash may already
	// have cached the previous mark. Without this the rotated dock icon would
	// not appear until something else happened to nudge it.
	if state.DockRotate && state.DockEnabled {
		if err := RefreshShellIcons(); err != nil {
			failures = append(failures, err.Error())
		}
	}

	// The token is recorded even when a section failed. Without that, a
	// permanently failing section — an unreadable custom file, say — would
	// re-advance the other section on every retry.
	if token != "" {
		if err := SetString(ctx, KeyRotationToken, token); err != nil {
			failures = append(failures, err.Error())
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("livery: rotation: %s", strings.Join(failures, "; "))
	}
	return nil
}
