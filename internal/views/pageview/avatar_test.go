package pageview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/avatar"
)

// A download is not an account update, and a face-file write is not a live
// one: each route's toast must claim exactly what happened.
func TestAvatarAppliedClaimsOnlyWhatTheRouteDid(t *testing.T) {
	entry := avatar.Avatar{CommonName: "Bob", Species: "Torosaurus latus"}
	tests := []struct {
		route      avatar.Route
		want       string
		notContain string
	}{
		{avatar.RouteBusctl, "is now your profile picture", "sign in"},
		{avatar.RouteFaceFile, "after you next sign in", "is now"},
		{avatar.RouteDryRun, "not applied", "is now"},
		{"", "not applied", "is now"},
	}
	for _, tt := range tests {
		got := AvatarApplied(tt.route, entry)
		if !strings.Contains(got, "Bob") || !strings.Contains(got, tt.want) || strings.Contains(got, tt.notContain) {
			t.Errorf("AvatarApplied(%q) = %q, want it to name Bob, contain %q, and not %q",
				tt.route, got, tt.want, tt.notContain)
		}
	}
}

// Both failure banners must tell the user the old picture is still theirs.
func TestAvatarFailuresSayThePictureWasKept(t *testing.T) {
	entry := avatar.Avatar{CommonName: "Bob"}
	for _, msg := range []string{AvatarPreviewFailed(entry), AvatarApplyFailed(entry)} {
		if !strings.Contains(msg, "Bob") || !strings.Contains(msg, "not changed") {
			t.Errorf("failure message %q must name the entry and say the picture was not changed", msg)
		}
	}
}

func TestAvatarCurrentRowPrecedence(t *testing.T) {
	entry := avatar.Avatar{CommonName: "Bob", Species: "Torosaurus latus"}
	if got := AvatarCurrentRow(true, &entry); got != (Row{Title: "Bob", Subtitle: "Torosaurus latus"}) {
		t.Errorf("an entry applied this session must name it, got %+v", got)
	}
	if got := AvatarCurrentRow(false, &entry); got.Title != "Bob" {
		t.Errorf("an applied entry wins even before its file is observed, got %+v", got)
	}
	existing, none := AvatarCurrentRow(true, nil), AvatarCurrentRow(false, nil)
	if existing == none {
		t.Errorf("an existing picture and no picture must read differently, both %+v", existing)
	}
}
