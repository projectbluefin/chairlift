package livery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// commandTimeout bounds every external call. All of them are local and
// sub-second in practice; the bound exists so a wedged helper cannot hang a
// UI action forever.
const commandTimeout = 30 * time.Second

// Surface is one place a mark is shown. Each is a themed icon *name* that
// ChairLift shadows in the user's own icon theme; none is a path, and none
// involves editing a file another package owns.
type Surface int

const (
	// AppGrid is the Show Applications button — "who you are".
	AppGrid Surface = iota
	// Panel is the top-bar menu button — "who you stand with".
	Panel
	// Dock is the Files application mark — "what you roll with".
	Dock
)

// surfaceSpec says where a surface's icon file goes and which rendition it
// takes.
type surfaceSpec struct {
	// theme is the icon theme the override must be written into, and it is
	// not the same for every surface. XDG resolves the *current* theme and
	// its parents before falling back to hicolor, so a name Adwaita already
	// ships can only be shadowed inside Adwaita. view-app-grid-symbolic is
	// such a name; org.gnome.Nautilus is not, which is why that one works
	// from hicolor. Getting this wrong produces a write that succeeds and an
	// icon that never changes.
	theme string
	// subdir is the theme-relative directory, which must match the context
	// directories the theme's index.theme declares.
	subdir string
	// name is the icon-theme name being shadowed.
	name string
}

// surfacesByDesktop maps each supported desktop environment to its surface table.
//
// GNOME and KDE shadow different application names: GNOME's Files application
// is org.gnome.Nautilus, while KDE Plasma's file manager is org.kde.dolphin.
// Why each lands in hicolor rather than the session's own theme is recorded
// once on DockIconNameKDE.
var surfacesByDesktop = map[deskenv.Desktop]map[Surface]surfaceSpec{
	deskenv.GNOME: {
		// dash-to-dock asks for `view-app-grid-${sessionMode}-symbolic`, i.e.
		// view-app-grid-user-symbolic, which exists nowhere on disk. It reaches
		// the real icon through St's fallback: appIcons.js sets
		// `fallbackIconName = this._iconActor.iconName` — the base
		// ShowAppsIcon's view-app-grid-symbolic — before overwriting iconName.
		// Shadowing the fallback name is therefore what actually moves the
		// button, and it moves the overview's app-grid button too, since both
		// resolve the same name.
		AppGrid: {theme: "Adwaita", subdir: filepath.Join("symbolic", "actions"), name: "view-app-grid-symbolic"},
		// ChairLift's own name, referenced only by the extension's
		// menuicon-setting, so writing it can never clobber another package's
		// icon and reverting is one unlink.
		// name is empty: the panel's icon name depends on the selection, so
		// iconPath computes it. See PanelIconName.
		Panel: {theme: "hicolor", subdir: filepath.Join("scalable", "actions"), name: ""},
		// The Files application's own name. GNOME stores one icon per
		// application, so this shadows the mark everywhere Files is drawn — the
		// dash, the app grid, the window switcher, notifications — not only on
		// the dock.
		Dock: {theme: "hicolor", subdir: filepath.Join("scalable", "apps"), name: DockIconNameGNOME},
	},
	deskenv.KDE: {
		// hicolor, not Breeze: see DockIconNameKDE.
		Dock: {theme: "hicolor", subdir: filepath.Join("scalable", "apps"), name: DockIconNameKDE},
	},
}

// surfaceFor returns the surface specification for the given surface on the
// target desktop environment.
//
// Unknown resolves to the GNOME table, and that is a deliberate exception to
// deskenv's fail-closed default rather than an oversight. Every ChairLift
// surface predates this map and was written into the GNOME names
// unconditionally, so a session deskenv cannot name — Cinnamon, Xfce, Sway, a
// container, a TTY, an unset session — kept working because those writes
// landed anyway. Failing closed here would turn that into "livery is
// unavailable on your desktop" for users who have marks installed today, a
// removal this change does not intend. The write is inert where nothing
// resolves the name: it places a file in the user's own icon theme and edits
// nothing another package owns.
//
// Only a desktop deskenv *does* recognize gets a fail-closed answer, because
// there the session is known not to resolve the other table's names: a
// surface missing from that desktop's table returns false and every caller
// reports it. That is what keeps KDE from receiving GNOME-targeted writes.
func surfaceFor(s Surface, de deskenv.Desktop) (surfaceSpec, bool) {
	table, ok := surfacesByDesktop[de]
	if !ok {
		table = surfacesByDesktop[deskenv.GNOME]
	}
	spec, ok := table[s]
	return spec, ok
}

