package firstrun

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBluefinCLISetupPreservesOptOutAndAppliesOnce(t *testing.T) {
	choice := NewBluefinCLISelection()
	choice.Observe(false, false)
	if !choice.Selected {
		t.Fatal("fresh setup must offer Bluefin CLI selected")
	}
	choice.Choose(false)
	choice.Observe(false, false) // a late readiness probe must not overwrite user intent
	if choice.Selected {
		t.Fatal("late probe discarded opt-out")
	}
	path := filepath.Join(t.TempDir(), "disabled")
	enable := func() error { t.Fatal("opted-out setup enabled Bluefin CLI"); return nil }
	disable := func() error { return os.WriteFile(path, []byte("off"), 0600) }
	if err := choice.Apply(enable, disable); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "off" {
		t.Fatalf("opt-out was not applied: %q, %v", content, err)
	}
	if err := choice.Apply(enable, func() error { t.Fatal("navigation reapplied completed choice"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestBluefinCLISetupFailureCanRetryAndRespectsPriorDisable(t *testing.T) {
	choice := NewBluefinCLISelection()
	choice.Observe(true, false)
	if choice.Selected {
		t.Fatal("setup reset an existing explicit opt-out")
	}
	choice.Choose(true)
	failure := errors.New("package install failed")
	if err := choice.Apply(func() error { return failure }, nil); !errors.Is(err, failure) {
		t.Fatalf("apply failure = %v", err)
	}
	path := filepath.Join(t.TempDir(), "enabled")
	if err := choice.Apply(func() error { return os.WriteFile(path, []byte("on"), 0600) }, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed choice was not retryable:", err)
	}
}

func TestBluefinCLISetupTracksLateStateUntilUserChooses(t *testing.T) {
	choice := NewBluefinCLISelection()
	choice.Observe(false, true)
	// An operation from a dismissed wizard can finish after this one starts.
	choice.Observe(true, false)
	if choice.Selected || !choice.Applied {
		t.Fatalf("late opt-out was not reflected in the new session: %+v", choice)
	}
	choice.Choose(true)
	choice.Observe(true, false)
	if !choice.Selected || choice.Applied {
		t.Fatalf("late status overrode an explicit selection: %+v", choice)
	}
}
