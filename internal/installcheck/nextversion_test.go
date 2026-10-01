package installcheck

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// nextVersion runs scripts/next-version.sh inside a throwaway git repository
// holding exactly tags, with the calendar slot pinned to 26.10 so the answer
// does not depend on today's date.
func nextVersion(t *testing.T, tags []string, extraArgs ...string) (string, error) {
	t.Helper()
	for _, tool := range []string{"git", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed; skipping next-version.sh check", tool)
		}
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "root")
	for _, tag := range tags {
		run("tag", tag)
	}

	args := []string{filepath.Join(RepoRoot(), "scripts", "next-version.sh")}
	args = append(args, extraArgs...)
	cmd := exec.Command("bash", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "NEXT_VERSION_SLOT=26.10")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stderr.String(), err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func TestNextVersionStableMonthlySequence(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want string
	}{
		{"first release of the month", nil, "v26.10.1"},
		{"next point release", []string{"v26.10.1"}, "v26.10.2"},
		{"other months do not count", []string{"v26.09.12", "v0.12.2"}, "v26.10.1"},
		{"legacy zero advances to one", []string{"v26.10.0"}, "v26.10.1"},
		{"prereleases do not advance stable sequence", []string{"v26.10.1-alpha.1", "v26.10.2-alpha.1"}, "v26.10.1"},
		{"prereleases after a stable release are ignored", []string{"v26.10.1", "v26.10.2-alpha.1"}, "v26.10.2"},
		{"sequence numbers compare numerically", []string{"v26.10.9", "v26.10.10"}, "v26.10.11"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nextVersion(t, tc.tags)
			if err != nil {
				t.Fatalf("next-version.sh failed: %v\n%s", err, got)
			}
			if got != tc.want {
				t.Errorf("tags %v: got %s, want %s", tc.tags, got, tc.want)
			}
		})
	}
}

func TestNextVersionRejectsPrereleaseArguments(t *testing.T) {
	if got, err := nextVersion(t, nil, "alpha.1"); err == nil {
		t.Fatalf("next-version.sh accepted a prerelease argument: %q", got)
	}
}
