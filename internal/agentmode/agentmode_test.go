package agentmode

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// repoRoot resolves the repository root from this file's own location, the
// same way internal/installcheck does: `go test` runs each package's tests
// with the package directory as the working directory, so the working
// directory is not the repository root.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// fullyReady is the observation every prerequisite-true case builds on, so a
// test that varies one field varies exactly that field.
func fullyReady() Observation {
	return Observation{
		Enabled:         true,
		Supported:       true,
		Provisioned:     true,
		ServiceActive:   true,
		EndpointHealthy: true,
		ActiveModel:     true,
		JanConfigured:   true,
	}
}

// TestClassifyProducesEveryState walks the declared state set and requires
// each one to be reachable. A state that no observation produces is dead
// weight in the contract and in the surface's switch, and it is the shape a
// refactor leaves behind when it collapses two states together.
func TestClassifyProducesEveryState(t *testing.T) {
	reachable := map[State]Observation{
		StateDisabled:     {Enabled: false, Supported: true, Provisioned: true, ServiceActive: true, EndpointHealthy: true, ActiveModel: true},
		StateUnavailable:  {Enabled: true, Supported: false},
		StateProvisioning: {Enabled: true, Supported: true, Provisioning: true},
		StateUnconfigured: {Enabled: true, Supported: true},
		StateDegraded:     {Enabled: true, Supported: true, Provisioned: true, ServiceActive: false},
		StateReady:        fullyReady(),
	}

	for _, state := range AllStates() {
		observation, ok := reachable[state]
		if !ok {
			t.Errorf("state %q has no observation in this test; add one rather than letting a declared state go unproduced", state)
			continue
		}
		if got := Classify(observation); got != state {
			t.Errorf("Classify(%+v) = %q, want %q", observation, got, state)
		}
	}

	for state := range reachable {
		if !containsState(AllStates(), state) {
			t.Errorf("this test produces %q, which AllStates() does not declare", state)
		}
	}
}

func containsState(states []State, want State) bool {
	for _, state := range states {
		if state == want {
			return true
		}
	}
	return false
}

// TestClassifyPrecedenceChain holds the ordering the package documents. Each
// case turns on two conditions that would produce different states on their
// own, and asserts which one wins — the whole value of the chain is that the
// winner does not depend on which caller asked.
func TestClassifyPrecedenceChain(t *testing.T) {
	tests := []struct {
		name        string
		observation Observation
		want        State
	}{
		{
			name:        "the user's off switch outranks an unsupported host",
			observation: Observation{Enabled: false, Supported: false},
			want:        StateDisabled,
		},
		{
			name:        "the user's off switch outranks a running setup",
			observation: Observation{Enabled: false, Supported: true, Provisioning: true},
			want:        StateDisabled,
		},
		{
			name:        "an unsupported host outranks a running setup",
			observation: Observation{Enabled: true, Supported: false, Provisioning: true},
			want:        StateUnavailable,
		},
		{
			name:        "a running setup outranks never having been configured",
			observation: Observation{Enabled: true, Supported: true, Provisioning: true, Provisioned: false},
			want:        StateProvisioning,
		},
		{
			name:        "never having been configured outranks a failing prerequisite",
			observation: Observation{Enabled: true, Supported: true, Provisioned: false, ServiceActive: false},
			want:        StateUnconfigured,
		},
		{
			name:        "a provisioned machine with a stopped service is degraded",
			observation: Observation{Enabled: true, Supported: true, Provisioned: true, ServiceActive: false, EndpointHealthy: false, ActiveModel: true},
			want:        StateDegraded,
		},
		{
			name:        "a provisioned machine with an unreachable endpoint is degraded",
			observation: Observation{Enabled: true, Supported: true, Provisioned: true, ServiceActive: true, EndpointHealthy: false, ActiveModel: true},
			want:        StateDegraded,
		},
		{
			name:        "a provisioned machine whose model went missing is degraded",
			observation: Observation{Enabled: true, Supported: true, Provisioned: true, ServiceActive: true, EndpointHealthy: true, ActiveModel: false},
			want:        StateDegraded,
		},
		{
			name:        "Jan being unconfigured does not keep Agent Mode out of ready",
			observation: Observation{Enabled: true, Supported: true, Provisioned: true, ServiceActive: true, EndpointHealthy: true, ActiveModel: true, JanConfigured: false},
			want:        StateReady,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.observation); got != test.want {
				t.Errorf("Classify(%+v) = %q, want %q", test.observation, got, test.want)
			}
		})
	}
}

