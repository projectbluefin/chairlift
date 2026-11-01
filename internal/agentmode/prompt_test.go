package agentmode

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

const testPrompt = "Bluefin 42: 2 failed units, 3 warnings. Inspect them with the read-only tools."

// holdTestPrompt holds prompt for one test and drops it afterwards, so no
// prompt leaks into another test's launch.
func holdTestPrompt(t *testing.T, prompt string) {
	t.Helper()
	HoldPrompt(prompt)
	t.Cleanup(func() { HoldPrompt("") })
}

func writePromptFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadPromptFile(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		got, err := ReadPromptFile(writePromptFile(t, []byte("\r\n  "+testPrompt+"\r\nThanks\x1b[31m\x00 \n")))
		if err != nil {
			t.Fatalf("ReadPromptFile() = %v", err)
		}
		if want := testPrompt + "\nThanks[31m"; got != want {
			t.Errorf("ReadPromptFile() = %q, want %q", got, want)
		}
	})

	t.Run("at the bound", func(t *testing.T) {
		data := []byte(strings.Repeat("a", MaxPromptBytes))
		if got, err := ReadPromptFile(writePromptFile(t, data)); err != nil || len(got) != MaxPromptBytes {
			t.Errorf("ReadPromptFile() = %d bytes, %v; want %d bytes", len(got), err, MaxPromptBytes)
		}
	})

	t.Run("refused", func(t *testing.T) {
		dir := t.TempDir()
		fifo := filepath.Join(dir, "fifo")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link")
		if err := os.Symlink(fifo, link); err != nil {
			t.Fatal(err)
		}
		tests := map[string]struct {
			path string
			want error
		}{
			"no path":     {path: ""},
			"missing":     {path: filepath.Join(dir, "missing"), want: os.ErrNotExist},
			"directory":   {path: dir},
			"fifo":        {path: fifo},
			"link a fifo": {path: link},
			"too large":   {path: writePromptFile(t, []byte(strings.Repeat("a", MaxPromptBytes+1))), want: ErrPromptTooLarge},
			"blank":       {path: writePromptFile(t, []byte(" \n\t\r\n")), want: ErrPromptEmpty},
			"binary":      {path: writePromptFile(t, []byte{0xff, 0xfe, 'h', 'i'}), want: ErrPromptNotText},
		}
		for name, tc := range tests {
			t.Run(name, func(t *testing.T) {
				got, err := ReadPromptFile(tc.path)
				if err == nil {
					t.Fatalf("ReadPromptFile(%q) = %q, nil; want an error", tc.path, got)
				}
				if tc.want != nil && !errors.Is(err, tc.want) {
					t.Errorf("ReadPromptFile(%q) = %v, want %v", tc.path, err, tc.want)
				}
			})
		}
	})
}

func TestResolvePromptPath(t *testing.T) {
	tests := []struct{ path, cwd, want string }{
		{"", "/home/u", ""},
		{"/run/user/1000/prompt", "/home/u", "/run/user/1000/prompt"},
		{"prompt.txt", "/home/u", "/home/u/prompt.txt"},
		{"../p", "/home/u/dir", "/home/u/p"},
		{"prompt.txt", "", "prompt.txt"},
	}
	for _, tc := range tests {
		if got := ResolvePromptPath(tc.path, tc.cwd); got != tc.want {
			t.Errorf("ResolvePromptPath(%q, %q) = %q, want %q", tc.path, tc.cwd, got, tc.want)
		}
	}
}

func TestHeldPromptReason(t *testing.T) {
	if got := HeldPromptReason(""); got != PromptHeldNote {
		t.Errorf("HeldPromptReason(\"\") = %q, want %q", got, PromptHeldNote)
	}
	reason := StatePackagesMissing.MissingPrerequisite()
	if got, want := HeldPromptReason(reason), reason+" "+PromptHeldNote; got != want {
		t.Errorf("HeldPromptReason(%q) = %q, want %q", reason, got, want)
	}
}

// The prompt is the user's text: a dry-run log shows the link it travels in
// but not what it says.
func TestCommandForLogRedactsThePrompt(t *testing.T) {
	args := []string{"/bin/llmman", "launch", "goose-desktop", "--", troubleshoot.NewSessionURL(testPrompt)}
	got := commandForLog(args)
	if strings.Contains(got, "failed") || strings.Contains(got, "units") {
		t.Errorf("commandForLog() = %q, shows the prompt", got)
	}
	if want := "/bin/llmman launch goose-desktop -- goose://new-session?prompt=<redacted>"; got != want {
		t.Errorf("commandForLog() = %q, want %q", got, want)
	}
}

