package actionmsg

import (
	"strings"
	"testing"
)

// Dry-run must never confirm a switch: the underlying helper call is
// short-circuited before pkexec, so nothing changed.
func TestChannelSwitchNeverConfirmsUnderDryRun(t *testing.T) {
	for _, toTesting := range []bool{true, false} {
		decision := ChannelSwitch(true, toTesting)
		if decision.Confirm {
			t.Errorf("ChannelSwitch(true, %v).Confirm = true, want false", toTesting)
		}
		if !strings.Contains(decision.Toast, "[DRY-RUN]") {
			t.Errorf("ChannelSwitch(true, %v).Toast = %q, want a dry-run preview", toTesting, decision.Toast)
		}
	}
}

func TestChannelSwitchConfirmsAndNamesTheChannel(t *testing.T) {
	testing_ := ChannelSwitch(false, true)
	if !testing_.Confirm {
		t.Error("ChannelSwitch(false, true).Confirm = false, want true")
	}
	if !strings.Contains(testing_.Toast, "testing") || !strings.Contains(testing_.Toast, "Restart") {
		t.Errorf("ChannelSwitch(false, true).Toast = %q, want it to name testing and ask for a restart", testing_.Toast)
	}

	stable := ChannelSwitch(false, false)
	if !strings.Contains(stable.Toast, "stable") {
		t.Errorf("ChannelSwitch(false, false).Toast = %q, want it to name stable", stable.Toast)
	}
}

func TestDeveloperModeNeverConfirmsUnderDryRun(t *testing.T) {
	for _, enable := range []bool{true, false} {
		decision := DeveloperMode(true, enable, []string{"docker"})
		if decision.Confirm {
			t.Errorf("DeveloperMode(true, %v).Confirm = true, want false", enable)
		}
		if !strings.Contains(decision.Toast, "[DRY-RUN]") {
			t.Errorf("DeveloperMode(true, %v).Toast = %q, want a dry-run preview", enable, decision.Toast)
		}
	}
}

func TestDeveloperModeLiveToastsAskForARelogin(t *testing.T) {
	for _, enable := range []bool{true, false} {
		decision := DeveloperMode(false, enable, nil)
		if !decision.Confirm {
			t.Errorf("DeveloperMode(false, %v).Confirm = false, want true", enable)
		}
		if !strings.Contains(decision.Toast, "Log out") {
			t.Errorf("DeveloperMode(false, %v).Toast = %q, want it to ask for a re-login", enable, decision.Toast)
		}
	}
}

// Issue #495: an enable whose helper skipped groups still confirms and names
// each group not granted, so it does not read as a complete grant.
func TestDeveloperModeNamesSkippedGroups(t *testing.T) {
	decision := DeveloperMode(false, true, []string{"docker", "incus-admin"})
	if !decision.Confirm {
		t.Errorf("DeveloperMode(partial enable) = %+v, want it confirmed", decision)
	}
	for _, group := range []string{"docker", "incus-admin"} {
		if !strings.Contains(decision.Toast, group) {
			t.Errorf("DeveloperMode(partial enable).Toast = %q, want it to name %s", decision.Toast, group)
		}
	}
	// ShowToast renders a single ellipsized line; keep the Dakota note no
	// longer than the plain enable toast plus the two group names, so the
	// names are not the part that gets cut off.
	if len(decision.Toast) > 72 {
		t.Errorf("DeveloperMode(partial enable).Toast = %q (%d chars), want at most 72 so it fits a plain toast", decision.Toast, len(decision.Toast))
	}
	if !strings.Contains(decision.Toast, "Log out") {
		t.Errorf("DeveloperMode(partial enable).Toast = %q, want it to ask for a re-login", decision.Toast)
	}
	if disable := DeveloperMode(false, false, []string{"docker"}); strings.Contains(disable.Toast, "docker") {
		t.Errorf("DeveloperMode(disable, skipped).Toast = %q, want no skipped-group note", disable.Toast)
	}
}

