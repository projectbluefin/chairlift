# 0015 — Agent Mode runs llmman as a ChairLift-owned user unit

- **Status:** Accepted
- **Date:** 2026-09-24

## Context

The Agents page shipped a "Local AI" switch backed by a rootless container
stack: a per-GPU-vendor image table pinned by digest, a served-model override,
and a quadlet under `~/.config/containers/systemd`. Epic
[#252](https://github.com/projectbluefin/chairlift/issues/252) replaces it with
a first-class **Agent Mode** built on
[llmman](https://github.com/llmmanorg/llmman), which:

- chooses the engine and backend for the hardware itself (container runtime,
  its own prebuilt `llama.cpp`, or a `llama-server` on `$PATH`) and owns the
  model store;
- serves Ollama-, OpenAI- and Anthropic-compatible APIs, listening on
  `127.0.0.1:17434` by default (`LLMMAN_HOST`);
- offers `serve --pull-only`, the one mode in which a failed engine fetch is an
  error rather than a warning;
- exposes `GET /llmman/node` (memory, loaded and stored models) and a web UI
  whose Shell tab is a terminal as the daemon's user unless `LLMMAN_SHELL` is
  off; records prompts for `llmman log` unless `LLMMAN_NOHISTORY` is set.

The local model runtime comes from Homebrew (`llmmanorg/tap/llmman`);
ChairLift owns a systemd **user** unit rather than delegating to `brew services`.
The owner's 2026-10-01 client cutover selects Goose, with its separate
diagnostic setup on Help. Enabling Agent Mode installs no chat client.

## Decision

**Ownership.** llmman owns model storage and inference; Homebrew owns the
`llmman` binary. `internal/aistack` owns exactly the following, each with one
cleanup policy:

| Artifact | Owner | Written | Removed |
|---|---|---|---|
| Generated Brewfile (temp file) | `aistack.Enable` | per enable | immediately after `brew bundle install` |
| `~/.config/systemd/user/chairlift-llmman.service` | `aistack` | enable | disable, only after the service is proven stopped |
| `~/.config/environment.d/10-chairlift-llmman.conf` | `aistack` | enable | disable, together with the unit |
| `OLLAMA_HOST` in the session activation environment | `aistack` | enable (best effort) | disable (`systemctl --user unset-environment`) |
| `llmman` binary and model store | Homebrew / llmman | enable (install only) | never by ChairLift |

No RamaLama migration or cleanup is performed; units and caches the former
stack wrote are left to the user. Lemonade is out of scope.

**Provisioning.** Enable renders the Brewfile (`tap "llmmanorg/tap"` then
`brew "llmmanorg/tap/llmman"` unless an executable already resolves) and runs
it through `internal/homebrew`'s single dry-run-gated runner. No Flatpak or
chat client is part of runtime provisioning. It resolves the absolute path
(`$PATH`, then beside `homebrew.ExecutablePath()`), runs `llmman serve --pull-only`, writes the unit
and fragment atomically, and runs `daemon-reload`, `enable`, `restart`. A
failure after writing removes what that call created, unless the unit already
existed. Every mutation is behind `dryrun.Enabled()`.

**States.** `aistack.Resolve` is the single state function:

| State | Predicate |
|---|---|
| unavailable | no Homebrew (the capability floor; the page is hidden) |
| unconfigured | no `llmman` resolves and no unit |
| provisioning | an enable or disable is in flight, or the unit exists and readiness has not been probed yet |
| ready | the unit exists and `GET /llmman/node` answers 200 with a JSON object within 2 s |
| degraded | the unit exists and `/llmman/node` does not answer |
| disabled | `llmman` resolves but no unit — turned off, software and models kept |

The switch reflects the configured service after its first readiness probe;
while that probe runs it is off and insensitive. `systemctl is-active` is
never readiness on its own. The model and preset rows remain visible but
insensitive until ready, with the unmet prerequisite shown. An empty alias
means no model is selected, never an invented Qwen default. Readiness and
model reads are generation-guarded; selecting a preset and toggling the
service share one mutation gate. A failed operation re-observes the unit and
endpoint rather than assuming nothing changed.
Selecting a model writes its canonical `hf.co/...` target to the
`bluefin-active` alias, restarts the owned user unit, and waits for readiness
before reporting selection. llmman reads aliases at startup: a saved alias
alone does not prove that the running daemon can serve it. A missing alias is
the ordinary no-selection state.


**Current Control Center surface (2026-10-03).** At the owner's request,
the page focuses on this computer: one **Agent Mode** switch, visible
**Active Model** and **Recommended Presets** rows, a **Goose** row with Launch
action, a **Show Ask Bluefin in menu** preference, and the selectable local
OpenAI-compatible connection address `http://127.0.0.1:17434/v1`.
Connection instructions are not hidden behind an expander.
Goose Desktop (`ublue-os/tap/goose-linux`) is the Agent Mode desktop GUI.
It is launched through llmman's invocation-scoped integration (`llmman launch goose-desktop --model <active-model>`)
with the active model without persisting provider or model into Goose's configuration.
Readiness requires Goose Desktop and `linux-mcp-server` installed and the hardened
Linux diagnostic extension verified on disk (stdio, enabled, real executable,
`--toolset FIXED`, and no SSH defaults).
`chairlift --ask-bluefin` dispatches to Goose Desktop when all prerequisites are met,
or presents Control Center on the Agents page naming the missing prerequisite.
Custom Command Menu visibility is managed via user-layer override/reset.
The remote-machine administration surface and its backend are
removed, avoiding a second configuration workflow on this local-mode page.
ChairLift leaves unrelated llmman configuration intact; it changes only its
selected-model alias through llmman's CLI and overrides aggregation in the unit.


**Security boundaries.**

- *Binding:* `LLMMAN_HOST=127.0.0.1:17434`. llmman refuses a reachable bind
  without keys; ChairLift provisions no keys and never sets `LLMMAN_AUTH=off`.
- *Local-only inference:* fixed `Environment=LLMMAN_PEERS=` overrides legacy
  aggregation settings, so removed controls cannot leave hidden offload active.
  Migration proves application with the live systemd `InvocationID`, stamped
  in the owned unit only after reload/restart succeeds. A matching file alone
  cannot bless a legacy daemon after an interrupted upgrade.
- *Web shell:* the unit carries the literal `LLMMAN_SHELL=off`.
- *Prompt history:* `LLMMAN_NOHISTORY=1`, because troubleshooting prompts carry
  system details.
- *CORS:* no `LLMMAN_ORIGINS`; wildcard CORS is forbidden.
- *Discovery:* only `OLLAMA_HOST` is published session-wide. `OPENAI_BASE_URL`
  and `OPENAI_API_KEY` are never set by default; running processes are never
  claimed to have changed.
- *MCP:* later clients run `linux-mcp-server` only with `--toolset FIXED`;
  SSH-key discovery and remote-host tools stay off unless separately opted in.
- *Configuration ownership:* ChairLift writes no llmman TOML. Its selected
  model alias is read and written through `llmman config get/set`.
  ChairLift owns the Goose configuration repair in-process. Before replacing an
  existing user Goose configuration, it saves a recoverable backup
  (`config.yaml.chairlift-backup`, 0600) atomically, only when contents change,
  never under dry-run, and never on fresh creation.
- *Secrets:* ChairLift stores and logs no API key, provider credential, or
  prompt, and this surface accepts no credentials.
- *Privilege:* no pkexec route, helper subcommand, or PolicyKit action.

**Issue map.** #253 (this decision), #254 (llmman provisioning and user
unit), #262 (`OLLAMA_HOST` discovery) land with it. #255 model picker, #256
control surface and launch intents, #257 Goose (in-process configuration repair
with recoverable backup), #258/#259 Oh My Pi, #261 Ask Bluefin dispatcher, #263
contributor flow, and #264 acceptance build on this contract.