// TestReadyMatchesTheAskBluefinTerms holds the implication AskBluefin's
// comment relies on: a ready state means the endpoint answers and a model is
// available, so the three-term predicate can be evaluated directly without
// widening when the state model changes. If a future state is added that
// reports ready without those terms, this test fails before the dispatcher
// starts launching Jan on a machine that cannot serve it.
func TestReadyMatchesTheAskBluefinTerms(t *testing.T) {
	observation := fullyReady()
	if !Ready(observation) {
		t.Fatalf("Ready(%+v) = false, want true", observation)
	}

	for _, term := range []struct {
		name string
		drop func(*Observation)
	}{
		{"the endpoint is unhealthy", func(o *Observation) { o.EndpointHealthy = false }},
		{"no model is available", func(o *Observation) { o.ActiveModel = false }},
	} {
		t.Run(term.name, func(t *testing.T) {
			broken := fullyReady()
			term.drop(&broken)
			if Ready(broken) {
				t.Errorf("Ready(%+v) = true, but %s", broken, term.name)
			}
			if got := AskBluefin(broken); got.Target != TargetAgentModeSurface {
				t.Errorf("AskBluefin(%+v).Target = %q, want %q", broken, got.Target, TargetAgentModeSurface)
			}
		})
	}
}

// TestReadyAndTheDispatcherAreNotTheSamePredicate pins the one place where
// the state model and the dispatcher deliberately differ, so a later
// "simplification" that collapses them fails here instead of shipping.
//
// Ready requires the service to be active; the dispatcher's three terms do
// not name it. A machine whose service is inactive but whose endpoint answers
// with a model loaded is one systemd has not caught up with — and the fact a
// chat client depends on is that the endpoint answers. Restating the
// dispatcher as Ready(o) && JanConfigured would refuse Jan on that machine.
func TestReadyAndTheDispatcherAreNotTheSamePredicate(t *testing.T) {
	observation := fullyReady()
	observation.ServiceActive = false

	if Ready(observation) {
		t.Fatalf("Ready(%+v) = true, want false: an inactive service is not ready", observation)
	}
	if got := AskBluefin(observation); got.Target != TargetJan {
		t.Errorf("AskBluefin(%+v).Target = %q, want %q: the dispatcher's terms are the endpoint, the model, and Jan",
			observation, got.Target, TargetJan)
	}
	if got := Classify(observation); got != StateDegraded {
		t.Errorf("Classify(%+v) = %q, want %q: the state model still reports the service problem",
			observation, got, StateDegraded)
	}
}

// TestAskBluefinLaunchesJanOnlyWhenEveryTermHolds is the truth table for the
// dispatcher. It covers all eight combinations of the three terms, because
// the failure this predicate exists to prevent — launching Jan against an
// endpoint that cannot answer — is produced by exactly one of them, and a
// test that only walks the all-true and all-false rows cannot see it.
func TestAskBluefinLaunchesJanOnlyWhenEveryTermHolds(t *testing.T) {
	for _, endpoint := range []bool{false, true} {
		for _, model := range []bool{false, true} {
			for _, jan := range []bool{false, true} {
				observation := fullyReady()
				observation.EndpointHealthy = endpoint
				observation.ActiveModel = model
				observation.JanConfigured = jan

				want := TargetAgentModeSurface
				if endpoint && model && jan {
					want = TargetJan
				}

				decision := AskBluefin(observation)
				if decision.Target != want {
					t.Errorf("AskBluefin(endpoint=%v model=%v jan=%v).Target = %q, want %q",
						endpoint, model, jan, decision.Target, want)
				}
				if decision.State != Classify(observation) {
					t.Errorf("AskBluefin(endpoint=%v model=%v jan=%v).State = %q, want the state Classify reports (%q)",
						endpoint, model, jan, decision.State, Classify(observation))
				}
				if want == TargetJan && len(decision.Unmet) != 0 {
					t.Errorf("AskBluefin(endpoint=%v model=%v jan=%v) launches Jan but reports unmet prerequisites %v",
						endpoint, model, jan, decision.Unmet)
				}
				if want == TargetAgentModeSurface && len(decision.Unmet) == 0 {
					t.Errorf("AskBluefin(endpoint=%v model=%v jan=%v) opens the surface with nothing to show",
						endpoint, model, jan)
				}
			}
		}
	}
}

