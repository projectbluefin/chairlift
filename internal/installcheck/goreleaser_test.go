package installcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/updex"
	"gopkg.in/yaml.v3"
)

// wantHomepage is the repository URL release.footer's "Full Changelog" line
// must contain. GoReleaser OSS (unlike Pro) has no metadata.homepage to
// template from, so the URL is a literal in the footer text itself — this
// constant is what TestGoreleaserReleaseFooterHasCanonicalRepoURL checks it
// against.
const wantHomepage = "https://github.com/projectbluefin/chairlift"

// loadGoreleaserConfig parses the real, repo-root .goreleaser.yaml — not a
// fixture or copy that could drift from the file goreleaser actually reads
// — using the yaml.v3 dependency already vendored for internal/config.
func loadGoreleaserConfig(t *testing.T) GoreleaserConfig {
	t.Helper()

	path := filepath.Join(RepoRoot(), ".goreleaser.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	var cfg GoreleaserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if len(cfg.Archives) == 0 {
		t.Fatalf("%s configures no release archives; the Homebrew cask installs from them", path)
	}
	return cfg
}

// TestGoreleaserArchivesShipAllSchemas asserts that every data/*.gschema.xml
// file is included in every release archive, so the Homebrew cask — which
// installs from that archive — ships all required schemas.
func TestGoreleaserArchivesShipAllSchemas(t *testing.T) {
	cfg := loadGoreleaserConfig(t)

	schemas, err := filepath.Glob(filepath.Join(RepoRoot(), "data", "*.gschema.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) == 0 {
		t.Fatal("no gschemas found in data/")
	}

	for _, absSchema := range schemas {
		relSchema, err := filepath.Rel(RepoRoot(), absSchema)
		if err != nil {
			t.Fatal(err)
		}
		for i, archive := range cfg.Archives {
			if !slices.Contains(archive.Files, relSchema) {
				t.Errorf("archives[%d] omits %s", i, relSchema)
			}
		}
	}
}

// TestGoreleaserArchivesCarryTheInstallSurface parses the real
// .goreleaser.yaml and asserts every release archive carries what a Homebrew
// cask install and an OS image's privileged integration take from it: the GUI,
// both fixed-path helpers under the exact file names internal/updex.HelperPath
// and internal/ublue.HelperPath expect, the three PolicyKit policies, the
// desktop entry, the wrapper, maintainer defaults, the channel-table example,
// and the license text.
//
// It checks every archive, so a second archive cannot silently omit a helper
// or policy. It also rejects
// passwordless PolicyKit rules and a live channel table in any archive: the
// channel table decides the image reference the privileged helper hands to
// bootc, so only the documented example may ship.
func TestGoreleaserArchivesCarryTheInstallSurface(t *testing.T) {
	cfg := loadGoreleaserConfig(t)

	buildByBinary := make(map[string]string, len(cfg.Builds))
	for _, build := range cfg.Builds {
		binary := build.Binary
		if binary == "" {
			binary = build.ID
		}
		buildByBinary[binary] = build.ID
	}

	wantBinaries := []string{
		"chairlift",
		filepath.Base(updex.HelperPath),
		filepath.Base(ublue.HelperPath),
	}
	wantFiles := []string{
		"LICENSE",
		"config.yml",
		"channels.example.yml",
		"data/chairlift-wrapper.sh",
		"data/io.projectbluefin.chairlift.desktop",
		"data/io.projectbluefin.chairlift.bootc.policy",
		"data/io.projectbluefin.chairlift.updex.policy",
		"data/io.projectbluefin.chairlift.ublue.policy",
	}

	for i, archive := range cfg.Archives {
		t.Run(fmt.Sprintf("archives[%d]", i), func(t *testing.T) {
			for _, binary := range wantBinaries {
				id, ok := buildByBinary[binary]
				if !ok {
					t.Errorf("no build produces %s", binary)
					continue
				}
				if len(archive.IDs) != 0 && !slices.Contains(archive.IDs, id) {
					t.Errorf("archives[%d].ids = %v omits build %q (%s)", i, archive.IDs, id, binary)
				}
			}

			for _, want := range wantFiles {
				if !slices.Contains(archive.Files, want) {
					t.Errorf("archives[%d] omits %s", i, want)
				}
			}

			for _, file := range archive.Files {
				if strings.HasSuffix(file, ".rules") {
					t.Errorf("archives[%d] ships passwordless PolicyKit rule %s", i, file)
				}
				for _, live := range imageinfo.SystemTablePaths {
					if filepath.Base(file) == filepath.Base(live) {
						t.Errorf("archives[%d] ships a live channel table %s (read from %s); ship only the example", i, file, live)
					}
				}
			}
		})
	}
}

// TestGoreleaserReleaseFooterHasCanonicalRepoURL parses the real
// .goreleaser.yaml and asserts release.footer's "Full Changelog" line
// contains the canonical repository URL (wantHomepage). GoReleaser OSS has
// no metadata.homepage to template the URL from — unlike Pro — so the URL
// is necessarily a literal here; this test is what catches it drifting from
// the actual repository location instead.
//
// The assertions are on the raw *template text* read from the YAML: the
// footer is expanded by goreleaser at release time, and goreleaser is not
// installed on the gate host or in make ci, so nothing here renders the
// template or shells out.
//
// It is deliberately non-vacuous: an absent or empty footer, or zero or more
// than one "Full Changelog" line, is a t.Fatal rather than a silent pass.
func TestGoreleaserReleaseFooterHasCanonicalRepoURL(t *testing.T) {
	cfg := loadGoreleaserConfig(t)

	footer := cfg.Release.Footer
	if strings.TrimSpace(footer) == "" {
		t.Fatal("release.footer is absent or empty in .goreleaser.yaml")
	}

	var matches []string
	for _, line := range strings.Split(footer, "\n") {
		if strings.Contains(line, "Full Changelog") {
			matches = append(matches, line)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("release.footer has %d lines containing %q, want exactly 1; footer:\n%s", len(matches), "Full Changelog", footer)
	}
	line := matches[0]

	if !strings.Contains(line, wantHomepage) {
		t.Errorf("Full Changelog line = %q, want it to contain the canonical repository URL %q", line, wantHomepage)
	}
	if !strings.Contains(line, "/compare/{{ .PreviousTag }}...{{ .Tag }}") {
		t.Errorf("Full Changelog line = %q, want it to keep the %q suffix", line, "/compare/{{ .PreviousTag }}...{{ .Tag }}")
	}
	if strings.Contains(line, ".ProjectName") {
		t.Errorf("Full Changelog line = %q, want no .ProjectName: the repository URL already ends in the repository name, so the two must not be concatenated", line)
	}
}
