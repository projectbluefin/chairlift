package updateflow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/userprefs"
)

func TestCoordinatorChecksStartConcurrently(t *testing.T) {
	started := make(chan SourceID, 2)
	release := make(chan struct{})
	providers := []*testProvider{
		{
			id:        Applications,
			available: true,
			check: func(context.Context) (CheckResult, error) {
				started <- Applications
				<-release
				return CheckResult{}, nil
			},
		},
		{
			id:        DeveloperTools,
			available: true,
			check: func(context.Context) (CheckResult, error) {
				started <- DeveloperTools
				<-release
				return CheckResult{}, nil
			},
		},
	}
	c := New(providerInterfaces(providers), nil)

	result := make(chan Snapshot, 1)
	go func() {
		result <- c.Check(context.Background(), allPreferences(), enabledConfiguration(), nil)
	}()

	waitForSources(t, started, Applications, DeveloperTools)
	close(release)

	select {
	case got := <-result:
		if got.Phase != PhaseReady {
			t.Fatalf("phase = %v, want %v", got.Phase, PhaseReady)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent check did not finish")
	}
}

func TestCoordinatorNewerCheckPublishesFinalStateOnly(t *testing.T) {
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	var calls atomic.Int32
	provider := &testProvider{
		id:        Applications,
		available: true,
		check: func(context.Context) (CheckResult, error) {
			if calls.Add(1) == 1 {
				close(oldStarted)
				<-releaseOld
				return CheckResult{Items: []Item{{Name: "old"}}}, nil
			}
			return CheckResult{Items: []Item{{Name: "new"}}}, nil
		},
	}
	c := New([]Provider{provider}, nil)

	var mu sync.Mutex
	var published []Snapshot
	publish := func(snapshot Snapshot) {
		mu.Lock()
		published = append(published, snapshot)
		mu.Unlock()
	}

	oldResult := make(chan Snapshot, 1)
	go func() {
		oldResult <- c.Check(context.Background(), allPreferences(), enabledConfiguration(), publish)
	}()
	<-oldStarted

	newResult := make(chan Snapshot, 1)
	go func() {
		newResult <- c.Check(context.Background(), allPreferences(), enabledConfiguration(), publish)
	}()

	var current Snapshot
	select {
	case current = <-newResult:
	case <-time.After(time.Second):
		t.Fatal("newer check did not finish")
	}
	close(releaseOld)

	select {
	case <-oldResult:
	case <-time.After(time.Second):
		t.Fatal("older check did not finish")
	}

	if len(current.Sources) != 1 || len(current.Sources[0].Items) != 1 ||
		current.Sources[0].Items[0].Name != "new" {
		t.Fatalf("newer result = %#v, want new item", current)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, snapshot := range published {
		if snapshot.Generation > current.Generation {
			t.Fatalf("published generation %d after current generation %d", snapshot.Generation, current.Generation)
		}
		if snapshot.Generation == current.Generation &&
			snapshot.Phase == PhaseReady &&
			len(snapshot.Sources[0].Items) == 1 &&
			snapshot.Sources[0].Items[0].Name == "old" {
			t.Fatal("older final state was published as the newer generation")
		}
	}
}

func TestCoordinatorFailedCheckRetainsLastKnownItemsAndRestart(t *testing.T) {
	checkErr := errors.New("check failed")
	var checks atomic.Int32
	provider := &testProvider{
		id:        Applications,
		available: true,
		check: func(context.Context) (CheckResult, error) {
			if checks.Add(1) == 1 {
				return CheckResult{
					Items:           []Item{{Name: "known"}},
					RestartRequired: true,
				}, nil
			}
			return CheckResult{
				Items:           []Item{{Name: "partial"}},
				RestartRequired: false,
			}, checkErr
		},
	}
	c := New([]Provider{provider}, nil)

	c.Check(context.Background(), allPreferences(), enabledConfiguration(), nil)
	got := c.Check(context.Background(), allPreferences(), enabledConfiguration(), nil)

	source := got.Sources[0]
	if !errors.Is(source.CheckErr, checkErr) {
		t.Fatalf("check error = %v, want %v", source.CheckErr, checkErr)
	}
	if len(source.Items) != 1 || source.Items[0].Name != "known" {
		t.Fatalf("failed-check items = %#v, want last known item", source.Items)
	}
	if !source.RestartRequired {
		t.Fatal("failed check cleared the last-known restart requirement")
	}
}

func TestCoordinatorSerializesGenerationStartWithPublication(t *testing.T) {
	provider := &testProvider{
		id:        Applications,
		available: true,
		check: func(context.Context) (CheckResult, error) {
			return CheckResult{}, nil
		},
	}
	c := New([]Provider{provider}, nil)
	finalPublished := make(chan struct{})
	releasePublication := make(chan struct{})
	var finalOnce sync.Once
	publish := func(snapshot Snapshot) {
		if snapshot.Generation == 1 && snapshot.Phase == PhaseReady {
			finalOnce.Do(func() {
				close(finalPublished)
				<-releasePublication
			})
		}
	}

	firstResult := make(chan Snapshot, 1)
	go func() {
		firstResult <- c.Check(context.Background(), allPreferences(), enabledConfiguration(), publish)
	}()
	<-finalPublished

	secondResult := make(chan Snapshot, 1)
	go func() {
		secondResult <- c.Check(context.Background(), allPreferences(), enabledConfiguration(), publish)
	}()

	advanced := false
	deadline := time.NewTimer(200 * time.Millisecond)
	ticker := time.NewTicker(time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
poll:
	for {
		if c.generation.Load() > 1 {
			advanced = true
			break
		}
		select {
		case <-deadline.C:
			break poll
		case <-ticker.C:
		}
	}

	close(releasePublication)
	select {
	case <-firstResult:
	case <-time.After(time.Second):
		t.Fatal("first check did not finish after publication was released")
	}
	select {
	case <-secondResult:
	case <-time.After(time.Second):
		t.Fatal("second check did not finish after publication was released")
	}
	if advanced {
		t.Fatal("new generation started while an older snapshot was publishing")
	}
}

func TestCoordinatorUpdateWaitsForBlockedCheck(t *testing.T) {
	checkStarted := make(chan struct{})
	releaseCheck := make(chan struct{})
	checkFinished := make(chan struct{})
	applyEntered := make(chan struct{})
	applyStarted := make(chan struct{})
	releaseApply := make(chan struct{})
	provider := &testProvider{
		id:        Applications,
		available: true,
		check: func(context.Context) (CheckResult, error) {
			close(checkStarted)
			<-releaseCheck
			close(checkFinished)
			return CheckResult{}, nil
		},
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			close(applyStarted)
			select {
			case <-checkFinished:
			default:
				t.Error("provider Apply entered before the blocked check finished")
			}
			close(applyEntered)
			<-releaseApply
			return ApplyResult{}, nil
		},
	}
	c := New([]Provider{provider}, nil)
	current := Snapshot{
		Phase:  PhaseReady,
		Action: ActionUpdateAll,
		Sources: []SourceState{{
			ID:         Applications,
			Configured: true,
			Available:  true,
			Enabled:    true,
			Items:      []Item{{Name: "update"}},
		}},
		TotalUpdates: 1,
	}

	checkDone := make(chan struct{})
	go func() {
		c.Check(context.Background(), allPreferences(), enabledConfiguration(), nil)
		close(checkDone)
	}()
	select {
	case <-checkStarted:
	case <-time.After(time.Second):
		t.Fatal("check did not start")
	}

	updateDone := make(chan struct{})
	go func() {
		c.UpdateAll(context.Background(), current, allPreferences(), nil)
		close(updateDone)
	}()

	select {
	case <-applyEntered:
		t.Fatal("mutation entered provider Apply while a check was blocked")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseCheck)
	select {
	case <-checkDone:
	case <-time.After(time.Second):
		t.Fatal("check did not finish")
	}

	select {
	case <-applyStarted:
	case <-time.After(time.Second):
		t.Fatal("mutation did not start after the check finished")
	}
	close(releaseApply)
	select {
	case <-updateDone:
	case <-time.After(time.Second):
		t.Fatal("mutation did not finish")
	}
}

func TestCoordinatorCheckWaitsForBlockedMutation(t *testing.T) {
	applyStarted := make(chan struct{})
	releaseApply := make(chan struct{})
	checkEntered := make(chan struct{})
	provider := &testProvider{
		id:        Applications,
		available: true,
		check: func(context.Context) (CheckResult, error) {
			close(checkEntered)
			return CheckResult{}, nil
		},
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			close(applyStarted)
			<-releaseApply
			return ApplyResult{}, nil
		},
	}
	c := New([]Provider{provider}, nil)
	current := Snapshot{
		Phase:  PhaseReady,
		Action: ActionUpdateAll,
		Sources: []SourceState{{
			ID:         Applications,
			Configured: true,
			Available:  true,
			Enabled:    true,
			Items:      []Item{{Name: "update"}},
		}},
		TotalUpdates: 1,
	}

	updateDone := make(chan struct{})
	go func() {
		c.UpdateAll(context.Background(), current, allPreferences(), nil)
		close(updateDone)
	}()
	select {
	case <-applyStarted:
	case <-time.After(time.Second):
		t.Fatal("mutation did not start")
	}

	checkDone := make(chan struct{})
	go func() {
		c.Check(context.Background(), allPreferences(), enabledConfiguration(), nil)
		close(checkDone)
	}()
	select {
	case <-checkEntered:
		t.Fatal("check entered provider Check while mutation was running")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseApply)
	select {
	case <-updateDone:
	case <-time.After(time.Second):
		t.Fatal("mutation did not finish")
	}
	select {
	case <-checkEntered:
	case <-time.After(time.Second):
		t.Fatal("check did not enter after the mutation finished")
	}
	select {
	case <-checkDone:
	case <-time.After(time.Second):
		t.Fatal("check did not finish")
	}
}

func TestCoordinatorCheckKeepsSourceRolesDistinct(t *testing.T) {
	disabled := &testProvider{id: Applications, available: true}
	unavailable := &testProvider{id: DeveloperTools, available: false}
	enabled := &testProvider{
		id:        SystemComponents,
		available: true,
		check: func(context.Context) (CheckResult, error) {
			return CheckResult{Items: []Item{{Name: "component"}}}, nil
		},
	}
	c := New(providerInterfaces([]*testProvider{disabled, unavailable, enabled}), nil)
	values := userprefs.Values{SystemComponents: true}
	got := c.Check(context.Background(), values, map[SourceID]bool{
		Applications:     false,
		DeveloperTools:   true,
		SystemComponents: true,
	}, nil)

	if len(got.Sources) != 3 {
		t.Fatalf("sources = %d, want 3", len(got.Sources))
	}
	if got.Sources[0].Configured || !got.Sources[0].Available || got.Sources[0].Enabled {
		t.Fatalf("administrator-disabled source = %#v", got.Sources[0])
	}
	if !got.Sources[1].Configured || got.Sources[1].Available || got.Sources[1].Enabled {
		t.Fatalf("runtime-unavailable source = %#v", got.Sources[1])
	}
	if !got.Sources[2].Configured || !got.Sources[2].Available || !got.Sources[2].Enabled {
		t.Fatalf("enabled source = %#v", got.Sources[2])
	}
	if got.TotalUpdates != 1 {
		t.Fatalf("total updates = %d, want 1", got.TotalUpdates)
	}
}

func TestCoordinatorAppliesInConstructorOrder(t *testing.T) {
	var mu sync.Mutex
	var order []SourceID
	providers := []*testProvider{
		{
			id:        SystemComponents,
			available: true,
			apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
				mu.Lock()
				order = append(order, SystemComponents)
				mu.Unlock()
				return ApplyResult{}, nil
			},
		},
		{
			id:        Applications,
			available: true,
			apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
				mu.Lock()
				order = append(order, Applications)
				mu.Unlock()
				return ApplyResult{}, nil
			},
		},
		{
			id:        OperatingSystem,
			available: true,
			apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
				mu.Lock()
				order = append(order, OperatingSystem)
				mu.Unlock()
				return ApplyResult{}, nil
			},
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: SystemComponents, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "system"}}},
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
		{ID: OperatingSystem, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "os"}}},
	}}

	got := New(providerInterfaces(providers), nil).UpdateAll(
		context.Background(), current, allPreferences(), nil,
	)
	if got.Phase != PhaseReady {
		t.Fatalf("phase = %v, want %v", got.Phase, PhaseReady)
	}
	mu.Lock()
	defer mu.Unlock()
	if !sameIDs(order, []SourceID{SystemComponents, Applications, OperatingSystem}) {
		t.Fatalf("apply order = %#v, want constructor order", order)
	}
}

