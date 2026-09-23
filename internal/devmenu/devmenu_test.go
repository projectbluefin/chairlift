package devmenu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestParseAndFormatEntry(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Entry
		wantFmt string
	}{
		{
			name: "bluefin terminal",
			raw:  "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', true)",
			want: Entry{
				Label:   "Terminal",
				Command: "ptyxis --new-window",
				Icon:    "utilities-terminal-symbolic",
				Visible: true,
			},
			wantFmt: "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', true)",
		},
		{
			name: "dakota containers",
			raw:  "('Containers', '/usr/bin/flatpak run com.ranfdev.DistroShelf', 'org.gnome.Boxes', false)",
			want: Entry{
				Label:   "Containers",
				Command: "/usr/bin/flatpak run com.ranfdev.DistroShelf",
				Icon:    "org.gnome.Boxes",
				Visible: false,
			},
			wantFmt: "('Containers', '/usr/bin/flatpak run com.ranfdev.DistroShelf', 'org.gnome.Boxes', false)",
		},
		{
			name: "escaped quotes, backslashes, and control characters",
			raw:  `('Terminal\'s Choice', 'echo \'hello\n\t\rworld\'', 'icon\\test', true)`,
			want: Entry{
				Label:   "Terminal's Choice",
				Command: "echo 'hello\n\t\rworld'",
				Icon:    `icon\test`,
				Visible: true,
			},
			wantFmt: `('Terminal\'s Choice', 'echo \'hello\n\t\rworld\'', 'icon\\test', true)`,
		},
		{
			name: "double quoted strings in tuple",
			raw:  `("Containers", "podman ps", "box-icon", false)`,
			want: Entry{
				Label:   "Containers",
				Command: "podman ps",
				Icon:    "box-icon",
				Visible: false,
			},
			wantFmt: "('Containers', 'podman ps', 'box-icon', false)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseEntry(tc.raw)
			if err != nil {
				t.Fatalf("ParseEntry(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("ParseEntry(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
			formatted := FormatEntry(got)
			if formatted != tc.wantFmt {
				t.Errorf("FormatEntry() = %q, want %q", formatted, tc.wantFmt)
			}
		})
	}
}

func TestParseEntryErrors(t *testing.T) {
	invalidInputs := []string{
		"",
		"not-a-tuple",
		"()",
		"('only-one')",
		"('one', 'two')",
		"('one', 'two', 'three')",
		"('one', 'two', 'three', notabool)",
		"('unterminated, 'two', 'three', true)",
		"('one' 'no-comma', 'three', true)",
	}

	for _, in := range invalidInputs {
		t.Run(in, func(t *testing.T) {
			_, err := ParseEntry(in)
			if err == nil {
				t.Errorf("ParseEntry(%q) expected error, got nil", in)
			}
		})
	}
}

func TestAvailableChecks(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}()

	t.Run("missing dconf tool", func(t *testing.T) {
		lookPath = func(file string) (string, error) {
			if file == "dconf" {
				return "", errors.New("dconf not found")
			}
			return "/usr/bin/" + file, nil
		}
		avail, err := available(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if avail {
			t.Error("available() = true, want false when dconf is missing")
		}
	})

	t.Run("no keys in dconf", func(t *testing.T) {
		lookPath = func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		}
		runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
			return "", nil
		}
		avail, err := available(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if avail {
			t.Error("available() = true, want false when dconf has no custom-command-list keys")
		}
	})

	t.Run("extension present via distro default key", func(t *testing.T) {
		lookPath = func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		}
		runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
			if name == "dconf" && len(args) == 2 && args[0] == "dump" && args[1] == DconfPath {
				return "[/]\ncommand1=('Terminal', 'ptyxis', 'utilities-terminal-symbolic', true)\n", nil
			}
			return "", nil
		}
		avail, err := available(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !avail {
			t.Error("available() = false, want true when distro default key exists in dconf")
		}
	})

	t.Run("extension present via user layer key", func(t *testing.T) {
		lookPath = func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		}
		runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
			if name == "dconf" && args[0] == "dump" {
				return "[/]\ncommand3=('Custom', 'custom-cmd', 'icon', true)\n", nil
			}
			return "", nil
		}
		avail, err := available(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !avail {
			t.Error("available() = false, want true when user key exists in dconf")
		}
	})

	t.Run("single dump replaces the per-key scan", func(t *testing.T) {
		lookPath = func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		}
		calls := 0
		runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
			calls++
			return "[/]\ncommand8=('Terminal', 'ptyxis', 'term', true)\n", nil
		}
		if _, err := available(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 1 {
			t.Errorf("available() spawned %d dconf processes, want 1", calls)
		}
	})

	t.Run("keys outside the command range are ignored", func(t *testing.T) {
		lookPath = func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		}
		runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
			return "[/]\nmenuicon-setting='ublue-logo-symbolic'\ncommand100=('Terminal', 'ptyxis', 'term', true)\n\n[nested]\ncommand1=('Terminal', 'ptyxis', 'term', true)\n", nil
		}
		avail, err := available(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if avail {
			t.Error("available() = true, want false when no command1..command99 key is present at the dumped path")
		}
	})
}

