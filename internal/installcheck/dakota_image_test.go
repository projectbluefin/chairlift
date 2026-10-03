package installcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDakotaImageSelection(t *testing.T) {
	script := filepath.Join(RepoRoot(), "test", "e2e", "dakota-image.sh")
	pinPattern := regexp.MustCompile(`^ghcr\.io/projectbluefin/dakota@sha256:[0-9a-f]{64}$`)
	for _, tc := range []struct {
		name, ci, actions, override string
		wantError                   bool
	}{
		{name: "local default"},
		{name: "CI default", ci: "true"},
		{name: "Actions default", actions: "true"},
		{name: "local override", override: "ghcr.io/projectbluefin/dakota:testing"},
		{name: "CI rejects floating override", ci: "true", override: "ghcr.io/projectbluefin/dakota:testing", wantError: true},
		{name: "Actions rejects override even with CI false", ci: "false", actions: "true", override: "other:latest", wantError: true},
		{name: "CI rejects another digest", ci: "true", override: "ghcr.io/projectbluefin/dakota@sha256:" + strings.Repeat("a", 64), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-eu", "-c", `source "$1"; printf '%s' "$IMAGE"`, "bash", script)
			cmd.Env = append(os.Environ(), "CI="+tc.ci, "GITHUB_ACTIONS="+tc.actions, "CHAIRLIFT_DAKOTA_IMAGE="+tc.override)
			out, err := cmd.CombinedOutput()
			if tc.wantError {
				if err == nil || !strings.Contains(string(out), "overrides are not allowed in CI") {
					t.Fatalf("expected CI rejection, got %q, %v", out, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("image selection failed: %s: %v", out, err)
			}
			if tc.override != "" {
				if string(out) != tc.override {
					t.Fatalf("local override = %q, want %q", out, tc.override)
				}
			} else if !pinPattern.Match(out) {
				t.Fatalf("default image must be an immutable Dakota digest, got %q", out)
			}
		})
	}
}

// Discover consumers rather than naming dakota_atspi.sh: replacing the harness
// must not silently restore a mutable tag in an E2E or release gate.
func TestDakotaHarnessesUsePinnedImage(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(RepoRoot(), "test", "e2e", "*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	consumers := 0
	for _, path := range paths {
		if filepath.Base(path) == "dakota-image.sh" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var executable []string
		for line := range strings.SplitSeq(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				executable = append(executable, line)
			}
		}
		src := strings.Join(executable, "\n")
		if !strings.Contains(src, "podman run") {
			continue
		}
		if strings.Contains(src, "CHAIRLIFT_DAKOTA_IMAGE") || strings.Contains(src, "ghcr.io/projectbluefin/dakota") {
			t.Errorf("%s defines its own Dakota image; source dakota-image.sh", path)
		}
		consumers++
		source := strings.Index(src, `source "$ROOT/test/e2e/dakota-image.sh"`)
		if source < 0 || source > strings.Index(src, "podman run") {
			t.Errorf("%s must load the pin before running containers", path)
		}
		if strings.Contains(src, "IMAGE=") || !strings.Contains(src, `"$IMAGE"`) {
			t.Errorf("%s must use the shared IMAGE selection", path)
		}
	}
	if consumers == 0 {
		t.Fatal("no Dakota container harness checked")
	}
}