func TestCoordinatorContinuesAfterApplyFailure(t *testing.T) {
	applyErr := errors.New("first provider failed")
	var secondCalled atomic.Bool
	first := &testProvider{
		id:        Applications,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			return ApplyResult{}, applyErr
		},
	}
	second := &testProvider{
		id:        DeveloperTools,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			secondCalled.Store(true)
			return ApplyResult{}, nil
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
		{ID: DeveloperTools, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "tool"}}},
	}}

	got := New([]Provider{first, second}, nil).UpdateAll(
		context.Background(), current, allPreferences(), nil,
	)
	if !secondCalled.Load() {
		t.Fatal("later provider was not attempted after an earlier failure")
	}
	if got.Phase != PhasePartialFailure {
		t.Fatalf("phase = %v, want %v", got.Phase, PhasePartialFailure)
	}
	if !sameIDs(got.FailedSources, []SourceID{Applications}) {
		t.Fatalf("failed sources = %#v, want applications", got.FailedSources)
	}
	if len(got.CompletedSources) != 1 || got.CompletedSources[0] != DeveloperTools {
		t.Fatalf("completed sources = %#v, want developer tools", got.CompletedSources)
	}
}

func TestCoordinatorRetryIncludesOnlyApplyFailures(t *testing.T) {
	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	first := &testProvider{
		id:        Applications,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			firstCalls.Add(1)
			if firstCalls.Load() == 1 {
				return ApplyResult{}, errors.New("retry me")
			}
			return ApplyResult{}, nil
		},
	}
	second := &testProvider{
		id:        DeveloperTools,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			secondCalls.Add(1)
			return ApplyResult{}, nil
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
		{ID: DeveloperTools, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "tool"}}},
	}}
	c := New([]Provider{first, second}, nil)

	failed := c.UpdateAll(context.Background(), current, allPreferences(), nil)
	retried := c.UpdateAll(context.Background(), failed, allPreferences(), nil)

	if firstCalls.Load() != 2 {
		t.Fatalf("failed provider calls = %d, want 2", firstCalls.Load())
	}
	if secondCalls.Load() != 1 {
		t.Fatalf("successful provider calls = %d, want 1", secondCalls.Load())
	}
	if retried.Phase != PhaseReady || retried.TotalUpdates != 0 {
		t.Fatalf("retry result = %#v, want ready with no updates", retried)
	}
	if len(retried.CompletedSources) != 2 {
		t.Fatalf("completed sources = %#v, want both sources", retried.CompletedSources)
	}
}

