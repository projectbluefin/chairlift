# Documentation

ChairLift keeps one documentation tree, `docs/`, using the local four-category
layout from [core ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md).
Task routing, canonical links, and reusable skills follow
[Project Bluefin Common](https://github.com/projectbluefin/common/blob/main/docs/SKILL.md).
Docs are split by the question they answer:

| Directory | Question | Contents |
| --- | --- | --- |
| [adr/](adr/) | **Why** did we choose this? | Repo-local Architecture Decision Records — immutable once accepted; superseded, never edited. Org-wide decisions go to frostyard/core instead (see [org-adrs.md](org-adrs.md)) |
| [design/](design/) | **How** does it fit together? | Living documents describing the current architecture; [design/overview.md](design/overview.md) is the entry point |
| [specs/](specs/) | **What exactly** is the contract? | Precise, testable descriptions of implemented interfaces |
| [plans/](plans/) | **When/in what order** do we build? | Active issue-owned plans with executable "Done when" outcomes; completed work belongs in living docs and Git history |
| [factory/](factory/) | **How** do we work across the Project Bluefin factory? | Local navigation for factory-assigned work; Common remains the authority for cross-repository process |

The `factory/` entry is a process and navigation surface; the four core
documentation categories remain `adr/`, `design/`, `specs/`, and `plans/`.

The user-facing overview is [index.md](index.md); this file is the
contributor-facing index of everything under `docs/`.

## Index

### Decisions (ADRs)

Repo-local decisions get the next free number from
[adr/TEMPLATE.md](adr/TEMPLATE.md); org-wide decisions that bind this repo
are listed in [org-adrs.md](org-adrs.md).

- [adr/0001-fixed-path-pkexec-privilege-boundary.md](adr/0001-fixed-path-pkexec-privilege-boundary.md)
  — every root mutation goes through pkexec at hardcoded absolute paths
  matching the polkit `exec.path`/`exec.argv1` annotations; the helper
  re-validates full argv; no passwordless rules ever ship
- [adr/0002-usr-prefix-is-the-only-supported-install-prefix.md](adr/0002-usr-prefix-is-the-only-supported-install-prefix.md)
  — `PREFIX=/usr` is the only supported install prefix (polkitd's fixed
  actions directory, pkexec's absolute path match); `DESTDIR` layers under it
- [adr/0003-two-tier-config-with-fail-closed-semantics.md](adr/0003-two-tier-config-with-fail-closed-semantics.md)
  — `/etc/chairlift` → `/usr/share/chairlift` → `config.dev.yml` →
  `config.yml`; only absence advances the search; a present-but-broken file
  disables every feature group
- [adr/0004-configuration-error-diagnostic-vocabulary.md](adr/0004-configuration-error-diagnostic-vocabulary.md)
  — fixed greppable `CONFIGURATION ERROR` log prefix, persistent toast, and
  the stable `ErrorKind` classification vocabulary
- [adr/0005-config-schema-reflected-from-canonical-struct.md](adr/0005-config-schema-reflected-from-canonical-struct.md)
  — the config schema is reflected from `Config`/`defaultConfig()` yaml tags;
  unknown keys hard-error; files are field-by-field overlays (explicit empty
  clears, omitted inherits)
- [adr/0006-split-system-integration-package-with-mutual-conflicts.md](adr/0006-split-system-integration-package-with-mutual-conflicts.md)
  — publish a GUI-less system-integration package that conflicts with the full package (superseded by ADR-0018)
- [adr/0007-pure-leaf-packages-route-around-untestable-gtk.md](adr/0007-pure-leaf-packages-route-around-untestable-gtk.md)
  — puregotk-importing packages stay test-free; all decidable logic lives in
  headless leaf packages with wiring tests proving the page builders use them
- [adr/0008-e2e-readiness-is-a-log-marker-contract.md](adr/0008-e2e-readiness-is-a-log-marker-contract.md)
  — E2E startup readiness is three exact stdout markers polled under
  dbus-run-session + a headless Mutter Wayland session; the log lines are a public API
- [adr/0009-dry-run-output-convention-and-single-decision-structs.md](adr/0009-dry-run-output-convention-and-single-decision-structs.md)
  — `internal/dryrun` as the single process-wide dry-run authority, fixed
  `[DRY-RUN]` message prefixes, and single tested decision structs gating
  toast + UI mutation together
- [adr/0010-docs-are-a-ci-gated-artifact.md](adr/0010-docs-are-a-ci-gated-artifact.md)
  — original current-state/historical classification and CI consistency;
  in-tree archival retention is superseded by ADR-0019
- [adr/0011-chairlift-owns-bluefin-family-rebasing.md](adr/0011-chairlift-owns-bluefin-family-rebasing.md)
  — ChairLift owns Bluefin-family image rebasing and channel switching, deriving
  switch targets from registry-verified channel and driver variant tables
- [adr/0012-ship-as-control-center-keep-chairlift-code-name.md](adr/0012-ship-as-control-center-keep-chairlift-code-name.md)
  — the application ships as "Control Center" and keeps ChairLift as the code
  name; `branding.AppName` owns every user-visible spelling, and three
  `internal/installcheck` gates hold the split
- [adr/0013-rollback-catalog-reads-the-registry-live.md](adr/0013-rollback-catalog-reads-the-registry-live.md)
  — the dated-build catalog is read from the registry at runtime by the
  read-only `internal/registrytags` leaf package, behind the `Client.HTTP`
  seam so no gated test reaches the network; pinning to a dated tag stays a
  separate decision because no image reference crosses the pkexec boundary
- [adr/0014-capability-driven-visibility-as-a-floor.md](adr/0014-capability-driven-visibility-as-a-floor.md)
  — page and group visibility is bounded by host capability (`Configured && Available`);
  page-level availability is synchronous and non-blocking, while group-level probes
  can run asynchronously without creating inert placeholders
- [adr/0015-agent-mode-llmman.md](adr/0015-agent-mode-llmman.md)
  — Agent Mode runs llmman (installed via a Homebrew Brewfile) as the
  ChairLift-owned user unit `chairlift-llmman.service` on
  loopback with the web shell and prompt history off; defines the six Agent
  Mode states, local model/preset readiness, and artifact ownership
- [adr/0016-printer-app-admin-denied-until-authenticated.md](adr/0016-printer-app-admin-denied-until-authenticated.md)
  — printer application administration is denied until authenticated: PAPPL
  serves web admin and IPP on one listener, so the boundary is authorization
  (authenticated or absent web admin), not loopback binding
- [adr/0017-pin-through-a-validated-day-word.md](adr/0017-pin-through-a-validated-day-word.md)
  — a pin is a `bootc switch` to a dated build named by the helper from a
  validated `YYYYMMDD` word plus the booted stream and `internal/imageinfo`'s
  table; two new ublue actions (`pin`, `unpin`), and the registry confirms the
  derived reference exists before anything is staged
- [adr/0018-distribute-via-homebrew-release-archives.md](adr/0018-distribute-via-homebrew-release-archives.md)
  — distribute exclusively through Homebrew release archives; retire distro nFPM
  packaging (`deb`, `rpm`, `apk`) and carry the full install surface (GUI,
  both helpers, and policies) in the published release archive
- [adr/0019-retire-obsolete-docs-to-git-history.md](adr/0019-retire-obsolete-docs-to-git-history.md)
  — retain active issue-owned plans only; preserve retired artifacts in Git,
  accepted decision bodies, and source-backed documentation checks

### Design

- [design/overview.md](design/overview.md) — architecture entry point:
  ownership, dependency flow, UI safety, configuration, build and release
- [design/package-managers.md](design/package-managers.md) — package providers,
  privileged executor boundaries, registry catalog, and view-layer seams
- [design/printer-applications.md](design/printer-applications.md) — the
  rootless printer-application quadlets: host-networking surface,
  authenticated-admin boundary, family inventory, and verified image state

- [design/destination-matrix.md](design/destination-matrix.md) — current
  seven-primary-page and Powerwash detail action ownership, with page-qualified
  config references; no proposed sidebar cutover

### Specs

- [specs/setup-model.md](specs/setup-model.md) — explicit setup using the real
  visible pages, navigation-only transitions, and recorded dispositions

- [specs/developer-feeds.md](specs/developer-feeds.md) — the developer feed
  OPML catalog: structure and attribute contract, the offline validation rules
  CI enforces, the curation requirement, and the manual liveness
  re-verification recipe

### Plans

No active implementation plans are retained. Use [plans/TEMPLATE.md](plans/TEMPLATE.md)
for issue-owned work. Completed and superseded plans, the Go-port proposal, and
the old superpowers execution artifacts are preserved in Git history; current
behavior belongs in the design docs and specs above.

### Factory and agent process

- [SKILL.md](SKILL.md) — local skill router
- [skills/index.md](skills/index.md) — canonical catalog of agent skill packages
- [factory/README.md](factory/README.md) — local Project Bluefin factory
  navigation and Common sidecar links
- [skills/](skills/) — canonical agent knowledge base; edit the matching
  `SKILL.md` package, never a compatibility surface
- [agents/skills/](agents/skills/) — legacy compatibility aliases only; do not
  edit these files

### Process and policy docs (uncategorized, indexed in place)

- [index.md](index.md) — user-facing overview: pages, shortcuts, optional
  dependencies, building and installing
- [reference.md](reference.md) — user-facing configuration/behavior reference
- [walkthrough.md](walkthrough.md) — every screen, using committed real-app captures
- [quality.md](quality.md) — quality dashboard: CI, coverage, release signals
- [metrics.md](metrics.md) and [metrics/README.md](metrics/README.md) —
  metrics definitions and the public metrics catalog
- [documentation-consistency.md](documentation-consistency.md) — checklist
  for keeping current-state docs aligned with source, preserving decision
  history, and retiring obsolete implementation plans
- [risk-tiers.md](risk-tiers.md) — change risk classification used by PRs
- [review-rubric.md](review-rubric.md) — pull request review rubric
- [SECURITY-AI.md](SECURITY-AI.md) — AI security policy
- [org-adrs.md](org-adrs.md) — frostyard/core ADRs that bind this repository
- [prompts/index.md](prompts/index.md) — reusable agent prompt catalog
- Category templates: [ADRs](adr/TEMPLATE.md), [design](design/TEMPLATE.md),
  [specs](specs/TEMPLATE.md), and [active plans](plans/TEMPLATE.md)

## Conventions

- **New docs start from their category's `TEMPLATE.md`** (in each directory).
- New repo-local decision → new ADR with the next number; if it reverses an
  old one, mark the old one `Superseded by NNNN` rather than editing it.
  Org-wide decisions become ADRs in frostyard/core plus a line in
  [org-adrs.md](org-adrs.md).
- Design docs are updated in place to always reflect reality.
- Specs describe the implemented contract; behavior changes land with code.
- Cross-link governing ADRs, implemented specs, and living designs. Link plans
  only while active; preserve references to retired work with revision-pinned
  Git history links rather than retaining duplicate instructions.
- Adding a doc means adding it to the index above.
- Doc-consistency tests in `internal/installcheck` pin literal paths and
  claims in these docs; when docs and tests disagree, update both in the
  same commit.
- Load only task-matching [skills](skills/index.md). Link Common's canonical
  factory policy and Prow workflow page instead of copying them.
- After reconciliation, validate local links and source citations and run the
  relevant repository gates. Keep root `README.md` human-owned.
