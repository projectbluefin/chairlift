package livery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

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
//
// The unit carries no network dependency. A user manager cannot order
// against `network-online.target`, which lives in the system manager, so the
// wait has to happen in the pass itself: Rotate retries the dock's fetch
// while the failure still looks like a network that is not up yet.
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
		quoted, err := systemdQuote("GSETTINGS_SCHEMA_DIR=" + dir)
		if err != nil {
			return fmt.Errorf("livery: carrying GSETTINGS_SCHEMA_DIR into the rotation unit: %w", err)
		}
		env = "Environment=" + quoted + "\n"
	}
	quotedExe, err := systemdQuote(exe)
	if err != nil {
		return fmt.Errorf("livery: writing the rotation unit's ExecStart: %w", err)
	}
	unit := fmt.Sprintf(unitTemplate, branding.AppName, env, quotedExe, RotateFlag)
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

// systemdQuote renders one value as a double-quoted systemd token.
//
// Both values the unit interpolates are paths the process discovers at
// runtime — os.Executable() and $GSETTINGS_SCHEMA_DIR — and a unit file is
// neither shell nor plain text. A space splits ExecStart into further
// arguments, a lone `%` starts a specifier systemd expands to something else
// (and an unknown one is an error), a backslash starts an escape, and a
// newline ends the directive, so a path containing one could carry an extra
// directive into the file. Quoting handles the first, doubling handles the
// next two, and a newline has no representation at all inside a unit value —
// that one can only be refused.
func systemdQuote(value string) (string, error) {
	if strings.ContainsAny(value, "\n\r") {
		return "", fmt.Errorf("a newline in %q cannot be written to a systemd unit", value)
	}
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%")
	return `"` + replacer.Replace(value) + `"`, nil
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
// That guard lives here rather than in NextID, which stays total so any other
// caller advancing a stale selection still lands on a real entry: NextID maps
// CustomID onto the first catalog entry, so without the check a user's own
// SVG would be silently replaced at the next login. The page desensitizes the
// rotation switch for a custom selection (pageview.LiveryRotationAvailable),
// but the stored flag outlives a selection change, so the headless pass has
// to refuse it too.
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
	rotatedDock := state.DockRotate && state.DockEnabled && state.DockID != CustomID
	if state.PanelRotate && state.PanelEnabled && state.PanelID != CustomID {
		next := NextID(state.PanelID)
		if err := Apply(ctx, Panel, Source{Kind: FromCatalog, Value: next}); err != nil {
			failures = append(failures, err.Error())
		} else if err := SetString(ctx, KeyPanelID, next); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if rotatedDock {
		next := NextCNCFID(state.DockID)
		if err := applyRotation(ctx, Dock, Source{Kind: FromCNCF, Value: next}); err != nil {
			failures = append(failures, err.Error())
		} else if err := SetString(ctx, KeyDockID, next); err != nil {
			failures = append(failures, err.Error())
		}
	}

	// The shell caches icon textures by name, and the rotation unit runs
	// after graphical-session.target — by which point the dash may already
	// have cached the previous mark. Without this the rotated dock icon would
	// not appear until something else happened to nudge it.
	if rotatedDock {
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

// rotateRetryDelays spaces the retries of a rotation step that needs the
// network. The unit runs at login, and on a host whose connectivity arrives
// after the graphical session does — Wi-Fi associating, a VPN coming up — the
// first fetch fails against a stack that is seconds away from working.
//
// The session token is recorded even for a failed pass, deliberately, so
// nothing retries the dock later in that session: without this the user would
// see no rotation at all until the next login, and the only trace would be a
// journal line. The delays are a variable so a test does not wait them out.
var rotateRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}

// applyRotation applies one rotated mark, retrying while the failure still
// looks like a network that has not come up.
func applyRotation(ctx context.Context, surface Surface, src Source) error {
	err := Apply(ctx, surface, src)
	for _, delay := range rotateRetryDelays {
		if err == nil || !retryableRotationError(err) {
			break
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(delay):
		}
		err = Apply(ctx, surface, src)
	}
	return err
}

// retryableRotationError reports whether waiting could plausibly change the
// answer. A mark the artwork repository does not publish, an expired context,
// or anything else non-network is a permanent answer and is not retried.
func retryableRotationError(err error) bool {
	if errors.Is(err, ErrIconNotFound) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// RotationContext bounds one headless rotation pass.
//
// It is longer than the package's ordinary command timeout because the pass
// may spend most of it waiting for the network; every individual command and
// fetch inside it keeps its own, shorter bound.
func RotationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), rotationTimeout)
}

// rotationTimeout covers the fetch plus every retry in rotateRetryDelays.
const rotationTimeout = 3 * time.Minute
