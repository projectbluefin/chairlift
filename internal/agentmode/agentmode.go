// Package agentmode is the single owner of Agent Mode's state model.
//
// Agent Mode is Control Center's first-class local-AI surface, built around
// llmman. The split of responsibilities is fixed: llmman owns model storage,
// inference, and the choice of engine for the detected hardware; Goose is the
// GUI troubleshooting client; Oh My Pi is the configured shell; Jan is the
// Ask Bluefin chat client; ChairLift owns the surface, the readiness decision,
// and the recommended model — nothing else. ChairLift therefore never writes
// a provider, an alias, a peer, or a credential: those are llmman's, and they
// move only through llmman's own commands.
//
// This package exists because three call sites must agree on one answer: the
// Agent Mode surface (what to draw), the Ask Bluefin dispatcher (whether to
// launch Jan), and the Ctrl+Alt+Backspace shortcut (that same dispatcher,
// reached a different way). When "ready" is computed separately at each call
// site, the menu entry and the shortcut eventually disagree — one launches
// Jan while the other opens the configuration surface. So the six states, the
// readiness predicate, the artifact ownership table, and the security
// boundaries all live here, and every call site renders what this package
// returns.
//
// The package is pure. It observes nothing, runs nothing, and writes nothing:
// every input is a field on Observation, and the caller is responsible for
// having established each fact from its owner. That keeps the state machine
// testable on a headless host, and it keeps the observation honest — a fact
// nobody can produce stays visible as a field with no producer rather than
// collapsing into a plausible default.
//
// This package owns machine identifiers, not prose. Every user-visible string
// for these states and prerequisites belongs to internal/views/pageview, which
// is where the repository's display text is tested (ADR-0012). String() is
// therefore the wire/log word, never a sentence to show a user.
//
// The one prose this package does carry is the ownership and boundary tables,
// and it is contract text rather than display text: the tables are quoted
// verbatim into ADR-0013, and internal/installcheck compares the two. Those
// sentences name roles ("the distribution", "this feature") rather than the
// product, because they are read in the record beside an Owner column that
// already names it; spelling the code name here would put it in a string no
// user sees and trip the display-name gate (ADR-0012) for no gain.
//
// See docs/adr/0013-agent-mode-architecture-and-state-contract.md.
package agentmode

// State is Agent Mode's lifecycle state. Exactly one state holds at a time,
// and Classify derives it from observation alone.
type State string

const (
	// StateDisabled means the user turned Agent Mode off. It outranks every
	// other state: an explicit "off" is not a malfunction to be diagnosed,
	// and a surface that reported "degraded" for a feature the user switched
	// off would be asking them to fix something they chose.
	StateDisabled State = "disabled"
	// StateUnavailable means this host cannot run Agent Mode at all — the
	// platform llmman supports is absent. It is terminal: no amount of
	// configuration moves it, so the surface explains rather than offers.
	StateUnavailable State = "unavailable"
	// StateProvisioning means a setup step is in flight: installing llmman,
	// starting the user service, or downloading the selected model. It is
	// transient and it is not a failure, so the surface reports progress
	// instead of an unmet prerequisite.
	StateProvisioning State = "provisioning"
	// StateUnconfigured means Agent Mode has never completed setup: llmman
	// may be installed, but no model has been selected and the user service
	// has not been provisioned. The surface offers setup.
	StateUnconfigured State = "unconfigured"
	// StateDegraded means setup completed at least once and a prerequisite
	// is failing now — the service is not active, the loopback endpoint does
	// not answer, or the model that was selected is no longer available.
	// The surface offers repair.
	StateDegraded State = "degraded"
	// StateReady means every prerequisite holds: the service is active, the
	// loopback endpoint answers, and an active model is available.
	StateReady State = "ready"
)

