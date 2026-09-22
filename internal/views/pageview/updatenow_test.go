package pageview

import (
	"strings"
	"testing"
)

// The row stands in for a privileged run that can pull gigabytes, so both
// costs have to be visible before the user presses anything: the
// administrator password and the size of the download.
func TestUpdateNowRowDisclosesItsCosts(t *testing.T) {
	row := UpdateNowRow()

	if row.Title == "" {
		t.Error("UpdateNowRow() has no title")
	}
	subtitle := strings.ToLower(row.Subtitle)
	for _, disclosure := range []string{"administrator password", "large download"} {
		if !strings.Contains(subtitle, disclosure) {
			t.Errorf("UpdateNowRow() subtitle %q does not disclose %q", row.Subtitle, disclosure)
		}
	}
}

// The updater is an implementation detail of the image, not a thing a person
// chose to install. Naming it — or the unit that normally runs it — asks the
// reader to know the plumbing before they can decide.
func TestUpdateNowCopyNamesNoTooling(t *testing.T) {
	texts := map[string]string{
		"row title":         UpdateNowRow().Title,
		"row subtitle":      UpdateNowRow().Subtitle,
		"running subtitle":  UpdateNowRunningSubtitle(),
		"finished subtitle": UpdateNowResultSubtitle(),
	}

	for where, text := range texts {
		lower := strings.ToLower(text)
		for _, forbidden := range []string{"uupd", "systemd", "timer", "pkexec", "polkit", "bootc"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s names %q: %q", where, forbidden, text)
			}
		}
	}
}

// A new system version only takes effect at the next start, but apps and
// packages are already updated when the run ends. The finished subtitle must
// therefore offer a restart rather than report one as required.
func TestUpdateNowResultOffersARestartConditionally(t *testing.T) {
	finished := strings.ToLower(UpdateNowResultSubtitle())

	if !strings.Contains(finished, "restart") {
		t.Errorf("finished subtitle %q never mentions restarting", UpdateNowResultSubtitle())
	}
	if !strings.Contains(finished, "if") {
		t.Errorf("finished subtitle %q states a restart unconditionally", UpdateNowResultSubtitle())
	}
}
