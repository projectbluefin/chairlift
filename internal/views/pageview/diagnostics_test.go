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
	if !strings.Contains(row.Subtitle, "support requests") {
		t.Errorf("SystemDiagnosticsRow().Subtitle = %q, want mention of support requests", row.Subtitle)
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

func TestHelpResourcesHumanActionTitles(t *testing.T) {
	resources := HelpResources(
		"https://projectbluefin.io",
		"https://github.com/projectbluefin/dakota/issues",
		"https://docs.projectbluefin.io",
	)

	if len(resources) != 3 {
		t.Fatalf("len(resources) = %d, want 3", len(resources))
	}
	if resources[0].Title != "Visit project website" {
		t.Errorf("resources[0].Title = %q, want 'Visit project website'", resources[0].Title)
	}
	if resources[1].Title != "Report a problem" {
		t.Errorf("resources[1].Title = %q, want 'Report a problem'", resources[1].Title)
	}
	if resources[2].Title != "Browse documentation" {
		t.Errorf("resources[2].Title = %q, want 'Browse documentation'", resources[2].Title)
	}
}
