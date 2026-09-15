package actionstate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMaintenancePageWiresCleanupAndResetGroups(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate maintenance_wiring_test.go")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	path := filepath.Join(repoRoot, "internal", "views", "maintenance_page.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(source)

	for _, required := range []string{
		`uh.config.IsGroupEnabled("maintenance_page", "maintenance_cleanup_group")`,
		`uh.config.IsGroupEnabled("maintenance_page", "maintenance_brew_group")`,
		`uh.config.IsGroupEnabled("maintenance_page", "maintenance_flatpak_group")`,
		`uh.config.IsGroupEnabled("maintenance_page", "maintenance_optimization_group")`,
		`uh.config.IsGroupEnabled("maintenance_page", "reset_group")`,
		`uh.buildResetGroup(page)`,
		`uh.runMaintenanceAction(title, script, sudo, btn)`,
		`actionmsg.MaintenanceScript(dryrun.Enabled(), title)`,
		`pageview.MaintenanceCommand(script, sudo)`,
		`uh.onBrewCleanupClicked(button)`,
		`homebrew.Cleanup()`,
		`actionmsg.Cleanup(dryrun.Enabled(), "Homebrew", output)`,
		`uh.onFlatpakCleanupClicked(button)`,
		`flatpak.UninstallUnused()`,
		`actionmsg.Cleanup(dryrun.Enabled(), "Flatpak", output)`,
		`homebrew.BundleDump(path, true)`,
		`actionmsg.BundleDump(dryrun.Enabled(), path)`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("maintenance_page.go wiring does not contain %q", required)
		}
	}
}
