package actionstate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
		// The recursion guard used to be a bare `toggle.SetState(` beside
		// every `SetActive`, which only held while every call site
		// remembered to pair them. guardedSwitch owns that pairing —
		// newGuardedSwitch sets both, and set() re-enters behind an
		// `applying` flag the handler checks — so the property is now
		// asserted where it is enforced rather than at each call site.
		`toggle.set(`,
		`newGuardedSwitch(`,
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
		"updates_page.go": {"driverGate"},
		"reset.go":        {"powerwashGate", "factoryResetGate"},
		"agents_page.go":  {"agentPresetGate"},
		// Free up space is offered again after every run (#488).
		"maintenance_page.go": {"freeUpSpaceGate"},
		// Set Up and Launch share one gate, and both are offered again.
		"troubleshoot.go": {"gooseGate", "askBluefinGate"},
		// The developer switch and the optional feed setup behind it are
		// both repeatable: the switch is used again after every toggle, and
		// the setup gate has to reopen when its worker finishes or a second
		// enable could never install anything. Update Features is offered
		// again after every run (#488).
		"features_page.go": {"developerGate", "developerFeedGate", "updateFeaturesGate"},
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
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "recovery.go"))
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

// Panel toggle mirrors the persisted SavedPanelIcon/SavedPanelMode into the
// page's in-memory view of state. Under --dry-run the writes are no-ops, so
// the mirror has to be skipped too — otherwise the next enable sees
// non-empty saved values in memory and skips the capture the user asked
// for. Issue #422.
//
// Both the enable (capture) and the disable (reset) path need their own
// gate, so the assertions below match each gate together with the
// assignments it has to contain. Matching the gate line on its own would
// still pass if one of the two gates were dropped, or if an assignment were
// moved out from under its gate.
func TestLiveryPanelToggleDoesNotMutateInMemoryStateUnderDryRun(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "livery_actions.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		`actionmsg.LiveryToggle(dryrun.Enabled(), enabled, pageview.LiverySectionName(surface))`,
		`if enabled && surface == livery.Panel && savedIcon == "" && savedMode == "" {`,
		`if err := livery.SetString(ctx, livery.KeySavedPanelIcon, icon); err != nil {`,
		`if err := livery.SetString(ctx, livery.KeySavedPanelMode, mode); err != nil {`,
		`restored, err := livery.RevertPanel(ctx, savedIcon, savedMode)`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("panel capture/revert omits %q", required)
		}
	}
	for name, snippet := range map[string]string{
		"capture": "\t\t\tif decision.MutateUI {\n" +
			"\t\t\t\tsgtk.RunOnMainThread(func() {\n" +
			"\t\t\t\t\tuh.liveryState.SavedPanelIcon = icon\n" +
			"\t\t\t\t\tuh.liveryState.SavedPanelMode = mode\n" +
			"\t\t\t\t})\n\t\t\t}\n",
		// The reset also follows a restore that landed before a failed
		// file sweep, so it is gated on restored as well.
		"reset": "\t\t\tif restored && decision.MutateUI {\n" +
			"\t\t\t\tsgtk.RunOnMainThread(func() {\n" +
			"\t\t\t\t\tuh.liveryState.SavedPanelIcon = \"\"\n" +
			"\t\t\t\t\tuh.liveryState.SavedPanelMode = \"\"\n" +
			"\t\t\t\t})\n\t\t\t}\n",
	} {
		if !strings.Contains(text, snippet) {
			t.Errorf("panel %s mirror bypasses its dry-run mutation decision", name)
		}
	}
	if strings.Contains(text, "livery.ClearPanelSettings(") {
		t.Error("the panel toggle restores settings itself instead of through livery.RevertPanel, which owns the restore-then-remove order")
	}
}

// A rotate switch's sensitivity follows its section's confirmed on/off state.
// Selection work captured that state when it started and re-applied it when
// it published, so a section enabled while the selection ran was locked out of
// rotation again until the page reloaded (#496). Publishers read the confirmed
// state at publish time, and the toggle commit records the new state before
// recomputing its dependents.
func TestLiveryRotateSensitivityReadsConfirmedSectionState(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "livery_actions.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "uh.syncLiveryRotateSensitive(surface, enabled)") {
		t.Error("a selection publisher recomputes rotate sensitivity from on/off state captured before its work ran")
	}
	commit := "\t\t\tuh.setLiveryToggleState(s, enabled)\n\t\t\tuh.setLiverySectionSensitive(s, enabled)\n"
	if !strings.Contains(text, commit) {
		t.Error("finishLiveryToggle no longer records the committed state before recomputing the section's rotate switch")
	}
}

// Every Livery handler that mirrors a change on success reads dry-run once,
// into an actionmsg decision that drives both the mirror and the preview
// toast (ADR-0009). Selections and rotation once read a bare preview flag
// and showed nothing under dry-run.
func TestLiveryPreviewsComeFromOneDecision(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "livery_actions.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	decisions := 0
	for call, want := range map[string]int{
		"actionmsg.LiveryToggle(dryrun.Enabled(), ":    2,
		"actionmsg.LiverySelection(dryrun.Enabled(), ": 4,
		"actionmsg.LiveryRotation(dryrun.Enabled(), ":  1,
	} {
		if got := strings.Count(text, call); got != want {
			t.Errorf("%d handlers build %s…), want %d", got, call, want)
		}
		decisions += want
	}
	if got := strings.Count(text, "dryrun.Enabled()"); got != decisions {
		t.Errorf("livery_actions.go reads dryrun.Enabled() %d times, want only the %d decision constructions", got, decisions)
	}
	if got := strings.Count(text, "liverystate.Selection("); got != 1 {
		t.Errorf("selection outcomes resolved in %d places, want only publishLiverySelection", got)
	}
	if !strings.Contains(text, "if saved && decision.Toast != \"\" {") ||
		!strings.Contains(text, "if err == nil && decision.Toast != \"\" {") {
		t.Error("a selection or rotation completion no longer shows its preview toast")
	}
}