// AllStates returns every state, in the order Classify evaluates them. The
// order is the precedence order: the first condition that matches wins, so a
// caller that renders "the most specific applicable state" gets it by
// scanning this slice rather than by re-deriving the chain.
func AllStates() []State {
	return []State{
		StateDisabled,
		StateUnavailable,
		StateProvisioning,
		StateUnconfigured,
		StateDegraded,
		StateReady,
	}
}

// Observation is everything Classify and AskBluefin are allowed to read.
//
// Every field is a fact some owner establishes, named in the field's comment.
// None of them is inferred from another: ServiceActive is not derived from
// EndpointHealthy, because a daemon that answers on its port while systemd
// reports it failed is a real state and the surface has to be able to show
// it. Provisioned is the one field that cannot be observed at all — it is a
// persisted marker, and it is what separates "never set up" from "was set up
// and has since broken".
type Observation struct {
	// Enabled is the user's intent: the Agent Mode switch is on. The user is
	// the owner; nothing else may set it.
	Enabled bool

	// Supported is whether this host can run llmman at all. Owner: the
	// runtime-validation step of the installation slice (#254).
	Supported bool

	// Provisioning is whether a setup step is in flight. Owner: the same
	// slice, which knows when it has work outstanding.
	Provisioning bool

	// Provisioned is whether setup has completed at least once — llmman is
	// installed, the user service is provisioned, and a model was selected.
	// It is a persisted marker, not an observation: without it, a model that
	// is deleted after setup is indistinguishable from a model that was
	// never chosen, and the surface would offer first-time setup to a user
	// whose working install just broke. Owner: the lifecycle slice (#254).
	Provisioned bool

	// ServiceActive is whether the llmman user service reports active.
	// Owner: the service-lifecycle slice (#254).
	ServiceActive bool

	// EndpointHealthy is whether the loopback endpoint answered its health
	// probe. Owner: the service-lifecycle slice (#254).
	EndpointHealthy bool

	// ActiveModel is whether a model is selected and resolvable through
	// llmman. Owner: the model-catalog slice (#255).
	ActiveModel bool

	// JanConfigured is whether the Jan integration is configured. It is not
	// part of the Agent Mode state — Agent Mode is ready without Jan — but it
	// is a term of the Ask Bluefin predicate. Owner: the dispatcher slice
	// (#261).
	JanConfigured bool
}

// Classify returns the state an observation describes.
//
// The chain is total and ordered, and each step is deliberate:
//
//   - Disabled outranks everything, because it is the user's intent and not
//     a condition to be diagnosed.
//   - Unavailable outranks provisioning, because there is nothing to
//     provision on a host that cannot run the runtime.
//   - Provisioning outranks unconfigured, because a first-time setup that is
//     running has not failed to be configured.
//   - Unconfigured outranks degraded, and is decided by the persisted
//     Provisioned marker rather than by the individual prerequisites.
//   - Degraded is the residual: setup completed, but at least one
//     prerequisite is false.
func Classify(o Observation) State {
	switch {
	case !o.Enabled:
		return StateDisabled
	case !o.Supported:
		return StateUnavailable
	case o.Provisioning:
		return StateProvisioning
	case !o.Provisioned:
		return StateUnconfigured
	case o.ServiceActive && o.EndpointHealthy && o.ActiveModel:
		return StateReady
	default:
		return StateDegraded
	}
}

// Ready reports whether Agent Mode is fully usable. It is exactly
// Classify(o) == StateReady, and exists so call sites do not restate the
// prerequisite list.
func Ready(o Observation) bool {
	return Classify(o) == StateReady
}

// Prerequisite is one unmet condition blocking an Ask Bluefin launch, named
// so the Agent Mode surface can show it. The rendered text for each one
// belongs to internal/views/pageview.
type Prerequisite string

