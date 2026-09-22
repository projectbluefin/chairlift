package installcheck

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/agentmode"
)

// agentModeADR is the decision record this gate holds the rest of the
// repository to. It is named once so a superseding record is a one-line
// change here plus the prose updates the other subtests demand.
const agentModeADR = "0013-agent-mode-architecture-and-state-contract.md"

// collapseWhitespace folds every run of whitespace to a single space, so a
// table cell that a Markdown renderer wraps across two source lines still
// compares equal to the one-line string it came from. Both sides of every
// comparison below go through it.
func collapseWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func readCollapsed(t *testing.T, relative string) string {
	t.Helper()
	return collapseWhitespace(readRepoFile(t, relative))
}

// stripInlineCode removes the Markdown inline-code backticks a table cell uses
// to set off an identifier the package spells bare. The record's cells and the
// package's strings are compared for equality after this, so a cell may
// decorate `ai_group` or `llmman` without that decoration reading as a
// disagreement about the policy itself. Nothing else is normalized: the words
// have to match.
func stripInlineCode(text string) string {
	return collapseWhitespace(strings.ReplaceAll(text, "`", ""))
}

// TestAgentModeContractMatchesTheDecisionRecord is the half of ADR-0013 that
// is not prose.
//
// The decision record and internal/agentmode carry the same facts in two
// forms a human and a machine can each read, which is exactly the arrangement
// that drifts: the package changes and the record does not, and the record is
// what a reviewer reads before approving the change. So the record is checked
// against the package rather than against a second copy of its own text —
// every state, artifact, cleanup policy, and security rule the package
// declares must appear in the record, and the record may not disagree about
// what the dispatcher's predicate is.
func TestAgentModeContractMatchesTheDecisionRecord(t *testing.T) {
	adr := readCollapsed(t, filepath.Join("docs", "adr", agentModeADR))
	adrProse := stripInlineCode(adr)

	t.Run("every state is defined", func(t *testing.T) {
		for _, state := range agentmode.AllStates() {
			if !strings.Contains(adr, "`"+string(state)+"`") {
				t.Errorf("ADR-0013 does not define the %q state; the surface has no state the record does not name", state)
			}
		}
	})

	t.Run("every artifact is owned with its cleanup policy", func(t *testing.T) {
		for _, artifact := range agentmode.Artifacts() {
			if !strings.Contains(adr, "`"+artifact.Name+"`") {
				t.Errorf("ADR-0013 does not name the %q artifact", artifact.Name)
			}
			if !strings.Contains(adrProse, collapseWhitespace(artifact.Cleanup)) {
				t.Errorf("ADR-0013 does not state the cleanup policy for %q:\n%s", artifact.Name, artifact.Cleanup)
			}
			if artifact.RepoPath != "" && !strings.Contains(adr, "`"+artifact.RepoPath+"`") {
				t.Errorf("ADR-0013 does not cite %s for artifact %q", artifact.RepoPath, artifact.Name)
			}
			if !strings.Contains(adr, artifact.Issue) {
				t.Errorf("ADR-0013 does not name slice %s as the owner of %q", artifact.Issue, artifact.Name)
			}
		}
	})

	t.Run("every security boundary is stated", func(t *testing.T) {
		for _, boundary := range agentmode.Boundaries() {
			if !strings.Contains(adr, "`"+boundary.Name+"`") {
				t.Errorf("ADR-0013 does not name the %q security boundary", boundary.Name)
			}
			if !strings.Contains(adrProse, collapseWhitespace(boundary.Rule)) {
				t.Errorf("ADR-0013 does not state the %q boundary's rule:\n%s", boundary.Name, boundary.Rule)
			}
		}
	})

	t.Run("the dispatcher predicate is the one the package implements", func(t *testing.T) {
		for _, term := range []string{
			"local endpoint healthy",
			"active model available",
			"Jan integration configured",
		} {
			if !strings.Contains(adr, term) {
				t.Errorf("ADR-0013 does not state the readiness predicate's %q term", term)
			}
		}

		// The record must keep the two predicates apart. Collapsing them is
		// the rewrite the package's own test forbids, so the record has to
		// say so rather than leave a reader to infer it.
		if !strings.Contains(adr, "not the same one") {
			t.Error("ADR-0013 no longer states that Agent Mode readiness and the Ask Bluefin predicate are different predicates")
		}
	})

	t.Run("the issue map is complete and closed", func(t *testing.T) {
		inMap := make(map[string]bool)
		for _, slice := range agentmode.WorkMap() {
			inMap[slice.Ref] = true
			if !strings.Contains(adr, slice.Ref) {
				t.Errorf("ADR-0013 does not name slice %s", slice.Ref)
			}
		}

		// A bare "#NNN" is a reference to this repository. Any one of them
		// outside the map is a child issue the record cites without the map
		// covering it, which is how a renumbered or dropped slice survives
		// review.
		bare := regexp.MustCompile(`(?:^|[^/\w])#(\d+)`)
		for _, match := range bare.FindAllStringSubmatch(readRepoFile(t, filepath.Join("docs", "adr", agentModeADR)), -1) {
			ref := "#" + match[1]
			if ref == "#252" { // the epic this record derives from
				continue
			}
			if !inMap[ref] {
				t.Errorf("ADR-0013 cites %s, which is not in the issue map", ref)
			}
		}
	})
}

