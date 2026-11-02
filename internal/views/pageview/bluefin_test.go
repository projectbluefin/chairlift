package pageview

import (
	"errors"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/devtools"
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
		{name: "on stable, switchable", switchable: true, wantHas: "less tested"},
		{name: "on testing, switchable", onTesting: true, switchable: true, wantHas: "less tested"},
		{name: "nothing to switch to", wantHas: "Not offered"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := ChannelRow(test.onTesting, test.switchable)
			if row.Title != "Get Updates Early" {
				t.Errorf("ChannelRow().Title = %q, want %q", row.Title, "Get Updates Early")
			}
			if !strings.Contains(row.Subtitle, test.wantHas) {
				t.Errorf("ChannelRow().Subtitle = %q, want it to contain %q", row.Subtitle, test.wantHas)
			}
		})
	}
}

// Both switchable states change the running operating system and need a
// restart. A row that offered the choice without saying so would be asking
// for a decision on incomplete information. (The Advanced group's
// description states the operating system is replaced.)
func TestSwitchableChannelRowsDiscloseTheConsequence(t *testing.T) {
	for _, onTesting := range []bool{true, false} {
		row := ChannelRow(onTesting, true)
		if !strings.Contains(strings.ToLower(row.Subtitle), "restart") {
			t.Errorf("ChannelRow(%v, true).Subtitle = %q, want it to mention a restart", onTesting, row.Subtitle)
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
		if !strings.Contains(strings.ToLower(got), "restart") {
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
		if !strings.Contains(strings.ToLower(got), "log out and back in") {
			t.Errorf("DeveloperResultSubtitle(%v) = %q, want it to ask for a re-login", enabled, got)
		}
	}
	if DeveloperResultSubtitle(true) == DeveloperResultSubtitle(false) {
		t.Error("DeveloperResultSubtitle does not distinguish the two directions")
	}
}

func TestGamingRow(t *testing.T) {
	on := GamingRow(true, 4, 0)
	if on.Title != "Gaming Mode" || !strings.Contains(on.Subtitle, "installed") {
		t.Errorf("GamingRow(true) = %+v, want title Gaming Mode and subtitle installed", on)
	}
	off := GamingRow(false, 0, 4)
	if off.Title != "Gaming Mode" || !strings.Contains(off.Subtitle, "Downloads can be large") {
		t.Errorf("GamingRow(false) = %+v, want title Gaming Mode and subtitle mentioning Downloads can be large", off)
	}
	partial := GamingRow(false, 2, 2)
	if !strings.Contains(partial.Subtitle, "Partly installed") || !strings.Contains(partial.Subtitle, "2 missing") {
		t.Errorf("GamingRow(partial) = %+v, want subtitle describing partial installation", partial)
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
		{name: "partial failure", enabled: true, changed: 4, failed: 2, wantHas: "2 couldn't be installed"},
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

func TestDockerControlPermissionFollowsObservedDirection(t *testing.T) {
	for _, test := range []struct {
		name                  string
		canEnable, canDisable bool
		whenOff, whenOn       bool
	}{
		{"neither action", false, false, false, false},
		{"enable only", true, false, true, false},
		{"disable only", false, true, false, true},
		{"both actions", true, true, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := DockerControlAllowed(false, test.canEnable, test.canDisable); got != test.whenOff {
				t.Errorf("inactive Docker operable=%v, want %v", got, test.whenOff)
			}
			if got := DockerControlAllowed(true, test.canEnable, test.canDisable); got != test.whenOn {
				t.Errorf("active Docker operable=%v, want %v", got, test.whenOn)
			}
		})
	}
}

func TestDockerSubtitleDistinguishesSystemAndClientState(t *testing.T) {
	probeErr := errors.New("socket diagnostic must not be shown")
	for _, test := range []struct {
		name  string
		state devtools.DockerState
		err   error
		want  string
	}{
		{"installable, nothing installed", devtools.DockerState{Available: true}, nil, "installs Docker from verified Homebrew packages"},
		{"installed and stopped", devtools.DockerState{Available: true, Installed: true}, nil, "turns on system Docker for all accounts"},
		{"ready", devtools.DockerState{Available: true, Installed: true, Active: true, Ready: true}, nil, "System Docker is ready for this account"},
		{"new login flag before ready", devtools.DockerState{Active: true, Ready: true, NeedsLogin: true}, probeErr, "Log out and back in"},
		{"wrapped new login", devtools.DockerState{Active: true}, errors.Join(probeErr, devtools.ErrDockerNewLogin), "Log out and back in"},
		{"running without access", devtools.DockerState{Available: true, Installed: true, Active: true, NoAccess: true}, probeErr, "your account can't use it yet. Turn this on to get access."},
		{"active failed ping", devtools.DockerState{Active: true}, probeErr, "running, but its socket is not responding"},
		{"failed units read", devtools.DockerState{}, probeErr, "Couldn't check Docker's system service"},
		{"masked paired unit", devtools.DockerState{Active: true, Unavailable: "Repair the image units."}, probeErr, "Repair the image units."},
		{"new login before unavailable", devtools.DockerState{Active: true, NeedsLogin: true, Unavailable: "Repair the image units."}, probeErr, "Log out and back in"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := DockerSubtitle(test.state, test.err)
			if !strings.Contains(got, test.want) || strings.Contains(got, probeErr.Error()) || strings.Contains(got, "rootless") {
				t.Fatalf("DockerSubtitle = %q, want %q without raw errors or rootless claims", got, test.want)
			}
			if (test.state.NeedsLogin || errors.Is(test.err, devtools.ErrDockerNewLogin)) && strings.Contains(got, "ready") {
				t.Fatalf("new login rendered as ready: %q", got)
			}
		})
	}
}

// The switch reads on whenever the daemon runs, unless this account has no
// access: an unresponsive daemon must stay stoppable, while a daemon this
// account cannot reach reads off so turning it on asks for the grant.
func TestDockerSwitchOnFollowsThisAccountsAccess(t *testing.T) {
	for _, test := range []struct {
		name  string
		state devtools.DockerState
		want  bool
	}{
		{"not installed", devtools.DockerState{Available: true}, false},
		{"installed and stopped", devtools.DockerState{Available: true, Installed: true}, false},
		{"ready", devtools.DockerState{Available: true, Installed: true, Active: true, Ready: true}, true},
		{"new login", devtools.DockerState{Available: true, Installed: true, Active: true, NeedsLogin: true}, true},
		{"running without access", devtools.DockerState{Available: true, Installed: true, Active: true, NoAccess: true}, false},
		{"running but not responding", devtools.DockerState{Available: true, Installed: true, Active: true}, true},
		{"running with paired unit missing", devtools.DockerState{Available: true, Active: true}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			on := DockerSwitchOn(test.state)
			if on != test.want {
				t.Fatalf("DockerSwitchOn(%+v) = %v, want %v", test.state, on, test.want)
			}
			// A daemon without access must dispatch enable, never disable.
			if test.state.NoAccess && (DockerControlAllowed(on, false, true) || !DockerControlAllowed(on, true, false)) {
				t.Fatalf("running without access is not routed to docker-enable")
			}
		})
	}
}

// The GUI (Homebrew) and the helper (OS image) ship separately. A helper that
// authorizes docker-enable but not docker-update predates the bottle install
// and fails after authentication on an image without Docker units ("usermod:
// group 'docker' does not exist" on Dakota 20261031 with a v26.10.4 helper).
func TestDockerEnableRequiresABottleCapableHelperWithoutImageUnits(t *testing.T) {
	missing := devtools.DockerState{Available: true}
	installed := devtools.DockerState{Available: true, Installed: true}
	masked := devtools.DockerState{Unavailable: "Repair the image units."}
	for _, test := range []struct {
		name               string
		state              devtools.DockerState
		enable, update     bool
		canEnable, outdate bool
	}{
		{"no units, old helper", missing, true, false, false, true},
		{"no units, current helper", missing, true, true, true, false},
		{"units present, old helper", installed, true, false, true, false},
		{"units present, current helper", installed, true, true, true, false},
		{"no enable action", missing, false, true, false, false},
		{"masked units", masked, true, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := DockerCanEnable(test.state, test.enable, test.update); got != test.canEnable {
				t.Errorf("DockerCanEnable = %v, want %v", got, test.canEnable)
			}
			if got := DockerHelperNeedsUpdate(test.state, test.enable, test.update); got != test.outdate {
				t.Errorf("DockerHelperNeedsUpdate = %v, want %v", got, test.outdate)
			}
		})
	}
	if !strings.Contains(DockerHelperOutdated, "Update this computer") {
		t.Fatalf("outdated-helper subtitle %q does not say how to fix it", DockerHelperOutdated)
	}
}

func TestDockerDisclosureLabelsNameHomebrewScope(t *testing.T) {
	if DockerRuntimeContents == DockerCLIContents || !strings.Contains(DockerRuntimeContents, "whole computer") || !strings.HasPrefix(DockerCLIContents, "Homebrew") {
		t.Fatalf("disclosure labels = %q / %q", DockerRuntimeContents, DockerCLIContents)
	}
}

func TestDockerEditorFailureSeparatesPreviewFromLiveAccess(t *testing.T) {
	live, preview := DockerEditorFailure(false), DockerEditorFailure(true)
	if !strings.Contains(live, "Docker access is enabled") || !strings.Contains(live, "VS Code setup couldn't finish") {
		t.Fatalf("live failure = %q", live)
	}
	if !strings.Contains(preview, "[DRY-RUN]") || !strings.Contains(preview, "no Docker changes made") || strings.Contains(preview, "access is enabled") {
		t.Fatalf("preview failure = %q", preview)
	}
}