// surfaceSupported reports whether a surface has anywhere to land on the given
// desktop. Callers that run unattended — rotation, most of all — ask this
// instead of letting Apply fail, so a section whose state was written under a
// different desktop is skipped rather than turned into an error every login.
func surfaceSupported(s Surface, de deskenv.Desktop) bool {
	_, ok := surfaceFor(s, de)
	return ok
}

// overridePathsForSurface returns every path this package could have written
// for one surface, across every desktop table, on this host.
//
// Clear consults all of them rather than only the current desktop's, because
// the desktop is read at call time and a user's sessions are not fixed: a
// Files mark applied under GNOME writes org.gnome.Nautilus.svg, and a Clear
// run in a later Plasma session that unlinked only org.kde.dolphin.svg would
// leave that file behind for good — state says the surface is off while the
// override still overrides. Clear never asks it for the panel, whose file name
// varies per selection; removePanelIcons sweeps those by prefix instead.
func overridePathsForSurface(s Surface, selectionID string) (map[string]string, error) {
	paths := map[string]string{}
	for desktop := range surfacesByDesktop {
		spec, ok := surfaceFor(s, desktop)
		if !ok {
			continue
		}
		path, err := IconPathFor(desktop, s, selectionID)
		if err != nil {
			return nil, err
		}
		paths[path] = spec.theme
	}
	return paths, nil
}

// detectDesktop detects the current session's desktop environment.
// It is an injection seam so tests can exercise desktop paths deterministically.
var detectDesktop = deskenv.Detect

func surfaceName(s Surface) string {
	switch s {
	case AppGrid:
		return "AppGrid"
	case Panel:
		return "Panel"
	case Dock:
		return "Dock"
	default:
		return fmt.Sprintf("Surface(%d)", s)
	}
}

// PanelIconPrefix begins every icon name ChairLift installs for the panel.
//
// The prefix is load-bearing twice over. It identifies ChairLift's own icons
// without any stored flag, which is how CapturePanelOverrides avoids saving
// one of its own names as "the user's previous icon"; and it namespaces them
// away from any icon another package owns.
const PanelIconPrefix = "chairlift-livery-"

// PanelIconName is the icon-theme name for one selection.
//
// The name varies per selection, and that is not cosmetic: GSettings emits no
// `changed::` signal when a key is written with the value it already holds.
// The extension refreshes its indicator only on that signal
// (extension.js:314), so a single fixed name with swapped file contents would
// leave the panel showing the previous mark until the shell restarted —
// verified with `gsettings monitor`, which reported two change events for
// three writes when one repeated a value. Writing a different name per
// selection makes the value genuinely change.
func PanelIconName(id string) string {
	return PanelIconPrefix + id + "-symbolic"
}

// DockIconName is the icon-theme name the Files mark shadows on GNOME.
// Retained for backward compatibility. See DockIconNameGNOME and DockIconNameKDE.
const DockIconName = DockIconNameGNOME

const (
	// DockIconNameGNOME is the icon-theme name the Files mark shadows on GNOME.
	DockIconNameGNOME = "org.gnome.Nautilus"

	// DockIconNameKDE is the icon-theme name the Files mark shadows on KDE Plasma.
	//
	// This is the canonical record of why the KDE Files mark is written into
	// hicolor rather than Breeze; other sites refer here instead of repeating
	// it. XDG resolves the current theme and its parents before hicolor, so a
	// name the session's theme already ships can only be shadowed inside that
	// theme. Breeze does not ship org.kde.dolphin.svg — it ships only
	// system-file-manager.svg — so nothing shadows hicolor for this name and
	// the placement overrides the application launcher icon across Plasma
	// surfaces without touching vendor desktop entries.
	DockIconNameKDE = "org.kde.dolphin"
)

// DockIconNameFor returns the icon-theme name shadowed by the Files mark on
// the given desktop environment.
func DockIconNameFor(desktop deskenv.Desktop) string {
	if desktop == deskenv.KDE {
		return DockIconNameKDE
	}
	return DockIconNameGNOME
}