// Gaming mode is the one toggle whose live run can partly or wholly fail,
// so Confirm is not simply !dryRun.
func TestGamingModeConfirmsOnlyWhenSomethingChanged(t *testing.T) {
	tests := []struct {
		name        string
		dryRun      bool
		enable      bool
		changed     int
		failed      int
		skipped     int
		wantConfirm bool
		wantToast   string
	}{
		{name: "dry run", dryRun: true, enable: true, changed: 6, wantToast: "[DRY-RUN] Preview: gaming components would be installed"},
		{name: "dry run removal", dryRun: true, changed: 6, wantToast: "[DRY-RUN] Preview: gaming components would be removed"},
		// Issue #352: every component is system-wide, so a preview must not
		// promise a removal the live run would not make.
		{name: "dry run with only system-wide components", dryRun: true, skipped: 6, wantToast: "[DRY-RUN] Preview: nothing to remove — 6 component(s) installed system-wide would be left in place"},
		{name: "dry run partial removal names the skipped ones", dryRun: true, changed: 3, skipped: 1, wantToast: "1 component(s) installed system-wide would be left in place"},
		{name: "dry run with nothing to do", dryRun: true, enable: true, wantToast: "already in the requested state"},
		{name: "dry run that could not read the inventory", dryRun: true, enable: true, failed: 1, wantToast: "could be installed (1 failed)"},
		{name: "full install", enable: true, changed: 6, wantConfirm: true, wantToast: "6 gaming component(s) installed"},
		{name: "full removal", changed: 6, wantConfirm: true, wantToast: "6 gaming component(s) removed"},
		{name: "partial install still confirms", enable: true, changed: 4, failed: 2, wantConfirm: true, wantToast: "2 failed"},
		{name: "total failure does not confirm", enable: true, changed: 0, failed: 6, wantConfirm: false, wantToast: "No gaming components could be installed"},
		{name: "nothing to do still confirms", enable: true, changed: 0, wantConfirm: true, wantToast: "already in the requested state"},
		// Everything present belongs to the system image, so removal is a
		// no-op and the switch must not claim gaming mode is now off.
		{name: "only system-wide components", changed: 0, skipped: 2, wantConfirm: false, wantToast: "left in place"},
		{name: "partial removal names the skipped ones", changed: 3, skipped: 1, wantConfirm: true, wantToast: "left in place"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := GamingMode(test.dryRun, test.enable, test.changed, test.failed, test.skipped)
			if decision.Confirm != test.wantConfirm {
				t.Errorf("GamingMode(%v, %v, %d, %d, %d).Confirm = %v, want %v",
					test.dryRun, test.enable, test.changed, test.failed, test.skipped, decision.Confirm, test.wantConfirm)
			}
			if !strings.Contains(decision.Toast, test.wantToast) {
				t.Errorf("GamingMode(...).Toast = %q, want it to contain %q", decision.Toast, test.wantToast)
			}
		})
	}
}

// Under dry-run nothing was reordered, so the row must not claim the
// previous image will boot next.
func TestRollbackNeverConfirmsUnderDryRun(t *testing.T) {
	decision := Rollback(true)
	if decision.Confirm {
		t.Error("Rollback(true).Confirm = true, want false")
	}
	if !strings.Contains(decision.Toast, "[DRY-RUN]") {
		t.Errorf("Rollback(true).Toast = %q, want a dry-run preview", decision.Toast)
	}
}

func TestRollbackLiveToastAsksForARestart(t *testing.T) {
	decision := Rollback(false)
	if !decision.Confirm {
		t.Error("Rollback(false).Confirm = false, want true")
	}
	if !strings.Contains(decision.Toast, "Restart") {
		t.Errorf("Rollback(false).Toast = %q, want it to ask for a restart", decision.Toast)
	}
}

