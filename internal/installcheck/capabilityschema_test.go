package installcheck

import (
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/capability"
	"github.com/projectbluefin/chairlift/internal/config"
)

// The page/group grammar has one declared owner: internal/config. Its
// exported schema surface (config.SchemaPages, config.SchemaGroups) is derived
// by reflection from Config's yaml tags and defaultConfig(), and
// TestNavigationPagesMatchConfigSchema/TestNavigationGroupsMatchConfigSchema
// already hold internal/navigation's copy of that grammar to it.
//
// internal/capability classifies the same grammar a third time, in the
// prerequisites table its page/group predicate resolves. Nothing would hold
// that copy to the owner either, and the failure mode is quieter than
// navigation's. navigation's copy is consulted for every group, so a group
// missing from it is merely unreachable in the sidebar. capability's copy is
// consulted for every group too, but Supports reports an *unclassified* pair
// as supported — deliberately, so a missing entry cannot silently hide a
// group at runtime. The two together mean a group added to config.yml and
// wired into a view renders on hosts whose backing tool is absent, which is
// the exact degradation policy the capability floor exists to enforce. No
// other gate would notice: the application builds, every existing test passes,
// and the mistake only shows on a host that lacks the tool.
//
// These tests close that edge, in both directions and as set equality rather
// than order: config.SchemaGroups sorts its result, while capability's
// prerequisites are in page order.

// capabilityPairs returns every page/group pair capability classifies, as
// "page/group" strings.
func capabilityPairs() []string {
	entries := capability.Prerequisites()
	pairs := make([]string, 0, len(entries))
	for _, entry := range entries {
		pairs = append(pairs, entry.Page+"/"+entry.Group)
	}
	return pairs
}

// configPairs returns every page/group pair the configuration schema declares,
// as "page/group" strings.
func configPairs(t *testing.T) []string {
	t.Helper()

	pages, err := config.SchemaPages()
	if err != nil {
		t.Fatalf("config.SchemaPages(): %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("config.SchemaPages() is empty: the gate would be vacuous")
	}

	var pairs []string
	for _, page := range pages {
		groups, err := config.SchemaGroups(page)
		if err != nil {
			t.Fatalf("config.SchemaGroups(%q): %v", page, err)
		}
		if len(groups) == 0 {
			t.Fatalf("config declares no groups for %q: the gate would be vacuous", page)
		}
		for _, group := range groups {
			pairs = append(pairs, page+"/"+group)
		}
	}
	return pairs
}

// TestCapabilityPrerequisitesMatchConfigSchema holds capability's
// prerequisites table and config's group schema to a bijection: every
// configurable group is classified, and every classified pair names a group
// the schema actually declares.
func TestCapabilityPrerequisitesMatchConfigSchema(t *testing.T) {
	want := sortedNames(configPairs(t))
	got := sortedNames(capabilityPairs())

	if !reflect.DeepEqual(got, want) {
		t.Errorf(
			"capability.Prerequisites() pairs = %v, config schema pairs = %v",
			got, want,
		)
	}
}

// TestEveryConfigurableGroupIsClassifiedOnce names the two failure shapes
// separately, because "missing" and "duplicated" need different fixes: a
// missing pair must be classified (or deliberately listed with no
// capabilities), while a duplicate is a table bug that would make the resolved
// requirement depend on which entry a lookup found.
func TestEveryConfigurableGroupIsClassifiedOnce(t *testing.T) {
	classified := make(map[string]int)
	for _, pair := range capabilityPairs() {
		classified[pair]++
	}
	if len(classified) == 0 {
		t.Fatal("capability.Prerequisites() is empty: the gate would be vacuous")
	}

	for _, pair := range configPairs(t) {
		switch classified[pair] {
		case 1:
		case 0:
			t.Errorf(
				"config declares %s but capability.Prerequisites() does not classify it; classify it, or list it with no capabilities if the group has no host prerequisite",
				pair,
			)
		default:
			t.Errorf("capability.Prerequisites() classifies %s %d times", pair, classified[pair])
		}
	}
}