func TestCoordinatorMaintenanceRunsOnceAfterCompleteLiveSuccess(t *testing.T) {
	var runs atomic.Int32
	maintenance := &testMaintenance{
		run: func(context.Context, func(Progress)) error {
			runs.Add(1)
			return nil
		},
	}
	provider := &testProvider{
		id:        Applications,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			return ApplyResult{Changed: true}, nil
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
	}}

	got := New([]Provider{provider}, maintenance).UpdateAll(
		context.Background(), current,
		userprefs.Values{Applications: true, MaintenanceAfterUpdates: true},
		nil,
	)
	if runs.Load() != 1 {
		t.Fatalf("maintenance runs = %d, want 1", runs.Load())
	}
	if !got.MaintenanceRan || got.MaintenanceErr != nil {
		t.Fatalf("maintenance state = ran %v err %v, want successful run", got.MaintenanceRan, got.MaintenanceErr)
	}
}

func TestCoordinatorRetriesMaintenanceWithoutReapplyingSuccessfulSources(t *testing.T) {
	maintenanceErr := errors.New("cleanup failed")
	var applyCalls atomic.Int32
	var maintenanceCalls atomic.Int32
	maintenance := &testMaintenance{
		run: func(context.Context, func(Progress)) error {
			if maintenanceCalls.Add(1) == 1 {
				return maintenanceErr
			}
			return nil
		},
	}
	provider := &testProvider{
		id:        Applications,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			applyCalls.Add(1)
			return ApplyResult{}, nil
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
	}}
	c := New([]Provider{provider}, maintenance)
	failed := c.UpdateAll(
		context.Background(), current,
		userprefs.Values{Applications: true, MaintenanceAfterUpdates: true},
		nil,
	)
	if failed.MaintenanceErr == nil {
		t.Fatal("initial maintenance failure was not retained")
	}

	retried := c.UpdateAll(
		context.Background(), failed,
		userprefs.Values{Applications: true, MaintenanceAfterUpdates: true},
		nil,
	)
	if applyCalls.Load() != 1 {
		t.Fatalf("successful provider apply calls = %d, want 1", applyCalls.Load())
	}
	if maintenanceCalls.Load() != 2 {
		t.Fatalf("maintenance calls = %d, want 2", maintenanceCalls.Load())
	}
	if retried.MaintenanceErr != nil || !retried.MaintenanceRan {
		t.Fatalf("maintenance retry state = ran %v err %v, want successful retry",
			retried.MaintenanceRan, retried.MaintenanceErr)
	}
}

