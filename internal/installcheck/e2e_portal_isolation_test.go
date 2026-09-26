package installcheck

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EHarnessIsolatesRuntimeAndPortals asserts that every E2E harness
// that launches the GTK binary — the dry-run startup smoke test, the
// walkthrough screenshot runner, and the behave AT-SPI suite runner —
// isolates XDG_RUNTIME_DIR to a private directory, sets GDK_DEBUG=no-portals,
// and forces the X11 backend with WAYLAND_DISPLAY cleared.
//
// Without the runtime and portal isolation, a headless run inherits the
// developer's live /run/user/<uid> and can trigger desktop portal actions or
// collide with the host session's xdg-document-portal FUSE mount. Without the
// backend isolation, GTK 4 prefers Wayland whenever WAYLAND_DISPLAY is set, so
// a developer's run opens its window on the live compositor instead of the
// private Xvfb display — CI never noticed because runners have no Wayland.
func TestE2EHarnessIsolatesRuntimeAndPortals(t *testing.T) {
	harnesses := []struct {
		path string
		want []string
	}{
		{
			path: filepath.Join("test", "e2e", "e2e_test.go"),
			want: []string{`"GDK_DEBUG=no-portals"`, `"XDG_RUNTIME_DIR="`, `"GDK_BACKEND=x11"`, `"WAYLAND_DISPLAY="`},
		},
		{
			path: filepath.Join("test", "e2e", "capture_walkthrough.sh"),
			want: []string{"GDK_DEBUG=no-portals", "XDG_RUNTIME_DIR=", "GDK_BACKEND=x11", "unset WAYLAND_DISPLAY"},
		},
		{
			path: filepath.Join("test", "e2e", "run_atspi.sh"),
			want: []string{"GDK_DEBUG=no-portals", "XDG_RUNTIME_DIR=", "GDK_BACKEND=x11", "unset WAYLAND_DISPLAY"},
		},
	}
	for _, harness := range harnesses {
		src := readRepoFile(t, harness.path)
		for _, want := range harness.want {
			if !strings.Contains(src, want) {
				t.Errorf("%s does not set %s in the application environment", harness.path, want)
			}
		}
	}
}
