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
