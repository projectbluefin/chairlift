# AGENTS

ChairLift is a GTK4/Libadwaita system-management GUI for
[Snow Linux](https://github.com/frostyard/snosi), written in idiomatic Go using
[puregotk](https://codeberg.org/puregotk/puregotk) bindings — **no CGO**. GTK,
Libadwaita, and GLib shared libraries are loaded at runtime via `dlopen`. The UI
is YAML-configuration-driven; feature groups toggle on and off per host.

## Build, test, lint

The app builds pure-Go (`CGO_ENABLED=0`); the race detector needs CGO.

- `make build` — builds `build/chairlift`, `build/chairlift-updex-helper`, and
  `build/chairlift-ublue-helper` (all `CGO_ENABLED=0`).
- `make test` — `go test ./...`.
- `make fmt` — `gofmt -s -w .`.
- `make lint` — `golangci-lint run`.
- `make ci` — runs every **host-independent** CI gate, in CI's order (go.mod
  tidy check, `go vet`, gofmt check, lint, unit tests, race detector, build).
  The build step reproduces CI's `linux/amd64` + `linux/arm64` matrix into
  `build/ci-linux-<arch>/`, then rebuilds natively, so a cross-arch-only
  compile failure cannot pass locally and break CI. Run it before pushing;
  the mill's deep gate calls this exact target. Codecov's remote project status
  additionally rejects coverage regressions greater than one percentage point;
  it has no fixed coverage target and cannot be mirrored locally.
- `make e2e` — builds both executables, checks the application's real
  `--help` surface, starts the dry-run GTK window under a private D-Bus/Xvfb
  session, stages `make install`, and executes the installed privileged
  helper's rejection paths. Startup polls the three readiness log markers for
  up to 30 seconds, requires one additional second of process stability, and
  terminates the private process group as soon as the smoke check passes. It
  requires GTK4, Libadwaita, `dbus-run-session`, and `xvfb-run`; the hosted E2E job
  installs those runtime dependencies explicitly because ordinary unit-test
  hosts intentionally do not carry them.
- `make install`'s default `PREFIX` is `/usr` — the only prefix under which
  the installed PolicyKit policy files land where `polkitd` reads them
  (`/usr/share/polkit-1/actions`) and the updex helper's installed
  path matches its fixed `pkexec` exec-path annotation (see the privilege
  boundary invariant below). It installs maintainer defaults at
  `/usr/share/chairlift/config.yml` and must never install or overwrite the
  administrator-owned `/etc/chairlift/config.yml`. GoReleaser publishes both
  the self-contained `projectbluefin-chairlift` package and the mutually exclusive
  `projectbluefin-chairlift-system-integration` companion for user-scoped GUI
  installs; every nFPM entry carrying policies must retain the same fixed
  paths.

CI (`.github/workflows/test.yml`) filters tests with `-run "^Test[^I]"
-skip "Integration"`. That filter excludes *any* test whose name begins `TestI`
— not only `TestIntegration` — or contains `Integration` anywhere. Those names
are reserved for tests that require a real environment (a live `brew`,
`flatpak`, `bootc`, or GTK display). Ordinary unit tests must not use the
`TestI` prefix: a test that trips the filter is never executed by `make ci` or
by CI and therefore protects nothing. The accident is easy to make, because
plain unit-test names such as `TestIsValid`, `TestInitConfig`, or `TestIndexOf`
all start with `TestI` and would be silently skipped; name them so the first
letter after `Test` is not `I` (see the GTK-headless skill below).

The separately invoked tests under `test/e2e/` are outside the
`./internal/...` unit-test scope by design. They are enforced by the E2E
workflow's explicit `make e2e` step; do not assume adding a test outside
`internal/` is enough without that dedicated gate.

There are no generated files and no codegen step; everything under version
control is hand-written Go, YAML, and data assets.

## Repository invariants

An agent must not break these:

- **Privilege boundary.** State-changing operations that require root go
  through `pkexec` (PolicyKit) with fixed, installed polkit policies and fixed
  helper binaries only: `pkexec /usr/libexec/bootc-update-stage` (action
  `io.projectbluefin.chairlift.bootc.stage`), `pkexec
  /usr/libexec/snosi-sysupdate-stage` (`internal/sysupdate.StageScriptPath`,
  action `io.projectbluefin.chairlift.sysupdate.stage`, native A/B hosts),
  `pkexec /usr/bin/chairlift-updex-helper` (`internal/updex.HelperPath`, actions
  `io.projectbluefin.chairlift.updex.{enable-feature,disable-feature,update}`), and
  `pkexec /usr/bin/chairlift-ublue-helper` (`internal/ublue.HelperPath`,
  actions `io.projectbluefin.chairlift.ublue.*` — see the helper-extension
  invariant below for the full subcommand list)
  — always that fixed absolute path, matching the
  `org.freedesktop.policykit.exec.path` annotation, with the updex subcommand
  matching `org.freedesktop.policykit.exec.argv1`. The helper must strictly
  reject unsupported argv because PolicyKit does not validate arguments after
  action selection. ChairLift ships no passwordless PolicyKit rules; normal
  administrator authentication applies. Homebrew tap trust (`brew trust`) is
  deliberately per-user and does **not** use pkexec, and neither does gaming
  mode, whose components are all user-scope Flatpaks. Do not add arbitrary
  privileged command execution, broaden what pkexec runs, or route new
  mutations around the fixed helper/policy pair.
- **Neither an image reference nor a username crosses the ublue pkexec
  boundary.** `chairlift-ublue-helper` receives a channel word only, and
  derives the concrete `bootc switch` target itself from the read-only image
  descriptor plus the channel table; it derives the account to modify from
  the `PKEXEC_UID` pkexec sets, never from argv. Accepting either as an
  argument would let an authenticated caller switch the machine to an
  arbitrary image, or add an arbitrary account to the privileged developer
  groups. `internal/ublue`'s test asserts that no argument crossing the
  boundary contains a `/`.
- **The release-channel table is keyed on the image, never on the tag alone.**
  `internal/imageinfo`'s `imageChannelMap` records, per registry path, which
  tags are stable streams, which are testing streams, and how each maps to
  the other. The entries were verified against GHCR by manifest request; the
  comment above the map carries the observed 200/404 results. Collapsing it
  back into a tag-keyed map reintroduces two references that do not exist
  (`ghcr.io/ublue-os/bluefin:testing` and
  `ghcr.io/projectbluefin/bluefin-lts:lts-testing`), which is a failed `bootc
  switch` on a user's OS rather than a cosmetic bug. Other images are added
  through a `channels.yml` override, which is read only from
  `/etc/chairlift/channels.yml` and `/usr/share/chairlift/channels.yml` —
  never the working directory, because the privileged helper resolves its
  switch target through the same table. The graphics-driver variant table
  lives in the same file's `drivers:` section for the same reason: a driver
  switch is resolved by the same helper, so it must not be configurable from
  a user-writable path, and keeping both tables in one file removes any way
  for the GUI and the helper to load different ones.
- **Update All composes; it does not add a privileged route.** `internal/updateall`
  is the pure sequencer for the OS/Flatpak/Homebrew update run: it executes
  nothing itself, taking every provider as a function seam whose production
  value is the existing `internal/bootc`, `internal/flatpak`, and
  `internal/homebrew` entry point. Its OS phase must keep going through
  `internal/bootc`'s staging path. Adding a `bootc upgrade` route to
  `chairlift-ublue-helper` would break both the staging-ownership invariant
  below and the system-integration package's fixed-path contract. The run's
  only new privileged surface is `restart`. `Summarize`'s `RestartRequired`
  is true only when an image was genuinely staged — the stage script is
  idempotent and exits 0 on an already-current system, so a successful OS
  phase is not by itself evidence anything changed.
- **New privileged operations extend the ublue helper; they do not add a
  binary.** `chairlift-ublue-helper` now carries eight subcommands
  (`channel-switch`, `dx-enable`, `dx-disable`, `restart`, `rollback`,
  `auto-updates-enable`, `auto-updates-disable`, `driver-switch`), each
  selected by exactly one PolicyKit action. Every one takes a fixed argv or a
  word validated against a closed set: no image reference, no username, no
  systemd unit, no delay, and no rollback target crosses the boundary, because each would be a value an authenticated caller controls.
  `internal/ubluehelper`'s tests assert this per command, and the e2e boundary
  test asserts the installed binary rejects each shape.
- **Every navigable page has a committed screenshot and a walkthrough entry.**
  `make screenshots` regenerates `docs/screenshots/` from the real
  application; `docs/walkthrough.md` is the user-facing tour built from them.
  `internal/installcheck`'s walkthrough tests run in `make ci` and fail when a
  page has no screenshot, when the document does not reference one, when a
  screenshot is orphaned, when a capture byproduct is committed, when a
  supported image in `imageinfo.KnownImages()` is not named, or when any group
  in `config.SchemaGroups` has no walkthrough entry. That last check is the
  forcing function: the group-to-phrase table is hand-written but its
  completeness is derived from the config schema, so a feature added to an
  *existing* page — which is how Update All, Automatic Updates, and Roll Back
  all landed — cannot slip through undocumented. The check is deliberately referential rather
  than a pixel comparison: font hinting and GTK point releases move pixels, so
  regenerating and diffing per push would churn the repository for no signal.
  Adding a page or a user-facing feature means running `make screenshots` and
  extending `docs/walkthrough.md` in the same change.
- **The `chairlift_e2e` stub surface is capped and centralized.** Three
  behaviors are stubbed so the screenshot walkthrough can render features a CI
  runner cannot have: the image descriptor (`CHAIRLIFT_IMAGE_INFO`), the
  unattended-update timer state (`CHAIRLIFT_AUTO_UPDATES`), and the graphics
  hardware (`CHAIRLIFT_GPU_VENDORS`). Every stub must be
  read in `internal/app/imageinfo_override_e2e.go` and nowhere else, behind
  the `chairlift_e2e` tag that only `make e2e` sets, with a no-op counterpart
  in `imageinfo_override.go`.
  `internal/installcheck.TestDescriptorOverrideStaysBehindTheE2EBuildTag`
  enforces both halves, asserts this rule names every stubbed variable, and
  must gain each new variable's name. Adding a stub
  means adding it to that one file and that one test — never a second tagged
  file, and never an untagged read. A stub may only affect a read-only,
  display-side classification: anything a privileged helper consults must
  keep resolving its own source of truth, because the helper is built without
  the tag.
- **OS staging execution has one owner.** `internal/stageexec` is the pure-Go
  leaf package that owns the progress event contract, merged stdout/stderr
  streaming, direct-child cancellation, error classification, completion event,
  and channel closure for both `internal/bootc` and `internal/sysupdate`.
  Provider packages retain their fixed paths, host detection, dry-run logging,
  and public error adapters; do not copy the process loop back into either one.
- **System-integration split.** The
  `projectbluefin-chairlift-system-integration` nFPM package contains the fixed-path
  updex helper, all three PolicyKit policies, and package-maintainer config,
  but not the GUI or an OS staging implementation. Distributions pairing it
  with a user-scoped ChairLift install must provide their trusted stage helper
  at `/usr/libexec/bootc-update-stage` before enabling `bootc_updates_group`;
  native A/B hosts ship `/usr/libexec/snosi-sysupdate-stage` (and the
  `/usr/lib/snosi/native-ab` marker) with the OS image, which
  `sysupdate_updates_group` requires. Do not make the privileged path
  configurable from ChairLift's user-writable configuration.
- **GTK main-thread safety.** All external tool calls run in goroutines; every
  UI update marshals back to the GTK main thread via
  `snowkit`'s `sgtk.RunOnMainThread(...)`. Never touch a widget directly from a
  worker goroutine.
- **Headless view coverage stays puregotk-free.** `internal/views` cannot host
  a test binary on ordinary CI hosts. Shared row text, page status, os-release
  parsing, help-link ordering, and maintenance-command selection live in the
  pure `internal/views/pageview` package; its wiring test must continue to
  cover all six page builders.
- **Navigation behavior has one authority.** Page order, titles, icons, and
  advertised/registered accelerators live in the pure
  `internal/navigation` package. It also decides page visibility from static
  group configuration: omit a functional page when all of its builder-backed
  groups are disabled, always retain Help, and compact Alt+number over visible
  pages. Mouse activation and window navigation actions must both call
  `Window.navigateToPage`, which applies the complete `navigation.Resolve`
  transition (visible-row index, visible child, title, and collapsed-layout
  content reveal). The app and shortcuts dialog must use the window's same
  visible inventory. Do not reintroduce a second page or shortcut inventory in
  `internal/window` or `internal/app`.
- **Homebrew update actions preserve known state.** Per-package upgrades and
  the top-level metadata update use `internal/views/actionstate` gates before
  spawning work. Failures and dry-run previews restore their controls without
  changing rows or counts. A live package success removes its row, decrements
  the count/badge, and refreshes; a failed refresh preserves that last known
  row/count state instead of replacing it with an invented zero.
- **Homebrew application actions are typed and refresh-safe.** Search queries
  both formula and cask namespaces and carries the result kind into
  `brew install [--cask]`. Search and installed-package refreshes use separate
  `actionstate.RefreshGate` generations; stale workers must not replace newer
  rows. Confirmed installs use an `actionstate.Gate`, restore controls on
  failure/dry-run, and refresh installed rows only after a live success.
  Installed formula/cask rows likewise confirm uninstall, formula rows confirm
  pin/unpin, and every row shares one gate across its mutation controls so
  actions cannot overlap. A live success completes the old controls and starts
  a generation-guarded inventory refresh; failure or dry-run restores them.
- **Update badge counts have one state owner.** Bootc, sysupdate, Flatpak,
  and Homebrew counts live in the pure `internal/views/badgestate` package.
  Refreshes replace a provider's count, successful row removals decrement
  without going negative, and the displayed total is always the sum of all
  four providers. Do not restore independent integer fields in `UserHome`.
- **Config-driven visibility is real.** Any group can be disabled in config
  (`config.IsGroupEnabled(page, group)`), so its widgets may never be
  constructed. Code that runs after an async action must not assume a widget
  from another group exists — nil-guard cross-group widget access. In
  particular, `brew_bundles_group` is independent of `brew_group`; bundle
  discovery and installs must not assume the formulae/casks expanders exist.
- **Configuration precedence fails closed.** Only a missing candidate advances
  to the next configuration search path. The first file that exists is
  authoritative: read, YAML, or schema errors must disable every configurable
  group, emit the `CONFIGURATION ERROR` diagnostic, and remain visible in the
  UI as a persistent toast until the file is fixed and ChairLift is restarted.
- **CI actions are immutable.** Every external `uses:` reference under
  `.github/workflows/` must use a full 40-character commit SHA. Keep the
  human-readable version or source ref in a trailing comment and update both
  intentionally. Local actions referenced with `./` are exempt. The
  `internal/installcheck` workflow scan enforces this across every workflow.
- **Every privileged dispatch point journals, unconditionally.** `internal/ublue.runHelper`
  and `internal/updex.runHelper` call `journal.Record` on every invocation, dry-run
  or live, before doing anything else. This is not a `chairlift_e2e` stub: with
  `$CHAIRLIFT_ACTION_JOURNAL` unset — every ordinary run — it costs one atomic
  load and does nothing else, so it ships in every released binary. Do not gate
  a new privileged call behind a helper that bypasses `runHelper`; the journal's
  value is that it is genuinely one choke point for every privileged action,
  not most of them.
- **Desktop notifications stay rare.** `internal/notify` sends exactly one:
  Update All's completion, because it is the one action long enough a user may
  have stepped away. A toggle or switch completes in view and already has a
  toast; do not add a second notification for the same instant event.
- **Enhanced Troubleshooting reads state, it does not infer it.**
  `internal/troubleshoot` ports Bluefin's `ujust probe` into one row, from
  `ublue-os/tap`: `linux-mcp-server` (which pulls `block-goose-cli`) plus the
  `goose-linux` cask. `goose-mcp-setup` exits 0 without writing anything when
  a Goose configuration already exists, so readiness must come from finding
  the `linux-tools` extension in `~/.config/goose/config.yaml` — never from
  the script's exit code, and never from the packages being installed. It is
  an action row, not a switch: turning it off would mean either leaving Goose
  calling a removed binary or rewriting a file another tool owns. ChairLift
  reads `GOOSE_PROVIDER` and never writes it; the row must keep naming the
  provider, because the default the setup script writes sends system details
  to Google and "AI assistant" alone implies otherwise. Everything is a
  user-scope Homebrew install and the MCP tools are read-only, so nothing
  here touches pkexec. `brew tap` is in `stateChangingCommands`, without
  which it would run for real under `--dry-run`, including during
  `make screenshots`.
- **The staged-update changelog never fetches on its own.** `internal/sbom`
  is pure — parse, diff, version ordering — with the registry round-trip
  behind the `FetchFunc` seam, so no gated test makes an outbound request
  (`fetch_test.go` drives a loopback `httptest` registry instead). Discovery
  must keep both paths: GHCR answers `/v2/<repo>/referrers/<digest>` with
  404 for these images, and the SBOM is only reachable through the
  specification's fallback tag (`sha256-<hex>`) — verified 2026-08-17. What
  that referrer serves is Syft JSON despite the `application/vnd.spdx+json`
  artifact type, so `Parse` accepts both shapes and errors rather than
  returning an empty map when it recognizes neither; a silent zero-package
  parse renders a blank changelog with nothing in the chain reporting a
  failure, which is a bug finupdate shipped. The diff runs only when the user
  presses Compare, because each side is tens of megabytes.
- **The local-AI stack is one switch, and it is unprivileged.** ChairLift
  ships one runtime (RamaLama) whose per-accelerator image is chosen by
  `internal/gpu`, not bluefinctl's twelve-quadlet vendor catalog — that
  catalog has no answer for an Intel or a GPU-less host. `internal/aistack`
  writes one quadlet to the user's `~/.config/containers/systemd` and drives
  it with `systemctl --user`, so the container is rootless in the invoking
  account and nothing is layered onto a bootc image. That is why its image
  and model overrides (`ai_images`, `ai_model`) live in the ordinary
  `config.yml` rather than the root-only `channels.yml`: pointing a rootless
  container at another image grants nothing running podman directly would
  not. Do not give it a pkexec route, and do not reintroduce a vendor/stack
  matrix in the UI.
- **Powerwash and Factory Reset are opt-in and always confirmed.**
  `reset_group` (maintenance_page) ships `enabled: false` in config.yml, the
  same default as `maintenance_cleanup_group`, because both actions are
  irreversible. Neither may run without the `AdwAlertDialog` confirmation in
  `internal/views/reset.go` first — that dialog's title and body come from
  `pageview.PowerwashConfirmation`/`FactoryResetConfirmation`, which is where
  the `--experimental` disclosure for Factory Reset's `bootc install reset`
  argv lives; do not move that text inline where it stops being tested.
  Powerwash needs no privilege (both steps run in the invoking account, like
  gaming mode); Factory Reset is the new `factory-reset` action on
  `chairlift-ublue-helper` and takes no argument, since it has exactly one
  target — the image already booted.

## Documentation

All documentation lives in the `docs/` tree, in frostyard/core's
four-category shape (core ADR-0025; the former `yeti/` AI-docs directory is
folded in). `docs/README.md` carries the category table, the index of every
doc, and the conventions — new docs start from their category's
`TEMPLATE.md`, and adding a doc means indexing it there:

- `docs/adr/` — why: repo-local decisions, immutable once accepted. Org-wide
  decisions go to frostyard/core instead, per
  [docs/org-adrs.md](docs/org-adrs.md).
- `docs/design/` — how it fits together: living architecture docs. The entry
  point is `docs/design/overview.md` (formerly `yeti/OVERVIEW.md`); read it
  and `docs/design/package-managers.md` (formerly `yeti/package-managers.md`)
  for architecture, patterns, and decision rationale before working. Write
  them to be maximally useful to an AI agent understanding the codebase —
  detailed architecture and rationale rather than user-facing guides.
- `docs/specs/` — exact contracts, changed only alongside implementing code.
- `docs/plans/` — phased plans with "Done when" outcomes.

After any change to source code, update relevant documentation in `AGENTS.md`,
`README.md`, and `docs/`. A task is not complete without reviewing
and updating relevant documentation. For behavior, configuration, dependency,
or install-layout changes, also follow
`docs/documentation-consistency.md`; current-state claims must be checked
against source/config/go.mod rather than copied from historical plans.

**.knowledge/ directory** is the repository's cross-session knowledge index.
Read `.knowledge/README.md` before working so prior corrections, handoffs,
durable lessons, and architecture guidance are discovered from their canonical
locations instead of duplicated into competing stores.

**.memory/ directory** is the repository's committed correction store for AI
agents. Read `.memory/README.md` and any learning artifacts in that directory
before working. Record verified corrections there when a session establishes
that a prior belief about ChairLift was wrong, and promote stable rules into
this file, `docs/agents/skills/`, or `docs/design/` as appropriate. Never record
secrets or personal data because the directory is version-controlled.

## Learned agent skills

**docs/agents/skills/** Read every file in `docs/agents/skills/` before
planning, implementing, or reviewing changes. Each file is a durable lesson
distilled from a previous automated run of
[the mill](https://github.com/frostyard/mill) (the spec→PR harness, configured
here via `.mill.toml`); they are binding guidance, not suggestions. New skills
are added by the mill's harvest step and reviewed like any other change in the
PR that carries them.

## Org-wide decisions

Org-level conventions this repo follows are recorded as ADRs in
frostyard/core — see [docs/org-adrs.md](docs/org-adrs.md) for the list that
binds this repo. Change the ADR (in core) before changing behavior it covers.
