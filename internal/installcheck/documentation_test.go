package installcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/config"
	"gopkg.in/yaml.v3"
)

func readRepoFile(t *testing.T, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(RepoRoot(), relative))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(data)
}

func TestCurrentDocumentationMatchesSourceFacts(t *testing.T) {
	t.Run("updex version comes from go.mod", func(t *testing.T) {
		goMod := readRepoFile(t, "go.mod")
		match := regexp.MustCompile(`(?m)^\s*github\.com/frostyard/updex\s+(v\S+)`).FindStringSubmatch(goMod)
		if len(match) != 2 {
			t.Fatal("go.mod does not contain a parseable github.com/frostyard/updex version")
		}
		overview := readRepoFile(t, filepath.Join("docs", "design", "overview.md"))
		want := "currently pinned to " + match[1] + " in go.mod"
		if !strings.Contains(overview, want) {
			t.Errorf("docs/design/overview.md does not contain %q", want)
		}
	})

	t.Run("bootc event contract has no duplicate error event", func(t *testing.T) {
		for _, path := range []string{
			filepath.Join("internal", "bootc", "stage.go"),
			filepath.Join("internal", "views", "updates_page.go"),
			filepath.Join("docs", "design", "overview.md"),
			filepath.Join("docs", "design", "package-managers.md"),
		} {
			if strings.Contains(readRepoFile(t, path), "EventError") {
				t.Errorf("%s still documents or implements removed EventError", path)
			}
		}
	})

	t.Run("historical port guide is marked and non-runnable", func(t *testing.T) {
		guide := readRepoFile(t, "README-go-port.md")
		for _, required := range []string{
			"**Historical document.**",
			"Do not use build commands from this historical proposal.",
			"[README.md](README.md#building-from-source)",
		} {
			if !strings.Contains(guide, required) {
				t.Errorf("README-go-port.md does not contain %q", required)
			}
		}
		for _, stale := range []string{
			"Go 1.22",
			"\ncd go\n",
			"make deps",
			"go mod download",
			"\ngo/\n",
		} {
			if strings.Contains(guide, stale) {
				t.Errorf("README-go-port.md still contains stale guidance %q", stale)
			}
		}
	})

	t.Run("configuration fallback wording is consistent", func(t *testing.T) {
		for _, path := range []string{
			"README.md",
			"CONFIG.md",
			filepath.Join("docs", "reference.md"),
		} {
			document := readRepoFile(t, path)
			for _, fact := range []string{
				"beside the",
				"executable",
				"current working",
				"development fallback",
			} {
				if !strings.Contains(document, fact) {
					t.Errorf("%s does not describe %q", path, fact)
				}
			}
		}
	})

	t.Run("optional visibility and install prefix are current", func(t *testing.T) {
		index := readRepoFile(t, filepath.Join("docs", "index.md"))
		if strings.Contains(index, "Groups for unavailable tools are hidden automatically") {
			t.Error("docs/index.md retains the false uniform runtime-visibility claim")
		}
		if !strings.Contains(index, "default `/usr`") {
			t.Error("docs/index.md does not document the /usr install default")
		}
	})

	t.Run("privileged integration inventory is complete", func(t *testing.T) {
		ubluePolicy := readRepoFile(t, filepath.Join("data", "io.projectbluefin.chairlift.ublue.policy"))
		if got := strings.Count(ubluePolicy, `<action id="io.projectbluefin.chairlift.ublue.`); got != 9 {
			t.Fatalf("ublue policy actions = %d, want 9", got)
		}

		current := strings.Join([]string{
			readRepoFile(t, "README.md"),
			readRepoFile(t, "AGENTS.md"),
			readRepoFile(t, filepath.Join("docs", "index.md")),
			readRepoFile(t, filepath.Join("docs", "adr", "0006-split-system-integration-package-with-mutual-conflicts.md")),
			readRepoFile(t, filepath.Join("docs", "design", "overview.md")),
			readRepoFile(t, filepath.Join("docs", "design", "package-managers.md")),
		}, "\n")

		for _, required := range []string{
			"/usr/bin/chairlift-updex-helper",
			"/usr/bin/chairlift-ublue-helper",
			"/usr/share/polkit-1/actions/io.projectbluefin.chairlift.bootc.policy",
			"/usr/share/polkit-1/actions/io.projectbluefin.chairlift.sysupdate.policy",
			"/usr/share/polkit-1/actions/io.projectbluefin.chairlift.updex.policy",
			"/usr/share/polkit-1/actions/io.projectbluefin.chairlift.ublue.policy",
			"/usr/share/chairlift/config.yml",
			"/usr/share/doc/chairlift/channels.example.yml",
			"nine actions",
			"factory-reset",
		} {
			if !strings.Contains(current, required) {
				t.Errorf("current documentation does not contain %q", required)
			}
		}

		for _, stale := range []string{
			"all three PolicyKit policies",
			"all three policies",
			"the three PolicyKit policies",
			"eight subcommands",
			"declaring the three actions",
			"builds two binaries",
			"chairlift-updex-helper` only",
		} {
			if strings.Contains(current, stale) {
				t.Errorf("current documentation still contains stale privileged-inventory claim %q", stale)
			}
		}
	})

	t.Run("public metrics catalog stays auditable", func(t *testing.T) {
		catalog := strings.Join(strings.Fields(readRepoFile(t, filepath.Join("docs", "metrics", "README.md"))), " ")
		for _, required := range []string{
			"../metrics.md",
			"actions/workflows/test.yml",
			"actions/workflows/nightly-compliance.yml",
			"app.codecov.io/gh/projectbluefin/chairlift",
			"https://api.github.com/repos/projectbluefin/chairlift",
			"does not currently attach a reliable provenance marker",
			"does not collect application usage telemetry",
		} {
			if !strings.Contains(catalog, required) {
				t.Errorf("docs/metrics/README.md does not contain %q", required)
			}
		}
	})

	t.Run("operational commands target canonical repo", func(t *testing.T) {
		metricsDoc := readRepoFile(t, filepath.Join("docs", "metrics.md"))
		if !strings.Contains(metricsDoc, "--repo projectbluefin/chairlift") {
			t.Error("docs/metrics.md does not target projectbluefin/chairlift")
		}
		if strings.Contains(metricsDoc, "frostyard/chairlift") {
			t.Error("docs/metrics.md still references frostyard/chairlift")
		}

		qualityDoc := readRepoFile(t, filepath.Join("docs", "quality.md"))
		if !strings.Contains(qualityDoc, "gh secret set ANTHROPIC_API_KEY --repo projectbluefin/chairlift") {
			t.Error("docs/quality.md does not target projectbluefin/chairlift for ANTHROPIC_API_KEY secret")
		}
		if strings.Contains(qualityDoc, "--repo frostyard/chairlift") {
			t.Error("docs/quality.md still references --repo frostyard/chairlift")
		}

		metricsReadme := readRepoFile(t, filepath.Join("docs", "metrics", "README.md"))
		if strings.Contains(metricsReadme, "frostyard/chairlift") {
			t.Error("docs/metrics/README.md still references frostyard/chairlift")
		}
	})

	t.Run("known stale claims stay removed", func(t *testing.T) {
		current := strings.Join([]string{
			readRepoFile(t, "README.md"),
			readRepoFile(t, "CONFIG.md"),
			readRepoFile(t, filepath.Join("docs", "index.md")),
			readRepoFile(t, filepath.Join("docs", "reference.md")),
		}, "\n")
		for _, stale := range []string{
			"updates_status_group",
			"Help page coming soon",
			"Help is coming soon",
			"Groups for unavailable tools are hidden automatically",
			"all features default to enabled, except\n`maintenance_cleanup_group`, which defaults to disabled",
			"all features default to enabled except\n`maintenance_cleanup_group`, which defaults to disabled",
			"all groups are enabled except\n`maintenance_cleanup_group`.",
			"every group except `maintenance_cleanup_group`, which\n    defaults to `false`",
		} {
			if strings.Contains(current, stale) {
				t.Errorf("current documentation still contains stale claim %q", stale)
			}
		}
	})
}

