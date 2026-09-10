package installcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var expectedLegacySkillAliases = map[string]string{
	"acceptance-criteria-need-automated-checks-not-inspection.md":          "../../skills/automated-acceptance/SKILL.md",
	"agents-md-is-a-plan-acceptance-criterion.md":                          "../../skills/agent-contracts/SKILL.md",
	"assert-line-numbers-are-parsed-and-positive-not-substring-matched.md": "../../skills/diagnostic-assertions/SKILL.md",
	"chunk-diff-budgets-scale-with-decision-table-rows.md":                 "../../skills/change-sizing/SKILL.md",
	"cross-chunk-test-contracts-must-be-tracked-forward.md":                "../../skills/evolving-test-contracts/SKILL.md",
	"derive-schema-from-canonical-struct-not-shadow-representation.md":     "../../skills/canonical-schema/SKILL.md",
	"discarded-merge-branches-still-need-validation.md":                    "../../skills/merge-validation/SKILL.md",
	"doc-chunks-must-fix-existing-contradictions.md":                       "../../skills/documentation-reconciliation/SKILL.md",
	"frozen-allowlist-authorization-is-per-entry-not-per-chunk.md":         "../../skills/frozen-allowlists/SKILL.md",
	"gate-test-scope-is-internal-only.md":                                  "../../skills/gated-test-placement/SKILL.md",
	"grep-acceptance-criteria-subtests-and-cross-chunk-conflicts.md":       "../../skills/grep-acceptance/SKILL.md",
	"grep-removal-criteria-must-exclude-mill-and-tests.md":                 "../../skills/removal-verification/SKILL.md",
	"grep-test-files-for-package-level-identifier-collisions.md":           "../../skills/package-namespace/SKILL.md",
	"gtk-headless-tests.md":                                                "../../skills/gtk-headless-testing/SKILL.md",
	"helper-functions-need-direct-test-calls.md":                           "../../skills/helper-test-surface/SKILL.md",
	"leaf-package-docs-must-enumerate-outcomes-not-summarize.md":           "../../skills/leaf-package-documentation/SKILL.md",
	"leaf-package-enumeration-docs-update-same-chunk.md":                   "../../skills/leaf-package-inventory/SKILL.md",
	"match-dependency-behavior-from-gomodcache-not-memory.md":              "../../skills/dependency-behavior/SKILL.md",
	"multi-tier-error-classification-needs-a-decision-table.md":            "../../skills/error-classification/SKILL.md",
	"regression-tests-must-cover-every-collection-entry.md":                "../../skills/collection-regressions/SKILL.md",
	"rename-only-scope-forbids-new-non-test-files.md":                      "../../skills/scope-control/SKILL.md",
	"split-oversized-chunks-by-concern-not-by-carve-out.md":                "../../skills/change-decomposition/SKILL.md",
	"trace-one-concrete-input-through-pipeline-order.md":                   "../../skills/pipeline-tracing/SKILL.md",
	"yaml-scalar-key-identity-needs-tag-not-just-value.md":                 "../../skills/yaml-key-identity/SKILL.md",
}

const factoryCanonicalSkillPackageCount = 26

type factorySkillFrontMatter struct {
	name         string
	description  string
	version      string
	lastUpdated  string
	tags         []string
	metadataType string
}

