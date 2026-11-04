package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/config"
	"github.com/projectbluefin/chairlift/internal/dockerbrew"
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

// TestDockerRuntimeActivation executes the actual generated startup argv on
// Dakota without a daemon, authentication, host kernel modules, or root.
func TestDockerRuntimeActivation(t *testing.T) {
	root := t.TempDir()
	first, second := "1111111111111111", "2222222222222222"
	for _, id := range []string{first, second} {
		if err := os.Mkdir(filepath.Join(root, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	current, active := filepath.Join(root, "current"), filepath.Join(root, "active")
	if err := os.Symlink(first, current); err != nil {
		t.Fatal(err)
	}
	var argv []string
	for _, line := range strings.Split(dockerbrew.ServiceUnit(root), "\n") {
		if value, ok := strings.CutPrefix(line, "ExecStartPre="); ok {
			argv = strings.Fields(value)
		}
	}
	if len(argv) == 0 {
		t.Fatal("generated service has no activation command")
	}
	command := func() *exec.Cmd { return exec.Command(argv[0], argv[1:]...) }
	assertActive := func(id string) {
		t.Helper()
		got, err := os.Readlink(active)
		if err != nil || got != id {
			t.Fatalf("active -> %q, want %q: %v", got, id, err)
		}
		cur, err := os.Lstat(current)
		if err != nil {
			t.Fatal(err)
		}
		act, err := os.Lstat(active)
		if err != nil || act.Mode()&os.ModeSymlink == 0 || !os.SameFile(cur, act) {
			t.Fatalf("activation did not hard-link the current symlink: %v", err)
		}
	}
	for range 2 {
		if out, err := command().CombinedOutput(); err != nil {
			t.Fatalf("activation %v failed: %v\n%s", argv, err, out)
		}
		assertActive(first)
	}
	lock, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	cmd := command()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("activation ignored the install lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// An install swaps current while activation is blocked on its lock.
	tmp := filepath.Join(root, ".next")
	if err := os.Symlink(second, tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, current); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("activation after the install lock: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("activation did not resume after the install lock")
	}
	assertActive(second)
	if out, err := command().CombinedOutput(); err != nil {
		t.Fatalf("repeated changed-set activation: %v\n%s", err, out)
	}
	assertActive(second)
}