func TestDocumentedConfigInventoryMatchesCanonicalSchema(t *testing.T) {
	pages, err := config.SchemaPages()
	if err != nil {
		t.Fatalf("config.SchemaPages(): %v", err)
	}

	for _, docName := range []string{"CONFIG.md", filepath.Join("docs", "reference.md")} {
		t.Run(docName, func(t *testing.T) {
			content := readRepoFile(t, docName)
			for _, page := range pages {
				groups, err := config.SchemaGroups(page)
				if err != nil {
					t.Fatalf("config.SchemaGroups(%q): %v", page, err)
				}
				for _, group := range groups {
					if !strings.Contains(content, "`"+group+"`") {
						t.Errorf("%s does not document canonical group %s.%s", docName, page, group)
					}
				}
			}
		})
	}
}

func TestDocumentedDefaultsMatchCanonicalSchema(t *testing.T) {
	for _, docName := range []string{
		"CONFIG.md",
		filepath.Join("docs", "reference.md"),
		filepath.Join("docs", "design", "overview.md"),
	} {
		t.Run(docName, func(t *testing.T) {
			content := readRepoFile(t, docName)
			for _, required := range []string{
				"maintenance_cleanup_group",
				"reset_group",
			} {
				if !strings.Contains(content, required) {
					t.Errorf("%s does not mention %s", docName, required)
				}
			}
		})
	}
}