// mockDconf simulates the dconf database with a user layer and a distro/default layer.
type mockDconf struct {
	user     map[string]string
	defaults map[string]string
	writes   map[string]string
	resets   map[string]bool
}

func newMockDconf() *mockDconf {
	return &mockDconf{
		user:     make(map[string]string),
		defaults: make(map[string]string),
		writes:   make(map[string]string),
		resets:   make(map[string]bool),
	}
}

func (m *mockDconf) runCommand(ctx context.Context, name string, args ...string) (string, error) {
	if name != "dconf" {
		return "", fmt.Errorf("unexpected command: %s", name)
	}

	sub := args[0]
	switch sub {
	case "dump":
		return dumpMock(args[1], m.defaults, m.user), nil
	case "read":
		if len(args) == 3 && args[1] == "-d" {
			path := args[2]
			return m.defaults[path], nil
		}
		path := args[1]
		if val, ok := m.user[path]; ok {
			return val, nil
		}
		return m.defaults[path], nil
	case "write":
		path := args[1]
		val := args[2]
		m.writes[path] = val
		m.user[path] = val
		return "", nil
	case "reset":
		path := args[1]
		m.resets[path] = true
		delete(m.user, path)
		return "", nil
	default:
		return "", fmt.Errorf("unsupported dconf subcommand: %s", sub)
	}
}

