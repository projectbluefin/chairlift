# 0013 — Agent Mode replaces Local AI, and one state contract owns readiness

- **Status:** Accepted
- **Date:** 2026-09-22

## Context

ChairLift's local-AI feature is one switch over one container. `internal/aistack`
writes a single rootless Quadlet whose image is chosen by `internal/gpu`, the
`ai_group` key on the Features page turns it on and off, and nothing else in the
application knows the feature exists. That shape cannot express the product the
Bluefin suite is moving to, for three reasons that are structural rather than
cosmetic.

First, the runtime is not one container. Agent Mode's inference is served by
`llmman`, which owns model storage, model selection, and the choice of engine
and backend for the detected hardware. A container definition in this
repository would be a second, competing authority over all four.

Second, there is no single client. Goose is the GUI troubleshooting client, Oh
My Pi is the configured shell, and Jan is the Ask Bluefin chat client, and each
reaches the same daemon with a different configuration obligation. A switch
that reports "on" has nothing to say about whether any of them can work.

Third, and the reason this record exists, there is no state. A user reaches
Agent Mode through at least three doors — the Features page, the Ask Bluefin
entry in the Custom Command Menu, and the Ctrl+Alt+Backspace shortcut — and the
epic requires two of those to be the *same* dispatcher. If each door decides
for itself whether the endpoint answers and a model is loaded, they diverge,
and the divergence is user-visible in the worst way: a shortcut that opens a
chat client which cannot reach anything, while the page that knows why is not
on screen.

