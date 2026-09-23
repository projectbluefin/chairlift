package pageview

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPageBuildersUsePurePresentations(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	viewsDir := filepath.Clean(filepath.Join(filepath.Dir(filename), ".."))

	tests := []struct {
		file     string
		required []string
		retired  []string
	}{
		{
			file: "livery_page.go",
			required: []string{
				"pageview.LiveryPageDescription",
				"pageview.LiveryChoices(",
				"pageview.LiveryAppGridRow(",
				"pageview.LiveryPanelRow(",
				"pageview.LiveryDockRow(",
				"pageview.LiveryRotationRow(",
			},
			retired: []string{
				// The mantra and its fragments are owned by pageview; an
				// inline copy here would drift from the sentence it came from.
				`"Who you are"`,
				`"Who you stand with"`,
				`"What you roll with"`,
			},
		},
		{
			file: "applications_page.go",
			required: []string{
				"bundleview.Describe(",
				"pageview.HomebrewPackage(",
				"pageview.FlatpakApplication(",
				"pageview.SearchResult(",
				"trustmsg.BundleMessage(",
			},
			retired: []string{
				`fmt.Sprintf("%s — %s", bundle.Description, bundle.Path)`,
				`fmt.Sprintf("%s (%s)", app.ApplicationID, app.Version)`,
				`row.SetSubtitle(result.Kind.DisplayName())`,
				"pageview.BrewBundle(",
				`"Brew Bundle Dump"`,
				"~/Brewfile",
				`fmt.Sprintf("Error: %v", err)`,
			},
		},
		{
			file: "updates_page.go",
			required: []string{
				"pageview.UntrustedTap(",
				"pageview.FlatpakUpdate(",
				"pageview.BootcUpdateSubtitle(",
				"pageview.BootcStageResultSubtitle(",
				"pageview.SysupdateUpdateSubtitle(",
				"pageview.SysupdateStageResultSubtitle(",
				"pageview.SysupdateRollbackSubtitle(",
				// Moved here with the release channel and the graphics
				// driver when the System page was deleted.
				"pageview.ChannelRow(",
				"pageview.GraphicsDriverRow(",
				// The system-version readout came with them. Digest
				// formatting now lives entirely inside
				// SystemVersionDetails, which calls ShortDigest itself —
				// stricter than the deleted system_page.go entry, which
				// only required the view to call ShortDigest. The banned
				// hand-slice below is carried across from that entry.
				"pageview.SystemVersionRow(",
				"pageview.SystemVersionDetails(",
				"pageview.StagingLogSubtitle(",
			},
			retired: []string{
				"strings.LastIndex(",
				`fmt.Sprintf("%s → %s", update.ApplicationID, update.NewVersion)`,
				`fmt.Sprintf("Update %s staged — restart to apply", version)`,
				"digest[:19]",
				"row.SetSubtitle(pkg.Version)",
				`"Roll Back"`,
			},
		},
		{
			file: "maintenance_page.go",
			required: []string{
				"pageview.MaintenanceCommand(",
				"cleanupview.Summarize(",
				"updateproviders.NewCleanup(",
			},
			retired: []string{
				`exec.CommandContext(ctx, "pkexec", script)`,
				"exec.CommandContext(ctx, script)",
				"row.SetSubtitle(action.Script)",
				`"Coming soon"`,
			},
		},
		{
			file: "features_page.go",
			required: []string{
				"pageview.Feature(",
				"pageview.FeatureGroupDescription(",
				"pageview.DeveloperRow(",
				"pageview.GamingRow(",
			},
			retired: []string{
				"row.SetTitle(feat.Description)",
				`fmt.Sprintf("%d features available", len(features))`,
				`"Developer Mode"`,
				`"Gaming Mode"`,
				"status.DevGroups",
			},
		},
		{
			file:     "help_page.go",
			required: []string{"pageview.HelpResources("},
			retired:  []string{`row.SetTitle("Website")`, `row.SetTitle("Report Issues")`},
		},
		{
			file: "agents_page.go",
			required: []string{
				"pageview.AIStackRow(",
				"pageview.AIStackDetails(",
				"pageview.AIStackGroupDescription(",
				"actionmsg.AIStack(",
			},
			// A bare switch re-enters ::state-set on a programmatic revert,
			// which would restart the model after a failed stop; the error
			// text names the unit file and belongs in the log, not a toast.
			retired: []string{
				`row.SetTitle("Local AI Model Server")`,
				`"Working..."`,
				"gtk.NewSwitch()",
				"Local AI failed: %v",
			},
		},
		{
			file: "recovery.go",
			required: []string{
				"pageview.BootcRollbackRow(",
				"pageview.SysupdateRollbackSubtitle(",
				"pageview.BootcRollbackResultSubtitle(",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join(viewsDir, tt.file)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(source)
			for _, fragment := range tt.required {
				if !strings.Contains(text, fragment) {
					t.Errorf("%s does not use %q", tt.file, fragment)
				}
			}
			for _, fragment := range tt.retired {
				if strings.Contains(text, fragment) {
					t.Errorf("%s still owns presentation logic %q", tt.file, fragment)
				}
			}
		})
	}
}

