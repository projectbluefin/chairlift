package agentmode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/launcher"
	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

func TestReadinessTable(t *testing.T) {
	tools := installedTools()
	noDesktop, noServer, noLLMMan, unsupported := tools, tools, tools, tools
	noDesktop.DesktopPath = ""
	noServer.ServerPath = ""
	noLLMMan.LLMManPath = ""
	unsupported.Supported = false

	tests := []struct {
		name      string
		facts     ReadinessFacts
		wantState State
	}{
		{"ready", ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: tools}, StateReady},
		{"daemon unavailable", ReadinessFacts{ActiveModel: testModel, Tools: tools}, StateDaemonUnavailable},
		{"llmman not resolved", ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: noLLMMan}, StateDaemonUnavailable},
		{"model unavailable", ReadinessFacts{DaemonHealthy: true, Tools: tools}, StateModelUnavailable},
		{"goose not installed", ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: noDesktop}, StatePackagesMissing},
		{"server not installed", ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: noServer}, StatePackagesMissing},
		// Set Up is the one thing the Goose row can do itself, so a missing
		// package is reported even while Agent Mode is off.
		{"packages before daemon", ReadinessFacts{Tools: noServer}, StatePackagesMissing},
		{"unsupported architecture", ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: unsupported}, StateUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.facts)
			if got != tt.wantState {
				t.Errorf("Evaluate() = %v, want %v", got, tt.wantState)
			}
			if got.Ready() != (tt.wantState == StateReady) {
				t.Errorf("Ready() = %v for %v", got.Ready(), got)
			}
			if got.CanSetUp() != (tt.wantState == StatePackagesMissing) {
				t.Errorf("CanSetUp() = %v for %v", got.CanSetUp(), got)
			}
		})
	}
}

func TestReadinessSubtitlesAndPrerequisites(t *testing.T) {
	for _, state := range []State{StateReady, StateDaemonUnavailable, StateModelUnavailable, StatePackagesMissing, StateUnsupported} {
		t.Run(state.String(), func(t *testing.T) {
			if state.Subtitle("qwen:test") == "" {
				t.Errorf("Subtitle() for %v is empty", state)
			}
			prereq := state.MissingPrerequisite()
			if (state == StateReady) != (prereq == "") {
				t.Errorf("MissingPrerequisite() for %v = %q", state, prereq)
			}
		})
	}
}

// Readiness reads no Goose configuration: the profile is written at launch,
// so the facts are the packages, the daemon, and the model.
func TestObserveLiveUsesDetectedTools(t *testing.T) {
	origDetect, origDaemon, origModel := detectTools, checkDaemon, checkModel
	t.Cleanup(func() { detectTools, checkDaemon, checkModel = origDetect, origDaemon, origModel })
	detectTools = installedTools
	checkDaemon = func(context.Context) bool { return true }
	checkModel = func(context.Context) (string, error) { return testModel, nil }

	state, facts, err := ObserveLive(context.Background())
	if err != nil || state != StateReady {
		t.Fatalf("ObserveLive() = %v, %v", state, err)
	}
	if facts.ActiveModel != testModel || facts.Tools != installedTools() {
		t.Errorf("facts = %+v", facts)
	}

	checkDaemon = func(context.Context) bool { return false }
	checkModel = func(context.Context) (string, error) {
		t.Error("model read although the daemon is down")
		return "", nil
	}
	if state, _, _ := ObserveLive(context.Background()); state != StateDaemonUnavailable {
		t.Errorf("ObserveLive() with daemon down = %v", state)
	}
}

// launchHost points the profile at a temporary data home and records what
// would be started. finish ends the recorded process.
type launchHost struct {
	profile troubleshoot.Profile
	cmds    []*exec.Cmd
	onExit  func(error)
}

func newLaunchHost(t *testing.T) *launchHost {
	t.Helper()
	dryrun.Set(false)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := &launchHost{profile: troubleshoot.ProfileAt(t.TempDir())}
	origProfile, origRun := profileFor, runCmd
	t.Cleanup(func() {
		profileFor, runCmd = origProfile, origRun
		dryrun.Set(false)
	})
	profileFor = func() (troubleshoot.Profile, error) { return h.profile, nil }
	runCmd = func(cmd *exec.Cmd, onExit func(error)) error {
		h.cmds = append(h.cmds, cmd)
		h.onExit = onExit
		return nil
	}
	return h
}

func readyFacts() ReadinessFacts {
	return ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: installedTools()}
}

// The launch writes ChairLift's own profile first, then runs llmman on the
// Agent Mode alias with Goose's config root and the desktop app's
// XDG_CONFIG_HOME inside that profile.
func TestLaunchWritesTheProfileAndIsolatesTheSession(t *testing.T) {
	h := newLaunchHost(t)
	if err := Launch(context.Background(), readyFacts(), nil); err != nil {
		t.Fatalf("Launch() = %v", err)
	}
	if len(h.cmds) != 1 {
		t.Fatalf("started %d commands, want 1", len(h.cmds))
	}
	cmd := h.cmds[0]
	wantArgs := []string{installedTools().LLMManPath, "launch", "goose-desktop", "--model", aistack.ActiveModelAlias}
	if !slices.Equal(cmd.Args, wantArgs) {
		t.Errorf("args = %q, want %q", cmd.Args, wantArgs)
	}
	for _, want := range []string{
		"GOOSE_PATH_ROOT=" + h.profile.GooseRoot(),
		"XDG_CONFIG_HOME=" + h.profile.DesktopConfigHome(),
	} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("env lacks %q", want)
		}
	}
	config, err := os.ReadFile(h.profile.ConfigPath())
	if err != nil {
		t.Fatalf("profile config not written before launch: %v", err)
	}
	if !strings.Contains(string(config), installedTools().ServerPath) {
		t.Errorf("profile config does not name linux-mcp-server:\n%s", config)
	}
	if _, err := os.Stat(h.profile.HintsPath()); err != nil {
		t.Errorf("profile hints not written: %v", err)
	}
}