func TestDocumentedOptionalFieldsCoverCanonicalGroupFields(t *testing.T) {
	fields, err := config.SchemaGroupFields()
	if err != nil {
		t.Fatalf("config.SchemaGroupFields(): %v", err)
	}

	configDoc := readRepoFile(t, "CONFIG.md")
	for _, field := range fields {
		if field == "enabled" {
			continue // handled separately in docs
		}
		if !strings.Contains(configDoc, "`"+field+"`") {
			t.Errorf("CONFIG.md does not document optional field %q", field)
		}
	}
}

func TestAIFixRequestedWorkflowIsLabelScoped(t *testing.T) {
	path := filepath.Join(".github", "workflows", "ai-fix-requested.yml")
	workflow := readRepoFile(t, path)

	var document yaml.Node
	if err := yaml.Unmarshal([]byte(workflow), &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, required := range []string{
		"issues:",
		"types: [labeled]",
		"issues: write",
		"github.event.issue.state == 'open'",
		"github.event.label.name == 'ai-fix-requested'",
		"actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd",
		"<!-- ai-fix-requested:${issueNumber} -->",
		"comments.some((comment) => comment.body?.includes(marker))",
		"@copilot Please implement",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("%s does not contain required contract %q", path, required)
		}
	}

	for _, unsafe := range []string{
		"pull_request_target:",
		"actions/checkout@",
		"github.event.issue.body",
		"github.event.issue.title",
		"context.payload.issue.body",
		"context.payload.issue.title",
		"contents: write",
		"\n        run:",
	} {
		if strings.Contains(workflow, unsafe) {
			t.Errorf("%s contains unsafe or unnecessary workflow surface %q", path, unsafe)
		}
	}

	quality := readRepoFile(t, filepath.Join("docs", "quality.md"))
	if !strings.Contains(quality, "`.github/workflows/ai-fix-requested.yml`") {
		t.Error("docs/quality.md does not document the AI-fix-requested workflow")
	}
}

func TestCopilotReviewApplyWorkflowContract(t *testing.T) {
	path := filepath.Join(".github", "workflows", "copilot-review-apply.yml")
	workflow := readRepoFile(t, path)

	for _, required := range []string{
		"pull_request_review:",
		"types: [submitted]",
		"contents: read",
		"pull-requests: write",
		"github.event.pull_request.state == 'open'",
		"!github.event.pull_request.draft",
		"actions/github-script@",
		"github.rest.issues.createComment",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("%s does not contain required contract %q", path, required)
		}
	}

	for _, unsafe := range []string{
		"pull_request_target:",
		"actions/checkout@",
		"github.event.review.body",
		"context.payload.review.body",
		"contents: write",
		"issues: write",
		"\n        run:",
	} {
		if strings.Contains(workflow, unsafe) {
			t.Errorf("%s contains unsafe or unnecessary workflow surface %q", path, unsafe)
		}
	}

	quality := readRepoFile(t, filepath.Join("docs", "quality.md"))
	if !strings.Contains(quality, "`.github/workflows/copilot-review-apply.yml`") {
		t.Error("docs/quality.md does not document the Copilot review apply workflow")
	}
	if !strings.Contains(quality, "The review workflow receives read-only contents and pull-requests") {
		t.Error("docs/quality.md does not document the review workflow's pull-requests write permission")
	}
	if strings.Contains(quality, "issues write, and pull-requests") {
		t.Error("docs/quality.md still claims the review workflow holds issues write permission")
	}
}