// Staging changes the pair of images Compare uses. Startup's status refresh
// alone is insufficient: the same window must enable Compare after staging.
func TestBootcStageRefreshesChangelogAvailability(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "updates_page.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stage := strings.SplitN(string(source), "func (uh *UserHome) onBootcStageClicked()", 2)
	if len(stage) != 2 {
		t.Fatal("bootc staging handler not found")
	}
	body := strings.SplitN(stage[1], "func (uh *UserHome) loadSysupdateUpdateStatus(", 2)[0]
	if !strings.Contains(body, "uh.refreshChangelogAvailability(status)") {
		t.Error("successful bootc staging never refreshes the Compare references and button")
	}
	// A successful stage with an unreadable status is not evidence that the
	// image is current. Refuse the success toast when the re-read failed.
	if !strings.Contains(body, "if statusErr != nil {") || !strings.Contains(body, "Could not verify staged update") {
		t.Error("bootc staging claims a known result after its status re-read failed")
	}
	allPath := filepath.Join(filepath.Dir(filename), "..", "update_all.go")
	allSource, err := os.ReadFile(allPath)
	if err != nil {
		t.Fatal(err)
	}
	completion := strings.SplitN(string(allSource), "func (uh *UserHome) finishUpdateAll(", 2)
	if len(completion) != 2 {
		t.Fatal("Update All completion handler not found")
	}
	allBody := strings.SplitN(completion[1], "func (uh *UserHome) onRestartClicked(", 2)[0]
	for _, required := range []string{"updateall.PhaseOS", "bootc.GetStatus(", "uh.updateCounts.SetObserved(badgestate.Bootc,", "uh.refreshChangelogAvailability(status)"} {
		if !strings.Contains(allBody, required) {
			t.Errorf("Update All OS staging never applies %q to the Compare row and badge", required)
		}
	}
}

func TestUpdateAllBrewPhaseUpgradesAfterMetadata(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "update_all.go"))
	if err != nil {
		t.Fatal(err)
	}
	runner := strings.SplitN(string(data), "func hostRunner()", 2)
	if len(runner) != 2 {
		t.Fatal("Update All runner not found")
	}
	body := strings.SplitN(runner[1], "func (uh *UserHome) onUpdateAllClicked(", 2)[0]
	update := strings.Index(body, "homebrew.Update(ctx)")
	upgrade := strings.Index(body, "homebrew.Upgrade(ctx, \"\")")
	if update < 0 || upgrade <= update {
		t.Error("Update All must refresh brew metadata and then upgrade installed packages using the run context")
	}
}

func TestUpdateAllRefreshesProviderInventories(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "update_all.go"))
	if err != nil {
		t.Fatal(err)
	}
	completion := strings.SplitN(string(data), "func (uh *UserHome) finishUpdateAll(", 2)
	if len(completion) != 2 {
		t.Fatal("Update All completion handler not found")
	}
	body := strings.SplitN(completion[1], "func (uh *UserHome) onRestartClicked(", 2)[0]
	for _, call := range []string{"uh.loadFlatpakUpdates()", "uh.loadOutdatedPackages()"} {
		if !strings.Contains(body, call) {
			t.Errorf("Update All leaves stale inventory without %s", call)
		}
	}
}

func TestSysupdateStageDoesNotClaimCurrentOnUnreadableStatus(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "updates_page.go"))
	if err != nil {
		t.Fatal(err)
	}
	stage := strings.SplitN(string(data), "func (uh *UserHome) onSysupdateStageClicked()", 2)
	if len(stage) != 2 {
		t.Fatal("native staging handler not found")
	}
	body := strings.SplitN(stage[1], "func (uh *UserHome) updateHomebrew(", 2)[0]
	if !strings.Contains(body, "if statusErr != nil {") || !strings.Contains(body, "Could not verify staged update") {
		t.Error("native A/B staging claims the system is current after an unreadable status")
	}
}

