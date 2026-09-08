---
name: factory-onboarding
description: Use when starting or resuming a Project Bluefin factory-assigned change in ChairLift.
version: 1.0.0
last_updated: 2026-09-08
tags:
  - factory
  - onboarding
metadata:
  type: reference
---

# Factory onboarding

Use this local checklist before investigating, designing, or implementing a
factory-assigned change:

1. Read the local [`AGENTS.md`](../../../AGENTS.md).
2. Read [`docs/SKILL.md`](../../SKILL.md), then choose the task-matching
   package from [`docs/skills/index.md`](../index.md).
3. Verify the GitHub issue or pull request and the corresponding Hive factory
   assignment before acting.
4. Query the Project Bluefin MCP for relevant knowledge before investigation,
   design, or implementation; check live factory status or the work queue when
   the task depends on assignment state.
5. Load Common's procedures as a sidecar rather than copying their policy:
   [`factory-onboarding.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/factory-onboarding.md)
   and
   [`agentic-model.md`](https://github.com/projectbluefin/common/blob/main/docs/factory/agentic-model.md).

This package is ChairLift's local entry point. Common remains authoritative for
cross-repository factory rules.
