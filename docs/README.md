# Documentation

ChairLift keeps one documentation tree, `docs/`, split by the question each
directory answers:

| Directory | Question | Contents |
| --- | --- | --- |
| [adr/](adr/) | **Why** did we choose this? | Repo-local Architecture Decision Records — immutable once accepted; superseded, never edited |
| [design/](design/) | **How** does it fit together? | Living documents describing the current architecture; [design/overview.md](design/overview.md) is the entry point |
| [specs/](specs/) | **What exactly** is the contract? | Precise, testable interface definitions, changed only alongside implementing code |
| [plans/](plans/) | **When/in what order** do we build? | Phased plans with "Done when" outcomes |
| [factory/](factory/) | **How** do we work across the Project Bluefin factory? | Local navigation for factory-assigned work; Common remains the authority for cross-repository process |

The `factory/` entry is a process and navigation surface; the four core
documentation categories remain `adr/`, `design/`, `specs/`, and `plans/`.

The user-facing overview is [index.md](index.md); this file is the
contributor-facing index of everything under `docs/`.

## Index

### Decisions (ADRs)

Repo-local decisions get the next free number from
[adr/TEMPLATE.md](adr/TEMPLATE.md).

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
  — self-contained `projectbluefin-chairlift` vs GUI-less
  `projectbluefin-chairlift-system-integration`, conflicting both ways, for
  user-scoped GUI installs such as the Homebrew cask
- [adr/0007-pure-leaf-packages-route-around-untestable-gtk.md](adr/0007-pure-leaf-packages-route-around-untestable-gtk.md)
  — puregotk-importing packages stay test-free; all decidable logic lives in
  headless leaf packages with wiring tests proving the page builders use them
- [adr/0008-e2e-readiness-is-a-log-marker-contract.md](adr/0008-e2e-readiness-is-a-log-marker-contract.md)
  — E2E startup readiness is three exact stdout markers polled under
  dbus-run-session + xvfb-run; the log lines are a public API
- [adr/0009-dry-run-output-convention-and-single-decision-structs.md](adr/0009-dry-run-output-convention-and-single-decision-structs.md)
  — `internal/dryrun` as the single process-wide dry-run authority, fixed
  `[DRY-RUN]` message prefixes, and single tested decision structs gating
  toast + UI mutation together
- [adr/0010-docs-are-a-ci-gated-artifact.md](adr/0010-docs-are-a-ci-gated-artifact.md)
  — documentation consistency enforced by string-matching unit tests (superseded by ADR-0013)
- [adr/0011-chairlift-owns-bluefin-family-rebasing.md](adr/0011-chairlift-owns-bluefin-family-rebasing.md)
  — ChairLift owns Bluefin-family image rebase logic, channel switches, and hardware-driver mapping
- [adr/0012-ship-as-control-center-keep-chairlift-code-name.md](adr/0012-ship-as-control-center-keep-chairlift-code-name.md)
  — the application ships as "Control Center" and keeps ChairLift as the code
  name; `branding.AppName` owns every user-visible spelling, and three
  `internal/installcheck` gates hold the split
- [adr/0013-purge-historical-plan-and-port-artifacts.md](adr/0013-purge-historical-plan-and-port-artifacts.md)
  — purge historical plan/port files in favor of living design documentation

### Design

- [design/overview.md](design/overview.md) — architecture entry point:
  purpose, dependency flow, key patterns, configuration, build and release
  (formerly `yeti/OVERVIEW.md`)
- [design/package-managers.md](design/package-managers.md) — the Homebrew,
  Flatpak, bootc, sysupdate, updex, and ublue wrappers and their view-layer leaf
  packages (formerly `yeti/package-managers.md`)

### Specs

*(none yet)*

### Plans

Active implementation plans with "Done when" outcomes start from
[plans/TEMPLATE.md](plans/TEMPLATE.md).
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
- [quality.md](quality.md) — quality dashboard: CI, coverage, release signals
- [metrics.md](metrics.md) and [metrics/README.md](metrics/README.md) —
  metrics definitions and the public metrics catalog
- [documentation-consistency.md](documentation-consistency.md) — checklist
  for keeping documentation in sync with source code and configuration
- [walkthrough.md](walkthrough.md) — every screen, captured from the real application
- [risk-tiers.md](risk-tiers.md) — change risk classification used by PRs
- [review-rubric.md](review-rubric.md) — pull request review rubric
- [SECURITY-AI.md](SECURITY-AI.md) — AI security policy
- [prompts/index.md](prompts/index.md) — reusable agent prompt catalog

## Conventions

- **New docs start from their category's `TEMPLATE.md`** (in each directory).
- New repo-local decision → new ADR with the next number; if it reverses an
  old one, mark the old one `Superseded by NNNN` rather than editing it.
- Design docs are updated in place to always reflect reality.
- Specs change only alongside the code that implements them.
- Cross-links between categories are mandatory in both directions.
- Adding a doc means adding it to the index above.
- Doc-consistency tests in `internal/installcheck` pin literal paths and
  claims in these docs; when docs and tests disagree, update both in the
  same commit.