func TestUpdateAllDryRunDoesNotAnnounceUpdates(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "update_all.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	clicked := strings.SplitN(text, "func (uh *UserHome) onUpdateAllClicked(", 2)
	if len(clicked) != 2 {
		t.Fatal("Update All click handler not found")
	}
	clickedBody := strings.SplitN(clicked[1], "func (uh *UserHome) applyUpdateAllEvent(", 2)[0]
	guard := strings.Index(clickedBody, "if !dryrun.Enabled() {")
	disable := strings.Index(clickedBody, "uh.updateAllRestart.SetSensitive(false)")
	if guard < 0 || disable <= guard {
		t.Error("Update All must guard restart-row sensitivity so previews preserve known state")
	}
	completion := strings.SplitN(text, "func (uh *UserHome) finishUpdateAll(", 2)
	if len(completion) != 2 {
		t.Fatal("Update All completion handler not found")
	}
	preview := strings.Index(completion[1], "[DRY-RUN] Preview")
	notify := strings.Index(completion[1], "NotifyBackground(")
	if preview < 0 || notify <= preview || !strings.Contains(completion[1][:notify], "if dryrun.Enabled() {") {
		t.Error("dry-run Update All must report a preview before any completion notification")
	}
	phase := strings.SplitN(text, "func (uh *UserHome) applyUpdateAllEvent(", 2)
	if len(phase) != 2 {
		t.Fatal("Update All phase renderer not found")
	}
	phaseBody := strings.SplitN(phase[1], "func (uh *UserHome) finishUpdateAll(", 2)[0]
	if !strings.Contains(phaseBody, "dryrun.Enabled() && event.Result.Outcome == updateall.OutcomeSucceeded") || !strings.Contains(phaseBody, "[DRY-RUN] Preview") {
		t.Error("a dry-run phase claims it finished updating packages")
	}
}

func TestUpdateAllRetryKeepsKnownRestart(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "update_all.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	clicked := strings.SplitN(text, "func (uh *UserHome) onUpdateAllClicked(", 2)
	if len(clicked) != 2 {
		t.Fatal("Update All click handler not found")
	}
	clickedBody := strings.SplitN(clicked[1], "func (uh *UserHome) applyUpdateAllEvent(", 2)[0]
	if strings.Contains(clickedBody, "uh.updateAllRestart.SetVisible(false)") || !strings.Contains(clickedBody, "uh.updateAllRestart.SetSensitive(false)") {
		t.Error("retry must temporarily disable, not discard, a known restart prompt")
	}
	finished := strings.SplitN(text, "func (uh *UserHome) finishUpdateAll(", 2)
	if len(finished) != 2 {
		t.Fatal("Update All completion handler not found")
	}
	finishBody := strings.SplitN(finished[1], "func (uh *UserHome) onRestartClicked(", 2)[0]
	for _, required := range []string{"uh.updateAllRestart.SetSensitive(true)", "status.Status.Staged", "uh.updateAllRestart.SetVisible(false)", "uh.updateAllRestart.SetVisible(true)"} {
		if !strings.Contains(finishBody, required) {
			t.Errorf("completion does not reconcile pending restart via %q", required)
		}
	}
}

func TestChangelogRefreshDiscardsOldImagePair(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "changelog.go"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(data), "func (uh *UserHome) refreshChangelogAvailability(", 2)
	if len(parts) != 2 {
		t.Fatal("changelog refresh not found")
	}
	refresh := strings.SplitN(parts[1], "func (uh *UserHome) onChangelogClicked(", 2)[0]
	if !strings.Contains(refresh, "booted != uh.changelogBooted || staged != uh.changelogStaged") || !strings.Contains(refresh, "uh.changelogSections = nil") {
		t.Error("changing the staged image leaves the previous package-diff sections visible")
	}
	compare := strings.SplitN(parts[1], "func (uh *UserHome) onChangelogClicked(", 2)
	if len(compare) != 2 || !strings.Contains(compare[1], "booted != uh.changelogBooted || staged != uh.changelogStaged") {
		t.Error("in-flight comparison can render a diff for an image no longer staged")
	}
}
