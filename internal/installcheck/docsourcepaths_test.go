package installcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ADR-0010 made documentation a CI-gated artifact, and
// documentation_test.go implements that decision as string-matching
// assertions: a required phrase that must be present, or a known-stale
// phrase that must stay absent. Every one of those is a constant added
// after a specific drift had already been noticed by a human, so the gate
// is reactive by construction — it protects exactly the claims that have
// gone wrong once before.
//
// This file adds the derived half. It reads no expectation from a list of
// remembered strings: it extracts the repository-relative source paths the
// documents themselves cite and requires each one to exist in the tree.
// Renaming or deleting a file therefore fails here instead of silently
// leaving every citation of it pointing at nothing.

// currentStateDocRoots is the set of documents whose source-path citations
// must resolve.
//
// Living guides and retained decisions cite source paths whose existence is
// checked here. Skills are excluded because their historical examples may
// legitimately name retired files; active plans are implementation proposals,
// not claims about the current tree. Retired proposals remain in Git history.
var currentStateDocRoots = []string{
	"README.md",
	"CONFIG.md",
	filepath.Join("docs", "index.md"),
	filepath.Join("docs", "reference.md"),
	filepath.Join("docs", "design"),
	filepath.Join("docs", "specs"),
	filepath.Join("docs", "adr"),
	"AGENTS.md",
	"CONTRIBUTING.md",
}

// knownMissingDocPaths records source paths that current-state
// documentation cites and the tree does not contain, each mapped to the
// issue tracking the prose correction.
//
// It is shrink-only: TestKnownMissingDocPathsAreStillMissing fails once a
// waived path exists, so the waiver cannot outlive the drift it describes
// and must be deleted in the same change that fixes the document. Adding
// an entry here is not a way to land a broken citation — it is a record
// that a fix is already tracked and blocked elsewhere.
var knownMissingDocPaths = map[string]string{}

// minCitedSourcePaths guards this gate against becoming vacuous. If a
// future edit to docSourcePathPattern, or a reorganisation of docs/,
// stopped the extractor from matching anything, every assertion below
// would pass over an empty set and the gate would silently protect
// nothing. 134 paths are cited today; the floor is set well beneath that so
// ordinary documentation edits do not trip it, while a parser regression
// that collapses the set still fails.
const minCitedSourcePaths = 100

// docSourcePathPattern matches one backtick-quoted, repository-relative
// source path, with an optional :line or :start-end suffix (ADR-0009 and
// the design documents both cite specific lines that way).
//
// .rules is deliberately absent from the extension set. The repository
// ships no PolicyKit .rules file by design — make install removes the
// legacy passwordless ones and TestPolkitPasswordlessRulesAreAbsent owns
// that invariant — so a document naming one is asserting a file must *not*
// exist, and requiring it to exist here would contradict that gate.
var docSourcePathPattern = regexp.MustCompile(
	`^(?:internal|cmd|data|test)/[A-Za-z0-9_.\-/]+\.(?:go|yml|yaml|sh|policy|desktop|svg|toml)(?::\d+(?:-\d+)?)?$`,
)

// fencedBlockPattern matches Markdown fenced code blocks (```...```).
// Fenced blocks are stripped before inline code span extraction because
// a three-backtick fence introduces an odd number of backticks that shifts
// inline span parity across the rest of the document, hiding subsequent
// backtick-quoted source path citations.
var fencedBlockPattern = regexp.MustCompile("(?s)```[^\n]*\n.*?```")

// backtickSpanPattern matches the contents of an inline code span.
// Citations are always written in backticks in these documents, and
// restricting extraction to code spans keeps ordinary prose — which may
// mention a package name in passing — out of the set. Soft-wrapped multi-line
// code spans (such as constant declarations split across a line break) are
// matched so trailing parenthetical citations are not missed by shifting parity.
var backtickSpanPattern = regexp.MustCompile("`([^`\n]+(?:\n[ \t]*[^ \t\r\n`][^`\n]*)?)`")

// citation is one extracted source-path reference: the path with any
// :line suffix stripped, plus the document it was read from, so a failure
// names the file a maintainer has to edit.
type citation struct {
	Path     string
	Document string
}

