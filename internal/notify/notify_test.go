package notify

import (
	"strings"
	"testing"
)

func TestUpdateAllCompleteCoversEveryOutcome(t *testing.T) {
	tests := []struct {
		name              string
		succeeded         int
		failed            int
		restartRequired   bool
		maintenanceFailed bool
		wantTitleHas      string
		wantBodyHas       string
		wantBodyLacks     string
		wantUrgency       Urgency
	}{
		{
			name:         "nothing applied",
			wantTitleHas: "Nothing to update",
		},
		{
			name:         "everything failed",
			failed:       2,
			wantTitleHas: "Update failed",
			wantUrgency:  UrgencyHigh,
		},
		{
			name:            "partial failure without a restart",
			succeeded:       1,
			failed:          1,
			wantTitleHas:    "problems",
			wantBodyHas:     "1 part(s) updated, 1 failed.",
			wantUrgency:     UrgencyHigh,
			restartRequired: false,
		},
		{
			name:            "partial failure with a restart still pending",
			succeeded:       1,
			failed:          1,
			restartRequired: true,
			wantTitleHas:    "problems",
			wantBodyHas:     "Restart",
			wantUrgency:     UrgencyHigh,
		},
		{
			name:            "clean run staged a restart",
			succeeded:       3,
			restartRequired: true,
			wantTitleHas:    "Update complete",
			wantBodyHas:     "Restart",
		},
		{
			// The run starts only with updates pending, so a clean run
			// installed them; "already up to date" was false.
			name:          "clean run installed updates",
			succeeded:     3,
			wantTitleHas:  "Update complete",
			wantBodyHas:   "All updates were installed.",
			wantBodyLacks: "already",
		},
		{
			name:              "updates installed but cleanup failed",
			succeeded:         1,
			maintenanceFailed: true,
			wantTitleHas:      "problems",
			wantBodyHas:       "cleaning up afterwards failed",
			wantBodyLacks:     "already",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := UpdateAllComplete(test.succeeded, test.failed, test.restartRequired, test.maintenanceFailed)
			if !strings.Contains(got.Title, test.wantTitleHas) {
				t.Errorf("Title = %q, want it to contain %q", got.Title, test.wantTitleHas)
			}
			if test.wantBodyHas != "" && !strings.Contains(got.Body, test.wantBodyHas) {
				t.Errorf("Body = %q, want it to contain %q", got.Body, test.wantBodyHas)
			}
			if test.wantBodyLacks != "" && strings.Contains(got.Body, test.wantBodyLacks) {
				t.Errorf("Body = %q, must not contain %q", got.Body, test.wantBodyLacks)
			}
			if got.Urgency != test.wantUrgency {
				t.Errorf("Urgency = %v, want %v", got.Urgency, test.wantUrgency)
			}
		})
	}
}

// A total failure and a partial failure must read differently: one says
// nothing happened, the other says something did, since a user acts on
// those two situations differently.
func TestUpdateAllCompleteDistinguishesTotalFromPartialFailure(t *testing.T) {
	total := UpdateAllComplete(0, 2, false, false)
	partial := UpdateAllComplete(1, 1, false, false)
	if total.Title == partial.Title {
		t.Errorf("total failure and partial failure share the same title %q", total.Title)
	}
}
