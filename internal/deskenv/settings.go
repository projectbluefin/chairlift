package deskenv

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Homebrew GLib otherwise uses its keyfile store while GNOME uses dconf.
// Make the installed native backend visible to both bindings and child tools.
var settingsModuleDirs = []string{
	"/usr/lib64/gio/modules",
	"/usr/lib/gio/modules",
	nativeMultiarchModules(),
}

func nativeMultiarchModules() string {
	switch runtime.GOARCH {
	case "amd64":
		return "/usr/lib/x86_64-linux-gnu/gio/modules"
	case "arm64":
		return "/usr/lib/aarch64-linux-gnu/gio/modules"
	default:
		return ""
	}
}

// ConfigureSettingsModules runs before GTK or headless rotation uses settings.
// Explicit GSETTINGS_BACKEND and existing extra modules remain untouched.
func ConfigureSettingsModules() error {
	for _, dir := range settingsModuleDirs {
		if dir == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, "libdconfsettings.so"))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		existing := os.Getenv("GIO_EXTRA_MODULES")
		for current := range strings.SplitSeq(existing, string(os.PathListSeparator)) {
			if current == dir {
				return nil
			}
		}
		if existing != "" {
			dir = existing + string(os.PathListSeparator) + dir
		}
		return os.Setenv("GIO_EXTRA_MODULES", dir)
	}
	return nil
}
