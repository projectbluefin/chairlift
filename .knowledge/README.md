# `.knowledge/` - cross-session knowledge index

This directory is ChairLift's stable discovery point for knowledge that should
survive an individual agent session. It indexes the repository's canonical
artifacts instead of copying their contents, which prevents guidance from
drifting between stores.

## Read before working

1. Read `AGENTS.md` for current repository invariants and required workflows.
2. Read `docs/SKILL.md`, then the matching package from `docs/skills/` for
   durable lessons from prior automated runs.
3. Read `.memory/README.md` and `.memory/corrections.jsonl`, when present, for
   verified corrections.
4. Check the host-provided session/workspace handoff outside this repository,
   when present. It is transient context and must not be committed here.
5. Read `docs/design/overview.md` and the relevant `docs/design/` subsystem
   documents (formerly `yeti/`) for architecture and decision rationale.

## Record knowledge in its canonical location

- Put verified corrections in `.memory/corrections.jsonl`.
- Put active handoffs only in the host-provided, non-committed
  session/workspace state; do not create or restore
  `.claude/session-summary.md`.
- Promote stable operating rules to `AGENTS.md`, reusable lessons to
  `docs/skills/`, architecture to `docs/design/`, and repo-local
  decision rationale to `docs/adr/`.

Keep this file as an index rather than a duplicate knowledge store. Record only
verified repository context, and never commit secrets, credentials, personal
data, or unverified speculation.
