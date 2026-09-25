package firstrun

import (
	"context"
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/capability"
	"github.com/projectbluefin/chairlift/internal/config"
	"github.com/projectbluefin/chairlift/internal/navigation"
)

func allSetupCapabilities() capability.Set {
	return capability.Set{capability.Homebrew: true, capability.Flatpak: true, capability.BootcStage: true}
}

func TestSetupAllTaskSubsetsAndTransitions(t *testing.T) {
	// Exercise all eight subsets, including each zero/one/two/three-step shape.
	for mask := 0; mask < 8; mask++ {
		enabled := map[PolicyRef]bool{
			{"livery_page", "livery_app_grid_group"}:    mask&1 != 0,
			{"applications_page", "brew_bundles_group"}: mask&2 != 0,
			{"updates_page", "flatpak_updates_group"}:   mask&4 != 0,
		}
		floor := capability.Compose(func(p, g string) bool { return enabled[PolicyRef{p, g}] }, allSetupCapabilities())
		before := navigation.VisibleItems(floor)
		bindings := navigation.Bindings(before)
		m := NewAssistantModel(floor)
		want := []string{StepIDWelcome}
		for i, id := range []string{StepIDTheme, StepIDApps, StepIDUpdates} {
			if mask&(1<<i) != 0 {
				want = append(want, id)
			}
		}
		var got []string
		for _, step := range m.Steps() {
			got = append(got, step.ID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mask %d: steps %v, want %v", mask, got, want)
		}
		for i, id := range want {
			if m.CurrentStep().ID != id {
				t.Fatalf("mask %d: stage %d = %s", mask, i, m.CurrentStep().ID)
			}
			for _, stored := range []Disposition{DispositionNotAddressed, DispositionSkipped, DispositionCompleted} {
				expected := DispositionSkipped
				if stored == DispositionCompleted {
					expected = DispositionCompleted
				}
				if m.Skip(stored) != expected || m.Dismiss(stored) != expected {
					t.Fatalf("mask %d: stage %d: skip/dismiss %s", mask, i, stored)
				}
			}
			if m.CurrentIndex() != i {
				t.Fatal("skip/dismiss changed navigation")
			}
			if i > 0 {
				prev, ok := m.Previous()
				if !ok || prev.ID != want[i-1] {
					t.Fatal("Back lost previous step")
				}
				_, done, d := m.Next()
				if done || d != DispositionNotAddressed {
					t.Fatal("Back/Next emitted a settled decision")
				}
			}
			finishes := m.ForwardFinishes()
			next, done, d := m.Next()
			if i == len(want)-1 {
				if !finishes || !done || next != nil || d != DispositionCompleted {
					t.Fatal("final forward did not finish")
				}
			} else if finishes || done || next.ID != want[i+1] || d != DispositionNotAddressed {
				t.Fatal("Next did not navigate without settling")
			}
		}
		if !reflect.DeepEqual(before, navigation.VisibleItems(floor)) || !reflect.DeepEqual(bindings, navigation.Bindings(before)) {
			t.Fatal("setup changed sidebar or accelerators")
		}
	}
}

func TestSetupEveryChoiceHonorsOriginalPolicyAndCapability(t *testing.T) {
	for _, step := range candidateSteps {
		for _, choice := range step.Choices {
			for _, ref := range choice.Policy {
				groups, err := config.SchemaGroups(ref.Page)
				if err != nil {
					t.Fatal(err)
				}
				known := false
				for _, group := range groups {
					known = known || group == ref.Group
				}
				if !known {
					t.Fatalf("%s: unknown policy %v", choice.ID, ref)
				}
				required, classified := capability.Required(ref.Page, ref.Group)
				if !classified {
					t.Fatalf("%s: unclassified policy %v", choice.ID, ref)
				}
				for _, configured := range []bool{false, true} {
					for _, supported := range []bool{false, true} {
						caps := allSetupCapabilities()
						for _, c := range required {
							caps[c] = supported
						}
						floor := capability.Compose(func(p, g string) bool { return configured || (PolicyRef{p, g} != ref) }, caps)
						m := NewAssistantModel(floor)
						found := false
						for _, active := range m.Steps() {
							if active.ID != StepIDWelcome && len(active.Choices) == 0 {
								t.Fatal("empty optional step")
							}
							for _, c := range active.Choices {
								found = found || c.ID == choice.ID
							}
						}
						want := configured && (supported || len(required) == 0)
						if found != want {
							t.Fatalf("%s configured=%v supported=%v: offered=%v", choice.ID, configured, supported, found)
						}
					}
				}
			}
		}
	}
}

func TestSetupFiltersChoicesAcrossNamespaces(t *testing.T) {
	cfg := &config.Config{FeaturesPage: config.PageConfig{"features_group": {Enabled: false}}}
	m := NewAssistantModel(capability.Compose(cfg.IsGroupEnabled, allSetupCapabilities()))
	for _, step := range m.Steps() {
		if step.ID != StepIDUpdates {
			continue
		}
		if len(step.Choices) != 3 {
			t.Fatalf("update choices = %v", step.Choices)
		}
		for _, c := range step.Choices {
			if c.ID == "system-components-enabled" {
				t.Fatal("Updates bypassed original features_page policy")
			}
		}
		return
	}
	t.Fatal("independent update choices were dropped")
}

func TestSetupNilFloorAndCompoundChoiceFailClosed(t *testing.T) {
	if got := NewAssistantModel(nil).TotalSteps(); got != 1 {
		t.Fatalf("nil floor: %d steps", got)
	}
	c := Choice{Policy: []PolicyRef{{"applications_page", "brew_bundles_group"}, {"updates_page", "brew_updates_group"}}}
	if c.enabled(func(p, g string) bool { return p == "applications_page" }) {
		t.Fatal("compound choice bypassed one policy")
	}
	if (Choice{}).enabled(func(p, g string) bool { return true }) {
		t.Fatal("unowned choice was enabled")
	}
}

func TestSetupSnapshotsCannotChangeChoicesOrPolicy(t *testing.T) {
	m := NewAssistantModel(capability.Compose(func(p, g string) bool { return true }, allSetupCapabilities()))
	expected := m.Steps()
	mutate := func(s Step) { s.Choices[0].ID = "bad"; s.Choices[0].Policy[0].Group = "bad" }
	mutate(m.Steps()[1])
	next, _, _ := m.SelectFlow(FlowChoiceConfigure)
	mutate(*next)
	mutate(m.CurrentStep())
	next, _, _ = m.Next()
	mutate(*next)
	prev, _ := m.Previous()
	mutate(*prev)
	if !reflect.DeepEqual(m.Steps(), expected) {
		t.Fatal("returned snapshot changed model")
	}
	if !reflect.DeepEqual(NewAssistantModel(capability.Compose(func(p, g string) bool { return true }, allSetupCapabilities())).Steps(), expected) {
		t.Fatal("snapshot changed candidate inventory")
	}
}

func TestSetupNavigationDoesNotProbeOrPersist(t *testing.T) {
	original := runCommand
	t.Cleanup(func() { runCommand = original })
	runCommand = func(context.Context, string, ...string) (string, error) {
		t.Fatal("model navigation executed a settings command")
		return "", nil
	}
	allowed := true
	calls := 0
	floor := capability.Compose(func(p, g string) bool { calls++; return allowed }, allSetupCapabilities())
	m := NewAssistantModel(floor)
	snapshot := m.Steps()
	initialCalls := calls
	allowed = false
	m.SelectFlow(FlowChoiceConfigure)
	m.Previous()
	for m.HasNext() {
		m.Next()
	}
	m.Next()
	m.Skip(DispositionNotAddressed)
	m.Dismiss(DispositionCompleted)
	if calls != initialCalls {
		t.Fatal("navigation re-evaluated session availability")
	}
	if !reflect.DeepEqual(snapshot, m.Steps()) {
		t.Fatal("session steps changed after construction")
	}
}
