package firstrun

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"

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

// GetDisposition reads the current setup state from GSettings.
func (g *GSettingsStore) GetDisposition(ctx context.Context) (Disposition, error) {
	out, err := runCommand(ctx, "gsettings", "get", SchemaID, KeyDisposition)
	if err != nil {
		verOut, verErr := runCommand(ctx, "gsettings", "get", SchemaID, KeyCompletedVersion)
		if verErr == nil {
			val := unquote(strings.TrimSpace(verOut))
			if val != "" {
				return DispositionCompleted, nil
			}
		}
		return DispositionNotAddressed, err
	}

	val := unquote(strings.TrimSpace(out))
	switch val {
	case string(DispositionSkipped):
		return DispositionSkipped, nil
	case string(DispositionCompleted):
		return DispositionCompleted, nil
	default:
		return DispositionNotAddressed, nil
	}
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

	if _, err := runCommand(ctx, "gsettings", "set", SchemaID, KeyDisposition, strconv.Quote(string(d))); err != nil {
		return fmt.Errorf("firstrun: writing disposition: %w", err)
	}

	if d == DispositionCompleted {
		if _, err := runCommand(ctx, "gsettings", "set", SchemaID, KeyCompletedVersion, strconv.Quote(version.Version)); err != nil {
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

// ShouldPresent decides whether to present the first-run assistant dialog.
//
// 1. Explicit setup (--setup flag or menu action) always presents, even under --dry-run.
// 2. Automated presentation is suppressed under --dry-run (e.g. during headless screenshots).
// 3. Automated presentation is suppressed if setup was already completed or skipped.
func ShouldPresent(ctx context.Context, dryRun, explicitSetup bool, store Store) bool {
	if explicitSetup {
		return true
	}
	if dryRun {
		return false
	}
	if store == nil {
		return true
	}

	disp, err := store.GetDisposition(ctx)
	if err != nil {
		return true
	}
	return !disp.IsSettled()
}
