# ChairLift skills catalog

The packages below are the canonical local skills. Read the package that
matches the task before acting.

## Factory and process

- [factory-onboarding](factory-onboarding/SKILL.md) — onboard a verified
  Project Bluefin factory assignment and load the Common sidecar procedures.
- [skill-improvement](skill-improvement/SKILL.md) — preserve durable lessons,
  corrections, and catalog integrity when a change is complete.

Factory and process work starts with these two packages; Common's linked
documents remain authoritative for cross-repository policy.

## Retained lessons

- [agent-contracts](agent-contracts/SKILL.md) — keep agent-contract claims and
  plan acceptance criteria aligned.
- [automated-acceptance](automated-acceptance/SKILL.md) — require executable
  checks instead of inspection-only acceptance.
- [canonical-schema](canonical-schema/SKILL.md) — derive validation from the
  authoritative schema or struct.
- [change-decomposition](change-decomposition/SKILL.md) — split oversized
  work by coherent concern.
- [change-sizing](change-sizing/SKILL.md) — scale diff budgets with decision
  table rows.
- [collection-regressions](collection-regressions/SKILL.md) — cover every
  entry in a consistency-check collection.
- [dependency-behavior](dependency-behavior/SKILL.md) — match dependency
  semantics from its source.
- [diagnostic-assertions](diagnostic-assertions/SKILL.md) — parse and validate
  generated diagnostic line numbers.
- [documentation-reconciliation](documentation-reconciliation/SKILL.md) —
  remove contradictory documentation claims.
- [evolving-test-contracts](evolving-test-contracts/SKILL.md) — track tests
  when later work changes an earlier contract.
- [error-classification](error-classification/SKILL.md) — fix error-kind
  boundaries with an explicit decision table.
- [frozen-allowlists](frozen-allowlists/SKILL.md) — keep allowlist
  authorization scoped to the named entry.
- [gated-test-placement](gated-test-placement/SKILL.md) — place tests where
  the repository's gates actually execute them.
- [grep-acceptance](grep-acceptance/SKILL.md) — account for subtests and
  cross-chunk conflicts in grep-based criteria.
- [gtk-headless-testing](gtk-headless-testing/SKILL.md) — keep tests out of
  packages that load GTK through puregotk.
- [helper-test-surface](helper-test-surface/SKILL.md) — call added helpers
  directly from tests when required.
- [leaf-package-documentation](leaf-package-documentation/SKILL.md) —
  enumerate every outcome owned by a leaf package.
- [leaf-package-inventory](leaf-package-inventory/SKILL.md) — update exact
  package enumerations with the package they describe.
- [merge-validation](merge-validation/SKILL.md) — validate discarded or
  overridden merge branches.
- [package-namespace](package-namespace/SKILL.md) — avoid package-level
  identifier collisions with tests.
- [pipeline-tracing](pipeline-tracing/SKILL.md) — trace a concrete input
  through layered validation order.
- [removal-verification](removal-verification/SKILL.md) — verify a removed
  identifier with a scoped repository search.
- [scope-control](scope-control/SKILL.md) — respect rename-only and
  no-production-code boundaries.
- [yaml-key-identity](yaml-key-identity/SKILL.md) — include YAML scalar tags
  when comparing keys.

The former `docs/agents/skills/*.md` files are compatibility aliases to these
canonical packages. Never edit the aliases; update their canonical targets.
