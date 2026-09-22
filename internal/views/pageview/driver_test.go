package pageview

import (
	"strings"
	"testing"
)

func TestGraphicsDriverRowCoversEveryState(t *testing.T) {
	tests := []struct {
		name        string
		current     string
		hardware    string
		recommended string
		wantHas     string
	}{
		{
			name:    "nothing to offer",
			current: "Standard", hardware: "AMD",
			wantHas: "Using the Standard driver for your AMD graphics",
		},
		{
			name:    "a switch is offered",
			current: "Standard", hardware: "NVIDIA + Intel", recommended: "NVIDIA (proprietary)",
			wantHas: "Switch to the NVIDIA (proprietary) driver for your NVIDIA + Intel graphics",
		},
		{
			name:        "a switch is offered without detected hardware",
			current:     "Standard",
			recommended: "NVIDIA (proprietary)",
			wantHas:     "Switch to the NVIDIA (proprietary) driver.",
		},
		{
			name:    "already on the driver",
			current: "NVIDIA (proprietary)", hardware: "NVIDIA",
			wantHas: "Using the NVIDIA (proprietary) driver",
		},
		{
			name:     "hardware known but driver unrecognized",
			hardware: "Intel",
			wantHas:  "Intel",
		},
		{
			name:    "nothing detected",
			wantHas: "No graphics hardware was detected",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := GraphicsDriverRow(test.current, test.hardware, test.recommended)
			if row.Title != "Graphics driver" {
				t.Errorf("GraphicsDriverRow().Title = %q, want %q", row.Title, "Graphics driver")
			}
			if !strings.Contains(row.Subtitle, test.wantHas) {
				t.Errorf("GraphicsDriverRow(%q, %q, %q).Subtitle = %q, want it to contain %q",
					test.current, test.hardware, test.recommended, row.Subtitle, test.wantHas)
			}
		})
	}
}

// Only the offering state may mention a restart; the informational states
// describe the machine and must not imply an action is pending. The offer
// also has to disclose that it replaces the operating system, and must not
// promise a result it cannot know.
func TestGraphicsDriverRowDisclosesTheCostOnlyWhenOffering(t *testing.T) {
	offering := GraphicsDriverRow("Standard", "NVIDIA", "NVIDIA (proprietary)")
	for _, want := range []string{"restart", "Replaces the operating system"} {
		if !strings.Contains(offering.Subtitle, want) {
			t.Errorf("the offering subtitle %q does not mention %q", offering.Subtitle, want)
		}
	}
	for _, promise := range []string{"faster", "better", "performance"} {
		if strings.Contains(strings.ToLower(offering.Subtitle), promise) {
			t.Errorf("the offering subtitle %q promises %q, which it cannot know", offering.Subtitle, promise)
		}
	}
	for _, row := range []Row{
		GraphicsDriverRow("Standard", "AMD", ""),
		GraphicsDriverRow("NVIDIA (proprietary)", "NVIDIA", ""),
		GraphicsDriverRow("", "", ""),
	} {
		if strings.Contains(row.Subtitle, "restart") {
			t.Errorf("informational subtitle %q should not mention a restart", row.Subtitle)
		}
	}
}

func TestGraphicsDriverResultDefersToARestart(t *testing.T) {
	got := GraphicsDriverResultSubtitle("NVIDIA (proprietary)")
	if !strings.Contains(got, "restart to apply") {
		t.Errorf("GraphicsDriverResultSubtitle() = %q, want it to ask for a restart", got)
	}
	if !strings.Contains(got, "NVIDIA (proprietary)") {
		t.Errorf("GraphicsDriverResultSubtitle() = %q, want it to name the driver", got)
	}
}