func factoryParseSkillFrontMatter(packageName, content string) (factorySkillFrontMatter, error) {
	var frontMatter factorySkillFrontMatter
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return frontMatter, fmt.Errorf("does not start with YAML front matter")
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return frontMatter, fmt.Errorf("has an unterminated YAML front matter block")
	}

	topLevel := make(map[string]string)
	seenTopLevel := make(map[string]bool)
	section := ""
	for _, line := range lines[1:end] {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		trimmed := strings.TrimSpace(line)
		if indent == 0 {
			key, value, ok := strings.Cut(trimmed, ":")
			if !ok || strings.TrimSpace(key) == "" {
				return frontMatter, fmt.Errorf("malformed front matter line %q", line)
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if seenTopLevel[key] {
				return frontMatter, fmt.Errorf("duplicate front matter field %q", key)
			}
			seenTopLevel[key] = true

			switch key {
			case "tags", "metadata":
				if value != "" {
					return frontMatter, fmt.Errorf("front matter field %q must contain an indented mapping or list", key)
				}
				section = key
			default:
				if value == "" || value == "#" || value == "null" || value == "~" {
					return frontMatter, fmt.Errorf("front matter field %q is empty", key)
				}
				topLevel[key] = value
				section = ""
			}
			continue
		}

		switch section {
		case "tags":
			if !strings.HasPrefix(trimmed, "-") {
				return frontMatter, fmt.Errorf("malformed tags entry %q", line)
			}
			tag := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			if tag == "" || tag == "#" || tag == "null" || tag == "~" {
				return frontMatter, fmt.Errorf("tags contains an empty entry")
			}
			frontMatter.tags = append(frontMatter.tags, tag)
		case "metadata":
			key, value, ok := strings.Cut(trimmed, ":")
			if !ok || strings.TrimSpace(key) != "type" {
				return frontMatter, fmt.Errorf("metadata contains an unsupported field %q", trimmed)
			}
			if strings.TrimSpace(value) == "" || strings.TrimSpace(value) == "#" ||
				strings.TrimSpace(value) == "null" || strings.TrimSpace(value) == "~" {
				return frontMatter, fmt.Errorf("metadata.type is empty")
			}
			if frontMatter.metadataType != "" {
				return frontMatter, fmt.Errorf("metadata.type is duplicated")
			}
			frontMatter.metadataType = strings.TrimSpace(value)
		default:
			return frontMatter, fmt.Errorf("unexpected indented front matter line %q", line)
		}
	}

	var ok bool
	if frontMatter.name, ok = topLevel["name"]; !ok || frontMatter.name == "" {
		return frontMatter, fmt.Errorf("name is empty or missing")
	}
	if frontMatter.description, ok = topLevel["description"]; !ok || frontMatter.description == "" {
		return frontMatter, fmt.Errorf("description is empty or missing")
	}
	if !strings.HasPrefix(frontMatter.description, "Use when") {
		return frontMatter, fmt.Errorf("description must begin with %q", "Use when")
	}
	if frontMatter.version, ok = topLevel["version"]; !ok || frontMatter.version == "" {
		return frontMatter, fmt.Errorf("version is empty or missing")
	}
	if frontMatter.lastUpdated, ok = topLevel["last_updated"]; !ok || frontMatter.lastUpdated == "" {
		return frontMatter, fmt.Errorf("last_updated is empty or missing")
	}
	if len(frontMatter.tags) == 0 {
		return frontMatter, fmt.Errorf("tags is empty or missing")
	}
	if frontMatter.metadataType != "reference" {
		return frontMatter, fmt.Errorf("metadata.type must equal %q", "reference")
	}
	if frontMatter.name != packageName {
		return frontMatter, fmt.Errorf("name %q does not match directory %q", frontMatter.name, packageName)
	}

	return frontMatter, nil
}

func factoryDocumentationReadFile(t *testing.T, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(RepoRoot(), relative))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(data)
}

func factoryCanonicalSkillPackages(t *testing.T) []string {
	t.Helper()

	skillsDir := filepath.Join(RepoRoot(), "docs", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read canonical skills directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("canonical skills directory is empty")
	}

	indexFound := false
	var packages []string
	for _, entry := range entries {
		if entry.Name() == "index.md" {
			if entry.IsDir() {
				t.Fatal("docs/skills/index.md is a directory")
			}
			indexFound = true
			continue
		}
		if !entry.IsDir() {
			t.Fatalf("docs/skills contains unexpected non-package entry %q", entry.Name())
		}

		skillPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		info, err := os.Stat(skillPath)
		if err != nil {
			t.Fatalf("canonical package %q is missing SKILL.md: %v", entry.Name(), err)
		}
		if info.IsDir() {
			t.Fatalf("canonical package %q has a directory named SKILL.md", entry.Name())
		}

		content, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("read canonical package %q: %v", entry.Name(), err)
		}
		if _, err := factoryParseSkillFrontMatter(entry.Name(), string(content)); err != nil {
			t.Fatalf("canonical package %q has invalid front matter: %v", entry.Name(), err)
		}
		packages = append(packages, entry.Name())
	}

	if !indexFound {
		t.Fatal("canonical skills directory is missing index.md")
	}
	if len(packages) == 0 {
		t.Fatal("canonical skills directory contains no packages")
	}
	if len(packages) != factoryCanonicalSkillPackageCount {
		t.Fatalf("canonical skills directory contains %d packages, want exactly %d", len(packages), factoryCanonicalSkillPackageCount)
	}
	sort.Strings(packages)
	return packages
}

