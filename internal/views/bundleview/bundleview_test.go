package bundleview

import (
	"sync"
	"testing"

	"github.com/projectbluefin/chairlift/internal/homebrew"
)

func TestPresentEnumeratesLoadOutcomes(t *testing.T) {
	tests := []struct {
		name               string
		count              int
		warning            string
		homebrewAvailable  bool
		wantDescription    string
		wantPlaceholder    string
		wantPlaceholderSub string
	}{
		{
			name:               "empty",
			homebrewAvailable:  true,
			wantDescription:    "No Brew bundles found",
			wantPlaceholder:    "No bundles available",
			wantPlaceholderSub: "Check the configured bundles_paths directories",
		},
		{
			name:               "empty with errors",
			warning:            "permission denied",
			homebrewAvailable:  true,
			wantDescription:    "Brew bundles could not be loaded",
			wantPlaceholder:    "Bundles unavailable",
			wantPlaceholderSub: "permission denied",
		},
		{
			name:              "one",
			count:             1,
			homebrewAvailable: true,
			wantDescription:   "1 Brew bundle available",
		},
		{
			name:              "multiple",
			count:             2,
			homebrewAvailable: true,
			wantDescription:   "2 Brew bundles available",
		},
		{
			name:              "partial",
			count:             2,
			warning:           "permission denied",
			homebrewAvailable: true,
			wantDescription:   "2 Brew bundles available; some configured paths could not be read: permission denied",
		},
		{
			name:              "one partial",
			count:             1,
			warning:           "permission denied",
			homebrewAvailable: true,
			wantDescription:   "1 Brew bundle available; some configured paths could not be read: permission denied",
		},
		{
			name:               "empty without homebrew",
			wantDescription:    "No Brew bundles found. Homebrew is not installed; install actions are disabled.",
			wantPlaceholder:    "No bundles available",
			wantPlaceholderSub: "Check the configured bundles_paths directories",
		},
		{
			name:            "one without homebrew",
			count:           1,
			wantDescription: "1 Brew bundle available. Homebrew is not installed; install actions are disabled.",
		},
		{
			name:            "partial without homebrew",
			count:           2,
			warning:         "permission denied",
			wantDescription: "2 Brew bundles available; some configured paths could not be read: permission denied. Homebrew is not installed; install actions are disabled.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Present(tt.count, tt.warning, tt.homebrewAvailable)
			if got.Description != tt.wantDescription {
				t.Errorf("Present() description = %q, want %q", got.Description, tt.wantDescription)
			}
			if got.PlaceholderTitle != tt.wantPlaceholder {
				t.Errorf("Present() placeholder title = %q, want %q", got.PlaceholderTitle, tt.wantPlaceholder)
			}
			if got.PlaceholderSubtitle != tt.wantPlaceholderSub {
				t.Errorf("Present() placeholder subtitle = %q, want %q", got.PlaceholderSubtitle, tt.wantPlaceholderSub)
			}
		})
	}
}

func TestGateAllowsOnlyOneConcurrentAction(t *testing.T) {
	var gate InstallGate
	const callers = 64

	start := make(chan struct{})
	results := make(chan bool, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- gate.TryStart()
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	acquired := 0
	for result := range results {
		if result {
			acquired++
		}
	}
	if acquired != 1 {
		t.Fatalf("concurrent TryStart acquisitions = %d, want exactly 1", acquired)
	}
}

func TestGateResetAndCompletion(t *testing.T) {
	var gate InstallGate
	if !gate.TryStart() {
		t.Fatal("zero-value gate did not start")
	}
	gate.Reset()
	if !gate.TryStart() {
		t.Fatal("reset gate did not restart")
	}
	gate.Complete()
	if gate.TryStart() {
		t.Fatal("completed gate restarted")
	}
	gate.Reset()
	if gate.TryStart() {
		t.Fatal("reset reopened a completed gate")
	}
}

func TestGateCompleteClosesAnIdleGate(t *testing.T) {
	var gate InstallGate
	gate.Complete()
	if gate.TryStart() {
		t.Fatal("gate completed while idle still started")
	}
	if gate.IsIdle() {
		t.Fatal("completed gate reported idle")
	}
}

func TestGateIsIdle(t *testing.T) {
	var gate InstallGate
	if !gate.IsIdle() {
		t.Fatal("zero-value gate did not report idle")
	}
	if !gate.TryStart() {
		t.Fatal("zero-value gate did not start")
	}
	if gate.IsIdle() {
		t.Fatal("running gate reported idle")
	}
	gate.Reset()
	if !gate.IsIdle() {
		t.Fatal("reset gate did not report idle")
	}
}

func TestRowActionReflectsStatusAndHomebrew(t *testing.T) {
	cases := []struct {
		name              string
		status            homebrew.BundleStatus
		homebrewAvailable bool
		wantLabel         string
		wantSensitive     bool
		wantCompleted     bool
	}{
		{
			name:              "installed with brew available",
			status:            homebrew.BundleInstalled,
			homebrewAvailable: true,
			wantLabel:         "Installed",
			wantSensitive:     false,
			wantCompleted:     true,
		},
		{
			name:              "update available with brew available",
			status:            homebrew.BundleUpdateAvailable,
			homebrewAvailable: true,
			wantLabel:         "Update",
			wantSensitive:     true,
			wantCompleted:     false,
		},
		{
			name:              "not installed with brew available",
			status:            homebrew.BundleNotInstalled,
			homebrewAvailable: true,
			wantLabel:         "Install",
			wantSensitive:     true,
			wantCompleted:     false,
		},
		{
			name:              "indeterminate with brew available",
			status:            homebrew.BundleIndeterminate,
			homebrewAvailable: true,
			wantLabel:         "Install",
			wantSensitive:     true,
			wantCompleted:     false,
		},
		{
			name:              "installed without brew available",
			status:            homebrew.BundleInstalled,
			homebrewAvailable: false,
			wantLabel:         "Install",
			wantSensitive:     false,
			wantCompleted:     false,
		},
		{
			name:              "not installed without brew available",
			status:            homebrew.BundleNotInstalled,
			homebrewAvailable: false,
			wantLabel:         "Install",
			wantSensitive:     false,
			wantCompleted:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RowAction(tc.status, tc.homebrewAvailable)
			if got.Label != tc.wantLabel {
				t.Errorf("RowAction() Label = %q, want %q", got.Label, tc.wantLabel)
			}
			if got.Sensitive != tc.wantSensitive {
				t.Errorf("RowAction() Sensitive = %v, want %v", got.Sensitive, tc.wantSensitive)
			}
			if got.Completed != tc.wantCompleted {
				t.Errorf("RowAction() Completed = %v, want %v", got.Completed, tc.wantCompleted)
			}
		})
	}
}
