package pageview

import (
	"strings"
	"testing"
)

// Recovery is a destination the user opens deliberately, so its page copy
// must name both jobs and must not read like a routine control.
func TestRecoveryPageSubtitleNamesBothJobs(t *testing.T) {
	sub := RecoveryPageSubtitle()
	if sub == "" {
		t.Fatal("RecoveryPageSubtitle() is empty")
	}
	if !strings.Contains(sub, "previous system version") || !strings.Contains(sub, "reset") {
		t.Errorf("RecoveryPageSubtitle() = %q, want it to name both returning and resetting", sub)
	}
}

// The System page entry points at the detail view; it must not expose a
// control on the routine System page itself.
func TestRecoveryEntrySubtitlePointsAtDetailView(t *testing.T) {
	sub := RecoveryEntrySubtitle()
	if sub == "" {
		t.Fatal("RecoveryEntrySubtitle() is empty")
	}
	if !strings.Contains(sub, "Recovery") || !strings.Contains(sub, "appears") {
		t.Errorf("RecoveryEntrySubtitle() = %q, want it to name the Recovery detail and that it is conditional", sub)
	}
}

// The Recovery copy must be distinct from the System page's own copy, so the
// two never read as the same control.
func TestRecoveryCopyIsDistinctFromSystemCopy(t *testing.T) {
	if RecoveryPageSubtitle() == RecoveryEntrySubtitle() {
		t.Error("RecoveryPageSubtitle and RecoveryEntrySubtitle are identical; they should differ")
	}
}
