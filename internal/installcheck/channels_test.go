package installcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
)

// The example channel table is the documentation an image maintainer copies
// to add their own image. If it stops parsing under the real validator, the
// first person to find out is someone whose release-channel row silently
// stopped working.
func TestExampleChannelTableParses(t *testing.T) {
	path := filepath.Join(RepoRoot(), "channels.example.yml")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	table, err := imageinfo.ParseTable(file)
	if err != nil {
		t.Fatalf("channels.example.yml does not parse: %v", err)
	}

	// It must add images rather than replace the shipped ones, or copying
	// the example verbatim would break the three supported systems.
	for _, builtin := range []string{
		"ghcr.io/ublue-os/bluefin",
		"ghcr.io/projectbluefin/bluefin-lts",
		"ghcr.io/projectbluefin/dakota",
	} {
		if _, ok := table[builtin]; !ok {
			t.Errorf("applying channels.example.yml drops the built-in entry for %s", builtin)
		}
	}
}

// The example must document the fixed read locations and must not present
// itself as a live file, since installing it as one would apply a switch
// mapping the administrator never chose.
func TestExampleChannelTableDocumentsItsReadPaths(t *testing.T) {
	content := readRepoFile(t, "channels.example.yml")
	for _, path := range imageinfo.SystemTablePaths {
		if !strings.Contains(content, path) {
			t.Errorf("channels.example.yml does not mention the read path %s", path)
		}
	}
}

// The Makefile and .goreleaser.yaml must install the example to the same
// documentation path, and must not install a live table to either read
// location.
func TestChannelTableIsNotInstalledLive(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")
	const wantExampleDst = "doc/chairlift/channels.example.yml"
	if !strings.Contains(makefile, wantExampleDst) {
		t.Errorf("Makefile does not install channels.example.yml to %s", wantExampleDst)
	}

	// Only install destinations are inspected, not prose: both files
	// legitimately name the live paths in comments explaining why they are
	// not installed.
	for _, source := range []struct {
		relative string
		isDest   func(line string) bool
	}{
		{"Makefile", func(line string) bool { return strings.Contains(line, "install -D") }},
		{".goreleaser.yaml", func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "dst:") }},
	} {
		for _, line := range strings.Split(readRepoFile(t, source.relative), "\n") {
			if !source.isDest(line) {
				continue
			}
			for _, live := range imageinfo.SystemTablePaths {
				if strings.Contains(line, live) {
					t.Errorf("%s installs to the live channel table path %s; only the example may be installed\n  %s",
						source.relative, live, strings.TrimSpace(line))
				}
			}
		}
	}
}

// The image-descriptor override exists so the screenshot walkthrough can
// render the Bluefin-family rows on a runner that is not a Bluefin system. A
// released binary that honored it could be made to tell a user their machine
// runs an image it does not, so it must stay behind the chairlift_e2e build
// tag that only `make e2e` sets.
func TestDescriptorOverrideStaysBehindTheE2EBuildTag(t *testing.T) {
	const envVar = "CHAIRLIFT_IMAGE_INFO"
	const autoUpdatesEnvVar = "CHAIRLIFT_AUTO_UPDATES"
	const gpuEnvVar = "CHAIRLIFT_GPU_VENDORS"

	overrideSource := readRepoFile(t, filepath.Join("internal", "app", "imageinfo_override_e2e.go"))
	if !strings.HasPrefix(overrideSource, "//go:build chairlift_e2e") {
		t.Error("internal/app/imageinfo_override_e2e.go does not open with //go:build chairlift_e2e")
	}
	if !strings.Contains(overrideSource, envVar) {
		t.Errorf("the tagged override does not read %s", envVar)
	}
	// The automatic-updates probe override shares the same file and the same
	// build tag, so it is covered by the same guard.
	for _, required := range []string{autoUpdatesEnvVar, gpuEnvVar} {
		if !strings.Contains(overrideSource, required) {
			t.Errorf("the tagged override does not read %s", required)
		}
	}

	// The default build must carry a no-op with the negated tag, or the
	// package would not compile without chairlift_e2e.
	untagged := readRepoFile(t, filepath.Join("internal", "app", "imageinfo_override.go"))
	if !strings.HasPrefix(untagged, "//go:build !chairlift_e2e") {
		t.Error("internal/app/imageinfo_override.go does not open with //go:build !chairlift_e2e")
	}
	for _, forbidden := range []string{envVar, autoUpdatesEnvVar, gpuEnvVar} {
		if strings.Contains(untagged, forbidden) {
			t.Errorf("the default build's override reads %s; it must be a no-op", forbidden)
		}
	}

	// No other package may read it, or the tag would not contain it.
	for _, relative := range []string{
		filepath.Join("internal", "app", "app.go"),
		filepath.Join("internal", "ublue", "ublue.go"),
		filepath.Join("internal", "autoupdate", "autoupdate.go"),
		filepath.Join("internal", "gpu", "gpu.go"),
		filepath.Join("cmd", "chairlift", "main.go"),
		filepath.Join("cmd", "chairlift-ublue-helper", "main.go"),
	} {
		source := readRepoFile(t, relative)
		for _, forbidden := range []string{envVar, autoUpdatesEnvVar, gpuEnvVar} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s reads %s outside the chairlift_e2e build tag", relative, forbidden)
			}
		}
	}

	// The cap is only a cap if its stated size matches reality. AGENTS.md
	// enumerates the stubbed behaviors; a fourth added without updating that
	// prose would leave the rule describing a smaller surface than exists.
	stubbed := []string{envVar, autoUpdatesEnvVar, gpuEnvVar}
	agents := readRepoFile(t, "AGENTS.md")
	for _, name := range stubbed {
		if !strings.Contains(agents, name) {
			t.Errorf("AGENTS.md's stub-surface rule does not name %s", name)
		}
	}
	for _, count := range []string{"Two\n  behaviors", "Four\n  behaviors"} {
		if strings.Contains(agents, count) {
			t.Errorf("AGENTS.md's stub-surface rule says %q but %d behaviors are stubbed", count, len(stubbed))
		}
	}

	// Only the e2e build may set the tag; `make build` — what `make ci` and
	// the release pipeline use — must not.
	makefile := readRepoFile(t, "Makefile")
	if !strings.Contains(makefile, "E2E_TAGS=chairlift_e2e") {
		t.Error("Makefile does not define E2E_TAGS=chairlift_e2e")
	}
	tagged := 0
	for _, line := range strings.Split(makefile, "\n") {
		if !strings.Contains(line, "$(GOBUILD)") || !strings.Contains(line, "E2E_TAGS") {
			continue
		}
		tagged++
		// Only the GUI may carry the tag. A privileged helper built with it
		// would no longer be the binary that ships.
		if !strings.HasSuffix(strings.TrimSpace(line), "./cmd/chairlift") {
			t.Errorf("a non-GUI binary is built with the e2e tag:\n  %s", strings.TrimSpace(line))
		}
	}
	if tagged != 1 {
		t.Errorf("Makefile builds %d targets with E2E_TAGS, want exactly 1 (the GUI)", tagged)
	}
	if strings.Contains(readRepoFile(t, ".goreleaser.yaml"), "chairlift_e2e") {
		t.Error(".goreleaser.yaml builds with the chairlift_e2e tag; released binaries must not honor the override")
	}
}
