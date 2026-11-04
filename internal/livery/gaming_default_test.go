package livery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/deskenv"
)

func enablePanelGroup(t *testing.T, enabled bool) {
	t.Helper()
	original := panelGroupEnabled
	panelGroupEnabled = func() bool { return enabled }
	t.Cleanup(func() { panelGroupEnabled = original })
}

func gamingDefaultCalls(t *testing.T, gaming bool, values map[string]string) []string {
	t.Helper()
	f := rotateState(t, values)
	enablePanelGroup(t, true)
	detectGaming = func() bool { return gaming }
	originalDesktop := detectDesktop
	detectDesktop = func() deskenv.Desktop { return deskenv.GNOME }
	t.Cleanup(func() { detectDesktop = originalDesktop })

	if err := EnsureGamingPanelDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f.calls
}

func wroteKey(calls []string, key string) bool {
	for _, call := range calls {
		if strings.HasPrefix(call, "gsettings set "+SchemaID+" "+key+" ") {
			return true
		}
	}
	return false
}

func TestEnsureGamingPanelDefaultAppliesOnce(t *testing.T) {
	calls := gamingDefaultCalls(t, true, map[string]string{KeyPanelEnabled: "false", KeyPanelID: "''"})
	if !wroteKey(calls, KeyPanelEnabled) || !wroteKey(calls, KeyPanelID) {
		t.Fatalf("gaming default not persisted: %v", calls)
	}
}

func TestEnsureGamingPanelDefaultLeavesChoicesAlone(t *testing.T) {
	cases := map[string]struct {
		gaming bool
		values map[string]string
	}{
		"not gaming":     {false, map[string]string{KeyPanelEnabled: "false", KeyPanelID: "''"}},
		"user chose":     {true, map[string]string{KeyPanelEnabled: "false", KeyPanelID: "'cncf'"}},
		"already on":     {true, map[string]string{KeyPanelEnabled: "true", KeyPanelID: "''"}},
		"user opted out": {true, map[string]string{KeyPanelEnabled: "false", KeyPanelID: "'open-gaming-collective'"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls := gamingDefaultCalls(t, tc.gaming, tc.values)
			if wroteKey(calls, KeyPanelEnabled) || wroteKey(calls, KeyPanelID) {
				t.Fatalf("settings changed: %v", calls)
			}
		})
	}
}

func TestEnsureGamingPanelDefaultSkipsWithoutPanelExtension(t *testing.T) {
	f := rotateState(t, map[string]string{KeyPanelEnabled: "false", KeyPanelID: "''"})
	enablePanelGroup(t, true)
	detectGaming = func() bool { return true }
	originalDesktop := detectDesktop
	detectDesktop = func() deskenv.Desktop { return deskenv.GNOME }
	t.Cleanup(func() { detectDesktop = originalDesktop })
	probe := "gsettings get " + extensionSchema
	f.reply[probe] = "No such schema \u201c" + extensionSchema + "\u201d"
	f.fail[probe] = errors.New("exit status 1")

	if err := EnsureGamingPanelDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.sawPrefix("gsettings set") {
		t.Fatalf("settings written without the panel extension: %v", f.calls)
	}
}

func TestEnsureGamingPanelDefaultSkipsWhenGroupDisabled(t *testing.T) {
	f := rotateState(t, map[string]string{KeyPanelEnabled: "false", KeyPanelID: "''"})
	enablePanelGroup(t, false)
	detectGaming = func() bool { return true }
	originalDesktop := detectDesktop
	detectDesktop = func() deskenv.Desktop { return deskenv.GNOME }
	t.Cleanup(func() { detectDesktop = originalDesktop })

	if err := EnsureGamingPanelDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.sawPrefix("gsettings set") {
		t.Fatalf("settings written with livery_foundation_group disabled: %v", f.calls)
	}
}
