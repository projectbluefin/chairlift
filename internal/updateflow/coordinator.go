package updateflow

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/projectbluefin/chairlift/internal/userprefs"
)

// Coordinator owns update-source state, aggregate decisions, and mutation
// ordering without depending on GTK.
type Coordinator struct {
	providers   []Provider
	maintenance Maintenance
	generation  atomic.Uint64
	running     atomic.Bool
	// Serialize operation admission so a mutation cannot race a new check
	// between setting Busy and acquiring the exclusive operation lock.
	operationStartMu sync.Mutex
	// Checks share this lock; UpdateAll holds it for its complete operation.
	operationMu sync.RWMutex
	mu          sync.Mutex
	last        Snapshot
	publishMu   sync.Mutex
}

// New creates a coordinator with a stable provider order.
func New(providers []Provider, maintenance Maintenance) *Coordinator {
	ordered := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if provider != nil {
			ordered = append(ordered, provider)
		}
	}
	return &Coordinator{
		providers:   ordered,
		maintenance: maintenance,
		last: Snapshot{
			Phase:  PhaseIdle,
			Action: ActionNone,
		},
	}
}

// Check concurrently checks every configured, available, user-enabled source.
func (c *Coordinator) Check(
	ctx context.Context,
	preferences userprefs.Values,
	configured map[SourceID]bool,
	publish func(Snapshot),
) Snapshot {
	c.operationStartMu.Lock()
	c.operationMu.RLock()
	c.operationStartMu.Unlock()
	defer c.operationMu.RUnlock()

	generation := c.beginGeneration()
	previous := c.snapshot()
	sources := c.checkSources(previous, preferences, configured)
	state := Snapshot{
		Generation:     generation,
		Sources:        sources,
		LastChecked:    previous.LastChecked,
		Preview:        false,
		MaintenanceRan: false,
	}
	state = derive(state)
	c.commit(generation, state, publish)

	var stateMu sync.Mutex
	var checks sync.WaitGroup
	for index, provider := range c.providers {
		if !sources[index].Enabled {
			continue
		}
		checks.Add(1)
		go func(index int, provider Provider) {
			defer checks.Done()
			result, err := provider.Check(ctx)

			stateMu.Lock()
			source := &sources[index]
			source.Checking = false
			if err != nil {
				source.CheckErr = err
			} else {
				source.Items = append([]Item(nil), result.Items...)
				source.CheckErr = nil
				source.ApplyErr = nil
				source.RestartRequired = result.RestartRequired
			}
			state.Sources = cloneSources(sources)
			state.Generation = generation
			publishedState := state.clone()
			stateMu.Unlock()

			c.commit(generation, derive(publishedState), publish)
		}(index, provider)
	}
	checks.Wait()

	stateMu.Lock()
	state.Sources = cloneSources(sources)
	state.LastChecked = time.Now()
	state.Generation = generation
	state.Current = ""
	state.Progress = ""
	state = derive(state)
	stateMu.Unlock()

	if !c.commit(generation, state, publish) {
		return c.snapshot()
	}
	return state.clone()
}

