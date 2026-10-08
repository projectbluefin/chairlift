package featurestatus

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/updex"
)

// featureName is the single feature name used by every case, so the
// distinctness assertion below cannot be satisfied merely by the name
// differing between cases.
const featureName = "f"

// branch identifies which of the six subtitle branches a case exercises. The
// distinctness assertion compares one representative case per branch.
type branch string

const (
	branchNone         branch = ""
	branchFailed       branch = "zero components, failed check"
	branchOneVersioned branch = "one update, known current version"
	branchOneUnknown   branch = "one update, empty current version"
	branchMany         branch = "several updates"
	branchCommon       branch = "no updates, agreed version"
	branchMixed        branch = "no updates, no agreed version"
)

type caseSpec struct {
	name    string
	results []updex.CheckResult
	want    string
	wantOK  bool
	branch  branch
}

func res(component, current, newest string, update bool) updex.CheckResult {
	return updex.CheckResult{
		Component:       component,
		CurrentVersion:  current,
		NewestVersion:   newest,
		UpdateAvailable: update,
	}
}

// tableCases is one entry per distinguishable input shape the Features page's
// update check can hand to Feature.
func tableCases() []caseSpec {
	return []caseSpec{
		{
			name:    "zero components, nil results",
			results: nil,
			want:    "f — couldn't check for updates",
			wantOK:  true,
			branch:  branchFailed,
		},
		{
			name:    "zero components, empty slice",
			results: []updex.CheckResult{},
			want:    "f — couldn't check for updates",
			wantOK:  true,
			branch:  branchFailed,
		},
		{
			name: "update in the first component only",
			results: []updex.CheckResult{
				res("comp", "1.0", "2.0", true),
				res("other", "1.0", "1.0", false),
				res("third", "1.0", "1.0", false),
			},
			want:   "f — update available for comp (v1.0 → v2.0)",
			wantOK: true,
			branch: branchOneVersioned,
		},
		{
			name: "update in a later component only",
			results: []updex.CheckResult{
				res("other", "1.0", "1.0", false),
				res("third", "1.0", "1.0", false),
				res("comp", "1.0", "2.0", true),
			},
			want:   "f — update available for comp (v1.0 → v2.0)",
			wantOK: true,
			branch: branchOneVersioned,
		},
		{
			name: "update in a middle component only",
			results: []updex.CheckResult{
				res("other", "1.0", "1.0", false),
				res("comp", "1.0", "2.0", true),
				res("third", "1.0", "1.0", false),
			},
			want:   "f — update available for comp (v1.0 → v2.0)",
			wantOK: true,
			branch: branchOneVersioned,
		},
		{
			name: "updates in several components",
			results: []updex.CheckResult{
				res("comp", "1.0", "2.0", true),
				res("other", "1.0", "1.0", false),
				res("third", "3.0", "4.0", true),
			},
			want:   "f — 2 updates available",
			wantOK: true,
			branch: branchMany,
		},
		{
			name: "updates in every component",
			results: []updex.CheckResult{
				res("comp", "1.0", "2.0", true),
				res("other", "1.0", "2.0", true),
				res("third", "3.0", "4.0", true),
			},
			want:   "f — 3 updates available",
			wantOK: true,
			branch: branchMany,
		},
		{
			name: "no updates at all",
			results: []updex.CheckResult{
				res("comp", "1.0", "1.0", false),
				res("other", "1.0", "1.0", false),
			},
			want:   "f — v1.0",
			wantOK: true,
			branch: branchCommon,
		},
		{
			name: "a component whose CurrentVersion is empty",
			results: []updex.CheckResult{
				res("comp", "", "2.0", true),
			},
			want:   "f — update available for comp (→ v2.0)",
			wantOK: true,
			branch: branchOneUnknown,
		},
		{
			name: "no updates and a component whose CurrentVersion is empty",
			results: []updex.CheckResult{
				res("comp", "", "1.0", false),
				res("other", "1.0", "1.0", false),
			},
			want:   "f — up to date",
			wantOK: true,
			branch: branchMixed,
		},
		{
			name: "components reporting different CurrentVersion values with no update",
			results: []updex.CheckResult{
				res("comp", "1.0", "1.0", false),
				res("other", "2.5", "2.5", false),
			},
			want:   "f — up to date",
			wantOK: true,
			branch: branchMixed,
		},
	}
}

