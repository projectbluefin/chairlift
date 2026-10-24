package troubleshoot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The lock names "<host>-<pid>". Only a live process on this host owns it;
// anything else is stale, or a launch is handed to a session that is gone.
func TestLockOwnerAcceptsOnlyALiveProcessOnThisHost(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Skipf("cannot start a short-lived process: %v", err)
	}
	cases := map[string]struct {
		target string
		want   bool
	}{
		"this process":           {host + "-" + strconv.Itoa(os.Getpid()), true},
		"a reaped process":       {host + "-" + strconv.Itoa(dead.Process.Pid), false},
		"another host":           {"not-" + host + "-" + strconv.Itoa(os.Getpid()), false},
		"no separator":           {host, false},
		"empty host":             {"-" + strconv.Itoa(os.Getpid()), false},
		"empty target":           {"", false},
		"non-numeric pid":        {host + "-abc", false},
		"zero pid":               {host + "-0", false},
		"negative pid":           {host + "--1", false},
		"pid with trailing junk": {host + "-" + strconv.Itoa(os.Getpid()) + "x", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := lockOwner(tc.target); got != tc.want {
				t.Errorf("lockOwner(%q) = %v, want %v", tc.target, got, tc.want)
			}
		})
	}
}

// A hostname that itself contains hyphens must still split at the last one.
func TestLockOwnerSplitsAtTheLastHyphen(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	target := host + "-" + strconv.Itoa(os.Getpid())
	if !lockOwner(target) {
		t.Errorf("lockOwner(%q) = false for this process", target)
	}
	if lockOwner("x-" + target) {
		t.Errorf("lockOwner accepted a hyphenated host that is not this one")
	}
}

func TestBootTimeIsInThePast(t *testing.T) {
	if _, err := os.Stat("/proc/stat"); err != nil {
		t.Skip("no /proc/stat on this host")
	}
	boot := bootTime()
	if boot.IsZero() {
		t.Fatal("bootTime() is zero on a host with /proc/stat; the reboot check is disabled")
	}
	if !boot.Before(time.Now()) {
		t.Errorf("bootTime() = %v, want a time before now", boot)
	}
}

func TestRunningWithoutALock(t *testing.T) {
	if ProfileAt(t.TempDir()).Running() {
		t.Error("Running() = true for a profile with no lock")
	}
}

func TestRunningNeedsALiveOwner(t *testing.T) {
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
	bootTime = func() time.Time { return time.Time{} }
	var asked string
	lockOwner = func(target string) bool { asked = target; return false }

	if profile.Running() {
		t.Error("Running() = true for a lock whose owner is gone")
	}
	if asked != "host-4242" {
		t.Errorf("lockOwner was asked about %q, want the symlink target", asked)
	}
}

func TestDefaultProfileFollowsXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data/home")
	got, err := DefaultProfile()
	if err != nil {
		t.Fatalf("DefaultProfile: %v", err)
	}
	if want := ProfileAt("/data/home"); got != want {
		t.Errorf("DefaultProfile() = %+v, want %+v", got, want)
	}
}

// The XDG spec says a relative $XDG_DATA_HOME is invalid and must be ignored.
func TestDefaultProfileIgnoresARelativeXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative/data")
	t.Setenv("HOME", "/home/user")
	got, err := DefaultProfile()
	if err != nil {
		t.Fatalf("DefaultProfile: %v", err)
	}
	if want := ProfileAt("/home/user/.local/share"); got != want {
		t.Errorf("DefaultProfile() = %+v, want %+v", got, want)
	}
}

func TestDefaultProfileWithoutAHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")
	if got, err := DefaultProfile(); err == nil {
		t.Errorf("DefaultProfile() = %+v, nil; want an error with no home directory", got)
	}
}

func TestReopenCommandStartsTheDesktopAppInTheProfile(t *testing.T) {
	profile := ProfileAt("/data")
	cmd, err := ReopenCommand(State{DesktopPath: "/opt/goose-desktop"}, profile)
	if err != nil {
		t.Fatalf("ReopenCommand: %v", err)
	}
	if want := []string{"/opt/goose-desktop"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("argv = %v, want %v", cmd.Args, want)
	}
	for _, want := range []string{"GOOSE_PATH_ROOT=" + profile.GooseRoot(), "XDG_CONFIG_HOME=" + profile.DesktopConfigHome()} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("environment lacks %s", want)
		}
	}
}

func TestReopenCommandRefusesAMissingDesktopApp(t *testing.T) {
	if cmd, err := ReopenCommand(State{LLMManPath: "/bin/llmman"}, ProfileAt("/data")); err == nil {
		t.Errorf("ReopenCommand = %v, nil; want an error with no desktop app", cmd.Args)
	}
}

func TestResolvePrefersAnAbsolutePathFromPATH(t *testing.T) {
	fakeHost(t, "amd64", "goose-desktop")
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	if got := resolve("goose-desktop"); got != "/usr/bin/goose-desktop" {
		t.Errorf("resolve() = %q, want the $PATH hit", got)
	}
}

// exec.LookPath can return a relative path when $PATH holds "."; Goose
// would resolve that against its own working directory, so it is ignored.
func TestResolveIgnoresARelativePATHHit(t *testing.T) {
	bin := fakeHost(t, "amd64", "goose-desktop")
	lookPath = func(name string) (string, error) { return "./" + name, nil }

	if got, want := resolve("goose-desktop"), filepath.Join(bin, "goose-desktop"); got != want {
		t.Errorf("resolve() = %q, want %q beside brew", got, want)
	}
}

