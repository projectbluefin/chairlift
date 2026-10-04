# Agent prompt catalog

These reusable prompts give coding agents a consistent starting point for
common ChairLift tasks. Copy a prompt, replace its bracketed placeholders, and
include the relevant issue or diff.

The prompts supplement rather than replace repository instructions. Start with
`AGENTS.md` and the task router `docs/SKILL.md`; load only the relevant canonical
packages from `docs/skills/`. Repository instructions take precedence, including
human ownership of root `README.md` and immutable accepted ADR decisions.

## Catalog

| Prompt | Use it for |
|---|---|
| [Implement an issue](implement-issue.md) | Planning, coding, testing, and documenting a scoped issue |
| [Review a change](review-change.md) | Risk-focused review of a branch or pull request |
| [Reconcile documentation](reconcile-documentation.md) | Checking current-state docs against source and configuration |

All implementation work should finish with `make ci`. If the environment
cannot run a gate, report the exact command and reason instead of claiming it
passed.