// extensionSchema is the Custom Command Menu extension's schema. It is
// installed by the extension and read-only to ChairLift; only the user's own
// dconf *values* under it are written here.
const extensionSchema = "org.gnome.shell.extensions.custom-command-list"

const (
	extensionIconKey = "menuicon-setting"
	// extensionModeKey gates whether the icon is drawn at all: 0 renders the
	// literal text "Commands", 1 renders custom text, and only 2 renders an
	// icon. Setting the icon key alone is a silent no-op on a host left at
	// the default, so both are written together.
	extensionModeKey  = "menuoptions-setting"
	extensionIconMode = "2"
)

// ErrExtensionMissing reports that the Custom Command Menu extension's schema
// is not installed, so the panel mark cannot be set on this host. It is a
// first-class value because it is an ordinary state on a non-Bluefin desktop,
// not a failure: the page disables its panel section rather than erroring.
var ErrExtensionMissing = errors.New("livery: the Custom Command Menu extension is not installed")

// SourceKind says where a mark's bytes come from.
type SourceKind int

const (
	// FromCatalog reads one of the embedded foundation marks.
	FromCatalog SourceKind = iota
	// FromFile reads the user's own SVG.
	FromFile
	// FromSimpleIcons fetches a mark from simpleicons.org by slug.
	FromSimpleIcons
	// FromCNCF fetches a project's color mark from cncf/artwork.
	FromCNCF
)

// Source identifies the artwork for a surface.
type Source struct {
	Kind SourceKind
	// Value is a catalog id, an absolute file path, or a Simple Icons slug,
	// according to Kind.
	Value string
}

// runCommand is an injection seam for external calls, so the install and
// revert paths are testable without a live session.
var runCommand = execCommand

// allowedCommands is every program this package may run.
//
// internal/installcheck classifies this call site as unprivileged, and that
// classification backs the "every privileged dispatch point journals"
// invariant. A seam that took an arbitrary program name would make the
// classification an assertion rather than a fact — nothing would stop a later
// caller passing pkexec — so the set is closed here and enforced at the call.
var allowedCommands = map[string]bool{
	"gsettings":             true,
	"dconf":                 true,
	"gtk-update-icon-cache": true,
	"systemctl":             true,
}

func execCommand(ctx context.Context, name string, args ...string) (string, error) {
	if !allowedCommands[name] {
		return "", fmt.Errorf("livery: %q is not an allowed command", name)
	}
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// dataHome is an injection seam for $XDG_DATA_HOME, so tests never write
// into a real home directory.
var dataHome = defaultDataHome

func defaultDataHome() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("livery: locating home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share"), nil
}

// themeDir returns the user's directory for one icon theme.
func themeDir(theme string) (string, error) {
	dir, err := dataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "icons", theme), nil
}

// IconPath returns where a surface's override is installed for a selection on
// the current desktop environment.
//
// selectionID is only consulted for the panel, whose icon name varies per
// selection; the other two surfaces shadow a fixed name their consumer
// already asks for.
func IconPath(s Surface, selectionID string) (string, error) {
	return IconPathFor(detectDesktop(), s, selectionID)
}

// IconPathFor returns where a surface's override is installed for a selection
// on a specific desktop environment.
func IconPathFor(desktop deskenv.Desktop, s Surface, selectionID string) (string, error) {
	spec, ok := surfaceFor(s, desktop)
	if !ok {
		return "", fmt.Errorf("livery: surface %s is not supported on %s", surfaceName(s), desktop)
	}
	dir, err := themeDir(spec.theme)
	if err != nil {
		return "", err
	}
	name := spec.name
	if s == Panel {
		name = PanelIconName(selectionID)
	}
	return filepath.Join(dir, spec.subdir, name+".svg"), nil
}

// panelIconDir is where every panel mark is installed, used to sweep marks
// left by previous selections.
func panelIconDir() (string, error) {
	spec, ok := surfaceFor(Panel, deskenv.GNOME)
	if !ok {
		return "", errors.New("livery: panel surface not configured")
	}
	dir, err := themeDir(spec.theme)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, spec.subdir), nil
}