func TestCoordinatorSkipsMaintenanceAfterFailure(t *testing.T) {
	var runs atomic.Int32
	maintenance := &testMaintenance{
		run: func(context.Context, func(Progress)) error {
			runs.Add(1)
			return nil
		},
	}
	provider := &testProvider{
		id:        Applications,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			return ApplyResult{}, errors.New("not installed")
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
	}}

	got := New([]Provider{provider}, maintenance).UpdateAll(
		context.Background(), current,
		userprefs.Values{Applications: true, MaintenanceAfterUpdates: true},
		nil,
	)
	if runs.Load() != 0 {
		t.Fatal("maintenance ran after an update failure")
	}
	if got.MaintenanceRan {
		t.Fatal("maintenance marked as run after an update failure")
	}
}

func TestCoordinatorSkipsMaintenanceDuringPreview(t *testing.T) {
	var runs atomic.Int32
	maintenance := &testMaintenance{
		run: func(context.Context, func(Progress)) error {
			runs.Add(1)
			return nil
		},
	}
	provider := &testProvider{
		id:        Applications,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			return ApplyResult{Preview: true}, nil
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
	}}

	got := New([]Provider{provider}, maintenance).UpdateAll(
		context.Background(), current,
		userprefs.Values{Applications: true, MaintenanceAfterUpdates: true},
		nil,
	)
	if runs.Load() != 0 {
		t.Fatal("maintenance ran during a preview")
	}
	if !got.Preview || got.Sources[0].Completed || got.TotalUpdates != 1 {
		t.Fatalf("preview result = %#v, want unchanged actionable state", got)
	}
}

