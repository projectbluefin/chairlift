---
name: factory-onboarding
description: Use when starting or resuming a Project Bluefin factory-assigned change in ChairLift.
version: 1.1.0
last_updated: 2026-09-20
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

## Local mechanic: the merge queue, and how to get back out of it

The gates themselves are Common's policy, not ChairLift's. Who may approve,
when the Merge Gate is satisfied, what the independent Security Gate covers,
and why `hold` is workflow-controlled all live in Common's
[`human-gates.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/human-gates.md)
and
[`label-workflow.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md);
read them there rather than reasoning from a local copy.

What is local, and what a factory agent has to know before touching a pull
request here, is that this repository merges through a **merge queue**:

- `gh pr merge <n>` enqueues the pull request; it does not merge it.
- While queued, the pull request still reports `state=OPEN` with
  `autoMergeRequest=null`, so neither field is evidence that nothing has
  happened yet.
- A queued pull request **can** be removed, but not with `gh pr merge
  --disable-auto`: that subcommand only cancels auto-merge, and against a
  queued or already-merged pull request it fails with
  `disablePullRequestAutoMerge`. The real operation is the GraphQL
  `dequeuePullRequest` mutation, which `gh` does not wrap:

  ```sh
  gh api graphql -f query='
    mutation($id: ID!) {
      dequeuePullRequest(input: {pullRequestId: $id}) {
        mergeQueueEntry { position }
      }
    }' -F id="$(gh pr view <n> --repo projectbluefin/chairlift --json id --jq .id)"
  ```

  Inspect the queue first with the `mergeQueue { entries }` field on
  `repository`, since a merged entry is already gone and cannot be dequeued.
- Irreversibility begins when the queue COMPLETES the merge, not when the
  pull request is enqueued. The window is short and is not visible in the
  pull request's own fields, so treat it as small but real rather than
  absent.
- `.github/workflows/test.yml` declares a `merge_group` trigger, so the queue
  revalidates each candidate on its own `gh-readonly-queue/main/pr-<n>-<sha>`
  ref and the single required check is **Tests Passed**. The queue's
  *existence* is still branch-protection configuration rather than something
  the workflow files state; do not conclude from a workflow that there is no
  queue, and do not conclude from a green pull-request check that the queue
  will not re-run everything against a newer `main`.
- Automation cannot push to `main`; it must open a pull request, and not
  with `GITHUB_TOKEN`: pull requests that token creates trigger no workflow
  runs, so **Tests Passed** never reports and the queue never admits them.
  `release-screenshots.yml` mints a MergeRaptor GitHub App token
  (`MERGERAPTOR_APP_ID`/`MERGERAPTOR_PRIVATE_KEY`) for
  `peter-evans/create-pull-request`, and its commit carries no `[skip ci]`
  for the same reason.

The practical rule: resolve every gate question — merge and security alike,
per pull request, not once per batch instruction — **before** the
`gh pr merge` call, because the window in which `dequeuePullRequest` can
still save you is measured in seconds. Default to
preparing and pushing the verified branch and stopping there; enqueue nothing
without an explicit, current instruction from the session owner. ADMIN
permission, a green `make ci`, and an agent-submitted approving review are
none of them the human gate.

Record findings as at most one consolidated comment per pull request rather
than narrating agent actions turn by turn.

**Learned from:** the 2026-09-18 triage session. Pull requests #124, #109 and
#127 were all enqueued with `gh pr merge` and completed by the queue before a
gate objection could be acted on; they are on `main` as the squash commits
`1373e2c (#124)`, `d208e56 (#109)` and `f55ee40 (#127)`. The #127 recovery
attempt also recorded the wrong lesson at first: `gh pr merge 127
--disable-auto` returned `disablePullRequestAutoMerge`, which was read as
"there is no dequeue". That inference was false — `dequeuePullRequest` exists
in the GraphQL schema — and the real reason the call failed is that the merge
had already completed. Reaching for the wrong tool and then generalising from
its error message is the mistake worth remembering here.
