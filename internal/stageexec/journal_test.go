package stageexec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/journal"
)

// Staging writes the inactive slot, or stages a `bootc switch`. It is the
// least undoable thing ChairLift does and therefore the escalation whose
// audit record matters most — yet for a long time Stage was the one pkexec
// owner that wrote no journal entry at all, so a journal captured on a real
// system showed helper actions and a silent gap where the OS update went.
// These tests pin both halves of the fix.

func TestStageJournalsLiveInvocation(t *testing.T) {
	entries := stageWithJournal(t, t.TempDir(), false)

	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Action != "stage-script.sh" {
		t.Errorf("journalled action = %q, want the script's base name %q", entry.Action, "stage-script.sh")
	}
	if entry.Suppressed != journal.SuppressedNone {
		t.Errorf("journalled suppressed = %q, want %q", entry.Suppressed, journal.SuppressedNone)
	}
	if len(entry.Args) != 0 {
		t.Errorf("journalled args = %v, want none: the stage scripts take no arguments", entry.Args)
	}
}

// The dry-run entry must be the same argv as the live one, differing only in
// Suppressed. That is what makes the journal an assertion surface: a test can
// prove which script Stage would have run without granting privilege.
func TestStageJournalsDryRunWithTheSameArgv(t *testing.T) {
	dir := t.TempDir()
	live := stageWithJournal(t, dir, false)
	dry := stageWithJournal(t, dir, true)

	if len(dry) != 1 {
		t.Fatalf("dry-run journal has %d entries, want 1", len(dry))
	}
	if dry[0].Suppressed != journal.SuppressedDryRun {
		t.Errorf("dry-run suppressed = %q, want %q", dry[0].Suppressed, journal.SuppressedDryRun)
	}
	if !reflect.DeepEqual(dry[0].WouldRun, live[0].WouldRun) {
		t.Errorf("dry-run WouldRun = %v, live WouldRun = %v; the two must differ only in Suppressed",
			dry[0].WouldRun, live[0].WouldRun)
	}
	if len(live[0].WouldRun) != 2 || !strings.HasSuffix(live[0].WouldRun[1], "stage-script.sh") {
		t.Errorf("WouldRun = %v, want the real two-word argv [<pkexec> <script>]", live[0].WouldRun)
	}
}

// stageWithJournal runs Stage once in dir against a real successful script,
// with a fresh journal sink, and returns the entries it wrote. dir is a
// parameter so two runs can be compared on an identical script path. The "pkexec" stand-in
// is the shell, so the live path exercises a genuine process start without
// requiring pkexec or root on the gate host.
func stageWithJournal(t *testing.T, dir string, dry bool) []journal.Entry {
	t.Helper()

	script := filepath.Join(dir, "stage-script.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho staged\n"), 0o755); err != nil {
		t.Fatalf("writing stage script: %v", err)
	}

	// A distinct sink per call: dir is shared so two runs compare on one
	// script path, but each run needs its own journal to count entries.
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, journalPath)
	journal.Reset()
	t.Cleanup(journal.Reset)

	if dry {
		dryrun.Set(true)
		t.Cleanup(func() { dryrun.Set(false) })
	}

	progressCh := make(chan ProgressEvent, 8)
	done := make(chan error, 1)
	go func() { done <- Stage(context.Background(), progressCh, "/bin/sh", script) }()
	for range progressCh { //nolint:revive // drain so Stage can finish
	}
	if err := <-done; err != nil {
		t.Fatalf("Stage(dry=%v) error = %v, want nil", dry, err)
	}

	return readStageJournal(t, journalPath)
}

func readStageJournal(t *testing.T, path string) []journal.Entry {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading journal %s: %v", path, err)
	}

	var entries []journal.Entry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry journal.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decoding journal line %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}
