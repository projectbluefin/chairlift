// Package shellextensions controls the two desktop integrations through GNOME's
// user-session extension tool. It never installs extensions or starts services.
package shellextensions

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"slices"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// Extension describes a supported integration and its initial presentation.
// Existing GNOME preferences always take precedence over DefaultEnabled.
type Extension struct {
	UUID           string
	Title          string
	Description    string
	DefaultEnabled bool
}

// Catalog returns only the extension IDs shipped by Dakota and Bluefin Bling.
func Catalog() []Extension {
	return []Extension{
		{"tailscale-gnome-qs@tailscale-qs.github.io", "Tailscale Integration", "Show Tailscale in Quick Settings.", true},
		{"syncthing-toggle@projectbluefin.io", "Sync Folder Integration", "Show Sync Folder in Quick Settings. Not ready yet.", false},
	}
}

// State is observed extension availability and configured enablement.
type State struct {
	Installed bool
	Enabled   bool
}

var runCommand = func(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "gnome-extensions", args...).CombinedOutput()
	return string(out), err
}

// Load reads GNOME's installed and enabled lists without modifying preferences.
// A missing tool or unavailable GNOME session is an error, not a disabled state.
func Load(ctx context.Context) (map[string]State, error) {
	installed, err := runCommand(ctx, "list")
	if err != nil {
		return nil, fmt.Errorf("could not list GNOME extensions: %w", err)
	}
	enabled, err := runCommand(ctx, "list", "--enabled")
	if err != nil {
		return nil, fmt.Errorf("could not read enabled GNOME extensions: %w", err)
	}
	states := make(map[string]State)
	for _, extension := range Catalog() {
		states[extension.UUID] = State{
			Installed: slices.Contains(strings.Fields(installed), extension.UUID),
			Enabled:   slices.Contains(strings.Fields(enabled), extension.UUID),
		}
	}
	return states, nil
}

// SetEnabled changes only an allowlisted extension, without privilege escalation.
// The caller reloads state afterward: a successful command is not proof that
// GNOME accepted the change (for example when settings are locked).
func SetEnabled(ctx context.Context, uuid string, enabled bool) error {
	if !slices.ContainsFunc(Catalog(), func(e Extension) bool { return e.UUID == uuid }) {
		return fmt.Errorf("unsupported GNOME extension %q", uuid)
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	if dryrun.Enabled() {
		log.Printf("Would execute: gnome-extensions %s %s", action, uuid)
		return nil
	}
	out, err := runCommand(ctx, action, uuid)
	if err != nil {
		return fmt.Errorf("could not %s extension: %w: %s", action, err, strings.TrimSpace(out))
	}
	return nil
}
