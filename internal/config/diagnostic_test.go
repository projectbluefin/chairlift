package config

import (
	"errors"
	"strings"
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
	// keeps the code name and the detail. The toast must carry neither the
	// path nor the parser's error text.
	toast := loadErr.ToastMessage()
	for _, want := range []string{branding.AppName, "restart"} {
		if !strings.Contains(toast, want) {
			t.Errorf("ToastMessage() = %q, want it to contain %q", toast, want)
		}
	}
	for _, leak := range []string{"/etc/chairlift", "permission denied", "ChairLift"} {
		if strings.Contains(toast, leak) {
			t.Errorf("ToastMessage() = %q leaks %q", toast, leak)
		}
	}
}
