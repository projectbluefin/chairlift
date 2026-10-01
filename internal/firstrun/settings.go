package firstrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/version"
)

// SchemaID is the GSettings schema identifying firstrun preferences.
const SchemaID = "io.projectbluefin.chairlift.firstrun"

const (
	// KeyCompletedVersion records the application version that finished setup.
	KeyCompletedVersion = "completed-version"

	// KeyDisposition records whether setup is not-addressed, skipped, or completed.
	KeyDisposition = "disposition"
)

// Keys is the complete key inventory for the firstrun schema.
var Keys = []string{
	KeyCompletedVersion,
	KeyDisposition,
}

// Store abstracts retrieval and persistence of setup disposition.
type Store interface {
	GetDisposition(ctx context.Context) (Disposition, error)
	SetDisposition(ctx context.Context, d Disposition) error
}

// MemoryStore provides a pure in-memory implementation for tests.
type MemoryStore struct {
	mu          sync.RWMutex
	disposition Disposition
}

// NewMemoryStore creates an in-memory Store with the given initial state.
func NewMemoryStore(initial Disposition) *MemoryStore {
	if initial == "" {
		initial = DispositionNotAddressed
	}
	return &MemoryStore{disposition: initial}
}

// GetDisposition returns the current disposition from memory.
func (m *MemoryStore) GetDisposition(ctx context.Context) (Disposition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.disposition, nil
}

// SetDisposition updates the disposition in memory.
func (m *MemoryStore) SetDisposition(ctx context.Context, d Disposition) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disposition = d
	return nil
}

// ErrSchemaMissing reports that the firstrun settings schema is not installed.
//
// A user-scoped install whose schema was never compiled into the system cache
// answers every read with "No such schema"; that is a different condition from
// a key the schema does not yet carry, and the caller must be able to tell
// them apart.
var ErrSchemaMissing = errors.New("firstrun: the firstrun settings schema is not installed")

// GSettingsStore manages persistence through GSettings.
type GSettingsStore struct{}

// NewGSettingsStore creates a Store backed by GSettings.
func NewGSettingsStore() *GSettingsStore {
	return &GSettingsStore{}
}

var runCommand = execCommand

func execCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stderr.String(), err
	}
	return stdout.String(), nil
}

// readAll returns every key in the firstrun schema from a single call.
//
// One `gsettings get` per key is one subprocess per key, and internal/livery
// documents why provider probes were deliberately moved off the GTK startup
// path for exactly that cost. `list-recursively` answers the whole schema in
// one spawn; its output is one "<schema> <key> <value>" line per key.
func readAll(ctx context.Context) (map[string]string, error) {
	out, err := runCommand(ctx, "gsettings", "list-recursively", SchemaID)
	if err != nil {
		if isMissingSchema(out) {
			return nil, ErrSchemaMissing
		}
		return nil, fmt.Errorf("firstrun: reading settings: %w: %s", err, strings.TrimSpace(out))
	}
	values := make(map[string]string, len(Keys))
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), " ", 3)
		if len(fields) < 3 || fields[0] != SchemaID {
			continue
		}
		values[fields[1]] = unquote(strings.TrimSpace(fields[2]))
	}
	if len(values) == 0 {
		return nil, ErrSchemaMissing
	}
	return values, nil
}

func isMissingSchema(out string) bool {
	return strings.Contains(out, "No such schema") || strings.Contains(out, "not installed")
}

// GetDisposition reads the current setup state from GSettings.
//
// A schema carrying a completed version but no recognized disposition is a
// setup finished by an earlier build, before the disposition key existed.
func (g *GSettingsStore) GetDisposition(ctx context.Context) (Disposition, error) {
	values, err := readAll(ctx)
	if err != nil {
		return DispositionNotAddressed, err
	}

	switch values[KeyDisposition] {
	case string(DispositionSkipped):
		return DispositionSkipped, nil
	case string(DispositionCompleted):
		return DispositionCompleted, nil
	}

	if values[KeyCompletedVersion] != "" {
		return DispositionCompleted, nil
	}
	return DispositionNotAddressed, nil
}

// SetDisposition writes the updated setup state to GSettings.
func (g *GSettingsStore) SetDisposition(ctx context.Context, d Disposition) error {
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would set %s %s=%s", SchemaID, KeyDisposition, d)
		if d == DispositionCompleted {
			log.Printf("[DRY-RUN] would set %s %s=%s", SchemaID, KeyCompletedVersion, version.Version)
		}
		return nil
	}

	if out, err := runCommand(ctx, "gsettings", "set", SchemaID, KeyDisposition, strconv.Quote(string(d))); err != nil {
		if isMissingSchema(out) {
			return ErrSchemaMissing
		}
		return fmt.Errorf("firstrun: writing disposition: %w", err)
	}

	if d == DispositionCompleted {
		if out, err := runCommand(ctx, "gsettings", "set", SchemaID, KeyCompletedVersion, strconv.Quote(version.Version)); err != nil {
			if isMissingSchema(out) {
				return ErrSchemaMissing
			}
			return fmt.Errorf("firstrun: writing completed version: %w", err)
		}
	}
	return nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')) {
		return s[1 : len(s)-1]
	}
	return s
}

// WriteTimeout bounds one disposition read or write. Each is a `gsettings`
// spawn; a wedged dconf must not pin a goroutine for the life of the process.
const WriteTimeout = 10 * time.Second

// RecordSkip persists a skip — Get Moving, or the dialog dismissed without a
// decision — without demoting a recorded completion: the assistant stays
// reachable after setup finished, and a skip there must not overwrite the
// stronger state. It reports the disposition now recorded and whether a
// write happened. A read that fails refuses the write: with the current
// state unknown, writing "skipped" could overwrite a completion, and the
// worst a refusal costs is the assistant returning until the read works.
func RecordSkip(ctx context.Context, store Store) (recorded Disposition, wrote bool, err error) {
	current, err := store.GetDisposition(ctx)
	if err != nil {
		return DispositionNotAddressed, false, fmt.Errorf("firstrun: reading the disposition before recording a skip: %w", err)
	}
	next := SkipPreserving(current)
	if next == current {
		return current, false, nil
	}
	if err := store.SetDisposition(ctx, next); err != nil {
		return current, false, err
	}
	return next, true, nil
}

// ShouldPresent permits setup only for an explicit launch request or menu action.
// It deliberately performs no disposition read on ordinary startup.
func ShouldPresent(explicitSetup bool) bool {
	return explicitSetup
}
