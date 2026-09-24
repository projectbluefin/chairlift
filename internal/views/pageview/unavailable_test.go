package pageview

import (
	"testing"

	"github.com/projectbluefin/chairlift/internal/capability"
)

func onlyGroups(pairs ...[2]string) func(page, group string) bool {
	return func(page, group string) bool {
		for _, p := range pairs {
			if p == [2]string{page, group} {
				return true
			}
		}
		return false
	}
}

func allGroups(string, string) bool { return true }

func TestUnavailableFeaturesListsConfiguredGroupsTheHostCannotBack(t *testing.T) {
	configured := onlyGroups(
		[2]string{"updates_page", "flatpak_updates_group"},
		[2]string{"maintenance_page", "reset_group"},
		[2]string{"agents_page", "agents_group"},
	)
	// Homebrew present backs Agent Mode; Distrobox alone backs Recovery.
	set := capability.Set{capability.Homebrew: true}

	got := UnavailableFeatures(set, configured)
	want := []Row{
		{Title: "Recovery", Subtitle: "Needs Flatpak or Distrobox"},
		{Title: "App updates", Subtitle: "Needs Flatpak"},
	}
	if len(got) != len(want) {
		t.Fatalf("UnavailableFeatures() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestUnavailableFeaturesOmitsConfigDisabledGroups(t *testing.T) {
	// Nothing on the host, but configuration enables nothing either: every
	// hidden group is the administrator's choice, not a missing tool.
	if got := UnavailableFeatures(capability.Set{}, func(string, string) bool { return false }); len(got) != 0 {
		t.Errorf("config-disabled groups listed: %+v", got)
	}
	if got := UnavailableFeatures(capability.Set{}, nil); len(got) != 0 {
		t.Errorf("nil configuration listed groups: %+v", got)
	}
}

func TestUnavailableFeaturesIsEmptyWhenEveryCapabilityIsPresent(t *testing.T) {
	set := capability.Set{}
	for _, p := range capability.Prerequisites() {
		for _, c := range p.AnyOf {
			set[c] = true
		}
	}
	if got := UnavailableFeatures(set, allGroups); len(got) != 0 {
		t.Errorf("fully capable host listed: %+v", got)
	}
}

func TestEveryCapabilityGatedGroupHasATitle(t *testing.T) {
	// With nothing present every gated group is listed, so a table gap shows
	// up as a blank title or an unnamed capability.
	rows := UnavailableFeatures(capability.Set{}, allGroups)
	if len(rows) == 0 {
		t.Fatal("no capability-gated groups; the check is vacuous")
	}
	for _, p := range capability.Prerequisites() {
		if len(p.AnyOf) == 0 {
			continue
		}
		if featureTitles[[2]string{p.Page, p.Group}] == "" {
			t.Errorf("%s/%s has no title", p.Page, p.Group)
		}
		for _, c := range p.AnyOf {
			if capabilityNames[c] == "" {
				t.Errorf("capability %q has no name", c)
			}
		}
	}
	for key := range featureTitles {
		if required, ok := capability.Required(key[0], key[1]); !ok || len(required) == 0 {
			t.Errorf("%s/%s is titled but not capability-gated", key[0], key[1])
		}
	}
}
