package e2e

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/navigation"
)

const atspiTimeout = 3 * time.Minute

// atspiRow is one sidebar row as the accessibility tree reported it.
type atspiRow struct {
	Index    int
	Name     string
	Selected bool
}

// atspiPage is the tree state observed after navigating to one page.
type atspiPage struct {
	Page string
	// SelectedIndex is the position of the single selected sidebar row, or
	// -1 when the count was not exactly one.
	SelectedIndex int
	SelectedName  string
	// SelectedCount distinguishes "nothing selected" from "two rows claim
	// to be selected"; both are failures, for different reasons.
	SelectedCount int
	// ContentTitleLabels counts labels outside the sidebar carrying the
	// page title, which is how the content side announces which page it is
	// showing.
	ContentTitleLabels int
}

// atspiControls is the control accessibility observed on one page.
type atspiControls struct {
	Page                  string
	ControlCount          int
	NamelessCount         int
	InoperableCount       int
	ToggleCount           int
	UnreadableToggleCount int
}

type atspiMenuButton struct {
	Name    string
	Role    string
	Showing bool
}

type atspiShortcutsDialog struct {
	Name       string
	Focused    bool
	HasUpdates bool
	HasQuit    bool
}

type atspiAboutDialog struct {
	Name         string
	Focused      bool
	AnnouncesApp bool
}

type atspiReport struct {
	Rows             []atspiRow
	Pages            []atspiPage
	Controls         []atspiControls
	MenuButton       atspiMenuButton
	PopoverItemCount int
	ShortcutsDialog  atspiShortcutsDialog
	AboutDialog      atspiAboutDialog
	Done             bool
}

// parseATSPIReport turns the probe's tab-separated records into a report.
//
// The probe makes no judgements, so every expectation lives on this side, in
// the package that can consult navigation directly. Parsing is strict:
// an unknown record or a malformed field is an error rather than a silently
// dropped line, because a probe that half-worked must not read as a pass.
func parseATSPIReport(output string) (atspiReport, error) {
	var report atspiReport

	for number, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if report.Done {
			return atspiReport{}, fmt.Errorf("line %d: record %q follows DONE", number+1, line)
		}

		parts := strings.Split(line, "\t")
		fields, err := atspiFields(parts[1:])
		if err != nil {
			return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
		}

		switch parts[0] {
		case "ROW":
			row, err := parseATSPIRow(fields)
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			report.Rows = append(report.Rows, row)
		case "PAGE":
			page, err := parseATSPIPage(fields)
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			report.Pages = append(report.Pages, page)
		case "CONTROLS":
			controls, err := parseATSPIControls(fields)
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			report.Controls = append(report.Controls, controls)
		case "MENU_BUTTON":
			name, err := atspiString(fields, "name")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			role, err := atspiString(fields, "role")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			showing, err := atspiInt(fields, "showing")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			report.MenuButton = atspiMenuButton{Name: name, Role: role, Showing: showing == 1}
		case "POPOVER":
			count, err := atspiInt(fields, "item_count")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			report.PopoverItemCount = count
		case "DIALOG_SHORTCUTS":
			name, err := atspiString(fields, "name")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			focused, err := atspiInt(fields, "focused")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			hasUpdates, err := atspiInt(fields, "has_updates")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			hasQuit, err := atspiInt(fields, "has_quit")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			report.ShortcutsDialog = atspiShortcutsDialog{
				Name:       name,
				Focused:    focused == 1,
				HasUpdates: hasUpdates == 1,
				HasQuit:    hasQuit == 1,
			}
		case "DIALOG_ABOUT":
			name, err := atspiString(fields, "name")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			focused, err := atspiInt(fields, "focused")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			announces, err := atspiInt(fields, "announces_app")
			if err != nil {
				return atspiReport{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			report.AboutDialog = atspiAboutDialog{
				Name:         name,
				Focused:      focused == 1,
				AnnouncesApp: announces == 1,
			}
		case "DONE":
			report.Done = true
		default:
			return atspiReport{}, fmt.Errorf("line %d: unknown record %q", number+1, parts[0])
		}
	}

	return report, nil
}

func atspiFields(parts []string) (map[string]string, error) {
	fields := make(map[string]string, len(parts))
	for _, part := range parts {
		key, value, found := strings.Cut(part, "=")
		if !found {
			return nil, fmt.Errorf("field %q is not key=value", part)
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, fmt.Errorf("field %q appears twice", key)
		}
		fields[key] = value
	}
	return fields, nil
}

func atspiInt(fields map[string]string, key string) (int, error) {
	raw, ok := fields[key]
	if !ok {
		return 0, fmt.Errorf("field %q is missing", key)
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("field %q = %q is not a number", key, raw)
	}
	return value, nil
}

func atspiString(fields map[string]string, key string) (string, error) {
	value, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("field %q is missing", key)
	}
	return value, nil
}

