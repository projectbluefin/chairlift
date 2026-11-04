package aistack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Observe drives the Agent Mode switch on the GTK main thread: Installed and
// UnitPresent must come from the resolved executable and ChairLift's own unit
// file, and an unknown config directory must read as "no unit", not on.
func TestObserveReportsTheExecutableAndOwnedUnit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		installed bool
		unit      bool
	}{
		{"nothing", false, false},
		{"runtime only", true, false},
		{"unit only", false, true},
		{"runtime and unit", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			if !tc.installed {
				if err := os.Remove(h.exe); err != nil {
					t.Fatal(err)
				}
			}
			if tc.unit {
				if err := writeAtomic(filepath.Join(h.config, unitRel), "x"); err != nil {
					t.Fatal(err)
				}
			}
			want := Facts{Capable: true, Installed: tc.installed, UnitPresent: tc.unit}
			if got := Observe(true); got != want {
				t.Errorf("Observe(true) = %+v, want %+v", got, want)
			}
		})
	}

	t.Run("unknown config dir", func(t *testing.T) {
		h := newHost(t)
		if err := writeAtomic(filepath.Join(h.config, unitRel), "x"); err != nil {
			t.Fatal(err)
		}
		configDir = func() (string, error) { return "", errors.New("no HOME") }
		got := Observe(false)
		if got.Capable || got.UnitPresent || !got.Installed {
			t.Errorf("Observe(false) with no config dir = %+v", got)
		}
	})
}