// dumpMock renders `dconf dump`-style keyfile output for the keys under prefix,
// with the user layer resolved over the distro defaults.
func dumpMock(prefix string, defaults, user map[string]string) string {
	merged := make(map[string]string)
	for path, val := range defaults {
		merged[path] = val
	}
	for path, val := range user {
		merged[path] = val
	}

	keys := make([]string, 0, len(merged))
	for path := range merged {
		if strings.HasPrefix(path, prefix) {
			keys = append(keys, strings.TrimPrefix(path, prefix))
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("[/]\n")
	for _, key := range keys {
		fmt.Fprintf(&sb, "%s=%s\n", key, merged[prefix+key])
	}
	return sb.String()
}

func TestApplyMockBluefinProfile(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}()

	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	// Standard Bluefin profile:
	// command1..7: other apps
	// command8: Terminal (ptyxis) - default visible: true
	// command9: absent
	termPath := DconfPath + "command8"
	initialTerm := "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', true)"

	mock := newMockDconf()
	mock.defaults[termPath] = initialTerm
	runCommand = mock.runCommand

	// Case 1: Developer mode turned OFF (Terminal must be hidden).
	// Because distro default has visible=true, resetting would leave it visible!
	// It must write visible=false into the user layer.
	if err := Apply(context.Background(), false); err != nil {
		t.Fatalf("Apply(false) error: %v", err)
	}

	if mock.writes[termPath] == "" {
		t.Errorf("Apply(false) did not write user override for %s", termPath)
	}
	wantHidden := "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', false)"
	if mock.writes[termPath] != wantHidden {
		t.Errorf("Apply(false) wrote %q, want %q", mock.writes[termPath], wantHidden)
	}

	// Case 2: Developer mode turned ON (Terminal must be visible).
	// Because distro default has visible=true and matches the desired tuple,
	// it must RESET the key so distro defaults shine through!
	if err := Apply(context.Background(), true); err != nil {
		t.Fatalf("Apply(true) error: %v", err)
	}

	if !mock.resets[termPath] {
		t.Errorf("Apply(true) did not reset %s to allow distro default to shine through", termPath)
	}
	if _, overridden := mock.user[termPath]; overridden {
		t.Errorf("Apply(true) left an override in user layer: %q", mock.user[termPath])
	}
}

func TestApplyMockDakotaProfile(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}()

	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	// Dakota profile:
	// command8: Terminal
	// command9: Containers (/usr/bin/flatpak run com.ranfdev.DistroShelf)
	termPath := DconfPath + "command8"
	contPath := DconfPath + "command9"
	termDef := "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', true)"
	contDef := "('Containers', '/usr/bin/flatpak run com.ranfdev.DistroShelf', 'org.gnome.Boxes', true)"

	mock := newMockDconf()
	mock.defaults[termPath] = termDef
	mock.defaults[contPath] = contDef
	runCommand = mock.runCommand

	// Case 1: Developer mode turned OFF -> both hidden
	if err := Apply(context.Background(), false); err != nil {
		t.Fatalf("Apply(false) error: %v", err)
	}

	wantTermHidden := "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', false)"
	wantContHidden := "('Containers', '/usr/bin/flatpak run com.ranfdev.DistroShelf', 'org.gnome.Boxes', false)"

	if mock.writes[termPath] != wantTermHidden {
		t.Errorf("command8 write = %q, want %q", mock.writes[termPath], wantTermHidden)
	}
	if mock.writes[contPath] != wantContHidden {
		t.Errorf("command9 write = %q, want %q", mock.writes[contPath], wantContHidden)
	}

	// Case 2: Developer mode turned ON -> both restored via reset to distro defaults
	if err := Apply(context.Background(), true); err != nil {
		t.Fatalf("Apply(true) error: %v", err)
	}

	if !mock.resets[termPath] {
		t.Errorf("command8 not reset on enable")
	}
	if !mock.resets[contPath] {
		t.Errorf("command9 not reset on enable")
	}
}

func TestApplyReorderedAndMultipleEntries(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}()

	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	// Reordered and multiple matching:
	// command1: Containers
	// command3: Terminal
	// command15: Terminal (secondary terminal profile)
	mock := newMockDconf()
	mock.defaults[DconfPath+"command1"] = "('Containers', 'distroshelf', 'box', true)"
	mock.defaults[DconfPath+"command3"] = "('Terminal', 'ptyxis', 'term', true)"
	mock.defaults[DconfPath+"command15"] = "('Terminal', 'alacritty', 'term2', true)"
	runCommand = mock.runCommand

	if err := Apply(context.Background(), false); err != nil {
		t.Fatalf("Apply(false) error: %v", err)
	}

	for _, key := range []string{"command1", "command3", "command15"} {
		written, ok := mock.writes[DconfPath+key]
		if !ok {
			t.Fatalf("entry %s was not updated", key)
		}
		entry, err := ParseEntry(written)
		if err != nil {
			t.Fatalf("parsing written entry %s: %v", key, err)
		}
		if entry.Visible {
			t.Errorf("entry %s visible = true, want false", key)
		}
	}
}

