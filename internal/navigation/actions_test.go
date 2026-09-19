package navigation

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// actionScopes maps a GAction prefix to the file that owns registering the
// actions in that scope. GTK resolves "app." against the GtkApplication and
// "win." against the window, so an accelerator is only live if the matching
// owner registered the action.
var actionScopes = map[string]string{
	"app.": filepath.Join("internal", "app", "app.go"),
	"win.": filepath.Join("internal", "window", "window.go"),
}

// newSimpleAction matches a gio.NewSimpleAction call and captures its name
// argument, plus the "+" that marks the name as a prefix concatenated with a
// runtime value (the per-page `"navigate-"+itemName` registration).
var newSimpleAction = regexp.MustCompile(`gio\.NewSimpleAction\(\s*"([^"]+)"(\s*\+)?`)

// registeredActions reports the action names a source file registers, split
// into exact names and the prefixes it registers a family under.
func registeredActions(t *testing.T, path string) (names map[string]bool, prefixes []string) {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	names = make(map[string]bool)
	for _, match := range newSimpleAction.FindAllStringSubmatch(string(source), -1) {
		if match[2] != "" {
			prefixes = append(prefixes, match[1])
			continue
		}
		names[match[1]] = true
	}
	return names, prefixes
}

// TestEveryAdvertisedActionIsRegistered holds the keyboard-shortcuts contract
// in the direction that actually breaks: navigation advertises an accelerator
// and the shortcuts dialog shows it, but GTK silently does nothing when no
// owner ever registered the target action. That is not hypothetical — the
// dialog advertised Ctrl+Q against app.quit while the application registered
// no quit action, so the key did nothing.
//
// The check derives what is advertised from navigation's own tables rather
// than from a hand-maintained list, so a shortcut added here without a
// matching registration fails instead of shipping as a dead key.
func TestEveryAdvertisedActionIsRegistered(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate actions_test.go")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))

	type scope struct {
		names    map[string]bool
		prefixes []string
	}
	scopes := make(map[string]scope, len(actionScopes))
	for prefix, relative := range actionScopes {
		names, prefixes := registeredActions(t, filepath.Join(repoRoot, relative))
		if len(names) == 0 && len(prefixes) == 0 {
			t.Fatalf("%s registers no actions; the %q scope check cannot fail and is not holding anything", relative, prefix)
		}
		scopes[prefix] = scope{names: names, prefixes: prefixes}
	}

	bindings := Bindings(Items())
	if len(bindings) == 0 {
		t.Fatal("Bindings(Items()) is empty; nothing is being checked")
	}

	for _, binding := range bindings {
		prefix, action, found := cutActionScope(binding.Action)
		if !found {
			t.Errorf("action %q has no known scope; extend actionScopes with the file that registers it", binding.Action)
			continue
		}
		owner := scopes[prefix]
		if owner.names[action] {
			continue
		}
		if matchesPrefix(action, owner.prefixes) {
			continue
		}
		t.Errorf(
			"%s is advertised for %v but %s registers no such action, so the accelerator does nothing",
			binding.Action, binding.Accelerators, actionScopes[prefix],
		)
	}
}

// cutActionScope splits a fully-qualified action into its registered scope
// prefix and the bare name the owner registers it under.
func cutActionScope(action string) (prefix, name string, found bool) {
	for candidate := range actionScopes {
		if strings.HasPrefix(action, candidate) {
			return candidate, strings.TrimPrefix(action, candidate), true
		}
	}
	return "", "", false
}

func matchesPrefix(action string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(action, prefix) && action != prefix {
			return true
		}
	}
	return false
}