func parseATSPIRow(fields map[string]string) (atspiRow, error) {
	index, err := atspiInt(fields, "index")
	if err != nil {
		return atspiRow{}, err
	}
	name, err := atspiString(fields, "name")
	if err != nil {
		return atspiRow{}, err
	}
	selected, err := atspiInt(fields, "selected")
	if err != nil {
		return atspiRow{}, err
	}
	if selected != 0 && selected != 1 {
		return atspiRow{}, fmt.Errorf("field \"selected\" = %d, want 0 or 1", selected)
	}
	return atspiRow{Index: index, Name: name, Selected: selected == 1}, nil
}

func parseATSPIPage(fields map[string]string) (atspiPage, error) {
	page, err := atspiString(fields, "page")
	if err != nil {
		return atspiPage{}, err
	}
	selectedIndex, err := atspiInt(fields, "selected_index")
	if err != nil {
		return atspiPage{}, err
	}
	selectedName, err := atspiString(fields, "selected_name")
	if err != nil {
		return atspiPage{}, err
	}
	selectedCount, err := atspiInt(fields, "selected_count")
	if err != nil {
		return atspiPage{}, err
	}
	contentTitleLabels, err := atspiInt(fields, "content_title_labels")
	if err != nil {
		return atspiPage{}, err
	}
	return atspiPage{
		Page:               page,
		SelectedIndex:      selectedIndex,
		SelectedName:       selectedName,
		SelectedCount:      selectedCount,
		ContentTitleLabels: contentTitleLabels,
	}, nil
}

func parseATSPIControls(fields map[string]string) (atspiControls, error) {
	page, err := atspiString(fields, "page")
	if err != nil {
		return atspiControls{}, err
	}
	controlCount, err := atspiInt(fields, "control_count")
	if err != nil {
		return atspiControls{}, err
	}
	namelessCount, err := atspiInt(fields, "nameless_count")
	if err != nil {
		return atspiControls{}, err
	}
	inoperableCount, err := atspiInt(fields, "inoperable_count")
	if err != nil {
		return atspiControls{}, err
	}
	toggleCount, err := atspiInt(fields, "toggle_count")
	if err != nil {
		return atspiControls{}, err
	}
	unreadableToggleCount, err := atspiInt(fields, "unreadable_toggle_count")
	if err != nil {
		return atspiControls{}, err
	}
	return atspiControls{
		Page:                  page,
		ControlCount:          controlCount,
		NamelessCount:         namelessCount,
		InoperableCount:       inoperableCount,
		ToggleCount:           toggleCount,
		UnreadableToggleCount: unreadableToggleCount,
	}, nil
}

