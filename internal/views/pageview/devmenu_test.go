package pageview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/devmenu"
)

// mockDconfStore provides an in-memory simulation of dconf defaults and user overrides.
type mockDconfStore struct {
	defaults map[string]string
	user     map[string]string
	writes   map[string]string
	resets   map[string]bool
}

func newMockDconfStore() *mockDconfStore {
	return &mockDconfStore{
		defaults: make(map[string]string),
		user:     make(map[string]string),
		writes:   make(map[string]string),
		resets:   make(map[string]bool),
	}
}

func (m *mockDconfStore) run(ctx context.Context, name string, args ...string) (string, error) {
	if name != "dconf" {
		return "", fmt.Errorf("unexpected command: %s", name)
	}

	sub := args[0]
	switch sub {
	case "dump":
		return m.dump(args[1]), nil
	case "read":
		if len(args) == 3 && args[1] == "-d" {
			return m.defaults[args[2]], nil
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

// dump renders `dconf dump`-style keyfile output for the keys under prefix,
// with the user layer resolved over the distro defaults.
func (m *mockDconfStore) dump(prefix string) string {
	merged := make(map[string]string)
	for path, val := range m.defaults {
		merged[path] = val
	}
	for path, val := range m.user {
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

func TestDeveloperMenuLabelMatchingOverFixedIndices(t *testing.T) {
	t.Run("mock bluefin profile", func(t *testing.T) {
		// Standard Bluefin: Terminal is command8, Containers is absent.
		store := newMockDconfStore()
		termKey := devmenu.DconfPath + "command8"
		store.defaults[termKey] = "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', true)"

		devmenu.SetTestRunners(
			func(file string) (string, error) { return "/usr/bin/" + file, nil },
			store.run,
		)
		defer devmenu.ResetTestRunners()

		// Developer Mode OFF -> Terminal must become visible=false
		if err := devmenu.Apply(context.Background(), false); err != nil {
			t.Fatalf("devmenu.Apply(false) error: %v", err)
		}

		wantHidden := "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', false)"
		if store.writes[termKey] != wantHidden {
			t.Errorf("command8 write = %q, want %q", store.writes[termKey], wantHidden)
		}
		// Confirm Containers (command9) was never touched or created
		if _, ok := store.writes[devmenu.DconfPath+"command9"]; ok {
			t.Errorf("command9 was written on Bluefin profile where Containers is absent")
		}

		// Developer Mode ON -> Terminal reset to distro default
		if err := devmenu.Apply(context.Background(), true); err != nil {
			t.Fatalf("devmenu.Apply(true) error: %v", err)
		}
		if !store.resets[termKey] {
			t.Errorf("command8 not reset on enable")
		}
	})

	t.Run("mock dakota profile", func(t *testing.T) {
		// Dakota: Terminal is command8, Containers is command9
		store := newMockDconfStore()
		termKey := devmenu.DconfPath + "command8"
		contKey := devmenu.DconfPath + "command9"
		store.defaults[termKey] = "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', true)"
		store.defaults[contKey] = "('Containers', '/usr/bin/flatpak run com.ranfdev.DistroShelf', 'org.gnome.Boxes', true)"

		devmenu.SetTestRunners(
			func(file string) (string, error) { return "/usr/bin/" + file, nil },
			store.run,
		)
		defer devmenu.ResetTestRunners()

		// Developer Mode OFF -> both Terminal and Containers must become visible=false
		if err := devmenu.Apply(context.Background(), false); err != nil {
			t.Fatalf("devmenu.Apply(false) error: %v", err)
		}

		wantTermHidden := "('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', false)"
		wantContHidden := "('Containers', '/usr/bin/flatpak run com.ranfdev.DistroShelf', 'org.gnome.Boxes', false)"
		if store.writes[termKey] != wantTermHidden {
			t.Errorf("command8 write = %q, want %q", store.writes[termKey], wantTermHidden)
		}
		if store.writes[contKey] != wantContHidden {
			t.Errorf("command9 write = %q, want %q", store.writes[contKey], wantContHidden)
		}

		// Developer Mode ON -> both reset to distro defaults
		if err := devmenu.Apply(context.Background(), true); err != nil {
			t.Fatalf("devmenu.Apply(true) error: %v", err)
		}
		if !store.resets[termKey] {
			t.Errorf("command8 not reset on enable")
		}
		if !store.resets[contKey] {
			t.Errorf("command9 not reset on enable")
		}
	})
}

func TestDeveloperMenuDistroDefaultVsUserOverrideTension(t *testing.T) {
	// Verifies tension resolved: resetting a key can reveal a distro default with visible=true,
	// which is NOT equivalent to enforcing off.
	store := newMockDconfStore()
	key := devmenu.DconfPath + "command8"
	// Distro default has visible=true
	store.defaults[key] = "('Terminal', 'ptyxis', 'term', true)"

	devmenu.SetTestRunners(
		func(file string) (string, error) { return "/usr/bin/" + file, nil },
		store.run,
	)
	defer devmenu.ResetTestRunners()

	// Turning off MUST write user-layer visible=false, because reset would reveal true!
	if err := devmenu.Apply(context.Background(), false); err != nil {
		t.Fatalf("Apply(false) error: %v", err)
	}
	if store.resets[key] {
		t.Error("Apply(false) called reset when distro default was visible=true")
	}
	if store.writes[key] != "('Terminal', 'ptyxis', 'term', false)" {
		t.Errorf("Apply(false) write = %q, want override with visible=false", store.writes[key])
	}

	// Now suppose distro default shipped with visible=false:
	store2 := newMockDconfStore()
	key2 := devmenu.DconfPath + "command8"
	store2.defaults[key2] = "('Terminal', 'ptyxis', 'term', false)"
	// User previously had an override visible=true
	store2.user[key2] = "('Terminal', 'ptyxis', 'term', true)"

	devmenu.SetTestRunners(
		func(file string) (string, error) { return "/usr/bin/" + file, nil },
		store2.run,
	)

	// Turning off should RESET the key because distro default already has visible=false
	if err := devmenu.Apply(context.Background(), false); err != nil {
		t.Fatalf("Apply(false) error: %v", err)
	}
	if !store2.resets[key2] {
		t.Error("Apply(false) did not reset key when distro default already matched visible=false")
	}
}

func TestDeveloperMenuFeaturesPageWiring(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	featuresPath := filepath.Join(repoRoot, "internal", "views", "features_page.go")
	featuresSource, err := os.ReadFile(featuresPath)
	if err != nil {
		t.Fatalf("read %s: %v", featuresPath, err)
	}
	text := string(featuresSource)

	// Check devmenu.Apply is imported and used
	if !strings.Contains(text, `"github.com/projectbluefin/chairlift/internal/devmenu"`) {
		t.Error("features_page.go does not import internal/devmenu")
	}

	// Must be invoked conditionally after ublue.SetDeveloperMode
	if !strings.Contains(text, `err := ublue.SetDeveloperMode(ctx, enabled)`) {
		t.Error("features_page.go does not call ublue.SetDeveloperMode")
	}
	if !strings.Contains(text, `succeeded := err == nil`) {
		t.Error("features_page.go does not define succeeded := err == nil")
	}
	if !strings.Contains(text, `if succeeded {`) {
		t.Error("features_page.go does not gate devmenu.Apply on succeeded")
	}
	if !strings.Contains(text, `devmenu.Apply(ctx, enabled)`) {
		t.Error("features_page.go does not call devmenu.Apply")
	}

	// Must not be called on view construction
	buildFuncStart := strings.Index(text, "func (uh *UserHome) buildFeaturesPage()")
	if buildFuncStart == -1 {
		t.Fatal("buildFeaturesPage not found")
	}
	buildFuncEnd := strings.Index(text, "func (uh *UserHome) checkAndLoadFeatures(")
	if buildFuncEnd == -1 {
		t.Fatal("checkAndLoadFeatures not found")
	}
	buildSection := text[buildFuncStart:buildFuncEnd]
	if strings.Contains(buildSection, "devmenu.Apply") {
		t.Error("buildFeaturesPage contains devmenu.Apply call; view construction must never write")
	}
}
