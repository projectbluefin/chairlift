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

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestResolveCoversEveryState(t *testing.T) {
	tests := []struct {
		name string
		f    Facts
		want State
	}{
		{"no homebrew outranks everything", Facts{UnitPresent: true, Checked: true, Healthy: true}, StateUnavailable},
		{"fresh host", Facts{Capable: true}, StateUnconfigured},
		{"in flight", Facts{Capable: true, Installed: true, Working: true}, StateProvisioning},
		{"unit not yet probed", Facts{Capable: true, Installed: true, UnitPresent: true}, StateProvisioning},
		{"unit and healthy endpoint", Facts{Capable: true, Installed: true, UnitPresent: true, Checked: true, Healthy: true}, StateReady},
		{"unit without endpoint", Facts{Capable: true, Installed: true, UnitPresent: true, Checked: true}, StateDegraded},
		{"llmman kept after disable", Facts{Capable: true, Installed: true}, StateDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.f); got != tt.want {
				t.Errorf("Resolve(%+v) = %v, want %v", tt.f, got, tt.want)
			}
		})
	}
}

func TestSwitchReadsOnOnlyWhileTheUnitIsOwned(t *testing.T) {
	on := map[State]bool{StateProvisioning: true, StateReady: true, StateDegraded: true}
	for s := StateUnavailable; s <= StateDisabled; s++ {
		if s.On() != on[s] {
			t.Errorf("State(%d).On() = %v", s, s.On())
		}
	}
}

func TestBrewfileTapsBeforeInstallingAndGatesJanOnArch(t *testing.T) {
	amd := Brewfile("amd64", false)
	tap, formula := strings.Index(amd, `tap "llmmanorg/tap"`), strings.Index(amd, `brew "llmmanorg/tap/llmman"`)
	if tap < 0 || formula < 0 || tap > formula {
		t.Errorf("amd64 Brewfile must tap before installing the formula:\n%s", amd)
	}
	if !strings.Contains(amd, `flatpak "ai.jan.Jan"`) {
		t.Errorf("amd64 Brewfile omits Jan:\n%s", amd)
	}
	if strings.Contains(Brewfile("arm64", false), "ai.jan.Jan") {
		t.Error("arm64 Brewfile installs Jan, whose Flathub build is x86_64-only")
	}
	if got := Brewfile("amd64", true); strings.Contains(got, "llmman") || !strings.Contains(got, "ai.jan.Jan") {
		t.Errorf("with llmman present the formula must be skipped and Jan kept:\n%s", got)
	}
	if got := Brewfile("arm64", true); got != "" {
		t.Errorf("nothing to install should be empty, got %q", got)
	}
}

func TestRenderUnitBindsLoopbackWithShellAndHistoryOff(t *testing.T) {
	unit, err := RenderUnit("/opt/brew/bin/llmman")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ExecStart=/opt/brew/bin/llmman serve\n",
		"Environment=LLMMAN_HOST=127.0.0.1:17434\n",
		"Environment=LLMMAN_SHELL=off\n",
		"Environment=LLMMAN_NOHISTORY=1\n",
		"WantedBy=default.target\n",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
	for _, banned := range []string{"linuxbrew", "%h", "0.0.0.0", "LLMMAN_ORIGINS", "LLMMAN_AUTH", "OPENAI"} {
		if strings.Contains(unit, banned) {
			t.Errorf("unit contains %q:\n%s", banned, unit)
		}
	}
}

func TestRenderUnitRejectsPathsSystemdWouldReinterpret(t *testing.T) {
	for _, exe := range []string{"llmman", "bin/llmman", "/home/a b/llmman", "/x/%h/llmman", "/x/$HOME/llmman"} {
		if _, err := RenderUnit(exe); err == nil {
			t.Errorf("RenderUnit(%q) accepted", exe)
		}
	}
}

func TestEnvFragmentPublishesOnlyOllamaHost(t *testing.T) {
	if got := EnvFragment(); got != "OLLAMA_HOST=127.0.0.1:17434\n" {
		t.Errorf("EnvFragment() = %q", got)
	}
}

// host isolates every seam: a temp config dir, a fake llmman beside a fake
// brew, a recorded command runner, and a recorded bundle install.
type host struct {
	config  string
	exe     string
	calls   []string
	bundles []string
	fail    map[string]error
	outputs map[string]string
}

