package installcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var commitActionPattern = regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)

func isImmutableActionReference(ref string) bool {
	return strings.HasPrefix(ref, "./") || commitActionPattern.MatchString(ref)
}

func TestActionReferenceClassification(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name string
		ref  string
		want bool
	}{
		{name: "commit pinned action", ref: "actions/checkout@" + sha, want: true},
		{name: "commit pinned nested action", ref: "frostyard/repogen/.github/actions/publish-to-r2@" + sha, want: true},
		{name: "local action", ref: "./.github/actions/check", want: true},
		{name: "version tag", ref: "actions/checkout@v6", want: false},
		{name: "branch", ref: "frostyard/repogen/.github/actions/publish-to-r2@main", want: false},
		{name: "short commit", ref: "actions/checkout@deadbeef", want: false},
		{name: "expression", ref: "actions/checkout@${{inputs.ref}}", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isImmutableActionReference(tc.ref); got != tc.want {
				t.Errorf("isImmutableActionReference(%q) = %v, want %v", tc.ref, got, tc.want)
			}
		})
	}
}

func TestWorkflowActionsUseImmutableCommitSHAs(t *testing.T) {
	workflowDir := filepath.Join(RepoRoot(), ".github", "workflows")
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		t.Fatalf("read workflow directory: %v", err)
	}

	workflowCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		workflowCount++

		path := filepath.Join(workflowDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		var document yaml.Node
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}

		var inspectUses func(*yaml.Node)
		inspectUses = func(node *yaml.Node) {
			if node.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(node.Content); i += 2 {
					key, value := node.Content[i], node.Content[i+1]
					if key.Kind == yaml.ScalarNode && key.Tag == "!!str" && key.Value == "uses" {
						if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
							t.Errorf("%s:%d: uses value must be a string", entry.Name(), value.Line)
						} else if !isImmutableActionReference(value.Value) {
							t.Errorf("%s:%d: external action %q must use a full 40-character commit SHA", entry.Name(), value.Line, value.Value)
						}
					}
					inspectUses(value)
				}
				return
			}
			for _, child := range node.Content {
				inspectUses(child)
			}
		}
		inspectUses(&document)
	}
	if workflowCount == 0 {
		t.Fatal("no workflow files found")
	}
}

func TestWorkflowUsesLeastPrivilege(t *testing.T) {
	path := filepath.Join(".github", "workflows", "test.yml")
	workflow := readRepoFile(t, path)

	var config struct {
		Permissions *map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflow), &config); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if config.Permissions == nil || len(*config.Permissions) != 0 {
		t.Errorf("top-level permissions = %v, want explicit empty permissions", config.Permissions)
	}

	expected := map[string]map[string]string{
		"lint":         {"contents": "read"},
		"unit-test":    {"contents": "read", "id-token": "write"},
		"race-test":    {"contents": "read"},
		"e2e":          {"contents": "read"},
		"verify":       {"contents": "read"},
		"build":        {"contents": "read"},
		"tests-passed": {},
	}
	if len(config.Jobs) != len(expected) {
		t.Errorf("workflow has %d jobs, want %d", len(config.Jobs), len(expected))
	}
	for name, want := range expected {
		job, ok := config.Jobs[name]
		if !ok {
			t.Errorf("workflow does not define %s job", name)
			continue
		}
		if len(job.Permissions) != len(want) {
			t.Errorf("%s permissions = %v, want %v", name, job.Permissions, want)
			continue
		}
		for permission, access := range want {
			if job.Permissions[permission] != access {
				t.Errorf("%s permissions = %v, want %v", name, job.Permissions, want)
				break
			}
		}
	}
}

