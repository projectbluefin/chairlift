package compass

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func stubShortcutRunner(t *testing.T, runner func(context.Context, string, ...string) (string, error)) {
	t.Helper()
	oldRun := runCmd
	oldLook := lookCmd
	runCmd = runner
	lookCmd = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() {
		runCmd = oldRun
		lookCmd = oldLook
	})
}

func TestShortcutAvailableRefusesNonGNOME(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	t.Setenv("DESKTOP_SESSION", "plasma")
	t.Setenv("KDE_FULL_SESSION", "true")

	if ShortcutAvailable() {
		t.Errorf("expected ShortcutAvailable()=false on Plasma/KDE")
	}

	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	t.Setenv("KDE_FULL_SESSION", "")
	t.Setenv("DESKTOP_SESSION", "gnome")
	oldLook := lookCmd
	lookCmd = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() { lookCmd = oldLook })
	if !ShortcutAvailable() {
		t.Errorf("expected ShortcutAvailable()=true on GNOME when tools exist")
	}
}

func TestSetShortcutEnablesAndCleansUp(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	store := map[string]string{
		mediaSchema + " custom-keybindings": "[]",
		inputSchema + " " + inputKey:        "['<Super>space', '<Alt>Shift_L']",
	}
	dconfStore := make(map[string]string)

	stubShortcutRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		if name == "gsettings" {
			switch args[0] {
			case "get":
				key := args[1] + " " + args[2]
				return store[key], nil
			case "set":
				key := args[1] + " " + args[2]
				store[key] = args[3]
				return "", nil
			}
		}
		if name == "dconf" {
			switch args[0] {
			case "read":
				return dconfStore[args[1]], nil
			case "write":
				dconfStore[args[1]] = args[2]
				return "", nil
			case "reset":
				if len(args) >= 3 && args[1] == "-f" {
					for k := range dconfStore {
						if strings.HasPrefix(k, args[2]) {
							delete(dconfStore, k)
						}
					}
				} else {
					delete(dconfStore, args[1])
				}
				delete(store, shortcutSchema+" command")
				delete(store, shortcutSchema+" binding")
				delete(store, shortcutSchema+" name")
				return "", nil
			}
		}
		return "", nil
	})

	// 1. Enable shortcut
	if err := SetShortcut(context.Background(), true); err != nil {
		t.Fatalf("SetShortcut(true) failed: %v", err)
	}
	bindings := parseList(store[mediaSchema+" custom-keybindings"])
	if !slices.Contains(bindings, shortcutPath) {
		t.Errorf("expected shortcutPath in bindings: %v", bindings)
	}
	if store[shortcutSchema+" command"] != quote(launchCommand) || store[shortcutSchema+" binding"] != quote(Binding) {
		t.Errorf("expected shortcut keys set, got command=%s binding=%s",
			store[shortcutSchema+" command"], store[shortcutSchema+" binding"])
	}
	inputs := parseList(store[inputSchema+" "+inputKey])
	if slices.Contains(inputs, Binding) {
		t.Errorf("expected <Super>space removed from input sources: %v", inputs)
	}

	// 1b. Repeated enable retains displaced status
	if err := SetShortcut(context.Background(), true); err != nil {
		t.Fatalf("SetShortcut(true) repeat failed: %v", err)
	}
	if dconfStore[displacedKey] == "" {
		t.Errorf("repeated enable lost displacedKey in dconf")
	}

	// 2. Disable shortcut
	if err := SetShortcut(context.Background(), false); err != nil {
		t.Fatalf("SetShortcut(false) failed: %v", err)
	}
	bindingsAfter := parseList(store[mediaSchema+" custom-keybindings"])
	if slices.Contains(bindingsAfter, shortcutPath) {
		t.Errorf("expected shortcutPath removed from bindings: %v", bindingsAfter)
	}
	inputsAfter := parseList(store[inputSchema+" "+inputKey])
	if !slices.Contains(inputsAfter, Binding) {
		t.Errorf("expected <Super>space restored to input sources: %v", inputsAfter)
	}
}