## Consequences

- ChairLift carries no image table, GPU-vendor selection, or model override;
  `ai_images` and `ai_model` are removed from the config schema, and the Agents
  page's capability floor moves from Podman to Homebrew.
- Engine choice is llmman's. An NVIDIA host without the container toolkit may
  fall back to CPU; the `serve --pull-only` output is logged, but the alpha does
  not yet parse and display the selected backend. #256 owns surfacing it.
- A tap outage or an untrusted-tap refusal fails the enable with Homebrew's
  diagnostic; nothing is written in that case.
- Environment changes reach only processes started afterwards; the UI says
  already-open apps and terminals must restart.

## Alternatives considered

- **Keep the container stack and add llmman beside it:** two runtimes with two
  state owners; rejected by #252's locked decisions.
- **`brew services start llmman`:** hands lifecycle, environment, and bind
  address to a plist ChairLift cannot pin; the unit could not carry
  `LLMMAN_SHELL=off` or `LLMMAN_NOHISTORY=1` under ChairLift's ownership.
- **Hardcode `/home/linuxbrew/.linuxbrew/bin/llmman` or `%h/.linuxbrew`:**
  breaks every non-default prefix; the path is resolved at setup time instead.
- **Session-wide `OPENAI_BASE_URL`:** silently redirects unrelated cloud
  clients; rejected.

## References

- Shapes: [design/overview.md § Agent Mode](../design/overview.md#agent-mode)
- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md),
  [ADR-0014](0014-capability-driven-visibility-as-a-floor.md)
- Epic: [#252](https://github.com/projectbluefin/chairlift/issues/252)
