package livery

import (
	"context"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/deskenv"
)

func gamingDefaultCalls(t *testing.T, gaming bool, values map[string]string) []string {
	t.Helper()
	f := rotateState(t, values)
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
