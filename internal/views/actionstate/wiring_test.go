package actionstate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A button that is made sensitive again after a run must release its gate;
// Complete permanently rejects every future click, including retries after
// a failed or dry-run action. Views cannot be imported by headless tests.
func TestRepeatableControlsReleaseTheirGates(t *testing.T) {
	read := func(file string) string {
		t.Helper()
		return readViewsFile(t, file)
	}
	functionBody := func(source, name string) string {
		t.Helper()
		return viewsFunctionBody(t, source, name)
	}

	bundleInstall := read("bundle_install.go")
	body := functionBody(bundleInstall, "(uh *UserHome) runSelectedBundlesInstall")
	for _, required := range []string{
		"if !uh.updateShell.beginMutation(",
		"uh.bundleInstallGate.Reset()",
		"pageview.UpdateBusyToast",
		"uh.updateShell.finishMutation()",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("runSelectedBundlesInstall must be admitted by the update shell: missing %q", required)
		}
	}
	if strings.Index(body, "uh.bundleInstallGate.TryStart()") > strings.Index(body, "uh.updateShell.beginMutation(") {
		t.Error("runSelectedBundlesInstall must take its own gate before the update shell's admission")
	}
	if finish, reset := strings.Index(body, "uh.updateShell.finishMutation()"), strings.Index(body, "defer uh.bundleInstallGate.Reset()"); finish < 0 || reset < 0 || finish > reset {
		t.Error("runSelectedBundlesInstall must release the admission before its own gate")
	}
	// One release, in the closure every outcome reaches: a failed collection
	// only `continue`s the loop, and a preview runs after the release.
	if n := strings.Count(body, "uh.updateShell.finishMutation()"); n != 1 {
		t.Errorf("runSelectedBundlesInstall releases the admission %d times, want exactly 1 on every path", n)
	}
	if finish, preview := strings.Index(body, "uh.updateShell.finishMutation()"), strings.Index(body, "if dryrun.Enabled()"); finish < 0 || preview < 0 || finish > preview {
		t.Error("runSelectedBundlesInstall must release the admission before its preview path reports")
	}
	if !strings.Contains(bundleInstall, "homebrew.BundleInstallTrusted(") {
		t.Error("a whole-collection install must trust the taps its Brewfile declares before loading casks (#816)")
	}

	cli := read("bluefin_cli.go")
	body = functionBody(cli, "(uh *UserHome) runBluefinCLI")
	for _, required := range []string{
		"if !uh.updateShell.beginMutation(",
		"cli.gate.Reset()",
		"pageview.UpdateBusyToast",
		"uh.updateShell.finishMutation()",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("runBluefinCLI must be admitted by the update shell: missing %q", required)
		}
	}
	if strings.Index(body, "cli.gate.TryStart()") > strings.Index(body, "uh.updateShell.beginMutation(") {
		t.Error("runBluefinCLI must take its own gate before the update shell's admission")
	}
	if finish, reset := strings.LastIndex(body, "uh.updateShell.finishMutation()"), strings.LastIndex(body, "cli.gate.Reset()"); finish < 0 || reset < 0 || finish > reset {
		t.Error("runBluefinCLI must release the admission before reporting its outcome")
	}
	// A failed apply and a preview both report through the same closure, and
	// it releases the admission before either branch writes a toast.
	if n := strings.Count(body, "uh.updateShell.finishMutation()"); n != 1 {
		t.Errorf("runBluefinCLI releases the admission %d times, want exactly 1 on every path", n)
	}
	if finish, preview := strings.Index(body, "uh.updateShell.finishMutation()"), strings.LastIndex(body, "dryrun.Enabled()"); finish < 0 || preview < 0 || finish > preview {
		t.Error("runBluefinCLI must release the admission before its preview path reports")
	}

	developer := read("developer_setup.go")
	for _, required := range []string{
		"func (uh *UserHome) withUpdateShellAdmission(",
		"uh.updateShell.beginMutation(",
		"uh.updateShell.finishMutation()",
		`"Installing the Dev Container CLI"`,
		`"Installing the developer app collection"`,
		"homebrew.BundleInstallTrusted(",
	} {
		if !strings.Contains(developer, required) {
			t.Errorf("Developer Mode Homebrew installs must be admitted by the update shell: missing %q", required)
		}
	}

	// The other pages that install Homebrew packages follow the same shape:
	// their own gate first, then update-shell admission, a visible refusal,
	// and finishMutation on every release path.
	for _, site := range []struct {
		file, function, gate string
		// finishes is how many release paths the site has: the run's one
		// completion closure, plus an early abort that had already admitted.
		finishes int
	}{
		{"agents_page.go", "(uh *UserHome) onAgentModeToggled", "uh.agentModeGate.TryStart()", 1},
		{"troubleshoot.go", "(uh *UserHome) setUpGoose", "uh.gooseGate.TryStart()", 1},
		{"developer_apps.go", "(uh *UserHome) installDeveloperApp", "uh.developerGate.TryStart()", 1},
		{"wallpapers.go", "(uh *UserHome) runWallpaperInstall", "uh.wallpaperGate.TryStart()", 2},
	} {
		body = functionBody(read(site.file), site.function)
		for _, required := range []string{
			"if !uh.updateShell.beginMutation(",
			"pageview.UpdateBusyToast",
			"uh.updateShell.finishMutation()",
		} {
			if !strings.Contains(body, required) {
				t.Errorf("%s must be admitted by the update shell: missing %q", site.function, required)
			}
		}
		if strings.Index(body, site.gate) > strings.Index(body, "uh.updateShell.beginMutation(") {
			t.Errorf("%s must take its own gate before the update shell's admission", site.function)
		}
		if n := strings.Count(body, "uh.updateShell.finishMutation()"); n != site.finishes {
			t.Errorf("%s releases the admission %d times, want %d", site.function, n, site.finishes)
		}
	}
}

