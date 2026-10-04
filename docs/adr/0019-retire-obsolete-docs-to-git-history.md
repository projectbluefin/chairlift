# 0019 — Preserve retired documentation in Git history

- **Status:** Accepted
- **Date:** 2026-10-04
- **Supersedes:** [ADR-0010](0010-docs-are-a-ci-gated-artifact.md), only its in-tree historical-artifact retention and associated banner/classification assertions

## Context

ADR-0010 retained historical proposals beside current guidance and enforced
that classification with prose assertions. The Go-port guide, completed
extension/binding migrations, modernization scripts, and suite-parity plan
now describe retired implementations. Their useful contracts already have
current homes in design docs, specs, and skills; maintaining their execution
instructions creates competing authority.

The documentation cleanup explicitly approved Git-history-only archival
instead of retaining these artifacts in the current tree. Current-state
source, configuration, installation, and documentation consistency checks
remain required.

## Decision

1. Keep `docs/plans/` for active, issue-owned work with executable completion
   criteria. Remove completed, superseded, or abandoned plans after reconciling
   durable contracts and lessons into existing living docs and skills.
2. Retire the Go-port guide and old superpowers execution artifacts. Git history
   preserves their contents; do not maintain an archive directory or duplicate
   backlog. Unscheduled work belongs in issues.
3. Preserve accepted ADR bodies. Update supersession status and repair references
   to retired artifacts with merged-revision-pinned history links when that
   historical evidence matters.
4. Remove tests that demand the retired artifacts or their historical banners.
   Keep source-backed configuration, packaging, command, skill/alias, screenshot,
   and cited-source-path checks. Documentation still passes `make ci`.

## Consequences

Current navigation exposes usable contracts rather than old implementation
recipes. Historical retrieval requires a Git revision or a pinned link instead
of browsing an in-tree archive. Plan removal must inspect inbound references,
active scope, and preservation of durable lessons before deletion.

## Alternatives considered

- **Keep marked historical archives:** rejected for this cleanup. Banners do
  not remove obsolete commands from agent discovery and repository searches.
- **Rewrite old plans as current guides:** rejected. That duplicates existing
  design/spec ownership and destroys the historical execution record.
- **Drop documentation checks:** rejected. Artifact retirement does not weaken
  the requirement that retained guidance matches live source.

## References

- Shapes: [documentation consistency](../documentation-consistency.md),
  [documentation index](../README.md), [active plan template](../plans/TEMPLATE.md)
- Procedure: [documentation reconciliation](../skills/documentation-reconciliation/SKILL.md)
- Builds on: [ADR-0010](0010-docs-are-a-ci-gated-artifact.md)
- Retained checks: `internal/installcheck/documentation_test.go`,
  `internal/installcheck/docsourcepaths_test.go`,
  `internal/installcheck/factorydocs_test.go`
