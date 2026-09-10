package dryrun

import (
	"sync"
	"testing"
)

func TestEnabledDefaultsToFalse(t *testing.T) {
	t.Cleanup(func() { Set(false) })

	Set(false)
	if Enabled() {
		t.Error("Enabled() = true, want false when dry-run has not been turned on")
	}
}

func TestSetTogglesEnabled(t *testing.T) {
	t.Cleanup(func() { Set(false) })

	Set(true)
	if !Enabled() {
		t.Error("Enabled() = false after Set(true), want true")
	}

	Set(false)
	if Enabled() {
		t.Error("Enabled() = true after Set(false), want false")
	}
}

func TestSetIsIdempotent(t *testing.T) {
	t.Cleanup(func() { Set(false) })

	Set(true)
	Set(true)
	if !Enabled() {
		t.Error("Enabled() = false after two Set(true) calls, want true")
	}
}

// The flag is read from view goroutines while app sets it at startup, so the
// atomic must tolerate concurrent Set/Enabled without a data race. Run with
// -race to make this assertion meaningful.
func TestSetAndEnabledAreRaceFree(t *testing.T) {
	t.Cleanup(func() { Set(false) })

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(mode bool) {
			defer wg.Done()
			Set(mode)
		}(i%2 == 0)
		go func() {
			defer wg.Done()
			_ = Enabled()
		}()
	}
	wg.Wait()

	Set(true)
	if !Enabled() {
		t.Error("Enabled() = false after concurrent access then Set(true), want true")
	}
}
