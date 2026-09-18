package installcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every target in ChairLift's Makefile is a command, not a file recipe: none
// of them produces a file of its own name, so every one must be declared
// .PHONY. An undeclared target is not a style nit, because make treats a
// target satisfied when a file or directory of that name already exists and
// then runs nothing at all, silently, exiting 0.
//
// That is not hypothetical. `test` was undeclared while the repository
// carried a `test/` directory, so `make test` printed "'test' is up to date"
// and ran no tests for as long as AGENTS.md documented it as `go test ./...`.
// It was found only because a post-merge gate run produced output that did
// not look like a test run. `fmt` and `lint` were equally undeclared and
// escaped only because no directory happens to share their names, which is
// luck rather than a property anyone maintains.
//
// This gate therefore checks the complete inventory rather than a sample: it
// reads every target the Makefile declares and requires each to appear in
// some .PHONY line. Adding a target without declaring it fails here.

// makefileTargetPattern matches a target definition at the start of a line.
// Recipe lines begin with a tab and are skipped by the anchor; `.PHONY` and
// other dot-directives are skipped by requiring a leading letter; variable
// assignments are rejected separately, because `FOO := bar` would otherwise
// look like a target named FOO.
var makefileTargetPattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_.-]*(?:[ \t]+[A-Za-z][A-Za-z0-9_.-]*)*)[ \t]*:([^=]|$)`)

// makefileAssignmentPattern matches the assignment operators that can appear
// where a target's colon would be: `:=`, `::=`, `?=`, `+=`, `!=`, and plain
// `=`. A line matching this is a variable, never a target.
var makefileAssignmentPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*[ \t]*(::?=|\?=|\+=|!=|=)`)

func readMakefile(t *testing.T) []string {
	t.Helper()

	path := filepath.Join(RepoRoot(), "Makefile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return strings.Split(string(data), "\n")
}

// makefileTargets returns every target name the Makefile declares, and
// phonyTargets returns every name listed on a .PHONY line.
func makefileTargets(t *testing.T, lines []string) ([]string, map[string]bool) {
	t.Helper()

	var targets []string
	phony := map[string]bool{}

	for _, line := range lines {
		if rest, ok := strings.CutPrefix(line, ".PHONY:"); ok {
			for _, name := range strings.Fields(rest) {
				phony[name] = true
			}
			continue
		}
		if makefileAssignmentPattern.MatchString(line) {
			continue
		}
		match := makefileTargetPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		targets = append(targets, strings.Fields(match[1])...)
	}

	return targets, phony
}

func TestMakefilePhonyCoversEveryTarget(t *testing.T) {
	targets, phony := makefileTargets(t, readMakefile(t))

	if len(targets) == 0 {
		t.Fatal("parsed no targets from the Makefile; the parser is broken, not the Makefile")
	}

	var undeclared []string
	for _, target := range targets {
		if !phony[target] {
			undeclared = append(undeclared, target)
		}
	}

	if len(undeclared) > 0 {
		sort.Strings(undeclared)
		t.Errorf("Makefile targets missing from .PHONY: %s\n"+
			"Every target here is a command that produces no file of its own name. "+
			"An undeclared target silently does nothing whenever a file or directory "+
			"of that name exists, which is how `make test` became a no-op.",
			strings.Join(undeclared, ", "))
	}
}

// A .PHONY entry naming a target that no longer exists is dead text that will
// outlive the reader who can tell whether it mattered, so the inventory is
// held in both directions.
func TestMakefilePhonyNamesNoMissingTarget(t *testing.T) {
	targets, phony := makefileTargets(t, readMakefile(t))

	declared := map[string]bool{}
	for _, target := range targets {
		declared[target] = true
	}

	var stale []string
	for name := range phony {
		if !declared[name] {
			stale = append(stale, name)
		}
	}

	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf(".PHONY names targets the Makefile does not define: %s", strings.Join(stale, ", "))
	}
}

// The specific regression: `test` must stay declared, because the `test/`
// directory that shadowed it is a permanent part of the repository (it holds
// the E2E suite `make e2e` builds).
func TestMakefileTestTargetIsPhonyBecauseTestDirectoryExists(t *testing.T) {
	info, err := os.Stat(filepath.Join(RepoRoot(), "test"))
	if err != nil || !info.IsDir() {
		t.Skip("no test/ directory; the shadowing hazard this pins does not exist")
	}

	_, phony := makefileTargets(t, readMakefile(t))
	if !phony["test"] {
		t.Error("the repository has a test/ directory, so an undeclared `test` target makes `make test` a silent no-op")
	}
}
