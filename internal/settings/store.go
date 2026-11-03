// Package settings adapts ChairLift's GSettings schema to the user preference
// model and GTK switch-row properties.
package settings

import (
	"github.com/projectbluefin/chairlift/internal/userprefs"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
)

// SchemaID is the installed GSettings schema identifier.
const SchemaID = "io.projectbluefin.chairlift.updates"

const (
	operatingSystemKey         = "operating-system-enabled"
	applicationsKey            = "applications-enabled"
	developerToolsKey          = "developer-tools-enabled"
	systemComponentsKey        = "system-components-enabled"
	printerDriversKey          = "printer-drivers-enabled"
	maintenanceAfterUpdatesKey = "maintenance-after-updates"
)

// Store reads user preferences from GSettings and binds boolean settings to
// GTK object properties.
type Store struct {
	settings *gio.Settings
	// keys holds the keys the installed schema defines. An installed schema
	// older than this binary lacks newer keys, and GSettings aborts the
	// process on any read or bind of an unknown key.
	keys map[string]bool
	// changed is the one GSettings::changed callback a Store connects; it
	// lives in the struct so puregotk's callback table holds one slot for
	// the Store's lifetime.
	changed func(gio.Settings, string)
}

// SchemaAvailable reports whether the updates schema is registered in the system
// or process GSettings schema cache.
func SchemaAvailable() bool {
	source := gio.SettingsSchemaSourceGetDefault()
	if source == nil {
		return false
	}
	schema := source.Lookup(SchemaID, true)
	if schema == nil {
		return false
	}
	schema.Unref()
	return true
}

// New creates a store backed by the ChairLift GSettings schema if present,
// or a no-op fallback store returning default preferences.
func New() *Store {
	if !SchemaAvailable() {
		return &Store{settings: nil}
	}
	source := gio.SettingsSchemaSourceGetDefault()
	schema := source.Lookup(SchemaID, true)
	defaults := keyDefaults()
	keys := make(map[string]bool, len(defaults))
	for key := range defaults {
		keys[key] = schema.HasKey(key)
	}
	schema.Unref()
	return &Store{settings: gio.NewSettings(SchemaID), keys: keys}
}

// keyDefaults maps every key Values reads to its no-schema default, taken
// from the fallback literal in Values so the two cannot drift.
func keyDefaults() map[string]bool {
	defaults := (*Store)(nil).Values()
	return map[string]bool{
		operatingSystemKey:         defaults.OperatingSystem,
		applicationsKey:            defaults.Applications,
		developerToolsKey:          defaults.DeveloperTools,
		systemComponentsKey:        defaults.SystemComponents,
		printerDriversKey:          defaults.PrinterDrivers,
		maintenanceAfterUpdatesKey: defaults.MaintenanceAfterUpdates,
	}
}

// Missing reports whether the schema is installed but predates key, so the
// key can be neither read nor bound.
func (s *Store) Missing(key string) bool {
	return s != nil && s.settings != nil && !s.keys[key]
}

// Values returns the current user preference values, defaulting to enabled
// when the schema, or a key in it, is not installed.
func (s *Store) Values() userprefs.Values {
	if s == nil || s.settings == nil {
		return userprefs.Values{
			OperatingSystem:         true,
			Applications:            true,
			DeveloperTools:          true,
			SystemComponents:        true,
			PrinterDrivers:          true,
			MaintenanceAfterUpdates: false,
		}
	}
	settings := keyReader{store: s}
	return userprefs.Values{
		OperatingSystem:         settings.GetBoolean(operatingSystemKey),
		Applications:            settings.GetBoolean(applicationsKey),
		DeveloperTools:          settings.GetBoolean(developerToolsKey),
		SystemComponents:        settings.GetBoolean(systemComponentsKey),
		PrinterDrivers:          settings.GetBoolean(printerDriversKey),
		MaintenanceAfterUpdates: settings.GetBoolean(maintenanceAfterUpdatesKey),
	}
}

// keyReader reads boolean keys, answering a key the installed schema lacks
// with the no-schema default from Values instead of aborting.
type keyReader struct {
	store *Store
}

func (r keyReader) GetBoolean(key string) bool {
	if r.store.Missing(key) {
		return keyDefaults()[key]
	}
	return r.store.settings.GetBoolean(key)
}

// BindBoolean binds a boolean GSettings key to an object's active property if available.
func (s *Store) BindBoolean(key string, object *gobject.Object) {
	if s == nil || s.settings == nil || s.Missing(key) {
		return
	}
	s.settings.Bind(key, object, "active", gio.GSettingsBindDefaultValue)
}

// OnSourceChanged calls fn on the GTK main thread whenever one of the
// update-source keys changes, from this process (the Preferences dialog's
// bound switches) or another (gsettings, dconf-editor). Call it once per
// Store, at build time: each call claims a puregotk callback slot. It does
// nothing when the schema is not installed, since nothing can then change.
func (s *Store) OnSourceChanged(fn func()) {
	if s == nil || s.settings == nil || fn == nil || s.changed != nil {
		return
	}
	s.changed = func(_ gio.Settings, key string) {
		switch key {
		case operatingSystemKey, applicationsKey, developerToolsKey, systemComponentsKey, printerDriversKey:
			fn()
		}
	}
	s.settings.ConnectChanged(&s.changed)
	// GSettings emits ::changed only for keys read after a handler is
	// connected (the dconf backend subscribes on read), so read them now.
	s.Values()
}
