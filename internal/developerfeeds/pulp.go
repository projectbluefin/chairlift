package developerfeeds

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
)

const (
	// PulpID is the Flathub application ID for the Pulp RSS/Atom reader.
	PulpID = "org.gnome.gitlab.cheywood.Pulp"
	// feedRemote is the origin Pulp is installed from.
	feedRemote = "flathub"
	// OPMLFileName is the staged catalog filename under the chairlift data dir.
	OPMLFileName = "developer-feeds.opml"
)

// opmlContent is the catalog staged by StageOPML. It defaults to the embedded
// catalog asset, but is a variable so tests can swap in a fixture and assert
// the staged bytes without re-embedding.
var opmlContent = string(Asset())

// IsInstalled reports whether Pulp is present in the user scope. It queries
// flatpak, never Pulp's sandboxed store. A flatpak query error is returned so
// the caller can decide whether to proceed or fail closed.
func IsInstalled() (bool, error) {
	apps, err := flatpak.ListUserApplications()
	if err != nil {
		return false, fmt.Errorf("listing user flatpaks: %w", err)
	}
	for _, app := range apps {
		if app.ApplicationID == PulpID {
			return true, nil
		}
	}
	return false, nil
}

// Provision installs Pulp into the user scope from flathub if it is not
// already present. Detection runs first so an already-installed Pulp never
// triggers a redundant install. The install itself enforces dryrun through
// the flatpak runner (install is a state-changing command); here we short
// circuit and log so dry-run does not even issue the query-driven install.
func Provision() error {
	installed, err := IsInstalled()
	if err != nil {
		return err
	}
	if installed {
		log.Printf("[developerfeeds] %s already installed in user scope", PulpID)
		return nil
	}

	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would install %s from %s (user scope)", PulpID, feedRemote)
		return nil
	}

	return flatpak.InstallFromRemote(PulpID, feedRemote, true)
}

// dataDir returns ~/.local/share/chairlift.
func dataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "chairlift"), nil
}

// opmlPath returns the absolute path of the staged catalog.
func opmlPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, OPMLFileName), nil
}

// StageOPML writes the curated developer feeds OPML catalog into the chairlift
// data directory with 0644 permissions, creating the directory if needed. It
// is a user-scope write — no pkexec, no root. In dry-run it logs the target
// path and writes nothing, so screenshot generation never mutates user state.
func StageOPML() error {
	path, err := opmlPath()
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would stage developer feeds OPML to %s", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return &PulpError{Message: fmt.Sprintf("create %s data dir: %v", branding.AppName, err), Err: err}
	}
	if err := os.WriteFile(path, append([]byte(strings.TrimSpace(opmlContent)), '\n'), 0o644); err != nil {
		return &PulpError{Message: fmt.Sprintf("stage developer feeds OPML: %v", err), Err: err}
	}
	return nil
}

// PulpError wraps a failed developerfeeds operation, carrying the underlying
// cause for errors.Is/errors.As classification.
type PulpError struct {
	Message string
	Err     error
}

func (e *PulpError) Error() string {
	return e.Message
}

// Unwrap exposes the underlying cause to errors.Is/errors.As.
func (e *PulpError) Unwrap() error {
	return e.Err
}
