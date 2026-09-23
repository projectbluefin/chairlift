package livery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
)

// SchemaID is ChairLift's own settings schema for this page.
//
// ChairLift has exactly one GSettings schema, and this is it. The Livery
// page's selections and switches are user preferences with no file on disk to
// infer them from — unlike, say, the AI stack, whose quadlet's presence is its
// own state — so they need somewhere to live that survives a restart and a
// reboot. dconf is where GNOME desktop preferences belong. Do not add keys
// here for state that can be observed.
const SchemaID = "io.projectbluefin.chairlift.livery"

// Settings keys. Each is also declared in
// data/io.projectbluefin.chairlift.livery.gschema.xml; TestSchemaDeclaresEveryKey
// holds the two lists to each other so neither can drift.
const (
	// App Grid — "who you are". It is set once and does not rotate: a
	// personal mark that changed on its own would stop being personal.
	KeyAppGridEnabled = "app-grid-enabled"
	KeyAppGridSlug    = "app-grid-slug"
	KeyAppGridCustom  = "app-grid-custom-path"

	// Panel — "who you stand with".
	KeyPanelEnabled   = "panel-enabled"
	KeyPanelID        = "panel-foundation"
	KeyPanelCustom    = "panel-custom-path"
	KeyPanelRotate    = "panel-rotate"
	KeySavedPanelIcon = "saved-panel-icon"
	KeySavedPanelMode = "saved-panel-mode"

	// Dock — "what you roll with".
	KeyDockEnabled = "dock-enabled"
	KeyDockID      = "dock-foundation"
	KeyDockCustom  = "dock-custom-path"
	KeyDockRotate  = "dock-rotate"

	KeyRotationToken = "last-rotation-token"
)

// Keys is the complete key inventory, in schema order.
var Keys = []string{
	KeyAppGridEnabled, KeyAppGridSlug, KeyAppGridCustom,
	KeyPanelEnabled, KeyPanelID, KeyPanelCustom, KeyPanelRotate,
	KeySavedPanelIcon, KeySavedPanelMode,
	KeyDockEnabled, KeyDockID, KeyDockCustom, KeyDockRotate,
	KeyRotationToken,
}

// ErrSchemaMissing reports that the Livery settings schema is not installed.
// It happens when running from a source build without `make schemas`, and the
// page degrades to a diagnostic rather than failing.
var ErrSchemaMissing = errors.New("livery: the Livery settings schema is not installed")

// State is the persisted Livery configuration.
type State struct {
	AppGridEnabled bool
	// AppGridSlug is a Simple Icons brand name, or CustomID to use
	// AppGridCustom instead. Empty means nothing has been chosen yet.
	AppGridSlug   string
	AppGridCustom string

	PanelEnabled bool
	PanelID      string
	PanelCustom  string
	PanelRotate  bool
	// SavedPanelIcon and SavedPanelMode are the extension's *user-layer*
	// values as they were before ChairLift first wrote one, captured so
	// revert restores exactly what the user had. Empty means the user had no
	// value of their own and the key should be reset instead of written; see
	// CapturePanelOverrides.
	SavedPanelIcon string
	SavedPanelMode string

	DockEnabled bool
	DockID      string
	DockCustom  string
	DockRotate  bool

	RotationToken string
}

// AppGridSource returns the artwork source for the app-grid surface.
func (s State) AppGridSource() Source {
	if s.AppGridSlug == CustomID {
		return Source{Kind: FromFile, Value: s.AppGridCustom}
	}
	return Source{Kind: FromSimpleIcons, Value: s.AppGridSlug}
}

// PanelSource returns the artwork source for the panel surface.
func (s State) PanelSource() Source {
	if s.PanelID == CustomID {
		return Source{Kind: FromFile, Value: s.PanelCustom}
	}
	return Source{Kind: FromCatalog, Value: s.PanelID}
}

// DockSource returns the artwork source for the dock surface.
//
// The dock shows a CNCF project's own color mark, from cncf/artwork — the
// thing you roll with, not the foundation you stand with. That is a different
// catalog from the panel's, and a much larger one, which is why the picker
// searches.
func (s State) DockSource() Source {
	if s.DockID == CustomID {
		return Source{Kind: FromFile, Value: s.DockCustom}
	}
	return Source{Kind: FromCNCF, Value: s.DockID}
}

