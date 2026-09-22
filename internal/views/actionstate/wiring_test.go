package actionstate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpdatesPageUsesGuardedRefreshDecisions(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	path := filepath.Join(repoRoot, "internal", "views", "updates_page.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(source)

	// The guard here is the gate/generation/row bookkeeping, not the button
	// copy: label text and its typography belong to the HIG pass, so the
	// control-state assertions below are deliberately copy-agnostic.
	for _, required := range []string{
		`if !updateGate.TryStart()`,
		`go uh.updateHomebrew(btn, updateGate)`,
		`if !upgradeGate.TryStart()`,
		`dryRun := dryrun.Enabled()`,
		`actionstate.PackageUpgrade(err == nil, dryRun)`,
		`actionstate.OutdatedRefresh(err == nil, currentCount, len(packages))`,
		`actionstate.OutdatedPresentation(refresh.Count)`,
		`uh.outdatedRows.Remove(row`,
		`uh.updateCounts.Add(badgestate.Homebrew, -1)`,
		`actionstate.OutdatedPresentation(remaining)`,
		`uh.loadOutdatedPackages()`,
		`actionstate.MetadataUpdate(err == nil, dryRun)`,
		`uh.loadOutdatedPackagesWithDone(func(bool)`,
		`generation := uh.brewRefresh.Begin()`,
		`go uh.loadOutdatedPackagesGeneration(generation, done)`,
		`!uh.brewRefresh.IsCurrent(generation)`,
		`uh.outdatedRows.Clear(func(row *adw.ActionRow)`,
		`uh.outdatedRows.Add(row)`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("updates-page wiring does not contain %q", required)
		}
	}
}
