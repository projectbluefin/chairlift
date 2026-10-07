package pageview

import (
	"strings"
	"testing"
)

// The Maintenance entry points at the detail view, so its subtitle names
// both jobs found there and no control of its own.
func TestRecoveryEntrySubtitleNamesBothJobs(t *testing.T) {
	sub := RecoveryEntrySubtitle()
	if !strings.Contains(sub, "earlier version") || !strings.Contains(sub, "reset") {
		t.Errorf("RecoveryEntrySubtitle() = %q, want it to name both going back and resetting", sub)
	}
}