func TestFeatureTable(t *testing.T) {
	for _, tc := range tableCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Feature(featureName, tc.results)

			if ok != tc.wantOK {
				t.Fatalf("Feature(%q, %#v) ok = %t, want %t", featureName, tc.results, ok, tc.wantOK)
			}
			if !ok {
				return
			}

			if got.Subtitle != tc.want {
				t.Errorf("Feature(%q, %#v).Subtitle = %q, want %q",
					featureName, tc.results, got.Subtitle, tc.want)
			}

			// HasUpdate is an OR across EVERY element, not a look at index 0.
			wantUpdate := false
			for _, r := range tc.results {
				if r.UpdateAvailable {
					wantUpdate = true
				}
			}
			if got.HasUpdate != wantUpdate {
				t.Errorf("Feature(%q, %#v).HasUpdate = %t, want %t",
					featureName, tc.results, got.HasUpdate, wantUpdate)
			}

			wantIncomplete := len(tc.results) == 0
			if got.Incomplete != wantIncomplete {
				t.Errorf("Feature(%q, %#v).Incomplete = %t, want %t",
					featureName, tc.results, got.Incomplete, wantIncomplete)
			}
		})
	}
}

func TestFeatureHasUpdateWhenAnyComponentReportsOne(t *testing.T) {
	for _, tc := range tableCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Feature(featureName, tc.results)
			if !ok {
				return
			}

			any := false
			for i, r := range tc.results {
				if r.UpdateAvailable {
					any = true
					if !got.HasUpdate {
						t.Errorf("component %d (%q) reports an update but HasUpdate = false",
							i, r.Component)
					}
				}
			}
			if !any && got.HasUpdate {
				t.Errorf("no component reports an update but HasUpdate = true (results %#v)", tc.results)
			}
			if got.HasUpdate != any {
				t.Errorf("HasUpdate = %t, want %t", got.HasUpdate, any)
			}
		})
	}
}

func TestFeatureWithZeroComponentsReportsFailedCheck(t *testing.T) {
	for _, results := range [][]updex.CheckResult{nil, {}} {
		got, ok := Feature(featureName, results)
		if !ok {
			t.Fatalf("Feature(%q, %#v) ok = false, want true", featureName, results)
		}
		want := fmt.Sprintf("%s — couldn't check for updates", featureName)
		if got.Subtitle != want {
			t.Errorf("Subtitle = %q, want %q", got.Subtitle, want)
		}
		if got.HasUpdate {
			t.Errorf("HasUpdate = true, want false")
		}
		if !got.Incomplete {
			t.Errorf("Incomplete = false, want true")
		}
	}

	if _, ok := Feature("", nil); ok {
		t.Errorf("Feature(%q, nil) ok = true, want false", "")
	}
}

func TestFeatureNeverRendersABareV(t *testing.T) {
	bareV := regexp.MustCompile(`v(\s|$|—|→)`)

	for _, tc := range tableCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Feature(featureName, tc.results)
			if !ok {
				return
			}
			if loc := bareV.FindString(got.Subtitle); loc != "" {
				t.Errorf("Feature(%q, %#v).Subtitle = %q renders a bare %q",
					featureName, tc.results, got.Subtitle, loc)
			}
		})
	}
}

func TestFeatureEmptyCurrentVersionOmitsTheVersionArrowSource(t *testing.T) {
	results := []updex.CheckResult{res("comp", "", "2.0", true)}

	got, ok := Feature(featureName, results)
	if !ok {
		t.Fatalf("Feature(%q, %#v) ok = false, want true", featureName, results)
	}

	const want = "f — update available for comp (→ v2.0)"
	if got.Subtitle != want {
		t.Errorf("Feature(%q, %#v).Subtitle = %q, want %q", featureName, results, got.Subtitle, want)
	}
	if strings.Contains(got.Subtitle, "v ") {
		t.Errorf("subtitle %q contains a bare %q", got.Subtitle, "v ")
	}
	if strings.HasSuffix(got.Subtitle, "v") {
		t.Errorf("subtitle %q ends in a bare %q", got.Subtitle, "v")
	}
}