// TestADisabledSwitchStillLaunchesJanWhileTheEndpointAnswers pins the row the
// dispatcher's truth table does not reach, because every row there is built
// from fullyReady() and therefore has Enabled=true.
//
// The three terms do not name the user's switch, so an observation with Agent
// Mode off and an endpoint that still answers launches Jan. That is a
// decision, not an oversight of "disabled outranks everything": that rule
// governs the state, which the decision still reports as StateDisabled so the
// surface never offers to repair a feature the user turned off. Jan belongs to
// the distribution and this switch never uninstalls it, and disabling Agent
// Mode unprovisions llmman's service — so on an ordinary machine the endpoint
// stops answering and the switch surfaces as the unmet prerequisite by itself.
func TestADisabledSwitchStillLaunchesJanWhileTheEndpointAnswers(t *testing.T) {
	observation := fullyReady()
	observation.Enabled = false

	decision := AskBluefin(observation)
	if decision.Target != TargetJan {
		t.Errorf("AskBluefin(%+v).Target = %q, want %q: the dispatcher's terms are the endpoint, the model, and Jan",
			observation, decision.Target, TargetJan)
	}
	if decision.State != StateDisabled {
		t.Errorf("AskBluefin(%+v).State = %q, want %q: the decision still reports the user's intent",
			observation, decision.State, StateDisabled)
	}
	if len(decision.Unmet) != 0 {
		t.Errorf("AskBluefin(%+v).Unmet = %v, want none: a launch has nothing unmet to show",
			observation, decision.Unmet)
	}

	observation.EndpointHealthy = false
	stopped := AskBluefin(observation)
	if stopped.Target != TargetAgentModeSurface {
		t.Errorf("AskBluefin(%+v).Target = %q, want %q: a disabled switch with a silent endpoint opens the surface",
			observation, stopped.Target, TargetAgentModeSurface)
	}
	if len(stopped.Unmet) != 1 || stopped.Unmet[0] != PrerequisiteEnabled {
		t.Errorf("AskBluefin(%+v).Unmet = %v, want only %q",
			observation, stopped.Unmet, PrerequisiteEnabled)
	}
}

// TestUnmetPrerequisitesNameTheCauseNotTheSymptom checks the gating rule: a
// host that cannot run llmman has an unhealthy endpoint as a consequence, and
// reporting that consequence would hand the user a repair step that cannot
// work.
func TestUnmetPrerequisitesNameTheCauseNotTheSymptom(t *testing.T) {
	tests := []struct {
		name        string
		observation Observation
		want        []Prerequisite
	}{
		{
			name:        "a disabled switch reports only itself",
			observation: Observation{Enabled: false, Supported: false},
			want:        []Prerequisite{PrerequisiteEnabled},
		},
		{
			name:        "an unsupported host reports only itself",
			observation: Observation{Enabled: true, Supported: false},
			want:        []Prerequisite{PrerequisiteHost},
		},
		{
			name:        "a running setup reports progress, not failure",
			observation: Observation{Enabled: true, Supported: true, Provisioning: true},
			want:        []Prerequisite{PrerequisiteProvisioning},
		},
		{
			name:        "a never-configured machine reports what setup still owes",
			observation: Observation{Enabled: true, Supported: true},
			want:        []Prerequisite{PrerequisiteEndpoint, PrerequisiteModel, PrerequisiteJan},
		},
		{
			name:        "a stopped service reports the endpoint and the model, in that order",
			observation: Observation{Enabled: true, Supported: true, Provisioned: true},
			want:        []Prerequisite{PrerequisiteEndpoint, PrerequisiteModel, PrerequisiteJan},
		},
		{
			name: "an otherwise ready machine reports only the Jan integration",
			observation: func() Observation {
				observation := fullyReady()
				observation.JanConfigured = false
				return observation
			}(),
			want: []Prerequisite{PrerequisiteJan},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := AskBluefin(test.observation).Unmet
			if len(got) != len(test.want) {
				t.Fatalf("unmet prerequisites = %v, want %v", got, test.want)
			}
			for i := range test.want {
				if got[i] != test.want[i] {
					t.Fatalf("unmet prerequisites = %v, want %v", got, test.want)
				}
			}
		})
	}
}

// TestLaunchIntentResolutionNeverLaunchesJanForTheSurfaceIntent pins the one
// intent that must not depend on readiness. A user who asked for the
// configuration surface is not asking for a chat client, however ready the
// machine is — and the resolution must not change when a window already
// exists, which is why it is a function of the observation alone.
func TestLaunchIntentResolutionNeverLaunchesJanForTheSurfaceIntent(t *testing.T) {
	for _, state := range []Observation{
		fullyReady(),
		{Enabled: true, Supported: true, Provisioned: true},
		{Enabled: false},
	} {
		decision := ResolveIntent(IntentAgentMode, state)
		if decision.Target != TargetAgentModeSurface {
			t.Errorf("ResolveIntent(%q, %+v).Target = %q, want %q",
				IntentAgentMode, state, decision.Target, TargetAgentModeSurface)
		}
	}

	if got := ResolveIntent(IntentAskBluefin, fullyReady()); got.Target != TargetJan {
		t.Errorf("ResolveIntent(%q, ready).Target = %q, want %q", IntentAskBluefin, got.Target, TargetJan)
	}
}