// readAll returns every key in ChairLift's schema from a single call.
//
// One `gsettings get` per key is one subprocess per key, and commit c24faa2
// deliberately moved provider probes off the GTK startup path for exactly
// that cost. `list-recursively` answers the whole schema in one spawn; its
// output is one "<schema> <key> <value>" line per key.
func readAll(ctx context.Context) (map[string]string, error) {
	out, err := runCommand(ctx, "gsettings", "list-recursively", SchemaID)
	if err != nil {
		if isMissingSchema(out) {
			return nil, ErrSchemaMissing
		}
		return nil, fmt.Errorf("livery: reading settings: %w: %s", err, strings.TrimSpace(out))
	}
	values := make(map[string]string, len(Keys))
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), " ", 3)
		if len(fields) < 3 || fields[0] != SchemaID {
			continue
		}
		values[fields[1]] = unquote(strings.TrimSpace(fields[2]))
	}
	if len(values) == 0 {
		return nil, ErrSchemaMissing
	}
	return values, nil
}

func settingsSet(ctx context.Context, key, value string) error {
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would set %s %s=%s", SchemaID, key, value)
		return nil
	}
	out, err := runCommand(ctx, "gsettings", "set", SchemaID, key, value)
	if err != nil {
		if isMissingSchema(out) {
			return ErrSchemaMissing
		}
		return fmt.Errorf("livery: writing %s: %w: %s", key, err, strings.TrimSpace(out))
	}
	return nil
}

// SetString persists a string key.
func SetString(ctx context.Context, key, value string) error {
	return settingsSet(ctx, key, strconv.Quote(value))
}

// SetBool persists a boolean key.
func SetBool(ctx context.Context, key string, value bool) error {
	return settingsSet(ctx, key, strconv.FormatBool(value))
}

// Load reads the full state.
//
// A malformed value is repaired rather than surfaced: dconf is writable by
// anything running as the user, and a selection that no longer names a
// shipped foundation should land the page on the default instead of leaving
// it blank or refusing to draw. Only a missing schema is an error the user
// has to know about, because that one they cannot fix from the page.
func Load(ctx context.Context) (State, error) {
	values, err := readAll(ctx)
	if err != nil {
		return State{}, err
	}

	// A gaming image defaults to the Open Gaming Collective's mark. This is
	// read here rather than baked in so that switching images adopts the new
	// default at the next load, without touching a selection the user made
	// for themselves.
	gaming := detectGaming()

	s := State{
		AppGridEnabled: parseBool(values[KeyAppGridEnabled]),
		AppGridSlug:    values[KeyAppGridSlug],
		AppGridCustom:  values[KeyAppGridCustom],

		PanelEnabled:   parseBool(values[KeyPanelEnabled]),
		PanelID:        resolveID(values[KeyPanelID], gaming),
		PanelCustom:    values[KeyPanelCustom],
		PanelRotate:    parseBool(values[KeyPanelRotate]),
		SavedPanelIcon: values[KeySavedPanelIcon],
		SavedPanelMode: values[KeySavedPanelMode],

		DockEnabled: parseBool(values[KeyDockEnabled]),
		DockID:      resolveCNCFID(values[KeyDockID]),
		DockCustom:  values[KeyDockCustom],
		DockRotate:  parseBool(values[KeyDockRotate]),

		RotationToken: values[KeyRotationToken],
	}
	return s, nil
}

// resolveID repairs a foundation selection, falling back to this machine's
// default for any value that is neither a shipped foundation nor the custom
// sentinel.
func resolveID(raw string, gaming bool) string {
	if raw == CustomID {
		return raw
	}
	if _, ok := Lookup(raw); !ok {
		return DefaultFoundationID(gaming)
	}
	return raw
}

// detectGaming is an injection seam for the image probe, so the gaming
// default is testable on any host rather than only on a gaming one.
//
// A host with no ublue-os descriptor is simply not a gaming image, which is
// the same answer the error path needs.
var detectGaming = func() bool {
	info, err := imageinfo.Detect()
	return err == nil && info.IsGaming()
}

// resolveCNCFID repairs a dock selection, falling back to the default
// project for a value the artwork catalog does not contain.
func resolveCNCFID(raw string) string {
	if raw == CustomID {
		return raw
	}
	if _, ok := LookupCNCF(raw); !ok {
		return DefaultCNCFID
	}
	return raw
}

// parseBool treats an unparseable value as false.
func parseBool(raw string) bool {
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return v
}
