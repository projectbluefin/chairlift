package actionmsg

import (
	"strings"
	"testing"
)

// TestBundleDump covers both dry-run states for the package-list export
// toast, and that neither names the file it writes.
func TestBundleDump(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		wantContains []string
		wantExact    string
	}{
		{
			name:      "live run confirms the export in plain words",
			dryRun:    false,
			wantExact: "Package list exported to your home folder",
		},
		{
			name:         "dry-run previews without claiming a save happened",
			dryRun:       true,
			wantContains: []string{"[DRY-RUN]", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BundleDump(tt.dryRun)

			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("BundleDump(%v) = %q, want %q", tt.dryRun, got, tt.wantExact)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("BundleDump(%v) = %q, want it to contain %q", tt.dryRun, got, want)
				}
			}
			for _, banned := range []string{"Brewfile", "/", "~"} {
				if strings.Contains(got, banned) {
					t.Errorf("BundleDump(%v) = %q, must not contain %q", tt.dryRun, got, banned)
				}
			}
		})
	}
}

func TestBundleInstallDecision(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		wantComplete bool
		wantExact    string
		wantContains []string
	}{
		{
			name:         "live install completes the row",
			wantComplete: true,
			wantExact:    "Command line tools installed",
		},
		{
			name:         "dry-run resets the row after a preview",
			dryRun:       true,
			wantComplete: false,
			wantContains: []string{"[DRY-RUN]", "Command line tools", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BundleInstall(tt.dryRun, "Command line tools")
			if got.Complete != tt.wantComplete {
				t.Errorf("BundleInstall(%v, …).Complete = %v, want %v", tt.dryRun, got.Complete, tt.wantComplete)
			}
			if tt.wantExact != "" && got.Toast != tt.wantExact {
				t.Errorf("BundleInstall(%v, …).Toast = %q, want %q", tt.dryRun, got.Toast, tt.wantExact)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got.Toast, want) {
					t.Errorf("BundleInstall(%v, …).Toast = %q, want it to contain %q", tt.dryRun, got.Toast, want)
				}
			}
		})
	}
}

// TestPackageInstallMessage covers both dry-run states for the Homebrew
// package-install toast text.
func TestPackageInstallMessage(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		pkgName      string
		wantExact    string
		wantContains []string
	}{
		{
			name:      "live run reports the package as installed",
			dryRun:    false,
			pkgName:   "ripgrep",
			wantExact: "ripgrep installed",
		},
		{
			name:         "dry-run previews without claiming an install happened",
			dryRun:       true,
			pkgName:      "ripgrep",
			wantContains: []string{"[DRY-RUN]", "ripgrep", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Install(tt.dryRun, tt.pkgName)

			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("Install(%v, %q) = %q, want %q", tt.dryRun, tt.pkgName, got, tt.wantExact)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("Install(%v, %q) = %q, want it to contain %q", tt.dryRun, tt.pkgName, got, want)
				}
			}
		})
	}
}

// TestUninstall covers both dry-run states for shared Homebrew/Flatpak
// uninstall toast text.
func TestUninstall(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		appID        string
		wantExact    string
		wantContains []string
	}{
		{
			name:      "live run reports the app as uninstalled",
			dryRun:    false,
			appID:     "org.mozilla.firefox",
			wantExact: "org.mozilla.firefox uninstalled",
		},
		{
			name:         "dry-run previews without claiming an uninstall happened",
			dryRun:       true,
			appID:        "org.mozilla.firefox",
			wantContains: []string{"[DRY-RUN]", "org.mozilla.firefox", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Uninstall(tt.dryRun, tt.appID)

			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("Uninstall(%v, %q) = %q, want %q", tt.dryRun, tt.appID, got, tt.wantExact)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("Uninstall(%v, %q) = %q, want it to contain %q", tt.dryRun, tt.appID, got, want)
				}
			}
		})
	}
}

func TestPin(t *testing.T) {
	tests := []struct {
		name      string
		dryRun    bool
		pin       bool
		wantExact string
	}{
		{
			name:      "live pin",
			pin:       true,
			wantExact: "ripgrep pinned",
		},
		{
			name:      "live unpin",
			wantExact: "ripgrep unpinned",
		},
		{
			name:      "dry-run pin",
			dryRun:    true,
			pin:       true,
			wantExact: "[DRY-RUN] Preview: ripgrep would be pinned — no changes made",
		},
		{
			name:      "dry-run unpin",
			dryRun:    true,
			wantExact: "[DRY-RUN] Preview: ripgrep would be unpinned — no changes made",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Pin(tt.dryRun, "ripgrep", tt.pin); got != tt.wantExact {
				t.Errorf("Pin(%v, %q, %v) = %q, want %q", tt.dryRun, "ripgrep", tt.pin, got, tt.wantExact)
			}
		})
	}
}

