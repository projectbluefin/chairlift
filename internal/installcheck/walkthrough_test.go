package installcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/config"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/navigation"
)

const (
	walkthroughDoc = "docs/walkthrough.md"
	screenshotDir  = "docs/screenshots"
)

// markdownImage matches an inline image reference and captures its path.
var markdownImage = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

// screenshotName is the file `make screenshots` writes for the page at the
// given zero-based position in navigation.Items().
func screenshotName(index int, page string) string {
	return fmt.Sprintf("%d-%s.png", index+1, page)
}

// A feature cannot land without a screenshot: every page ChairLift can
// navigate to must have one on disk and be shown in the walkthrough.
//
// This is deliberately a referential check rather than a pixel comparison.
// Regenerating and diffing images in CI fails on font hinting and GTK point
// releases, which is churn rather than signal; what actually needs guarding
// is that the document and the captured set stay in step.
func TestWalkthroughDocumentsEveryPage(t *testing.T) {
	doc := readRepoFile(t, walkthroughDoc)

	items := navigation.Items()
	if len(items) == 0 {
		t.Fatal("navigation.Items() is empty; there are no pages to document")
	}

	for index, item := range items {
		name := screenshotName(index, item.Name)
		t.Run(item.Name, func(t *testing.T) {
			path := filepath.Join(RepoRoot(), screenshotDir, name)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("%s has no screenshot at %s/%s: %v\nrun `make screenshots`", item.Name, screenshotDir, name, err)
			}
			if info.Size() == 0 {
				t.Errorf("%s/%s is empty", screenshotDir, name)
			}
			if !strings.Contains(doc, "screenshots/"+name) {
				t.Errorf("%s does not reference screenshots/%s", walkthroughDoc, name)
			}
			// The page's title is how a reader finds the section, so the
			// document must name it.
			if !strings.Contains(doc, item.Title) {
				t.Errorf("%s never mentions the %q page", walkthroughDoc, item.Title)
			}
		})
	}
}

// Every image the document references must exist, or the published
// walkthrough renders broken images.
func TestWalkthroughImageReferencesResolve(t *testing.T) {
	doc := readRepoFile(t, walkthroughDoc)

	matches := markdownImage.FindAllStringSubmatch(doc, -1)
	if len(matches) == 0 {
		t.Fatalf("%s contains no image references", walkthroughDoc)
	}

	for _, match := range matches {
		reference := match[1]
		t.Run(reference, func(t *testing.T) {
			// References are relative to the document's own directory.
			path := filepath.Join(RepoRoot(), "docs", reference)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s references %s, which does not exist: %v", walkthroughDoc, reference, err)
			}
		})
	}
}

// A screenshot nobody references is a stale capture from a page that has
// since been renamed or removed.
func TestNoOrphanedScreenshots(t *testing.T) {
	doc := readRepoFile(t, walkthroughDoc)

	entries, err := os.ReadDir(filepath.Join(RepoRoot(), screenshotDir))
	if err != nil {
		t.Fatalf("reading %s: %v", screenshotDir, err)
	}

	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".png") {
			continue
		}
		found++
		if !strings.Contains(doc, "screenshots/"+entry.Name()) {
			t.Errorf("%s/%s is not referenced by %s; it is a stale capture", screenshotDir, entry.Name(), walkthroughDoc)
		}
	}
	if found != len(navigation.Items()) {
		t.Errorf("%s holds %d screenshots, want one per navigable page (%d)", screenshotDir, found, len(navigation.Items()))
	}
}

// The capture byproducts must never be committed: the .xwd dumps are large,
// and the log and stub files are per-run scratch.
func TestScreenshotDirectoryHoldsOnlyImages(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(RepoRoot(), screenshotDir))
	if err != nil {
		t.Fatalf("reading %s: %v", screenshotDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("%s/%s is a directory; `make screenshots` should have removed it", screenshotDir, entry.Name())
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".png") {
			t.Errorf("%s/%s is not a screenshot; only PNGs belong here", screenshotDir, entry.Name())
		}
	}
}