// stripLineSuffix removes a trailing :line or :start-end from a cited
// path. The suffix is part of the citation, not part of the filename.
func stripLineSuffix(cited string) string {
	i := strings.LastIndex(cited, ":")
	if i < 0 {
		return cited
	}
	return cited[:i]
}

// extractCitedSourcePaths returns every source path cited by document,
// which is the document's text, in the order encountered.
func extractCitedSourcePaths(document, text string) []citation {
	var found []citation
	cleanText := fencedBlockPattern.ReplaceAllString(text, "")
	for _, span := range backtickSpanPattern.FindAllStringSubmatch(cleanText, -1) {
		raw := span[1]
		candidates := []string{
			strings.TrimSpace(raw),
			strings.TrimSpace(strings.ReplaceAll(raw, "\n", " ")),
			strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, "\n", ""), " ", "")),
		}
		var matched string
		for _, c := range candidates {
			if docSourcePathPattern.MatchString(c) {
				matched = c
				break
			}
		}
		if matched == "" {
			continue
		}
		found = append(found, citation{Path: stripLineSuffix(matched), Document: document})
	}
	return found
}

// currentStateDocuments returns the repository-relative path of every
// Markdown document in currentStateDocRoots, expanding directory roots
// recursively. It fails the test rather than skipping when a root is
// missing: a root that has been renamed must be corrected here, not
// silently dropped from the gate's scope.
func currentStateDocuments(t *testing.T) []string {
	t.Helper()

	var documents []string
	for _, root := range currentStateDocRoots {
		absolute := filepath.Join(RepoRoot(), root)
		info, err := os.Stat(absolute)
		if err != nil {
			t.Fatalf("current-state documentation root %s is missing: %v", root, err)
		}
		if !info.IsDir() {
			documents = append(documents, root)
			continue
		}
		err = filepath.WalkDir(absolute, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				return nil
			}
			relative, relErr := filepath.Rel(RepoRoot(), path)
			if relErr != nil {
				return relErr
			}
			documents = append(documents, relative)
			return nil
		})
		if err != nil {
			t.Fatalf("walking current-state documentation root %s: %v", root, err)
		}
	}

	sort.Strings(documents)
	return documents
}

// citedSourcePaths returns every citation in the current-state document
// set, deduplicated by path+document pair and sorted for a stable subtest
// order.
func citedSourcePaths(t *testing.T) []citation {
	t.Helper()

	seen := make(map[citation]bool)
	var all []citation
	for _, document := range currentStateDocuments(t) {
		for _, found := range extractCitedSourcePaths(document, readRepoFile(t, document)) {
			if seen[found] {
				continue
			}
			seen[found] = true
			all = append(all, found)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Path != all[j].Path {
			return all[i].Path < all[j].Path
		}
		return all[i].Document < all[j].Document
	})
	return all
}

// TestCurrentDocsCiteExistingSourcePaths is the gate: every source path
// cited by current-state documentation must exist in the tree.
func TestCurrentDocsCiteExistingSourcePaths(t *testing.T) {
	for _, cited := range citedSourcePaths(t) {
		t.Run(cited.Document+" cites "+cited.Path, func(t *testing.T) {
			if issue, waived := knownMissingDocPaths[cited.Path]; waived {
				t.Skipf("%s is a recorded missing citation; prose fix tracked by %s", cited.Path, issue)
			}
			if _, err := os.Stat(filepath.Join(RepoRoot(), cited.Path)); err != nil {
				t.Errorf("%s cites %s, which does not exist in the repository: %v\n"+
					"Update the document, or — if the correction is blocked elsewhere — "+
					"record the path in knownMissingDocPaths with the issue tracking it.",
					cited.Document, cited.Path, err)
			}
		})
	}
}

// TestKnownMissingDocPathsAreStillMissing keeps the waiver list
// shrink-only. Once a waived path exists, the documentation that cites it
// is no longer wrong and the waiver is stale, so it must be deleted rather
// than left to suppress a real future regression at the same path.
func TestKnownMissingDocPathsAreStillMissing(t *testing.T) {
	for path, issue := range knownMissingDocPaths {
		t.Run(path, func(t *testing.T) {
			if _, err := os.Stat(filepath.Join(RepoRoot(), path)); err == nil {
				t.Errorf("%s now exists, so the citations of it are correct. "+
					"Remove its knownMissingDocPaths entry (tracked by %s).", path, issue)
			}
		})
	}
}