const (
	// PrerequisiteEnabled is the user's own switch.
	PrerequisiteEnabled Prerequisite = "agent-mode-enabled"
	// PrerequisiteHost is the platform llmman supports.
	PrerequisiteHost Prerequisite = "host-support"
	// PrerequisiteProvisioning is not a fault: setup is still running.
	PrerequisiteProvisioning Prerequisite = "provisioning"
	// PrerequisiteEndpoint is the loopback endpoint's health probe.
	PrerequisiteEndpoint Prerequisite = "endpoint-healthy"
	// PrerequisiteModel is a selected, resolvable model.
	PrerequisiteModel Prerequisite = "active-model"
	// PrerequisiteJan is the Jan integration.
	PrerequisiteJan Prerequisite = "jan-integration"
)

// LaunchTarget is what a launch intent opens.
type LaunchTarget string

const (
	// TargetJan launches the ai.jan.Jan Flatpak, already pointed at the local
	// endpoint.
	TargetJan LaunchTarget = "jan"
	// TargetAgentModeSurface opens the Agent Mode surface in Control Center,
	// with every unmet prerequisite visible. It is the fallback for both
	// intents, and it is what opens when Control Center is cold.
	TargetAgentModeSurface LaunchTarget = "agent-mode-surface"
)

// Decision is the answer both launch intents share.
type Decision struct {
	// Target is what must open.
	Target LaunchTarget
	// State is the Agent Mode state at the moment of the decision, so the
	// surface can render the state and the unmet list from one observation
	// rather than taking a second reading that could disagree with this one.
	State State
	// Unmet lists every condition that is false, in the order the surface
	// must present them. It is empty exactly when Target is TargetJan.
	Unmet []Prerequisite
}

// AskBluefin resolves the Ask Bluefin intent — the Custom Command Menu entry
// and the Ctrl+Alt+Backspace shortcut, which are two triggers of this one
// decision.
//
// Jan is launched only when all three terms hold: the local endpoint is
// healthy, an active model is available, and the Jan integration is
// configured. Otherwise the Agent Mode surface opens with the unmet
// prerequisites visible, so the user is shown what is missing rather than a
// chat client that cannot reach anything.
//
// The three terms are evaluated directly rather than through Classify, so the
// predicate is the one the epic states and does not silently widen when the
// state model changes. The two are deliberately not the same predicate: Ready
// requires Enabled, Supported, Provisioned, !Provisioning and ServiceActive on
// top of the endpoint and the model, so Ready(o) implies the endpoint and
// model terms but the converse does not hold. A machine whose service is
// inactive while its endpoint answers with a model loaded is one systemd has
// not caught up with, and the fact a chat client depends on is that the
// endpoint answers — so the dispatcher launches Jan there while the surface
// still reports the service problem. Rewriting this as Ready(o) &&
// o.JanConfigured would refuse Jan on that machine, which is why the
// distinction has its own test.
//
// That gap includes Enabled, and the omission is deliberate rather than an
// oversight of the "disabled outranks everything" rule. The rule governs the
// state this package reports: Classify still answers StateDisabled, and the
// Decision carries that state so the surface never offers to repair a feature
// the user switched off. It does not govern Jan, which the distribution
// installs and owns (ADR-0013's ownership table) and which ChairLift's switch
// never uninstalls. Turning Agent Mode off stops and unprovisions llmman's
// user service through llmman's own commands, so the endpoint stops answering
// and the dispatcher opens the surface with PrerequisiteEnabled on its own.
// Refusing a launch while the endpoint *does* still answer would withhold a
// working chat client because of a switch that no longer describes the
// machine. TestADisabledSwitchStillLaunchesJanWhileTheEndpointAnswers pins
// this row so the behavior is a decision rather than a side effect.
func AskBluefin(o Observation) Decision {
	if o.EndpointHealthy && o.ActiveModel && o.JanConfigured {
		return Decision{Target: TargetJan, State: Classify(o)}
	}
	return Decision{
		Target: TargetAgentModeSurface,
		State:  Classify(o),
		Unmet:  unmetPrerequisites(o),
	}
}