// TestATSPINavigationTree drives the real application through every
// navigation page with the accessibility bridge enabled and asserts what an
// assistive technology would observe.
//
// What this verifies, precisely: the application publishes itself on the
// accessibility bus at all; the sidebar is exposed as a list whose rows
// carry the page titles as accessible names, none of them blank; and each
// Alt+<number> accelerator leaves exactly one row selected — the one
// navigation.Resolve says it should be — with the content side announcing
// that page's title. What it does not verify is that any page is usable or
// looks correct; the tree says what is announced, not whether the
// announcement is good.
//
// This is the assertion layer ADR-0008 left out on purpose. That ADR rejected
// AT-SPI *as a readiness signal*, because the application's own log markers
// say more about startup and a11y was switched off for hermeticity. Startup
// readiness here still comes from those markers; the accessibility bus is
// enabled only afterwards, and only inside the run's private D-Bus session,
// so the hermeticity argument is preserved.
//
// The run is in --dry-run, so no state-changing operation can execute while
// the tree is read.
func TestATSPINavigationTree(t *testing.T) {
	// The walkthrough's chairlift_e2e-tagged binary, for the same reason it
	// uses it: the untagged binary beside it is replaced mid-suite by the
	// staged-install test's `make install`.
	app := filepath.Join(e2eBuildDir(t), "e2e", "chairlift")
	requireExecutable(t, app)

	script := filepath.Join(repoRoot(t), "test", "e2e", "run_atspi_navigation.sh")
	requireExecutable(t, script)

	requireATSPIStack(t)

	// Page names, titles and order come from internal/navigation, the single
	// authority for the accelerators the script presses. A page added there
	// is asserted here without touching this test.
	items := navigation.Items()
	if len(items) == 0 {
		t.Fatal("navigation.Items() is empty; there is nothing to navigate")
	}

	outDir := t.TempDir()
	args := []string{app, outDir}
	for _, item := range items {
		args = append(args, item.Name, item.Title)
	}

	cmd := exec.Command(script, args...)
	cmd.Dir = repoRoot(t)
	// A private session, as the smoke test uses: startup reads the Homebrew
	// inventory, and brew workers outlive the script and keep writing into
	// the temporary HOME, which makes t.TempDir's cleanup fail.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	output := &lockedBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	// Cleanups run last-registered-first, so this drain runs before Go
	// removes the TempDir registered above, on every exit path including
	// t.Fatal and the timeout below.
	t.Cleanup(func() {
		if cmd.Process == nil {
			return
		}
		if err := awaitSessionExit(hostDrain(), cmd.Process.Pid, shutdownTimeout, drainTimeout); err != nil {
			t.Errorf("AT-SPI probe session %d: %v", cmd.Process.Pid, err)
		}
	})

	if err := runWithTimeout(cmd, atspiTimeout); err != nil {
		t.Fatalf("AT-SPI navigation probe failed: %v\n%s\n%s", err, output.String(), atspiLogs(outDir))
	}

	raw, err := os.ReadFile(filepath.Join(outDir, "atspi-results.txt"))
	if err != nil {
		t.Fatalf("read probe results: %v\n%s", err, atspiLogs(outDir))
	}

	report, err := parseATSPIReport(string(raw))
	if err != nil {
		t.Fatalf("parse probe results: %v\nresults:\n%s", err, raw)
	}
	if !report.Done {
		t.Fatalf("probe did not finish; results:\n%s\n%s", raw, atspiLogs(outDir))
	}

	assertATSPISidebar(t, report.Rows, items)
	assertATSPIPages(t, report.Pages, items)
	assertATSPIControls(t, report.Controls, items)
	assertATSPIMenusAndDialogs(t, report)
}

// assertATSPIControls verifies that focusable action controls are accessible across all pages.
func assertATSPIControls(t *testing.T, controls []atspiControls, items []navigation.Item) {
	t.Helper()
	if len(controls) != len(items) {
		t.Errorf("probe reported controls for %d pages, want %d", len(controls), len(items))
	}
	for _, c := range controls {
		if c.ControlCount == 0 {
			t.Errorf("page %q reported 0 focusable action controls", c.Page)
		}
		if c.NamelessCount > 0 {
			t.Errorf("page %q has %d nameless focusable action controls", c.Page, c.NamelessCount)
		}
		if c.InoperableCount > 0 {
			t.Errorf("page %q has %d inoperable action controls", c.Page, c.InoperableCount)
		}
		if c.UnreadableToggleCount > 0 {
			t.Errorf("page %q has %d toggles whose checked state could not be read", c.Page, c.UnreadableToggleCount)
		}
	}
}

// assertATSPIMenusAndDialogs verifies the Main Menu button, popover, shortcuts dialog, and about dialog.
func assertATSPIMenusAndDialogs(t *testing.T, report atspiReport) {
	t.Helper()
	if report.MenuButton.Name != "Main Menu" {
		t.Errorf("Main Menu button name = %q, want %q", report.MenuButton.Name, "Main Menu")
	}
	if !report.MenuButton.Showing {
		t.Errorf("Main Menu button is not showing")
	}
	if report.PopoverItemCount < 2 {
		t.Errorf("popover reported %d items, want at least 2", report.PopoverItemCount)
	}
	if report.ShortcutsDialog.Name != "Keyboard Shortcuts" {
		t.Errorf("shortcuts dialog name = %q, want %q", report.ShortcutsDialog.Name, "Keyboard Shortcuts")
	}
	if !report.ShortcutsDialog.Focused {
		t.Errorf("shortcuts dialog did not receive AT-SPI focus")
	}
	if !report.ShortcutsDialog.HasUpdates {
		t.Errorf("shortcuts dialog does not list \"Go to Updates\"")
	}
	if !report.ShortcutsDialog.HasQuit {
		t.Errorf("shortcuts dialog does not list \"Quit\"")
	}
	if report.AboutDialog.Name != "About" && report.AboutDialog.Name != "About "+branding.AppName {
		t.Errorf("about dialog name = %q, want \"About\" or %q", report.AboutDialog.Name, "About "+branding.AppName)
	}
	if !report.AboutDialog.Focused {
		t.Errorf("about dialog did not receive AT-SPI focus")
	}
	if !report.AboutDialog.AnnouncesApp {
		t.Errorf("about dialog does not announce the application name")
	}
}