// TestUpgrade covers both dry-run states for the per-package Homebrew
// upgrade toast text.
func TestUpgrade(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		pkgName      string
		wantExact    string
		wantContains []string
	}{
		{
			name:      "live run reports the package as upgraded",
			dryRun:    false,
			pkgName:   "ripgrep",
			wantExact: "ripgrep upgraded",
		},
		{
			name:         "dry-run previews without claiming an upgrade happened",
			dryRun:       true,
			pkgName:      "ripgrep",
			wantContains: []string{"[DRY-RUN]", "ripgrep", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Upgrade(tt.dryRun, tt.pkgName)

			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("Upgrade(%v, %q) = %q, want %q", tt.dryRun, tt.pkgName, got, tt.wantExact)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("Upgrade(%v, %q) = %q, want it to contain %q", tt.dryRun, tt.pkgName, got, want)
				}
			}
		})
	}
}

// TestUpdate covers both dry-run states for the per-app Flatpak update toast
// text.
func TestUpdate(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		appID        string
		wantExact    string
		wantContains []string
	}{
		{
			name:      "live run reports the app as updated",
			dryRun:    false,
			appID:     "org.mozilla.firefox",
			wantExact: "org.mozilla.firefox updated",
		},
		{
			name:         "dry-run previews without claiming an update happened",
			dryRun:       true,
			appID:        "org.mozilla.firefox",
			wantContains: []string{"[DRY-RUN]", "org.mozilla.firefox", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Update(tt.dryRun, tt.appID)

			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("Update(%v, %q) = %q, want %q", tt.dryRun, tt.appID, got, tt.wantExact)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("Update(%v, %q) = %q, want it to contain %q", tt.dryRun, tt.appID, got, want)
				}
			}
		})
	}
}

// TestSelfUpdate covers both dry-run states for a package manager
// self-update toast text (e.g. Homebrew's own `brew update`).
func TestSelfUpdate(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		tool         string
		wantExact    string
		wantContains []string
	}{
		{
			name:      "live run reports fixed completion message",
			dryRun:    false,
			tool:      "Homebrew",
			wantExact: "Homebrew updated successfully",
		},
		{
			name:         "dry-run previews without claiming an update happened",
			dryRun:       true,
			tool:         "Homebrew",
			wantContains: []string{"[DRY-RUN]", "Homebrew", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelfUpdate(tt.dryRun, tt.tool)

			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("SelfUpdate(%v, %q) = %q, want %q", tt.dryRun, tt.tool, got, tt.wantExact)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("SelfUpdate(%v, %q) = %q, want it to contain %q", tt.dryRun, tt.tool, got, want)
				}
			}
		})
	}
}

// TestSystemStage covers all four (dryRun, staged) combinations for both
// staging providers. Dry-run cannot claim that a status re-read is work done
// by this click, while live staging retains its fixed completion messages.
func TestSystemStage(t *testing.T) {
	const stagedMsg = "System update staged. Restart to apply."
	const upToDateMsg = "System is up to date"

	tests := []struct {
		name         string
		dryRun       bool
		staged       bool
		wantExact    string
		wantContains []string
	}{
		{
			name:      "live run, staged reports the fixed staged message",
			dryRun:    false,
			staged:    true,
			wantExact: stagedMsg,
		},
		{
			name:      "live run, not staged reports the fixed up-to-date message",
			dryRun:    false,
			staged:    false,
			wantExact: upToDateMsg,
		},
		{
			name:         "dry-run, staged previews without claiming completion",
			dryRun:       true,
			staged:       true,
			wantContains: []string{"no changes made"},
		},
		{
			name:         "dry-run, not staged previews without claiming completion",
			dryRun:       true,
			staged:       false,
			wantContains: []string{"no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SystemStage(tt.dryRun, tt.staged)

			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("SystemStage(%v, %v) = %q, want %q", tt.dryRun, tt.staged, got, tt.wantExact)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("SystemStage(%v, %v) = %q, want it to contain %q", tt.dryRun, tt.staged, got, want)
				}
			}
			if tt.dryRun {
				if !strings.Contains(got, "Preview") && !strings.Contains(got, "[DRY-RUN]") {
					t.Errorf("SystemStage(%v, %v) = %q, want it to contain %q or %q", tt.dryRun, tt.staged, got, "Preview", "[DRY-RUN]")
				}
				if strings.Contains(got, stagedMsg) {
					t.Errorf("SystemStage(%v, %v) = %q, must not contain the non-dry-run staged completion string %q", tt.dryRun, tt.staged, got, stagedMsg)
				}
				if strings.Contains(got, upToDateMsg) {
					t.Errorf("SystemStage(%v, %v) = %q, must not contain the non-dry-run up-to-date completion string %q", tt.dryRun, tt.staged, got, upToDateMsg)
				}
			}
		})
	}
}

