package pageview

import (
	"strings"
	"testing"
)

func TestAIStackRowNamesTheGraphicsHardwareWithoutNamingTheComputeStack(t *testing.T) {
	row := AIStackRow("AMD", true)

	if !strings.Contains(row.Subtitle, "AMD") {
		t.Errorf("subtitle does not say which graphics hardware helps: %q", row.Subtitle)
	}
	if strings.Contains(row.Subtitle, "ROCm") || strings.Contains(row.Title, "ROCm") {
		t.Errorf("the compute stack's name belongs in Details, not the switch row: %q / %q", row.Title, row.Subtitle)
	}
}

func TestAIStackRowWarnsThatAMachineWithoutAGraphicsCardIsSlow(t *testing.T) {
	row := AIStackRow("None detected", false)

	if !strings.Contains(row.Subtitle, "slow") {
		t.Errorf("subtitle does not warn that answers will be slow: %q", row.Subtitle)
	}
	if strings.Contains(row.Subtitle, "None detected") {
		t.Errorf("subtitle repeats the detection placeholder as if it were hardware: %q", row.Subtitle)
	}
}

func TestAIStackRowAlwaysWarnsAboutTheDownload(t *testing.T) {
	for _, accelerated := range []bool{true, false} {
		row := AIStackRow("NVIDIA", accelerated)
		if !strings.Contains(row.Subtitle, "gigabytes") {
			t.Errorf("AIStackRow(accelerated=%v) does not warn about the download size: %q", accelerated, row.Subtitle)
		}
	}
}

func TestAIStackGroupDescriptionSaysNothingIsSentToACloud(t *testing.T) {
	description := AIStackGroupDescription()

	if !strings.Contains(description, "cloud") {
		t.Errorf("group description does not address where the data goes: %q", description)
	}
}

func TestAIStackResultSubtitleDoesNotClaimTheModelWasDeleted(t *testing.T) {
	stopped := AIStackResultSubtitle(false)

	if !strings.Contains(stopped, "kept") {
		t.Errorf("stopping implies the download was discarded: %q", stopped)
	}
}

// A failed stop is the one outcome that must not read as success: the unit is
// deliberately preserved when the service cannot be proven stopped, so the
// text has to keep saying the model is running.
func TestAIStackFailureTextDoesNotClaimTheModelStopped(t *testing.T) {
	subtitle := AIStackFailureSubtitle(false)
	toast := AIStackFailureToast(false)

	for name, text := range map[string]string{"subtitle": subtitle, "toast": toast} {
		lower := strings.ToLower(text)
		if !strings.Contains(lower, "still running") {
			t.Errorf("%s does not say the model is still running: %q", name, text)
		}
		if !strings.Contains(lower, "nothing was removed") {
			t.Errorf("%s does not say the preserved unit was left alone: %q", name, text)
		}
	}
}

func TestAIStackFailedStartSaysNothingChanged(t *testing.T) {
	if got := AIStackFailureSubtitle(true); !strings.Contains(got, "Nothing") {
		t.Errorf("a failed start does not say nothing changed: %q", got)
	}
	if got := AIStackFailureToast(true); !strings.Contains(got, "Nothing") {
		t.Errorf("a failed start toast does not say nothing changed: %q", got)
	}
}

func TestAIStackDetailsGiveTheAddressAndTheModel(t *testing.T) {
	rows := AIStackDetails(AIStackFacts{
		Model:       "ollama://llama3.2:3b",
		Hardware:    "AMD",
		Accelerator: "ROCm",
		Accelerated: true,
		Port:        8080,
		Available:   true,
	})

	found := map[string]string{}
	for _, row := range rows {
		found[row.Title] = row.Subtitle
	}

	if found["Model"] != "llama3.2:3b" {
		t.Errorf("model row = %q, want the bare model name", found["Model"])
	}
	if !strings.Contains(found["Address for other apps"], "8080") {
		t.Errorf("address row does not carry the port: %q", found["Address for other apps"])
	}
	if found["Graphics acceleration"] != "AMD (ROCm)" {
		t.Errorf("acceleration row = %q, want vendor and stack", found["Graphics acceleration"])
	}
	if _, ok := found["Missing software"]; ok {
		t.Error("an available host was told software is missing")
	}
}

func TestAIStackDetailsNameWhatIsMissingWhenTheFeatureCannotRun(t *testing.T) {
	rows := AIStackDetails(AIStackFacts{Model: "ollama://x", Port: 8080})

	if len(rows) == 0 || rows[0].Title != "Missing software" {
		t.Fatalf("the reason the feature is unavailable is not the first detail: %+v", rows)
	}
	if rows[0].Subtitle == "" {
		t.Error("the missing software is not named")
	}
}

func TestAIStackAccelerationDetailDoesNotStutterTheVendor(t *testing.T) {
	rows := AIStackDetails(AIStackFacts{
		Model:       "ollama://x",
		Hardware:    "Intel",
		Accelerator: "Intel oneAPI",
		Accelerated: true,
		Port:        8080,
		Available:   true,
	})

	for _, row := range rows {
		if row.Title != "Graphics acceleration" {
			continue
		}
		if row.Subtitle != "Intel oneAPI" {
			t.Errorf("acceleration row = %q, want the stack name alone", row.Subtitle)
		}
	}
}

func TestAIStackAccelerationDetailSaysWhatRunsItWithoutAGPU(t *testing.T) {
	rows := AIStackDetails(AIStackFacts{Model: "ollama://x", Hardware: "None detected", Accelerator: "CPU", Port: 8080, Available: true})

	for _, row := range rows {
		if row.Title != "Graphics acceleration" {
			continue
		}
		if !strings.Contains(row.Subtitle, "processor") {
			t.Errorf("acceleration row = %q, want it to say the processor runs it", row.Subtitle)
		}
	}
}

func TestAIModelNameDropsTheSourceScheme(t *testing.T) {
	tests := map[string]string{
		"ollama://llama3.2:3b":       "llama3.2:3b",
		"huggingface://org/model":    "org/model",
		"llama3.2:3b":                "llama3.2:3b",
		"  ollama://llama3.2:3b    ": "llama3.2:3b",
		"":                           "Not configured",
		"ollama://":                  "ollama://",
	}

	for ref, want := range tests {
		if got := AIModelName(ref); got != want {
			t.Errorf("AIModelName(%q) = %q, want %q", ref, got, want)
		}
	}
}