// TestWaivedDocPathsAreActuallyCited stops the waiver list from accruing
// entries for paths no document mentions any more. Such an entry protects
// nothing and makes the list read as a larger debt than it is.
func TestWaivedDocPathsAreActuallyCited(t *testing.T) {
	cited := make(map[string]bool)
	for _, found := range citedSourcePaths(t) {
		cited[found.Path] = true
	}

	for path, issue := range knownMissingDocPaths {
		if !cited[path] {
			t.Errorf("knownMissingDocPaths waives %s (%s), but no current-state "+
				"document cites it any more; delete the entry.", path, issue)
		}
	}
}

// TestCurrentDocsCiteManySourcePaths guards the gate itself: a regression
// in the extractor must fail loudly rather than leave the assertions above
// iterating over an empty set.
func TestCurrentDocsCiteManySourcePaths(t *testing.T) {
	cited := citedSourcePaths(t)
	if len(cited) < minCitedSourcePaths {
		t.Errorf("extracted %d cited source paths from current-state documentation, want at least %d; "+
			"the extractor is probably no longer matching citations", len(cited), minCitedSourcePaths)
	}
}

// TestDocSourcePathPatternMatchesCitationForms pins the extractor's own
// behaviour, so the shapes it accepts and rejects are a stated contract
// rather than whatever the regexp happens to do.
func TestDocSourcePathPatternMatchesCitationForms(t *testing.T) {
	cases := []struct {
		cited string
		match bool
		want  string
	}{
		{"internal/dryrun/dryrun.go", true, "internal/dryrun/dryrun.go"},
		{"internal/views/dryrun.go:16", true, "internal/views/dryrun.go"},
		{"internal/updex/updex.go:160-164", true, "internal/updex/updex.go"},
		{"cmd/chairlift/main.go", true, "cmd/chairlift/main.go"},
		{"data/io.projectbluefin.chairlift.updex.policy", true, "data/io.projectbluefin.chairlift.updex.policy"},
		{"test/e2e/run.sh", true, "test/e2e/run.sh"},
		// A dot-qualified symbol is not a path.
		{"internal/dryrun.Enabled", false, ""},
		// An ellipsis in a package glob is not a path.
		{"internal/...", false, ""},
		// A bare directory carries no extension to resolve.
		{"internal/views", false, ""},
		// Absolute install destinations are not repository paths.
		{"/usr/share/chairlift/config.yml", false, ""},
		// Roots outside the source tree are out of scope.
		{"docs/design/overview.md", false, ""},
		// .rules files are asserted absent elsewhere; see the pattern's doc comment.
		{"data/io.projectbluefin.chairlift.updex.rules", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.cited, func(t *testing.T) {
			got := docSourcePathPattern.MatchString(tc.cited)
			if got != tc.match {
				t.Fatalf("docSourcePathPattern.MatchString(%q) = %v, want %v", tc.cited, got, tc.match)
			}
			if tc.match {
				if stripped := stripLineSuffix(tc.cited); stripped != tc.want {
					t.Errorf("stripLineSuffix(%q) = %q, want %q", tc.cited, stripped, tc.want)
				}
			}
		})
	}
}

// TestExtractCitedSourcePathsReadsOnlyCodeSpans pins the other half of the
// extractor: citations are read from inline code spans, and prose that
// merely names a path is not treated as a citation.
func TestExtractCitedSourcePathsReadsOnlyCodeSpans(t *testing.T) {
	text := "Preview mode lives in `internal/dryrun/dryrun.go`, not in " +
		"internal/views/dryrun.go, and the helper is `cmd/chairlift-updex-helper/main.go`.\n" +
		"ADR-0009 cites `internal/views/dryrun.go:16`.\n"

	got := extractCitedSourcePaths("docs/design/overview.md", text)
	want := []citation{
		{Path: "internal/dryrun/dryrun.go", Document: "docs/design/overview.md"},
		{Path: "cmd/chairlift-updex-helper/main.go", Document: "docs/design/overview.md"},
		{Path: "internal/views/dryrun.go", Document: "docs/design/overview.md"},
	}

	if len(got) != len(want) {
		t.Fatalf("extracted %d citations (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("citation %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
