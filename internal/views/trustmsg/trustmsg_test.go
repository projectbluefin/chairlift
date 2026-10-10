package trustmsg

import (
	"strings"
	"testing"
)

func TestUpgradeMessage(t *testing.T) {
	for _, available := range []bool{true, false} {
		got := UpgradeMessage("example", available)
		if !strings.Contains(got, "example") {
			t.Errorf("UpgradeMessage must identify the affected package: %q", got)
		}
		for _, redirect := range []string{"Manage source trust", "below"} {
			if strings.Contains(got, redirect) != available {
				t.Errorf("UpgradeMessage(group available=%v) = %q; redirect %q must only name an available section", available, got, redirect)
			}
		}
	}
}

func TestBundleMessage(t *testing.T) {
	got := BundleMessage("Coding fonts")
	if !strings.Contains(got, "Coding fonts") {
		t.Errorf("BundleMessage must name the collection: %q", got)
	}
	// The person cannot run a command from here, so the message names no
	// command, tap, or packaging term, and points at no UI section.
	for _, forbid := range []string{"brew", "tap", "Brew", "Untrusted Homebrew Taps", "below", "Updates", "/"} {
		if strings.Contains(got, forbid) {
			t.Errorf("BundleMessage = %q, must not contain %q", got, forbid)
		}
	}
}