func TestSubtitleBranchesArePairwiseDistinct(t *testing.T) {
	seen := make(map[branch]string)
	for _, tc := range tableCases() {
		if tc.branch == branchNone {
			continue
		}
		if _, ok := seen[tc.branch]; ok {
			continue
		}
		got, ok := Feature(featureName, tc.results)
		if !ok {
			t.Fatalf("case %q: Feature ok = false, want true", tc.name)
		}
		seen[tc.branch] = got.Subtitle
	}

	if len(seen) != 6 {
		t.Fatalf("covered %d subtitle branches, want all 6", len(seen))
	}

	for a, subA := range seen {
		for b, subB := range seen {
			if a != b && subA == subB {
				t.Errorf("branch %q and branch %q both produce %q; every branch must be distinguishable",
					a, b, subA)
			}
		}
	}
}

func TestSubtitleSingularOneUpdate(t *testing.T) {
	results := []updex.CheckResult{
		res("comp", "1.0", "2.0", true),
		res("other", "1.0", "1.0", false),
	}

	got, _ := Feature(featureName, results)
	const want = "f — update available for comp (v1.0 → v2.0)"
	if got.Subtitle != want {
		t.Errorf("Subtitle = %q, want %q", got.Subtitle, want)
	}
	if strings.Contains(got.Subtitle, "updates available") {
		t.Errorf("Subtitle = %q, want the singular %q", got.Subtitle, "update available")
	}
}

func TestSubtitlePluralMultipleUpdates(t *testing.T) {
	results := []updex.CheckResult{
		res("comp", "1.0", "2.0", true),
		res("other", "1.0", "2.0", true),
	}

	got, _ := Feature(featureName, results)
	const want = "f — 2 updates available"
	if got.Subtitle != want {
		t.Errorf("Subtitle = %q, want %q", got.Subtitle, want)
	}
	if !strings.Contains(got.Subtitle, "updates available") {
		t.Errorf("Subtitle = %q, want the plural %q", got.Subtitle, "updates available")
	}
}

// preCheckDescription is the description loadFeatures sets before the update
// check runs. Every completed-check description must differ from it.
const preCheckDescription = "9 features available"

func TestGroupDescriptionSingularOneUpdate(t *testing.T) {
	got := GroupDescription(9, 1)
	const want = "9 features available (1 update)"
	if got != want {
		t.Errorf("GroupDescription(9, 1) = %q, want %q", got, want)
	}
	if strings.Contains(got, "1 updates") {
		t.Errorf("GroupDescription(9, 1) = %q, must not contain %q", got, "1 updates")
	}
}

func TestGroupDescriptionPluralManyUpdates(t *testing.T) {
	got := GroupDescription(9, 3)
	const want = "9 features available (3 updates)"
	if got != want {
		t.Errorf("GroupDescription(9, 3) = %q, want %q", got, want)
	}
}

func TestGroupDescriptionZeroUpdates(t *testing.T) {
	got := GroupDescription(9, 0)
	const want = "9 features available — all up to date"
	if got != want {
		t.Errorf("GroupDescription(9, 0) = %q, want %q", got, want)
	}
	if got == preCheckDescription {
		t.Errorf("GroupDescription(9, 0) = %q, want it to differ from the pre-check string %q",
			got, preCheckDescription)
	}
	if strings.Contains(got, "(0 updates)") {
		t.Errorf("GroupDescription(9, 0) = %q, must not contain %q", got, "(0 updates)")
	}
}