func TestCoordinatorRetryPreservesPreviewPendingBeforeMaintenance(t *testing.T) {
	maintenance := &testMaintenance{
		run: func(context.Context, func(Progress)) error {
			t.Fatal("maintenance ran while preview work remained pending")
			return nil
		},
	}
	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	first := &testProvider{
		id:        Applications,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			firstCalls.Add(1)
			return ApplyResult{Preview: true}, nil
		},
	}
	second := &testProvider{
		id:        DeveloperTools,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			if secondCalls.Add(1) == 1 {
				return ApplyResult{}, errors.New("retry me")
			}
			return ApplyResult{}, nil
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
		{ID: DeveloperTools, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "tool"}}},
	}}
	c := New([]Provider{first, second}, maintenance)
	failed := c.UpdateAll(
		context.Background(), current,
		userprefs.Values{Applications: true, DeveloperTools: true, MaintenanceAfterUpdates: true},
		nil,
	)
	retried := c.UpdateAll(
		context.Background(), failed,
		userprefs.Values{Applications: true, DeveloperTools: true, MaintenanceAfterUpdates: true},
		nil,
	)

	if firstCalls.Load() != 1 {
		t.Fatalf("preview provider apply calls = %d, want 1", firstCalls.Load())
	}
	if secondCalls.Load() != 2 {
		t.Fatalf("failed provider apply calls = %d, want 2", secondCalls.Load())
	}
	if retried.Preview != true {
		t.Fatal("retry cleared the pending preview state")
	}
	if len(retried.Sources[0].Items) != 1 || retried.Sources[0].Completed {
		t.Fatalf("preview source after retry = %#v, want pending incomplete source",
			retried.Sources[0])
	}
	if retried.TotalUpdates != 1 {
		t.Fatalf("pending updates after retry = %d, want 1", retried.TotalUpdates)
	}
}

