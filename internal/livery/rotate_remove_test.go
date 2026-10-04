package livery

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// installedUnit points the unit path at a fresh config home and, when body is
// non-empty, places a unit there as an earlier InstallRotation would have.
func installedUnit(t *testing.T, body string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := UnitPath()
	if err != nil {
		t.Fatalf("UnitPath: %v", err)
	}
	if body != "" {
		if err := writeFileAtomically(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func unitBody(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatalf("reading the unit: %v", err)
	}
	return string(data), true
}

func TestRemoveRotationWithoutAUnitTouchesNothing(t *testing.T) {
	f := newFakeCommands(t)
	installedUnit(t, "")

	if err := RemoveRotation(context.Background()); err != nil {
		t.Fatalf("RemoveRotation with no unit: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("an absent unit still reached the user manager: %v", f.calls)
	}
}

func TestRemoveRotationDisablesThenDeletesTheUnit(t *testing.T) {
	f := newFakeCommands(t)
	path := installedUnit(t, "installed unit")

	if err := RemoveRotation(context.Background()); err != nil {
		t.Fatalf("RemoveRotation: %v", err)
	}
	if _, ok := unitBody(t, path); ok {
		t.Fatal("the unit file survived a successful removal")
	}
	want := []string{
		"systemctl --user disable " + UnitName,
		"systemctl --user daemon-reload",
	}
	if !slices.Equal(f.calls, want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
}

func TestRemoveRotationInDryRunKeepsTheUnit(t *testing.T) {
	f := newFakeCommands(t)
	path := installedUnit(t, "installed unit")
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	if err := RemoveRotation(context.Background()); err != nil {
		t.Fatalf("RemoveRotation in dry-run: %v", err)
	}
	if body, ok := unitBody(t, path); !ok || body != "installed unit" {
		t.Fatalf("dry-run changed the unit: %q, present=%t", body, ok)
	}
	if len(f.calls) != 0 {
		t.Fatalf("dry-run reached the user manager: %v", f.calls)
	}
}

func TestRemoveRotationKeepsTheUnitWhenDisableIsRefused(t *testing.T) {
	f := newFakeCommands(t)
	path := installedUnit(t, "installed unit")
	f.fail["systemctl --user disable"] = errors.New("user manager refused")

	if err := RemoveRotation(context.Background()); err == nil {
		t.Fatal("a refused disable reported success")
	}
	if body, ok := unitBody(t, path); !ok || body != "installed unit" {
		t.Fatalf("a refused disable still removed the unit: %q, present=%t", body, ok)
	}
	if f.sawPrefix("systemctl --user daemon-reload") {
		t.Fatalf("reloaded after a refused disable: %v", f.calls)
	}
}

// TestRemoveRotationRestoresTheUnitWhenReloadFails pins the rollback: once
// the file is gone, a failed reload must put the previous unit back and
// re-enable it, so the switch the page still shows matches the schedule.
func TestRemoveRotationRestoresTheUnitWhenReloadFails(t *testing.T) {
	f := newFakeCommands(t)
	path := installedUnit(t, "installed unit")
	f.fail["systemctl --user daemon-reload"] = errors.New("user manager unavailable")

	err := RemoveRotation(context.Background())
	if err == nil {
		t.Fatal("a failed reload reported success")
	}
	if body, ok := unitBody(t, path); !ok || body != "installed unit" {
		t.Fatalf("rollback did not restore the previous unit: %q, present=%t", body, ok)
	}
	if !f.sawPrefix("systemctl --user enable " + UnitName) {
		t.Fatalf("rollback did not re-enable the unit: %v", f.calls)
	}
}

func TestSyncRotationUnitRemovesTheUnitWhenNothingRotates(t *testing.T) {
	newFakeCommands(t)
	path := installedUnit(t, "installed unit")

	if err := SyncRotationUnit(context.Background(), State{}); err != nil {
		t.Fatalf("SyncRotationUnit: %v", err)
	}
	if _, ok := unitBody(t, path); ok {
		t.Fatal("the unit survived with both rotation switches off")
	}
}

func TestConfigureRotationLeavesTheUnitWhenSettingsCannotBeRead(t *testing.T) {
	f := rotateState(t, map[string]string{KeyPanelRotate: "false", KeyDockRotate: "true"})
	path := installedUnit(t, "installed unit")
	f.fail["gsettings list-recursively "+SchemaID] = errors.New("dconf unavailable")

	observed, err := ConfigureRotation(context.Background(), State{})
	if err == nil {
		t.Fatal("an unreadable state reported success")
	}
	if observed != (State{}) {
		t.Fatalf("returned a state it could not read: %+v", observed)
	}
	if f.sawPrefix("systemctl") || f.sawPrefix("gsettings set") {
		t.Fatalf("acted without knowing the current state: %v", f.calls)
	}
	if body, ok := unitBody(t, path); !ok || body != "installed unit" {
		t.Fatalf("unit changed: %q, present=%t", body, ok)
	}
}

// TestConfigureRotationRollsBackWhenAPreferenceWriteFails covers the two
// writes after the schedule was accepted: whichever fails, the persisted
// switches and the unit must both return to the previous choice.
func TestConfigureRotationRollsBackWhenAPreferenceWriteFails(t *testing.T) {
	panelSet := "gsettings set " + SchemaID + " " + KeyPanelRotate + " "
	dockSet := "gsettings set " + SchemaID + " " + KeyDockRotate + " "

	t.Run("panel", func(t *testing.T) {
		f := rotateState(t, map[string]string{KeyPanelRotate: "false", KeyDockRotate: "false"})
		path := installedUnit(t, "")
		f.fail[panelSet+"true"] = errors.New("dconf write refused")

		observed, err := ConfigureRotation(context.Background(), State{PanelRotate: true})
		if err == nil {
			t.Fatal("a refused panel write reported success")
		}
		if observed.PanelRotate || observed.DockRotate {
			t.Fatalf("returned the rejected choice: %+v", observed)
		}
		if f.sawPrefix(dockSet) {
			t.Fatalf("wrote the dock switch after the panel write failed: %v", f.calls)
		}
		if _, ok := unitBody(t, path); ok {
			t.Fatal("rollback left the unit installed though nothing rotates")
		}
	})

	t.Run("dock", func(t *testing.T) {
		f := rotateState(t, map[string]string{KeyPanelRotate: "false", KeyDockRotate: "false"})
		path := installedUnit(t, "")
		f.fail[dockSet+"true"] = errors.New("dconf write refused")

		observed, err := ConfigureRotation(context.Background(), State{PanelRotate: true, DockRotate: true})
		if err == nil {
			t.Fatal("a refused dock write reported success")
		}
		if observed.PanelRotate || observed.DockRotate {
			t.Fatalf("returned the rejected choice: %+v", observed)
		}
		if !slices.Contains(f.calls, panelSet+"false") {
			t.Fatalf("the panel switch was not restored: %v", f.calls)
		}
		if _, ok := unitBody(t, path); ok {
			t.Fatal("rollback left the unit installed though nothing rotates")
		}
	})
}
