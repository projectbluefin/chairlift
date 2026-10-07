package actionmsg

import (
	"errors"
	"testing"
)

// TestAskBluefinMenuReportsEveryOutcome covers the Ask Bluefin menu switch:
// a preview, a write or read-back failure, and a write that did not take
// effect each say so, and the switch never keeps an unconfirmed position.
func TestAskBluefinMenuReportsEveryOutcome(t *testing.T) {
	writeErr := errors.New("dconf write failed")
	readErr := errors.New("dconf read failed")
	for _, requested := range []bool{true, false} {
		tests := []struct {
			name     string
			dryRun   bool
			observed bool
			err      error
			readErr  error
			want     AskBluefinMenuDecision
		}{
			{
				name:     "live change observed",
				observed: requested,
				want:     AskBluefinMenuDecision{Active: requested},
			},
			{
				name:     "dry run reads back the old value",
				dryRun:   true,
				observed: !requested,
				want:     AskBluefinMenuDecision{Active: !requested, Toast: "Preview only — the menu entry was not changed."},
			},
			{
				name:     "live change not applied",
				observed: !requested,
				want:     AskBluefinMenuDecision{Active: !requested, Toast: "The Ask Bluefin menu entry did not change.", Error: true},
			},
			{
				name:     "write failed",
				observed: !requested,
				err:      writeErr,
				want:     AskBluefinMenuDecision{Active: !requested, Toast: "Could not update the Ask Bluefin menu entry.", Error: true},
			},
			{
				name:    "read-back failed after a successful write",
				readErr: readErr,
				want:    AskBluefinMenuDecision{Active: !requested, Toast: "Could not verify the Ask Bluefin menu entry.", Error: true},
			},
			{
				name:    "write and read-back failed",
				err:     writeErr,
				readErr: readErr,
				want:    AskBluefinMenuDecision{Active: !requested, Toast: "Could not update the Ask Bluefin menu entry.", Error: true},
			},
			{
				name:    "dry run with a failed read-back",
				dryRun:  true,
				readErr: readErr,
				want:    AskBluefinMenuDecision{Active: !requested, Toast: "Could not verify the Ask Bluefin menu entry.", Error: true},
			},
		}
		for _, tc := range tests {
			got := AskBluefinMenu(tc.dryRun, requested, tc.observed, tc.err, tc.readErr)
			if got != tc.want {
				t.Errorf("requested=%v %s: AskBluefinMenu() = %+v, want %+v", requested, tc.name, got, tc.want)
			}
		}
	}
}