func factoryMarkdownFenceMarker(line string) byte {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0
	}
	run := 1
	for run < len(line) && line[run] == line[0] {
		run++
	}
	if run < 3 {
		return 0
	}
	return line[0]
}

var factoryMarkdownInlineLinkPattern = regexp.MustCompile(`\[[^\]\n]+\]\(([^)\s]+)(\s+("[^"]*"|'[^']*'|\([^)]*\)))?\)`)

func factoryMarkdownInlineCodeAt(line string, offset int) bool {
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}

		run := 1
		for i+run < len(line) && line[i+run] == '`' {
			run++
		}
		delimiter := line[i : i+run]
		closeOffset := strings.Index(line[i+run:], delimiter)
		if closeOffset < 0 {
			return offset >= i
		}

		end := i + run + closeOffset + run
		if offset >= i && offset < end {
			return true
		}
		i = end
	}
	return false
}

func factoryMarkdownInlineLinkTargets(markdown string) []string {
	var targets []string
	fenceMarker := byte(0)
	for _, line := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if fenceMarker != 0 {
			if factoryMarkdownFenceMarker(trimmed) == fenceMarker {
				fenceMarker = 0
			}
			continue
		}
		if marker := factoryMarkdownFenceMarker(trimmed); marker != 0 {
			fenceMarker = marker
			continue
		}
		for _, match := range factoryMarkdownInlineLinkPattern.FindAllStringSubmatchIndex(line, -1) {
			if factoryMarkdownInlineCodeAt(line, match[0]) {
				continue
			}
			targets = append(targets, line[match[2]:match[3]])
		}
	}
	return targets
}

