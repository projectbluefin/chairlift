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

Maintainer decisions for the alpha: llmman comes from Homebrew
(`llmmanorg/tap/llmman`); the Jan chat client is the Flatpak `ai.jan.Jan`,
installed through the same Brewfile; ChairLift owns a systemd **user** unit
rather than delegating to `brew services`. Goose, Oh My Pi, peers, the model
picker, Ask Bluefin, and the contributor flow are later children of #252.

## Decision

**Ownership.** llmman owns model storage and inference; Homebrew owns the
`llmman` binary; Flatpak owns Jan. `internal/aistack` owns exactly the
following, each with one cleanup policy:

| Artifact | Owner | Written | Removed |
|---|---|---|---|
| Generated Brewfile (temp file) | `aistack.Enable` | per enable | immediately after `brew bundle install` |
| `~/.config/systemd/user/chairlift-llmman.service` | `aistack` | enable | disable, only after the service is proven stopped |
| `~/.config/environment.d/10-chairlift-llmman.conf` | `aistack` | enable | disable, together with the unit |
| `OLLAMA_HOST` in the session activation environment | `aistack` | enable (best effort) | disable (`systemctl --user unset-environment`) |
| `llmman` binary, Jan, llmman's model store | Homebrew / Flatpak / llmman | enable (install only) | never by ChairLift |

No RamaLama migration or cleanup is performed; units and caches the former
stack wrote are left to the user. Lemonade is out of scope.

**Provisioning.** Enable renders the Brewfile (`tap "llmmanorg/tap"` then
`brew "llmmanorg/tap/llmman"` unless an `llmman` already resolves;
`flatpak "ai.jan.Jan"` on x86_64 only, because its Flathub build is
x86_64-only) and runs it through `internal/homebrew`'s single dry-run-gated
runner. It resolves `llmman`'s absolute path (`$PATH`, then beside
`homebrew.ExecutablePath()`), runs `llmman serve --pull-only`, writes the unit
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

The switch reads on in provisioning, ready, and degraded. `systemctl
is-active` is never readiness on its own.

**Readiness predicate for Ask Bluefin and its shortcut.** Both dispatch the
same predicate: Agent Mode is *ready* (above) **and** an active model is
available on the endpoint **and** the Jan integration is configured for this
architecture. Otherwise they open the Agents page with the unmet prerequisite
visible. In this alpha no model picker or Jan integration exists yet, so the
predicate cannot be met and no launcher is wired; #255 and #261 complete it.

**Security boundaries.**

- *Binding:* `LLMMAN_HOST=127.0.0.1:17434`. llmman refuses a reachable bind
  without keys; ChairLift provisions no keys and never sets `LLMMAN_AUTH=off`.
- *Web shell:* the unit carries the literal `LLMMAN_SHELL=off`.
- *Prompt history:* `LLMMAN_NOHISTORY=1`, because troubleshooting prompts carry
  system details.
- *CORS:* no `LLMMAN_ORIGINS`. If Jan needs its Tauri origin, only that exact,
  verified origin may be added; wildcard CORS is forbidden.
- *Discovery:* only `OLLAMA_HOST` is published session-wide. `OPENAI_BASE_URL`
  and `OPENAI_API_KEY` are never set by default; running processes are never
  claimed to have changed.
- *MCP:* later clients run `linux-mcp-server` only with `--toolset FIXED`;
  SSH-key discovery and remote-host tools stay off unless separately opted in.
- *Peers:* client-side consumption only, configured through `llmman config
  set`, authenticated with llmman's own peer key; this host is never
  advertised as a peer, and no firewall, key distribution, folder sync, or
  sharding is performed. Known peer addresses and their enabled states are
  tracked locally in `~/.local/share/chairlift/agent-mode-peers.json`; the peer
  key is passed to `llmman config set` on argv (a known limitation until stdin
  support lands).
- *Configuration ownership:* ChairLift writes no llmman TOML; aliases, peers,
  and auth go through `llmman config set/get`. Goose provider choice stays
  invocation-scoped (`llmman launch goose`); OMP uses a named, isolated
  profile.
- *Secrets:* ChairLift stores and logs no API key, provider credential, or
  prompt.
- *Privilege:* no pkexec route, helper subcommand, or PolicyKit action.

**Issue map.** #253 (this decision), #254 (llmman provisioning and user
unit), #262 (`OLLAMA_HOST` discovery) land with it. #255 model picker, #256
control surface and launch intents, #257 Goose, #258/#259 Oh My Pi, #260
peers, #261 Ask Bluefin dispatcher and Jan launch policy, #263 contributor
flow, and #264 acceptance build on this contract.

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