// Under dry-run the timer was never touched, so confirming would leave the
// switch disagreeing with systemd.
func TestAutomaticUpdatesNeverConfirmsUnderDryRun(t *testing.T) {
	for _, enable := range []bool{true, false} {
		decision := AutomaticUpdates(true, enable)
		if decision.Confirm {
			t.Errorf("AutomaticUpdates(true, %v).Confirm = true, want false", enable)
		}
		if !strings.Contains(decision.Toast, "[DRY-RUN]") {
			t.Errorf("AutomaticUpdates(true, %v).Toast = %q, want a dry-run preview", enable, decision.Toast)
		}
	}
}

func TestAutomaticUpdatesLiveToastsNameTheDirection(t *testing.T) {
	on := AutomaticUpdates(false, true)
	off := AutomaticUpdates(false, false)

	for _, decision := range []FeatureToggleDecision{on, off} {
		if !decision.Confirm {
			t.Error("AutomaticUpdates(false, _).Confirm = false, want true")
		}
	}
	if !strings.Contains(on.Toast, "on") || !strings.Contains(off.Toast, "off") {
		t.Errorf("AutomaticUpdates toasts do not name the direction: on=%q off=%q", on.Toast, off.Toast)
	}
}

func TestDriverSwitchNeverConfirmsUnderDryRun(t *testing.T) {
	decision := DriverSwitch(true, "NVIDIA (proprietary)")
	if decision.Confirm {
		t.Error("DriverSwitch(true, _).Confirm = true, want false")
	}
	if !strings.Contains(decision.Toast, "[DRY-RUN]") {
		t.Errorf("DriverSwitch(true, _).Toast = %q, want a dry-run preview", decision.Toast)
	}
}

func TestDriverSwitchLiveToastNamesTheImageAndRestart(t *testing.T) {
	decision := DriverSwitch(false, "NVIDIA (proprietary)")
	if !decision.Confirm {
		t.Error("DriverSwitch(false, _).Confirm = false, want true")
	}
	if !strings.Contains(decision.Toast, "NVIDIA (proprietary)") {
		t.Errorf("DriverSwitch toast %q does not name the image", decision.Toast)
	}
	if !strings.Contains(decision.Toast, "Restart") {
		t.Errorf("DriverSwitch toast %q does not ask for a restart", decision.Toast)
	}
}

func TestFactoryResetNeverConfirmsUnderDryRun(t *testing.T) {
	decision := FactoryReset(true)
	if decision.Confirm {
		t.Error("FactoryReset(true).Confirm = true, want false")
	}
	if !strings.Contains(decision.Toast, "[DRY-RUN]") {
		t.Errorf("FactoryReset(true).Toast = %q, want a dry-run preview", decision.Toast)
	}
}

func TestFactoryResetLiveToastAsksForARestart(t *testing.T) {
	decision := FactoryReset(false)
	if !decision.Confirm {
		t.Error("FactoryReset(false).Confirm = false, want true")
	}
	if !strings.Contains(decision.Toast, "Restart") {
		t.Errorf("FactoryReset(false).Toast = %q, want it to ask for a restart", decision.Toast)
	}
}

func TestPinBuildNeverConfirmsUnderDryRun(t *testing.T) {
	decision := PinBuild(true, "20260920")
	if decision.Confirm {
		t.Error("PinBuild(true).Confirm = true, want false")
	}
	if !strings.Contains(decision.Toast, "[DRY-RUN]") || !strings.Contains(decision.Toast, "20260920") {
		t.Errorf("PinBuild(true).Toast = %q, want dry-run preview with day", decision.Toast)
	}
}

func TestPinBuildLiveToastAsksForARestart(t *testing.T) {
	decision := PinBuild(false, "20260920")
	if !decision.Confirm {
		t.Error("PinBuild(false).Confirm = false, want true")
	}
	if !strings.Contains(decision.Toast, "Restart") {
		t.Errorf("PinBuild(false).Toast = %q, want restart request", decision.Toast)
	}
}

