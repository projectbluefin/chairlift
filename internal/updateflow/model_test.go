package updateflow

import (
	"errors"
	"testing"
	"time"
)

func TestStateDecisionTable(t *testing.T) {
	item := Item{Name: "one", CurrentVersion: "1", AvailableVersion: "2"}
	secondItem := Item{Name: "two", CurrentVersion: "2", AvailableVersion: "3"}
	checkErr := errors.New("check failed")
	applyErr := errors.New("apply failed")

	tests := []struct {
		name        string
		snapshot    Snapshot
		wantPhase   Phase
		wantAction  Action
		wantTotal   int
		wantFailed  []SourceID
		wantRestart bool
	}{
		{
			name: "no available sources",
			snapshot: Snapshot{Sources: []SourceState{
				{ID: Applications, Configured: true, Available: false, Enabled: false, Items: []Item{item}},
				{ID: DeveloperTools, Configured: false, Available: true, Enabled: false, Items: []Item{item}},
			}},
			wantPhase: PhaseReady, wantAction: ActionNone,
		},
		{
			name: "administrator disabled and runtime unavailable remain distinct",
			snapshot: Snapshot{Sources: []SourceState{
				{ID: Applications, Configured: false, Available: true, Enabled: false},
				{ID: DeveloperTools, Configured: true, Available: false, Enabled: false},
			}},
			wantPhase: PhaseReady, wantAction: ActionNone,
		},
		{
			name: "all checks current",
			snapshot: Snapshot{Sources: []SourceState{
				{ID: Applications, Configured: true, Available: true, Enabled: true},
				{ID: DeveloperTools, Configured: true, Available: true, Enabled: true},
			}},
			wantPhase: PhaseReady, wantAction: ActionCheck,
		},
		{
			name: "one update",
			snapshot: Snapshot{Sources: []SourceState{
				{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{item}},
			}},
			wantPhase: PhaseReady, wantAction: ActionUpdateAll, wantTotal: 1,
		},
		{
			name: "many updates",
			snapshot: Snapshot{Sources: []SourceState{
				{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{item, secondItem}},
				{ID: DeveloperTools, Configured: true, Available: true, Enabled: true, Items: []Item{item}},
			}},
			wantPhase: PhaseReady, wantAction: ActionUpdateAll, wantTotal: 3,
		},
		{
			name: "existing restart pending with no new updates",
			snapshot: Snapshot{Sources: []SourceState{
				{ID: OperatingSystem, Configured: true, Available: true, Enabled: true, RestartRequired: true},
			}},
			wantPhase: PhaseRestartRequired, wantAction: ActionRestart, wantRestart: true,
		},
		{
			name: "one failed check retains previous items",
			snapshot: Snapshot{Sources: []SourceState{
				{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{item}, CheckErr: checkErr},
				{ID: DeveloperTools, Configured: true, Available: true, Enabled: true},
			}},
			wantPhase: PhaseCheckFailed, wantAction: ActionCheck, wantTotal: 1,
			wantFailed: []SourceID{Applications},
		},
		{
			name: "all failed checks",
			snapshot: Snapshot{Sources: []SourceState{
				{ID: Applications, Configured: true, Available: true, Enabled: true, CheckErr: checkErr},
				{ID: DeveloperTools, Configured: true, Available: true, Enabled: true, CheckErr: checkErr},
			}},
			wantPhase: PhaseCheckFailed, wantAction: ActionCheck,
			wantFailed: []SourceID{Applications, DeveloperTools},
		},
		{
			name: "update in progress",
			snapshot: Snapshot{
				Action: ActionUpdateAll,
				Sources: []SourceState{
					{ID: Applications, Configured: true, Available: true, Enabled: true, Updating: true, Items: []Item{item}},
				},
			},
			wantPhase: PhaseUpdating, wantAction: ActionNone, wantTotal: 1,
		},
		{
			name: "partial mutation failure",
			snapshot: Snapshot{
				Action: ActionUpdateAll,
				Sources: []SourceState{
					{ID: Applications, Configured: true, Available: true, Enabled: true, Completed: true},
					{ID: DeveloperTools, Configured: true, Available: true, Enabled: true, Items: []Item{item}, ApplyErr: applyErr},
				},
				CompletedSources: []SourceID{Applications},
			},
			wantPhase: PhasePartialFailure, wantAction: ActionRetryFailed, wantTotal: 1,
			wantFailed: []SourceID{DeveloperTools},
		},
		{
			name: "complete mutation failure",
			snapshot: Snapshot{
				Action: ActionUpdateAll,
				Sources: []SourceState{
					{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{item}, ApplyErr: applyErr},
					{ID: DeveloperTools, Configured: true, Available: true, Enabled: true, Items: []Item{secondItem}, ApplyErr: applyErr},
				},
			},
			wantPhase: PhasePartialFailure, wantAction: ActionRetryFailed, wantTotal: 2,
			wantFailed: []SourceID{Applications, DeveloperTools},
		},
		{
			name: "restart wins after successful update",
			snapshot: Snapshot{Sources: []SourceState{
				{ID: OperatingSystem, Configured: true, Available: true, Enabled: true, RestartRequired: true, Completed: true},
			}},
			wantPhase: PhaseRestartRequired, wantAction: ActionRestart, wantRestart: true,
		},
		{
			name: "dry run success remains actionable",
			snapshot: Snapshot{
				Preview: true,
				Sources: []SourceState{
					{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{item}},
				},
			},
			wantPhase: PhaseReady, wantAction: ActionUpdateAll, wantTotal: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := derive(test.snapshot)
			if got.Phase != test.wantPhase {
				t.Fatalf("phase = %v, want %v", got.Phase, test.wantPhase)
			}
			if got.Action != test.wantAction {
				t.Fatalf("action = %v, want %v", got.Action, test.wantAction)
			}
			if got.TotalUpdates != test.wantTotal {
				t.Fatalf("total updates = %d, want %d", got.TotalUpdates, test.wantTotal)
			}
			if !sameIDs(got.FailedSources, test.wantFailed) {
				t.Fatalf("failed sources = %#v, want %#v", got.FailedSources, test.wantFailed)
			}
			if test.name == "complete mutation failure" && got.CompletedSources == nil {
				t.Fatal("complete mutation failure returned a nil completed-source slice")
			}
			if got.RestartRequired() != test.wantRestart {
				t.Fatalf("restart required = %v, want %v", got.RestartRequired(), test.wantRestart)
			}
		})
	}
}