func TestResolveSkipsADirectoryBesideBrew(t *testing.T) {
	bin := fakeHost(t, "amd64")
	if err := os.Mkdir(filepath.Join(bin, "goose-desktop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolve("goose-desktop"); got != "" {
		t.Errorf("resolve() = %q, want empty for a directory", got)
	}
}

func TestResolveWithoutHomebrew(t *testing.T) {
	fakeHost(t, "amd64")
	brewExecutable = func() string { return "" }
	if got := resolve("goose-desktop"); got != "" {
		t.Errorf("resolve() = %q, want empty with no brew and nothing on $PATH", got)
	}
}

// Progress names only the steps that run, so a resumed install does not
// claim to repeat work that is already done.
func TestSetupReportsOnlyTheStepsItRuns(t *testing.T) {
	fakeHost(t, "amd64", "linux-mcp-server")
	recordBrew(t, "")
	var progress []string

	if _, err := Setup(Detect(), func(name string) { progress = append(progress, name) }); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if want := []string{"Getting ready (1 of 2)", "Installing the Goose app (2 of 2)"}; !reflect.DeepEqual(progress, want) {
		t.Errorf("progress = %v, want %v", progress, want)
	}
}

func TestSetupWithNothingToDo(t *testing.T) {
	fakeHost(t, "amd64", "linux-mcp-server", "goose-desktop")
	calls := recordBrew(t, "")

	state, err := Setup(Detect(), func(name string) { t.Errorf("progress(%q) on an installed host", name) })
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("ran brew on an installed host: %v", *calls)
	}
	if !state.Installed() {
		t.Errorf("Setup returned %+v, want installed", state)
	}
}

func TestSetupStopsWhenTheTrustFails(t *testing.T) {
	fakeHost(t, "amd64")
	calls := recordBrew(t, "")
	trustPackage = func(name string, cask bool) error {
		*calls = append(*calls, "trust "+name)
		return errors.New("untrusted")
	}

	_, err := Setup(Detect(), nil)
	if err == nil || !strings.Contains(err.Error(), "installing goose's troubleshooting tools") {
		t.Fatalf("Setup error = %v, want it to name the tools step", err)
	}
	if slices.Contains(*calls, "install "+ServerFormula) {
		t.Errorf("installed a package Homebrew was not told to trust: %v", *calls)
	}
}

func TestProfileWriteReportsAWriteFailure(t *testing.T) {
	withUserConfig(t, t.TempDir())
	prev := writeFile
	t.Cleanup(func() { writeFile = prev })
	writeFile = func(string, []byte) error { return errors.New("disk full") }

	if err := ProfileAt(t.TempDir()).Write("/opt/linux-mcp-server"); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("Write error = %v, want the write failure", err)
	}
}

func TestProfileWriteReportsAnUnreadableConfig(t *testing.T) {
	withUserConfig(t, t.TempDir())
	profile := ProfileAt(t.TempDir())
	if err := os.MkdirAll(profile.ConfigPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := profile.Write("/opt/linux-mcp-server"); err == nil {
		t.Error("Write succeeded over a config path it cannot read")
	}
}

func TestProfileWriteRejectsARelativeServer(t *testing.T) {
	withUserConfig(t, t.TempDir())
	data := t.TempDir()
	if err := ProfileAt(data).Write("linux-mcp-server"); err == nil {
		t.Error("Write accepted a relative linux-mcp-server path")
	}
	if entries, _ := os.ReadDir(data); len(entries) != 0 {
		t.Errorf("a rejected Write left %v behind", entries)
	}
}

// Without a user config directory there is nothing to link, and the profile
// write itself still succeeds.
func TestProfileWriteWithoutAUserConfigDir(t *testing.T) {
	prev := userConfigDir
	t.Cleanup(func() { userConfigDir = prev })
	userConfigDir = func() (string, error) { return "", errors.New("no config dir") }
	profile := ProfileAt(t.TempDir())

	if err := profile.Write("/opt/linux-mcp-server"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(profile.DesktopConfigHome()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("desktop config home exists (%v); nothing should have been linked", err)
	}
}

func TestLinkDesktopSettingsReportsAnUncreatableConfigHome(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Profile{Root: root}).linkDesktopSettings(t.TempDir()); err == nil {
		t.Error("linkDesktopSettings succeeded under a regular file")
	}
}

func TestWriteAtomicReplacesTheFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "nested", "config.yaml")
	for _, body := range []string{"first", "second"} {
		if err := writeAtomic(dest, []byte(body)); err != nil {
			t.Fatalf("writeAtomic(%q): %v", body, err)
		}
		if got, _ := os.ReadFile(dest); string(got) != body {
			t.Errorf("dest = %q, want %q", got, body)
		}
	}
	info, err := os.Stat(filepath.Dir(dest))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("created directory mode = %o, want 700", perm)
	}
}

// A failed rename must not leave the temporary file in the profile.
func TestWriteAtomicCleansUpAfterAFailedRename(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "config.yaml")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeAtomic(dest, []byte("data")); err == nil {
		t.Fatal("writeAtomic replaced a non-empty directory")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".troubleshoot-*"))
	if len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

func TestWriteAtomicReportsAnUncreatableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filepath.Join(file, "config.yaml"), []byte("data")); err == nil {
		t.Error("writeAtomic succeeded under a regular file")
	}
}
