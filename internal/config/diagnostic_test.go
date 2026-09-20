package config

import (
	"errors"
	"testing"

	"github.com/projectbluefin/chairlift/internal/branding"
)

func TestLoadErrorDiagnosticMessages(t *testing.T) {
	loadErr := &LoadError{
		Path:   "/etc/chairlift/config.yml",
		Kind:   KindRead,
		Detail: "reading configuration file",
		Err:    errors.New("permission denied"),
	}

	const wantLog = "CONFIGURATION ERROR: config read error: /etc/chairlift/config.yml: reading configuration file: permission denied; all feature groups were disabled; fix the configuration file and restart ChairLift"
	if got := loadErr.LogMessage(); got != wantLog {
		t.Fatalf("LogMessage() = %q, want %q", got, wantLog)
	}

	// The toast is user-facing, so it names the product; the log line above
	// keeps the code name. That asymmetry is the point of the assertion.
	wantToast := "Configuration error: config read error: /etc/chairlift/config.yml: reading configuration file: permission denied. All feature groups are disabled. Fix the configuration file and restart " + branding.AppName + "."
	if got := loadErr.ToastMessage(); got != wantToast {
		t.Fatalf("ToastMessage() = %q, want %q", got, wantToast)
	}
}
