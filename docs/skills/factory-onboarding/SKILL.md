---
name: factory-onboarding
description: Use when starting or resuming factory work in ChairLift, submitting changes through the merge queue, or publishing a stable release; load issue-lifecycle for intake, label decisions, assignment and delivery verification.
version: 1.4.0
last_updated: 2026-10-04
tags:
  - factory
  - onboarding
metadata:
  type: reference
---

# Factory onboarding

Follow Common's
[`Copyable Agent Onboarding`](https://github.com/projectbluefin/common/blob/main/docs/skills/factory-onboarding.md#copyable-agent-onboarding)
and [agentic model](https://github.com/projectbluefin/common/blob/main/docs/factory/agentic-model.md),
with ChairLift's [`AGENTS.md`](../../../AGENTS.md), [`task router`](../../SKILL.md)
and [`canonical catalog`](../index.md) as local authority. Verify the repository,
issue/PR, branch target, scope and corresponding Hive assignment before acting.
Query Project Bluefin MCP knowledge before investigation, design or implementation;
check live factory status/work queue when assignment state matters.

Common is a sidecar, not a replacement local contract. Keep transient task
records in non-committed session state; preserve durable lessons through
[`skill-improvement`](../skill-improvement/SKILL.md), not a tracked session log.

For issue intake, label/catalog decisions, acceptance and assignment, Prow
commands, migration or reporter verification, load
[`issue-lifecycle`](../issue-lifecycle/SKILL.md). It links shared policy and
owns the local Homebrew-versus-image-helper delivery boundary. Verify live
issue state and the local workflow/policy wiring before changing labels.
Common's documentation-only direct-push exception does not bypass ChairLift's
pull-request and merge-queue contract.

## Local mechanic: the merge queue, and how to get back out of it

Human approval and independent security gates come from Common's linked
[`human-gates.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/human-gates.md)
and
[`label-workflow.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md).
ChairLift's actual merge requirements are its native ruleset: one required
approving review, **Tests Passed**, and the squash/ALLGREEN merge queue. Read
the live ruleset before publishing; an administrative bypass capability is
not permission to use it. Issue acceptance is separate from PR approval.

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

## Stable release cutover

ChairLift's release name is `vYY.MM.N`, starting at `N=1` each month. Run
`./scripts/next-version.sh` to inspect the next tag without creating it;
historical alpha tags do not advance the stable sequence, and the script
rejects prerelease arguments. `make bump` no longer accepts a prerelease
suffix. Do not delete or rewrite historical releases to change naming.

After an externally approved pull request completes the merge queue, fetch
`origin/main`, verify its exact head has green CI, and release only from that
clean head. Never tag the feature branch while waiting for approval.
`goreleaser check` validates the publication configuration without publishing;
`release.prerelease: false` publishes a full release and GoReleaser's default
`make_latest: true` marks it latest. The existing tag workflow owns builds,
SBOMs, signatures, and publication; use it rather than uploading local builds.

Source: Context7 `/goreleaser/goreleaser`,
[`release` configuration](https://goreleaser.com/customization/release/)
and [`goreleaser check`](https://goreleaser.com/cmd/goreleaser_check/).

### Verification

- The next-version regression exercises fresh and existing stable tags,
  monthly reset, and historical prerelease tags in temporary repositories.
- The final PR head passed CI and a human reviewer approved it; its merge
  queue candidate passed `Tests Passed` before the release tag was created.
- The release workflow succeeded and the published release is neither a
  draft nor a prerelease; signed archives and checksums exist for both arches.

### Red flags

- A tag points at an unmerged feature branch, or an agent approval substitutes
  for the human gate.
- A stable tag is created to conceal failed or incomplete required CI.
- Local archives replace the workflow's signed publication path.
