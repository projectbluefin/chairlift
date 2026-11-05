package pageview

import (
	"errors"
	"testing"

	"github.com/projectbluefin/chairlift/internal/devtools"
)

func TestWSLEngineDocumentationURL(t *testing.T) {
	for _, tc := range []struct {
		backend string
		wantURL string
		wantTip string
	}{
		{"nsl", NSLDocumentationURL, "NSL documentation"},
		{"", NSLDocumentationURL, "NSL documentation"},
		{"other", NSLDocumentationURL, "NSL documentation"},
		{"lima", "https://lima-vm.io/docs/", "Lima documentation"},
	} {
		if got := WSLEngineDocumentationURL(tc.backend); got != tc.wantURL {
			t.Errorf("WSLEngineDocumentationURL(%q) = %q, want %q", tc.backend, got, tc.wantURL)
		}
		if got := WSLEngineDocumentationTooltip(tc.backend); got != tc.wantTip {
			t.Errorf("WSLEngineDocumentationTooltip(%q) = %q, want %q", tc.backend, got, tc.wantTip)
		}
	}
}

func TestWSLDiagnosticsKeepBackendFailuresVisible(t *testing.T) {
	create := errors.New("nsl [create ubuntu --distro ubuntu:26.04]: exit status 1: creating ubuntu failed; nothing was kept: lstat /home/tester/.local/share/nsl/vm/data.raw: no such file or directory")
	read := errors.New("nsl [list]: exit status 1: service unavailable")
	probe := "nsl [run -m ubuntu --cd / true]: exit status 1: machine not ready"
	for _, tc := range []struct {
		name          string
		action, state error
		probe, want   string
	}{
		{"create", create, nil, "", create.Error()},
		{"post-action read", create, read, "", create.Error() + "\n" + read.Error()},
		{"read after awaiting login", nil, read, "", read.Error()},
		{"read already in setup result", errors.Join(create, read), read, "", create.Error() + "\n" + read.Error()},
		{"readiness", nil, nil, probe, probe},
		{"all failures", create, read, probe, create.Error() + "\n" + read.Error() + "\n" + probe},
		{"healthy", nil, nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WSLDetails(tc.action, tc.state, tc.probe); got != tc.want {
				t.Errorf("WSLDetails = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWSLObservationPreservesKnownRunningMachineOnReadFailure(t *testing.T) {
	previous := devtools.WSLState{Exists: true, Running: true, Machine: "debian"}
	err := errors.New("list failed")
	if got := WSLObservation(previous, devtools.WSLState{}, err); got != previous {
		t.Fatalf("failed read replaced running state: %+v", got)
	}
	stopped := devtools.WSLState{Exists: true, Machine: "debian"}
	if got := WSLObservation(previous, stopped, nil); got != stopped {
		t.Fatalf("successful recheck did not replace state: %+v", got)
	}
	for _, tc := range []struct {
		backend, want string
	}{
		{devtools.BackendNSL, "Couldn't check your Linux virtual machine."},
		{devtools.BackendLima, "Couldn't check the Ubuntu virtual machine."},
	} {
		if got := WSLSubtitle(tc.backend, previous, err); got != tc.want {
			t.Errorf("%s failed read subtitle = %q, want %q", tc.backend, got, tc.want)
		}
	}
}

func TestWSLActionAdmissionOnReadFailure(t *testing.T) {
	for _, tc := range []struct {
		name              string
		active, canEnable bool
		err               error
		want              bool
	}{
		{"running with failed read", true, false, errors.New("read failed"), true},
		{"running without prerequisites", true, false, nil, true},
		{"stopped with failed read", false, true, errors.New("read failed"), false},
		{"stopped with prerequisites", false, true, nil, true},
		{"stopped without prerequisites", false, false, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WSLAllowed(tc.active, tc.canEnable, tc.err); got != tc.want {
				t.Errorf("WSLAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}