// unmetPrerequisites lists what is blocking, minimally.
//
// The list is gated on the state rather than assembled from every false term,
// because the terms are not independent: on a host that cannot run llmman the
// endpoint is unhealthy as a consequence, and reporting "endpoint unhealthy"
// alongside "host unsupported" would hand the user a repair step that cannot
// work. Each state therefore contributes only the conditions that are its own
// cause.
//
// StateUnconfigured is the one state that names consequences, and it does so
// deliberately: there is no "setup incomplete" prerequisite, because setup is
// not a condition a user restores — it is the act of establishing the
// endpoint, the model, and the integration in the first place. The residual
// list is what setup still owes, which is the cause a first-time user can act
// on. The unavailable state is different only because nothing in its list can
// be acted on at all.
func unmetPrerequisites(o Observation) []Prerequisite {
	switch Classify(o) {
	case StateDisabled:
		return []Prerequisite{PrerequisiteEnabled}
	case StateUnavailable:
		return []Prerequisite{PrerequisiteHost}
	case StateProvisioning:
		return []Prerequisite{PrerequisiteProvisioning}
	}

	var unmet []Prerequisite
	if !o.EndpointHealthy {
		unmet = append(unmet, PrerequisiteEndpoint)
	}
	if !o.ActiveModel {
		unmet = append(unmet, PrerequisiteModel)
	}
	if !o.JanConfigured {
		unmet = append(unmet, PrerequisiteJan)
	}
	return unmet
}

// Intent is a stable launch intent: the fixed name a desktop entry, a menu
// item, or a D-Bus activation uses to ask Control Center for a surface.
type Intent string

const (
	// IntentAgentMode opens the Agent Mode surface directly. It is what the
	// surface's own entry points use.
	IntentAgentMode Intent = "agent-mode"
	// IntentAskBluefin is the dispatcher: Jan when it can work, the surface
	// otherwise.
	IntentAskBluefin Intent = "ask-bluefin"
)

// AllIntents returns every intent, so a completeness test can cover the set
// rather than a hand-copied list of it.
func AllIntents() []Intent {
	return []Intent{IntentAgentMode, IntentAskBluefin}
}

// ResolveIntent returns the decision an intent makes against an observation.
//
// It is deliberately a function of the observation and not of whether a
// Control Center instance is already running. A launch intent resolves to the
// same surface either way: a cold start shows it in the window it creates, and
// a warm start shows it in the window that already exists through the
// application's action mechanism. Nothing here opens a second window, and no
// caller may branch on "already running" to choose a different target — that
// branch is what turns a single application instance into two windows and two
// states.
//
// IntentAgentMode never launches Jan, however ready the machine is: a user who
// asked for the configuration surface is not asking for a chat client.
func ResolveIntent(intent Intent, o Observation) Decision {
	if intent == IntentAskBluefin {
		return AskBluefin(o)
	}
	return Decision{
		Target: TargetAgentModeSurface,
		State:  Classify(o),
		Unmet:  unmetPrerequisites(o),
	}
}

// Owner is the single component that writes an artifact. A value not in this
// set is a second owner, which is the failure this table exists to prevent.
type Owner string

const (
	// OwnerChairLift is this repository.
	OwnerChairLift Owner = "chairlift"
	// OwnerLLMMan is llmman: the daemon, the model store, and the
	// configuration that points at aliases, peers, and credentials.
	OwnerLLMMan Owner = "llmman"
	// OwnerGoose is Goose and the extensions configured into it.
	OwnerGoose Owner = "goose"
	// OwnerOhMyPi is Oh My Pi.
	OwnerOhMyPi Owner = "oh-my-pi"
	// OwnerJan is the Jan Flatpak.
	OwnerJan Owner = "jan"
	// OwnerBluefin is the distribution's own packaging and menu surface.
	OwnerBluefin Owner = "bluefin"
	// OwnerUser is the person at the machine, for artifacts whose content is
	// their own choice and which no program may rewrite.
	OwnerUser Owner = "user"
)