func TestCoordinatorBusyRejectsOverlappingMutations(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	provider := &testProvider{
		id:        Applications,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			close(started)
			<-release
			return ApplyResult{}, nil
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
	}}
	c := New([]Provider{provider}, nil)

	firstResult := make(chan Snapshot, 1)
	go func() {
		firstResult <- c.UpdateAll(context.Background(), current, allPreferences(), nil)
	}()
	<-started
	if !c.Busy() {
		t.Fatal("coordinator is not busy during mutation")
	}

	second := c.UpdateAll(context.Background(), current, allPreferences(), nil)
	if second.Phase != PhaseUpdating || !c.Busy() {
		t.Fatalf("overlapping mutation result = %#v, busy = %v", second, c.Busy())
	}
	close(release)

	select {
	case <-firstResult:
	case <-time.After(time.Second):
		t.Fatal("first mutation did not finish")
	}
	if c.Busy() {
		t.Fatal("coordinator remained busy after mutation")
	}
}

func TestCoordinatorPublishesCompleteImmutableSnapshots(t *testing.T) {
	provider := &testProvider{
		id:        Applications,
		available: true,
		check: func(context.Context) (CheckResult, error) {
			return CheckResult{Items: []Item{{Name: "app"}}}, nil
		},
	}
	c := New([]Provider{provider}, nil)
	var publishes int
	got := c.Check(context.Background(), allPreferences(), enabledConfiguration(), func(snapshot Snapshot) {
		publishes++
		if len(snapshot.Sources) != 1 {
			t.Fatalf("published sources = %d, want 1", len(snapshot.Sources))
		}
		if snapshot.Sources[0].Items != nil {
			snapshot.Sources[0].Items[0].Name = "mutated"
		}
		snapshot.CompletedSources = append(snapshot.CompletedSources, OperatingSystem)
		snapshot.FailedSources = append(snapshot.FailedSources, DeveloperTools)
	})
	if publishes < 2 {
		t.Fatalf("publish count = %d, want checking and final snapshots", publishes)
	}
	if got.Sources[0].Items[0].Name != "app" {
		t.Fatalf("final item = %q, publish callback mutated coordinator state", got.Sources[0].Items[0].Name)
	}
	if len(got.CompletedSources) != 0 || len(got.FailedSources) != 0 {
		t.Fatalf("final source sets = completed %#v failed %#v, publish callback mutated coordinator state",
			got.CompletedSources, got.FailedSources)
	}
}

