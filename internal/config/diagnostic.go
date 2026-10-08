package config

import (
	"fmt"

	"github.com/projectbluefin/chairlift/internal/branding"
)

// LogMessage renders the high-signal startup diagnostic for an authoritative
// configuration failure.
func (e *LoadError) LogMessage() string {
	return fmt.Sprintf(
		"CONFIGURATION ERROR: %s; all feature groups were disabled; fix the configuration file and restart ChairLift",
		e.Error(),
	)
}

// ToastMessage renders the persistent user-facing startup diagnostic for an
// authoritative configuration failure.
//
// Unlike LogMessage above, this reaches a user — internal/window feeds it to
// ShowErrorToast — so it names the product, not the code name, and leaves
// the file path and parser detail to the log.
func (e *LoadError) ToastMessage() string {
	return fmt.Sprintf(
		"%s's settings file has a mistake, so most features are off. Fix it, then restart %s.",
		branding.AppName, branding.AppName,
	)
}