func TestSetShortcutPreservesEmptyInitialInputSources(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	store := map[string]string{
		mediaSchema + " custom-keybindings": "[]",
		inputSchema + " " + inputKey:        "[]",
	}
	dconfStore := make(map[string]string)

	stubShortcutRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		if name == "gsettings" {
			switch args[0] {
			case "get":
				return store[args[1]+" "+args[2]], nil
			case "set":
				store[args[1]+" "+args[2]] = args[3]
				return "", nil
			}
		}
		if name == "dconf" {
			switch args[0] {
			case "read":
				return dconfStore[args[1]], nil
			case "write":
				dconfStore[args[1]] = args[2]
				return "", nil
			case "reset":
				return "", nil
			}
		}
		return "", nil
	})

	if err := SetShortcut(context.Background(), true); err != nil {
		t.Fatalf("SetShortcut(true) failed: %v", err)
	}
	if dconfStore[displacedKey] != "" {
		t.Errorf("expected no displaced key recorded when initial inputs were empty, got %q", dconfStore[displacedKey])
	}
	if err := SetShortcut(context.Background(), false); err != nil {
		t.Fatalf("SetShortcut(false) failed: %v", err)
	}
	inputsAfter := parseList(store[inputSchema+" "+inputKey])
	if len(inputsAfter) != 0 {
		t.Errorf("expected inputs to stay empty after disabling shortcut, got %v", inputsAfter)
	}
}

func TestParseAndFormatList(t *testing.T) {
	items := []string{"/path/1/", "/path/2/"}
	formatted := formatList(items)
	parsed := parseList(formatted)
	if !reflect.DeepEqual(parsed, items) {
		t.Errorf("roundtrip failed: got %v, want %v", parsed, items)
	}
}

func TestSuppressInternalHotkey(t *testing.T) {
	tmp := t.TempDir()
	if err := SuppressInternalHotkey(tmp); err != nil {
		t.Fatalf("SuppressInternalHotkey failed: %v", err)
	}

	cfgPath := tmp + "/.var/app/org.tunaos.compass/config/compass/compass.json"
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading config failed: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parsing config failed: %v", err)
	}
	launcher, ok := root["launcher"].(map[string]any)
	if !ok {
		t.Fatalf("launcher missing in config: %v", root)
	}
	if launcher["hotkey"] != "" {
		t.Fatalf("expected launcher.hotkey == \"\", got %v", launcher["hotkey"])
	}
}

func writeCompassConfig(t *testing.T, home, body string) string {
	t.Helper()
	dir := filepath.Join(home, ".var", "app", AppID, "config", "compass")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "compass.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The user's own Compass settings survive: only launcher.hotkey changes.
func TestSuppressInternalHotkeyPreservesExistingSettings(t *testing.T) {
	home := t.TempDir()
	path := writeCompassConfig(t, home, `{"theme":"dark","launcher":{"hotkey":"<Super>space","width":640}}`)

	if err := SuppressInternalHotkey(home); err != nil {
		t.Fatalf("SuppressInternalHotkey: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parsing rewritten config: %v", err)
	}
	if root["theme"] != "dark" {
		t.Errorf("theme = %v, want dark (top-level setting lost)", root["theme"])
	}
	launcher, ok := root["launcher"].(map[string]any)
	if !ok {
		t.Fatalf("launcher missing in config: %v", root)
	}
	if launcher["width"] != float64(640) {
		t.Errorf("launcher.width = %v, want 640 (launcher setting lost)", launcher["width"])
	}
	if launcher["hotkey"] != "" {
		t.Errorf("launcher.hotkey = %v, want empty", launcher["hotkey"])
	}
}

// A config Compass cannot parse is reported and left as it was, never
// replaced by a near-empty one.
func TestSuppressInternalHotkeyLeavesMalformedConfigUntouched(t *testing.T) {
	home := t.TempDir()
	const body = `{"theme": "dark",`
	path := writeCompassConfig(t, home, body)

	if err := SuppressInternalHotkey(home); err == nil {
		t.Fatal("SuppressInternalHotkey accepted malformed JSON")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body {
		t.Errorf("malformed config rewritten to %q, want it unchanged", data)
	}
}

func TestSuppressInternalHotkeySkipsEmptyHome(t *testing.T) {
	if err := SuppressInternalHotkey(""); err != nil {
		t.Fatalf("SuppressInternalHotkey(\"\") = %v, want nil", err)
	}
}
