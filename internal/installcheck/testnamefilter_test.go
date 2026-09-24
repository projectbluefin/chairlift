package installcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// CI runs unit tests as `go test ./internal/... -run "^Test[^I]" -skip
// "Integration"` (the `ci` target's unit-test and race-detector steps, and
// the Unit Tests and Race Detection jobs in .github/workflows/test.yml).
// Two name shapes are therefore invisible to every gate:
//
//   - anything whose first character after `Test` is `I` — not only
//     `TestIntegration...`, but ordinary names like `TestIsValid`,
//     `TestInitConfig`, `TestIndexOf`;
//   - anything containing `Integration` anywhere.
//
// AGENTS.md reserves those two shapes for tests that need a real environment
// (a live brew, flatpak, bootc, or GTK display) and places them under
// `test/e2e/`, which is why this scan stops at `internal/`. That split is not
// a preference: `make e2e` runs `go test ./test/e2e` unfiltered, so that is
// the only directory where a reserved name is reached by an enforced gate.
// The same name under `internal/` is reached by none — the filtered
// unit-test step skips it and the e2e step never looks there — so there is
// nothing an allowlist here could legitimately hold. Such a test is still
// perfectly runnable by hand, and that is the trap rather than a
// consolation: `make test` and a bare `go test ./...` both execute it, so
// it passes on the author's machine and in review, and is silently absent
// from every gate that decides whether main is green.
//
// Inside `internal/` the shape is therefore always an accident, and the
// accident is silent: the test compiles, a local `go test ./...` passes it,
// and no gate ever executes it.
//
// It is not hypothetical. When this gate was written it found nine such tests
// across five packages — distrobox, gaming, version, installcheck, and a
// since-removed OS update provider — including TestGoreleaserPublishesSystemIntegrationPackage,
// the test AGENTS.md and docs/adr/0006 both name as the enforcement for the
// system-integration package split. Its name matched the `-skip "Integration"`
// half of the filter, so the unit-test step never selected it. All nine were
// renamed; all nine pass.
//
// Prefer renaming so the first letter after `Test` names the subject
// (`TestValidConfigRejectsUnknownGroup`, not `TestIsValidRejectsUnknownGroup`).

func TestNoInternalTestNameIsExcludedByTheCIFilter(t *testing.T) {
	root := filepath.Join(RepoRoot(), "internal")
	fileSet := token.NewFileSet()

	var excluded []string
	scanned := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		parsed, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}

		relative, relErr := filepath.Rel(RepoRoot(), path)
		if relErr != nil {
			relative = path
		}

		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			name := function.Name.Name
			if !strings.HasPrefix(name, "Test") {
				continue
			}
			scanned++
			if strings.HasPrefix(name, "TestI") || strings.Contains(name, "Integration") {
				excluded = append(excluded, relative+": "+name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	if scanned == 0 {
		t.Fatal("found no test functions under internal/; the scan is broken, not the tree")
	}

	if len(excluded) > 0 {
		sort.Strings(excluded)
		t.Errorf("these tests under internal/ are excluded by CI's own filter and therefore never run:\n  %s\n"+
			"`-run \"^Test[^I]\"` drops any name whose first letter after Test is I; `-skip \"Integration\"` "+
			"drops any name containing Integration. Rename so the subject follows Test, or move the test to "+
			"test/e2e if it genuinely needs a live environment.",
			strings.Join(excluded, "\n  "))
	}
}
