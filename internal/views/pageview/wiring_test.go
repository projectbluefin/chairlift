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
				// Developer-mode onboarding and the optional feed setup both
				// reach their admission rule rather than inlining it, so the
				// "only after a confirmed live enable" contract stays
				// asserted in the puregotk-free packages that own it.
				"pageview.DeveloperOnboardingTargets(",
				"actionmsg.DeveloperFeedSetupPlan(",
				"actionmsg.DeveloperFeedFeedback(",
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
			file: "firstrun.go",
			required: []string{
				"pageview.GetMovingDescription(",
				"pageview.ConfigStepInfoSubtitle(",
				// Advance owns the "is there a step left to display" answer.
				"a.model.Advance(",
				// The forward button's label depends on whether a step
				// follows; pageview owns which word describes the click.
				"pageview.StepForwardAction(",
				"pageview.SetupCompletedMessage",
				// A skip must not overwrite a recorded completion.
				"firstrun.SkipPreserving(",
			},
			retired: []string{
				// Asking HasNext after Next skipped the final step: the move
				// onto it already made HasNext false, so the dialog closed
				// and recorded completion without ever showing it.
				"a.model.HasNext()",
				`"Control Center"`,
				`infoRow.SetSubtitle("`,
				// Completion copy belongs in pageview with the rest.
				`"Setup completed!"`,
				// A forward button hard-labeled Finish misdescribes every
				// intermediate step it advances through.
				`gtk.NewButtonWithLabel("Finish")`,
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
	viewsPath := filepath.Join(filepath.Dir(filename), "..", "views.go")
	viewsSource, err := os.ReadFile(viewsPath)
	if err != nil {
		t.Fatal(err)
	}
	viewsBody := string(viewsSource)
	for _, required := range []string{"updateflow.OperatingSystem", "bootc.GetStatus(", "uh.updateCounts.SetObserved(badgestate.Bootc,", "uh.refreshChangelogAvailability(status)"} {
		if !strings.Contains(viewsBody, required) {
			t.Errorf("Update All OS staging never applies %q to the Compare row and badge", required)
		}
	}
}

func TestUpdateAllRefreshesProviderInventories(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "views.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, call := range []string{"uh.loadFlatpakUpdates()", "uh.loadOutdatedPackages()"} {
		if !strings.Contains(body, call) {
			t.Errorf("Update All leaves stale inventory without %s", call)
		}
	}
}

func TestUpdateAllDryRunDoesNotAnnounceUpdates(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "update_shell.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "final.Preview") {
		t.Error("dry-run Update All must not announce completion when final.Preview is true")
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
