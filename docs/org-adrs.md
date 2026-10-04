# Org-wide decisions (frostyard/core ADRs)

This index links inherited org-level conventions in
[frostyard/core](https://github.com/frostyard/core/tree/main/docs/adr) and records
how the live ChairLift implementation uses or departs from them. An applicability
note here does not change an upstream ADR's status. For factory operating rules,
use the [Common sidecar map](factory/README.md).

- [ADR-0004 — Product-namespaced filesystem paths, split by lifetime tier](https://github.com/frostyard/core/blob/main/docs/adr/0004-product-namespaced-filesystem-tiers.md) — /etc/chairlift vs /usr/share/chairlift config precedence follows this
- [ADR-0010 — Publish packages through the shared repogen action](https://github.com/frostyard/core/blob/main/docs/adr/0010-publish-packages-via-repogen-to-r2.md) — not exercised: ChairLift publishes release archives for Homebrew, not distro packages
- [ADR-0011 — Distro packages are named frostyard-<tool>](https://github.com/frostyard/core/blob/main/docs/adr/0011-frostyard-prefixed-package-names.md) — not exercised: there is no distro package naming surface in the release configuration
- [ADR-0012 — svu-derived versions, make bump, and the rolling dev prerelease](https://github.com/frostyard/core/blob/main/docs/adr/0012-svu-versioning-and-rolling-dev-prerelease.md) — `make bump` is kept, but ChairLift departs from svu: stable calendar tags (`vYY.MM.N`, N starting at 1) come from `scripts/next-version.sh`; historical alpha tags do not advance the sequence, and there is no prerelease option, rolling dev prerelease, or snapshot workflow
- [ADR-0034 — Cancel stale rolling dev releases](https://github.com/frostyard/core/blob/main/docs/adr/0034-cancel-stale-rolling-dev-releases.md) — not exercised: no rolling-dev snapshot workflow remains; nightly compliance runs gates without publishing a release
- [ADR-0013 — Component releases trigger image rebuilds via repository_dispatch](https://github.com/frostyard/core/blob/main/docs/adr/0013-release-fanout-via-repository-dispatch.md) — not exercised: releases publish to GitHub Releases for Homebrew consumption without image fanout
- [ADR-0016 — Reverse-DNS org.frostyard.* identifiers](https://github.com/frostyard/core/blob/main/docs/adr/0016-reverse-dns-org-frostyard-identifiers.md) — ChairLift retains the fixed `io.projectbluefin.chairlift` identity; [local ADR-0012](adr/0012-ship-as-control-center-keep-chairlift-code-name.md) records the product/code-name split
- [ADR-0018 — Org-wide agent instruction and knowledge surfaces](https://github.com/frostyard/core/blob/main/docs/adr/0018-org-wide-agent-instruction-and-knowledge-surfaces.md) — local AGENTS entry point, canonical `docs/skills/*/SKILL.md` packages, `.knowledge/` index, and `.memory/` corrections; `docs/agents/skills` contains compatibility aliases only
- [ADR-0019 — Repository governance as machine-readable policy with risk tiers](https://github.com/frostyard/core/blob/main/docs/adr/0019-governance-as-code-and-risk-tiers.md) — .github/policies/, auto-qa-tuning, risk tiers
- [ADR-0020 — Trust boundaries for AI automation in CI](https://github.com/frostyard/core/blob/main/docs/adr/0020-ai-automation-trust-boundaries.md) — claude-code-review analyze/publish split, HTML idempotency markers
- [ADR-0021 — SHA-pinned actions and least-privilege CI workflows](https://github.com/frostyard/core/blob/main/docs/adr/0021-sha-pinned-actions-and-least-privilege-ci.md) — `internal/installcheck/workflows_test.go` enforces third-party SHA pins and local-action exemptions; the adopted first-party `projectbluefin/actions` runtime uses managed `v1`, with candidate refs limited to read-only, secret-free policy previews
- [ADR-0022 — make ci is the canonical gate; TestI* is reserved](https://github.com/frostyard/core/blob/main/docs/adr/0022-make-ci-gate-and-test-naming-filter.md) — make ci, the Test[^I] filter, internal/installcheck pattern
- [ADR-0025 — One docs/ tree per repository, in core's four-category shape](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md) — docs/{adr,design,specs,plans} + indexed docs/README.md; yeti/ folded into docs/design/

When changing behavior covered by one of these, update or supersede the ADR
in frostyard/core first, then change this repo in the same effort.

## Reading local decision history

Accepted local ADRs preserve decisions; living source owns current inventories.
Check a record's filename, subject, status and any approved amendment or
supersession before citing it. Repair bibliographic links without changing the
decision; a substantive policy change needs an accepted amendment or superseding
record, not an inferred status change in this index.

- The capability decision's filename is
  [0014](adr/0014-capability-driven-visibility-as-a-floor.md), although its
  heading still says `0013`. Actual
  [0013](adr/0013-rollback-catalog-reads-the-registry-live.md) is registry catalog
  reads; [0015](adr/0015-agent-mode-llmman.md) is Agent Mode.
- [ADR-0018](adr/0018-distribute-via-homebrew-release-archives.md) records the
  Homebrew-only archive cutover and supersedes
  [ADR-0006](adr/0006-split-system-integration-package-with-mutual-conflicts.md).
  The old nFPM package inventory belongs to that superseded record, not the
  current install surface in `.goreleaser.yaml` and `Makefile`.
- The approved amendments to
  [ADR-0001](adr/0001-fixed-path-pkexec-privilege-boundary.md),
  [ADR-0002](adr/0002-usr-prefix-is-the-only-supported-install-prefix.md) and
  [ADR-0012](adr/0012-ship-as-control-center-keep-chairlift-code-name.md) reconcile
  helper/packaging references with that cutover. Historical context and a
  superseded inventory do not override current source, PolicyKit files or
  [design/overview.md](design/overview.md).

## Verification

Check release claims in `scripts/next-version.sh`, `.goreleaser.yaml`, and
`.github/workflows/`; inspect every workflow, not a remembered snapshot job.
Resolve local ADR identity and status against the actual `docs/adr/` files.
After reconciling source-backed claims, the integration owner runs `make ci`.