func newHost(t *testing.T) *host {
	t.Helper()
	h := &host{config: t.TempDir(), fail: map[string]error{}, outputs: map[string]string{}}
	bin := t.TempDir()
	h.exe = filepath.Join(bin, "llmman")
	if err := os.WriteFile(h.exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := []func(){}
	restore := func(f func()) { saved = append(saved, f) }
	oc, ol, ob, oi, or, on := configDir, lookPath, brewPath, installBundle, run, nodeURL
	restore(func() { configDir, lookPath, brewPath, installBundle, run, nodeURL = oc, ol, ob, oi, or, on })
	configDir = func() (string, error) { return h.config, nil }
	lookPath = func(string) (string, error) { return "", errors.New("not on PATH") }
	brewPath = func() string { return filepath.Join(bin, "brew") }
	installBundle = func(path string) error {
		data, err := os.ReadFile(path)
		h.bundles = append(h.bundles, string(data))
		return err
	}
	run = func(_ context.Context, name string, args ...string) (string, error) {
		call := strings.TrimSpace(filepath.Base(name) + " " + strings.Join(args, " "))
		h.calls = append(h.calls, call)
		return h.outputs[call], h.fail[call]
	}
	t.Cleanup(func() {
		for _, f := range saved {
			f()
		}
	})
	return h
}

func (h *host) exists(t *testing.T, rel string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(h.config, rel))
	return err == nil
}

const (
	unitRel = "systemd/user/" + ServiceName
	envRel  = "environment.d/" + EnvFragmentName
)

func TestEnableProvisionsInOrderWithTheResolvedPath(t *testing.T) {
	h := newHost(t)
	if err := Enable(context.Background()); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	want := []string{
		"llmman serve --pull-only",
		"systemctl --user daemon-reload",
		"systemctl --user enable " + ServiceName,
		"systemctl --user restart " + ServiceName,
		"dbus-update-activation-environment --systemd OLLAMA_HOST=127.0.0.1:17434",
	}
	if strings.Join(h.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(h.calls, "\n"), strings.Join(want, "\n"))
	}
	unit, err := os.ReadFile(filepath.Join(h.config, unitRel))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), "ExecStart="+h.exe+" serve\n") {
		t.Errorf("unit does not use the resolved path %s:\n%s", h.exe, unit)
	}
	env, err := os.ReadFile(filepath.Join(h.config, envRel))
	if err != nil || string(env) != EnvFragment() {
		t.Errorf("fragment = %q, %v", env, err)
	}
	// llmman already resolved, so only Jan (or nothing) was bundled.
	for _, b := range h.bundles {
		if strings.Contains(b, Formula) {
			t.Errorf("bundled the formula although llmman was present:\n%s", b)
		}
	}
}

func TestEnableInstallsTheFormulaWhenLLMManIsMissing(t *testing.T) {
	h := newHost(t)
	if err := os.Remove(h.exe); err != nil {
		t.Fatal(err)
	}
	installBundle = func(path string) error {
		data, _ := os.ReadFile(path)
		h.bundles = append(h.bundles, string(data))
		return os.WriteFile(h.exe, []byte("#!/bin/sh\n"), 0o755)
	}
	if err := Enable(context.Background()); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if len(h.bundles) != 1 || !strings.Contains(h.bundles[0], `brew "`+Formula+`"`) {
		t.Errorf("bundles = %q", h.bundles)
	}
}

func TestEnableStopsBeforeWritingWhenTheRuntimeCheckFails(t *testing.T) {
	h := newHost(t)
	h.fail["llmman serve --pull-only"] = errors.New("no engine")
	if err := Enable(context.Background()); err == nil {
		t.Fatal("Enable succeeded with a failed runtime check")
	}
	if h.exists(t, unitRel) || h.exists(t, envRel) {
		t.Error("files written despite a failed runtime check")
	}
}

func TestEnableRollsBackWhatItWroteWhenStartFails(t *testing.T) {
	h := newHost(t)
	h.fail["systemctl --user restart "+ServiceName] = errors.New("start failed")
	if err := Enable(context.Background()); err == nil {
		t.Fatal("Enable succeeded with a failed start")
	}
	if h.exists(t, unitRel) || h.exists(t, envRel) {
		t.Error("unit or fragment left behind after a failed start")
	}
}

func TestEnableKeepsAPreexistingUnitWhenStartFails(t *testing.T) {
	h := newHost(t)
	if err := writeAtomic(filepath.Join(h.config, unitRel), "old"); err != nil {
		t.Fatal(err)
	}
	h.fail["systemctl --user restart "+ServiceName] = errors.New("start failed")
	if err := Enable(context.Background()); err == nil {
		t.Fatal("Enable succeeded with a failed start")
	}
	if !h.exists(t, unitRel) {
		t.Error("retry removed a unit this call did not create")
	}
}

