package liverystate

import (
	"reflect"
	"testing"
)

// The three attempt kinds share one transition table because they share one
// rule: only a live success may replace confirmed state. Each is exercised
// independently so a future change to one call site cannot quietly change the
// others' contract.
func TestOutcomesEnumerateEveryAttemptKind(t *testing.T) {
	tests := []struct {
		name   string
		saved  bool
		dryRun bool
		want   Outcome
	}{
		{
			name: "dry-run preview commits nothing",
			// The setting was logged as "would set" and SetString returned
			// nil, but nothing persisted: the page must keep showing the real
			// selection.
			saved:  true,
			dryRun: true,
		},
		{
			name:  "completely unchanged state keeps the last confirmed value",
			saved: false,
		},
		{
			name:  "live success commits the candidate",
			saved: true,
			want:  Outcome{Commit: true},
		},
		{
			name:   "dry-run after a failure still commits nothing",
			saved:  false,
			dryRun: true,
		},
	}

	kinds := map[string]func(Result, bool) Outcome{
		"selection": Selection,
		"toggle":    Toggle,
		"rotation":  Rotation,
	}
	for kind, decide := range kinds {
		t.Run(kind, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					got := decide(Result{Saved: tt.saved}, tt.dryRun)
					if !reflect.DeepEqual(got, tt.want) {
						t.Fatalf("%s(Result{Saved:%v}, dryRun=%v) = %#v, want %#v",
							kind, tt.saved, tt.dryRun, got, tt.want)
					}
				})
			}
		})
	}
}