// removePanelIcons deletes every mark ChairLift has installed for the panel.
//
// Selections leave a file behind under their own name, so without this a user
// who tried several would accumulate one SVG per mark they had ever picked.
// The prefix match is what makes "ours" identifiable.
func removePanelIcons() error {
	dir, err := panelIconDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("livery: listing panel icons: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), PanelIconPrefix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("livery: removing %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// writeFileAtomically writes data to dest, creating parent directories.
//
// The rename matters: GTK may be reading the theme concurrently, and a
// half-written SVG resolves to a blank icon rather than an error the user
// could act on.
func writeFileAtomically(dest string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("livery: creating directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".livery-*")
	if err != nil {
		return fmt.Errorf("livery: creating temporary file: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("livery: writing %s: %w", dest, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("livery: closing %s: %w", dest, err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return fmt.Errorf("livery: setting permissions: %w", err)
	}
	if err := os.Rename(name, dest); err != nil {
		return fmt.Errorf("livery: installing %s: %w", dest, err)
	}
	return nil
}

// refreshIconCache rebuilds one theme's icon cache.
//
// This is load-bearing, not hygiene: when a theme directory carries an
// icon-theme.cache, GTK trusts the cache and will not see a newly written
// SVG at all, so the whole feature silently no-ops without it.
//
// -t is --ignore-theme-index, which is what lets this work in a user theme
// directory that has no index.theme of its own — the system directory for the
// same theme name supplies it, and GTK merges the two.
func refreshIconCache(ctx context.Context, theme string) error {
	dir, err := themeDir(theme)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	if _, err := exec.LookPath("gtk-update-icon-cache"); err != nil {
		// Without the tool GTK falls back to scanning by directory mtime,
		// which the write above already bumped.
		return nil
	}
	if out, err := runCommand(ctx, "gtk-update-icon-cache", "-f", "-t", "-q", dir); err != nil {
		return fmt.Errorf("livery: refreshing %s icon cache: %w: %s", theme, err, strings.TrimSpace(out))
	}
	return nil
}

// resolve turns a Source into SVG bytes for one rendition.
//
// Every failure names the thing that failed. A missing custom file, an
// unreachable service, or an unknown slug is reported rather than silently
// substituted, because substituting would leave the page claiming a
// selection the user cannot see.
func resolve(ctx context.Context, src Source) ([]byte, error) {
	switch src.Kind {
	case FromCatalog:
		return Asset(src.Value)
	case FromFile:
		if src.Value == "" {
			return nil, errors.New("livery: no custom icon file has been chosen")
		}
		data, err := os.ReadFile(src.Value)
		if err != nil {
			return nil, fmt.Errorf("livery: reading custom icon %s: %w", src.Value, err)
		}
		if !looksLikeSVG(data) {
			return nil, fmt.Errorf("livery: %s is not an SVG image", src.Value)
		}
		return data, nil
	case FromSimpleIcons:
		return FetchSimpleIcon(ctx, src.Value)
	case FromCNCF:
		return FetchCNCFIcon(ctx, src.Value)
	default:
		return nil, fmt.Errorf("livery: unknown source kind %d", src.Kind)
	}
}

// looksLikeSVG checks for an <svg root element. It is a shape check, not a
// validator: the point is to reject a PNG or a text file chosen by mistake
// with a message naming the file, rather than installing bytes that resolve
// to a blank icon.
func looksLikeSVG(data []byte) bool {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return strings.Contains(string(head), "<svg")
}

// Apply installs a mark on one surface.
//
// The panel additionally needs the extension pointed at ChairLift's icon
// name; the other two surfaces shadow a name their consumer already asks for,
// so installing the file is the whole operation.
func Apply(ctx context.Context, s Surface, src Source) error {
	desktop := detectDesktop()
	spec, ok := surfaceFor(s, desktop)
	if !ok {
		return fmt.Errorf("livery: surface %s is not supported on %s", surfaceName(s), desktop)
	}
	data, err := resolve(ctx, src)
	if err != nil {
		return err
	}
	selectionID := src.Value
	if src.Kind != FromCatalog {
		// A custom file or a fetched brand has no catalog id, but the panel
		// still needs a stable name that differs from the previous
		// selection's. Custom files append a short content hash so replacing
		// one custom file with another triggers the extension's changed signal;
		// brands and CNCF marks use their unique slug/value.
		switch src.Kind {
		case FromFile:
			sum := sha256.Sum256(data)
			selectionID = CustomID + "-" + hex.EncodeToString(sum[:4])
		case FromSimpleIcons:
			selectionID = "brand-" + src.Value
		case FromCNCF:
			selectionID = "cncf-" + src.Value
		}
	}
	dest, err := IconPathFor(desktop, s, selectionID)
	if err != nil {
		return err
	}

	// The artwork is resolved above even in dry-run, so a missing custom
	// file or an unknown brand name is still reported: those are the
	// failures a user most needs to see, and reporting them costs no
	// mutation. Only the writes stop here.
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would install %d bytes at %s and refresh the %s icon cache", len(data), dest, spec.theme)
		if s == Panel {
			log.Printf("[DRY-RUN] would set %s %s=%s and %s=%s",
				extensionSchema, extensionIconKey, PanelIconName(selectionID), extensionModeKey, extensionIconMode)
		}
		return nil
	}

	// Sweep marks from previous selections before installing this one, so a
	// user who tries several does not accumulate an SVG per mark.
	if s == Panel {
		if err := removePanelIcons(); err != nil {
			return err
		}
	}

	if err := writeFileAtomically(dest, data); err != nil {
		return err
	}
	if err := refreshIconCache(ctx, spec.theme); err != nil {
		return err
	}
	if s != Panel {
		return nil
	}
	if err := gsettingsSet(ctx, extensionSchema, extensionIconKey, PanelIconName(selectionID)); err != nil {
		return err
	}
	return gsettingsSet(ctx, extensionSchema, extensionModeKey, extensionIconMode)
}

// Clear removes a surface's override, restoring whatever the system supplies.
//
// Removal spans every desktop's variant of the surface, not just the running
// session's, and deliberately does not consult the running session at all: see
// overridePathsForSurface. Apply fails closed on a surface the current desktop
// has no table entry for, but Clear must not, or a mark applied under GNOME
// would be unremovable from a later Plasma session — state would read
// "disabled" while the override file kept overriding.
func Clear(ctx context.Context, s Surface) error {
	if s == Panel {
		return clearPanel(ctx)
	}
	// Every desktop's variant of this surface is removed, not only the one
	// this session would write, so a mark applied under another desktop does
	// not outlive the state that says it is gone.
	paths, err := overridePathsForSurface(s, "")
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("livery: surface %s has no override on any desktop", surfaceName(s))
	}
	themes := map[string]bool{}
	for _, theme := range paths {
		themes[theme] = true
	}
	if dryrun.Enabled() {
		for path, theme := range paths {
			log.Printf("[DRY-RUN] would remove %s and refresh the %s icon cache", path, theme)
		}
		return nil
	}
	for path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("livery: removing %s: %w", path, err)
		}
	}
	for theme := range themes {
		if err := refreshIconCache(ctx, theme); err != nil {
			return err
		}
	}
	// The app grid lives in a theme directory ChairLift created. Leaving an
	// empty tree behind would keep a user-scope Adwaita directory in the
	// search path for no reason, so prune it back out. Pruning is safe for
	// any theme here: it removes only directories that are already empty.
	if s == AppGrid {
		for theme := range themes {
			pruneEmptyThemeDir(theme)
		}
	}
	return nil
}

