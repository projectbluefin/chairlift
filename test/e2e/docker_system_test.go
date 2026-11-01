package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/config"
)

// TestIntegrationDockerSystem drives the real GUI and fixed helpers in the
// dedicated rootless-Podman Dakota fixture. The image ships no Docker: the
// helper downloads Homebrew's signed formula index and bottles, installs a
// root-owned runtime with managed units, creates the docker group, and grants
// access; the GUI retires a planted rootless daemon. Network access is required.
func TestIntegrationDockerSystem(t *testing.T) {
	if os.Getenv("CHAIRLIFT_DOCKER_INTEGRATION") != "1" {
		t.Skip("run make e2e-docker: a private native system manager and network access to Homebrew's bottles are required")
	}
	app := filepath.Join(e2eBuildDir(t), "e2e", "chairlift")
	requireExecutable(t, app)
	python := atspiPython()
	requireATSPIStack(t, python)
	outDir := filepath.Join(e2eBuildDir(t), "docker-system")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Disable unrelated live controls, deriving the inventory from the schema.
	pages, err := config.SchemaPages()
	if err != nil {
		t.Fatal(err)
	}
	fixture := make(map[string]map[string]map[string]bool, len(pages))
	for _, page := range pages {
		groups, groupErr := config.SchemaGroups(page)
		if groupErr != nil {
			t.Fatal(groupErr)
		}
		fixture[page] = make(map[string]map[string]bool, len(groups))
		for _, group := range groups {
			fixture[page][group] = map[string]bool{"enabled": page == "features_page" && group == "dx_group"}
		}
	}
	data, err := json.Marshal(fixture) // JSON is also valid YAML.
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "config.dev.yml"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	root := repoRoot(t)
	cmd := exec.Command("dbus-run-session", "--", filepath.Join(root, "test", "e2e", "wayland_session.sh"),
		python, filepath.Join(root, "test", "e2e", "docker_system_integration.py"), app, outDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GDK_DEBUG=no-portals", "GSETTINGS_BACKEND=memory", "GTK_A11Y=atspi",
		"LANG=C", "LC_ALL=C",
		"XDG_RUNTIME_DIR=/tmp/chairlift-docker-runtime",
		"CHAIRLIFT_WAYLAND_LOG_DIR="+outDir,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	output := &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = output, output
	t.Cleanup(func() {
		if cmd.Process != nil {
			if err := awaitSessionExit(hostDrain(), cmd.Process.Pid, shutdownTimeout, drainTimeout); err != nil {
				t.Errorf("Docker integration session: %v", err)
			}
		}
	})
	err = runWithTimeout(cmd, 25*time.Minute)
	t.Logf("live system Docker integration:\n%s", output.String())
	if err != nil {
		t.Fatalf("system Docker activation/UI failed: %v\nartifacts: %s\n%s\n%s", err, outDir,
			output.String(), tailFile(filepath.Join(outDir, "chairlift.log"), 20000))
	}
}