// AllOwners returns every owner, so a test can reject a table entry that
// invents a second writer.
func AllOwners() []Owner {
	return []Owner{
		OwnerChairLift,
		OwnerLLMMan,
		OwnerGoose,
		OwnerOhMyPi,
		OwnerJan,
		OwnerBluefin,
		OwnerUser,
	}
}

// Artifact is one file or service Agent Mode touches, with its single owner
// and its stated cleanup policy.
type Artifact struct {
	// Name is the stable identifier ADR-0013's ownership table uses. The two
	// must agree: the ADR is the prose a maintainer reads and this table is
	// what a gate checks, so a name that appears in only one of them is the
	// drift the gate exists to catch.
	Name string
	// RepoPath is the repository-relative path this artifact occupies when it
	// is a file in this repository, and empty otherwise. It is empty for the
	// artifacts that live on the user's machine or in another project, and
	// for the paths a later slice has not fixed yet — an ADR that cited a
	// path before the slice that creates it would be asserting a file exists
	// when it does not.
	RepoPath string
	// Owner is the single component that writes it.
	Owner Owner
	// Cleanup is what happens to it when Agent Mode is disabled or removed.
	// Every artifact states one, including the artifacts that are deliberately
	// left alone — "left in place" is a decision, and an unstated policy is
	// how a model store or a user's Goose configuration gets deleted by
	// someone who assumed it was ours.
	Cleanup string
	// Issue is the slice that lands it or removes it.
	Issue string
}

// Artifacts is the ownership table: every file and service Agent Mode owns,
// with exactly one owner and a stated cleanup policy for each.
func Artifacts() []Artifact {
	return []Artifact{
		{
			Name:     "agent-mode-state-model",
			RepoPath: "internal/agentmode/agentmode.go",
			Owner:    OwnerChairLift,
			Cleanup:  "Repository code. Removed only by a superseding decision record; it holds no user state.",
			Issue:    "#253",
		},
		{
			Name:    "agent-mode-surface",
			Owner:   OwnerChairLift,
			Cleanup: "The surface and its rows are removed with the feature group. It persists nothing, so removal leaves no user data behind.",
			Issue:   "#256",
		},
		{
			Name:     "local-ai-runtime",
			RepoPath: "internal/aistack/aistack.go",
			Owner:    OwnerChairLift,
			Cleanup:  "Deleted outright, together with the ai_group key and its quadlet. No migration, no cache conversion, and no legacy cleanup: the model cache and the pulled images are left where they are.",
			Issue:    "#256",
		},
		{
			Name:    "local-ai-config-key",
			Owner:   OwnerChairLift,
			Cleanup: "Removed from the schema. A configuration file that still sets ai_group is then an unknown key, which the fail-closed configuration rules (ADR-0003, ADR-0005) reject rather than ignore, so the removal slice must say so in its release notes.",
			Issue:   "#256",
		},
		{
			Name:    "llmman-user-service",
			Owner:   OwnerLLMMan,
			Cleanup: "Stopped and unprovisioned through llmman's own commands when Agent Mode is disabled. llmman's unit and configuration files are never edited directly.",
			Issue:   "#254",
		},
		{
			Name:    "llmman-model-store",
			Owner:   OwnerLLMMan,
			Cleanup: "Left in place when Agent Mode is disabled: the models are large and expensive to re-fetch, and removing them is a disk-space decision the user did not make by turning a switch off. Deletion is a separate, explicitly confirmed action.",
			Issue:   "#255",
		},
		{
			Name:    "llmman-configuration",
			Owner:   OwnerLLMMan,
			Cleanup: "Written only through llmman's own config commands. There is no second writer for aliases, peers, or authentication, so there is nothing to clean up.",
			Issue:   "#260",
		},
		{
			Name:    "ask-bluefin-menu-entry",
			Owner:   OwnerBluefin,
			Cleanup: "The distribution owns the Custom Command Menu entry and the Ctrl+Alt+Backspace binding. The Agent panel may remove the menu entry, and doing so removes neither the shortcut nor Jan.",
			Issue:   "#261",
		},
		{
			Name:    "jan-flatpak",
			Owner:   OwnerJan,
			Cleanup: "Installed and removed by the distribution. Jan is never installed or uninstalled here, so nothing this feature does can leave the user without it.",
			Issue:   "#261",
		},
		{
			Name:    "goose-configuration",
			Owner:   OwnerGoose,
			Cleanup: "Never rewritten. The MCP setup is additive and read-only, and a configuration that already exists is merged with rather than replaced.",
			Issue:   "#257",
		},
		{
			Name:    "goose-provider-selection",
			Owner:   OwnerUser,
			Cleanup: "Invocation-scoped. Goose is pointed at llmman for one launch and nothing is persisted, so the user's own provider settings survive untouched.",
			Issue:   "#257",
		},
		{
			Name:    "linux-mcp-server",
			Owner:   OwnerBluefin,
			Cleanup: "Read-only, restricted to its fixed toolset. It is neither installed nor removed here, and its toolset is never widened.",
			Issue:   "#257",
		},
		{
			Name:    "oh-my-pi-profile",
			Owner:   OwnerOhMyPi,
			Cleanup: "An isolated named profile. Global Oh My Pi authentication, sessions, settings, caches, models, and MCP entries are never read or written, so removing the profile removes everything this feature added.",
			Issue:   "#258",
		},
		{
			Name:    "aggregation-peer-credentials",
			Owner:   OwnerLLMMan,
			Cleanup: "Stored and removed by llmman. They are never read, copied, logged, or distributed.",
			Issue:   "#260",
		},
		{
			Name:    "contributor-appliance-launch",
			Owner:   OwnerBluefin,
			Cleanup: "A terminal session that persists nothing. The isolated appliance is created and destroyed by the contributor tooling, not here.",
			Issue:   "#263",
		},
	}
}