// holdLock makes the profile look like a running Goose session: a Chromium
// SingletonLock naming this host and a live process (the test itself).
func holdLock(t *testing.T, profile troubleshoot.Profile) {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(profile.DesktopConfigHome(), "Goose")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fmt.Sprintf("%s-%d", host, os.Getpid()), filepath.Join(dir, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
}

// Ask Bluefin with Goose already open opens Goose the ordinary way: start
// goose-desktop again in the same profile so Goose's own single-instance
// lock hands it to the running session. Going through llmman would be
// refused while the lock is held.
func TestLaunchWithASessionRunningReopensGoose(t *testing.T) {
	h := newLaunchHost(t)
	holdLock(t, h.profile)

	if err := Launch(context.Background(), readyFacts(), nil); err != nil {
		t.Fatalf("Launch() = %v", err)
	}
	if len(h.cmds) != 1 {
		t.Fatalf("started %d commands, want 1", len(h.cmds))
	}
	cmd := h.cmds[0]
	if want := []string{installedTools().DesktopPath}; !slices.Equal(cmd.Args, want) {
		t.Errorf("args = %q, want %q", cmd.Args, want)
	}
	if !slices.Contains(cmd.Env, "XDG_CONFIG_HOME="+h.profile.DesktopConfigHome()) {
		t.Error("the reopen does not run in the profile, so the lock would not hand it over")
	}
	if _, err := os.Stat(h.profile.ConfigPath()); err == nil {
		t.Error("rewrote the profile under a running session")
	}
}

// A lock left behind by a process that is gone is no session: the launch
// starts a fresh one through llmman.
func TestLaunchIgnoresAStaleLock(t *testing.T) {
	h := newLaunchHost(t)
	dir := filepath.Join(h.profile.DesktopConfigHome(), "Goose")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	host, _ := os.Hostname()
	if err := os.Symlink(host+"-2147483646", filepath.Join(dir, "SingletonLock")); err != nil {
		t.Fatal(err)
	}

	if err := Launch(context.Background(), readyFacts(), nil); err != nil {
		t.Fatalf("Launch() = %v", err)
	}
	if len(h.cmds) != 1 || h.cmds[0].Args[0] != installedTools().LLMManPath {
		t.Fatalf("commands = %v, want one llmman launch", h.cmds)
	}
}

func TestLaunchDryRunStartsNothing(t *testing.T) {
	h := newLaunchHost(t)
	dryrun.Set(true)
	if err := Launch(context.Background(), readyFacts(), nil); err != nil {
		t.Fatalf("Launch() in dry-run = %v", err)
	}
	if len(h.cmds) != 0 {
		t.Error("Launch() in dry-run started a process")
	}
	if _, err := os.Stat(h.profile.ConfigPath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry-run wrote the profile: %v", err)
	}
}

func TestLaunchFailed(t *testing.T) {
	t.Run("canceled context", func(t *testing.T) {
		newLaunchHost(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := Launch(ctx, readyFacts(), nil); !errors.Is(err, context.Canceled) {
			t.Errorf("Launch() = %v, want context.Canceled", err)
		}
	})

	t.Run("not ready", func(t *testing.T) {
		h := newLaunchHost(t)
		facts := readyFacts()
		facts.ActiveModel = ""
		err := Launch(context.Background(), facts, nil)
		if err == nil || !strings.Contains(err.Error(), "No model is selected") {
			t.Errorf("Launch() = %v, want the missing prerequisite", err)
		}
		if len(h.cmds) != 0 {
			t.Errorf("an unready launch started %d commands", len(h.cmds))
		}
	})

	t.Run("start error", func(t *testing.T) {
		newLaunchHost(t)
		startFailure := errors.New("cannot spawn llmman")
		runCmd = func(*exec.Cmd, func(error)) error { return startFailure }
		if err := Launch(context.Background(), readyFacts(), nil); !errors.Is(err, startFailure) {
			t.Errorf("Launch() = %v, want %v", err, startFailure)
		}
	})

	t.Run("profile error", func(t *testing.T) {
		newLaunchHost(t)
		profileFor = func() (troubleshoot.Profile, error) { return troubleshoot.Profile{}, errors.New("no home") }
		if err := Launch(context.Background(), readyFacts(), nil); err == nil {
			t.Error("Launch() succeeded without a profile")
		}
	})
}

func TestLaunchChildSurvivesCallerContext(t *testing.T) {
	newLaunchHost(t)
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		sleepBin = "/bin/sleep"
	}

	var capturedCmd *exec.Cmd
	runCmd = func(cmd *exec.Cmd, onExit func(error)) error {
		capturedCmd = cmd
		cmd.Path = sleepBin
		cmd.Args = []string{sleepBin, "2"}
		return launcher.Run(cmd, onExit)
	}

	callerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := Launch(callerCtx, readyFacts(), nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	cancel()

	if capturedCmd == nil || capturedCmd.Process == nil {
		t.Fatal("expected process to be started, got nil Process")
	}
	t.Cleanup(func() { _ = capturedCmd.Process.Kill() })

	time.Sleep(50 * time.Millisecond)
	if err := capturedCmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("child process was killed upon caller context cancellation: %v", err)
	}
}
