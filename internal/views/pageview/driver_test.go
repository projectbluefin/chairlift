package pageview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/gpu"
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
			wantHas: "No graphics hardware found.",
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

// A virtual machine has no graphics hardware, and gpu.Set.Describe answers
// with a sentence for that case. The row used to interpolate it into "for
// your No graphics hardware detected graphics" (#489).
func TestGraphicsDriverRowWithoutDetectedHardware(t *testing.T) {
	none := (gpu.Set{}).Describe()
	if got, want := GraphicsDriverRow("Standard", none, "").Subtitle, "Using the Standard driver."; got != want {
		t.Errorf("GraphicsDriverRow(%q, %q, \"\").Subtitle = %q, want %q", "Standard", none, got, want)
	}
	if got, want := GraphicsDriverRow("", none, "").Subtitle, "No graphics hardware found."; got != want {
		t.Errorf("GraphicsDriverRow(\"\", %q, \"\").Subtitle = %q, want %q", none, got, want)
	}
	if got := GraphicsDriverRow("Standard", none, "NVIDIA (proprietary)").Subtitle; strings.Contains(got, none) {
		t.Errorf("offering subtitle %q interpolates the no-hardware sentence", got)
	}
}

// Only the offering state may mention a restart; the informational states
// describe the machine and must not imply an action is pending. The offer
// must not promise a result it cannot know. (The Advanced group's
// description says both controls change the whole operating system.)
func TestGraphicsDriverRowDisclosesTheCostOnlyWhenOffering(t *testing.T) {
	offering := GraphicsDriverRow("Standard", "NVIDIA", "NVIDIA (proprietary)")
	if !strings.Contains(strings.ToLower(offering.Subtitle), "restart") {
		t.Errorf("the offering subtitle %q does not mention a restart", offering.Subtitle)
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
	if !strings.Contains(strings.ToLower(got), "restart") {
		t.Errorf("GraphicsDriverResultSubtitle() = %q, want it to ask for a restart", got)
	}
	if !strings.Contains(got, "NVIDIA (proprietary)") {
		t.Errorf("GraphicsDriverResultSubtitle() = %q, want it to name the driver", got)
	}
}