func TestReturnToStreamNeverConfirmsUnderDryRun(t *testing.T) {
	decision := ReturnToStream(true)
	if decision.Confirm {
		t.Error("ReturnToStream(true).Confirm = true, want false")
	}
	if !strings.Contains(decision.Toast, "[DRY-RUN]") {
		t.Errorf("ReturnToStream(true).Toast = %q, want dry-run preview", decision.Toast)
	}
}

func TestReturnToStreamLiveToastAsksForARestart(t *testing.T) {
	decision := ReturnToStream(false)
	if !decision.Confirm {
		t.Error("ReturnToStream(false).Confirm = false, want true")
	}
	if !strings.Contains(decision.Toast, "Restart") {
		t.Errorf("ReturnToStream(false).Toast = %q, want restart request", decision.Toast)
	}
}

func TestPowerwashConfirmsOnlyWhenSomethingWasRemoved(t *testing.T) {
	tests := []struct {
		name        string
		dryRun      bool
		succeeded   int
		failed      int
		wantConfirm bool
		wantToast   string
	}{
		{name: "dry run", dryRun: true, wantToast: "[DRY-RUN]"},
		{name: "both succeeded", succeeded: 2, wantConfirm: true, wantToast: "complete"},
		{name: "nothing installed", wantConfirm: true, wantToast: "Nothing was installed"},
		{name: "one failed, one succeeded", succeeded: 1, failed: 1, wantConfirm: true, wantToast: "problems"},
		{name: "total failure does not confirm", failed: 2, wantConfirm: false, wantToast: "nothing was removed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Powerwash(test.dryRun, test.succeeded, test.failed)
			if decision.Confirm != test.wantConfirm {
				t.Errorf("Powerwash(%v, %d, %d).Confirm = %v, want %v",
					test.dryRun, test.succeeded, test.failed, decision.Confirm, test.wantConfirm)
			}
			if !strings.Contains(decision.Toast, test.wantToast) {
				t.Errorf("Powerwash(...).Toast = %q, want it to contain %q", decision.Toast, test.wantToast)
			}
		})
	}
}

func TestAgentModeDryRunDoesNotConfirm(t *testing.T) {
	for _, enable := range []bool{true, false} {
		decision := AgentMode(true, enable)
		if decision.Confirm || !strings.Contains(decision.Toast, "DRY-RUN") {
			t.Errorf("dry run enable=%v: %+v", enable, decision)
		}
	}
}

func TestAgentModeDisableSaysModelsWereKept(t *testing.T) {
	decision := AgentMode(false, false)
	if !decision.Confirm || !strings.Contains(decision.Toast, "kept") {
		t.Errorf("live disable: %+v", decision)
	}
}

func TestPrinterAppDryRunDoesNotConfirmAndNamesTheRow(t *testing.T) {
	for _, enable := range []bool{true, false} {
		decision := PrinterApp(true, enable, "HP printers (HPLIP)")
		if decision.Confirm || !strings.HasPrefix(decision.Toast, "[DRY-RUN] Preview: HP printers (HPLIP) would be") || !strings.HasSuffix(decision.Toast, "— no changes made") {
			t.Errorf("dry run enable=%v: %+v", enable, decision)
		}
	}
}

func TestPrinterAppLiveConfirmsAndDisableSaysSettingsWereKept(t *testing.T) {
	on := PrinterApp(false, true, "Gutenprint printers")
	if !on.Confirm || on.Toast != "Gutenprint printers is on." {
		t.Errorf("live enable: %+v", on)
	}
	off := PrinterApp(false, false, "Gutenprint printers")
	if !off.Confirm || !strings.HasPrefix(off.Toast, "Gutenprint printers is off") || !strings.Contains(off.Toast, "kept") {
		t.Errorf("live disable: %+v", off)
	}
}
