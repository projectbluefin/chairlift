package pageview

import (
	"strings"
	"testing"
)

// W3-11: the Maintenance entry promised "reset this machine" on the shipped
// configuration (reset_group off), where the detail held only Published
// versions. The subtitle now names exactly what the detail offers.
func TestRecoveryEntrySubtitleNamesOnlyWhatTheDetailOffers(t *testing.T) {
	cases := []struct {
		name  string
		offer RecoveryOffer
		want  string
	}{
		{
			name:  "shipped default with no previous deployment",
			offer: RecoveryOffer{PublishedVersions: true},
			want:  "Browse published system versions.",
		},
		{
			name:  "rollback and published versions",
			offer: RecoveryOffer{Rollback: true, PublishedVersions: true},
			want:  "Roll back to the previous system version or browse published system versions.",
		},
		{
			name:  "reset only",
			offer: RecoveryOffer{Reset: true},
			want:  "Reset this machine.",
		},
		{
			name:  "everything",
			offer: RecoveryOffer{Rollback: true, ReturnToStream: true, PublishedVersions: true, Reset: true},
			want:  "Roll back to the previous system version, return to your release stream, browse published system versions, or reset this machine.",
		},
		{
			name:  "nothing built yet",
			offer: RecoveryOffer{},
			want:  "Nothing to roll back to or reset on this system right now.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RecoveryEntrySubtitle(tc.offer); got != tc.want {
				t.Errorf("RecoveryEntrySubtitle(%+v) = %q, want %q", tc.offer, got, tc.want)
			}
		})
	}
}

// Every combination: "reset" appears exactly when reset is offered, and
// "roll back" exactly when a rollback is.
func TestRecoveryEntrySubtitleNeverPromisesAnAbsentControl(t *testing.T) {
	for mask := range 16 {
		offer := RecoveryOffer{
			Rollback:          mask&1 != 0,
			ReturnToStream:    mask&2 != 0,
			PublishedVersions: mask&4 != 0,
			Reset:             mask&8 != 0,
		}
		sub := RecoveryEntrySubtitle(offer)
		if offer.Reset != strings.Contains(sub, "eset this machine") {
			t.Errorf("%+v: subtitle %q disagrees with Reset", offer, sub)
		}
		if offer.Rollback != strings.Contains(sub, "oll back to the previous") {
			t.Errorf("%+v: subtitle %q disagrees with Rollback", offer, sub)
		}
		if offer.ReturnToStream != strings.Contains(sub, "release stream") {
			t.Errorf("%+v: subtitle %q disagrees with ReturnToStream", offer, sub)
		}
		if offer.PublishedVersions != strings.Contains(sub, "published system versions") {
			t.Errorf("%+v: subtitle %q disagrees with PublishedVersions", offer, sub)
		}
		if !strings.HasSuffix(sub, ".") || strings.ToUpper(sub[:1]) != sub[:1] {
			t.Errorf("%+v: subtitle %q is not a capitalised sentence", offer, sub)
		}
	}
}

// Configuration and capability stay apart: an administrator's "off" is not
// reported as the host lacking support.
func TestResetAvailabilityKeepsConfigurationAndCapabilityApart(t *testing.T) {
	cases := []struct {
		configured, effective bool
		want                  ResetAvailability
	}{
		{false, false, ResetDisabledByConfig},
		{true, false, ResetUnsupported},
		{true, true, ResetOffered},
	}
	for _, tc := range cases {
		if got := ResetAvailabilityFor(tc.configured, tc.effective); got != tc.want {
			t.Errorf("ResetAvailabilityFor(%v, %v) = %v, want %v", tc.configured, tc.effective, got, tc.want)
		}
	}
}

// A detail without reset rows explains why; one with them says nothing extra,
// because the reset group carries its own description.
func TestRecoveryPageDescriptionExplainsAMissingReset(t *testing.T) {
	if got := RecoveryPageDescription(ResetOffered); got != "" {
		t.Errorf("RecoveryPageDescription(ResetOffered) = %q, want empty", got)
	}
	config := RecoveryPageDescription(ResetDisabledByConfig)
	if !strings.Contains(config, "configuration") {
		t.Errorf("RecoveryPageDescription(ResetDisabledByConfig) = %q, want it to name the configuration", config)
	}
	unsupported := RecoveryPageDescription(ResetUnsupported)
	if unsupported == "" || strings.Contains(unsupported, "configuration") {
		t.Errorf("RecoveryPageDescription(ResetUnsupported) = %q, want a non-configuration explanation", unsupported)
	}
}
