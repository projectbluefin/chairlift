// Package userprefs is the pure model of the user's update preferences, read
// from the io.projectbluefin.chairlift.updates GSettings schema by
// internal/settings and handed to internal/updateflow's Coordinator. It holds
// requested participation only: whether a source is actually available on
// this host is the coordinator's decision, made from each source's
// updateflow.Policy, so nothing here masks a preference against availability.
package userprefs

// Values contains the user's requested participation in each update source
// and whether successful updates should be followed by maintenance.
type Values struct {
	OperatingSystem         bool
	Applications            bool
	DeveloperTools          bool
	SystemComponents        bool
	Printers                bool
	MaintenanceAfterUpdates bool
}