// TestEveryIntentResolvesToAKnownTarget covers the declared intent set rather
// than a hand-copied list of it, so an intent added without a resolution arm
// fails here instead of silently falling through to the surface.
func TestEveryIntentResolvesToAKnownTarget(t *testing.T) {
	targets := map[LaunchTarget]bool{TargetJan: true, TargetAgentModeSurface: true}
	for _, intent := range AllIntents() {
		if got := ResolveIntent(intent, fullyReady()); !targets[got.Target] {
			t.Errorf("ResolveIntent(%q, ready).Target = %q, which is not a declared launch target", intent, got.Target)
		}
	}
}

// TestEveryArtifactHasOneOwnerAndAStatedCleanupPolicy holds the ownership
// table to the rule the ADR states. A duplicate name is two entries for one
// artifact, an unknown owner is a second writer, and an empty cleanup policy
// is how a file gets deleted by someone who assumed it was ours.
func TestEveryArtifactHasOneOwnerAndAStatedCleanupPolicy(t *testing.T) {
	owners := make(map[Owner]bool)
	for _, owner := range AllOwners() {
		owners[owner] = true
	}

	seen := make(map[string]bool)
	for _, artifact := range Artifacts() {
		if artifact.Name == "" {
			t.Error("an artifact has no name")
			continue
		}
		if seen[artifact.Name] {
			t.Errorf("artifact %q appears twice; one artifact has one row", artifact.Name)
		}
		seen[artifact.Name] = true

		if !owners[artifact.Owner] {
			t.Errorf("artifact %q names owner %q, which is not in AllOwners()", artifact.Name, artifact.Owner)
		}
		if strings.TrimSpace(artifact.Cleanup) == "" {
			t.Errorf("artifact %q states no cleanup policy", artifact.Name)
		}
		if !strings.HasPrefix(artifact.Issue, "#") {
			t.Errorf("artifact %q names issue %q, which is not a bare issue reference", artifact.Name, artifact.Issue)
		}
	}

	for _, owner := range AllOwners() {
		used := false
		for _, artifact := range Artifacts() {
			if artifact.Owner == owner {
				used = true
				break
			}
		}
		if !used {
			t.Errorf("owner %q owns no artifact; an owner with nothing to own is a name that will drift", owner)
		}
	}
}

// TestEveryArtifactRepoPathExists keeps the table from citing a file the tree
// does not have. The ownership table is read as fact, so a row pointing at a
// deleted path is a claim about the repository that is simply wrong — the
// same failure mode internal/installcheck's citation gate catches in prose.
func TestEveryArtifactRepoPathExists(t *testing.T) {
	for _, artifact := range Artifacts() {
		if artifact.RepoPath == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(repoRoot(), artifact.RepoPath)); err != nil {
			t.Errorf("artifact %q cites %s, which does not exist: %v", artifact.Name, artifact.RepoPath, err)
		}
	}
}

// TestBoundariesCoverTheRequiredSecuritySurface checks the security contract
// against the boundary list the ADR is required to state, rather than against
// a copy of itself: the six named areas are the ones the epic's acceptance
// criteria require, and a rewrite that dropped one would otherwise pass.
func TestBoundariesCoverTheRequiredSecuritySurface(t *testing.T) {
	required := []string{
		"loopback-binding",
		"web-shell-disabled",
		"mcp-tool-restrictions",
		"peer-authentication",
		"configuration-ownership",
		"secret-storage",
	}

	stated := make(map[string]bool)
	for _, boundary := range Boundaries() {
		if boundary.Name == "" || strings.TrimSpace(boundary.Rule) == "" {
			t.Errorf("boundary %+v is missing a name or a rule", boundary)
		}
		if stated[boundary.Name] {
			t.Errorf("boundary %q is stated twice", boundary.Name)
		}
		stated[boundary.Name] = true
	}

	for _, name := range required {
		if !stated[name] {
			t.Errorf("the security contract does not state the %q boundary", name)
		}
	}
}

// TestWorkMapNamesEveryEpicSlice holds the issue map to the epic's own
// numbering. The ADR is the reference a later slice reads before it starts,
// and a map missing one of the twelve ChairLift slices is how a slice ends up
// contradicting the decision it was supposed to follow.
func TestWorkMapNamesEveryEpicSlice(t *testing.T) {
	refs := make(map[string]bool)
	for _, slice := range WorkMap() {
		if refs[slice.Ref] {
			t.Errorf("work map names %s twice", slice.Ref)
		}
		refs[slice.Ref] = true
		if strings.TrimSpace(slice.Scope) == "" {
			t.Errorf("work map entry %s states no scope", slice.Ref)
		}
	}

	for number := 253; number <= 264; number++ {
		ref := "#" + strconv.Itoa(number)
		if !refs[ref] {
			t.Errorf("work map does not name %s", ref)
		}
	}
}
