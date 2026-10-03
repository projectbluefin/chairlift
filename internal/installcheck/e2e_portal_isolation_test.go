package installcheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EHarnessIsolatesRuntimeAndPortals asserts that every E2E harness
// that launches the GTK binary — the dry-run startup smoke test, the
// walkthrough screenshot runner, and the behave AT-SPI suite runner — runs it
// inside test/e2e/wayland_session.sh with a private XDG_RUNTIME_DIR and
// GDK_DEBUG=no-portals, and that the session script itself points GTK at its
// own headless Mutter by absolute socket path.
//
// Without the runtime and portal isolation, a headless run inherits the
// developer's live /run/user/<uid> and can trigger desktop portal actions or
// collide with the host session's xdg-document-portal FUSE mount. Without the
// absolute socket, a client launched with a different XDG_RUNTIME_DIR — every
// behave scenario is — would resolve a bare WAYLAND_DISPLAY name somewhere
// else, possibly on the developer's live compositor.
func TestE2EHarnessIsolatesRuntimeAndPortals(t *testing.T) {
	harnesses := []struct {
		path string
		want []string
	}{
		{
			path: filepath.Join("test", "e2e", "e2e_test.go"),
			want: []string{`"wayland_session.sh"`, `"GDK_DEBUG=no-portals"`, `"XDG_RUNTIME_DIR="`},
		},
		{
			path: filepath.Join("test", "e2e", "walkthrough_test.go"),
			want: []string{`"wayland_session.sh"`, `"XDG_RUNTIME_DIR="`},
		},
		{
			path: filepath.Join("test", "e2e", "capture_walkthrough.sh"),
			want: []string{
				"GDK_DEBUG=no-portals",
				"run capture_walkthrough.sh inside wayland_session.sh",
				"/run/user/*) echo \"refusing the live session's compositor",
			},
		},
		{
			path: filepath.Join("test", "e2e", "run_atspi.sh"),
			want: []string{"wayland_session.sh", "GDK_DEBUG=no-portals", "XDG_RUNTIME_DIR="},
		},
		{
			path: filepath.Join("test", "e2e", "wayland_session.sh"),
			want: []string{
				"--headless", "--no-x11",
				`export WAYLAND_DISPLAY="$SOCKET"`,
				"export GDK_BACKEND=wayland",
				"unset DISPLAY",
				"/run/user/*) echo \"refusing the live session's runtime directory",
			},
		},
	}
	for _, harness := range harnesses {
		src := readRepoFile(t, harness.path)
		for _, want := range harness.want {
			if !strings.Contains(src, want) {
				t.Errorf("%s does not contain %q", harness.path, want)
			}
		}
	}
}

// x11Tooling names what the E2E harness used before it ran on native Wayland:
// an X server, X clients, and the packages that ship them. Bluefin and Dakota
// are Wayland-only and the harness tests ChairLift on headless Mutter, so none
// of these has a reason to come back.
var x11Tooling = []string{
	"Xvfb", "xvfb", "xorg-server", "Xwayland",
	"xdotool", "xdpyinfo", "xwd", "x11-apps", "x11-utils", "xauth", "dbus-x11",
	"GDK_BACKEND=x11", "DISPLAY=:",
}

// TestE2EHarnessHasNoX11Dependency scans every file the E2E harness and its
// CI execute — scripts, Go, Python, workflow and action YAML, the Makefile —
// for X11 tooling. Prose is out of scope on purpose: documentation has to be
// able to say why the harness left X11 behind. A test that needs a display
// gets wayland_session.sh; a test that needs input or a screenshot gets
// features/lib/wayland_remote.py.
func TestE2EHarnessHasNoX11Dependency(t *testing.T) {
	root := RepoRoot()
	var files []string
	for _, dir := range []string{filepath.Join("test", "e2e"), filepath.Join(".github")} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(entry.Name(), ".md") {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	files = append(files, filepath.Join(root, "Makefile"))
	if len(files) < 10 {
		t.Fatalf("scanned only %d files; the walk is broken, not the harness", len(files))
	}

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)
		for number, line := range strings.Split(string(data), "\n") {
			for _, token := range x11Tooling {
				if strings.Contains(line, token) {
					t.Errorf("%s:%d names %q; the E2E harness runs on native Wayland:\n  %s",
						rel, number+1, token, strings.TrimSpace(line))
				}
			}
		}
	}
}