The epic (projectbluefin/chairlift#252) locks the decisions this record makes
binding and splits the work across twelve slices and six cross-repository
dependencies. What follows is the architecture those slices share, the state
model they read, and the ownership boundary between ChairLift and everything
else in the stack.

This record is deliberately written before the code it governs. The state
contract in `internal/agentmode/agentmode.go` is the executable half of it: the
package is pure, its tables are the ones below, and
`internal/installcheck/agentmode_test.go` fails when the two drift apart.

## Decision

Agent Mode is a first-class Control Center surface backed by `llmman`, and it
owns exactly four things: the surface itself, the state model that says what to
draw, the readiness decision every launch path shares, and the recommended
model. Everything else in the stack belongs to the component that already owns
it.

The current Local AI implementation is **removed**, not extended. `ai_group`
and `internal/aistack` do not become a fallback path, a compatibility mode, or
a second runtime: a host may have one local-AI implementation, and after this
epic it is Agent Mode. The removal itself is slice #256.

### The six states

Agent Mode is in exactly one of six states, and `agentmode.Classify` derives it
from observation alone. The order below is the precedence order: the first
condition that holds decides, and a later condition never overrides an earlier
one.

| State | Meaning | Decided by |
| --- | --- | --- |
| `disabled` | The user turned Agent Mode off. | The user's own switch. Outranks everything: an explicit "off" is not a malfunction to diagnose, and a surface that reported `degraded` for a feature the user switched off would ask them to fix something they chose. |
| `unavailable` | This host cannot run `llmman` at all. | The runtime-validation step of #254. Terminal — no configuration moves it, so the surface explains rather than offers. |
| `provisioning` | A setup step is in flight: installing `llmman`, starting the user service, or downloading the selected model. | The installation slice. Transient and not a failure, so the surface reports progress instead of an unmet prerequisite. |
| `unconfigured` | Setup has never completed: no model has been selected and the user service has not been provisioned. | A persisted *provisioned* marker, not an observation. |
| `degraded` | Setup completed at least once and a prerequisite is failing now. | The residual: provisioned, but the service is not active, the endpoint does not answer, or the selected model is no longer available. |
| `ready` | Every prerequisite holds. | Service active, endpoint healthy, active model available. |

The `unconfigured`/`degraded` boundary is the one that needs a persisted fact
and cannot be inferred from the machine's present condition. Without it, a
model deleted after a working install is indistinguishable from a model never
chosen, and the surface offers first-time setup to a user whose installation
just broke. The marker is owned by the lifecycle slice (#254) and is the only
field in the contract that is remembered rather than observed.

`ready` does not require Jan. Agent Mode is a working local-inference stack
whether or not a chat client is configured, and folding the client's
configuration into the stack's readiness would report a broken Agent Mode on
every host that simply has not installed Jan yet.

### The readiness predicate

Two predicates exist, and they are deliberately not the same one.

**Agent Mode readiness** — used by the surface to decide what to draw — is
`service active ∧ endpoint healthy ∧ active model available`.

**The Ask Bluefin predicate** — used by the dispatcher, and therefore by both
the Custom Command Menu entry and the Ctrl+Alt+Backspace shortcut — is exactly
three terms:

```
launch Jan  ⟺  local endpoint healthy  ∧  active model available  ∧  Jan integration configured
otherwise      open the Agent Mode surface, with the unmet prerequisite visible
```

The dispatcher's terms do not include the service's systemd state. A machine
whose service is inactive while the endpoint answers with a model loaded is one
systemd has not caught up with, and the fact a chat client depends on is that
the endpoint answers. The two predicates must not be collapsed into
`AgentModeReady ∧ JanConfigured`: that rewrite would refuse Jan on exactly that
machine. `internal/agentmode` holds the distinction as a test rather than as a
comment.

The dispatcher's terms do not include the user's switch either, and that
follows from the same reasoning rather than contradicting `disabled`'s
precedence. `disabled` outranks every other *state*, and the decision carries
that state, so the surface never offers to repair a feature the user switched
off. Jan is not that feature: the distribution installs and owns it, and
nothing this switch does removes it. Turning Agent Mode off stops and
unprovisions `llmman`'s user service, so the endpoint stops answering and the
dispatcher opens the surface with the switch as the unmet prerequisite on its
own. Refusing a launch on a machine whose endpoint still answers would withhold
a working chat client on the strength of a switch that no longer describes the
machine, so that row is pinned by a test rather than left as an accident.

Both intents resolve through one function so the menu entry and the shortcut
cannot disagree. A cold start and a warm start resolve to the same surface: the
intent never opens a second window, and no caller may branch on "an instance is
already running" to choose a different target.

The unmet-prerequisite list is gated on the state, minimally. On a host that
cannot run `llmman` the endpoint is unhealthy as a *consequence*, and reporting
"endpoint unhealthy" beside "host unsupported" would hand the user a repair
step that cannot work. Each state therefore contributes only the conditions
that are its own cause, and the surface shows the first one as primary.

`unconfigured` is the one state whose list names conditions setup has not yet
established — the endpoint, the model, and the integration — rather than a
single "setup incomplete" prerequisite. There is no such prerequisite because
setup is not a condition a user restores; it is the act of establishing those
three, so the residual list *is* the cause a first-time user can act on. The
contrast with `unavailable` holds: there, nothing in the list can be acted on
at all.

### Ownership

Every file and service Agent Mode touches has one owner and a stated cleanup
policy. "One owner" means one writer: the row names the component that may
write it, and every other component reads it or asks for the change through
that owner.

| Artifact | Owner | Cleanup policy | Slice |
| --- | --- | --- | --- |
| `agent-mode-state-model` — `internal/agentmode/agentmode.go` | chairlift | Repository code. Removed only by a superseding decision record; it holds no user state. | #253 |
| `agent-mode-surface` | chairlift | The surface and its rows are removed with the feature group. It persists nothing, so removal leaves no user data behind. | #256 |
| `local-ai-runtime` — `internal/aistack/aistack.go` | chairlift | Deleted outright, together with the `ai_group` key and its quadlet. No migration, no cache conversion, and no legacy cleanup: the model cache and the pulled images are left where they are. | #256 |
| `local-ai-config-key` — `ai_group` | chairlift | Removed from the schema. A configuration file that still sets `ai_group` is then an unknown key, which the fail-closed configuration rules (ADR-0003, ADR-0005) reject rather than ignore, so the removal slice must say so in its release notes. | #256 |
| `llmman-user-service` — the rootless user service on `127.0.0.1:17434` | llmman | Stopped and unprovisioned through `llmman`'s own commands when Agent Mode is disabled. `llmman`'s unit and configuration files are never edited directly. | #254 |
| `llmman-model-store` | llmman | Left in place when Agent Mode is disabled: the models are large and expensive to re-fetch, and removing them is a disk-space decision the user did not make by turning a switch off. Deletion is a separate, explicitly confirmed action. | #255 |
| `llmman-configuration` — aliases, peers, authentication | llmman | Written only through `llmman`'s own config commands. There is no second writer for aliases, peers, or authentication, so there is nothing to clean up. | #260 |
| `ask-bluefin-menu-entry` — the Custom Command Menu entry and the Ctrl+Alt+Backspace binding | bluefin | The distribution owns the Custom Command Menu entry and the Ctrl+Alt+Backspace binding. The Agent panel may remove the menu entry, and doing so removes neither the shortcut nor Jan. | #261 |
| `jan-flatpak` — `ai.jan.Jan` | jan | Installed and removed by the distribution. Jan is never installed or uninstalled here, so nothing this feature does can leave the user without it. | #261 |
| `goose-configuration` — `~/.config/goose/config.yaml` | goose | Never rewritten. The MCP setup is additive and read-only, and a configuration that already exists is merged with rather than replaced. | #257 |
| `goose-provider-selection` | user | Invocation-scoped. Goose is pointed at `llmman` for one launch and nothing is persisted, so the user's own provider settings survive untouched. | #257 |
| `linux-mcp-server` | bluefin | Read-only, restricted to its fixed toolset. It is neither installed nor removed here, and its toolset is never widened. | #257 |
| `oh-my-pi-profile` | oh-my-pi | An isolated named profile. Global Oh My Pi authentication, sessions, settings, caches, models, and MCP entries are never read or written, so removing the profile removes everything this feature added. | #258 |
| `aggregation-peer-credentials` | llmman | Stored and removed by `llmman`. They are never read, copied, logged, or distributed. | #260 |
| `contributor-appliance-launch` | bluefin | A terminal session that persists nothing. The isolated appliance is created and destroyed by the contributor tooling, not here. | #263 |

Two consequences follow directly from the table. The first is that ChairLift
owns no model store: nothing in this repository may cache, copy, prune, or
mirror a model file, because the second writer is how a store becomes
unreadable to its owner. The second is that no file owned by another component
is edited in place — not Goose's configuration, not `llmman`'s TOML, not a
global Oh My Pi profile — and a slice that needs a change in one of them owes
an upstream change request instead.

### Security boundaries

| Boundary | Rule | Slice |
| --- | --- | --- |
| `loopback-binding` | The ordinary `llmman` daemon binds loopback only (`127.0.0.1:17434`). No LAN-facing listener is created, no firewall rule is added or changed, and this machine is never advertised as a peer. | #254 |
| `web-shell-disabled` | The user service sets `LLMMAN_SHELL=off` literally, so `llmman`'s web shell is not reachable at all. Enabling it is a user decision made outside this application, and it must not be offered here. | #254 |
| `mcp-tool-restrictions` | `linux-mcp-server` runs restricted to its fixed toolset. Automatic SSH-key discovery and remote-host access stay off unless the user separately opts in, and the Goose MCP setup is additive and read-only. | #257 |
| `peer-authentication` | Consuming an aggregation peer requires an authenticated endpoint. Peers are consumed only, never served: advertising this host, distributing credentials, mutating the firewall, synchronizing model folders, and sharding a model across machines are all out of scope. | #260 |
| `configuration-ownership` | `llmman`'s configuration is written only through `llmman`'s own config commands. Goose provider selection stays invocation-scoped, and the Oh My Pi integration is an isolated named profile that never touches global Oh My Pi state. | #260 |
| `secret-storage` | Credentials live in `llmman`'s own store. They are never read, copied, logged, or distributed, and no credential crosses into this application's configuration or its action journal. | #260 |
| `no-privilege-escalation` | Agent Mode has no `pkexec` path. Installation, the user service, and every configuration write stay in the invoking account, so no PolicyKit action is added for it and no new privileged helper subcommand may be introduced for it. | #254 |
| `prompt-history` | Prompt-history persistence is disabled by default, because diagnostic prompts contain system details. Turning it on is the user's explicit choice. | #254 |

`no-privilege-escalation` is the boundary a reviewer is most likely to be
asked to bend, and it is the one with the least room. Agent Mode installs into
the user's account and runs a user service; a privileged helper for it would
extend the fixed-path PolicyKit surface (ADR-0001) to a component whose entire
configuration is user-writable, which is the combination ADR-0001 exists to
prevent. Nothing about Agent Mode requires root, and a slice that appears to
need it has a design problem rather than a privilege problem.

### The issue map

This record governs the epic's foundation and every slice that touches the
surface, the dispatcher, or an artifact in the ownership table above. The map
below is the authoritative list; a slice whose number is not here does not
change the architecture this record fixes.

| Slice | Scope |
| --- | --- |
| #253 | This decision: the architecture, the state contract, ownership, and the invariant cutover. |
| #254 | `llmman` installation, runtime validation, and the user service lifecycle. |
| #255 | The live model catalog and the hardware-aware model picker. |
| #256 | The Agent Mode control surface and the launch intents; removes the Local AI runtime and its config key. |
| #257 | Goose Desktop plus the hardened, read-only `linux-mcp-server`. |
| #258 | The isolated Bluefin Oh My Pi shell and its MCP hookup. |
| #259 | Oh My Pi performance, cache, concurrency, and failure qualification. |
| #260 | Safe one-way `llmman` peer consumption. |
| #261 | The Ask Bluefin dispatcher, the Jan launch policy, and the menu preference. |
| #262 | Safe desktop endpoint discovery. |
| #263 | The contributor-appliance preflight and terminal launch. |
| #264 | The release gate: end-to-end coverage, documentation, screenshots, and cross-repository verification. |

Cross-repository dependencies, none of which this repository can complete:
`ublue-os/homebrew-tap#686` (package Oh My Pi), `ublue-os/homebrew-tap#687`
(make the Goose MCP setup additive and explicitly read-only),
`llmmanorg/llmman#532` (launch Goose Desktop without persistent provider
mutation), `janhq/jan#9009` (a supported opt-in local `llmman` provider),
`projectbluefin/common#1163` (default Jan install, the Ask Bluefin menu entry,
and the Ctrl+Alt+Backspace binding), `projectbluefin/common#1164` (align the AI
tools bundle and remove RamaLama from new installs),
`projectbluefin/common#1102` (package the Bluefin contributor command), and
`projectbluefin/contribute#653` (permit explicit authenticated local-`llmman`
inference inside the isolated appliance).

Dependency order: #253 → #254 → #255 → #256, with the integration slices
branching from that foundation. #257 and #258 feed #259. #261 plus the Jan
contract feed `projectbluefin/common#1163`. #258 plus the contributor
dependencies feed #263. #264 closes only when every branch above is complete.

## Consequences

The launch paths cannot disagree. The surface, the menu entry, and the
shortcut read one decision function, so a change to what "ready" means lands
in one place and the three doors move together. The cost is that the surface
now depends on a package it did not before, and that package's `Observation`
must be populated from real probes rather than from guesses — a field nobody
can produce is a field that silently reads `false` and pins the surface in
`degraded`.

Removing `ai_group` is a breaking configuration change. Unknown keys are a
hard error under ADR-0003's fail-closed rule, so an administrator's
`/etc/chairlift/config.yml` that still carries the key will fail the whole
configuration rather than lose one group. That is the correct behavior — a
silent drop is how a configuration file stops describing the machine — but it
is a visible break for anyone who had customized the Local AI images or model,
and #256 owes a release note saying so.

Removing the runtime also removes the two facts AGENTS.md currently states
about it: the four digest-pinned images and the stop-before-remove rule for its
quadlet. Both belonged to `internal/aistack`. The reasoning that produced them
is not lost — `docs/skills/multi-arch-digest-pinning/SKILL.md` carries the
manifest-index rule for any future pinned reference — but the specific pins
are gone with the code that used them, and no slice may reintroduce a container
definition in this repository to hold them.

Agent Mode is harder to test than the switch it replaces, because its
prerequisites are a running daemon, a downloaded model, and a Flatpak. Nothing
in the state contract runs any of them: `Classify` and the dispatcher are pure
functions of an observation, so the state machine is covered on a headless
runner and the surface's job reduces to producing an honest observation. The
end-to-end path is #264's, in the real desktop environment.

## Alternatives considered

- **Extend `internal/aistack` into Agent Mode.** Rejected: the switch is a
  quadlet writer, and Agent Mode needs a service lifecycle, a model store it
  does not own, and a readiness model. Extending it would leave the RamaLama
  container as a second runtime path, which is the outcome the epic forbids.
- **Keep `ai_group` as a compatibility alias for the new surface.** Rejected:
  the key names a container image and a model reference, neither of which
  survives the change of runtime. An alias would accept settings it cannot
  honor, which is worse than rejecting the file.
- **Let each launch path compute readiness.** Rejected: it is the status quo
  this record exists to end, and the divergence is not hypothetical — the
  shortcut and the page would each have to re-derive the same predicate from a
  different set of probes.
- **Collapse the dispatcher onto `AgentModeReady ∧ JanConfigured`.** Rejected:
  it adds a fourth term the epic's predicate does not have and refuses Jan on a
  machine whose endpoint is serving. The distinction is held by a test.
- **Derive `unconfigured` from the absence of prerequisites.** Rejected:
  without a persisted marker a deleted model and a never-chosen model are the
  same observation, so a broken install would be reported as first-time setup.
- **Give the surface its own copy of the ownership table for display.**
  Rejected: the table is the boundary. A display copy is a second list to keep
  in step, and the gate that compares the ADR to the contract would not see it.
- **Ship a `pkexec` helper so setup can install system-wide.** Rejected:
  nothing in Agent Mode requires root, and it would extend the fixed-path
  PolicyKit surface (ADR-0001) to a user-writable component.

## References

- Shapes: [design/overview.md](../design/overview.md),
  [reference.md](../reference.md), [walkthrough.md](../walkthrough.md)
- Governs: the epic [projectbluefin/chairlift#252](https://github.com/projectbluefin/chairlift/issues/252)
  and the slices in the issue map above
- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md),
  [ADR-0003](0003-two-tier-config-with-fail-closed-semantics.md),
  [ADR-0005](0005-config-schema-reflected-from-canonical-struct.md),
  [ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md),
  [ADR-0010](0010-docs-are-a-ci-gated-artifact.md),
  [ADR-0012](0012-ship-as-control-center-keep-chairlift-code-name.md)
