package devtools

import (
	"context"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// A VM can be running while its shell is broken. Keep it stoppable and retain
// the backend's diagnosis instead of turning that observation into plain success.
func TestWSLRunningShellFailureRetainsDiagnosis(t *testing.T) {
	for _, backend := range []string{BackendNSL, BackendLima} {
		t.Run(backend, func(t *testing.T) {
			if backend == BackendNSL {
				fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' 'ubuntu running' ;;\nrun) echo 'guest disk is unavailable' >&2; exit 1 ;;\nesac\n")
			} else {
				fakeLima(t, "case \"$1\" in\nlist) echo '{\"name\":\"ubuntu\",\"status\":\"Running\"}' ;;\nshell) echo 'guest disk is unavailable' >&2; exit 1 ;;\nesac\n")
			}
			state, err := WSLStatus(context.Background(), backend)
			if err != nil || !state.Exists || !state.Running || state.Ready {
				t.Fatalf("running machine must remain stoppable: state=%+v err=%v", state, err)
			}
			if !strings.Contains(state.Diagnostic, "guest disk is unavailable") {
				t.Fatalf("shell failure diagnosis was lost: %+v", state)
			}
		})
	}
}

func TestWSLPreviewNeverProbesAShell(t *testing.T) {
	previous := dryrun.Enabled()
	t.Cleanup(func() { dryrun.Set(previous) })
	for _, backend := range []string{BackendNSL, BackendLima} {
		t.Run(backend, func(t *testing.T) {
			var calls, list, probe string
			if backend == BackendNSL {
				calls = fakeNSL(t, "case \"$1\" in\nlist) printf '%s\\n' 'MACHINE STATE' 'ubuntu running' ;;\n*) exit 0 ;;\nesac\n")
				list, probe = "list", "run -m ubuntu --cd / true"
			} else {
				calls = fakeLima(t, "case \"$1\" in\nlist) echo '{\"name\":\"ubuntu\",\"status\":\"Running\"}' ;;\n*) exit 0 ;;\nesac\n")
				list, probe = "list --json", "shell ubuntu true"
			}
			dryrun.Set(true)
			state, err := WSLStatus(context.Background(), backend)
			assertCalls(t, calls, list)
			if err != nil || !state.Exists || !state.Running || state.Ready || !state.ReadinessUnchecked || state.Diagnostic != "" {
				t.Fatalf("preview must preserve running inventory without claiming readiness: %+v %v", state, err)
			}
			dryrun.Set(false)
			state, err = WSLStatus(context.Background(), backend)
			if err != nil || !state.Ready || state.ReadinessUnchecked {
				t.Fatalf("live observation must still verify shell readiness: %+v %v", state, err)
			}
			assertCalls(t, calls, list, list, probe)
		})
	}
}
