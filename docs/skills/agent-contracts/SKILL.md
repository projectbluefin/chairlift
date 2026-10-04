---
name: agent-contracts
description: Use when a change affects AGENTS.md claims or plan acceptance criteria.
version: 1.1.0
last_updated: 2026-10-04
tags:
  - planning
  - agent-contracts
metadata:
  type: reference
---

# A chunk that changes what AGENTS.md asserts must list AGENTS.md itself in `files`

**When it applies:** Planning or reviewing any chunk that changes a fixed
path, default, or invariant that AGENTS.md's "Repository invariants" or
"Build, test, lint" sections currently describe in prose — e.g. the
`chairlift-updex-helper` install path, the `pkexec` targets, the default
`make install` `PREFIX`, or any other fact AGENTS.md states as fact rather
than pointing to code for.

**What to do:** Check each changed fact against AGENTS.md's current wording and
include its reconciliation in the same chunk's files and acceptance criteria.
Updating another doc while AGENTS.md still states the old helper path, default,
or invariant leaves the contract stale. Update relevant `AGENTS.md` and `docs/`
claims; root `README.md` is human-owned, so report necessary corrections without
editing it. Do not pad unrelated chunks with AGENTS.md changes: relevance, not a
mandatory edit to every doc, is the bar.

**Learned from:** issue #59's mill run, plan round 2 — a chunk changed the
updex helper invocation to a fixed `/usr/bin/chairlift-updex-helper` path and
changed the supported source-install prefix, updating README.md and yeti/ but
omitting AGENTS.md from its `files`/acceptance criteria. The reviewer rejected
the plan because AGENTS.md's privilege-boundary and build/install text were
now directly stale, even though the other two doc locations were current.