// Ready: a fresh session opens with the held prompt prefilled, and the
// prompt is let go once delivered.
func TestLaunchOpensAFreshSessionWithTheHeldPrompt(t *testing.T) {
	h := newLaunchHost(t)
	holdTestPrompt(t, testPrompt)

	if _, err := Launch(context.Background(), readyFacts(), nil); err != nil {
		t.Fatalf("Launch() = %v", err)
	}
	if len(h.cmds) != 1 {
		t.Fatalf("started %d commands, want 1", len(h.cmds))
	}
	want := []string{installedTools().LLMManPath, "launch", "goose-desktop", "--model", aistack.ActiveModelAlias, "--", troubleshoot.NewSessionURL(testPrompt)}
	if !slices.Equal(h.cmds[0].Args, want) {
		t.Errorf("args = %q, want %q", h.cmds[0].Args, want)
	}
	if got := PendingPrompt(); got != "" {
		t.Errorf("PendingPrompt() after launch = %q, want none", got)
	}

	// The next launch is an ordinary one.
	h.cmds = nil
	if _, err := Launch(context.Background(), readyFacts(), nil); err != nil {
		t.Fatalf("second Launch() = %v", err)
	}
	if len(h.cmds) != 1 || slices.Contains(h.cmds[0].Args, "--") {
		t.Errorf("second launch = %v, want no prompt", h.cmds)
	}
}

// Running session: the prompt goes to Goose's single-instance hand-off as a
// new-session link, which the running session opens as a prefilled chat.
func TestLaunchHandsTheHeldPromptToARunningSession(t *testing.T) {
	h := newLaunchHost(t)
	holdLock(t, h.profile)
	holdTestPrompt(t, testPrompt)

	result, err := Launch(context.Background(), readyFacts(), nil)
	if err != nil {
		t.Fatalf("Launch() = %v", err)
	}
	if result != LaunchHandedOff {
		t.Errorf("Launch() result = %v, want LaunchHandedOff", result)
	}
	if len(h.cmds) != 1 {
		t.Fatalf("started %d commands, want 1", len(h.cmds))
	}
	if want := []string{installedTools().DesktopPath, troubleshoot.NewSessionURL(testPrompt)}; !slices.Equal(h.cmds[0].Args, want) {
		t.Errorf("args = %q, want %q", h.cmds[0].Args, want)
	}
	if got := PendingPrompt(); got != "" {
		t.Errorf("PendingPrompt() after hand-off = %q, want none", got)
	}
}

// Not ready: nothing starts and the prompt stays held, so the first launch
// after the Agents setup flow still opens with it.
func TestLaunchKeepsTheHeldPromptUntilGooseIsReady(t *testing.T) {
	h := newLaunchHost(t)
	holdTestPrompt(t, testPrompt)

	unready := readyFacts()
	unready.ActiveModel = ""
	if _, err := Launch(context.Background(), unready, nil); err == nil {
		t.Fatal("Launch() succeeded while not ready")
	}
	if len(h.cmds) != 0 {
		t.Fatalf("an unready launch started %d commands", len(h.cmds))
	}
	if got := PendingPrompt(); got != testPrompt {
		t.Fatalf("PendingPrompt() after an unready launch = %q, want it kept", got)
	}

	if _, err := Launch(context.Background(), readyFacts(), nil); err != nil {
		t.Fatalf("Launch() once ready = %v", err)
	}
	if len(h.cmds) != 1 || !slices.Contains(h.cmds[0].Args, troubleshoot.NewSessionURL(testPrompt)) {
		t.Errorf("launch once ready = %v, want the held prompt", h.cmds)
	}
}

// A launch that fails to start keeps the prompt for the next try.
func TestLaunchKeepsTheHeldPromptWhenTheStartFails(t *testing.T) {
	newLaunchHost(t)
	holdTestPrompt(t, testPrompt)
	runCmd = func(*exec.Cmd, func(error)) error { return errors.New("cannot spawn") }

	if _, err := Launch(context.Background(), readyFacts(), nil); err == nil {
		t.Fatal("Launch() succeeded although the start failed")
	}
	if got := PendingPrompt(); got != testPrompt {
		t.Errorf("PendingPrompt() after a failed start = %q, want it kept", got)
	}
}

// A newer --ask-bluefin prompt that arrives while a launch delivers an older
// one is not dropped with it.
func TestReleasePromptKeepsANewerPrompt(t *testing.T) {
	holdTestPrompt(t, "newer")
	releasePrompt("older")
	if got := PendingPrompt(); got != "newer" {
		t.Errorf("PendingPrompt() = %q, want the newer prompt kept", got)
	}
	releasePrompt("newer")
	if got := PendingPrompt(); got != "" {
		t.Errorf("PendingPrompt() = %q, want none", got)
	}
}

func TestLaunchDryRunDeliversThePromptWithoutStarting(t *testing.T) {
	h := newLaunchHost(t)
	dryrun.Set(true)
	holdTestPrompt(t, testPrompt)

	if _, err := Launch(context.Background(), readyFacts(), nil); err != nil {
		t.Fatalf("Launch() in dry-run = %v", err)
	}
	if len(h.cmds) != 0 {
		t.Error("Launch() in dry-run started a process")
	}
	if got := PendingPrompt(); got != "" {
		t.Errorf("PendingPrompt() after a dry-run launch = %q, want none", got)
	}
}
