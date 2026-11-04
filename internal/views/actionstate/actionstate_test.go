package actionstate

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestPackageMutationsEnumerateEveryInstalledOutcome(t *testing.T) {
	operations := map[string]func(bool, bool) Decision{
		"uninstall": PackageUninstall,
		"pin":       PackagePin,
	}
	tests := []struct {
		name      string
		succeeded bool
		dryRun    bool
		want      Decision
	}{
		{
			name: "failure restores controls",
			want: Decision{RestoreControl: true},
		},
		{
			name:      "dry-run success restores controls",
			succeeded: true,
			dryRun:    true,
			want:      Decision{RestoreControl: true},
		},
		{
			name:      "live success completes controls and refreshes inventory",
			succeeded: true,
			want:      Decision{Refresh: true, CompleteControl: true},
		},
	}

	for operation, decide := range operations {
		t.Run(operation, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					got := decide(tt.succeeded, tt.dryRun)
					if !reflect.DeepEqual(got, tt.want) {
						t.Fatalf("%s decision(%v, %v) = %#v, want %#v", operation, tt.succeeded, tt.dryRun, got, tt.want)
					}
				})
			}
		})
	}
}

func TestGateRejectsRepeatedConcurrentStarts(t *testing.T) {
	var gate Gate
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
	var gate Gate
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

func TestGateRunningOnlyWhileHeld(t *testing.T) {
	var gate Gate
	if gate.Running() {
		t.Fatal("zero-value gate reports running")
	}
	gate.TryStart()
	if !gate.Running() {
		t.Fatal("started gate does not report running")
	}
	gate.Reset()
	if gate.Running() {
		t.Fatal("reset gate still reports running")
	}
	gate.TryStart()
	gate.Complete()
	if gate.Running() {
		t.Fatal("completed gate still reports running")
	}
}

func TestGateIdleOnlyWhenItCanStart(t *testing.T) {
	var gate Gate
	if !gate.Idle() {
		t.Fatal("zero-value gate is not idle")
	}
	gate.TryStart()
	if gate.Idle() {
		t.Fatal("started gate reports idle")
	}
	gate.Reset()
	if !gate.Idle() {
		t.Fatal("reset gate is not idle")
	}
	gate.TryStart()
	gate.Complete()
	if gate.Idle() {
		t.Fatal("completed gate reports idle, but it can never start again")
	}
}

func TestRefreshGateAllowsOnlyTheNewestGeneration(t *testing.T) {
	var gate RefreshGate
	if gate.IsCurrent(0) {
		t.Fatal("zero generation is current before any refresh")
	}

	first := gate.Begin()
	if !gate.IsCurrent(first) {
		t.Fatalf("first generation %d is not current", first)
	}

	second := gate.Begin()
	if second <= first {
		t.Fatalf("second generation %d is not newer than first %d", second, first)
	}
	if gate.IsCurrent(first) {
		t.Fatalf("superseded generation %d remains current", first)
	}
	if !gate.IsCurrent(second) {
		t.Fatalf("newest generation %d is not current", second)
	}
}

func TestRefreshGateConcurrentRequestsHaveOneCurrentGeneration(t *testing.T) {
	var gate RefreshGate
	const callers = 64

	generations := make(chan uint64, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			generations <- gate.Begin()
		}()
	}
	wg.Wait()
	close(generations)

	seen := make(map[uint64]bool, callers)
	current := 0
	for generation := range generations {
		if seen[generation] {
			t.Fatalf("generation %d was assigned more than once", generation)
		}
		seen[generation] = true
		if gate.IsCurrent(generation) {
			current++
		}
	}
	if len(seen) != callers {
		t.Fatalf("unique generation count = %d, want %d", len(seen), callers)
	}
	if current != 1 {
		t.Fatalf("current generation count = %d, want exactly 1", current)
	}
}

