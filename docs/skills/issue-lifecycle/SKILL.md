---
name: issue-lifecycle
description: Use when filing or triaging ChairLift reports, classifying labels, accepting or assigning work, linking PRs, recording delivery, verifying fixes, or migrating the local lifecycle.
version: 1.1.0
last_updated: 2026-10-04
tags:
  - factory
  - issues
  - labels
  - release
metadata:
  type: reference
---

# ChairLift issue lifecycle

ChairLift is onboarded and owns its intake, label catalog and application
delivery boundary. The shared implementation lives in `projectbluefin/actions`;
this repository consumes its managed `v1` rather than maintaining another bot.
Start with local [`AGENTS.md`](../../../AGENTS.md) and
[`factory-onboarding`](../factory-onboarding/SKILL.md). Common supplies
[cross-repository label guidance](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md)
and [human gates](https://github.com/projectbluefin/common/blob/main/docs/skills/human-gates.md).
Its Common-only pilot commands are not ChairLift commands; use this local
catalog and the released Actions runtime for the adopted procedures below.

## Deployed source and Hive boundary

- [`.github/issue-policy.json`](../../../.github/issue-policy.json) defines local
  vocabulary, protected reader signals and release-shaped delivery evidence.
- [`issue-lifecycle.yml`](../../../.github/workflows/issue-lifecycle.yml) consumes
  the [released reusable workflow](https://github.com/projectbluefin/actions/blob/v1/.github/workflows/reusable-issue-lifecycle.yml).
  Events apply reconciliation; hourly repair is labels-only. Explicit dispatch
  defaults to read-only. Prow runs before reconciliation in the same serialized
  shared workflow; scheduled repair stays quiet.
- [`issue-policy-preview.yml`](../../../.github/workflows/issue-policy-preview.yml)
  consumes the [read-only preview](https://github.com/projectbluefin/actions/blob/v1/.github/workflows/reusable-issue-policy-preview.yml)
  with read permissions and no secrets. It can inspect candidate data/runtime
  refs without activating them. Production uses managed `v1`; third-party
  actions remain SHA-pinned. A preview is not permission to apply a migration.

Hive supplies assignment and scheduling, not ChairLift acceptance. The local
runtime uses Hive's native `needs-human` enumeration gate while scope is
unaccepted, human-only, paused, blocked, unclassified or otherwise ineligible;
it preserves independent human/App gates after acceptance. This is GitHub-side
enforcement, not a Hive deployment or scheduling change, and cannot guarantee
that cached or differently configured workers obey it. Verify the trusted
human acceptance event, current scope, assignment, preference and independent
gates even when Hive reports work ready. Neither PR approval nor delivered
software substitutes for those implementation gates.

## Preflight and intake

1. Verify the target is `projectbluefin/chairlift`, the exact issue or PR, the
   current scope and any Hive assignment. Read `.knowledge/README.md` and
   `.memory/README.md` plus their indexed corrections before acting.
2. Read the default-branch [`issue-policy.json`](../../../.github/issue-policy.json)
   and caller workflow. That catalog, not another repository's labels or a work
   board column, defines the local vocabulary. Read live assignment and Hive
   state when needed; an offered queue item is not implementation approval.
3. Use the [bug or feature forms](../../../.github/ISSUE_TEMPLATE/). Bug reports
   need actual behavior, expected behavior, reproducible steps, ChairLift's
   About/package version, and the booted OS/image/version. Identify the page or
   control, installation source, and any affected privileged helper. Logs and
   screenshots must be redacted. A missing fact should be reported as unknown,
   not invented.
4. Preserve the **Automation preference** exactly: **Human interaction only**,
   **Machine analysis is welcome**, or **No preference**. The first requests
   `human-only`; the other choices never accept implementation. Routine status
   reconciliation is not permission for machine analysis or agent execution.
5. Keep Common reports in Common. Link cross-repository dependencies where they
   are; this intake does not transfer reports, reroute application code, or
   authorize a change to image ownership. Questions can use ChairLift
   Discussions. Blank/CLI intake still needs classification and assessment;
   reporters never need label permissions or slash commands.

Preflight is complete when the repository, scope, preference, source version,
required evidence and next human decision are known or explicitly requested.

## Catalog and reader contracts

Open issues carry **one** of `needs-triage`, `triage/needs-information`,
`triage/accepted`, `awaiting-release`, or `needs-verification`. PRs carry no
issue-stage labels: native assignment, review requests, review state, checks
and the merge queue describe their progress. A managed canonical kind describes
work, not authorization; an open issue has exactly one primary kind. Protected
operational `kind/*` signals do not count toward classification. Missing or
conflicting classification needs a maintainer decision, not an arbitrary dispatch.

The catalog contains:

- **Descriptive intake:** this catalog's `intake_rules` declares ACMM/guide/quality
  and title/body classification locally. Literal selectors are bounded and can
  add catalog metadata only; they do not authenticate a filing agent, grant
  consent/acceptance, assign work or override an existing primary kind.
  Native `question` denial is not auto-projected from a title; existing explicit
  assignments remain until their real owner resolves and withdraws the gate.
- **Kinds:** `kind/bug`, `kind/feature`, `kind/task`, `kind/documentation`,
  and `kind/debt`. These are primary work types, not stacked topics.
- **Native areas:** the route identities `updates`, `applications`, `agents`,
  `features`, `livery`, `maintenance`, `help`, and `recovery`, plus source-backed
  subsystem areas `homebrew`, `flatpak`, `bootc`, `updex`, `privileged-helper`,
  `configuration`, `accessibility`, `ci`, and `release`. Cross-cutting
  `area/quality`, `area/testing`, `area/security` and `area/architecture` stack
  alongside these, so classifying a bug does not erase its security or testing
  context. Areas do not replace route or code ownership. Route identity is
  defined by [`internal/navigation`](../../../internal/navigation/navigation.go).
- **Independent overlays/gates:** `blocked`, `hold`, `human-only`,
  `needs-human`, `needs-decision`, `needs-kind`, and `tracking`. Preserve their
  owner and reason when classifying or advancing a stage. Acceptance clears
  only an eligible lifecycle-bot gate, not a human/App gate or pause.
  `human-only` is set from the reporter's preference at intake; a maintainer
  can waive it by removing the label, and the bot won't re-add it. On an
  accepted issue, `needs-decision` pauses work; acceptance stays, and removing
  `needs-decision` resumes it.
- **Operational readers:** existing `agent/*`, `hive/*`, `from-review`,
  `priority/*`, `acmm`, `ai-fix-requested`, newcomer and disposition labels remain
  separate from stage and kind. An implementation-request label still requires
  current accepted scope and all independent safety gates.
- **Protected operational signals:** `protected_labels` names `kind/tech-debt`
  and `source:agent`. Their existing assignments and live definitions remain
  unmanaged and unchanged; they are not descriptive aliases, intake targets,
  primary kinds, ambiguity candidates or retirement candidates.

The unprefixed `bug`, `enhancement`, `question` and `epic` labels are retained
reader contracts, not extra canonical kinds. `kind_sources` derives a primary
kind from existing reader metadata only when no primary kind exists; it never
writes reverse mirrors. `gate_labels` preserves `question` as an independent
answer-required negative gate, not implementation acceptance. Pinned Hive
source uses the legacy labels in ordering, exact contributor skips and tracker
detection. Keep their definitions and assignments until an evidenced reader
cutover; Prow cannot edit them or clear the operational question gate.
The [Hive operator reference](https://github.com/hivecommons/hive/blob/263382dd59d27c9a921d261ab4dd2172fa0d0f79/src/docs/labels-and-control-signals.md)
also documents `from-review` as informational, agent lane tokens as segmented
routing, tracker detection by the final `epic` segment, and the independent
`needs-human` enumeration gate. This is reader evidence, not a promise that
cached or differently configured workers cannot act.

Live descriptions of `source:agent` and `kind/tech-debt` claim an auto-merge role,
but descriptions and historical issue text do not establish active consumer
configuration. The pinned Hive operator reference documents a configurable
`governor.labels.automerge` queue label (default `lgtm`), not an intrinsic debt
or provenance approval. ChairLift's live operator configuration is unconfirmed.
Only the Hive operator can resolve that limited uncertainty. Until then, neither
onboarding nor sync rewrites their definitions or changes their assignments.
Use non-colliding `kind/debt` for ordinary debt; `from-review` retains the pinned
reader's review-finding meaning.

Standing trackers [#137](https://github.com/projectbluefin/chairlift/issues/137),
[#252](https://github.com/projectbluefin/chairlift/issues/252), and
[#328](https://github.com/projectbluefin/chairlift/issues/328) retain their
child-work/portfolio role. Their bodies enumerate unfinished sub-work; an old
human queue or a merged child is not proof that the parent is complete. Keep
`tracking` and the human preference; assign concrete child tasks separately.

## Maintainer acceptance and assignment

1. Assess the report at `needs-triage`. Select `triage/needs-information` if
   information or a decision is missing, then post a new specific request.
   Explicitly `@mention` the reporter only if they must answer; otherwise name
   the responsible maintainer. Old or answered questions do not request action.
   A reporter's ordinary reply returns the report to assessment, not directly
   to accepted implementation.
2. Review or edit scope and acceptance criteria **before** accepting. Resolve
   a `needs-decision` reason through its owner. In GitHub's **Labels** picker,
   add `triage/accepted` as a human with current write/maintain/admin permission.
   Removing `needs-triage` or `needs-human` is not approval: the bot restores
   waiting labels until it has a valid acceptance event. `/hive approve` and
   Prow commands are not ChairLift lifecycle acceptance controls.
   An operational `question` label remains an independent answer-required gate
   even after acceptance. Its owner resolves the answer and explicitly removes
   that label in the Labels picker before new implementation can start; a
   `/kind` command or acceptance never clears it.
3. Treat body changes and a return to assessment/information gathering as a
   new scope requiring fresh acceptance. Bot maintenance does not replace the
   underlying human acceptance or withdrawal. Decline or close a duplicate
   with the explicit reason instead of inventing another stage.
4. **Acceptance does not assign a contributor.** Assign in GitHub or explicitly
   route accepted work through the existing factory process. Verify the
   assignee, scope, `human-only`, unresolved gates, `blocked` and `hold` before
   starting. Name each blocker, its owner and what permits resuming. Neither
   analysis consent nor an old queue authorizes implementation.

Every lifecycle post should state **Status**, then the relevant role headings
(**Maintainer**, **Contributor**, **Reporter**), concise action bullets, and an
explicit **Reporter action**. Keep an unchanged waiting notice quiet; do not
replace a question, blocker or tracker instruction with a dense generic post.
Reporters reply normally and need no label privileges.

## Implementation, native review and release

1. Link product fixes using **`Refs #NNN`** while installation delivery or
   reporter verification is outstanding. Use **`Closes #NNN`** only for code-only
   work whose acceptance criteria are satisfied at merge, or a report already
   delivered and verified. A merged related reference may be documentation;
   it does not automatically advance the actual fix or close the report.
2. Implement the accepted scope, add the regression coverage and current-state
   docs, and complete the [PR template](../../../.github/pull_request_template.md).
   Follow [CONTRIBUTING](../../../CONTRIBUTING.md), the local risk/security
   gates, and Common's linked human gates. Preserve native approvals,
   assignees, review requests and existing queue decisions.
3. Follow ChairLift's native `main — review policy`: one required approving
   review, the **Tests Passed** aggregate, and the squash/ALLGREEN merge queue.
   The queue runs `merge_group` validation; green branch CI or an agent review
   does not substitute for the human gate. Re-read live rulesets before
   publication. Administrative capability is not authorization to bypass.
4. After verifying that the **actual accepted fix** merged, a maintainer names
   its PR/commit and affected component and selects `awaiting-release` in the
   Labels picker. Merge is not publication. Keep the product report open.
5. Publish only from clean, reviewed, CI-green `origin/main`, using ChairLift's
   existing stable `vYY.MM.N` tag workflow. The
   [local release checklist](../factory-onboarding/SKILL.md#stable-release-cutover)
   owns exact release commands and checks. ChairLift is **Homebrew-only**;
   there are no deb/rpm/apk packages. Verify the successful publishing run,
   signed architecture-appropriate release assets and the Homebrew package
   that the reporter can actually install.
6. If the fix changes `chairlift-helper`, `chairlift-updex-helper`, PolicyKit
   files, or an image-owned dependency, also verify delivery on the reporter's
   affected booted image. A Homebrew app publication does not update helpers
   installed at `/usr/bin/`, policies under `/usr/share/polkit-1/actions/`, or
   the image-provided `/usr/libexec/bootc-update-stage`. Link the image evidence
   and record any necessary update/reboot in the verification instructions.

## Delivery evidence and reporter verification

A trusted human verifies installation availability, then edits the issue body
with a **Delivery evidence** heading and the following fields before selecting
`needs-verification` in the Labels picker:

```text
Package: <actual Homebrew cask/tap or delivered component>
Version: <actual published stable vYY.MM.N version>
Fix revision: <full 40-hex merged commit>
Release/build: https://<actual published release or successful publishing run>
Verify: <exact install/update, restart/reboot and original reproduction steps>
```

Use real values, not a guessed version or a skipped publication run. Include
separate image/helper delivery links when required; the runtime checks the
record shape and authorized request, while the human verifies the fix really
reaches the named installation. Record the final body first, then select the
stage so the authorization matches the body revision.

The reporter installs the named version, performs any specified image update
and restart/reboot, repeats the original reproduction, and replies normally:

- **Confirmed fixed** plus the tested version confirms the delivered fix and
  allows closure after the authorized verification request.
- **Still broken** plus the tested version and evidence returns the report to
  triage; do not infer success from silence or a merged PR.

A maintainer reviews other replies and unresolved scope. No reporter command,
label permission, automatic merge-close or early approval is required.

## Maintainer Prow commands

[`prow.yaml`](../../../.github/prow.yaml) configures the constrained wrapper for
CNCF Prow GitHub Actions **v3.0.1**
([immutable upstream revision](https://github.com/cncf/prow-github-actions/tree/187c5e3cd95a329c43448e1bdb3b1f5249232e44)).
The local catalog is the allowlist. Send exactly one command as the entire
comment; current human write/maintain/admin permission is checked. Mutating
commands are **issue-only**, not a replacement PR review/merge interface.

| Command | Authorized result |
| --- | --- |
| `/kind VALUE` | Select one managed catalog kind; refused while a protected operational `kind/*` is assigned |
| `/area VALUE` | Add one catalog area; other areas remain |
| `/remove-area VALUE` | Remove that catalog area |
| `/hold` | Pause the issue with literal `hold` |
| `/hold cancel`, `/unhold`, `/remove-hold` | Explicitly withdraw literal `hold` only |
| `/help`, `/prow help` | Wrapper-only informational help; no labels change |

For example, `/kind bug` selects `kind/bug`; `/area updates` adds
`area/updates`. Hold withdrawal does not clear `blocked`, `human-only`,
`needs-human`, `do-not-merge*` or acceptance requirements. Read the pause reason
before explicitly withdrawing it. The wrapper reports the actual applied,
denied, invalid or failed result with specific next steps. Check that result,
not merely the command's presence.

`/kind debt` selects ordinary `kind/debt`, never operational `kind/tech-debt`.
Upstream exclusive `/kind` removes every other `kind/*`, including labels outside
its allowlist. The wrapper therefore denies execution if protected `kind/tech-debt`
is already assigned, even for an otherwise valid/no-op kind request. In GitHub's
**Labels** picker, select the desired managed kind and deselect only other managed
primary kinds; leave `kind/tech-debt`, `source:agent` and independent gates
unchanged. Do not remove the protected signal just to make a slash command work.
An operator must confirm the live consumer configuration before anyone changes
these operational assignments or definitions.

There is no generic `/label`, `/remove-kind`, stage/acceptance command,
assignment, approval, implementation dispatch or merge command in this surface.
Upstream `/help` mutates a label, so it is not forwarded; help here is read-only.
Prow's PR label-to-merge path is not enabled. Reporters continue to use normal
replies, not these maintainer controls.

## Full-history catalog migration

Use Common's shared
[factory entry procedure](https://github.com/projectbluefin/common/blob/main/docs/skills/factory-onboarding.md#copyable-agent-onboarding)
for local authority and human gates. For activation, preview/archive and mutation,
use ChairLift's callers above and the
[released Actions implementation](https://github.com/projectbluefin/actions/blob/v1/scripts/issue_policy.py),
not Common's repository-specific script or dispatch commands. Locally:

1. Inventory **all** issue and PR assignments, open and closed, and every label
   writer/reader before mutation. Archive definitions, assignments, preference,
   scope and current assignees. Update owned writers together so they cannot
   restore retired stages; preserve operational and external Hive contracts.
   ChairLift's `prior_comment_markers` names the existing Common hidden marker
   solely to reuse an authoritative machine status already in transferred
   history. New reports use the ChairLift marker; this configuration neither
   transfers reports nor grants old bot text acceptance or delivery authority.
2. Review the catalog's `label_aliases` and `retired_stages` against that archive.
   Descriptive mappings are explicit:

   | Retired description | Canonical target |
   | --- | --- |
   | `docs`, `documentation` | `kind/documentation` |
   | `quality`, `testing`, `security`, `architecture` | Corresponding `area/*` |
   | `tech-debt` | `kind/debt` |
   | `roadmap` | `kind/task` |
   | `accessibility`, `ci` | Corresponding `area/*` |

   `kind_sources` establishes `bug` → `kind/bug`, `enhancement` →
   `kind/feature`, and `question`/`epic` → `kind/task` when no primary kind
   exists, retaining every operational reader assignment. `question` remains
   an independent negative gate and `epic` retains tracker handling; neither
   becomes a dispatchable kind. Conflicting kinds require a human
   classification rather than silently discarding meaning.
   Existing `kind/tech-debt` and `source:agent` are explicitly protected instead
   of being aliases or kinds. They stay unchanged on open/closed issues and PRs;
   ordinary `tech-debt` migration never adds either operational signal.
3. Retire `1-triage`, `2-discussing`, `3-human-queue`, `3-clanker-queue`,
   `4-review`, `status/discussing`, `status/queued`, `status/claimed`,
   `queue/agent-ready`, and `queue/claimed` as stage assignments. None maps to
   acceptance. Old human routing remains `human-only`; ambiguous queues return
   to assessment. Preserve explicit human scope, trackers, independent gates,
   `blocked`/`hold`, all assignees, approvals and native PR state. Closed history
   receives taxonomy cleanup, not new open-issue stages or reopening.
4. Apply only the reviewed labels-only migration after the shared implementation
   and caller pass CI and are present on `main`. Archive before writing; no
   migration comments, mentions, closures, transfers, assignments or approvals.
   GITHUB_TOKEN mutations do not reliably trigger a second Actions workflow:
   serialize Prow and reconciliation in the caller and keep scheduled repair.
5. Retire obsolete definitions only after all historical assignments are
   accounted for and all supported writers/clients have cut over. Retained
   operational labels are not retirement candidates. Keep the archive outside
   the repository and use the deployment's backup artifacts for recovery.
6. Prove one issue stage, one managed primary kind, no PR stages, preserved
   operational signals/gates/preferences, truthful bot/Prow outcomes, and no
   premature close with real event flows.
   A green docs-only check or catalog creation alone is not activation proof.
