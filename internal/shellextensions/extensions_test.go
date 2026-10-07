package shellextensions

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func stubRunner(t *testing.T, run func(context.Context, ...string) (string, error)) {
	t.Helper()
	old := runCommand
	runCommand = run
	dryrun.Set(false)
	t.Cleanup(func() { runCommand = old; dryrun.Set(false) })
}

func TestCatalogDefaults(t *testing.T) {
	got := Catalog()
	if len(got) != 2 || got[0].UUID != "tailscale-gnome-qs@tailscale-qs.github.io" || !got[0].DefaultEnabled || got[1].UUID != "syncthing-toggle@projectbluefin.io" || got[1].DefaultEnabled {
		t.Fatalf("unexpected integrations: %+v", got)
	}
}

// Every catalog entry can be enabled, so a description calling the feature
// unusable contradicts the switch beside it (W3-14).
func TestCatalogNeverCallsASwitchableExtensionUnready(t *testing.T) {
	for _, extension := range Catalog() {
		if strings.Contains(strings.ToLower(extension.Description), "not ready") {
			t.Errorf("%s is switchable but described as not ready: %q", extension.Title, extension.Description)
		}
	}
}

func TestLoadPreservesGNOMEChoices(t *testing.T) {
	// Deliberately reverse the suggested defaults. The page must show the
	// user's choice, and an unrelated extension must not enter the result.
	var calls [][]string
	stubRunner(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, args)
		if len(args) == 1 {
			return "tailscale-gnome-qs@tailscale-qs.github.io\nsyncthing-toggle@projectbluefin.io\nother@example.org\n", nil
		}
		return "syncthing-toggle@projectbluefin.io\nother@example.org\n", nil
	})
	states, err := Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || states[Catalog()[0].UUID] != (State{Installed: true}) || states[Catalog()[1].UUID] != (State{Installed: true, Enabled: true}) {
		t.Fatalf("states = %+v", states)
	}
	if !reflect.DeepEqual(calls, [][]string{{"list"}, {"list", "--enabled"}}) {
		t.Fatalf("unexpected commands: %v", calls)
	}
}

func TestLoadMissingAndFailures(t *testing.T) {
	for _, failAt := range []int{0, 1, 2} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			calls := 0
			stubRunner(t, func(_ context.Context, _ ...string) (string, error) {
				calls++
				if calls == failAt {
					return "", errors.New("session unavailable")
				}
				return "", nil
			})
			states, err := Load(context.Background())
			if failAt > 0 {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			for _, state := range states {
				if state.Installed || state.Enabled {
					t.Fatalf("missing extension: %+v", state)
				}
			}
		})
	}
}

func TestSetEnabledCommands(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enable", false: "disable"}[enabled], func(t *testing.T) {
			var got []string
			stubRunner(t, func(_ context.Context, args ...string) (string, error) { got = args; return "", nil })
			uuid := "syncthing-toggle@projectbluefin.io"
			if err := SetEnabled(context.Background(), uuid, enabled); err != nil {
				t.Fatal(err)
			}
			action := "disable"
			if enabled {
				action = "enable"
			}
			if !reflect.DeepEqual(got, []string{action, uuid}) {
				t.Fatalf("args = %v", got)
			}
		})
	}
}

func TestSetEnabledRejectsUnknownAndDryRunDoesNotExecute(t *testing.T) {
	stubRunner(t, func(_ context.Context, _ ...string) (string, error) { t.Fatal("must not execute"); return "", nil })
	for _, preview := range []bool{false, true} {
		dryrun.Set(preview)
		if err := SetEnabled(context.Background(), "other@example.org", true); err == nil {
			t.Fatal("accepted unknown extension")
		}
	}
	for _, extension := range Catalog() {
		for _, enabled := range []bool{false, true} {
			if err := SetEnabled(context.Background(), extension.UUID, enabled); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSetEnabledReportsFailure(t *testing.T) {
	failure := errors.New("failed")
	stubRunner(t, func(_ context.Context, _ ...string) (string, error) { return "settings locked", failure })
	if err := SetEnabled(context.Background(), Catalog()[0].UUID, true); !errors.Is(err, failure) {
		t.Fatalf("error = %v", err)
	}
}
