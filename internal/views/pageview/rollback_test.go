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
		// composefs: the rollback's version and image creation time are
		// root-only, but the deployment exists and Roll Back is offered.
		{name: "neither readable still names a destination", wantHas: "Return to the previous version the next time you restart"},
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
		BootcRollbackRow("", ""),
	} {
		if !strings.Contains(row.Subtitle, "restart") {
			t.Errorf("BootcRollbackRow().Subtitle = %q, want it to name the restart", row.Subtitle)
		}
	}
}

// The row is shown only beside an enabled Roll Back button, so no subtitle may
// deny the destination the button acts on (#521: composefs knows neither the
// rollback's version nor its release date).
func TestBootcRollbackRowNeverDeniesItsDestination(t *testing.T) {
	for _, args := range [][2]string{{"", ""}, {"", "not-a-time"}, {"42", ""}, {"", "2026-08-10T20:08:01-06:00"}} {
		got := BootcRollbackRow(args[0], args[1]).Subtitle
		if strings.Contains(strings.ToLower(got), "no previous version") {
			t.Errorf("BootcRollbackRow(%q, %q).Subtitle = %q, denies the destination Roll Back acts on", args[0], args[1], got)
		}
	}
}

func TestBootcRollbackResultDoesNotClaimTheRunningSystemChanged(t *testing.T) {
	got := BootcRollbackResultSubtitle()
	if !strings.Contains(got, "restart") {
		t.Errorf("BootcRollbackResultSubtitle() = %q, want it to ask for a restart", got)
	}
}
