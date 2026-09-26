package e2e

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/navigation"
)

// atspiTimeout bounds the whole behave run. Each scenario launches its own
// application, so this grows with the suite; the per-step deadlines inside
// the suite are what catch a hung control.
const atspiTimeout = 25 * time.Minute

// requireATSPIEnv makes a missing accessibility stack a failure instead of a
// skip. The E2E workflow sets it: a skipping gate proves nothing, and before
// the job installed the stack a real regression on main went unnoticed.
const requireATSPIEnv = "CHAIRLIFT_REQUIRE_ATSPI"

// atspiPythonEnv names the interpreter that has behave and dogtail, for a
// venv created from test/e2e/requirements-atspi.txt. Defaults to python3.
const atspiPythonEnv = "CHAIRLIFT_ATSPI_PYTHON"

// atspiTagsEnv narrows the run to behave tag expressions, e.g.
// CHAIRLIFT_ATSPI_TAGS=@maintenance while iterating on one destination.
const atspiTagsEnv = "CHAIRLIFT_ATSPI_TAGS"

// atspiOutEnv keeps the run's artifacts (JUnit XML, logs, failure tree dumps
// and screenshots) in a named directory the E2E workflow uploads.
const atspiOutEnv = "CHAIRLIFT_ATSPI_OUT"

type atspiNavItem struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// TestATSPIBehaveSuite runs test/e2e/features — ChairLift's behave suite in
// projectbluefin/testsuite's shape — against the real application, headless.
//
// Readiness still comes from the application's own log markers, as ADR-0008
// requires; the accessibility bus is only what the steps read and drive, and
// it lives inside the run's private D-Bus session. Every scenario launches
// ChairLift with --dry-run and a private action journal, so a step can prove
// which privileged command a click would have run without running it.
//
// The page and shortcut inventories are passed in from internal/navigation,
// the single authority for both, so the suite cannot drift from them.
func TestATSPIBehaveSuite(t *testing.T) {
	// The chairlift_e2e-tagged binary: the untagged one beside it is replaced
	// mid-suite by the staged-install test's `make install`.
	app := filepath.Join(e2eBuildDir(t), "e2e", "chairlift")
	requireExecutable(t, app)

	script := filepath.Join(repoRoot(t), "test", "e2e", "run_atspi.sh")
	requireExecutable(t, script)

	python := atspiPython()
	requireATSPIStack(t, python)

	items := navigation.Items()
	if len(items) == 0 {
		t.Fatal("navigation.Items() is empty; there is nothing to navigate")
	}
	nav := make([]atspiNavItem, 0, len(items))
	for _, item := range items {
		nav = append(nav, atspiNavItem{Name: item.Name, Title: item.Title})
	}
	navJSON, err := json.Marshal(nav)
	if err != nil {
		t.Fatalf("encode navigation inventory: %v", err)
	}
	var shortcutTitles []string
	for _, shortcut := range navigation.Shortcuts(items) {
		shortcutTitles = append(shortcutTitles, shortcut.Title)
	}
	shortcutJSON, err := json.Marshal(shortcutTitles)
	if err != nil {
		t.Fatalf("encode shortcut inventory: %v", err)
	}

	outDir := os.Getenv(atspiOutEnv)
	if outDir == "" {
		outDir = t.TempDir()
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create %s: %v", outDir, err)
	}

	args := []string{app, outDir}
	if tags := os.Getenv(atspiTagsEnv); tags != "" {
		args = append(args, "--tags", tags)
	}

	cmd := exec.Command(script, args...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"CHAIRLIFT_NAVIGATION="+string(navJSON),
		"CHAIRLIFT_SHORTCUTS="+string(shortcutJSON),
		"CHAIRLIFT_APP_NAME="+branding.AppName,
		atspiPythonEnv+"="+python,
	)
	// A private session: startup's Homebrew readers outlive each scenario's
	// application in their own process groups, so only a session-wide drain
	// catches them before the output directory is removed.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	output := &lockedBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	// Registered after t.TempDir, so it runs first.
	t.Cleanup(func() {
		if cmd.Process == nil {
			return
		}
		if err := awaitSessionExit(hostDrain(), cmd.Process.Pid, shutdownTimeout, drainTimeout); err != nil {
			t.Errorf("AT-SPI suite session %d: %v", cmd.Process.Pid, err)
		}
	})

	runErr := runWithTimeout(cmd, atspiTimeout)
	t.Logf("behave output:\n%s", output.String())
	if runErr != nil {
		t.Fatalf("AT-SPI behave suite failed: %v\nartifacts: %s\n%s", runErr, outDir, tailFile(filepath.Join(outDir, "behave.log"), 20000))
	}

	assertATSPIExecutedScenarios(t, outDir)
}

