---
name: skill-improvement
description: Use when finishing a ChairLift change and deciding what durable knowledge to preserve.
version: 1.0.0
last_updated: 2026-09-08
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

Load Common's full procedure as a sidecar:
[`skill-improvement.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/skill-improvement.md).
This package records ChairLift's local outputs without copying Common's policy.
