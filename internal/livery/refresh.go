package livery

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// RefreshShellIcons makes GNOME Shell pick up a newly installed icon without
// the user logging out.
//
// Writing the icon and refreshing the theme cache is not enough on its own:
// the shell caches icon textures by name in StTextureCache, so the dash keeps
// drawing the mark it already has. Touching the applications directory fires
// GAppInfoMonitor, which makes the shell re-read application information and
// resolve the icons again — and that turns out to cover the app-grid glyph as
// well as the Files mark, verified live on a Wayland session by swapping each
// and watching the dock update with nothing else run.
//
// Three heavier alternatives were tried first and are all worse:
//
//   - Disabling and re-enabling dash-to-dock rebuilds the dash *widgets*, but
//     they ask for the same icon name and are handed the same cached texture,
//     so it does not work at all. It is also dangerous: the disable persists
//     to GSettings, so a crash mid-reload leaves the user with no dock across
//     reboots, on a system where dash-to-dock *is* the dock.
//   - Toggling `org.gnome.desktop.interface icon-theme` away and back does
//     work, by clearing the texture cache wholesale. But it mutates a global
//     appearance setting: a crash between the two writes leaves every
//     application on the wrong icon theme, and restoring the value naively
//     pins it as a user override that shadows any later distro change.
//   - `org.gnome.Shell.Eval` and `ReloadExtension` are gated to unsafe-mode
//     since GNOME 41; both introspect and both refuse the call.
//
// What is left is a timestamp update on a directory the user already owns. It
// cannot fail halfway, leaves nothing behind, and needs no recovery path.
func RefreshShellIcons() error {
	dir, err := applicationsDir()
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would touch %s so GNOME Shell re-resolves application icons", dir)
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	now := time.Now()
	if err := os.Chtimes(dir, now, now); err != nil {
		return err
	}
	return nil
}

// applicationsDir is the user's XDG applications directory, whose mtime
// GAppInfoMonitor watches.
func applicationsDir() (string, error) {
	home, err := dataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "applications"), nil
}
