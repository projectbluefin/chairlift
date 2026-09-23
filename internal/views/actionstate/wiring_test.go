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

	for _, required := range []string{
		`if !updateGate.TryStart()`,
		`btn.SetLabel("Updating...")`,
		`go uh.updateHomebrew(btn, updateGate)`,
		`if !upgradeGate.TryStart()`,
		`btn.SetLabel("Upgrading...")`,
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
		`button.SetLabel("Update")`,
		`uh.outdatedRows.Clear(func(row *adw.ActionRow)`,
		`uh.outdatedRows.Add(row)`,
		`Error refreshing updates:`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("updates-page wiring does not contain %q", required)
		}
	}
}

func TestFeaturesPageDeveloperModeUsesGate(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))

	viewsPath := filepath.Join(repoRoot, "internal", "views", "views.go")
	viewsSource, err := os.ReadFile(viewsPath)
	if err != nil {
		t.Fatalf("read %s: %v", viewsPath, err)
	}
	viewsText := string(viewsSource)
	if !strings.Contains(viewsText, "developerGate") || !strings.Contains(viewsText, "actionstate.Gate") {
		t.Errorf("views.go UserHome does not contain developerGate actionstate.Gate field")
	}

	featuresPath := filepath.Join(repoRoot, "internal", "views", "features_page.go")
	featuresSource, err := os.ReadFile(featuresPath)
	if err != nil {
		t.Fatalf("read %s: %v", featuresPath, err)
	}
	featuresText := string(featuresSource)

	for _, required := range []string{
		`uh.developerGate.TryStart()`,
		`uh.developerGate.Reset()`,
		`toggle.SetState(`,
	} {
		if !strings.Contains(featuresText, required) {
			t.Errorf("features_page wiring does not contain %q", required)
		}
	}
}

// A button that is made sensitive again after a run must release its gate;
// Complete permanently rejects every future click, including retries after
// a failed or dry-run action. Views cannot be imported by headless tests.
func TestRepeatableControlsReleaseTheirGates(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	viewsDir := filepath.Clean(filepath.Join(filepath.Dir(filename), ".."))
	for file, gates := range map[string][]string{
		"update_all.go":  {"updateAllGate"},
		"system_page.go": {"driverGate"},
		"reset.go":       {"powerwashGate", "factoryResetGate"},
	} {
		data, err := os.ReadFile(filepath.Join(viewsDir, file))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, gate := range gates {
			if !strings.Contains(text, "uh."+gate+".TryStart()") || !strings.Contains(text, "uh."+gate+".Reset()") || strings.Contains(text, "uh."+gate+".Complete()") {
				t.Errorf("%s: repeatable %s must start and reset, never complete", file, gate)
			}
		}
	}
}

// bootc rollback toggles the selected deployment. A successful live click
// must close its gate; only a failed attempt or dry-run preview may retry.
func TestRollbackGateCompletesOnlyAfterLiveSuccess(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "updates_page.go"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(data), "func (uh *UserHome) onBootcRollbackClicked()", 2)
	if len(parts) != 2 {
		t.Fatal("rollback handler not found")
	}
	body := parts[1]
	for _, required := range []string{"uh.bootcRollbackGate.TryStart()", "if decision.Confirm {", "uh.bootcRollbackGate.Complete()", "button.SetSensitive(false)", "uh.bootcRollbackGate.Reset()", "button.SetSensitive(true)"} {
		if !strings.Contains(body, required) {
			t.Errorf("rollback must remain one-shot on live success but retry after failure or preview: missing %q", required)
		}
	}
}
