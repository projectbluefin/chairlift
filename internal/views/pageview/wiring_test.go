package pageview

import (
	"go/ast"
	"go/parser"
	"go/token"
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
				"pageview.LiveryAppGridGroupDescription(",
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
			},
			retired: []string{
				`fmt.Sprintf("%s — %s", bundle.Description, bundle.Path)`,
				"pageview.BrewBundle(",
				"~/Brewfile",
				`fmt.Sprintf("Error: %v", err)`,
				"homebrew.BundleInstall(",
			},
		},
		{
			file: "updates_page.go",
			required: []string{
				"pageview.UntrustedTap(",
				"pageview.BootcUpdateSubtitle(",
				"pageview.BootcStageResultSubtitle(",
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
			file: "printers_page.go",
			required: []string{
				"pageview.PrintersGroupTitle(",
				"pageview.PrinterFamilyRow(",
				"pageview.PrinterAppSubtitle(",
				"pageview.PrinterAppFailureToast(",
				"actionmsg.PrinterApp(",
				// Readiness comes from the state model, never from the unit
				// file alone (#331, #361).
				"printerapp.Resolve(",
			},
			retired: []string{
				`"Printers"`,
				"printerapp.IsEnabled(",
			},
		},
		{
			file:     "help_page.go",
			required: []string{"pageview.HelpResources(", "pageview.UnavailableFeatures(", "pageview.SystemDiagnosticsRow("},
			retired:  []string{`row.SetTitle("Website")`, `row.SetTitle("Report Issues")`},
		},
		{
			file: "agents_page.go",
			required: []string{
				"pageview.AgentModeSubtitle(",
				"pageview.AgentModeGroupDescription(",
				"actionmsg.AgentMode(",
				"pageview.AgentModeActiveModelTitle()",
				"pageview.AgentModePresetsTitle()",
			},
			// A bare switch re-enters ::state-set on a programmatic revert,
			// which would restart the service after a failed stop; the error
			// text names the unit file and belongs in the log, not a toast.
			retired: []string{
				`row.SetTitle("Local AI Model Server")`,
				`"Working..."`,
				"gtk.NewSwitch()",
				"Local AI failed: %v",
			},
		},
		{
			file: "troubleshoot.go",
			required: []string{
				"pageview.TroubleshootGroupTitle()",
				"pageview.TroubleshootGroupDescription()",
				"pageview.GooseRow(",
				"pageview.GooseSetupToast(",
				// Readiness and the launch both belong to agentmode, which
				// writes ChairLift's own Goose profile before launching.
				"agentmode.ObserveLive(",
				"agentmode.Launch(",
				"troubleshoot.Setup(",
			},
			// The Goose row is the one surface: no gtk-launch of the cask's
			// desktop file (which would bypass the profile), and no repair of
			// the user's own Goose configuration.
			retired: []string{
				"launchApp(",
				"EnsureDiagnosticsConfigured",
			},
		},
		{
			file: "recovery.go",
			required: []string{
				"pageview.BootcRollbackRow(",
				"pageview.BootcRollbackResultSubtitle(",
				"pageview.UnpinRow(",
				"pageview.UnpinConfirmation(",
			},
		},
		{
			file: "versions.go",
			required: []string{
				"pageview.PublishedVersionsRow(",
				"pageview.PublishedVersionsSummary(",
				"pageview.PublishedVersions(",
				"pageview.PinConfirmation(",
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

// Both staging entry points refresh Compare; the shell owns the aggregate badge.
func TestBootcStageRefreshesChangelogAvailability(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	viewsDir := filepath.Join(filepath.Dir(filename), "..")
	functionBody := func(file, name string) string {
		t.Helper()
		path := filepath.Join(viewsDir, file)
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		set := token.NewFileSet()
		parsed, err := parser.ParseFile(set, path, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Body != nil {
				return string(source[set.Position(fn.Body.Pos()).Offset:set.Position(fn.Body.End()).Offset])
			}
		}
		t.Fatalf("%s: function %s not found", file, name)
		return ""
	}
	for _, check := range []struct {
		file, function string
		required       []string
	}{
		{"updates_page.go", "onBootcStageClicked", []string{"uh.refreshChangelogAvailability(status)", "if statusErr != nil {", "Could not verify staged update", "uh.updateShell.StartCheck()"}},
		{"views.go", "OnUpdateFinished", []string{"if final.Preview {", "range final.CompletedSources", "case updateflow.OperatingSystem:", "bootc.GetStatus(", "if err != nil {", "uh.refreshChangelogAvailability(status)"}},
		{"update_shell.go", "Render", []string{"s.toasts.SetUpdateBadge(snapshot.TotalUpdates)"}},
		{"update_shell.go", "StartUpdate", []string{"s.onUpdateFinished(final)"}},
	} {
		body := functionBody(check.file, check.function)
		for _, assertion := range check.required {
			if !strings.Contains(body, assertion) {
				t.Errorf("%s.%s no longer preserves staged Compare/badge refresh: %s", check.file, check.function, assertion)
			}
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

// The OS source row owns the restart action now (#439): the source row
// builds a "Restart now" suffix on the Operating system source and toggles
// its visibility as RestartRequired changes, so the page-level status
// panel does not carry the restart state.
func TestOperatingSystemRowOwnsRestartButton(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "source_row.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, fragment := range []string{
		`state.ID == updateflow.OperatingSystem`,
		`"Restart now"`,
		`SetVisible(state.RestartRequired)`,
		`state.RestartRequired != r.restartShown`,
		`shell.StartRestart()`,
		`setRestartButtonSensitive`,
		`!shell.restartInFlight.Load()`,
		`if r.restartButton != nil && restartInFlight`,
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("source_row.go no longer wires the Operating system row's Restart now button: %q", fragment)
		}
	}
}

// The page-level status panel clears when a deployment is staged (#439);
// the row carries the message instead of the status page. updatepresent's
// restart case must leave the panel's title, description, and banner empty
// so the wordmark leads into the source groups, keeping only the
// announcement a screen reader needs.
func TestPhaseRestartRequiredClearsStatusPanel(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "updatepresent", "updatepresent.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "case updateflow.PhaseRestartRequired:") {
		t.Fatal("PhaseRestartRequired case missing from updatepresent")
	}
	// The restart case must not advertise a title, description, or banner
	// above the wordmark; if any of these strings return the OS row carries
	// the message and the panel stays blank.
	for _, banned := range []string{
		`Title: gotext.Get("Restart required")`,
		`Description: gotext.Get("Restart to finish installing updates.")`,
		`Banner:      gotext.Get("Restart required")`,
		`ActionStyle = "destructive-action"`,
	} {
		if strings.Contains(text, banned) {
			t.Errorf("PhaseRestartRequired still renders %q in the status panel", banned)
		}
	}
}
