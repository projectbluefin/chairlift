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
		for _, redirect := range []string{"Unverified sources", "below"} {
			if strings.Contains(got, redirect) != available {
				t.Errorf("UpgradeMessage(group available=%v) = %q; redirect %q must only name an available section", available, got, redirect)
			}
		}
	}
}

func TestBundleMessage(t *testing.T) {
	t.Run("with specific tap", func(t *testing.T) {
		got := BundleMessage("cli", "foo/bar")
		for _, want := range []string{"Brew bundle cli", "trusting third-party taps", "brew trust foo/bar"} {
			if !strings.Contains(got, want) {
				t.Errorf("BundleMessage(\"cli\", \"foo/bar\") = %q, want it to contain %q", got, want)
			}
		}
		for _, forbid := range []string{"Untrusted Homebrew Taps", "below", "Updates"} {
			if strings.Contains(got, forbid) {
				t.Errorf("BundleMessage(\"cli\", \"foo/bar\") = %q, must not contain %q", got, forbid)
			}
		}
	})

	t.Run("without specific tap", func(t *testing.T) {
		got := BundleMessage("cli", "")
		for _, want := range []string{"Brew bundle cli", "trusting third-party taps"} {
			if !strings.Contains(got, want) {
				t.Errorf("BundleMessage(\"cli\", \"\") = %q, want it to contain %q", got, want)
			}
		}
		for _, forbid := range []string{"Untrusted Homebrew Taps", "below", "Updates", "brew trust"} {
			if strings.Contains(got, forbid) {
				t.Errorf("BundleMessage(\"cli\", \"\") = %q, must not contain %q", got, forbid)
			}
		}
	})
}
