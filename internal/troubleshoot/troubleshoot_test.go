package troubleshoot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"gopkg.in/yaml.v3"
)

// fakeHost points detection at a temporary Homebrew bin holding the named
// executables, with nothing on $PATH.
func fakeHost(t *testing.T, arch string, present ...string) string {
	t.Helper()
	bin := t.TempDir()
	for _, name := range present {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prevLook, prevBrew, prevArch := lookPath, brewExecutable, goarch
	t.Cleanup(func() { lookPath, brewExecutable, goarch = prevLook, prevBrew, prevArch })
	lookPath = func(string) (string, error) { return "", errors.New("not on PATH") }
	brewExecutable = func() string { return filepath.Join(bin, "brew") }
	goarch = arch
	return bin
}

// recordBrew replaces the Homebrew seams and returns what they were asked
// to do, in order.
func recordBrew(t *testing.T, fail string) *[]string {
	t.Helper()
	prevTap, prevInstall, prevTrust := tapPackage, installPackage, trustPackage
	t.Cleanup(func() { tapPackage, installPackage, trustPackage = prevTap, prevInstall, prevTrust })
	var calls []string
	tapPackage = func(name string) error {
		calls = append(calls, "tap "+name)
		return nil
	}
	trustPackage = func(name string, cask bool) error {
		call := "trust --formula " + name
		if cask {
			call = "trust --cask " + name
		}
		calls = append(calls, call)
		return nil
	}
	installPackage = func(name string, cask bool) error {
		call := "install " + name
		if cask {
			call = "install --cask " + name
		}
		calls = append(calls, call)
		if name == fail {
			return errors.New("boom")
		}
		return nil
	}
	return &calls
}

// A direct launch has no Homebrew bin on $PATH; the pieces must still be
// found beside brew, or an installed feature reads as missing.
func TestDetectFindsPackagesBesideBrew(t *testing.T) {
	bin := fakeHost(t, "amd64", "linux-mcp-server", "goose-desktop", "llmman")

	state := Detect()

	if state.ServerPath != filepath.Join(bin, "linux-mcp-server") || state.DesktopPath != filepath.Join(bin, "goose-desktop") {
		t.Fatalf("Detect() = %+v, want both packages resolved beside brew", state)
	}
	if !state.Ready() {
		t.Errorf("Ready() = false for a host with every piece: %+v", state)
	}
}

func TestReadyNeedsEveryPiece(t *testing.T) {
	full := State{Supported: true, ServerPath: "/s", DesktopPath: "/d", LLMManPath: "/l"}
	cases := map[string]func(*State){
		"no server":      func(s *State) { s.ServerPath = "" },
		"no desktop app": func(s *State) { s.DesktopPath = "" },
		"no llmman":      func(s *State) { s.LLMManPath = "" },
		"arm64":          func(s *State) { s.Supported = false },
	}
	if !full.Ready() {
		t.Fatal("Ready() = false with every piece present")
	}
	for name, strip := range cases {
		t.Run(name, func(t *testing.T) {
			state := full
			strip(&state)
			if state.Ready() {
				t.Errorf("Ready() = true for %+v", state)
			}
		})
	}
}

// The desktop cask pipes its RPM into cpio, which it does not declare and
// Bluefin does not ship: cpio must be installed first, every time the cask is.
// Each ublue-os/tap package is trusted right before its install, or Homebrew
// refuses it from the untrusted tap (#482).
func TestSetupInstallsTheExtractorBeforeTheCask(t *testing.T) {
	fakeHost(t, "amd64")
	calls := recordBrew(t, "")

	if _, err := Setup(Detect(), nil); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	want := []string{
		"tap " + Tap,
		"trust --formula " + ServerFormula,
		"install " + ServerFormula,
		"install " + ExtractorFormula,
		"trust --cask " + DesktopCask,
		"install --cask " + DesktopCask,
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("brew calls = %v, want %v", *calls, want)
	}
}

func TestSetupResumesAHalfDoneInstall(t *testing.T) {
	fakeHost(t, "amd64", "linux-mcp-server")
	calls := recordBrew(t, "")

	if _, err := Setup(Detect(), nil); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if slices.Contains(*calls, "install "+ServerFormula) {
		t.Errorf("reinstalled the server that was already present: %v", *calls)
	}
	if !slices.Contains(*calls, "install --cask "+DesktopCask) {
		t.Errorf("did not install the missing desktop app: %v", *calls)
	}
}

func TestSetupRefusesAnUnsupportedArchitecture(t *testing.T) {
	fakeHost(t, "arm64")
	calls := recordBrew(t, "")

	_, err := Setup(Detect(), nil)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Setup error = %v, want ErrUnsupported", err)
	}
	if len(*calls) != 0 {
		t.Errorf("ran brew on a host the app does not exist for: %v", *calls)
	}
}