// clearPanel sweeps the panel's marks.
//
// The panel's file name varies per selection, so overridePathsForSurface
// cannot name them; removePanelIcons matches PanelIconPrefix instead. The
// panel exists in one table only, and its theme is fixed, so the sweep is
// desktop-independent for the same reason Clear is.
func clearPanel(ctx context.Context) error {
	spec, ok := surfaceFor(Panel, deskenv.GNOME)
	if !ok {
		return errors.New("livery: panel surface not configured")
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would remove the panel marks and refresh the %s icon cache", spec.theme)
		return nil
	}
	if err := removePanelIcons(); err != nil {
		return err
	}
	return refreshIconCache(ctx, spec.theme)
}

// ClearPanelSettings puts the extension's settings back the way they were.
//
// savedIcon and savedMode are the user-layer values CapturePanelOverrides
// recorded before the first write. Empty means the user had none, so the key
// is reset and the lower layers — the distro default on Bluefin, the schema
// default elsewhere — answer again. Restoring a fixed string instead would be
// wrong on every host: the schema default is 'utilities-terminal-symbolic',
// Bluefin's distro layer says 'ublue-logo-symbolic', and a third user may
// have set something else.
func ClearPanelSettings(ctx context.Context, savedIcon, savedMode string) error {
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would restore %s %s=%q and %s=%q (empty means reset)",
			extensionSchema, extensionIconKey, savedIcon, extensionModeKey, savedMode)
		return nil
	}
	if err := restoreKey(ctx, extensionIconKey, savedIcon); err != nil {
		return err
	}
	if err := restoreKey(ctx, extensionModeKey, savedMode); err != nil {
		return err
	}
	return ForgetPanelOverrides(ctx)
}

