package devtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// Bluefin DX's 10-vscode.sh and /etc/skel/.config/Code/User/settings.json:
// https://github.com/ublue-os/bluefin/tree/466f6f3584a55a83f63821cc44551a3808d952b0/system_files/dx
var vsCodeExtensions = [...]string{
	"ms-vscode-remote.remote-containers",
	"ms-vscode-remote.remote-ssh",
	"ms-azuretools.vscode-containers",
}

const vsCodeDefaultSettings = `{
    "window.titleBarStyle": "custom",
    "editor.fontFamily": "'Cascadia Code', 'Droid Sans Mono', 'monospace', monospace, 'Symbols Nerd Font Mono'",
    "update.mode": "none"
}
`

// The seam covers both PATH and the Homebrew fallback used by executable.
var vsCodeExecutable = func() string { return executable("code") }

// ErrVSCodeNotInstalled means native Microsoft VS Code is absent, so editor
// setup does not apply to this computer; it is not a setup failure.
var ErrVSCodeNotInstalled = errors.New("native Microsoft VS Code (code) is not installed")

// SetupVSCode seeds absent native Microsoft VS Code settings and installs the
// Bluefin DX extensions once per user at setup version 1. Existing settings,
// including JSONC and symlinks, are never rewritten. Errors are optional-setup
// failures for the caller to report, not grounds to reverse Developer access.
func SetupVSCode(ctx context.Context, progress func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	code := vsCodeExecutable()
	if code == "" {
		return ErrVSCodeNotInstalled
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// libsetup.sh uses this override and .version.user.vscode == "1":
	// https://github.com/ublue-os/packages/blob/b63e18a459e607841335447b3caddb120c5c20d3/packages/ublue-setup-services/src/lib/libsetup.sh
	// It records the version BEFORE running the hook. Verify extensions before
	// honoring that marker, because it alone is not evidence of successful setup.
	marker := os.Getenv("SETUP_CHECKER_FILE")
	if marker == "" {
		marker = filepath.Join(home, ".local", "share", "ublue", "setup_versioning.json")
	}
	state := make(map[string]any)
	data, err := os.ReadFile(marker)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read VS Code setup version: %w", err)
	}
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&state); err != nil {
			return fmt.Errorf("read VS Code setup version: %w", err)
		}
		if len(bytes.TrimSpace(data[decoder.InputOffset():])) != 0 {
			return errors.New("invalid trailing VS Code setup version JSON")
		}
		if state == nil {
			return errors.New("VS Code setup version must be a JSON object")
		}
	}
	versions, err := vsCodeVersionObject(state, "version")
	if err != nil {
		return err
	}
	user, err := vsCodeVersionObject(versions, "user")
	if err != nil {
		return err
	}
	marked := user["vscode"] == "1"
	settings := filepath.Join(home, ".config", "Code", "User", "settings.json")
	if err := seedVSCodeSettings(settings); err != nil {
		return fmt.Errorf("seed VS Code settings: %w", err)
	}
	var installed []string
	if marked && !dryrun.Enabled() {
		output, err := command(ctx, false, code, "--list-extensions")
		if err != nil {
			return err
		}
		installed = strings.Fields(output)
	}
	for _, extension := range vsCodeExtensions {
		if slices.Contains(installed, extension) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if progress != nil {
			progress("Installing VS Code extension " + extension + "…")
		}
		if _, err := command(ctx, true, code, "--install-extension", extension); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would record VS Code setup version 1 in %s", marker)
		return nil
	}
	if marked {
		return nil
	}
	user["vscode"] = "1"
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(marker), ".vscode-setup-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), marker)
}

func vsCodeVersionObject(parent map[string]any, key string) (map[string]any, error) {
	if value, exists := parent[key]; exists {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("VS Code setup version %q must be a JSON object", key)
		}
		return object, nil
	}
	object := make(map[string]any)
	parent[key] = object
	return object, nil
}

func seedVSCodeSettings(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would seed native VS Code settings at %s", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return nil // Another process created settings; it owns their contents.
	}
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(vsCodeDefaultSettings)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}