func TestCoordinatorMaintenanceFailureIsRetained(t *testing.T) {
	maintenanceErr := errors.New("cleanup failed")
	maintenance := &testMaintenance{
		run: func(context.Context, func(Progress)) error {
			return maintenanceErr
		},
	}
	provider := &testProvider{
		id:        Applications,
		available: true,
		apply: func(context.Context, []Item, func(Progress)) (ApplyResult, error) {
			return ApplyResult{}, nil
		},
	}
	current := Snapshot{Sources: []SourceState{
		{ID: Applications, Configured: true, Available: true, Enabled: true, Items: []Item{{Name: "app"}}},
	}}
	got := New([]Provider{provider}, maintenance).UpdateAll(
		context.Background(), current,
		userprefs.Values{Applications: true, MaintenanceAfterUpdates: true},
		nil,
	)
	if !got.MaintenanceRan || !errors.Is(got.MaintenanceErr, maintenanceErr) {
		t.Fatalf("maintenance error state = ran %v err %v", got.MaintenanceRan, got.MaintenanceErr)
	}
	if got.Phase != PhasePartialFailure || got.Action != ActionRetryFailed {
		t.Fatalf("phase/action = %v/%v, want partial failure/retry", got.Phase, got.Action)
	}
}

func allPreferences() userprefs.Values {
	return userprefs.Values{
		OperatingSystem:  true,
		Applications:     true,
		DeveloperTools:   true,
		SystemComponents: true,
	}
}

func enabledConfiguration() map[SourceID]bool {
	return map[SourceID]bool{
		OperatingSystem:  true,
		Applications:     true,
		DeveloperTools:   true,
		SystemComponents: true,
	}
}

type testProvider struct {
	id        SourceID
	available bool
	check     func(context.Context) (CheckResult, error)
	apply     func(context.Context, []Item, func(Progress)) (ApplyResult, error)
}

func (p *testProvider) ID() SourceID { return p.id }

func (p *testProvider) Available() bool { return p.available }

func (p *testProvider) Check(ctx context.Context) (CheckResult, error) {
	if p.check == nil {
		return CheckResult{}, nil
	}
	return p.check(ctx)
}

func (p *testProvider) Apply(ctx context.Context, items []Item, progress func(Progress)) (ApplyResult, error) {
	if p.apply == nil {
		return ApplyResult{}, nil
	}
	return p.apply(ctx, items, progress)
}

type testMaintenance struct {
	run func(context.Context, func(Progress)) error
}

func (m *testMaintenance) Run(ctx context.Context, progress func(Progress)) error {
	return m.run(ctx, progress)
}

func providerInterfaces(providers []*testProvider) []Provider {
	result := make([]Provider, len(providers))
	for i, provider := range providers {
		result[i] = provider
	}
	return result
}

func waitForSources(t *testing.T, started <-chan SourceID, wants ...SourceID) {
	t.Helper()
	seen := make(map[SourceID]bool, len(wants))
	for range wants {
		select {
		case got := <-started:
			seen[got] = true
		case <-time.After(time.Second):
			t.Fatalf("not all sources started concurrently: seen %#v", seen)
		}
	}
	for _, want := range wants {
		if !seen[want] {
			t.Fatalf("source %s did not start concurrently; seen %#v", want, seen)
		}
	}
}
