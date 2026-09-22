# 0013 — Purge historical plan and port artifacts in favor of living design documentation

- **Status:** Accepted
- **Date:** 2026-09-22
- **Supersedes:** [ADR-0010](0010-docs-are-a-ci-gated-artifact.md)

## Context

ADR-0010 established documentation consistency tests and partitioned repository
documentation into "current-state" and "historical" tiers (`README-go-port.md`,
`docs/plans/`, `docs/superpowers/`). While this prevented historical artifacts
from breaking source-path assertion gates, retaining superseded proposals and
historical execution plans accumulated stale references (e.g. dead tools like
`nbc`, stale repository targets, and deprecated workflow designs) that confused
contributing agents and human developers alike.

Living design documentation (`docs/design/`, `docs/specs/`, `docs/adr/`) and
canonical skills (`docs/skills/`) provide complete architecture, rationale, and
decision histories.

## Decision

1. Purge obsolete historical artifacts (`README-go-port.md`, `docs/superpowers/`,
   and historical plans in `docs/plans/` that are fully implemented). Retain category
   `TEMPLATE.md` files and active living design docs.
2. Update `docs/documentation-consistency.md` and `docs/README.md` to remove the
   retained historical tier requirements while retaining CI gating for
   all living, current-state documentation.
3. Remove historical document assertions from `internal/installcheck/documentation_test.go`
   and `internal/installcheck/docsourcepaths_test.go`.

## Consequences

- Stale implementation proposals and run artifacts no longer pollute repository
  searches.
- Contributing agents rely exclusively on living design documentation (`docs/design/`)
  and canonical ADRs (`docs/adr/`).
- CI gating remains strictly in effect: `make ci` and `internal/installcheck`
  continue to verify that all current-state documentation matches source code
  and configuration facts.

## Alternatives Considered

- **Retaining historical documents with warning banners:** Rejected — banners
  did not prevent tools, search queries, or agent prompts from ingesting
  superseded designs, outdated tool references (`nbc`), and stale configuration
  keys.
- **Archiving to an external repository or branch:** Rejected — git history
  already preserves historical commits and past plans cleanly without cluttering
  the main branch working tree.

## References

- Shapes: [documentation-consistency.md](../documentation-consistency.md),
  [README.md (docs index)](../README.md), [design/overview.md](../design/overview.md)
- Supersedes: [ADR-0010 — Treat documentation as a CI-gated artifact split into current-state and historical](0010-docs-are-a-ci-gated-artifact.md)
- Enforced by: `internal/installcheck/documentation_test.go`,
  `internal/installcheck/docsourcepaths_test.go`