func TestEnableFailsWhenNoExecutableResolvesAfterInstall(t *testing.T) {
	h := newHost(t)
	if err := os.Remove(h.exe); err != nil {
		t.Fatal(err)
	}
	if err := Enable(context.Background()); err == nil {
		t.Fatal("Enable succeeded without an llmman executable")
	}
	if len(h.calls) != 0 || h.exists(t, unitRel) {
		t.Errorf("acted without an executable: %v", h.calls)
	}
}

func TestDryRunMutatesNothing(t *testing.T) {
	h := newHost(t)
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	if err := Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 0 || len(h.bundles) != 0 || h.exists(t, unitRel) || h.exists(t, envRel) {
		t.Errorf("dry run acted: calls=%v bundles=%v", h.calls, h.bundles)
	}
}

func TestDisableRemovesOnlyOwnedFiles(t *testing.T) {
	h := newHost(t)
	for _, rel := range []string{unitRel, envRel, "environment.d/50-user.conf"} {
		if err := writeAtomic(filepath.Join(h.config, rel), "x"); err != nil {
			t.Fatal(err)
		}
	}
	if err := Disable(context.Background()); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if h.exists(t, unitRel) || h.exists(t, envRel) {
		t.Error("owned file survived disable")
	}
	if !h.exists(t, "environment.d/50-user.conf") {
		t.Error("disable removed a user's own environment fragment")
	}
	if !strings.Contains(strings.Join(h.calls, "\n"), "systemctl --user unset-environment OLLAMA_HOST") {
		t.Errorf("OLLAMA_HOST not cleared: %v", h.calls)
	}
	if _, err := os.Stat(h.exe); err != nil {
		t.Error("disable removed the llmman binary")
	}
}

func TestDisableKeepsTheUnitWhileTheServiceMayStillRun(t *testing.T) {
	for state, keep := range map[string]bool{"active": true, "deactivating": true, "": true, "inactive": false, "failed": false} {
		t.Run(state, func(t *testing.T) {
			h := newHost(t)
			for _, rel := range []string{unitRel, envRel} {
				if err := writeAtomic(filepath.Join(h.config, rel), "x"); err != nil {
					t.Fatal(err)
				}
			}
			h.fail["systemctl --user disable --now "+ServiceName] = errors.New("stop failed")
			h.outputs["systemctl --user is-active "+ServiceName] = state
			err := Disable(context.Background())
			if (err != nil) != keep || h.exists(t, unitRel) != keep || h.exists(t, envRel) != keep {
				t.Errorf("state %q: err=%v unit=%v fragment=%v, want kept=%v", state, err, h.exists(t, unitRel), h.exists(t, envRel), keep)
			}
		})
	}
}

func TestHealthyRequiresAJSONAnswerFromTheNodeRoute(t *testing.T) {
	newHost(t)
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("case") {
		case "ok":
			_, _ = w.Write([]byte(`{"memory":1,"loaded":{},"stored":{}}`))
		case "error":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "html":
			_, _ = w.Write([]byte("<html>"))
		case "hang":
			<-block
		}
	}))
	t.Cleanup(srv.Close)
	// Registered after srv.Close so it runs first: Close waits for handlers.
	t.Cleanup(func() { close(block) })
	for c, want := range map[string]bool{"ok": true, "error": false, "html": false, "hang": false} {
		nodeURL = srv.URL + "/llmman/node?case=" + c
		start := time.Now()
		if got := Healthy(context.Background()); got != want {
			t.Errorf("%s: Healthy = %v, want %v", c, got, want)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("%s: probe was not bounded", c)
		}
	}
}

func TestExecutablePrefersPathThenBrewSibling(t *testing.T) {
	h := newHost(t)
	if got := Executable(); got != h.exe {
		t.Errorf("brew sibling: got %q want %q", got, h.exe)
	}
	lookPath = func(string) (string, error) { return "/usr/local/bin/llmman", nil }
	if got := Executable(); got != "/usr/local/bin/llmman" {
		t.Errorf("PATH: got %q", got)
	}
	lookPath = func(string) (string, error) { return "", errors.New("none") }
	brewPath = func() string { return "" }
	if got := Executable(); got != "" {
		t.Errorf("no brew, no PATH: got %q", got)
	}
}
