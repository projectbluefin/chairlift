---
name: skill-improvement
description: Use when finishing a ChairLift change and deciding what durable knowledge to preserve.
version: 1.1.0
last_updated: 2026-10-04
tags:
  - factory
  - documentation
metadata:
  type: reference
---

# Skill improvement

Treat every completed change as producing two outputs: the requested
repository change and a knowledge decision.

For the knowledge decision:

1. Check whether the change established a durable lesson. If so, put it in
   the closest canonical package under `docs/skills/`; edit `SKILL.md`, never a
   compatibility alias.
2. Verify that [`docs/skills/index.md`](../index.md) links every canonical
   package exactly once and that [`docs/SKILL.md`](../../SKILL.md) still points
   to the catalog.
3. When source-backed evidence disproves an earlier belief, append a
   correction to `.memory/corrections.jsonl` using the schema in
   [`.memory/README.md`](../../../.memory/README.md).
4. Keep the trigger specific (`description` begins `Use when`) and the workflow
   short. Retain the local `name`, `version`, `last_updated`, block-list `tags`,
   and `metadata.type: reference` fields required by
   `internal/installcheck/factorydocs_test.go`; Common's larger catalog schema
   and generator are not ChairLift requirements.
5. Link the current source owner or canonical skill for details. Label
   **Learned from** examples as history rather than freezing PR status, source
   line numbers, or retired symbols into current instructions. Aim below 200
   lines and keep packages below 500; disclose branch-specific reference only
   when lazy loading earns another file, not to hit a line target.

## Verification

The integration owner runs
`go test ./internal/installcheck -run '^TestFactoryDocumentationContract$'`
and `make ci` after the complete change. That gate checks frontmatter, catalog
membership, canonical entry points, and retained compatibility aliases; it does
not prove that skill prose matches current behavior. Verify factual claims
against the files the skill names, and leave unrelated skills unchanged.

Load Common's full procedure as a sidecar:
[`skill-improvement.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/skill-improvement.md).
This package records ChairLift's local outputs without copying Common's policy.
