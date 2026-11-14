package compass

import (
	"context"
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
