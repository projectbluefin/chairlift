package userprefs

// Values contains the user's requested participation in each update source
// and whether successful updates should be followed by maintenance.
type Values struct {
	OperatingSystem         bool
	Applications            bool
	DeveloperTools          bool
	SystemComponents        bool
	MaintenanceAfterUpdates bool
}

// Availability contains the sources that the administrator configuration and
// runtime checks make available.
type Availability struct {
	OperatingSystem  bool
	Applications     bool
	DeveloperTools   bool
	SystemComponents bool
}

// Effective applies runtime availability to source preferences. Maintenance
// remains a user preference independent of source availability.
func Effective(values Values, available Availability) Values {
	values.OperatingSystem = values.OperatingSystem && available.OperatingSystem
	values.Applications = values.Applications && available.Applications
	values.DeveloperTools = values.DeveloperTools && available.DeveloperTools
	values.SystemComponents = values.SystemComponents && available.SystemComponents
	return values
}
