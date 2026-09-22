package pageview

import (
	"strings"
	"testing"
	"time"
)

func TestBootcRollbackRowDescribesTheOneDestination(t *testing.T) {
	const timestamp = "2026-08-10T20:08:01-06:00"
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	date := parsed.Local().Format("2 January 2006")

	tests := []struct {
		name      string
		version   string
		timestamp string
		wantHas   string
	}{
		{name: "version and timestamp", version: "42.20260810", timestamp: timestamp, wantHas: "version 42.20260810, released " + date},
		{name: "version only", version: "42.20260810", wantHas: "version 42.20260810"},
		{name: "timestamp only", timestamp: timestamp, wantHas: "the version from " + date},
		{name: "unreadable timestamp still names a destination", timestamp: "not-a-time", wantHas: "Return to the previous version"},
		{name: "neither", wantHas: "No previous version is kept"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := BootcRollbackRow(test.version, test.timestamp)
			if row.Title != "Go back to the previous version" {
				t.Errorf("BootcRollbackRow().Title = %q, want %q", row.Title, "Go back to the previous version")
			}
			if !strings.Contains(row.Subtitle, test.wantHas) {
				t.Errorf("BootcRollbackRow(%q, %q).Subtitle = %q, want it to contain %q",
					test.version, test.timestamp, row.Subtitle, test.wantHas)
			}
		})
	}
}

// Every populated form must say the change takes effect at a restart, since
// going back does not alter the running system.
func TestBootcRollbackRowAlwaysDefersToARestart(t *testing.T) {
	for _, row := range []Row{
		BootcRollbackRow("42", "2026-08-10T20:08:01-06:00"),
		BootcRollbackRow("42", ""),
		BootcRollbackRow("", "2026-08-10T20:08:01-06:00"),
		BootcRollbackRow("", "not-a-time"),
	} {
		if !strings.Contains(row.Subtitle, "restart") {
			t.Errorf("BootcRollbackRow().Subtitle = %q, want it to name the restart", row.Subtitle)
		}
	}
	if strings.Contains(BootcRollbackRow("", "").Subtitle, "restart") {
		t.Error("the nothing-to-return-to subtitle should not mention a restart")
	}
}

func TestBootcRollbackResultDoesNotClaimTheRunningSystemChanged(t *testing.T) {
	got := BootcRollbackResultSubtitle()
	if !strings.Contains(got, "restart") {
		t.Errorf("BootcRollbackResultSubtitle() = %q, want it to ask for a restart", got)
	}
}