func TestApplyPreservesUnrelatedCommandsAndTupleContents(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}()

	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	mock := newMockDconf()
	// Unrelated commands
	mock.defaults[DconfPath+"command1"] = "('Web', 'firefox', 'web-browser', true)"
	mock.defaults[DconfPath+"command2"] = "('Calculator', 'gnome-calculator', 'calc', false)"
	// Matching command with complex custom string
	mock.defaults[DconfPath+"command8"] = "('Terminal', 'sh -c \"echo \\'hi\\' && exec ptyxis\"', 'custom-term-icon', true)"
	runCommand = mock.runCommand

	if err := Apply(context.Background(), false); err != nil {
		t.Fatalf("Apply(false) error: %v", err)
	}

	// Verify command1 and command2 were never touched
	if _, ok := mock.writes[DconfPath+"command1"]; ok {
		t.Error("unrelated command1 was written")
	}
	if _, ok := mock.writes[DconfPath+"command2"]; ok {
		t.Error("unrelated command2 was written")
	}

	// Verify command8 preserved command and icon exactly
	written := mock.writes[DconfPath+"command8"]
	entry, err := ParseEntry(written)
	if err != nil {
		t.Fatalf("parsing written command8: %v", err)
	}
	if entry.Label != "Terminal" {
		t.Errorf("Label = %q, want 'Terminal'", entry.Label)
	}
	if entry.Command != "sh -c \"echo 'hi' && exec ptyxis\"" {
		t.Errorf("Command = %q, want preserved", entry.Command)
	}
	if entry.Icon != "custom-term-icon" {
		t.Errorf("Icon = %q, want 'custom-term-icon'", entry.Icon)
	}
	if entry.Visible != false {
		t.Error("Visible = true, want false")
	}
}

func TestApplyPreviewDryRun(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	origWriter := log.Writer()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
		log.SetOutput(origWriter)
		dryrun.Set(false)
	}()

	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	mock := newMockDconf()
	mock.defaults[DconfPath+"command8"] = "('Terminal', 'ptyxis', 'term', true)"
	runCommand = mock.runCommand

	dryrun.Set(true)

	// Case 1: write branch preview
	if err := Apply(context.Background(), false); err != nil {
		t.Fatalf("Apply(false) under dry-run error: %v", err)
	}

	// No write or reset should have occurred in mock
	if len(mock.writes) > 0 {
		t.Errorf("dry-run performed writes: %v", mock.writes)
	}
	if len(mock.resets) > 0 {
		t.Errorf("dry-run performed resets: %v", mock.resets)
	}

	// Must log the preview string for write
	wantLog := "[DRY-RUN] would set Custom Command Menu command8 visible=false"
	if !strings.Contains(buf.String(), wantLog) {
		t.Errorf("log output %q does not contain %q", buf.String(), wantLog)
	}

	// Case 2: reset branch preview when default matches target
	buf.Reset()
	mock.user[DconfPath+"command8"] = "('Terminal', 'ptyxis', 'term', false)"
	if err := Apply(context.Background(), true); err != nil {
		t.Fatalf("Apply(true) under dry-run error: %v", err)
	}
	if len(mock.writes) > 0 {
		t.Errorf("dry-run reset performed writes: %v", mock.writes)
	}
	if len(mock.resets) > 0 {
		t.Errorf("dry-run reset performed resets: %v", mock.resets)
	}
	wantResetLog := "[DRY-RUN] would reset Custom Command Menu command8 to default"
	if !strings.Contains(buf.String(), wantResetLog) {
		t.Errorf("log output %q does not contain %q", buf.String(), wantResetLog)
	}
}

