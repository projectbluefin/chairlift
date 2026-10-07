package pageview

import "github.com/projectbluefin/chairlift/internal/shellextensions"

// DesktopIntegrationRow is what one Desktop integrations row shows after
// GNOME's extension lists were read.
type DesktopIntegrationRow struct {
	// Active is the switch position. It is GNOME's observed enabled state,
	// never the catalog default: an extension that is not installed, or a
	// host whose extension lists could not be read, shows off.
	Active bool
	// Sensitive is true only when the extension is installed and the read
	// succeeded, so the switch can be changed.
	Sensitive bool
	Subtitle  string
}

// DesktopIntegration maps one extension's observed state to its row. loadErr
// is the error from shellextensions.Load; states is its result.
func DesktopIntegration(extension shellextensions.Extension, states map[string]shellextensions.State, loadErr error) DesktopIntegrationRow {
	if loadErr != nil {
		return DesktopIntegrationRow{Subtitle: "Requires GNOME Shell and the gnome-extensions tool."}
	}
	state := states[extension.UUID]
	if !state.Installed {
		return DesktopIntegrationRow{Subtitle: "This GNOME extension is not installed."}
	}
	return DesktopIntegrationRow{Active: state.Enabled, Sensitive: true, Subtitle: extension.Description}
}