// ForgetPanelOverrides drops the recorded capture once it has been restored.
//
// The capture is only taken when both saved keys are empty, so leaving the
// restored values behind would make every later enable reuse the very first
// capture: enable, disable, set a panel icon by hand, enable, disable would
// restore the pre-first-enable value and silently discard the newer manual
// choice. Clearing the keys after a successful restore makes each enable
// capture the user layer as it stands at that moment.
func ForgetPanelOverrides(ctx context.Context) error {
	if err := SetString(ctx, KeySavedPanelIcon, ""); err != nil {
		return err
	}
	return SetString(ctx, KeySavedPanelMode, "")
}

func restoreKey(ctx context.Context, key, saved string) error {
	if saved == "" {
		return gsettingsReset(ctx, extensionSchema, key)
	}
	return gsettingsSet(ctx, extensionSchema, key, saved)
}

// gsettingsGet reads one key, returning ErrExtensionMissing when the schema
// is not installed.
//
// Reading through the `gsettings` tool rather than binding GSettings in
// process is deliberate. g_settings_new() on an unknown schema id aborts the
// process rather than returning an error, and extension schemas are not
// uniformly installed — so an in-process read would turn "extension not
// present" into a ChairLift crash at startup. The tool exits nonzero instead,
// which is recoverable, and it matches how every other provider in this
// codebase reaches a host tool.
func gsettingsGet(ctx context.Context, schema, key string) (string, error) {
	out, err := runCommand(ctx, "gsettings", "get", schema, key)
	if err != nil {
		if isMissingSchema(out) {
			return "", ErrExtensionMissing
		}
		return "", fmt.Errorf("livery: reading %s %s: %w: %s", schema, key, err, strings.TrimSpace(out))
	}
	return unquote(strings.TrimSpace(out)), nil
}

func gsettingsSet(ctx context.Context, schema, key, value string) error {
	out, err := runCommand(ctx, "gsettings", "set", schema, key, value)
	if err != nil {
		if isMissingSchema(out) {
			return ErrExtensionMissing
		}
		return fmt.Errorf("livery: writing %s %s: %w: %s", schema, key, err, strings.TrimSpace(out))
	}
	return nil
}

// gsettingsReset drops the user-layer value for a key, so the effective
// value falls back to whatever lower layer supplies it.
func gsettingsReset(ctx context.Context, schema, key string) error {
	out, err := runCommand(ctx, "gsettings", "reset", schema, key)
	if err != nil {
		if isMissingSchema(out) {
			return ErrExtensionMissing
		}
		return fmt.Errorf("livery: resetting %s %s: %w: %s", schema, key, err, strings.TrimSpace(out))
	}
	return nil
}

func isMissingSchema(out string) bool {
	return strings.Contains(out, "No such schema") || strings.Contains(out, "not installed")
}

// unquote converts the GVariant string literal `gsettings` prints back into
// its value.
//
// GVariant does not always quote with apostrophes. A value that itself
// contains one is printed double-quoted instead — `gsettings` reports
// /home/o'brien/mark.svg as "/home/o'brien/mark.svg" — and either form
// carries backslash escapes for control characters and for the delimiter.
// Stripping only surrounding single quotes therefore handed the free-form
// *-custom-path keys back with their quotes and escapes still attached, and
// Apply then failed on a path that does not exist with a misleading error.
// Values that are not quoted at all — booleans, numbers — pass through.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	quote := s[0]
	if quote != '\'' && quote != '"' {
		return s
	}
	if s[len(s)-1] != quote {
		return s
	}
	body := s[1 : len(s)-1]
	if !strings.ContainsRune(body, '\\') {
		return body
	}
	var b strings.Builder
	b.Grow(len(body))
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' || i+1 >= len(body) {
			b.WriteByte(c)
			continue
		}
		i++
		switch body[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		default:
			// Covers \\, \' and \" — and anything else is passed through as
			// the literal character, which is what GVariant's own parser does.
			b.WriteByte(body[i])
		}
	}
	return b.String()
}

