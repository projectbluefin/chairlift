package pageview

import (
	"strings"
	"testing"
)

// The row is the text a person reads before deciding. Overstating the scope
// ("everything") is the failure mode this pins: Powerwash leaves files,
// settings, and the system image alone, and the row has to say so.
func TestPowerwashRowStatesItsRealScope(t *testing.T) {
	row := PowerwashRow()
	if strings.Contains(strings.ToLower(row.Title), "everything") {
		t.Errorf("PowerwashRow().Title = %q, want it not to claim it removes everything", row.Title)
	}
	for _, want := range []string{"apps you installed", "containers"} {
		if !strings.Contains(row.Subtitle, want) {
			t.Errorf("PowerwashRow().Subtitle = %q, want it to name %q", row.Subtitle, want)
		}
	}
	if !strings.Contains(row.Subtitle, "stay as they are") {
		t.Errorf("PowerwashRow().Subtitle = %q, want it to say what survives", row.Subtitle)
	}
}

// A run that removed nothing must never read as a run that cleared the
// machine, and a partial failure must not read as a success.
func TestPowerwashResultSubtitleNeverOverstatesTheRun(t *testing.T) {
	tests := []struct {
		name      string
		succeeded int
		failed    int
		want      string
	}{
		{"all removed", 2, 0, "Removed the apps you installed"},
		{"partial failure", 1, 1, "Some apps could not be removed — see the system logs for details"},
		{"total failure", 0, 2, "Nothing could be removed — see the system logs for details"},
		{"nothing installed", 0, 0, "There was nothing installed to remove"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PowerwashResultSubtitle(tt.succeeded, tt.failed); got != tt.want {
				t.Errorf("PowerwashResultSubtitle(%d, %d) = %q, want %q",
					tt.succeeded, tt.failed, got, tt.want)
			}
		})
	}
}

// The confirmation is the one place a user learns Powerwash is irreversible
// and does not touch the image; both facts must be present.
func TestPowerwashConfirmationStatesWhatItDoesAndDoesNot(t *testing.T) {
	title, body := PowerwashConfirmation()
	if !strings.Contains(title, "Remove Everything") {
		t.Errorf("PowerwashConfirmation() title = %q, want it to name the action", title)
	}
	for _, want := range []string{"Flatpak", "Distrobox", "cannot be undone"} {
		if !strings.Contains(body, want) {
			t.Errorf("PowerwashConfirmation() body = %q, want it to contain %q", body, want)
		}
	}
}

func TestFactoryResetRowNamesWhatItReplaces(t *testing.T) {
	row := FactoryResetRow()
	if row.Title != "Reset the system" {
		t.Errorf("FactoryResetRow().Title = %q, want %q", row.Title, "Reset the system")
	}
	for _, want := range []string{"Reinstalls the system", "Your files and apps stay", "restart"} {
		if !strings.Contains(row.Subtitle, want) {
			t.Errorf("FactoryResetRow().Subtitle = %q, want it to state %q", row.Subtitle, want)
		}
	}
}

// The one non-negotiable requirement: --experimental must be named in the
// confirmation, not hidden behind friendlier wording, because it is the
// single fact most likely to change a user's mind about proceeding.
func TestFactoryResetConfirmationNamesExperimental(t *testing.T) {
	title, body := FactoryResetConfirmation()
	if !strings.Contains(title, "Factory Reset") {
		t.Errorf("FactoryResetConfirmation() title = %q, want it to name the action", title)
	}
	if !strings.Contains(body, "--experimental") {
		t.Errorf("FactoryResetConfirmation() body = %q, want it to name --experimental", body)
	}
	if !strings.Contains(body, "cannot be undone") {
		t.Errorf("FactoryResetConfirmation() body = %q, want it to state irreversibility", body)
	}
}

func TestFactoryResetResultDefersToARestart(t *testing.T) {
	got := FactoryResetResultSubtitle()
	if !strings.Contains(got, "restart") {
		t.Errorf("FactoryResetResultSubtitle() = %q, want it to ask for a restart", got)
	}
}
