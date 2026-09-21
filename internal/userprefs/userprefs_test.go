package userprefs

import "testing"

func TestEffectivePreferencesCannotEnableUnavailableSources(t *testing.T) {
	sources := []struct {
		name         string
		setRequested func(*Values, bool)
		setAvailable func(*Availability, bool)
		get          func(Values) bool
	}{
		{"operating system", func(v *Values, b bool) { v.OperatingSystem = b }, func(a *Availability, b bool) { a.OperatingSystem = b }, func(v Values) bool { return v.OperatingSystem }},
		{"applications", func(v *Values, b bool) { v.Applications = b }, func(a *Availability, b bool) { a.Applications = b }, func(v Values) bool { return v.Applications }},
		{"developer tools", func(v *Values, b bool) { v.DeveloperTools = b }, func(a *Availability, b bool) { a.DeveloperTools = b }, func(v Values) bool { return v.DeveloperTools }},
		{"system components", func(v *Values, b bool) { v.SystemComponents = b }, func(a *Availability, b bool) { a.SystemComponents = b }, func(v Values) bool { return v.SystemComponents }},
	}

	for _, source := range sources {
		for _, requested := range []bool{false, true} {
			for _, available := range []bool{false, true} {
				values := Values{MaintenanceAfterUpdates: true}
				availability := Availability{}
				source.setRequested(&values, requested)
				source.setAvailable(&availability, available)
				got := Effective(values, availability)
				if want := requested && available; source.get(got) != want {
					t.Errorf("%s requested=%v available=%v: got %v, want %v",
						source.name, requested, available, source.get(got), want)
				}
				if !got.MaintenanceAfterUpdates {
					t.Fatal("maintenance preference changed with source availability")
				}
			}
		}
	}
}

func TestEffectivePreferencesPreservesMaintenancePreference(t *testing.T) {
	for _, maintenance := range []bool{false, true} {
		values := Values{
			OperatingSystem:         true,
			Applications:            true,
			DeveloperTools:          true,
			SystemComponents:        true,
			MaintenanceAfterUpdates: maintenance,
		}
		got := Effective(values, Availability{
			OperatingSystem:  true,
			Applications:     true,
			DeveloperTools:   true,
			SystemComponents: true,
		})
		if got.MaintenanceAfterUpdates != maintenance {
			t.Errorf("maintenance=%v: got %v", maintenance, got.MaintenanceAfterUpdates)
		}
	}
}
