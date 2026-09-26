package pageview

import (
	"strings"
	"testing"
)

// TestFlatpakUninstallConfirmationNamesTheAppAndItsScope holds the facts a
// user needs before confirming: which app goes, and whether it goes only from
// their account or from everyone's.
func TestFlatpakUninstallConfirmationNamesTheAppAndItsScope(t *testing.T) {
	userTitle, userBody := FlatpakUninstallConfirmation("Firefox", true)
	systemTitle, systemBody := FlatpakUninstallConfirmation("Text Editor", false)

	if userTitle != "Uninstall Firefox?" {
		t.Errorf("user title = %q, want %q", userTitle, "Uninstall Firefox?")
	}
	if systemTitle != "Uninstall Text Editor?" {
		t.Errorf("system title = %q, want %q", systemTitle, "Uninstall Text Editor?")
	}
	if !strings.Contains(userBody, "Firefox") || !strings.Contains(userBody, "your account") {
		t.Errorf("user body = %q, want it to name the app and the caller's account", userBody)
	}
	if strings.Contains(userBody, "everyone") {
		t.Errorf("user body = %q, must not claim a removal for everyone", userBody)
	}
	if !strings.Contains(systemBody, "Text Editor") || !strings.Contains(systemBody, "everyone") {
		t.Errorf("system body = %q, want it to name the app and that it goes for everyone", systemBody)
	}
}
