package pageview

import (
	"strings"
	"testing"
)

// The switch replaces the operating system, so no state may leave that
// unsaid, and none may leak the tag the machine happens to run.
func TestChannelRowExplainsEveryState(t *testing.T) {
	tests := []struct {
		name       string
		onTesting  bool
		switchable bool
		wantHas    string
	}{
		{name: "on stable, switchable", switchable: true, wantHas: "before they are fully tested"},
		{name: "on testing, switchable", onTesting: true, switchable: true, wantHas: "Turning this off"},
		{name: "nothing to switch to", wantHas: "does not offer early updates"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := ChannelRow(test.onTesting, test.switchable)
			if row.Title != "Get updates early" {
				t.Errorf("ChannelRow().Title = %q, want %q", row.Title, "Get updates early")
			}
			if !strings.Contains(row.Subtitle, test.wantHas) {
				t.Errorf("ChannelRow().Subtitle = %q, want it to contain %q", row.Subtitle, test.wantHas)
			}
		})
	}
}

// Both switchable states replace the running operating system and need a
// restart. A row that offered the choice without saying so would be asking
// for a decision on incomplete information.
func TestSwitchableChannelRowsDiscloseTheConsequence(t *testing.T) {
	for _, onTesting := range []bool{true, false} {
		row := ChannelRow(onTesting, true)
		subtitle := strings.ToLower(row.Subtitle)
		for _, want := range []string{"replaces the operating system", "restart"} {
			if !strings.Contains(subtitle, want) {
				t.Errorf("ChannelRow(%v, true).Subtitle = %q, want it to contain %q", onTesting, row.Subtitle, want)
			}
		}
	}
	if inert := ChannelRow(false, false); strings.Contains(inert.Subtitle, "restart") {
		t.Errorf("the inert subtitle %q should not mention a restart", inert.Subtitle)
	}
}

// The switch only downloads the replacement; the running system is
// unchanged until reboot. No result subtitle may imply otherwise.
func TestChannelSwitchResultAlwaysAsksForARestart(t *testing.T) {
	for _, toTesting := range []bool{true, false} {
		got := ChannelSwitchResultSubtitle(toTesting)
		if !strings.Contains(got, "restart to apply") {
			t.Errorf("ChannelSwitchResultSubtitle(%v) = %q, want it to ask for a restart", toTesting, got)
		}
	}
	if ChannelSwitchResultSubtitle(true) == ChannelSwitchResultSubtitle(false) {
		t.Error("ChannelSwitchResultSubtitle does not distinguish the two directions")
	}
}

// The row must describe the capability without naming the supplementary
// groups that carry it: a group name is an implementation detail no one
// deciding whether to switch this on can act upon.
func TestDeveloperRowNamesTheCapabilityNotTheGroups(t *testing.T) {
	for _, active := range []bool{true, false} {
		row := DeveloperRow(active)
		for _, group := range []string{"docker", "incus-admin", "libvirt", "dialout"} {
			if strings.Contains(row.Subtitle, group) {
				t.Errorf("DeveloperRow(%v).Subtitle = %q, want it not to name the group %q", active, row.Subtitle, group)
			}
		}
	}

	// Turning it on needs an administrator, which the person has to be
	// told before they press the switch, not after.
	if !strings.Contains(DeveloperRow(false).Subtitle, "administrator password") {
		t.Errorf("DeveloperRow(false).Subtitle = %q, want it to disclose the administrator password",
			DeveloperRow(false).Subtitle)
	}
	if DeveloperRow(true).Subtitle == DeveloperRow(false).Subtitle {
		t.Error("DeveloperRow does not distinguish on from off")
	}
}

// The change only applies to new login sessions, so neither outcome may read
// as immediately effective.
func TestDeveloperResultAlwaysAsksForALogout(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		got := DeveloperResultSubtitle(enabled)
		if !strings.Contains(got, "log out") {
			t.Errorf("DeveloperResultSubtitle(%v) = %q, want it to ask for a re-login", enabled, got)
		}
	}
	if DeveloperResultSubtitle(true) == DeveloperResultSubtitle(false) {
		t.Error("DeveloperResultSubtitle does not distinguish the two directions")
	}
}

func TestGamingResultReportsCountsAndFailures(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		changed int
		failed  int
		wantHas string
	}{
		{name: "installed", enabled: true, changed: 6, wantHas: "Installed 6 gaming apps"},
		{name: "removed", changed: 4, wantHas: "Removed 4 gaming apps"},
		{name: "one app", enabled: true, changed: 1, wantHas: "1 gaming app."},
		{name: "partial failure", enabled: true, changed: 4, failed: 2, wantHas: "2 could not be installed"},
		{name: "nothing to do", enabled: true, wantHas: "Nothing needed changing"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := GamingResultSubtitle(test.enabled, test.changed, test.failed)
			if !strings.Contains(got, test.wantHas) {
				t.Errorf("GamingResultSubtitle(%v, %d, %d) = %q, want it to contain %q",
					test.enabled, test.changed, test.failed, got, test.wantHas)
			}
		})
	}
}

// Each of the four installation shapes reads differently, so a component
// present in both scopes is not mistaken for a system-wide-only one, and the
// e2e scenarios can tell them apart by suffix.
func TestGamingComponentStatusNamesEveryScope(t *testing.T) {
	tests := []struct {
		user, system bool
		want         string
	}{
		{want: "Not installed"},
		{system: true, want: "Installed system-wide"},
		{user: true, want: "Installed for your account"},
		{user: true, system: true, want: "Installed system-wide and for your account"},
	}
	for _, test := range tests {
		if got := GamingComponentStatus(test.user, test.system); got != test.want {
			t.Errorf("GamingComponentStatus(%v, %v) = %q, want %q", test.user, test.system, got, test.want)
		}
	}
}
