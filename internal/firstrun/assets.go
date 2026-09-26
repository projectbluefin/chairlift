package firstrun

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// assets holds the embedded Project Bluefin brand and mascot illustrations.
//
// Embedding rather than referencing filesystem paths ensures the assistant
// is fully self-contained in standalone binaries and cannot suffer missing-asset
// failures in minimal environments.
//
//go:embed assets/*.svg
var assets embed.FS

const (
	// AssetWordmarkDark is the dark-theme variant with light lettering.
	AssetWordmarkDark = "assets/bluefin-wordmark-dark.svg"

	// AssetWordmarkLight is the light-theme variant with dark lettering.
	AssetWordmarkLight = "assets/bluefin-wordmark-light.svg"
)

var (
	cacheMu  sync.Mutex
	cacheDir string
)

// Asset returns the raw SVG byte contents for the specified embedded asset path.
func Asset(name string) ([]byte, error) {
	data, err := assets.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("firstrun: reading asset %s: %w", name, err)
	}
	return data, nil
}

// Wordmark returns the SVG bytes for the wordmark tailored to light or dark themes.
func Wordmark(dark bool) ([]byte, error) {
	if dark {
		return Asset(AssetWordmarkDark)
	}
	return Asset(AssetWordmarkLight)
}

// AssetPath returns a filesystem path for the requested embedded asset.
//
// The embedded bytes are written to a process-scoped temporary directory so
// GTK and librsvg can load the vector graphic via file path. The embedded
// copy is the only source: an earlier version preferred
// internal/firstrun/<name> relative to the working directory as a
// development convenience, which meant a binary launched from an untrusted
// directory rendered an attacker-planted SVG through librsvg.
func AssetPath(name string) (string, error) {
	data, err := Asset(name)
	if err != nil {
		return "", err
	}

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if cacheDir == "" {
		dir, err := os.MkdirTemp("", "bluefin-firstrun-assets-*")
		if err != nil {
			return "", fmt.Errorf("firstrun: creating asset temp dir: %w", err)
		}
		cacheDir = dir
	}

	dest := filepath.Join(cacheDir, filepath.Base(name))
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return "", fmt.Errorf("firstrun: writing cached asset: %w", err)
		}
	}
	return dest, nil
}

// CleanupAssets removes the temporary directory AssetPath extracted into.
//
// The directory is process-scoped, so nothing outside this process can still
// be reading from it once the process is shutting down; leaving it behind
// accumulates one stray directory per application run.
func CleanupAssets() error {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if cacheDir == "" {
		return nil
	}
	dir := cacheDir
	cacheDir = ""
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("firstrun: removing asset temp dir: %w", err)
	}
	return nil
}