func TestWaitHealthyReturnsOnTheFirstHealthyAnswer(t *testing.T) {
	newHost(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"memory":1}`))
	}))
	t.Cleanup(srv.Close)
	nodeURL = srv.URL + "/llmman/node"
	start := time.Now()
	if !WaitHealthy(context.Background(), time.Minute) {
		t.Fatal("WaitHealthy = false for a healthy daemon")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("WaitHealthy did not return on the first healthy answer")
	}
}

func TestWaitHealthyGivesUpWhenTheWindowCloses(t *testing.T) {
	newHost(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	nodeURL = srv.URL + "/llmman/node"
	srv.Close()
	start := time.Now()
	if WaitHealthy(context.Background(), 50*time.Millisecond) {
		t.Fatal("WaitHealthy = true with no daemon listening")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("WaitHealthy outlived its window")
	}
}

func TestWaitHealthyHonoursACancelledCaller(t *testing.T) {
	newHost(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	nodeURL = srv.URL + "/llmman/node"
	srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if WaitHealthy(ctx, time.Hour) {
		t.Fatal("WaitHealthy = true for a cancelled caller")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("WaitHealthy ignored the caller's cancellation")
	}
}

// A unit systemd refuses to enable must not be left behind claiming Agent
// Mode is on, and nothing may be started.
func TestEnableRollsBackWhenTheUnitCannotBeEnabled(t *testing.T) {
	h := newHost(t)
	h.fail["systemctl --user enable --no-reload "+ServiceName] = errors.New("enable failed")
	if err := Enable(context.Background()); err == nil {
		t.Fatal("Enable succeeded although systemd refused the unit")
	}
	if h.exists(t, unitRel) || h.exists(t, envRel) {
		t.Error("unit or fragment left behind after enable failed")
	}
	calls := strings.Join(h.calls, "\n")
	if strings.Contains(calls, "systemctl --user restart "+ServiceName) {
		t.Errorf("started a service systemd refused to enable: %v", h.calls)
	}
	if !strings.HasSuffix(calls, "systemctl --user daemon-reload") {
		t.Errorf("rollback did not reload systemd last: %v", h.calls)
	}
}

func TestEnableRollsBackTheUnitWhenTheFragmentCannotBeWritten(t *testing.T) {
	h := newHost(t)
	// A regular file where environment.d must be makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(h.config, "environment.d"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Enable(context.Background()); err == nil {
		t.Fatal("Enable succeeded without writing the environment fragment")
	}
	if h.exists(t, unitRel) {
		t.Error("unit left behind after the fragment write failed")
	}
	if strings.Contains(strings.Join(h.calls, "\n"), "enable --no-reload") {
		t.Errorf("enabled the unit after the fragment write failed: %v", h.calls)
	}
}

// Publishing OLLAMA_HOST to the running session is best effort: a session
// bus that refuses it must not turn a started service into a failed enable.
func TestEnableSucceedsWhenTheSessionEnvironmentCannotBeUpdated(t *testing.T) {
	h := newHost(t)
	h.fail["dbus-update-activation-environment --systemd OLLAMA_HOST="+Address] = errors.New("no session bus")
	if err := Enable(context.Background()); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !h.exists(t, unitRel) || !h.exists(t, envRel) {
		t.Error("a best-effort session update failure removed the unit or fragment")
	}
}

func TestEnableAndDisableActOnNothingWithoutAConfigDir(t *testing.T) {
	h := newHost(t)
	configDir = func() (string, error) { return "", errors.New("no HOME") }
	if err := Enable(context.Background()); err == nil {
		t.Error("Enable succeeded without a config directory")
	}
	if err := Disable(context.Background()); err == nil {
		t.Error("Disable succeeded without a config directory")
	}
	if len(h.calls) != 0 || len(h.brew) != 0 || len(h.bundles) != 0 {
		t.Errorf("acted without a config directory: calls=%v brew=%v bundles=%v", h.calls, h.brew, h.bundles)
	}
	if _, err := UnitPath(); err == nil {
		t.Error("UnitPath succeeded without a config directory")
	}
	if _, err := EnvFragmentPath(); err == nil {
		t.Error("EnvFragmentPath succeeded without a config directory")
	}
}

// systemd reports "unknown" for a unit it no longer has loaded; that is as
// stopped as "inactive", so the owned files go. Its answer is trimmed first.
func TestDisableTreatsAnUnknownOrPaddedStoppedStateAsStopped(t *testing.T) {
	for _, state := range []string{"unknown", "inactive\n", " failed "} {
		t.Run(strings.TrimSpace(state), func(t *testing.T) {
			h := newHost(t)
			for _, rel := range []string{unitRel, envRel} {
				if err := writeAtomic(filepath.Join(h.config, rel), "x"); err != nil {
					t.Fatal(err)
				}
			}
			h.fail["systemctl --user disable --now "+ServiceName] = errors.New("stop failed")
			h.outputs["systemctl --user is-active "+ServiceName] = state
			if err := Disable(context.Background()); err != nil {
				t.Fatalf("Disable: %v", err)
			}
			if h.exists(t, unitRel) || h.exists(t, envRel) {
				t.Error("owned file survived disable of a stopped service")
			}
		})
	}
}

func TestDisableReportsWhyTheServiceCouldNotBeConfirmedStopped(t *testing.T) {
	h := newHost(t)
	stopErr := errors.New("stop failed")
	h.fail["systemctl --user disable --now "+ServiceName] = stopErr
	h.fail["systemctl --user is-active "+ServiceName] = errors.New("bus unreachable")
	err := Disable(context.Background())
	if !errors.Is(err, stopErr) || !strings.Contains(err.Error(), "could not verify") || !strings.Contains(err.Error(), "bus unreachable") {
		t.Errorf("Disable error = %v, want the stop error and why it could not be verified", err)
	}

	h.fail["systemctl --user is-active "+ServiceName] = nil
	h.outputs["systemctl --user is-active "+ServiceName] = "activating"
	err = Disable(context.Background())
	if !errors.Is(err, stopErr) || !strings.Contains(err.Error(), ServiceName+" is activating") {
		t.Errorf("Disable error = %v, want the stop error and the live state", err)
	}
}

func TestDisableSurfacesFilesItCannotRemoveAndAReloadFailure(t *testing.T) {
	t.Run("unremovable unit", func(t *testing.T) {
		h := newHost(t)
		// A non-empty directory at the unit path cannot be os.Remove'd.
		if err := writeAtomic(filepath.Join(h.config, unitRel, "keep"), "x"); err != nil {
			t.Fatal(err)
		}
		if err := Disable(context.Background()); err == nil {
			t.Error("Disable succeeded although the unit could not be removed")
		}
	})
	t.Run("reload", func(t *testing.T) {
		h := newHost(t)
		reloadErr := errors.New("reload failed")
		h.fail["systemctl --user daemon-reload"] = reloadErr
		if err := Disable(context.Background()); !errors.Is(err, reloadErr) {
			t.Errorf("Disable error = %v, want %v", err, reloadErr)
		}
	})
	t.Run("session environment is best effort", func(t *testing.T) {
		h := newHost(t)
		h.fail["systemctl --user unset-environment OLLAMA_HOST"] = errors.New("no session bus")
		if err := Disable(context.Background()); err != nil {
			t.Errorf("Disable: %v", err)
		}
	})
}

func TestExecCommandReportsTrimmedOutputAndTheLastErrorLine(t *testing.T) {
	const sh = "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skip("no /bin/sh")
	}
	out, err := execCommand(context.Background(), sh, "-c", "echo '  hello  '")
	if err != nil || out != "hello" {
		t.Errorf("success: out=%q err=%v", out, err)
	}
	out, err = execCommand(context.Background(), sh, "-c", "echo first; echo last >&2; exit 3")
	if err == nil {
		t.Fatal("non-zero exit reported as success")
	}
	if out != "first\nlast" {
		t.Errorf("failure output = %q", out)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "sh -c ") || !strings.HasSuffix(msg, ": last") || strings.Contains(msg, ": first") {
		t.Errorf("failure error = %q, want the command and only the last output line", msg)
	}
}

func TestLastLine(t *testing.T) {
	for in, want := range map[string]string{"": "", "one": "one", "a\nb": "b", "a\nb\n": ""} {
		if got := lastLine(in); got != want {
			t.Errorf("lastLine(%q) = %q, want %q", in, got, want)
		}
	}
}
