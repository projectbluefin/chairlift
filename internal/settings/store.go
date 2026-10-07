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
	maintenanceAfterUpdatesKey = "maintenance-after-updates"
)

// Store reads user preferences from GSettings and binds boolean settings to
// GTK object properties.
type Store struct {
	settings *gio.Settings
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
	return &Store{settings: gio.NewSettings(SchemaID)}
}

// Values returns the current user preference values, defaulting to enabled
// when the schema is not installed.
func (s *Store) Values() userprefs.Values {
	if s == nil || s.settings == nil {
		return userprefs.Values{
			OperatingSystem:         true,
			Applications:            true,
			DeveloperTools:          true,
			SystemComponents:        true,
			MaintenanceAfterUpdates: false,
		}
	}
	return userprefs.Values{
		OperatingSystem:         s.settings.GetBoolean(operatingSystemKey),
		Applications:            s.settings.GetBoolean(applicationsKey),
		DeveloperTools:          s.settings.GetBoolean(developerToolsKey),
		SystemComponents:        s.settings.GetBoolean(systemComponentsKey),
		MaintenanceAfterUpdates: s.settings.GetBoolean(maintenanceAfterUpdatesKey),
	}
}

// BindBoolean binds a boolean GSettings key to an object's active property if available.
func (s *Store) BindBoolean(key string, object *gobject.Object) {
	if s == nil || s.settings == nil {
		return
	}
	s.settings.Bind(key, object, "active", gio.GSettingsBindDefaultValue)
}

// OnSourceChanged calls fn on the GTK main thread whenever one of the four
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
		case operatingSystemKey, applicationsKey, developerToolsKey, systemComponentsKey:
			fn()
		}
	}
	s.settings.ConnectChanged(&s.changed)
	// GSettings emits ::changed only for keys read after a handler is
	// connected (the dconf backend subscribes on read), so read them now.
	s.Values()
}