// Every configurable group must be documented. The list of group-to-phrase
// mappings below is hand-written, but its *completeness* is enforced: the
// group names come from config.SchemaGroups, so a group added to any page
// with no entry here fails this test.
//
// That forcing function is the point. Deriving only from navigation.Items()
// would miss every feature added to an existing page — which is how Update
// All, Automatic Updates, and Roll Back all landed.
func TestWalkthroughCoversEveryConfigurableGroup(t *testing.T) {
	doc := readRepoFile(t, walkthroughDoc)

	// Each group's evidence in the walkthrough. A group whose feature is
	// genuinely not user-visible maps to an empty phrase, which documents
	// the omission rather than hiding it.
	documented := map[string]string{
		// system_page
		"system_info_group":  "Your system details",
		"bootc_status_group": "queued for the next",
		"health_group":       "Mission Center",
		"channel_group":      "Release Channel",
		// updates_page
		"update_all_group":        "Update All",
		"bootc_updates_group":     "System Updates",
		"sysupdate_updates_group": "System Updates",
		"flatpak_updates_group":   "Flatpak",
		"brew_updates_group":      "Homebrew",
		"brew_trust_group":        "unofficial",
		// applications_page
		"applications_installed_group": "Your installed apps",
		"flatpak_user_group":           "Flatpak",
		"flatpak_system_group":         "Flatpak",
		"brew_group":                   "Homebrew packages",
		"brew_search_group":            "search across both",
		"brew_bundles_group":           "Bundles",
		// maintenance_page
		"maintenance_cleanup_group":      "your distribution added",
		"maintenance_brew_group":         "Clean up files",
		"maintenance_flatpak_group":      "Clean up files",
		"maintenance_optimization_group": "Clean up files",
		"reset_group":                    "Powerwash",
		// features_page
		"features_group":        "feature manager",
		"dx_group":              "Developer Mode",
		"gaming_group":          "Gaming Mode",
		"ai_group":              "Local AI",
		"troubleshooting_group": "Enhanced Troubleshooting",
		// help_page
		"help_resources_group": "issues",
	}

	pages, err := config.SchemaPages()
	if err != nil {
		t.Fatalf("config.SchemaPages(): %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("config.SchemaPages() is empty")
	}

	seen := make(map[string]bool, len(documented))
	for _, page := range pages {
		groups, err := config.SchemaGroups(page)
		if err != nil {
			t.Fatalf("config.SchemaGroups(%q): %v", page, err)
		}
		for _, group := range groups {
			seen[group] = true
			t.Run(page+"/"+group, func(t *testing.T) {
				phrase, ok := documented[group]
				if !ok {
					t.Fatalf("group %q has no walkthrough entry; document it in %s and add it to this table",
						group, walkthroughDoc)
				}
				if phrase == "" {
					return // deliberately not user-visible
				}
				if !strings.Contains(doc, phrase) {
					t.Errorf("%s does not document group %q (looked for %q)", walkthroughDoc, group, phrase)
				}
			})
		}
	}

	// A stale entry means a group was removed and the table was not updated.
	for group := range documented {
		if !seen[group] {
			t.Errorf("this table names group %q, which no page declares any more", group)
		}
	}
}

// Every supported image must be named in its own verification comment in
// internal/imageinfo/imageinfo.go — the code-adjacent, canonical record of
// what was checked against the registry and when. That claim does not belong
// in docs/walkthrough.md: the walkthrough is what a user reads to judge
// whether the interface is simple, and a stream-availability table read like
// a spec, not a tour. Keeping the fact in one place also means it cannot
// drift between two documents that both claim to state it.
func TestWalkthroughSupportedImagesAreVerifiedInImageinfo(t *testing.T) {
	source := readRepoFile(t, filepath.Join("internal", "imageinfo", "imageinfo.go"))

	images := imageinfo.KnownImages()
	if len(images) == 0 {
		t.Fatal("imageinfo.KnownImages() is empty")
	}
	for _, image := range images {
		if !strings.Contains(source, image) {
			t.Errorf("internal/imageinfo/imageinfo.go does not record verification for %s", image)
		}
	}
}

// The document is only discoverable if it is indexed where the repository
// says documents are indexed.
func TestWalkthroughIsIndexed(t *testing.T) {
	index := readRepoFile(t, filepath.Join("docs", "README.md"))
	if !strings.Contains(index, "walkthrough.md") {
		t.Errorf("docs/README.md does not index %s", walkthroughDoc)
	}

	readme := readRepoFile(t, "README.md")
	if !strings.Contains(readme, "docs/walkthrough.md") {
		t.Errorf("README.md does not link to %s", walkthroughDoc)
	}
}