func TestApplyDoesNotPinWhenSemanticallyEqual(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}()

	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	mock := newMockDconf()
	// User override matches desired entry semantically, but uses different quoting and spacing
	mock.user[DconfPath+"command8"] = `("Terminal",  "ptyxis --new-window",  "utilities-terminal-symbolic",  false)`
	runCommand = mock.runCommand

	// Apply(false) when entry is already semantically hidden:
	// must NOT rewrite or pin user overrides
	if err := Apply(context.Background(), false); err != nil {
		t.Fatalf("Apply(false) error: %v", err)
	}

	if len(mock.writes) > 0 {
		t.Errorf("Apply(false) rewrote semantically identical entry: %v", mock.writes)
	}
}

func TestApplyDoesNotResetWhenSemanticallyEqualToDefault(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}()

	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	mock := newMockDconf()
	mock.defaults[DconfPath+"command8"] = `('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', true)`
	// Resolved value equals the default entry, differing only in quoting and spacing.
	mock.user[DconfPath+"command8"] = `("Terminal",  "ptyxis --new-window",  "utilities-terminal-symbolic",  true)`
	runCommand = mock.runCommand

	if err := Apply(context.Background(), true); err != nil {
		t.Fatalf("Apply(true) error: %v", err)
	}

	if len(mock.resets) > 0 {
		t.Errorf("Apply(true) reset an entry already equal to the default: %v", mock.resets)
	}
	if len(mock.writes) > 0 {
		t.Errorf("Apply(true) wrote an entry already equal to the default: %v", mock.writes)
	}
}

func TestApplyNeverConsultsGsettings(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}()

	lookPath = func(file string) (string, error) {
		return "/usr/bin/" + file, nil
	}

	termPath := DconfPath + "command8"
	initialTerm := "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', true)"

	mock := newMockDconf()
	mock.defaults[termPath] = initialTerm

	// Model a host where gsettings reports "No such schema" (like Bluefin where the schema
	// is compiled only in the extension's private directory): devmenu must never call it.
	runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "gsettings" {
			t.Errorf("devmenu invoked gsettings %v; availability must come from dconf alone", args)
			return "No such schema \"org.gnome.shell.extensions.custom-command-list\"", errors.New("exit 1")
		}
		return mock.runCommand(ctx, name, args...)
	}

	// Apply(false) must still act and write visible=false to user layer.
	if err := Apply(context.Background(), false); err != nil {
		t.Fatalf("Apply(false) failed on host with missing gsettings schema: %v", err)
	}

	wantHidden := "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', false)"
	if mock.writes[termPath] != wantHidden {
		t.Errorf("Apply(false) wrote %q, want %q", mock.writes[termPath], wantHidden)
	}
}

func TestApplyOperationalFailures(t *testing.T) {
	origLookPath := lookPath
	origRunCommand := runCommand
	defer func() {
		lookPath = origLookPath
		runCommand = origRunCommand
	}()

	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	t.Run("read failure", func(t *testing.T) {
		runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
			return "", errors.New("dconf read error: disk failure")
		}
		err := Apply(context.Background(), false)
		if err == nil {
			t.Fatal("expected error on dconf read failure, got nil")
		}
		if !strings.Contains(err.Error(), "dumping "+DconfPath) {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("write failure", func(t *testing.T) {
		runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
			switch args[0] {
			case "dump":
				return "[/]\ncommand1=('Terminal', 'ptyxis', 'term', true)\n", nil
			case "read":
				return "('Terminal', 'ptyxis', 'term', true)", nil
			case "write":
				return "permission denied", errors.New("write failed")
			}
			return "", nil
		}
		err := Apply(context.Background(), false)
		if err == nil {
			t.Fatal("expected error on dconf write failure, got nil")
		}
		if !strings.Contains(err.Error(), "writing command1") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("default read failure", func(t *testing.T) {
		runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
			if args[0] == "dump" {
				return "[/]\ncommand1=('Terminal', 'ptyxis', 'term', true)\n", nil
			}
			return "", errors.New("dconf read error: disk failure")
		}
		err := Apply(context.Background(), false)
		if err == nil {
			t.Fatal("expected error on dconf default read failure, got nil")
		}
		if !strings.Contains(err.Error(), "reading default command1") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
