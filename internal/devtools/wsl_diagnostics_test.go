package devtools

import (
	"context"
	"strings"
	"testing"
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
