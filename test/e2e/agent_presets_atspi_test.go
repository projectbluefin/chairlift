package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestAgentModePresetsThroughATSPIVerifiesActivation drives the live Agents
// page through the same accessibility bus an assistive technology uses. The
// private fake host makes Agent Mode report ready without installing software
// or contacting the host's user systemd manager; --dry-run keeps preset
// activation from configuring or pulling a model.
func TestAgentModePresetsThroughATSPIVerifiesActivation(t *testing.T) {
	requireATSPIStack(t)

	root := repoRoot(t)
	app := filepath.Join(e2eBuildDir(t), "e2e", "chairlift")
	requireExecutable(t, app)

	runner := filepath.Join(root, "test", "e2e", "run_atspi_navigation.sh")
	requireExecutable(t, runner)
	probe := filepath.Join(root, "test", "e2e", "agent_presets_atspi_probe.py")
	requireExecutable(t, probe)
	prelaunch := filepath.Join(root, "test", "e2e", "agent_presets_prelaunch.sh")
	requireExecutable(t, prelaunch)

	outDir := t.TempDir()
	cmd := exec.Command(runner, app, outDir, "--probe", probe, filepath.Join(outDir, "chairlift.log"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(),
		"CHAIRLIFT_SCHEMA_DIR="+os.Getenv("CHAIRLIFT_SCHEMA_DIR"),
		"CHAIRLIFT_ATSPI_PRELAUNCH_HOOK="+prelaunch,
	)
	output := &lockedBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	t.Cleanup(func() {
		if cmd.Process == nil {
			return
		}
		if err := awaitSessionExit(hostDrain(), cmd.Process.Pid, shutdownTimeout, drainTimeout); err != nil {
			t.Errorf("Agent Mode AT-SPI probe session %d: %v", cmd.Process.Pid, err)
		}
	})

	if err := runWithTimeout(cmd, atspiTimeout); err != nil {
		for _, name := range []string{"chairlift.log", "probe.log", "node.log", "atspi-results.txt", "proxy_blocked.log"} {
			if contents, readErr := os.ReadFile(filepath.Join(outDir, name)); readErr == nil {
				t.Logf("%s:\n%s", name, contents)
			}
		}
		t.Fatalf("Agent Mode AT-SPI run failed: %v\noutput:\n%s", err, output.String())
	}

	resultsRaw, err := os.ReadFile(filepath.Join(outDir, "atspi-results.txt"))
	if err != nil {
		t.Fatalf("read AT-SPI results: %v", err)
	}
	resultsText := string(resultsRaw)

	records, err := parsePresetATSPIRecords(resultsText)
	if err != nil {
		t.Fatalf("parse Agent Mode AT-SPI report: %v\nreport:\n%s", err, resultsText)
	}
	for _, want := range []string{
		"PAGE\tname=Agents\tselected=1",
		"CONTROL\tname=Agent Mode",
		"STATUS\tname=Ready.",
		"PRESET\tname=Active Model",
		"PRESET\tname=Recommended Presets",
		"BUTTON\tname=Switch…",
		"DIALOG\tname=Switch Model Preset",
	} {
		if !strings.Contains(resultsText, want) {
			t.Errorf("AT-SPI report missing %q\nreport:\n%s", want, resultsText)
		}
	}

	for _, family := range []string{
		"Qwen (Recommended)",
		"Mistral / Ministral",
		"Gemma",
		"DeepSeek",
		"GPT-OSS",
	} {
		if !containsPresetChoice(records, family) {
			t.Errorf("preset choice %q is missing or has no accessible name", family)
		}
	}
	if !containsPresetRecord(records, "ACTIVATED", "family", "Gemma") {
		t.Errorf("AT-SPI did not activate the Gemma preset\nreport:\n%s", resultsText)
	}
	if !containsPresetRecordContaining(records, "MODEL", "name", "unsloth/gemma-3") {
		t.Errorf("AT-SPI did not expose the activated Gemma model in Active Model\nreport:\n%s", resultsText)
	}

	log, err := os.ReadFile(filepath.Join(outDir, "chairlift.log"))
	if err != nil {
		t.Fatalf("read ChairLift log: %v", err)
	}
	logText := string(log)
	for _, want := range []string{
		"[DRY-RUN] would configure alias bluefin-active to unsloth/gemma-3",
	} {
		if !strings.Contains(logText, want) {
			t.Errorf("Gemma dry-run activation log missing %q\nlog:\n%s", want, logText)
		}
	}

	// Verify that the live catalog lookup was intercepted and blocked so offline fallback was used
	proxyLog, err := os.ReadFile(filepath.Join(outDir, "proxy_blocked.log"))
	if err != nil {
		t.Fatalf("read proxy blocked log: %v", err)
	}
	if !strings.Contains(string(proxyLog), "huggingface.co") {
		t.Errorf("expected external model catalog fetch to be intercepted by proxy; proxy log:\n%s", proxyLog)
	}

	// Verify that neither brew nor llmman stubs were invoked with real mutation commands in dry-run mode
	llmmanLog, err := os.ReadFile(filepath.Join(outDir, "llmman_invocations.log"))
	if err == nil {
		for _, line := range strings.Split(string(llmmanLog), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "config get") {
				continue
			}
			t.Errorf("llmman stub recorded mutation call in dry-run mode: %q", line)
		}
	}
	brewLog, err := os.ReadFile(filepath.Join(outDir, "brew_invocations.log"))
	if err == nil {
		for _, line := range strings.Split(string(brewLog), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "outdated") || strings.HasPrefix(line, "tap-info") || strings.HasPrefix(line, "info") || strings.HasPrefix(line, "--version") {
				continue
			}
			t.Errorf("brew stub recorded mutation call in dry-run mode: %q", line)
		}
	}
}