func TestFactoryDocumentationContract(t *testing.T) {
	t.Run("canonical skill packages", func(t *testing.T) {
		factoryCanonicalSkillPackages(t)
	})

	t.Run("catalog is exhaustive", func(t *testing.T) {
		packages := factoryCanonicalSkillPackages(t)
		index := factoryDocumentationReadFile(t, filepath.Join("docs", "skills", "index.md"))

		canonicalTargets := make(map[string]struct{}, len(packages))
		for _, packageName := range packages {
			canonicalTargets[packageName+"/SKILL.md"] = struct{}{}
		}

		catalogTargets := make(map[string]int)
		for _, target := range factoryMarkdownInlineLinkTargets(index) {
			catalogTargets[target]++
		}

		for _, packageName := range packages {
			relative := packageName + "/SKILL.md"
			links := catalogTargets[relative]
			switch links {
			case 0:
				t.Errorf("package %q is missing its catalog link", packageName)
			case 1:
			default:
				t.Errorf("package %q has %d catalog links, want exactly one", packageName, links)
			}
		}

		catalogTargetNames := make([]string, 0, len(catalogTargets))
		for target := range catalogTargets {
			catalogTargetNames = append(catalogTargetNames, target)
		}
		sort.Strings(catalogTargetNames)
		for _, target := range catalogTargetNames {
			if _, ok := canonicalTargets[target]; !ok {
				t.Errorf("catalog contains unknown link target %q", target)
			}
		}

		for _, required := range []string{"factory-onboarding", "skill-improvement"} {
			found := false
			for _, packageName := range packages {
				if packageName == required {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("required factory package %q is missing", required)
			}
		}
	})

	t.Run("legacy aliases resolve locally", func(t *testing.T) {
		root := RepoRoot()
		aliasDir := filepath.Join(root, "docs", "agents", "skills")
		canonicalDir := filepath.Join(root, "docs", "skills")
		entries, err := os.ReadDir(aliasDir)
		if err != nil {
			t.Fatalf("read legacy skills directory: %v", err)
		}

		discovered := make(map[string]struct{})
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".md" {
				discovered[entry.Name()] = struct{}{}
			}
		}

		for name := range expectedLegacySkillAliases {
			if _, ok := discovered[name]; !ok {
				t.Errorf("missing expected legacy alias %q", name)
			}
		}
		for name := range discovered {
			if _, ok := expectedLegacySkillAliases[name]; !ok {
				t.Errorf("unexpected legacy alias %q", name)
			}
		}

		aliasNames := make([]string, 0, len(expectedLegacySkillAliases))
		for name := range expectedLegacySkillAliases {
			aliasNames = append(aliasNames, name)
		}
		sort.Strings(aliasNames)
		for _, name := range aliasNames {
			aliasPath := filepath.Join(aliasDir, name)
			info, err := os.Lstat(aliasPath)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatalf("stat legacy alias %q: %v", name, err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("legacy alias %q is not a symbolic link", name)
				continue
			}

			target, err := os.Readlink(aliasPath)
			if err != nil {
				t.Fatalf("read legacy alias %q: %v", name, err)
			}
			if filepath.IsAbs(target) {
				t.Fatalf("legacy alias %q uses an absolute target %q", name, target)
			}
			if want := expectedLegacySkillAliases[name]; target != want {
				t.Errorf("legacy alias %q targets %q, want %q", name, target, want)
			}

			resolved, err := filepath.EvalSymlinks(aliasPath)
			if err != nil {
				t.Fatalf("resolve legacy alias %q: %v", name, err)
			}
			relative, err := filepath.Rel(canonicalDir, resolved)
			if err != nil || filepath.IsAbs(relative) ||
				relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
				t.Fatalf("legacy alias %q resolves outside docs/skills: %s", name, resolved)
			}
			if filepath.Base(resolved) != "SKILL.md" {
				t.Fatalf("legacy alias %q resolves to %q, want a SKILL.md file", name, resolved)
			}
			resolvedInfo, err := os.Stat(resolved)
			if err != nil {
				t.Fatalf("stat resolved legacy alias %q: %v", name, err)
			}
			if resolvedInfo.IsDir() {
				t.Fatalf("legacy alias %q resolves to directory %q", name, resolved)
			}
		}
	})

	t.Run("factory entry points agree", func(t *testing.T) {
		root := RepoRoot()
		localSkillRouter := factoryDocumentationReadFile(t, filepath.Join("docs", "SKILL.md"))
		routerTargets := factoryMarkdownInlineLinkTargets(localSkillRouter)
		catalogPath := filepath.Join(root, "docs", "skills", "index.md")
		catalogInfo, err := os.Stat(catalogPath)
		if err != nil {
			t.Fatalf("stat canonical skill catalog: %v", err)
		}
		if catalogInfo.IsDir() {
			t.Fatal("canonical skill catalog is a directory")
		}
		foundCatalogLink := false
		for _, target := range routerTargets {
			if target != "skills/index.md" {
				continue
			}
			foundCatalogLink = true
			resolved := filepath.Clean(filepath.Join(filepath.Dir(filepath.Join(root, "docs", "SKILL.md")), filepath.FromSlash(target)))
			if resolved != catalogPath {
				t.Errorf("docs/SKILL.md link %q resolves to %q, want %q", target, resolved, catalogPath)
			}
		}
		if !foundCatalogLink {
			t.Error("docs/SKILL.md does not contain the exact local link target skills/index.md")
		}

		requiredCommonLinks := []string{
			"https://github.com/projectbluefin/common/blob/main/docs/factory/agentic-model.md",
			"https://github.com/projectbluefin/common/blob/main/docs/skills/factory-onboarding.md",
			"https://github.com/projectbluefin/common/blob/main/docs/skills/human-gates.md",
			"https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md",
			"https://github.com/projectbluefin/common/blob/main/docs/skills/skill-improvement.md",
		}
		for _, relative := range []string{"AGENTS.md", filepath.Join("docs", "factory", "README.md")} {
			document := factoryDocumentationReadFile(t, relative)
			targets := factoryMarkdownInlineLinkTargets(document)
			for _, required := range requiredCommonLinks {
				found := false
				for _, target := range targets {
					if target == required {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s does not link %q", relative, required)
				}
			}
		}

		agents := factoryDocumentationReadFile(t, "AGENTS.md")
		for _, required := range []string{"docs/skills/", "skill-improvement"} {
			if !strings.Contains(agents, required) {
				t.Errorf("AGENTS.md does not name %q", required)
			}
		}
	})

	t.Run("retired Frostyard skills are absent", func(t *testing.T) {
		for _, relative := range []string{
			filepath.Join("docs", "agents", "skills"),
			filepath.Join("docs", "skills"),
		} {
			root := filepath.Join(RepoRoot(), relative)
			err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if path != root && strings.HasPrefix(info.Name(), "frostyard-") {
					t.Errorf("retired Frostyard skill path remains: %s", filepath.Join(relative, strings.TrimPrefix(path, root+string(os.PathSeparator))))
					if info.IsDir() {
						return filepath.SkipDir
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk %s: %v", relative, err)
			}
		}
	})
}
