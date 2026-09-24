package installcheck

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EHarnessIsolatesRuntimeAndPortals asserts that both the E2E dry-run
// startup smoke test and the walkthrough screenshot runner explicitly isolate
// XDG_RUNTIME_DIR to a private directory and set GDK_DEBUG=no-portals.
//
// Without this isolation, a headless test run inherits the developer's live
// /run/user/<uid> directory and can trigger desktop portal actions or collide
// with the host session's xdg-document-portal FUSE mount.
func TestE2EHarnessIsolatesRuntimeAndPortals(t *testing.T) {
	e2eTestPath := filepath.Join("test", "e2e", "e2e_test.go")
	e2eSrc := readRepoFile(t, e2eTestPath)

	for _, want := range []string{
		`"GDK_DEBUG=no-portals"`,
		`"XDG_RUNTIME_DIR="`,
	} {
		if !strings.Contains(e2eSrc, want) {
			t.Errorf("%s does not set %s in the smoke process environment", e2eTestPath, want)
		}
	}

	captureScriptPath := filepath.Join("test", "e2e", "capture_walkthrough.sh")
	captureSrc := readRepoFile(t, captureScriptPath)

	for _, want := range []string{
		"GDK_DEBUG=no-portals",
		"XDG_RUNTIME_DIR=",
	} {
		if !strings.Contains(captureSrc, want) {
			t.Errorf("%s does not set %s in the capture environment", captureScriptPath, want)
		}
	}
}