// A click to use Goose is the user's acknowledgment, so when a launch
// re-reads live readiness and finds the packages gone (for example after a
// system update removed the Homebrew installs while the profile persists),
// the row must reinstall rather than show an idle "Goose isn't set up yet"
// toast. Issue #845.
func TestLaunchDelegatesToSetupWhenPackagesMissing(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "troubleshoot.go"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(data), "func (uh *UserHome) launchGoose()", 2)
	if len(parts) != 2 {
		t.Fatal("launchGoose not found")
	}
	body, _, _ := strings.Cut(parts[1], "\nfunc ")

	if !strings.Contains(body, "if state.CanSetUp() {") {
		t.Error("launchGoose must re-check readiness with state.CanSetUp()")
	}
	// The delegation runs on the main thread (it touches the row) and reuses
	// the same Set Up path, so one click reinstalls.
	if !strings.Contains(body, "uh.setUpGoose()") || !strings.Contains(body, "sgtk.RunOnMainThread") {
		t.Error("launchGoose must delegate the reinstall to setUpGoose on the main thread")
	}
	// After delegating, launchGoose returns rather than falling through to the
	// launch path with the packages missing.
	branch, _, _ := strings.Cut(body[strings.Index(body, "if state.CanSetUp() {"):], "\n\t\t}\n")
	if !strings.Contains(branch, "uh.setUpGoose()") || !strings.Contains(branch, "return") {
		t.Error("the CanSetUp branch must delegate to setUpGoose and return")
	}
}

// readViewsFile returns a source file of the views package.
func readViewsFile(t *testing.T, file string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate wiring_test.go")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", file))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// viewsFunctionBody returns the text of the named function up to the next one.
func viewsFunctionBody(t *testing.T, source, name string) string {
	t.Helper()
	parts := strings.SplitN(source, "func "+name+"(", 2)
	if len(parts) != 2 {
		t.Fatalf("function %s not found", name)
	}
	body, _, _ := strings.Cut(parts[1], "\nfunc ")
	return body
}

// EnableHardware starts by itself when initial setup opens, usually while the
// startup update check still holds the update shell. Its refusal must not show
// the busy toast, which answers a user action and would greet the Welcome step
// with no input (#864).
func TestEnableHardwareRefusalIsSilentAtSetupLaunch(t *testing.T) {
	body := viewsFunctionBody(t, readViewsFile(t, "hardware_group.go"), "(uh *UserHome) EnableHardware")
	for _, required := range []string{"view.setup.TryStart()", "if !uh.updateShell.beginMutation(", "uh.updateShell.finishMutation()"} {
		if !strings.Contains(body, required) {
			t.Errorf("EnableHardware must still be admitted by the update shell: missing %q", required)
		}
	}
	if strings.Contains(body, "pageview.UpdateBusyToast") {
		t.Error("EnableHardware runs without user input at setup launch and must not show the busy toast")
	}
}