// TestATSPIFeaturesHaveNoUndefinedSteps parses every feature with behave's
// dry run. It needs no display, bus or GTK, so a misspelled step fails in
// seconds instead of after the application has started.
func TestATSPIFeaturesHaveNoUndefinedSteps(t *testing.T) {
	python := atspiPython()
	requireATSPIPython(t, python)

	features := filepath.Join(repoRoot(t), "test", "e2e", "features")
	cmd := exec.Command(python, "-m", "behave", features, "--dry-run", "--no-summary", "--format", "null")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "CHAIRLIFT_ATSPI_APP=/nonexistent", "CHAIRLIFT_ATSPI_OUT="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("behave --dry-run failed (undefined or ambiguous steps): %v\n%s", err, out)
	}
}

func atspiPython() string {
	if python := os.Getenv(atspiPythonEnv); python != "" {
		return python
	}
	return "python3"
}

// runWithTimeout runs cmd to completion or kills it after timeout. For a
// command started in its own session (Setsid), the kill covers the whole
// process group, not just the leader. WaitDelay bounds Wait even if a
// descendant outside that group still holds the captured output pipes;
// callers drain the session with awaitSessionExit afterwards.
func runWithTimeout(cmd *exec.Cmd, timeout time.Duration) error {
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = shutdownTimeout
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setsid {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		} else {
			_ = cmd.Process.Kill()
		}
		<-done
		return fmt.Errorf("%s did not finish within %s", cmd.Path, timeout)
	}
}

func tailFile(path string, limit int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("%s: unreadable (%v)", path, err)
	}
	if len(data) > limit {
		data = data[len(data)-limit:]
	}
	return fmt.Sprintf("--- %s ---\n%s", path, data)
}

// requireATSPIStack skips when the accessibility runtime is absent, naming
// exactly what is missing — unless CHAIRLIFT_REQUIRE_ATSPI is set, as the E2E
// workflow sets it, in which case the absence is a failure.
func requireATSPIStack(t *testing.T, python string) {
	t.Helper()
	var missing []string
	for _, command := range []string{"Xvfb", "dbus-run-session"} {
		if _, err := exec.LookPath(command); err != nil {
			missing = append(missing, command)
		}
	}
	missing = append(missing, missingPythonModules(python)...)
	reportMissing(t, missing)
}

// requireATSPIPython is requireATSPIStack for checks that need only behave.
func requireATSPIPython(t *testing.T, python string) {
	t.Helper()
	reportMissing(t, missingPythonModules(python))
}

func missingPythonModules(python string) []string {
	if _, err := exec.LookPath(python); err != nil {
		return []string{python}
	}
	var missing []string
	for _, module := range []string{"behave", "dogtail", "pyatspi"} {
		probe := "import " + module
		if module == "dogtail" {
			// dogtail.tree and dogtail.rawinput connect to the bus on import,
			// so probe what they need instead: its config module and the
			// GTK 3 typelibs rawinput requires (gir1.2-gtk-3.0).
			probe = "from dogtail.config import config; import gi; gi.require_version('Gtk', '3.0'); gi.require_version('Gdk', '3.0')"
		}
		if err := exec.Command(python, "-c", probe).Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				missing = append(missing, "python module "+module)
			} else {
				missing = append(missing, fmt.Sprintf("python module %s (probe failed: %v)", module, err))
			}
		}
	}
	return missing
}

func reportMissing(t *testing.T, missing []string) {
	t.Helper()
	if len(missing) == 0 {
		return
	}
	message := fmt.Sprintf("accessibility stack is unavailable, so nothing about accessibility is being proven here; install: %s (see test/e2e/requirements-atspi.txt)",
		strings.Join(missing, ", "))
	if os.Getenv(requireATSPIEnv) != "" {
		t.Fatal(message)
	}
	t.Skip(message)
}

type junitTestSuite struct {
	Name     string `xml:"name,attr"`
	Tests    int    `xml:"tests,attr"`
	Errors   int    `xml:"errors,attr"`
	Failures int    `xml:"failures,attr"`
	Skipped  int    `xml:"skipped,attr"`
}

func assertATSPIExecutedScenarios(t *testing.T, outDir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(outDir, "junit", "*.xml"))
	if err != nil {
		t.Fatalf("finding junit XML files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("AT-SPI suite produced no JUnit XML files under %s/junit", outDir)
	}
	totalExecuted := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		var suite junitTestSuite
		if err := xml.Unmarshal(data, &suite); err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}
		executed := suite.Tests - suite.Skipped
		if executed > 0 {
			totalExecuted += executed
		}
	}
	if totalExecuted == 0 {
		t.Fatalf("AT-SPI suite executed 0 scenarios across %d testsuites (all skipped or empty); a run with no executed tests cannot pass", len(files))
	}
}