// UpdateAll serially applies pending updates in the coordinator's provider
// order. A retry only includes sources that still carry ApplyErr.
func (c *Coordinator) UpdateAll(
	ctx context.Context,
	current Snapshot,
	preferences userprefs.Values,
	publish func(Snapshot),
) Snapshot {
	c.operationStartMu.Lock()
	if !c.running.CompareAndSwap(false, true) {
		c.operationStartMu.Unlock()
		return c.snapshot()
	}
	c.operationMu.Lock()
	c.operationStartMu.Unlock()
	defer c.running.Store(false)
	defer c.operationMu.Unlock()

	generation := c.beginGeneration()
	state := current.clone()
	state.Generation = generation
	state = c.orderSources(state)
	retry := current.Action == ActionRetryFailed || current.Phase == PhasePartialFailure
	retryMaintenance := retry && current.MaintenanceErr != nil
	state.Preview = current.Preview && retry
	state.MaintenanceRan = false
	if !retryMaintenance {
		state.MaintenanceErr = nil
	}
	state.Current = ""
	state.Progress = ""

	if !retry {
		state.CompletedSources = make([]SourceID, 0)
	}
	targets := make(map[SourceID]bool)
	targetCount := 0
	for i := range state.Sources {
		source := &state.Sources[i]
		source.Checking = false
		source.Updating = false
		if retry {
			targets[source.ID] = source.ApplyErr != nil
		} else {
			targets[source.ID] = source.Enabled && len(source.Items) > 0
		}
		if targets[source.ID] {
			targetCount++
			source.Completed = false
		}
	}

	state.Action = ActionUpdateAll
	if retry {
		state.Action = ActionRetryFailed
	}
	for _, provider := range c.providers {
		if targets[provider.ID()] {
			state.Current = provider.ID()
			index := sourceIndex(state.Sources, provider.ID())
			if index >= 0 {
				state.Sources[index].Updating = true
			}
			break
		}
	}
	state = derive(state)
	c.commit(generation, state, publish)

	var stateMu sync.Mutex
	hadFailure := false
	hadPreview := state.Preview
	for _, provider := range c.providers {
		id := provider.ID()
		if !targets[id] {
			continue
		}

		stateMu.Lock()
		index := sourceIndex(state.Sources, id)
		if index < 0 {
			stateMu.Unlock()
			continue
		}
		items := append([]Item(nil), state.Sources[index].Items...)
		state.Current = id
		state.Progress = ""
		state.Sources[index].Updating = true
		state = derive(state)
		stateMu.Unlock()
		c.commit(generation, state, publish)

		progress := func(update Progress) {
			stateMu.Lock()
			state.Current = update.Source
			if state.Current == "" {
				state.Current = id
			}
			state.Progress = update.Message
			state.Generation = generation
			progressState := derive(state)
			stateMu.Unlock()
			c.commit(generation, progressState, publish)
		}

		result, err := provider.Apply(ctx, items, progress)

		stateMu.Lock()
		sourceIndexValue := sourceIndex(state.Sources, id)
		if sourceIndexValue >= 0 {
			source := &state.Sources[sourceIndexValue]
			source.Updating = false
			if err != nil {
				source.ApplyErr = err
				source.Completed = false
				removeSourceID(&state.CompletedSources, id)
				hadFailure = true
			} else {
				source.ApplyErr = nil
				if result.Preview {
					source.Completed = false
					hadPreview = true
				} else {
					source.RestartRequired = source.RestartRequired || result.RestartRequired
					source.Completed = true
					source.Items = nil
					appendSourceID(&state.CompletedSources, id)
				}
			}
		}
		state.Preview = state.Preview || result.Preview
		state.Current = id
		state.Progress = ""
		if nextID := nextTargetID(c.providers, targets, id); nextID != "" {
			state.Current = nextID
			if nextIndex := sourceIndex(state.Sources, nextID); nextIndex >= 0 {
				state.Sources[nextIndex].Updating = true
			}
		}
		state.Generation = generation
		completedState := derive(state)
		stateMu.Unlock()
		c.commit(generation, completedState, publish)
	}

	stateMu.Lock()
	state.Current = ""
	state.Progress = ""
	state.Generation = generation
	state = derive(state)
	stateMu.Unlock()

	if (targetCount > 0 || retryMaintenance) && !hadFailure && !hadPreview &&
		len(retrySources(state)) == 0 && hasCompletedSource(state) &&
		!hasPendingWork(state) && preferences.MaintenanceAfterUpdates && c.maintenance != nil {
		stateMu.Lock()
		state.MaintenanceErr = nil
		state.MaintenanceRan = true
		state = derive(state)
		stateMu.Unlock()
		c.commit(generation, state, publish)

		maintenanceProgress := func(progress Progress) {
			stateMu.Lock()
			state.Current = progress.Source
			state.Progress = progress.Message
			state.Generation = generation
			progressState := derive(state)
			stateMu.Unlock()
			c.commit(generation, progressState, publish)
		}
		err := c.maintenance.Run(ctx, maintenanceProgress)
		stateMu.Lock()
		state.MaintenanceErr = err
		state.Current = ""
		state.Progress = ""
		state.Generation = generation
		state = derive(state)
		stateMu.Unlock()
		c.commit(generation, state, publish)
	}

	stateMu.Lock()
	state.Current = ""
	state.Progress = ""
	state.Generation = generation
	state = derive(state)
	stateMu.Unlock()
	if !c.commit(generation, state, publish) {
		return c.snapshot()
	}
	return state.clone()
}