func TestSetupNamesTheStepThatFailed(t *testing.T) {
	fakeHost(t, "amd64", "linux-mcp-server")
	recordBrew(t, ExtractorFormula)

	_, err := Setup(Detect(), nil)
	if err == nil || !strings.Contains(err.Error(), "installing the goose app") {
		t.Fatalf("Setup error = %v, want it to name the Goose app step", err)
	}
}

// The profile is the whole tool surface: read-only local inspection and the
// knowledge search, nothing a model could use to change the machine, and no
// provider — llmman supplies that through the environment.
func TestRenderConfigIsTheCompleteReadOnlyToolSet(t *testing.T) {
	data, err := RenderConfig("/home/linuxbrew/.linuxbrew/bin/linux-mcp-server", nil)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("rendered config is not YAML: %v", err)
	}
	for _, key := range []string{"GOOSE_PROVIDER", "GOOSE_MODEL", "active_provider", "providers"} {
		if _, ok := cfg[key]; ok {
			t.Errorf("profile config sets %s; the provider must come from llmman", key)
		}
	}
	exts, _ := cfg["extensions"].(map[string]any)
	var enabled []string
	for name, raw := range exts {
		if ext, _ := raw.(map[string]any); ext["enabled"] == true {
			enabled = append(enabled, name)
		}
	}
	slices.Sort(enabled)
	if !reflect.DeepEqual(enabled, []string{"bluefin-knowledge", "linux-tools"}) {
		t.Fatalf("enabled extensions = %v, want exactly linux-tools and bluefin-knowledge", enabled)
	}
	// Goose adds a missing platform extension with its own default, and
	// these two default to on: developer runs shell commands, and the
	// extension manager could turn developer back on. They must be present
	// and off, not merely absent.
	for _, name := range []string{"developer", "extensionmanager"} {
		ext, ok := exts[name].(map[string]any)
		if !ok || ext["enabled"] != false {
			t.Errorf("%s = %v, want present and disabled", name, exts[name])
		}
	}
	tools, _ := exts["linux-tools"].(map[string]any)
	args := strings.Join(toStrings(tools["args"]), " ")
	for _, want := range []string{"--toolset FIXED", "--host-mode LOCAL_ONLY"} {
		if !strings.Contains(args, want) {
			t.Errorf("linux-tools args %q lack %q", args, want)
		}
	}
	knowledge, _ := exts["bluefin-knowledge"].(map[string]any)
	if knowledge["uri"] != KnowledgeURI || knowledge["type"] != "streamable_http" {
		t.Errorf("bluefin-knowledge = %v", knowledge)
	}
	if got := toStrings(knowledge["available_tools"]); !reflect.DeepEqual(got, []string{"search_knowledge"}) {
		t.Errorf("knowledge tools = %v, want only search_knowledge", got)
	}
}

func TestRenderConfigRejectsARelativeServer(t *testing.T) {
	if _, err := RenderConfig("linux-mcp-server", nil); err == nil {
		t.Fatal("RenderConfig accepted a bare command name; Goose would resolve it against its own PATH")
	}
}

