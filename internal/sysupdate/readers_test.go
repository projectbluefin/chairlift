package sysupdate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The state-file paths are a contract with snosi's stager, which writes them
// from root while this process only reads them. Nothing else in the tree
// re-states the literals, so a typo here is invisible until the UI silently
// reports "no check has run this boot" forever.
func TestStateFilePathsAreTheSnosiContract(t *testing.T) {
	if UpdateCheckPath != "/run/snosi/update-check" {
		t.Errorf("UpdateCheckPath = %q, want /run/snosi/update-check", UpdateCheckPath)
	}
	if StagedSemaphorePath != "/run/snosi/update-staged" {
		t.Errorf("StagedSemaphorePath = %q, want /run/snosi/update-staged", StagedSemaphorePath)
	}
}

// An unreadable — as opposed to absent — state file is a real error and must
// be reported rather than mistaken for "nothing staged". A directory at the
// file's path is the portable way to provoke it without depending on running
// as an unprivileged user (root can read a 0o000 file).
func TestReadersDistinguishUnreadableFromAbsent(t *testing.T) {
	dir := t.TempDir()
	notAFile := filepath.Join(dir, "occupied")
	if err := os.Mkdir(notAFile, 0o755); err != nil {
		t.Fatal(err)
	}

	check, err := readUpdateCheckFrom(notAFile)
	if err == nil {
		t.Errorf("readUpdateCheckFrom(directory) = (%+v, nil), want an error", check)
	}
	if check != nil {
		t.Errorf("readUpdateCheckFrom(directory) value = %+v, want nil", check)
	}

	staged, err := readStagedUpdateFrom(notAFile)
	if err == nil {
		t.Errorf("readStagedUpdateFrom(directory) = (%+v, nil), want an error", staged)
	}
	if staged != nil {
		t.Errorf("readStagedUpdateFrom(directory) value = %+v, want nil", staged)
	}
}

// The semaphore's presence is the reboot-pending signal; its contents are
// advisory. An empty or unparseable file still means "staged".
func TestReadStagedUpdateFromTolerantContents(t *testing.T) {
	tests := []struct {
		name string
		data string
		want StagedUpdate
	}{
		{name: "empty file is still a semaphore", data: ""},
		{name: "no key=value lines", data: "garbage\nmore garbage\n"},
		{
			name: "bootc digest shape",
			data: "image=ghcr.io/projectbluefin/snow\ndigest=sha256:0123456789abcdef0123\nstaged_at=2026-08-10T20:08:03-06:00\n",
			want: StagedUpdate{
				Image:    "ghcr.io/projectbluefin/snow",
				Digest:   "sha256:0123456789abcdef0123",
				StagedAt: "2026-08-10T20:08:03-06:00",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "update-staged")
			writeFile(t, path, tc.data)

			staged, err := readStagedUpdateFrom(path)
			if err != nil {
				t.Fatalf("readStagedUpdateFrom: %v", err)
			}
			if staged == nil {
				t.Fatal("readStagedUpdateFrom(present file) = nil, want a value: the file's presence is the signal")
			}
			if *staged != tc.want {
				t.Errorf("readStagedUpdateFrom = %+v, want %+v", *staged, tc.want)
			}
			if !(Status{Staged: staged}).IsStaged() {
				t.Error("IsStaged() = false for a present semaphore")
			}
		})
	}
}

// DisplayVersion is what the staged row renders. The nil receiver is a real
// state — Status.Staged is nil whenever the semaphore is absent — and a
// digest shorter than the 12-character elision must survive intact.
func TestDisplayVersionEdges(t *testing.T) {
	var absent *StagedUpdate
	if got := absent.DisplayVersion(); got != "" {
		t.Errorf("(*StagedUpdate)(nil).DisplayVersion() = %q, want \"\"", got)
	}

	tests := []struct {
		name   string
		staged StagedUpdate
		want   string
	}{
		{name: "short digest is not truncated", staged: StagedUpdate{Digest: "sha256:abc123"}, want: "abc123"},
		{name: "exactly twelve is not truncated", staged: StagedUpdate{Digest: "sha256:0123456789ab"}, want: "0123456789ab"},
		{name: "invalid version falls through to digest", staged: StagedUpdate{Version: "not-a-version", Digest: "sha256:0123456789abcdef"}, want: "0123456789ab"},
		{name: "neither version nor digest", staged: StagedUpdate{Image: "snow-ab"}, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.staged.DisplayVersion(); got != tc.want {
				t.Errorf("DisplayVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeLsblk puts an `lsblk` on $PATH that records its argv and emits body on
// stdout, exiting with status. It returns the argv-capture file's path.
func fakeLsblk(t *testing.T, body string, status int) string {
	t.Helper()
	dir := t.TempDir()
	if strings.Contains(body, "'") {
		t.Fatalf("fake lsblk body may not contain a single quote: %q", body)
	}
	argvPath := filepath.Join(dir, "argv")
	// Builtins only: $PATH is replaced wholesale so the fake is the only
	// lsblk reachable, which also removes cat and every other coreutil.
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + argvPath + "\n" +
		"printf '%s\\n' '" + body + "'\n" +
		"exit " + strconv.Itoa(status) + "\n"
	if err := os.WriteFile(filepath.Join(dir, lsblkCommand), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return argvPath
}

func TestRunLsblkArgvAndOutput(t *testing.T) {
	argvPath := fakeLsblk(t, `{"blockdevices":[]}`, 0)

	output, err := runLsblk(context.Background())
	if err != nil {
		t.Fatalf("runLsblk: %v", err)
	}
	if strings.TrimSpace(string(output)) != `{"blockdevices":[]}` {
		t.Errorf("runLsblk stdout = %q, want the lsblk JSON verbatim", output)
	}

	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("reading recorded argv: %v", err)
	}
	// Slot discovery depends on PARTLABEL being requested in JSON form;
	// dropping either flag makes every host look like it has no other slot.
	if got := strings.TrimSpace(string(argv)); got != "-J -o PATH,PARTLABEL" {
		t.Errorf("lsblk argv = %q, want %q", got, "-J -o PATH,PARTLABEL")
	}
}

func TestRunLsblkFailureIsASysupdateError(t *testing.T) {
	fakeLsblk(t, "", 1)

	output, err := runLsblk(context.Background())
	if err == nil {
		t.Fatalf("runLsblk with a failing lsblk = (%q, nil), want an error", output)
	}
	if output != nil {
		t.Errorf("runLsblk output on failure = %q, want nil", output)
	}

	var sysErr *Error
	if !errors.As(err, &sysErr) {
		t.Fatalf("runLsblk error = %T, want *sysupdate.Error", err)
	}
	if !strings.HasPrefix(sysErr.Message, "lsblk failed:") {
		t.Errorf("error message = %q, want an \"lsblk failed:\" prefix", sysErr.Message)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("wrapped error = %v, want the exec.ExitError preserved for errors.As", sysErr.Err)
	}
}

func TestRunLsblkMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if _, err := runLsblk(context.Background()); err == nil {
		t.Fatal("runLsblk with no lsblk on PATH = nil error, want an error")
	}
}

// A failing lsblk must degrade to "no rollback available" rather than
// propagate: the updates page renders the result without an error path.
func TestRollbackVersionDegradesWhenLsblkFails(t *testing.T) {
	fakeLsblk(t, "", 1)

	if version, ok := RollbackVersion(context.Background()); ok || version != "" {
		t.Errorf("RollbackVersion() with a failing lsblk = (%q, %v), want (\"\", false)", version, ok)
	}
}