// TestTapTrust covers both dry-run states for Homebrew tap trust, asserting
// both the UI-mutation gate (MutateUI) and the Toast text. MutateUI is the
// criterion that directly proves the Untrusted Homebrew Taps UI does not
// imply a tap was trusted (row removed, group hidden, refresh triggered)
// after a dry-run click, since homebrew.TrustPackages's underlying `brew
// trust` never actually runs under dry-run.
func TestTapTrust(t *testing.T) {
	tests := []struct {
		name             string
		dryRun           bool
		tapName          string
		wantMutateUI     bool
		wantToast        string
		wantToastContain []string
	}{
		{
			name:         "live run trusts the tap and mutates the UI",
			dryRun:       false,
			tapName:      "some/tap",
			wantMutateUI: true,
			wantToast:    "Trusted some/tap. Its packages can update again.",
		},
		{
			name:             "dry-run previews without mutating the UI",
			dryRun:           true,
			tapName:          "some/tap",
			wantMutateUI:     false,
			wantToastContain: []string{"some/tap", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TapTrust(tt.dryRun, tt.tapName)

			if got.MutateUI != !tt.dryRun {
				t.Errorf("TapTrust(%v, %q).MutateUI = %v, want %v", tt.dryRun, tt.tapName, got.MutateUI, !tt.dryRun)
			}
			if got.MutateUI != tt.wantMutateUI {
				t.Errorf("TapTrust(%v, %q).MutateUI = %v, want %v", tt.dryRun, tt.tapName, got.MutateUI, tt.wantMutateUI)
			}
			if tt.wantToast != "" && got.Toast != tt.wantToast {
				t.Errorf("TapTrust(%v, %q).Toast = %q, want %q", tt.dryRun, tt.tapName, got.Toast, tt.wantToast)
			}
			if tt.dryRun && !strings.Contains(got.Toast, "Preview") && !strings.Contains(got.Toast, "[DRY-RUN]") {
				t.Errorf("TapTrust(%v, %q).Toast = %q, want it to contain %q or %q", tt.dryRun, tt.tapName, got.Toast, "Preview", "[DRY-RUN]")
			}
			for _, want := range tt.wantToastContain {
				if !strings.Contains(got.Toast, want) {
					t.Errorf("TapTrust(%v, %q).Toast = %q, want it to contain %q", tt.dryRun, tt.tapName, got.Toast, want)
				}
			}
		})
	}
}

// TestMaintenanceScript covers both dry-run states for configured custom
// maintenance scripts, asserting both the execution gate (Execute) and the
// Toast text. Execute is the criterion that directly proves no
// state-changing path runs in dry-run for custom scripts, which have no
// wrapper package of their own to gate this the way homebrew/flatpak/bootc/
// updex do.
func TestMaintenanceScript(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		title        string
		wantExecute  bool
		wantToast    string
		wantContains []string
	}{
		{
			name:        "live run executes and reports completion",
			dryRun:      false,
			title:       "Clear tmp",
			wantExecute: true,
			wantToast:   "Clear tmp completed",
		},
		{
			name:         "dry-run never executes and previews instead",
			dryRun:       true,
			title:        "Clear tmp",
			wantExecute:  false,
			wantContains: []string{"[DRY-RUN]", "Clear tmp", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaintenanceScript(tt.dryRun, tt.title)

			if got.Execute != !tt.dryRun {
				t.Errorf("MaintenanceScript(%v, %q).Execute = %v, want %v", tt.dryRun, tt.title, got.Execute, !tt.dryRun)
			}
			if got.Execute != tt.wantExecute {
				t.Errorf("MaintenanceScript(%v, %q).Execute = %v, want %v", tt.dryRun, tt.title, got.Execute, tt.wantExecute)
			}
			if tt.wantToast != "" && got.Toast != tt.wantToast {
				t.Errorf("MaintenanceScript(%v, %q).Toast = %q, want %q", tt.dryRun, tt.title, got.Toast, tt.wantToast)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got.Toast, want) {
					t.Errorf("MaintenanceScript(%v, %q).Toast = %q, want it to contain %q", tt.dryRun, tt.title, got.Toast, want)
				}
			}
		})
	}
}