// Busy reports whether a mutation is currently running.
func (c *Coordinator) Busy() bool {
	return c.running.Load()
}

func (c *Coordinator) checkSources(
	previous Snapshot,
	preferences userprefs.Values,
	configured map[SourceID]bool,
) []SourceState {
	previousByID := make(map[SourceID]SourceState, len(previous.Sources))
	for _, source := range previous.Sources {
		previousByID[source.ID] = source
	}

	sources := make([]SourceState, len(c.providers))
	for index, provider := range c.providers {
		id := provider.ID()
		previousSource := previousByID[id]
		available := provider.Available()
		sources[index] = SourceState{
			ID:              id,
			Configured:      configured[id],
			Available:       available,
			Enabled:         configured[id] && available && preferenceEnabled(preferences, id),
			Items:           append([]Item(nil), previousSource.Items...),
			RestartRequired: previousSource.RestartRequired,
			Checking:        configured[id] && available && preferenceEnabled(preferences, id),
		}
	}
	return sources
}

func (c *Coordinator) orderSources(state Snapshot) Snapshot {
	byID := make(map[SourceID]SourceState, len(state.Sources))
	for _, source := range state.Sources {
		byID[source.ID] = source
	}
	ordered := make([]SourceState, 0, len(state.Sources))
	seen := make(map[SourceID]bool, len(state.Sources))
	for _, provider := range c.providers {
		if source, ok := byID[provider.ID()]; ok {
			ordered = append(ordered, source)
			seen[provider.ID()] = true
		}
	}
	for _, source := range state.Sources {
		if !seen[source.ID] {
			ordered = append(ordered, source)
			seen[source.ID] = true
		}
	}
	state.Sources = ordered
	return state
}

func (c *Coordinator) snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last.clone()
}

func (c *Coordinator) beginGeneration() uint64 {
	c.publishMu.Lock()
	generation := c.generation.Add(1)
	c.publishMu.Unlock()
	return generation
}

func (c *Coordinator) commit(generation uint64, state Snapshot, publish func(Snapshot)) bool {
	state.Generation = generation
	state = state.clone()

	c.publishMu.Lock()
	defer c.publishMu.Unlock()

	c.mu.Lock()
	if c.generation.Load() != generation {
		c.mu.Unlock()
		return false
	}
	c.last = state.clone()
	c.mu.Unlock()

	if publish != nil {
		publish(state.clone())
	}
	return true
}

func sourceIndex(sources []SourceState, id SourceID) int {
	for index, source := range sources {
		if source.ID == id {
			return index
		}
	}
	return -1
}

func nextTargetID(providers []Provider, targets map[SourceID]bool, current SourceID) SourceID {
	foundCurrent := false
	for _, provider := range providers {
		if provider.ID() == current {
			foundCurrent = true
			continue
		}
		if foundCurrent && targets[provider.ID()] {
			return provider.ID()
		}
	}
	return ""
}

func appendSourceID(ids *[]SourceID, id SourceID) {
	if containsSourceID(*ids, id) {
		return
	}
	*ids = append(*ids, id)
}

func removeSourceID(ids *[]SourceID, id SourceID) {
	for index, candidate := range *ids {
		if candidate == id {
			*ids = append((*ids)[:index], (*ids)[index+1:]...)
			return
		}
	}
}

func hasCompletedSource(state Snapshot) bool {
	return len(state.CompletedSources) > 0
}

func hasPendingWork(state Snapshot) bool {
	for _, source := range state.Sources {
		if source.Enabled && source.Configured && source.Available && len(source.Items) > 0 {
			return true
		}
	}
	return false
}