// Boundary is one security boundary Agent Mode must keep.
type Boundary struct {
	// Name is the stable identifier ADR-0013's security section uses.
	Name string
	// Rule is the boundary, stated as the thing that must remain true.
	Rule string
	// Issue is the slice that lands or enforces it.
	Issue string
}

// Boundaries is the security contract. Each rule is written so that a
// reviewer can decide whether a proposed change keeps it, which is the only
// test prose can pass.
func Boundaries() []Boundary {
	return []Boundary{
		{
			Name:  "loopback-binding",
			Rule:  "The ordinary llmman daemon binds loopback only (127.0.0.1:17434). No LAN-facing listener is created, no firewall rule is added or changed, and this machine is never advertised as a peer.",
			Issue: "#254",
		},
		{
			Name:  "web-shell-disabled",
			Rule:  "The user service sets LLMMAN_SHELL=off literally, so llmman's web shell is not reachable at all. Enabling it is a user decision made outside this application, and it must not be offered here.",
			Issue: "#254",
		},
		{
			Name:  "mcp-tool-restrictions",
			Rule:  "linux-mcp-server runs restricted to its fixed toolset. Automatic SSH-key discovery and remote-host access stay off unless the user separately opts in, and the Goose MCP setup is additive and read-only.",
			Issue: "#257",
		},
		{
			Name:  "peer-authentication",
			Rule:  "Consuming an aggregation peer requires an authenticated endpoint. Peers are consumed only, never served: advertising this host, distributing credentials, mutating the firewall, synchronizing model folders, and sharding a model across machines are all out of scope.",
			Issue: "#260",
		},
		{
			Name:  "configuration-ownership",
			Rule:  "llmman's configuration is written only through llmman's own config commands. Goose provider selection stays invocation-scoped, and the Oh My Pi integration is an isolated named profile that never touches global Oh My Pi state.",
			Issue: "#260",
		},
		{
			Name:  "secret-storage",
			Rule:  "Credentials live in llmman's own store. They are never read, copied, logged, or distributed, and no credential crosses into this application's configuration or its action journal.",
			Issue: "#260",
		},
		{
			Name:  "no-privilege-escalation",
			Rule:  "Agent Mode has no pkexec path. Installation, the user service, and every configuration write stay in the invoking account, so no PolicyKit action is added for it and no new privileged helper subcommand may be introduced for it.",
			Issue: "#254",
		},
		{
			Name:  "prompt-history",
			Rule:  "Prompt-history persistence is disabled by default, because diagnostic prompts contain system details. Turning it on is the user's explicit choice.",
			Issue: "#254",
		},
	}
}

