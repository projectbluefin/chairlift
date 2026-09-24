package installcheck

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// releaseScreenshotsWorkflow is the automation that lands regenerated
// screenshots after a release. Branch protection with a merge queue rejects
// direct pushes to main (GH013: "Changes must be made through the merge
// queue"), so this workflow must ship its result as a pull request instead.
// These tests pin that contract so the push-to-main failure mode
// (projectbluefin/chairlift#312) cannot silently return.
func TestReleaseScreenshotsLandsAsPullRequest(t *testing.T) {
	path := filepath.Join(".github", "workflows", "release-screenshots.yml")
	workflow := readRepoFile(t, path)

	var config struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string            `yaml:"name"`
				Run  string            `yaml:"run"`
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflow), &config); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	job, ok := config.Jobs["capture"]
	if !ok {
		t.Fatalf("%s does not define the capture job", path)
	}

	// No run: step may push to main. The "Switch to main" step legitimately
	// checks out main locally to apply the regenerated PNGs onto current
	// main's tree; that checkout must stay a checkout, not become a push.
	pushToMain := regexp.MustCompile(`(?m)^\s*git\s+push\b[^|;&]*(\brefs/heads/main\b|\bdirect[^\n]*main\b|origin\s+main\b)`)
	for _, step := range job.Steps {
		if step.Run == "" {
			continue
		}
		if pushToMain.MatchString(step.Run) {
			t.Errorf("step %q pushes to main; branch protection requires the merge queue — land the result as a pull request instead", step.Name)
		}
	}

	// The landing step must open a pull request with a commit-SHA-pinned
	// create-pull-request action (the SHA half is enforced repo-wide by
	// TestWorkflowActionsUseImmutableCommitSHAs; assert the action identity
	// here so this test still names the contract if the workflow is
	// restructured).
	var hasCreatePullRequest, hasBranchInput, hasBaseInput bool
	for _, step := range job.Steps {
		if !strings.HasPrefix(step.Uses, "peter-evans/create-pull-request@") {
			continue
		}
		hasCreatePullRequest = true
		if step.With["branch"] == "release-screenshots" {
			hasBranchInput = true
		}
		if step.With["base"] == "main" {
			hasBaseInput = true
		}
	}
	if !hasCreatePullRequest {
		t.Fatalf("%s never opens a pull request with peter-evans/create-pull-request", path)
	}
	if !hasBranchInput {
		t.Errorf("create-pull-request step does not target a dedicated release-screenshots branch; the PR branch must not be main")
	}
	if !hasBaseInput {
		t.Errorf("create-pull-request step does not set base: main, so the refreshed screenshots may not land on the default branch")
	}

	// The user-facing tour documents the same mechanism; keep both claims
	// from drifting apart.
	walkthrough := readRepoFile(t, filepath.Join("docs", "walkthrough.md"))
	for _, required := range []string{
		"opens a pull request with any changed PNGs",
		"merge queue",
	} {
		if !strings.Contains(walkthrough, required) {
			t.Errorf("docs/walkthrough.md does not state that screenshots land via a pull request (%q missing)", required)
		}
	}
	if strings.Contains(walkthrough, "commits any changed PNGs to `main`") {
		t.Errorf("docs/walkthrough.md still claims a direct commit to main, which branch protection rejects")
	}
}
