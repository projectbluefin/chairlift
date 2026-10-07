package pageview

import (
	"strings"
	"testing"
)

// noCommandText fails when user-facing reset text carries a command, a flag,
// or a path — none of which a person who never opens a terminal can read.
func noCommandText(t *testing.T, name, text string) {
	t.Helper()
	for _, banned := range []string{"--", "bootc", "/", "pkexec", "image", "system logs"} {
		if strings.Contains(text, banned) {
			t.Errorf("%s = %q, must not contain %q", name, text, banned)
		}
	}
}

// The row is the text a person reads before deciding. Overstating the scope
// ("everything") is the failure mode this pins: Powerwash removes Flatpak
// apps and Distrobox containers only, and the row has to say what survives
// (#525).
func TestPowerwashRowStatesItsRealScope(t *testing.T) {
	row := PowerwashRow()
	if strings.Contains(strings.ToLower(row.Title), "everything") {
		t.Errorf("PowerwashRow().Title = %q, want it not to claim it removes everything", row.Title)
	}
	for _, want := range []string{"Flatpak", "Distrobox", "Your files", "Apps page", "stay"} {
		if !strings.Contains(row.Subtitle, want) {
			t.Errorf("PowerwashRow().Subtitle = %q, want it to name %q", row.Subtitle, want)
		}
	}
	noCommandText(t, "PowerwashRow().Subtitle", row.Subtitle)
}

// A run that removed nothing must never read as a run that cleared the
// machine, and a partial failure must not read as a success.
func TestPowerwashResultSubtitleNeverOverstatesTheRun(t *testing.T) {
	all := PowerwashResultSubtitle(2, 0)
	partial := PowerwashResultSubtitle(1, 1)
	failed := PowerwashResultSubtitle(0, 2)
	nothing := PowerwashResultSubtitle(0, 0)

	if !strings.HasPrefix(all, "Removed") {
		t.Errorf("all removed = %q, want it to report the removal", all)
	}
	for name, got := range map[string]string{"partial failure": partial, "total failure": failed, "nothing installed": nothing} {
		if strings.HasPrefix(got, "Removed") {
			t.Errorf("%s = %q, must not read as a completed removal", name, got)
		}
	}
	if !strings.Contains(partial, "couldn't") || !strings.Contains(failed, "Couldn't") {
		t.Errorf("failures must say so: partial=%q failed=%q", partial, failed)
	}
	if partial == failed || failed == nothing {
		t.Errorf("each outcome needs its own text: partial=%q failed=%q nothing=%q", partial, failed, nothing)
	}
	for _, got := range []string{all, partial, failed, nothing} {
		noCommandText(t, "PowerwashResultSubtitle", got)
	}
}

// The confirmation is the one place a user learns Powerwash is irreversible
// and what it leaves alone; both facts must be present.
func TestPowerwashConfirmationStatesWhatItDoesAndDoesNot(t *testing.T) {
	title, body := PowerwashConfirmation()
	if !strings.Contains(title, "Flatpak Apps and Containers") {
		t.Errorf("PowerwashConfirmation() title = %q, want it to name the action", title)
	}
	for _, want := range []string{"Flatpak", "Distrobox", "Your files", "can't be undone"} {
		if !strings.Contains(body, want) {
			t.Errorf("PowerwashConfirmation() body = %q, want it to contain %q", body, want)
		}
	}
	noCommandText(t, "PowerwashConfirmation() body", body)
}

func TestFactoryResetRowNamesWhatItReplaces(t *testing.T) {
	row := FactoryResetRow()
	for _, want := range []string{"operating system", "Your files and apps stay", "restart"} {
		if !strings.Contains(row.Subtitle, want) {
			t.Errorf("FactoryResetRow().Subtitle = %q, want it to state %q", row.Subtitle, want)
		}
	}
	noCommandText(t, "FactoryResetRow().Subtitle", row.Subtitle)
}

// The one non-negotiable requirement: the confirmation must say the reset
// method is experimental, in words a person reads, because it is the single
// fact most likely to change their mind about proceeding. It must not do so
// by printing the command's flag.
func TestFactoryResetConfirmationDisclosesExperimental(t *testing.T) {
	title, body := FactoryResetConfirmation()
	if !strings.Contains(title, "Factory Reset") {
		t.Errorf("FactoryResetConfirmation() title = %q, want it to name the action", title)
	}
	for _, want := range []string{"experimental", "can't be undone", "restart", "Your files and apps stay"} {
		if !strings.Contains(body, want) {
			t.Errorf("FactoryResetConfirmation() body = %q, want it to contain %q", body, want)
		}
	}
	noCommandText(t, "FactoryResetConfirmation() body", body)
}

func TestFactoryResetResultDefersToARestart(t *testing.T) {
	got := FactoryResetResultSubtitle()
	if !strings.Contains(got, "Restart") {
		t.Errorf("FactoryResetResultSubtitle() = %q, want it to ask for a restart", got)
	}
}