func TestReleaseWorkflowGatedOnRequiredChecks(t *testing.T) {
	path := filepath.Join(".github", "workflows", "release.yml")
	workflow := readRepoFile(t, path)

	var config struct {
		Permissions *map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Needs       any               `yaml:"needs"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflow), &config); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if config.Permissions == nil || len(*config.Permissions) != 0 {
		t.Errorf("top-level permissions = %v, want explicit empty permissions", config.Permissions)
	}

	gateJob, ok := config.Jobs["gate"]
	if !ok {
		t.Fatal("release.yml must define a 'gate' job")
	}
	if len(gateJob.Permissions) != 1 || gateJob.Permissions["contents"] != "read" {
		t.Errorf("gate job permissions = %v, want contents: read", gateJob.Permissions)
	}
	gateRunsMakeCI := false
	for _, step := range gateJob.Steps {
		if strings.Contains(step.Run, "make ci") {
			gateRunsMakeCI = true
			break
		}
	}
	if !gateRunsMakeCI {
		t.Errorf("gate job steps must execute 'make ci'")
	}

	e2eJob, ok := config.Jobs["e2e"]
	if !ok {
		t.Fatal("release.yml must define an 'e2e' job")
	}
	if len(e2eJob.Permissions) != 1 || e2eJob.Permissions["contents"] != "read" {
		t.Errorf("e2e job permissions = %v, want contents: read", e2eJob.Permissions)
	}
	e2eRunsMakeE2E := false
	for _, step := range e2eJob.Steps {
		if strings.Contains(step.Run, "make e2e") {
			e2eRunsMakeE2E = true
			break
		}
	}
	if !e2eRunsMakeE2E {
		t.Errorf("e2e job steps must execute 'make e2e'")
	}

	goreleaserJob, ok := config.Jobs["goreleaser"]
	if !ok {
		t.Fatal("release.yml must define a 'goreleaser' job")
	}
	if len(goreleaserJob.Permissions) != 1 || goreleaserJob.Permissions["contents"] != "write" {
		t.Errorf("goreleaser job permissions = %v, want contents: write", goreleaserJob.Permissions)
	}

	var needsList []string
	switch v := goreleaserJob.Needs.(type) {
	case string:
		needsList = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				needsList = append(needsList, s)
			}
		}
	}
	hasGate := false
	hasE2E := false
	for _, n := range needsList {
		if n == "gate" {
			hasGate = true
		}
		if n == "e2e" {
			hasE2E = true
		}
	}
	if !hasGate || !hasE2E {
		t.Errorf("goreleaser job needs = %v, want [gate, e2e]", needsList)
	}
}

// mergeQueueGateJob is the aggregating job in test.yml, and
// mergeQueueGateContext is the check-run name it publishes. That name is the
// single required status check on the default-branch ruleset, so renaming
// either one means editing the ruleset in the same change.
const (
	mergeQueueGateJob     = "tests-passed"
	mergeQueueGateContext = "Tests Passed"
)

// testWorkflowTrigger is the subset of one `on:` entry these tests read. A
// bare `merge_group:` parses as null and leaves every field zero, so trigger
// presence is decided by the map key rather than by a non-nil value.
type testWorkflowTrigger struct {
	Types          []string `yaml:"types"`
	Branches       []string `yaml:"branches"`
	BranchesIgnore []string `yaml:"branches-ignore"`
}

// testWorkflowDocument names `on` directly: yaml.v3 resolves the YAML 1.2 core
// schema, in which `on` is an ordinary string rather than a spelling of true,
// so the key survives unmarshaling unchanged.
type testWorkflowDocument struct {
	On   map[string]testWorkflowTrigger `yaml:"on"`
	Jobs map[string]struct {
		Name  string   `yaml:"name"`
		If    string   `yaml:"if"`
		Needs []string `yaml:"needs"`
	} `yaml:"jobs"`
}

func readTestWorkflow(t *testing.T) testWorkflowDocument {
	t.Helper()
	path := filepath.Join(".github", "workflows", "test.yml")
	var document testWorkflowDocument
	if err := yaml.Unmarshal([]byte(readRepoFile(t, path)), &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return document
}

func TestTestsWorkflowRunsForTheMergeQueue(t *testing.T) {
	document := readTestWorkflow(t)

	mergeGroup, ok := document.On["merge_group"]
	if !ok {
		t.Fatal("test.yml declares no merge_group trigger; a queued pull request would merge with no validation of its merge-group head")
	}
	// github.ref for merge_group is refs/heads/gh-readonly-queue/main/pr-<n>-<sha>
	// and never refs/heads/main, so a branch filter naming main matches
	// nothing — it does not narrow this trigger, it removes it.
	if len(mergeGroup.Branches) != 0 || len(mergeGroup.BranchesIgnore) != 0 {
		t.Errorf("merge_group declares branches=%v branches-ignore=%v; filters run against the gh-readonly-queue ref, so this trigger must stay unfiltered",
			mergeGroup.Branches, mergeGroup.BranchesIgnore)
	}

	// Adding the queue must not cost the existing signals.
	for _, trigger := range []string{"push", "pull_request"} {
		existing, ok := document.On[trigger]
		if !ok {
			t.Errorf("test.yml no longer declares the %s trigger", trigger)
			continue
		}
		if len(existing.Branches) != 1 || existing.Branches[0] != "main" {
			t.Errorf("%s branches = %v, want [main]", trigger, existing.Branches)
		}
	}
}

func TestMergeQueueGateWaitsForEveryTestJob(t *testing.T) {
	document := readTestWorkflow(t)

	gate, ok := document.Jobs[mergeQueueGateJob]
	if !ok {
		t.Fatalf("test.yml defines no %s job", mergeQueueGateJob)
	}
	if gate.Name != mergeQueueGateContext {
		t.Errorf("%s name = %q, want %q: the ruleset requires that exact context", mergeQueueGateJob, gate.Name, mergeQueueGateContext)
	}
	// GitHub counts a skipped required check as a passing one, so a gate that
	// runs only when its dependencies succeed reports success for a run whose
	// tests failed.
	if strings.ReplaceAll(gate.If, " ", "") != "always()" {
		t.Errorf("%s if = %q, want always()", mergeQueueGateJob, gate.If)
	}

	needed := make(map[string]bool, len(gate.Needs))
	for _, need := range gate.Needs {
		needed[need] = true
		if _, ok := document.Jobs[need]; !ok {
			t.Errorf("%s needs %q, which test.yml does not define", mergeQueueGateJob, need)
		}
	}
	for name := range document.Jobs {
		if name == mergeQueueGateJob {
			continue
		}
		if !needed[name] {
			t.Errorf("%s does not need job %q, so the queue would merge while %q is failing", mergeQueueGateJob, name, name)
		}
	}

	quality := readRepoFile(t, filepath.Join("docs", "quality.md"))
	if !strings.Contains(quality, mergeQueueGateContext) {
		t.Errorf("docs/quality.md does not name the required %q check", mergeQueueGateContext)
	}
}