type presetATSPIRecord struct {
	kind   string
	fields map[string]string
}

func parsePresetATSPIRecords(report string) ([]presetATSPIRecord, error) {
	var records []presetATSPIRecord
	for lineNumber, line := range strings.Split(strings.Trim(report, "\r\n"), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) == 0 || parts[0] == "" {
			return nil, fmt.Errorf("line %d has no record kind", lineNumber+1)
		}
		record := presetATSPIRecord{kind: parts[0], fields: make(map[string]string, len(parts)-1)}
		for _, part := range parts[1:] {
			key, value, ok := strings.Cut(part, "=")
			if !ok || key == "" {
				return nil, fmt.Errorf("line %d has malformed field %q", lineNumber+1, part)
			}
			if _, duplicate := record.fields[key]; duplicate {
				return nil, fmt.Errorf("line %d repeats field %q", lineNumber+1, key)
			}
			record.fields[key] = value
		}
		records = append(records, record)
	}
	if len(records) == 0 || records[len(records)-1].kind != "DONE" {
		return nil, fmt.Errorf("report does not end with DONE")
	}
	return records, nil
}

func containsPresetChoice(records []presetATSPIRecord, name string) bool {
	for _, record := range records {
		if record.kind == "CHOICE" && record.fields["name"] == name && record.fields["role"] != "" {
			return true
		}
	}
	return false
}

func containsPresetRecord(records []presetATSPIRecord, kind, key, value string) bool {
	for _, record := range records {
		if record.kind == kind && record.fields[key] == value {
			return true
		}
	}
	return false
}

func containsPresetRecordContaining(records []presetATSPIRecord, kind, key, fragment string) bool {
	for _, record := range records {
		if record.kind == kind && strings.Contains(record.fields[key], fragment) {
			return true
		}
	}
	return false
}

func TestParseAgentPresetATSPIRecords(t *testing.T) {
	report := "PAGE\tname=Agents\tselected=1\nCHOICE\tname=Gemma\trole=push button\nACTIVATED\tfamily=Gemma\nDONE\n"
	records, err := parsePresetATSPIRecords(report)
	if err != nil {
		t.Fatalf("parsePresetATSPIRecords() error = %v", err)
	}
	if !containsPresetChoice(records, "Gemma") || !containsPresetRecord(records, "ACTIVATED", "family", "Gemma") {
		t.Fatalf("parsed report lost its choice or activation: %#v", records)
	}
}

func TestParseAgentPresetATSPIRejectsMalformedReports(t *testing.T) {
	for _, test := range []struct {
		name   string
		report string
	}{
		{name: "missing completion", report: "PAGE\tname=Agents\n"},
		{name: "empty record kind", report: "\tname=Agents\nDONE\n"},
		{name: "malformed field", report: "PAGE\tname\nDONE\n"},
		{name: "duplicate field", report: "PAGE\tname=Agents\tname=Help\nDONE\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parsePresetATSPIRecords(test.report); err == nil {
				t.Fatalf("parsePresetATSPIRecords(%q) succeeded, want error", test.report)
			}
		})
	}
}
