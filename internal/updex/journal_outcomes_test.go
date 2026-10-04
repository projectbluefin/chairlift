package updex

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/journal"
	"github.com/projectbluefin/chairlift/internal/updexhelper"
)

func writeCustomFakePkexec(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-pkexec")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake pkexec: %v", err)
	}
	return path
}

func withJournalSink(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, path)
	journal.Reset()
	t.Cleanup(journal.Reset)
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	return path
}

func readJournal(t *testing.T, path string) []journal.Entry {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening journal %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	var entries []journal.Entry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry journal.Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("decoding journal line %q: %v", scanner.Text(), err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning journal: %v", err)
	}
	return entries
}

// The updex helper calls the updex library in-process and runs no
// subprocess, so a successful outcome carries no derived commands.
func TestRunHelperJournalsSuccessWithoutDerivedCommands(t *testing.T) {
	journalPath := withJournalSink(t)

	fakePkexec := writeCustomFakePkexec(t, "#!/bin/sh\necho 'enabled feature demo'\nexit 0\n")

	stdout, _, err := runHelper(context.Background(), fakePkexec, updexhelper.CommandEnableFeature, "demo")
	if err != nil {
		t.Fatalf("runHelper returned error: %v", err)
	}
	if stdout != "enabled feature demo\n" {
		t.Errorf("stdout = %q, want the helper's output unchanged", stdout)
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2 (dispatch + outcome)", len(entries))
	}
	if dispatch := entries[0]; dispatch.Action != updexhelper.CommandEnableFeature || dispatch.Suppressed != journal.SuppressedNone {
		t.Errorf("dispatch = %+v, want action enable-feature, suppressed no", dispatch)
	}
	outcome := entries[1]
	if outcome.Outcome != journal.OutcomeSucceeded || len(outcome.Executed) != 0 {
		t.Errorf("outcome = %+v, want succeeded with no executed commands", outcome)
	}
}

func TestRunHelperJournalsRefusedOnExit126(t *testing.T) {
	journalPath := withJournalSink(t)

	script := "#!/bin/sh\nexit 126\n"
	fakePkexec := writeCustomFakePkexec(t, script)

	_, _, err := runHelper(context.Background(), fakePkexec, updexhelper.CommandEnableFeature, "demo")
	if err == nil {
		t.Fatal("runHelper = nil, want error on exit 126")
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2", len(entries))
	}

	outcome := entries[1]
	if outcome.Action != updexhelper.CommandEnableFeature || outcome.Outcome != journal.OutcomeRefused {
		t.Errorf("outcome = %+v, want action enable-feature, outcome refused", outcome)
	}
	if outcome.ExitCode == nil || *outcome.ExitCode != 126 {
		t.Errorf("outcome.ExitCode = %v, want 126", outcome.ExitCode)
	}
}

func TestRunHelperJournalsFailedOnNonZeroExit(t *testing.T) {
	journalPath := withJournalSink(t)

	script := "#!/bin/sh\necho 'updex failed' >&2\nexit 1\n"
	fakePkexec := writeCustomFakePkexec(t, script)

	_, _, err := runHelper(context.Background(), fakePkexec, updexhelper.CommandEnableFeature, "demo")
	if err == nil {
		t.Fatal("runHelper = nil, want error on exit 1")
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2", len(entries))
	}

	outcome := entries[1]
	if outcome.Action != updexhelper.CommandEnableFeature || outcome.Outcome != journal.OutcomeFailed {
		t.Errorf("outcome = %+v, want action enable-feature, outcome failed", outcome)
	}
	if outcome.ExitCode == nil || *outcome.ExitCode != 1 {
		t.Errorf("outcome.ExitCode = %v, want 1", outcome.ExitCode)
	}
}

func TestRunHelperJournalsCancelledOnContextCancel(t *testing.T) {
	journalPath := withJournalSink(t)

	script := "#!/bin/sh\nexec sleep 30\n"
	fakePkexec := writeCustomFakePkexec(t, script)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	_, _, err := runHelper(ctx, fakePkexec, updexhelper.CommandEnableFeature, "demo")
	if err == nil {
		t.Fatal("runHelper = nil, want error on cancelled context")
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2", len(entries))
	}

	outcome := entries[1]
	if outcome.Action != updexhelper.CommandEnableFeature || outcome.Outcome != journal.OutcomeCancelled {
		t.Errorf("outcome = %+v, want action enable-feature, outcome cancelled", outcome)
	}
}