// A later Goose may add a platform extension this package has never heard
// of, enabled by default, and even turn one of ours off. The next launch
// must disable the stranger and keep the two tools on.
func TestRenderConfigDisablesExtensionsGooseAddedSince(t *testing.T) {
	current := []byte(`extensions:
  newshell:
    enabled: true
    type: platform
    name: newshell
  computercontroller:
    enabled: true
    type: builtin
    name: computercontroller
  linux-tools:
    enabled: false
    type: stdio
    name: linux-tools
    cmd: /old/linux-mcp-server
`)
	data, err := RenderConfig("/new/linux-mcp-server", current)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	var cfg struct {
		Extensions map[string]map[string]any `yaml:"extensions"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"newshell", "computercontroller"} {
		ext := cfg.Extensions[name]
		if ext == nil || ext["enabled"] != false || ext["type"] == nil {
			t.Errorf("%s = %v, want kept with its type and disabled", name, ext)
		}
	}
	tools := cfg.Extensions["linux-tools"]
	if tools["enabled"] != true || tools["cmd"] != "/new/linux-mcp-server" {
		t.Errorf("linux-tools = %v, want re-enabled at the current path", tools)
	}
}

func toStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

// withUserConfig points the user's config directory at dir for one test.
func withUserConfig(t *testing.T, dir string) {
	t.Helper()
	prev := userConfigDir
	t.Cleanup(func() { userConfigDir = prev })
	userConfigDir = func() (string, error) { return dir, nil }
}

func TestProfileWriteStaysInsideTheProfile(t *testing.T) {
	withUserConfig(t, t.TempDir())
	data := t.TempDir()
	profile := ProfileAt(data)

	if err := profile.Write("/opt/linux-mcp-server"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var written []string
	_ = filepath.WalkDir(data, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			written = append(written, path)
		}
		return nil
	})
	want := []string{profile.HintsPath(), profile.ConfigPath()}
	slices.Sort(written)
	slices.Sort(want)
	if !reflect.DeepEqual(written, want) {
		t.Errorf("wrote %v, want exactly %v", written, want)
	}
	if got, _ := os.ReadFile(profile.HintsPath()); string(got) != Hints() {
		t.Error("hints file does not hold the session instructions")
	}
}

// The session instructions have to push small local models (Qwen3-8B by
// default) to reach for the read-only linux-tools tools instead of answering
// from training. The hint names the most common queries that get hallucinated
// (hostname, kernel) with the matching tool name, and forbids inventing
// system facts that the tool would have returned. Issue #523.
func TestHintsDirectTheModelToCallLinuxTools(t *testing.T) {
	hint := Hints()
	mustContain := []string{
		// Imperative language: a hint that says "look before you answer"
		// is easy for a small model to skim over.
		"You MUST inspect",
		"get_system_information",
		"get_disk_usage",
		// The failure mode in the issue: the model invents the answer
		// from training data instead of calling the tool.
		"Do not invent",
		"Do not answer",
		// Tool names the model could forget exist; the hint has to call
		// them out by name so they are advertised in the prompt.
		"linux-tools",
		"search_knowledge",
	}
	for _, fragment := range mustContain {
		if !strings.Contains(hint, fragment) {
			t.Errorf("session hint does not contain %q", fragment)
		}
	}
}

// Moving XDG_CONFIG_HOME isolates Goose's Electron profile but would also
// hide the user's default browser and dconf from the session; both are
// linked back, and a name already in the profile is never replaced.
func TestProfileWriteLinksTheUsersDesktopSettings(t *testing.T) {
	user := t.TempDir()
	if err := os.WriteFile(filepath.Join(user, "mimeapps.list"), []byte("[Default Applications]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(user, "dconf"), 0o700); err != nil {
		t.Fatal(err)
	}
	withUserConfig(t, user)
	profile := ProfileAt(t.TempDir())
	own := filepath.Join(profile.DesktopConfigHome(), "dconf")
	if err := os.MkdirAll(own, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := profile.Write("/opt/linux-mcp-server"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got, err := os.Readlink(filepath.Join(profile.DesktopConfigHome(), "mimeapps.list")); err != nil || got != filepath.Join(user, "mimeapps.list") {
		t.Errorf("mimeapps.list link = %q, %v; want the user's file", got, err)
	}
	if info, err := os.Lstat(own); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("an existing dconf directory in the profile was replaced: %v", err)
	}
}

func TestProfileWriteInDryRunWritesNothing(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	data := t.TempDir()

	if err := ProfileAt(data).Write("/opt/linux-mcp-server"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, _ := os.ReadDir(data)
	if len(entries) != 0 {
		t.Errorf("dry-run wrote %v", entries)
	}
}

// The launch must isolate both halves of Goose: its own config root, and
// the desktop app's Electron profile, whose single-instance lock would
// otherwise hand this launch to a Goose window the user already has open.
func TestCommandIsolatesTheSessionAndWrapsItInLLMMan(t *testing.T) {
	state := State{Supported: true, ServerPath: "/s", DesktopPath: "/d", LLMManPath: "/bin/llmman"}
	profile := ProfileAt("/data")

	cmd, err := Command(state, profile, "bluefin-active")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if want := []string{"/bin/llmman", "launch", "goose-desktop", "--model", "bluefin-active"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("argv = %v, want %v", cmd.Args, want)
	}
	for _, want := range []string{"GOOSE_PATH_ROOT=" + profile.GooseRoot(), "XDG_CONFIG_HOME=" + profile.DesktopConfigHome()} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("environment lacks %s", want)
		}
	}
	if strings.HasPrefix(profile.GooseRoot(), profile.DesktopConfigHome()) || strings.HasPrefix(profile.DesktopConfigHome(), profile.GooseRoot()) {
		t.Error("Goose root and desktop profile overlap")
	}
}

func TestCommandRefusesAnIncompleteSetup(t *testing.T) {
	ready := State{Supported: true, ServerPath: "/s", DesktopPath: "/d", LLMManPath: "/bin/llmman"}
	if _, err := Command(State{}, ProfileAt("/data"), "bluefin-active"); err == nil {
		t.Error("Command succeeded with nothing installed")
	}
	if _, err := Command(ready, ProfileAt("/data"), ""); err == nil {
		t.Error("Command succeeded with no model")
	}
}

// Goose's own output is the only place a failed start says why (#544: Mesa
// and Electron errors on a VM without 3D acceleration). Both launches must
// hand it to ChairLift's stderr, which the session journals, not /dev/null.
func TestLaunchCommandsKeepGooseOutput(t *testing.T) {
	state := State{Supported: true, ServerPath: "/s", DesktopPath: "/d", LLMManPath: "/bin/llmman"}
	profile := ProfileAt("/data")
	fresh, err := Command(state, profile, "bluefin-active")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	reopen, err := ReopenCommand(state, profile)
	if err != nil {
		t.Fatalf("ReopenCommand: %v", err)
	}
	for name, cmd := range map[string]*exec.Cmd{"Command": fresh, "ReopenCommand": reopen} {
		if cmd.Stdout != os.Stderr || cmd.Stderr != os.Stderr {
			t.Errorf("%s output = (%v, %v), want both os.Stderr", name, cmd.Stdout, cmd.Stderr)
		}
	}
}

// TestRunningIgnoresALockFromBeforeBoot pins the reboot case: the profile
// keeps a lock Goose never removed, and its pid may now name an unrelated
// live process. Handing the launch to it would skip the profile write and
// llmman's environment, so only a lock written during this boot counts.
func TestRunningIgnoresALockFromBeforeBoot(t *testing.T) {
	profile := ProfileAt(t.TempDir())
	dir := filepath.Join(profile.DesktopConfigHome(), "Goose")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("host-4242", filepath.Join(dir, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	oldOwner, oldBoot := lockOwner, bootTime
	t.Cleanup(func() { lockOwner, bootTime = oldOwner, oldBoot })
	lockOwner = func(string) bool { return true }

	bootTime = func() time.Time { return time.Now().Add(-time.Hour) }
	if !profile.Running() {
		t.Error("Running() = false for a live lock written this boot")
	}
	bootTime = func() time.Time { return time.Now().Add(time.Hour) }
	if profile.Running() {
		t.Error("Running() = true for a lock written before this boot")
	}
}
