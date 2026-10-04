# ChairLift skills catalog

This is the exhaustive, hand-maintained catalog of canonical local packages.
Use `docs/SKILL.md` for the quick task router, then read the matching package's
description and body. Historical **Learned from** examples explain a durable
lesson; current symbols and behavior must still be verified against source.

## Factory and process

- [factory-onboarding](factory-onboarding/SKILL.md) — start a verified
  Project Bluefin factory assignment, use the merge queue, or cut a release.
- [issue-lifecycle](issue-lifecycle/SKILL.md) — file and triage reports, accept or
  assign work, use maintainer Prow commands, migrate labels, and verify delivery
  to the Homebrew app and any image-installed helpers.
- [skill-improvement](skill-improvement/SKILL.md) — preserve durable lessons,
  corrections, and catalog integrity when a change is complete.

Load onboarding for factory entry, issue-lifecycle for ChairLift intake and
delivery, and skill-improvement when preserving a lesson. Common's linked
documents remain authoritative for cross-repository policy; the local catalog
and shared Actions caller own ChairLift's adopted lifecycle.

## Planning and scope

- [agent-contracts](agent-contracts/SKILL.md) — keep documented invariants and
  acceptance criteria aligned.
- [automated-acceptance](automated-acceptance/SKILL.md) — require executable
  checks where acceptance asks for automated coverage.
- [change-decomposition](change-decomposition/SKILL.md) — split oversized
  work by coherent concern.
- [change-sizing](change-sizing/SKILL.md) — size changes from their actual
  decision-table coverage, not flat guesses.
- [evolving-test-contracts](evolving-test-contracts/SKILL.md) — update earlier
  tests when a later change supersedes their contract.
- [grep-acceptance](grep-acceptance/SKILL.md) — distinguish top-level test lists
  from subtest execution and reconcile cross-chunk search criteria.
- [scope-control](scope-control/SKILL.md) — respect rename-only and
  no-production-code boundaries.

## Validation and dependency semantics

- [canonical-schema](canonical-schema/SKILL.md) — derive validation from the
  authoritative schema or struct.
- [dependency-behavior](dependency-behavior/SKILL.md) — read the dependency's
  pinned source for exact parser semantics.
- [error-classification](error-classification/SKILL.md) — define error-kind
  boundaries with a decision table.
- [merge-validation](merge-validation/SKILL.md) — validate discarded or
  overridden input branches when the contract requires it.
- [pipeline-tracing](pipeline-tracing/SKILL.md) — trace a concrete input
  through layered validation order.
- [yaml-key-identity](yaml-key-identity/SKILL.md) — include YAML scalar tags
  when comparing keys.

## Tests and repository gates

- [collection-regressions](collection-regressions/SKILL.md) — cover every
  entry in a consistency-check collection.
- [diagnostic-assertions](diagnostic-assertions/SKILL.md) — parse and validate
  diagnostic line numbers rather than matching a substring.
- [exemption-justification](exemption-justification/SKILL.md) — verify every
  gate exemption's stated reason.
- [fail-open-tables-need-a-totality-gate](fail-open-tables-need-a-totality-gate/SKILL.md) —
  hold permissive-default tables to the schema they classify.
- [fixture-path-fidelity](fixture-path-fidelity/SKILL.md) — justify fixture
  paths against the external tool's real layout.
- [frozen-allowlists](frozen-allowlists/SKILL.md) — keep authorization scoped
  to the named allowlist entry.
- [gated-test-placement](gated-test-placement/SKILL.md) — place unit tests and
  E2E tests where their separate gates actually execute them.
- [gtk-headless-testing](gtk-headless-testing/SKILL.md) — keep puregotk out of
  unit-test binaries; write and debug the Dakota behave AT-SPI suite.
- [helper-test-surface](helper-test-surface/SKILL.md) — call helpers directly
  when that is an explicit acceptance criterion.
- [self-referential-assertions](self-referential-assertions/SKILL.md) — keep
  expected values independent of the code and host under test.

## Runtime, build, and supply chain

- [bounded-stream-rendering](bounded-stream-rendering/SKILL.md) — cap both
  queued callbacks and retained widgets for streamed output.
- [multi-arch-digest-pinning](multi-arch-digest-pinning/SKILL.md) — pin the
  manifest index where available and verify supported platforms at roll time.
- [package-namespace](package-namespace/SKILL.md) — avoid package-level
  identifier collisions with same-package tests.
- [phony-target-shadowing](phony-target-shadowing/SKILL.md) — declare command
  targets `.PHONY` before a directory shadows them.

## Documentation and removal

- [documentation-reconciliation](documentation-reconciliation/SKILL.md) —
  reconcile source-backed claims, active plans, and immutable decision history.
- [leaf-package-documentation](leaf-package-documentation/SKILL.md) — document
  concrete outcomes and the model/adapter boundary.
- [leaf-package-inventory](leaf-package-inventory/SKILL.md) — update exact
  package enumerations alongside the package they describe.
- [removal-verification](removal-verification/SKILL.md) — scope absence checks
  to shipped code and current docs without fighting regression evidence.

The former `docs/agents/skills/*.md` files are compatibility aliases to these
canonical packages. Never edit the aliases; update their canonical targets.