// dconfUserValue returns the key's value in the *user* layer only, empty when
// the user has none.
//
// This distinction is the whole basis of a correct revert, and it is not
// visible through `gsettings get`, which reports the merged effective value
// across every layer. On a Bluefin host the panel icon comes from a distro
// default in /etc/dconf/db/distro.d, not from the user and not from the
// schema — so `gsettings get` answers 'ublue-logo-symbolic' while the user
// layer is empty. Restoring that string as a user value would pin the mark
// forever and silently override any future change to the distro default;
// resetting the key instead lets the distro layer answer again.
// dconfUserValue reports whether the user has set their own value for a key,
// and what it is.
//
// Getting this right took a live experiment, because the obvious primitives
// lie. `dconf read` resolves the *whole* stack — on a Bluefin host it happily
// returns 'ublue-logo-symbolic' from /etc/dconf/db/distro when the user has
// set nothing — and `dconf dump` does the same. Treating either as "what the
// user set" made revert record a distro default as a user value and then
// write it back, pinning the mark permanently and overriding any later change
// to the distro layer. That is precisely the failure this package's revert
// exists to avoid.
//
// The reliable test is current-versus-default: `dconf read -d` returns what
// the key would resolve to with the user layer removed, so a difference is a
// user override and equality is not. The case where a user has deliberately
// set the same string as the default is indistinguishable and harmless —
// resetting the key leaves them with the value they chose.
//
// known=false means the value could not be read at all, which the caller must
// not treat as "the user had nothing".
func dconfUserValue(ctx context.Context, schema, key string) (value string, known bool) {
	if _, err := exec.LookPath("dconf"); err != nil {
		return "", false
	}
	path := "/" + strings.ReplaceAll(schema, ".", "/") + "/" + key

	current, err := runCommand(ctx, "dconf", "read", path)
	if err != nil {
		return "", false
	}
	def, err := runCommand(ctx, "dconf", "read", "-d", path)
	if err != nil {
		return "", false
	}

	cur := strings.TrimSpace(current)
	if cur == strings.TrimSpace(def) {
		return "", true
	}
	return unquote(cur), true
}

// CapturePanelOverrides records the user-layer values ChairLift is about to
// replace, so ClearPanelSettings can put them back exactly.
//
// A value that is already one of ChairLift's own icon names is never captured.
// Without that guard a second Apply would record the first one's name as "the
// user's previous icon", and revert would restore a mark ChairLift had itself
// installed and then deleted. The PanelIconPrefix match is what makes ours
// identifiable, with nothing extra persisted.
//
// ok is false when the user layer could not be read; the caller must not
// treat that as "the user had nothing".
func CapturePanelOverrides(ctx context.Context) (icon, mode string, ok bool) {
	icon, iconKnown := dconfUserValue(ctx, extensionSchema, extensionIconKey)
	mode, modeKnown := dconfUserValue(ctx, extensionSchema, extensionModeKey)
	if !iconKnown || !modeKnown {
		return "", "", false
	}
	if strings.HasPrefix(icon, PanelIconPrefix) {
		icon = ""
		if mode == extensionIconMode {
			mode = ""
		}
	}
	return icon, mode, true
}

// PanelAvailable reports whether the panel mark can be set on this host.
func PanelAvailable(ctx context.Context) bool {
	_, err := gsettingsGet(ctx, extensionSchema, extensionIconKey)
	return err == nil
}

// DefaultContext returns a context with the package's standard timeout.
func DefaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), commandTimeout)
}

// pruneEmptyThemeDir removes a user theme directory ChairLift created once it
// holds no icons, so reverting leaves no trace in the icon search path.
//
// Failures are ignored on purpose: a leftover empty directory is harmless,
// and the revert it accompanies has already succeeded.
func pruneEmptyThemeDir(theme string) {
	root, err := themeDir(theme)
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(root, "symbolic", "actions"))
	_ = os.Remove(filepath.Join(root, "symbolic"))
	_ = os.Remove(filepath.Join(root, "icon-theme.cache"))
	_ = os.Remove(root)
}
