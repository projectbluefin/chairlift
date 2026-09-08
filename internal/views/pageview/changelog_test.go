package pageview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/sbom"
)

func sampleResult() sbom.Result {
	return sbom.Result{
		Upgraded:   []sbom.Change{{Name: "kernel", From: "6.17.4", To: "6.17.9"}},
		Downgraded: []sbom.Change{{Name: "bootc", From: "1.7.0", To: "1.6.2"}},
		Added:      []sbom.Change{{Name: "new-tool", To: "1.0.0"}},
		Removed:    []sbom.Change{{Name: "old-tool", From: "0.4.1"}},
		Changed:    []sbom.Change{{Name: "vendored", From: "aaaa", To: "bbbb"}},
	}
}

func TestChangelogRowSaysWhenThereIsNothingToCompare(t *testing.T) {
	if got := ChangelogRow(false).Subtitle; !strings.Contains(got, "once an update is staged") {
		t.Errorf("subtitle = %q", got)
	}
	if got := ChangelogRow(true).Subtitle; strings.Contains(got, "once an update is staged") {
		t.Errorf("staged subtitle still says there is nothing to compare: %q", got)
	}
}

func TestChangelogSummaryCountsEveryCategory(t *testing.T) {
	summary := ChangelogSummary(sampleResult())

	for _, want := range []string{"1 upgraded", "1 downgraded", "1 added", "1 removed", "1 changed"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q is missing %q", summary, want)
		}
	}
}

func TestChangelogSummaryOfAnIdenticalPairSaysSo(t *testing.T) {
	if got := ChangelogSummary(sbom.Result{}); !strings.Contains(got, "No package differences") {
		t.Errorf("summary = %q", got)
	}
}

func TestChangelogSectionsOmitEmptyCategoriesAndLabelUnknownOrder(t *testing.T) {
	sections := ChangelogSections(sbom.Result{
		Upgraded: []sbom.Change{{Name: "kernel", From: "6.17.4", To: "6.17.9"}},
		Changed:  []sbom.Change{{Name: "vendored", From: "aaaa", To: "bbbb"}},
	})

	if len(sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(sections))
	}
	if sections[0].Title != "Upgraded" {
		t.Errorf("first section = %q, want Upgraded", sections[0].Title)
	}
	if sections[0].Entries[0].Subtitle != "6.17.4 → 6.17.9" {
		t.Errorf("upgrade subtitle = %q", sections[0].Entries[0].Subtitle)
	}
	// A pair whose direction is unknown must not read as an upgrade.
	if !strings.Contains(sections[1].Title, "order unknown") {
		t.Errorf("second section = %q", sections[1].Title)
	}
}

func TestChangelogSectionsShowASingleVersionForAdditionsAndRemovals(t *testing.T) {
	sections := ChangelogSections(sbom.Result{
		Added:   []sbom.Change{{Name: "new-tool", To: "1.0.0"}},
		Removed: []sbom.Change{{Name: "old-tool", From: "0.4.1"}},
	})

	if sections[0].Entries[0].Subtitle != "1.0.0" {
		t.Errorf("added subtitle = %q, want the new version alone", sections[0].Entries[0].Subtitle)
	}
	if sections[1].Entries[0].Subtitle != "0.4.1" {
		t.Errorf("removed subtitle = %q, want the old version alone", sections[1].Entries[0].Subtitle)
	}
}