// A section switch's work queues in its section's selection line and holds
// the section's rows insensitive while pending. Run beside a pick, turning a
// section off let the pick's Apply land after Clear, leaving a mark installed
// under a switch and a stored setting that both read off.
func TestLiveryTogglesQueueBehindSelections(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "livery_actions.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for handler, call := range map[string]string{
		"func (uh *UserHome) onLiveryAppGridToggled(": "uh.runLiveryToggleWork(livery.AppGrid, func() {",
		"func (uh *UserHome) onLiverySurfaceToggled(": "uh.runLiveryToggleWork(surface, func() {",
	} {
		_, body, found := strings.Cut(text, handler)
		if !found {
			t.Fatalf("%s… is gone", handler)
		}
		body, _, _ = strings.Cut(body, "\n}\n")
		if !strings.Contains(body, call) {
			t.Errorf("%s… does not queue its work through %s", handler, call)
		}
		if strings.Contains(body, "go func()") {
			t.Errorf("%s… starts an unordered goroutine", handler)
		}
	}
	_, helper, found := strings.Cut(text, "func (uh *UserHome) runLiveryToggleWork(")
	if !found {
		t.Fatal("runLiveryToggleWork is gone")
	}
	helper, _, _ = strings.Cut(helper, "\n}\n")
	for _, required := range []string{
		"uh.setLiverySectionSensitive(s, false)",
		"serializer := uh.liverySelectionWork(s)",
		"ticket := serializer.Reserve()",
		"go serializer.Run(ticket, work)",
	} {
		if !strings.Contains(helper, required) {
			t.Errorf("runLiveryToggleWork omits %q", required)
		}
	}
	if !strings.Contains(text, "\t\t\tuh.setLiverySectionSensitive(s, confirmed)\n") {
		t.Error("a failed or previewed toggle leaves its section's rows insensitive")
	}
}

// An installed-list rebuild during a running uninstall or pin replaced the
// row with an idle gate and detached the busy controls, so a second click
// overlapped the first brew command (W2-APPS-1). Rows take their gates from
// the list's RowGates, a rebuild defers while one runs, and every release
// path settles the owed reload.
func TestHomebrewRowGatesSurviveListRebuilds(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "applications_page.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (uh *UserHome) loadHomebrewPackages()")
	if start < 0 {
		t.Fatal("loadHomebrewPackages not found")
	}
	load, _, _ := strings.Cut(text[start:], "\nfunc ")
	if strings.Contains(load, "&actionstate.Gate{}") {
		t.Error("loadHomebrewPackages builds untracked row gates a rebuild would silently replace")
	}
	for _, list := range []string{"formulaGates", "caskGates"} {
		for _, call := range []string{"if uh." + list + ".Defer() {", "uh." + list + ".Rebuild()", "gate := uh." + list + ".New()"} {
			if !strings.Contains(load, call) {
				t.Errorf("loadHomebrewPackages is missing %q", call)
			}
		}
		if !strings.Contains(text, "uh."+list+".Settled()") {
			t.Errorf("a deferred %s rebuild is never settled", list)
		}
	}
	// Two dialog cancellations and the shared finish each release a gate.
	if got := strings.Count(text, "uh.settleHomebrewRows("); got < 3 {
		t.Errorf("settleHomebrewRows is called %d times, want every gate release (cancel pin, cancel uninstall, finish)", got)
	}
}

// Goose Set Up, enabling Agent Mode, and a collection install that stopped
// partway all install Homebrew packages, but none re-read the Apps
// inventory, so the new packages stayed unlisted and their collections kept
// offering Install until restart (W2-APPS-2). Every Homebrew install outside
// the Apps row actions reaches homebrewInventoryChanged.
func TestHomebrewInstallsElsewhereRefreshTheAppsInventory(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	viewsDir := filepath.Join(filepath.Dir(filename), "..")
	body := func(file, signature string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(viewsDir, file))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		start := strings.Index(text, signature)
		if start < 0 {
			t.Fatalf("%s: %q not found", file, signature)
		}
		fn, _, _ := strings.Cut(text[start:], "\nfunc ")
		return fn
	}
	const refresh = "uh.homebrewInventoryChanged()"
	for _, c := range []struct {
		file, signature string
		calls           int
	}{
		{"troubleshoot.go", "func (uh *UserHome) setUpGoose()", 1},
		{"agents_page.go", "func (uh *UserHome) onAgentModeToggled(", 1},
		// The failure branch (partial install) and the live success.
		{"bundle_install.go", "func (uh *UserHome) runBundleInstall(", 2},
		{"developer_tools.go", "func (uh *UserHome) onDeveloperOption(", 1},
	} {
		if got := strings.Count(body(c.file, c.signature), refresh); got != c.calls {
			t.Errorf("%s %s calls %s %d times, want %d", c.file, c.signature, refresh, got, c.calls)
		}
	}
	goose := body("troubleshoot.go", "func (uh *UserHome) setUpGoose()")
	if strings.Index(goose, refresh) > strings.Index(goose, "if err != nil {") {
		t.Error("Goose setup refreshes the inventory only after success; a partial setup also installed packages")
	}
	agent := body("agents_page.go", "func (uh *UserHome) onAgentModeToggled(")
	if strings.Index(agent, refresh) > strings.Index(agent, "if err != nil {\n\t\t\t\tlog.Printf(\"views: agent mode toggle") {
		t.Error("Agent Mode refreshes the inventory only after success; a failed enable may already have installed llmman")
	}
}