// TestSerializerRunsOneAttemptAtATime pins the property the Livery selection
// handlers need: two picks cannot have their persist-and-apply interleave.
func TestSerializerRunsOneAttemptAtATime(t *testing.T) {
	var s Serializer
	var mu sync.Mutex
	var inFlight, overlaps int

	var wg sync.WaitGroup
	for range 8 {
		ticket := s.Claim()
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Run(ticket, func() {
				mu.Lock()
				inFlight++
				if inFlight > 1 {
					overlaps++
				}
				mu.Unlock()
				time.Sleep(time.Millisecond)
				mu.Lock()
				inFlight--
				mu.Unlock()
			})
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if overlaps != 0 {
		t.Errorf("serialized work overlapped %d times", overlaps)
	}
}

// TestSerializerSkipsASupersededAttempt is what keeps the persisted selection
// and the installed mark naming the same thing: an overtaken pick does no
// work, because the newer pick writes both halves itself.
func TestSerializerSkipsASupersededAttempt(t *testing.T) {
	var s Serializer

	stale := s.Claim()
	newest := s.Claim()

	if s.Run(stale, func() { t.Error("a superseded attempt ran") }) {
		t.Error("Run reported that a superseded attempt ran")
	}
	var latestRan bool
	if !s.Run(newest, func() { latestRan = true }) {
		t.Error("Run refused the newest attempt")
	}
	if !latestRan {
		t.Error("the newest attempt did not run")
	}
}

// TestSerializerRefusesAnUnclaimedTicket keeps a zero value — the shape a
// caller that forgot to Claim would pass — from running work out of order.
func TestSerializerRefusesAnUnclaimedTicket(t *testing.T) {
	var s Serializer
	if s.Run(Ticket{}, func() { t.Error("unclaimed work ran") }) {
		t.Error("Run accepted a zero ticket")
	}
}

func TestSerializerIsCurrentFollowsLaterClaims(t *testing.T) {
	var s Serializer
	if s.IsCurrent(0) {
		t.Fatal("unclaimed completion is current")
	}
	first := s.Claim()
	if !s.IsCurrent(first.Generation()) {
		t.Fatal("new completion is stale")
	}
	second := s.Claim()
	if s.IsCurrent(first.Generation()) || !s.IsCurrent(second.Generation()) {
		t.Fatal("new request did not supersede first completion")
	}
	if s.Reserve(); !s.IsCurrent(second.Generation()) {
		t.Fatal("a reservation superseded the newest claim")
	}
}

// TestSerializerRunsInQueueOrder is what lets a Livery section switch be
// ordered with the picks made before it. A pick in flight, a second pick, and
// a toggle all waited on one mutex, and whichever woke first ran: a toggle
// that turned the section off could run before the second pick, whose Apply
// then installed a mark under a switch reading off.
func TestSerializerRunsInQueueOrder(t *testing.T) {
	for range 50 {
		var s Serializer
		release := make(chan struct{})
		var mu sync.Mutex
		var order []string
		record := func(name string) func() {
			return func() {
				mu.Lock()
				order = append(order, name)
				mu.Unlock()
			}
		}

		var wg sync.WaitGroup
		start := func(ticket Ticket, fn func()) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.Run(ticket, fn)
			}()
		}
		first := s.Claim()
		start(first, func() { <-release; record("first")() })
		second := s.Reserve()
		third := s.Reserve()
		// Start the later tickets in reverse so the scheduler, not the
		// queue, would decide the order if the queue did not.
		start(third, record("third"))
		start(second, record("second"))
		close(release)
		wg.Wait()

		if want := []string{"first", "second", "third"}; !reflect.DeepEqual(order, want) {
			t.Fatalf("ran %v, want %v", order, want)
		}
	}
}

// TestSerializerReservationOutlivesLaterClaims pins that a reservation is
// never superseded: a section switch queued behind a pick must still run when
// the user picks again before it reaches the front, and the newer pick waits
// for it.
func TestSerializerReservationOutlivesLaterClaims(t *testing.T) {
	var s Serializer
	stale := s.Claim()
	reserved := s.Reserve()
	newest := s.Claim()

	var order []string
	if s.Run(stale, func() { order = append(order, "stale") }) {
		t.Error("a superseded claim ran")
	}
	if !s.Run(reserved, func() { order = append(order, "reserved") }) {
		t.Error("a later claim superseded the reservation")
	}
	if !s.Run(newest, func() { order = append(order, "newest") }) {
		t.Error("the newest claim did not run")
	}
	if want := []string{"reserved", "newest"}; !reflect.DeepEqual(order, want) {
		t.Errorf("ran %v, want %v", order, want)
	}
}

// A list rebuild while a row's uninstall or pin is running discarded the
// only controls showing it and gave the replacement row an idle gate, so a
// second click started an overlapping brew command (W2-APPS-1).
func TestRowGatesDeferRebuildWhileARowActionRuns(t *testing.T) {
	var rows RowGates
	if rows.Defer() {
		t.Fatal("an empty list deferred its first rebuild")
	}
	rows.Rebuild()
	jq := rows.New()
	ripgrep := rows.New()
	if rows.Defer() {
		t.Fatal("idle rows deferred a rebuild")
	}

	if !ripgrep.TryStart() {
		t.Fatal("idle row gate refused a start")
	}
	if !rows.Defer() {
		t.Fatal("a rebuild would replace a row whose action is running")
	}
	if rows.Settled() {
		t.Fatal("deferred rebuild ran while the row action is still running")
	}
	if !jq.TryStart() || !rows.Defer() {
		t.Fatal("a second running row must keep the rebuild deferred")
	}
	jq.Complete()
	if rows.Settled() {
		t.Fatal("deferred rebuild ran while another row action is still running")
	}
	ripgrep.Reset()
	if !rows.Settled() {
		t.Fatal("the owed rebuild did not run once every row action settled")
	}
	if rows.Settled() {
		t.Fatal("one deferred rebuild settled twice")
	}
}

func TestRowGatesRebuildForgetsReplacedRowsAndDebt(t *testing.T) {
	var rows RowGates
	old := rows.New()
	old.TryStart()
	if !rows.Defer() {
		t.Fatal("running row did not defer the rebuild")
	}
	old.Reset()
	rows.Rebuild()
	if rows.Settled() {
		t.Fatal("a rebuild that ran still reports a deferred one owed")
	}
	replacement := rows.New()
	if replacement == old {
		t.Fatal("rebuild reused a replaced row's gate")
	}
	old.TryStart()
	if rows.Defer() {
		t.Fatal("a replaced row's gate still blocks rebuilds")
	}
}
