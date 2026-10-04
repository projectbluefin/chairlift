# Org-wide decisions (frostyard/core ADRs)

Conventions this repository follows that are decided at the org level are
recorded as ADRs in
[frostyard/core](https://github.com/frostyard/core/tree/main/docs/adr).
The ones that bind ChairLift:

- [ADR-0004 — Product-namespaced filesystem paths, split by lifetime tier](https://github.com/frostyard/core/blob/main/docs/adr/0004-product-namespaced-filesystem-tiers.md) — /etc/chairlift vs /usr/share/chairlift config precedence follows this
- [ADR-0010 — Publish packages through the shared repogen action](https://github.com/frostyard/core/blob/main/docs/adr/0010-publish-packages-via-repogen-to-r2.md) — no longer exercised: ChairLift publishes no distro packages (Homebrew cask only); to be retired in frostyard/core
- [ADR-0011 — Distro packages are named frostyard-<tool>](https://github.com/frostyard/core/blob/main/docs/adr/0011-frostyard-prefixed-package-names.md) — no longer exercised: ChairLift publishes no distro packages (Homebrew cask only); to be retired in frostyard/core
- [ADR-0012 — svu-derived versions, make bump, and the rolling dev prerelease](https://github.com/frostyard/core/blob/main/docs/adr/0012-svu-versioning-and-rolling-dev-prerelease.md) — `make bump` is kept, but ChairLift departs from svu: stable calendar tags (`vYY.MM.N`, N starting at 1) come from `scripts/next-version.sh`; historical alpha tags do not advance the sequence, and there is no prerelease option, rolling dev prerelease, or snapshot workflow
- [ADR-0034 — Cancel stale rolling dev releases](https://github.com/frostyard/core/blob/main/docs/adr/0034-cancel-stale-rolling-dev-releases.md) — no longer exercised: ChairLift ships no rolling dev releases or snapshot workflow
- [ADR-0013 — Component releases trigger image rebuilds via repository_dispatch](https://github.com/frostyard/core/blob/main/docs/adr/0013-release-fanout-via-repository-dispatch.md) — no longer exercised: releases publish to GitHub Releases for Homebrew consumption without image fanout
- [ADR-0016 — Reverse-DNS org.frostyard.* identifiers](https://github.com/frostyard/core/blob/main/docs/adr/0016-reverse-dns-org-frostyard-identifiers.md) — io.projectbluefin.chairlift app ID, desktop file, polkit actions
- [ADR-0018 — Org-wide agent instruction and knowledge surfaces](https://github.com/frostyard/core/blob/main/docs/adr/0018-org-wide-agent-instruction-and-knowledge-surfaces.md) — AGENTS.md symlinks, docs/agents/skills, .memory/ (its yeti/ AI-docs directory is superseded on that point by ADR-0025)
- [ADR-0019 — Repository governance as machine-readable policy with risk tiers](https://github.com/frostyard/core/blob/main/docs/adr/0019-governance-as-code-and-risk-tiers.md) — .github/policies/, auto-qa-tuning, risk tiers
- [ADR-0020 — Trust boundaries for AI automation in CI](https://github.com/frostyard/core/blob/main/docs/adr/0020-ai-automation-trust-boundaries.md) — claude-code-review analyze/publish split, HTML idempotency markers
- [ADR-0021 — SHA-pinned actions and least-privilege CI workflows](https://github.com/frostyard/core/blob/main/docs/adr/0021-sha-pinned-actions-and-least-privilege-ci.md) — enforced by internal/installcheck/workflows_test.go
- [ADR-0022 — make ci is the canonical gate; TestI* is reserved](https://github.com/frostyard/core/blob/main/docs/adr/0022-make-ci-gate-and-test-naming-filter.md) — make ci, the Test[^I] filter, internal/installcheck pattern
- [ADR-0025 — One docs/ tree per repository, in core's four-category shape](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md) — docs/{adr,design,specs,plans} + indexed docs/README.md; yeti/ folded into docs/design/

When changing behavior covered by one of these, update or supersede the ADR
in frostyard/core first, then change this repo in the same effort.