// Slice is one issue in the epic's work map, as ADR-0013 publishes it.
type Slice struct {
	// Ref is the issue reference. A bare "#NNN" is an issue in this
	// repository; anything else is written as owner/repo#NNN.
	Ref string
	// Scope is what the slice lands or removes.
	Scope string
}

// WorkMap is the epic's issue map, restricted to what ADR-0013 governs.
//
// The ADR is the reference a later slice reads before it starts, and this
// table is what a gate checks it against: every slice named here must appear
// in the ADR, and the ADR may not cite a bare issue number that is not here.
// That second half is what catches a child issue contradicting the decision —
// a slice renumbered, renamed, or quietly dropped leaves the ADR citing
// something the map does not contain.
func WorkMap() []Slice {
	return []Slice{
		{Ref: "#253", Scope: "This decision: the architecture, the state contract, ownership, and the invariant cutover."},
		{Ref: "#254", Scope: "llmman installation, runtime validation, and the user service lifecycle."},
		{Ref: "#255", Scope: "The live model catalog and the hardware-aware model picker."},
		{Ref: "#256", Scope: "The Agent Mode control surface and the launch intents; removes the Local AI runtime and its config key."},
		{Ref: "#257", Scope: "Goose Desktop plus the hardened, read-only linux-mcp-server."},
		{Ref: "#258", Scope: "The isolated Bluefin Oh My Pi shell and its MCP hookup."},
		{Ref: "#259", Scope: "Oh My Pi performance, cache, concurrency, and failure qualification."},
		{Ref: "#260", Scope: "Safe one-way llmman peer consumption."},
		{Ref: "#261", Scope: "The Ask Bluefin dispatcher, the Jan launch policy, and the menu preference."},
		{Ref: "#262", Scope: "Safe desktop endpoint discovery."},
		{Ref: "#263", Scope: "The contributor-appliance preflight and terminal launch."},
		{Ref: "#264", Scope: "The release gate: end-to-end coverage, documentation, screenshots, and cross-repository verification."},
		{Ref: "ublue-os/homebrew-tap#686", Scope: "Package Oh My Pi."},
		{Ref: "ublue-os/homebrew-tap#687", Scope: "Make the Goose MCP setup additive and explicitly read-only."},
		{Ref: "llmmanorg/llmman#532", Scope: "Launch Goose Desktop without persistent provider mutation."},
		{Ref: "janhq/jan#9009", Scope: "A supported opt-in local llmman provider for Jan."},
		{Ref: "projectbluefin/common#1163", Scope: "Default Jan install, the Ask Bluefin menu entry, and the Ctrl+Alt+Backspace binding."},
		{Ref: "projectbluefin/common#1164", Scope: "Align the AI tools bundle and remove RamaLama from new installs."},
		{Ref: "projectbluefin/common#1102", Scope: "Package the Bluefin contributor command."},
		{Ref: "projectbluefin/contribute#653", Scope: "Permit explicit authenticated local-llmman inference inside the isolated appliance."},
	}
}
