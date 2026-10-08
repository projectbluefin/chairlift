package pageview

import (
	"strings"
	"testing"
)

func TestSystemDiagnosticsRow(t *testing.T) {
	row := SystemDiagnosticsRow()
	if row.Title != "System diagnostics" {
		t.Errorf("SystemDiagnosticsRow().Title = %q, want %q", row.Title, "System diagnostics")
	}
	if !strings.Contains(row.Subtitle, "help request") {
		t.Errorf("SystemDiagnosticsRow().Subtitle = %q, want mention of asking for help", row.Subtitle)
	}
}

func TestDiagnosticsClipboardToast(t *testing.T) {
	toast := DiagnosticsClipboardToast()
	if !strings.Contains(toast, "clipboard") {
		t.Errorf("DiagnosticsClipboardToast() = %q, want mention of clipboard", toast)
	}
}

func TestFormatScrubbedDiagnostics(t *testing.T) {
	data := DiagnosticsData{
		OSName:     "Bluefin",
		OSVersion:  "42.20260810",
		ImageRef:   "ghcr.io/ublue-os/bluefin:latest",
		Kernel:     "6.17.4-200.fc44.x86_64",
		DesktopEnv: "GNOME",
		GPU:        "NVIDIA + Intel",
	}

	formatted := FormatScrubbedDiagnostics(data, "jorge", "/var/home/jorge")
	if !strings.Contains(formatted, "OS: Bluefin 42.20260810") {
		t.Errorf("formatted text missing OS info: %q", formatted)
	}
	if !strings.Contains(formatted, "Image: ghcr.io/ublue-os/bluefin:latest") {
		t.Errorf("formatted text missing image info: %q", formatted)
	}
	if !strings.Contains(formatted, "Kernel: 6.17.4-200.fc44.x86_64") {
		t.Errorf("formatted text missing kernel info: %q", formatted)
	}
	if !strings.Contains(formatted, "Desktop: GNOME") {
		t.Errorf("formatted text missing desktop env: %q", formatted)
	}
	if !strings.Contains(formatted, "Graphics: NVIDIA + Intel") {
		t.Errorf("formatted text missing graphics info: %q", formatted)
	}
}

// The Updates page's Details row tells a user its identifiers are the ones
// to quote when asking for help; the clipboard copy Help offers for exactly
// that purpose must carry them, and the digest must be whole — a truncated
// digest cannot be matched against a registry.
func TestFormatScrubbedDiagnosticsNamesTheBootedBuild(t *testing.T) {
	digest := "sha256:110fdf396bd1c0ffee00000000000000000000000000000000000000000000ab"
	formatted := FormatScrubbedDiagnostics(DiagnosticsData{
		OSName:        "dakota",
		OSVersion:     "testing",
		ImageRef:      "ghcr.io/projectbluefin/dakota",
		SystemVersion: "testing-20261006",
		Digest:        digest,
	}, "", "")
	if !strings.Contains(formatted, "Version: testing-20261006\n") {
		t.Errorf("formatted text missing system version: %q", formatted)
	}
	if !strings.Contains(formatted, "Build ID: "+digest+"\n") {
		t.Errorf("formatted text missing full digest: %q", formatted)
	}

	absent := FormatScrubbedDiagnostics(DiagnosticsData{OSName: "dakota"}, "", "")
	if strings.Contains(absent, "Version:") || strings.Contains(absent, "Build ID:") {
		t.Errorf("unknown version or digest must be omitted, not printed empty: %q", absent)
	}
}

func TestFormatScrubbedDiagnosticsScrubbing(t *testing.T) {
	data := DiagnosticsData{
		OSName:     "Bluefin",
		OSVersion:  "42.20260810",
		ImageRef:   "ghcr.io/ublue-os/bluefin:latest (cached at /var/home/alice/.cache)",
		Kernel:     "6.17.4",
		DesktopEnv: "GNOME",
		GPU:        "Intel",
	}

	formatted := FormatScrubbedDiagnostics(data, "alice", "/var/home/alice")
	if strings.Contains(formatted, "alice") {
		t.Errorf("formatted text leaked username/homedir: %q", formatted)
	}
	if !strings.Contains(formatted, "~/.cache") {
		t.Errorf("formatted text did not scrub to ~: %q", formatted)
	}
}

// Each title names what its configured key holds in the distribution's
// shipped configuration: website is the documentation site, chat is the
// Ask Bluefin assistant (projectbluefin/common's help_resources_group).
func TestHelpResourcesHumanActionTitles(t *testing.T) {
	resources := HelpResources(
		"https://docs.projectbluefin.io/",
		"https://issues.projectbluefin.io/",
		"https://ask.projectbluefin.io/",
	)

	want := []HelpResource{
		{Title: "Browse documentation", URL: "https://docs.projectbluefin.io/"},
		{Title: "Report a problem", URL: "https://issues.projectbluefin.io/"},
		{Title: "Ask for help", URL: "https://ask.projectbluefin.io/"},
	}
	if len(resources) != len(want) {
		t.Fatalf("len(resources) = %d, want %d", len(resources), len(want))
	}
	for i := range want {
		if resources[i] != want[i] {
			t.Errorf("resources[%d] = %#v, want %#v", i, resources[i], want[i])
		}
	}
}
