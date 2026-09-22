package installcheck

import (
	"go/build"
	"io/fs"
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
	// the example verbatim would break every supported system.
	for _, builtin := range []string{
		"ghcr.io/ublue-os/bluefin",
		"ghcr.io/projectbluefin/bluefin-lts",
		"ghcr.io/projectbluefin/dakota",
		"ghcr.io/projectbluefin/dakota-gaming",
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
	stubbed := []string{envVar, autoUpdatesEnvVar, gpuEnvVar}

	taggedOverrideRel := filepath.Join("internal", "app", "imageinfo_override_e2e.go")
	overrideSource := readRepoFile(t, taggedOverrideRel)
	if !strings.HasPrefix(overrideSource, "//go:build chairlift_e2e") {
		t.Errorf("%s does not open with //go:build chairlift_e2e", taggedOverrideRel)
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
	untaggedOverrideRel := filepath.Join("internal", "app", "imageinfo_override.go")
	untagged := readRepoFile(t, untaggedOverrideRel)
	if !strings.HasPrefix(untagged, "//go:build !chairlift_e2e") {
		t.Errorf("%s does not open with //go:build !chairlift_e2e", untaggedOverrideRel)
	}
	for _, forbidden := range stubbed {
		if strings.Contains(untagged, forbidden) {
			t.Errorf("the default build's override reads %s; it must be a no-op", forbidden)
		}
	}

	// Build contexts for the CI target matrix (linux/amd64 and linux/arm64)
	// without the e2e tag, plus the e2e tagged context.
	amd64Ctx := build.Default
	amd64Ctx.GOOS = "linux"
	amd64Ctx.GOARCH = "amd64"
	amd64Ctx.BuildTags = nil

	arm64Ctx := build.Default
	arm64Ctx.GOOS = "linux"
	arm64Ctx.GOARCH = "arm64"
	arm64Ctx.BuildTags = nil

	e2eCtx := build.Default
	e2eCtx.BuildTags = []string{"chairlift_e2e"}

	// Verify build constraints on the two override counterparts.
	appDir := filepath.Join(RepoRoot(), "internal", "app")
	matchUntaggedOverrideAmd64, err := amd64Ctx.MatchFile(appDir, "imageinfo_override.go")
	if err != nil {
		t.Fatalf("amd64 MatchFile imageinfo_override.go: %v", err)
	}
	matchUntaggedOverrideArm64, err := arm64Ctx.MatchFile(appDir, "imageinfo_override.go")
	if err != nil {
		t.Fatalf("arm64 MatchFile imageinfo_override.go: %v", err)
	}
	if !matchUntaggedOverrideAmd64 || !matchUntaggedOverrideArm64 {
		t.Errorf("%s must match untagged build constraints", untaggedOverrideRel)
	}
	matchUntaggedOverrideE2E, err := e2eCtx.MatchFile(appDir, "imageinfo_override.go")
	if err != nil {
		t.Fatalf("e2e MatchFile imageinfo_override.go: %v", err)
	}
	if matchUntaggedOverrideE2E {
		t.Errorf("%s must not match e2e build constraints", untaggedOverrideRel)
	}

	matchTaggedOverrideAmd64, err := amd64Ctx.MatchFile(appDir, "imageinfo_override_e2e.go")
	if err != nil {
		t.Fatalf("amd64 MatchFile imageinfo_override_e2e.go: %v", err)
	}
	matchTaggedOverrideArm64, err := arm64Ctx.MatchFile(appDir, "imageinfo_override_e2e.go")
	if err != nil {
		t.Fatalf("arm64 MatchFile imageinfo_override_e2e.go: %v", err)
	}
	if matchTaggedOverrideAmd64 || matchTaggedOverrideArm64 {
		t.Errorf("%s must not match untagged build constraints", taggedOverrideRel)
	}
	matchTaggedOverrideE2E, err := e2eCtx.MatchFile(appDir, "imageinfo_override_e2e.go")
	if err != nil {
		t.Fatalf("e2e MatchFile imageinfo_override_e2e.go: %v", err)
	}
	if !matchTaggedOverrideE2E {
		t.Errorf("%s must match e2e build constraints", taggedOverrideRel)
	}

	// Discover every non-test Go source file across the repository and assert
	// that stubbed variables appear ONLY in the tagged override file.
	scanned := 0
	root := RepoRoot()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		scanned++
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if rel == taggedOverrideRel {
			return nil
		}

		dir := filepath.Dir(path)
		filename := entry.Name()

		matchAmd64, matchErr := amd64Ctx.MatchFile(dir, filename)
		if matchErr != nil {
			t.Errorf("amd64 MatchFile %s: %v", rel, matchErr)
		}
		matchArm64, matchErr := arm64Ctx.MatchFile(dir, filename)
		if matchErr != nil {
			t.Errorf("arm64 MatchFile %s: %v", rel, matchErr)
		}
		matchE2E, matchErr := e2eCtx.MatchFile(dir, filename)
		if matchErr != nil {
			t.Errorf("e2e MatchFile %s: %v", rel, matchErr)
		}

		// A second tagged file is forbidden: only imageinfo_override_e2e.go
		// may be conditioned on chairlift_e2e.
		if matchE2E && (!matchAmd64 || !matchArm64) {
			t.Errorf("%s matches e2e build tag but not untagged build; only %s may be tagged with chairlift_e2e", rel, taggedOverrideRel)
		}

		source := readRepoFile(t, rel)
		for _, forbidden := range stubbed {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s reads %s outside the chairlift_e2e build tag", rel, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo for Go sources: %v", err)
	}
	if scanned < 2 {
		t.Fatalf("found %d non-test Go sources; the scan is broken, not the tree", scanned)
	}

	// The cap is only a cap if its stated size matches reality. AGENTS.md
	// enumerates the stubbed behaviors; a fourth added without updating that
	// prose would leave the rule describing a smaller surface than exists.
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

// The example channel table must document the driver table format and
// provide working examples for both standard and vendor-specific drivers.
func TestExampleChannelTableDocumentsDrivers(t *testing.T) {
	content := readRepoFile(t, "channels.example.yml")
	for _, required := range []string{
		"drivers:",
		"standard:",
		"nvidia:",
		"nvidia-open:",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("channels.example.yml does not document driver format (%q)", required)
		}
	}
}