// TestCurrentStateDocsDescribeAgentMode requires every document a user or
// contributor reads to name the feature the epic is building, and the two
// architecture-facing ones to route the reader to the decision record. A
// current-state document that describes only the superseded Local AI
// implementation tells a reader the repository ships something it is removing.
func TestCurrentStateDocsDescribeAgentMode(t *testing.T) {
	for _, path := range []string{
		"README.md",
		"AGENTS.md",
		"CONFIG.md",
		filepath.Join("docs", "index.md"),
		filepath.Join("docs", "reference.md"),
		filepath.Join("docs", "design", "overview.md"),
	} {
		t.Run(path, func(t *testing.T) {
			if document := readRepoFile(t, path); !strings.Contains(document, "Agent Mode") {
				t.Errorf("%s does not mention Agent Mode", path)
			}
		})
	}

	for _, path := range []string{
		"AGENTS.md",
		filepath.Join("docs", "design", "overview.md"),
	} {
		t.Run(path+" cites the decision record", func(t *testing.T) {
			document := readRepoFile(t, path)
			if !strings.Contains(document, agentModeADR) && !strings.Contains(document, "ADR-0013") {
				t.Errorf("%s does not cite ADR-0013, so its Agent Mode claims have no authority behind them", path)
			}
		})
	}
}

// TestRamaLamaClaimsCarryTheSupersedingDecision is the derived half of the
// cutover.
//
// ADR-0013 removes the RamaLama-backed Local AI implementation without
// promising a compatibility path, but the code it removes is still in the
// tree until #256 lands, so a current-state document may still have to
// describe it. The rule this gate enforces is that it may not do so *alone*:
// any mention of the superseded runtime must sit in a document that also
// cites the decision superseding it. The alternative — a list of forbidden
// sentences — only ever covers the drift someone already noticed.
func TestRamaLamaClaimsCarryTheSupersedingDecision(t *testing.T) {
	for _, document := range currentStateDocuments(t) {
		// The decision record is the superseding authority, so it is the one
		// document that cannot cite itself: its own account of what RamaLama
		// was is the thing every other document is being routed to.
		if filepath.Base(document) == agentModeADR {
			continue
		}
		text := readRepoFile(t, document)
		lowered := strings.ToLower(text)
		mentions := strings.Contains(lowered, "ramalama")
		if !mentions {
			continue
		}
		if !strings.Contains(text, agentModeADR) && !strings.Contains(text, "ADR-0013") {
			t.Errorf("%s describes the superseded RamaLama runtime without citing ADR-0013; "+
				"either route the reader to the decision that supersedes it or stop making the claim", document)
		}
	}
}

// TestRemovedLocalAIClaimsStayRemoved names the specific sentences ADR-0013
// replaces. The derived gate above catches a RamaLama claim with no decision
// beside it; this one catches a claim that keeps the decision's citation while
// still selling the removed runtime as the shipped architecture.
func TestRemovedLocalAIClaimsStayRemoved(t *testing.T) {
	for _, path := range []string{
		"AGENTS.md",
		"CONFIG.md",
		filepath.Join("docs", "reference.md"),
		filepath.Join("docs", "design", "overview.md"),
		filepath.Join("docs", "index.md"),
		"README.md",
	} {
		t.Run(path, func(t *testing.T) {
			document := readCollapsed(t, path)
			for _, stale := range []string{
				"ships one runtime (RamaLama)",
				"RamaLama publishes a per-accelerator image",
				"quay.io/ramalama",
				"Runs a language model in a rootless container on detected hardware",
				"Local AI language model served in rootless Quadlet/Podman container",
			} {
				if strings.Contains(document, stale) {
					t.Errorf("%s still contains the superseded Local AI claim %q", path, stale)
				}
			}
		})
	}
}