func TestSnapshotCloneDeepCopiesMutableState(t *testing.T) {
	when := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	original := Snapshot{
		Generation: 7,
		Sources: []SourceState{{
			ID:    Applications,
			Items: []Item{{Name: "one"}},
		}},
		CompletedSources: []SourceID{Applications},
		FailedSources:    []SourceID{DeveloperTools},
		LastChecked:      when,
	}

	clone := original.clone()
	clone.Sources[0].Items[0].Name = "changed"
	clone.Sources = append(clone.Sources, SourceState{ID: OperatingSystem})
	clone.CompletedSources[0] = OperatingSystem
	clone.FailedSources[0] = OperatingSystem

	if original.Sources[0].Items[0].Name != "one" {
		t.Fatal("clone shares item storage")
	}
	if len(original.Sources) != 1 {
		t.Fatal("clone shares source storage")
	}
	if original.CompletedSources[0] != Applications {
		t.Fatal("clone shares completed-source storage")
	}
	if original.FailedSources[0] != DeveloperTools {
		t.Fatal("clone shares failed-source storage")
	}
	if !original.LastChecked.Equal(when) {
		t.Fatal("clone changed timestamp")
	}
}

func TestApplyRetryTargetsOnlyApplyFailures(t *testing.T) {
	snapshot := Snapshot{
		Action: ActionRetryFailed,
		Sources: []SourceState{
			{ID: Applications, Enabled: true, Items: []Item{{Name: "completed"}}, Completed: true},
			{ID: DeveloperTools, Enabled: true, Items: []Item{{Name: "failed"}}, ApplyErr: errors.New("nope")},
		},
	}

	got := retrySources(snapshot)
	if !sameIDs(got, []SourceID{DeveloperTools}) {
		t.Fatalf("retry sources = %#v, want %#v", got, []SourceID{DeveloperTools})
	}
}

func sameIDs(got, want []SourceID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