func TestGroupDescriptionCheckFailedSaysSo(t *testing.T) {
	got := GroupDescriptionCheckFailed(9)

	if !strings.Contains(got, "couldn't check for updates") {
		t.Errorf("GroupDescriptionCheckFailed(9) = %q, want it to contain %q", got, "couldn't check for updates")
	}
	for _, forbidden := range []string{"(0 updates)", "all up to date"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("GroupDescriptionCheckFailed(9) = %q, must not contain %q", got, forbidden)
		}
	}
	if got == preCheckDescription {
		t.Errorf("GroupDescriptionCheckFailed(9) = %q, want it to differ from the pre-check string %q",
			got, preCheckDescription)
	}
}

func TestGroupDescriptionIncompleteZeroUpdates(t *testing.T) {
	got := GroupDescriptionIncomplete(9, 0)
	const want = "9 features available — some couldn't be checked for updates"
	if got != want {
		t.Errorf("GroupDescriptionIncomplete(9, 0) = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"(0 updates)", "all up to date"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("GroupDescriptionIncomplete(9, 0) = %q, must not contain %q", got, forbidden)
		}
	}
	if got == preCheckDescription {
		t.Errorf("GroupDescriptionIncomplete(9, 0) = %q, want it to differ from the pre-check string %q",
			got, preCheckDescription)
	}
}

func TestGroupDescriptionIncompleteSingular(t *testing.T) {
	got := GroupDescriptionIncomplete(9, 1)
	const want = "9 features available (1 update) — some couldn't be checked for updates"
	if got != want {
		t.Errorf("GroupDescriptionIncomplete(9, 1) = %q, want %q", got, want)
	}
	if strings.Contains(got, "1 updates") {
		t.Errorf("GroupDescriptionIncomplete(9, 1) = %q, must not contain %q", got, "1 updates")
	}
}

func TestGroupDescriptionIncompletePlural(t *testing.T) {
	got := GroupDescriptionIncomplete(9, 3)
	const want = "9 features available (3 updates) — some couldn't be checked for updates"
	if got != want {
		t.Errorf("GroupDescriptionIncomplete(9, 3) = %q, want %q", got, want)
	}
}

func TestGroupDescriptionsAreDistinct(t *testing.T) {
	descriptions := map[string]string{
		"failed":                GroupDescriptionCheckFailed(9),
		"zero updates":          GroupDescription(9, 0),
		"one update":            GroupDescription(9, 1),
		"many updates":          GroupDescription(9, 3),
		"incomplete zero":       GroupDescriptionIncomplete(9, 0),
		"incomplete one update": GroupDescriptionIncomplete(9, 1),
		"incomplete many":       GroupDescriptionIncomplete(9, 3),
		"pre-check":             preCheckDescription,
	}

	seen := make(map[string]string)
	for name, desc := range descriptions {
		if other, ok := seen[desc]; ok {
			t.Errorf("case %q and case %q both produce %q", name, other, desc)
			continue
		}
		seen[desc] = name
	}

	for name, desc := range descriptions {
		if name == "pre-check" {
			continue
		}
		if !strings.HasPrefix(desc, preCheckDescription) {
			t.Errorf("%s description = %q, want it to keep the %q fragment verbatim",
				name, desc, preCheckDescription)
		}
	}
}

func TestGroupDescriptionCountsFeaturesNotComponents(t *testing.T) {
	// One feature with three outdated components counts once.
	results := []updex.CheckResult{
		res("comp", "1.0", "2.0", true),
		res("other", "1.0", "2.0", true),
		res("third", "1.0", "2.0", true),
	}

	status, ok := Feature(featureName, results)
	if !ok || !status.HasUpdate {
		t.Fatalf("Feature(%q, %#v) = (%+v, %t), want a single feature with an update",
			featureName, results, status, ok)
	}

	featuresWithUpdates := 0
	if status.HasUpdate {
		featuresWithUpdates++
	}

	got := GroupDescription(9, featuresWithUpdates)
	want := fmt.Sprintf("%s (1 update)", preCheckDescription)
	if got != want {
		t.Errorf("GroupDescription(9, %d) = %q, want %q", featuresWithUpdates, got, want)
	}
}