// assertATSPISidebar holds the part of the contract that is about the
// sidebar's existence and labelling, independent of navigation.
func assertATSPISidebar(t *testing.T, rows []atspiRow, items []navigation.Item) {
	t.Helper()

	if len(rows) != len(items) {
		t.Fatalf("accessibility tree exposes %d sidebar rows, want %d (%s); "+
			"a mismatch also invalidates the Alt+<number> mapping the probe used",
			len(rows), len(items), navigationTitles(items))
	}
	for index, row := range rows {
		if row.Index != index {
			t.Errorf("sidebar row %d reported index %d", index, row.Index)
		}
		// A blank accessible name is the accessibility failure that matters
		// most here: the row is reachable and operable, and a screen reader
		// can say nothing about it.
		if row.Name == "" {
			t.Errorf("sidebar row %d (%s) has a blank accessible name", index, items[index].Title)
			continue
		}
		if row.Name != items[index].Title {
			t.Errorf("sidebar row %d accessible name = %q, want %q", index, row.Name, items[index].Title)
		}
	}
}

// assertATSPIPages holds the navigation contract: every accelerator lands on
// the page navigation.Resolve says it lands on, and says so in the tree.
func assertATSPIPages(t *testing.T, pages []atspiPage, items []navigation.Item) {
	t.Helper()

	if len(pages) != len(items) {
		t.Fatalf("probe reported %d page transitions, want %d", len(pages), len(items))
	}

	available := func(string) bool { return true }
	for index, page := range pages {
		item := items[index]
		if page.Page != item.Name {
			t.Errorf("transition %d is for page %q, want %q", index, page.Page, item.Name)
			continue
		}

		want, ok := navigation.Resolve(item.Name, items, available)
		if !ok {
			t.Fatalf("navigation.Resolve(%q) reports the page is unreachable; the test's own inventory is wrong", item.Name)
		}

		if page.SelectedCount != 1 {
			t.Errorf("after Alt+%d (%s), %d sidebar rows are selected, want exactly 1",
				index+1, item.Title, page.SelectedCount)
			continue
		}
		if page.SelectedIndex != want.SelectedIndex {
			t.Errorf("after Alt+%d (%s), selected sidebar row is %d (%q), want %d",
				index+1, item.Title, page.SelectedIndex, page.SelectedName, want.SelectedIndex)
		}
		if page.SelectedName != want.Title {
			t.Errorf("after Alt+%d, selected row announces %q, want %q",
				index+1, page.SelectedName, want.Title)
		}
		// The sidebar subtree is excluded from this count, so it is the
		// content area — not the row that was just selected — announcing
		// which page is showing.
		if page.ContentTitleLabels < 1 {
			t.Errorf("after Alt+%d, no label outside the sidebar announces %q; "+
				"the content area does not identify the page it is showing",
				index+1, want.Title)
		}
	}
}

func navigationTitles(items []navigation.Item) string {
	titles := make([]string, 0, len(items))
	for _, item := range items {
		titles = append(titles, item.Title)
	}
	return strings.Join(titles, ", ")
}

func atspiLogs(outDir string) string {
	var builder strings.Builder
	for _, name := range []string{"chairlift.log", "probe.log"} {
		data, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			fmt.Fprintf(&builder, "%s: unreadable (%v)\n", name, err)
			continue
		}
		fmt.Fprintf(&builder, "--- %s ---\n%s\n", name, data)
	}
	return builder.String()
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

// requireATSPIStack skips rather than fails when the accessibility runtime is
// absent, which is the opposite of how the rest of this package treats a
// missing dependency.
//
// The E2E CI job installs at-spi2-core, gir1.2-atspi-2.0 and python3-dogtail,
// so there the assertions bind. A developer machine without them gets a skip
// naming exactly what is missing, so a run that quietly proves nothing says
// so; it never weakens the assertions where the stack is present.
func requireATSPIStack(t *testing.T) {
	t.Helper()

	var missing []string
	for _, command := range []string{"Xvfb", "xdpyinfo", "dbus-run-session", "python3"} {
		if _, err := exec.LookPath(command); err != nil {
			missing = append(missing, command)
		}
	}
	if len(missing) == 0 {
		if err := exec.Command("python3", "-c", "import dogtail").Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				missing = append(missing, "python3-dogtail")
			} else {
				missing = append(missing, fmt.Sprintf("python3-dogtail (probe failed: %v)", err))
			}
		}
	}
	if len(missing) > 0 {
		t.Skipf("accessibility stack is unavailable, so nothing about accessibility is being proven here; install: %s",
			strings.Join(missing, ", "))
	}
}
