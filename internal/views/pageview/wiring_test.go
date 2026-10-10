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
				"pageview.PackageListExportSubtitle",
				"pageview.HomebrewPackageButtonName(",
				"button.ResetRelation(gtk.AccessibleRelationLabelledByValue)",
			},
			retired: []string{
				`fmt.Sprintf("%s — %s", bundle.Description, bundle.Path)`,
				"pageview.BrewBundle(",
				"~/Brewfile",
				`fmt.Sprintf("Error: %v", err)`,
				"homebrew.BundleInstall(",
				// The export overwrites any Brewfile, not only an earlier
				// export (W2-APPS-4).
				"exported last time",
				// A bare label names every row's button alike (W2-APPS-5);
				// row buttons go through setPackageButtonLabel.
				`gtk.NewButtonWithLabel("Uninstall")`,
				"gtk.NewButtonWithLabel(pinLabel)",
				"primary.SetLabel(",
			},
		},
		{
			file: "updates_page.go",
			required: []string{
				"pageview.UntrustedTap(",
				"pageview.UntrustedTapPackage(",
				"pageview.TapTrustConfirmation(",
				"pageview.BootcUpdateSubtitle(",
				"pageview.BootcStageResultSubtitle(",
				// Moved here with the release channel and the graphics
				// driver when the System page was deleted.
				"pageview.ChannelRow(",
				"pageview.GraphicsDriverRow(",
				// The system-version readout came with them. Digest
				// formatting lives entirely inside SystemVersionDetails,
				// which now gives the whole digest (W3-07); the banned
				// hand-slice below is carried across from the deleted
				// system_page.go entry.
				"pageview.SystemVersionRow(",
				"pageview.SystemVersionDetails(",
				"pageview.StagingLogSubtitle(",
				// W3-01: the action stages an update, so its label and
				// running and failure copy come from pageview, where the
				// test holds them to describing a download.
				"pageview.BootcStageButtonLabel",
				"pageview.BootcStageRunningSubtitle",
				"pageview.BootcStageFailureSubtitle(hadOutput)",
				// W3-07: the support identifiers are selectable.
				"row.SetSubtitleSelectable(true)",
				// W3-08: the Details expander is hidden until a line
				// arrives, so an empty one is never offered.
				"logExpander.SetVisible(false)",
				"s.logExpander.SetVisible(true)",
			},
			retired: []string{
				"strings.LastIndex(",
				`fmt.Sprintf("%s → %s", update.ApplicationID, update.NewVersion)`,
				`fmt.Sprintf("Update %s staged — restart to apply", version)`,
				"digest[:19]",
				"row.SetSubtitle(pkg.Version)",
				`"Roll Back"`,
				`"Check for updates"`,
				`"Checking for updates…"`,
				`"The update could not be downloaded. Open Details to see what happened."`,
				"ShortDigest(",
				// The tap-trust dialog's count is pluralized in pageview.
				"installed programs from %s at once",
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
			file: "developer_tools.go",
			required: []string{
				"pageview.DeveloperTool(",
				"pageview.DeveloperToolChecking(",
				"pageview.DeveloperToolInstalling(",
				"pageview.DeveloperToolUnverified(",
				"SetAccessibleLabel(item.button, view.AccessibleLabel)",
				// W3-10: a tool removed on Apps or in a terminal read
				// "Installed" until restart. The re-read is connected once
				// at build and generation-guarded against gated actions.
				"ConnectMap(&uh.developerToolsMapped)",
				"uh.developerToolRefresh.IsCurrent(generation)",
			},
			retired: []string{
				`"Optional Homebrew tool; installed only when you choose it."`,
				`"Installed through Homebrew."`,
				`"Checking installed state…"`,
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
				// A live launch says what it observed, including a hand-off
				// to a session that may have no window (#544).
				"pageview.GooseLaunchToast(",
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
			file: "contribute.go",
			// Preflight re-runs whenever the group is shown, so a requirement
			// the user fixed elsewhere (Hive registration, Podman) does not
			// leave the button insensitive until restart; reads are
			// generation-guarded against a running session.
			required: []string{
				"ConnectMap(&uh.contributeMapped)",
				"uh.contributeRefresh.IsCurrent(generation)",
				"uh.contributeGate.Running()",
			},
		},
		{
			file: "recovery.go",
			required: []string{
				"pageview.BootcRollbackRow(",
				"pageview.BootcRollbackResultSubtitle(",
			},
			// The published-versions calendar (pin / return to stream)
			// is withdrawn (#522): Powerwash offers Roll Back only.
			retired: []string{"buildRecoveryVersionsGroup(", "buildReturnToStreamRow(", "ublue.Pin(", "ublue.Unpin("},
		},
		{
			// Collection buttons are named after the collection (W2-APPS-5).
			file:     "bundle_install.go",
			required: []string{"bundleview.InstallButtonName(label, b.title)"},
			retired:  []string{"SetAccessibleLabel(control.button, label)"},
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
		{"updates_page.go", "onBootcStageClicked", []string{"uh.refreshChangelogAvailability(status)", "uh.renderSystemVersion(versionGeneration, status)", "if statusErr != nil {", "Couldn't confirm the update is ready", "uh.updateShell.StartCheck()"}},
		{"views.go", "OnUpdateFinished", []string{"if final.Preview {", "range final.CompletedSources", "case updateflow.OperatingSystem:", "bootc.GetStatus(", "if err != nil {", "uh.refreshChangelogAvailability(status)", "uh.renderSystemVersion(versionGeneration, status)"}},
		// W3-06: the System version readout re-renders from each observed
		// status, ordered so an older read cannot replace a newer one.
		{"updates_page.go", "renderSystemVersion", []string{"uh.systemVersionRefresh.IsCurrent(generation)", "uh.systemVersionRows.Clear(", "uh.systemVersionRow == nil"}},
		{"update_shell.go", "Render", []string{"s.toasts.SetUpdateBadge(snapshot.TotalUpdates)"}},
		// W4: a dry-run Update all says it was a preview.
		{"update_shell.go", "StartUpdate", []string{"s.onUpdateFinished(final)", "if final.Preview {", "s.toasts.ShowToast(actionmsg.UpdateAllPreview)"}},
		// W3-15: a preference changed while a check ran is re-checked once
		// that check returns, and after a mutation that refused the check.
		{"update_shell.go", "StartCheck", []string{"sgtk.RunOnMainThread(s.checkFinished)"}},
		{"update_shell.go", "checkFinished", []string{"updatepresent.RecheckAfterCheck(s.snapshot, s.currentPreferences())", "s.StartCheck()"}},
		{"update_shell.go", "PreferencesChanged", []string{"updatepresent.RecheckForPreferences(s.snapshot, s.currentPreferences())", "s.StartCheck()"}},
		{"update_shell.go", "finishMutation", []string{"s.PreferencesChanged()"}},
		// W4: every action that stages or replaces the operating system is
		// admitted by the update shell, so none races an update run's own
		// staging; a refusal says why, and the admission ends before the
		// follow-up check the shell would otherwise refuse.
		{"updates_page.go", "onBootcStageClicked", []string{"if !uh.updateShell.beginMutation() {", "pageview.UpdateBusyToast", "uh.updateShell.finishMutation()"}},
		{"updates_page.go", "onDriverSwitchClicked", []string{"if !uh.updateShell.beginMutation() {", "uh.driverGate.Reset()", "pageview.UpdateBusyToast", "uh.updateShell.finishMutation()"}},
		{"updates_page.go", "onChannelToggled", []string{"if !uh.updateShell.beginMutation() {", "toggle.set(!toTesting)", "pageview.UpdateBusyToast", "uh.updateShell.finishMutation()"}},
		// W4: a dismissed password prompt is a brief cancellation, not a
		// persistent error, on every Updates-page privileged action.
		{"updates_page.go", "onBootcStageClicked", []string{"pageview.PrivilegedFailureToast(stageErr,"}},
		{"updates_page.go", "onDriverSwitchClicked", []string{"uh.showPrivilegedFailure(err,"}},
		{"updates_page.go", "onChannelToggled", []string{"uh.showPrivilegedFailure(err,"}},
		{"updates_page.go", "showPrivilegedFailure", []string{"pageview.PrivilegedFailureToast(err, failure)"}},
	} {
		body := functionBody(check.file, check.function)
		for _, assertion := range check.required {
			if !strings.Contains(body, assertion) {
				t.Errorf("%s.%s no longer carries required update wiring: %s", check.file, check.function, assertion)
			}
		}
	}
	// refreshAfterOSSwitch starts a check, which the shell refuses while the
	// switch still holds its admission.
	for _, function := range []string{"onDriverSwitchClicked", "onChannelToggled"} {
		body := functionBody("updates_page.go", function)
		finish := strings.Index(body, "uh.updateShell.finishMutation()")
		refresh := strings.Index(body, "uh.refreshAfterOSSwitch()")
		if finish < 0 || refresh < 0 || finish > refresh {
			t.Errorf("updates_page.go.%s must end its update-shell admission before refreshAfterOSSwitch", function)
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
	// The in-flight guard compares the button's label with the label the
	// comparison set; one constant keeps the two spellings from drifting
	// (a three-dot "Comparing..." never matched the "Comparing…" it set).
	text := string(data)
	if got := strings.Count(text, "pageview.ChangelogComparingLabel"); got < 2 {
		t.Errorf("changelog.go uses pageview.ChangelogComparingLabel %d times, want both the label and its guard", got)
	}
	if strings.Contains(text, `"Comparing`) {
		t.Error("changelog.go spells the Comparing label inline instead of using pageview.ChangelogComparingLabel")
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

// The Roll Back heading belongs to the rollback row alone (#490). It used to
// share a group with Return to stream and Published versions, so a host with
// no previous deployment hid the row and left the heading standing over
// unrelated rows. The group now holds only the rollback row and is what the
// status loader shows and hides.
func TestRollBackHeadingHidesWithItsRow(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "recovery.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, path, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	bodies := map[string]string{}
	for _, declaration := range parsed.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Body != nil {
			bodies[fn.Name.Name] = string(source[set.Position(fn.Body.Pos()).Offset:set.Position(fn.Body.End()).Offset])
		}
	}

	build := bodies["buildRecoveryRollbackGroup"]
	for _, required := range []string{`group.SetTitle("Roll Back")`, "group.SetVisible(false)", "uh.bootcRollbackGroup = group"} {
		if !strings.Contains(build, required) {
			t.Errorf("buildRecoveryRollbackGroup no longer builds a hidden, owned Roll Back group: missing %q", required)
		}
	}
	for _, banned := range []string{"buildReturnToStreamRow(", "buildPublishedVersionsRow(", "buildRecoveryVersionsGroup("} {
		if strings.Contains(build, banned) {
			t.Errorf("buildRecoveryRollbackGroup puts %s under the Roll Back heading", banned)
		}
	}

	load := bodies["loadBootcRollbackStatus"]
	if !strings.Contains(load, "uh.bootcRollbackGroup.SetVisible(offered)") {
		t.Error("loadBootcRollbackStatus must show and hide the whole Roll Back group from one offered decision")
	}
	if strings.Contains(load, "uh.bootcRollbackRow.SetVisible(") {
		t.Error("loadBootcRollbackStatus hides only the row, leaving the Roll Back heading orphaned")
	}
}

// W3-11: the Maintenance entry's subtitle is derived from what the Powerwash
// detail built, not a fixed promise of a reset. It is written once the detail
// is built and again whenever the rollback check reveals or hides Roll Back.
func TestRecoveryEntrySubtitleFollowsTheDetail(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	bodies := map[string]string{}
	for _, name := range []string{"recovery.go", "maintenance_page.go"} {
		path := filepath.Join(filepath.Dir(filename), "..", name)
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
			if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Body != nil {
				bodies[fn.Name.Name] = string(source[set.Position(fn.Body.Pos()).Offset:set.Position(fn.Body.End()).Offset])
			}
		}
	}

	if strings.Contains(bodies["buildMaintenancePage"], "RecoveryEntrySubtitle(") {
		t.Error("buildMaintenancePage writes the Powerwash entry subtitle before the detail is built")
	}
	for _, fn := range []string{"buildRecoveryPage", "loadBootcRollbackStatus"} {
		if !strings.Contains(bodies[fn], "uh.refreshRecoveryEntry()") {
			t.Errorf("%s does not refresh the Powerwash entry subtitle", fn)
		}
	}
	refresh := bodies["refreshRecoveryEntry"]
	for _, required := range []string{"uh.bootcRollbackOffered", `"reset_group"`} {
		if !strings.Contains(refresh, required) {
			t.Errorf("refreshRecoveryEntry ignores %s", required)
		}
	}
	if !strings.Contains(bodies["buildRecoveryPage"], "page.SetDescription(pageview.RecoveryPageDescription(") {
		t.Error("buildRecoveryPage does not explain a missing reset")
	}
	if strings.Contains(bodies["buildRecoveryPage"], "buildRecoveryVersionsGroup(") {
		t.Error("buildRecoveryPage builds the published-versions calendar withdrawn by #522")
	}
}

// The Manage source trust group (#537) had two defects at the widget layer.
// A failed source check added its "Could not check software sources" row to
// the group and then again to an untitled, collapsed expander, so a sighted
// user saw a blank row and not the message or its Retry button; with that
// shape, make e2e's walkthrough (whose host check fails) captured every page
// as the same frame. And per-package rows were keyed by
// qualified name alone, so a formula and a cask sharing a name in one tap
// collided: trusting the formula removed the cask's row and stranded the
// formula's on "Trusting\xe2\x80\xa6". The error row is a direct child of the group,
// and both the build and the removal key packages by kind and name.
func TestUntrustedSourceRowsStayVisibleAndDistinct(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "updates_page.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, path, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	bodies := map[string]string{}
	for _, declaration := range parsed.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Body != nil {
			bodies[fn.Name.Name] = string(source[set.Position(fn.Body.Pos()).Offset:set.Position(fn.Body.End()).Offset])
		}
	}

	load := bodies["loadUntrustedTaps"]
	for _, required := range []string{
		"uh.brewTrustGroup.Add(&row.Widget)",
		"uh.brewTrustError = row",
		"packages[trustPackageKey{homebrew.Formula, formula}]",
		"packages[trustPackageKey{homebrew.Cask, cask}]",
	} {
		if !strings.Contains(load, required) {
			t.Errorf("loadUntrustedTaps: missing %q", required)
		}
	}
	for _, banned := range []string{"AddRow(&row.Widget)", "packages[formula]", "packages[cask]"} {
		if strings.Contains(load, banned) {
			t.Errorf("loadUntrustedTaps: %q hides the error row or keys packages by name alone", banned)
		}
	}

	trust := bodies["trustPackage"]
	if !strings.Contains(trust, "trustPackageKey{kind, qualifiedName}") || strings.Contains(trust, "packages[qualifiedName]") {
		t.Error("trustPackage must find the trusted row by kind and name, not name alone")
	}

	// After a per-package trust the tap's remaining packages, not its first
	// load, drive the expander's count, the Trust Tap dialog, and a later
	// tap-wide trust; otherwise the dialog miscounts and the handled
	// package is trusted a second time.
	for _, required := range []string{
		"entry.tap = entry.tap.Without(kind, qualifiedName)",
		"entry.expander.SetSubtitle(pageview.UntrustedTap(tapName, entry.tap.Formulae, entry.tap.Casks).Subtitle)",
	} {
		if !strings.Contains(trust, required) {
			t.Errorf("trustPackage: missing %q", required)
		}
	}
	confirm := bodies["confirmTrustTap"]
	for _, required := range []string{
		"pageview.TapTrustConfirmation(entry.tap.Name, entry.tap.Count())",
		"go uh.trustTap(entry.tap, button)",
	} {
		if !strings.Contains(confirm, required) {
			t.Errorf("confirmTrustTap: missing %q", required)
		}
	}
}