// TestFeatureToggle covers both dry-run states and both enable/disable for
// a feature switch toggle, asserting both the switch-confirmation gate
// (Confirm) and the Toast text. Confirm is the criterion that directly
// proves the switch does not visually imply a state change after a
// dry-run preview, since updex.EnableFeature/DisableFeature's underlying
// pkexec call never actually runs under dry-run.
func TestFeatureToggle(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		enable       bool
		featName     string
		wantConfirm  bool
		wantToast    string
		wantContains []string
	}{
		{
			name:        "live enable confirms the switch",
			dryRun:      false,
			enable:      true,
			featName:    "docker",
			wantConfirm: true,
			wantToast:   "docker enabled. Update to download, reboot to apply.",
		},
		{
			name:        "live disable confirms the switch",
			dryRun:      false,
			enable:      false,
			featName:    "docker",
			wantConfirm: true,
			wantToast:   "docker disabled. Update to apply, reboot to complete.",
		},
		{
			name:         "dry-run enable previews without confirming the switch",
			dryRun:       true,
			enable:       true,
			featName:     "docker",
			wantConfirm:  false,
			wantContains: []string{"docker", "enabled", "no changes made"},
		},
		{
			name:         "dry-run disable previews without confirming the switch",
			dryRun:       true,
			enable:       false,
			featName:     "docker",
			wantConfirm:  false,
			wantContains: []string{"docker", "disabled", "no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FeatureToggle(tt.dryRun, tt.enable, tt.featName)

			if got.Confirm != !tt.dryRun {
				t.Errorf("FeatureToggle(%v, %v, %q).Confirm = %v, want %v", tt.dryRun, tt.enable, tt.featName, got.Confirm, !tt.dryRun)
			}
			if got.Confirm != tt.wantConfirm {
				t.Errorf("FeatureToggle(%v, %v, %q).Confirm = %v, want %v", tt.dryRun, tt.enable, tt.featName, got.Confirm, tt.wantConfirm)
			}
			if tt.wantToast != "" && got.Toast != tt.wantToast {
				t.Errorf("FeatureToggle(%v, %v, %q).Toast = %q, want %q", tt.dryRun, tt.enable, tt.featName, got.Toast, tt.wantToast)
			}
			if tt.dryRun && !strings.Contains(got.Toast, "Preview") && !strings.Contains(got.Toast, "[DRY-RUN]") {
				t.Errorf("FeatureToggle(%v, %v, %q).Toast = %q, want it to contain %q or %q", tt.dryRun, tt.enable, tt.featName, got.Toast, "Preview", "[DRY-RUN]")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got.Toast, want) {
					t.Errorf("FeatureToggle(%v, %v, %q).Toast = %q, want it to contain %q", tt.dryRun, tt.enable, tt.featName, got.Toast, want)
				}
			}
		})
	}
}

// TestFeatureUpdate covers both dry-run states for the Features page
// "Update" button toast text.
func TestFeatureUpdate(t *testing.T) {
	tests := []struct {
		name         string
		dryRun       bool
		wantExact    string
		wantContains []string
	}{
		{
			name:      "live run reports fixed completion message",
			dryRun:    false,
			wantExact: "Features updated. Changes apply after reboot.",
		},
		{
			name:         "dry-run previews without claiming completion",
			dryRun:       true,
			wantContains: []string{"no changes made"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FeatureUpdate(tt.dryRun)

			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("FeatureUpdate(%v) = %q, want %q", tt.dryRun, got, tt.wantExact)
			}
			if tt.dryRun && !strings.Contains(got, "Preview") && !strings.Contains(got, "[DRY-RUN]") {
				t.Errorf("FeatureUpdate(%v) = %q, want it to contain %q or %q", tt.dryRun, got, "Preview", "[DRY-RUN]")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("FeatureUpdate(%v) = %q, want it to contain %q", tt.dryRun, got, want)
				}
			}
		})
	}
}
