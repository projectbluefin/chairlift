# Quality dashboard

This page is the entry point for auditing ChairLift's current quality signals.
It links to live reports rather than copying pass rates or coverage percentages
that would immediately become stale. The [public metrics catalog](metrics/)
collects those read-only sources and their interpretation boundaries.

[![Tests](https://github.com/projectbluefin/chairlift/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/projectbluefin/chairlift/actions/workflows/test.yml?query=branch%3Amain)

## Live signals

| Signal | What it reports | Source |
|---|---|---|
| Tests workflow | Latest lint, filtered unit tests, race detection, E2E, sharded AT-SPI, verification, and cross-architecture build results | [GitHub Actions](https://github.com/projectbluefin/chairlift/actions/workflows/test.yml) |
| Nightly compliance | Daily full CI, E2E, and known-vulnerability scan results for the default branch | [GitHub Actions](https://github.com/projectbluefin/chairlift/actions/workflows/nightly-compliance.yml) |
| Issue lifecycle | Reviewed stage authority, catalog-bound descriptive intake and constrained Prow reports | [GitHub Actions](https://github.com/projectbluefin/chairlift/actions/workflows/issue-lifecycle.yml) |
| Issue policy preview | Read-only proposed reconciliation and migration for caller catalog changes, including full closed-issue/PR history | [GitHub Actions](https://github.com/projectbluefin/chairlift/actions/workflows/issue-policy-preview.yml) |
| Pull request checks | Gate results attached to each proposed change, including reruns and logs | Open a pull request and select its **Checks** tab |
| Claude code review | Maintainer-triggered, read-only AI review comments for a selected pull request | [GitHub Actions](https://github.com/projectbluefin/chairlift/actions/workflows/claude-code-review.yml) |
| PR acceptance | Accepted and closed pull request counts over a rolling 90-day cohort | [Metric definition and reproducible query](metrics.md) |
| Build artifacts | Seven-day Linux binaries for the workflow's amd64 and arm64 matrix | Open a successful workflow run and view **Artifacts** |
| Release history | Published versions and release assets | [GitHub Releases](https://github.com/projectbluefin/chairlift/releases) |

## Enforced checks

The repository's `Tests` workflow runs on pushes to `main`, on pull requests
targeting `main`, and on every merge group the merge queue builds. Its jobs
provide these independent signals:

- **Lint** — `golangci-lint` over the Go source.
- **Unit Tests** — headless tests under `internal/...`, with atomic coverage.
- **Race Detection** — the same internal test scope under the race detector.
- **E2E** — `make e2e` under a headless GTK runtime, with the walkthrough
  screenshots uploaded as an artifact.
- **AT-SPI** — `make e2e-atspi`: the behave + dogtail AT-SPI suite
  (`test/e2e/features`) driving every destination inside the Dakota `testing`
  image pinned by digest in `test/e2e/dakota-image.sh` (currently
  linux/amd64), split into parallel shards (`AT-SPI (<shard>)`) by behave tag
  expression, each uploading its results as `atspi-results-<shard>`.
- **Verify** — tidy-module, `go vet`, and `gofmt` checks.
- **Build** — Linux builds for amd64 and arm64.
- **Tests Passed** — an aggregating job that succeeds only when every job
  above succeeded.

## The merge queue's gate

`main` merges through a merge queue, which validates a candidate on a
temporary `gh-readonly-queue/main/pr-<n>-<sha>` ref rather than on the pull
request's own head. That is a distinct `merge_group` event: neither `push` nor
`pull_request` fires for it, so a workflow that does not declare
`merge_group` produces no checks for the merge group at all, and the queue has
nothing to wait for.

Two pieces have to agree for the queue to gate:

1. `.github/workflows/test.yml` declares `merge_group`, deliberately with no
   branch filter — `github.ref` there is the queue ref, never `main`, so a
   `branches: [main]` filter would match nothing and silently disable queue
   validation.
2. The default-branch ruleset requires the **Tests Passed** check from the
   GitHub Actions app. It is the only required context by design: the
   individual job names are free to change (`Build` alone expands per matrix
   entry), while this one fans out to all of them through `needs`, so branch
   protection cannot drift out of step with CI.

`Tests Passed` runs with `if: always()` because GitHub counts a *skipped*
required check as a passing one; a gate that ran only when its dependencies
succeeded would report success for a run whose tests failed.
`internal/installcheck`'s `TestMergeQueueGateWaitsForEveryTestJob` holds both
properties and fails when a job is added to the workflow without being wired
into the gate.

`make ci` mirrors the **host-independent** checks locally in fail-fast order
and also rebuilds native binaries at the end. It does not run E2E or AT-SPI;
use `make e2e` and `make e2e-atspi` separately on a host with podman and Go.
The command definitions live in [`Makefile`](../Makefile).

```bash
make ci
```

To inspect the unit coverage scope locally, run:

```bash
go test ./internal/... \
  -coverprofile=coverage.out \
  -covermode=atomic \
  -run '^Test[^I]' \
  -skip Integration
go tool cover -func=coverage.out
```

The test-name filters are significant: ordinary unit tests must not begin with
`TestI` or contain `Integration`, because the gated command excludes them.
Tests for packages that import puregotk also cannot run headlessly; decidable
logic belongs in a pure package under `internal/`, as required by `AGENTS.md`.

## Nightly compliance

The `Nightly compliance` workflow runs every day at 04:17 UTC and can also be
started manually. It checks out the current default branch, runs the complete
host-independent `make ci` gate, runs `make e2e` and `make e2e-atspi` inside
the Dakota image under a private headless Mutter Wayland session (the same
`test/e2e/dakota.sh` the hosted E2E job uses), then runs `govulncheck ./...`
against the current Go vulnerability database. This catches dependency
disclosures and environment drift even when no pull request is active.

The workflow has read-only repository permission, persists no checkout
credentials, consumes no repository secrets, and publishes nothing. Its
third-party actions are pinned to commits and its Go compliance tools are
installed at explicit versions. A nightly failure is an investigation signal;
it does not replace pull-request checks or authorize an automatic merge.

Third-party actions use full 40-character commit SHAs with reviewed version
comments. Shared `projectbluefin/actions` production uses managed `@v1`;
candidate refs are limited to the secret-free, entirely read-only issue-policy
preview interface. The workflow security scan enforces both
the immutable third-party boundary and this constrained first-party authority.

## Release gating

The `goreleaser` workflow runs on pushed tags (`*`). Before any release or
asset can be published, publication is gated on two required verification jobs:

- **Host-independent quality gate** (`gate`) — runs with read-only repository
  permission and executes `make ci` (tidy-module, vet, formatting,
  `golangci-lint`, unit tests, race detector, and cross-architecture builds).
- **End-to-end quality gate** (`e2e`) — runs with read-only repository
  permission and executes `make e2e` and `make e2e-atspi` inside the Dakota
  image under a private headless Mutter Wayland session.

The `goreleaser` publishing job depends on both gate jobs (`needs: [gate, e2e]`)
and receives `contents: write` and `id-token: write` (for keyless cosign signing)
only after both have succeeded. It explicitly disables Go setup caching
(`cache: false`) so build or module caches populated during test and E2E gate
jobs cannot reach the release build. A failure in either gate stops publication.
Passing those checks is evidence for the tagged commit, not proof that every
hardware combination or failure mode has been exercised.

## Reviewing agent changes

For an agent-authored pull request, audit the signals in this order:

1. Read the issue and diff to confirm the change stays within scope.
2. Check the pull request's commit history and changed-files list for unrelated
   edits, generated artifacts, or missing documentation.
3. Require a green Tests workflow; inspect logs rather than relying only on the
   aggregate check mark.
4. Review coverage for changed pure-Go logic and verify regression tests cover
   the reported failure mode.
5. Apply the [pull request review rubric](review-rubric.md), including the
   repository invariants and learned skills, then record concrete findings on
   the pull request.

## Automated Copilot feedback

`.github/workflows/copilot-review-apply.yml` closes the feedback loop for
Copilot pull request reviews. When the trusted Copilot reviewer submits a
commented or changes-requested review with inline findings on an open,
non-draft pull request, the workflow posts one deduplicated `@copilot` fix
request linked to that review. A review without inline findings does not start
a fix cycle.

Issue implementation uses the shared lifecycle and native GitHub assignment.
An `ai-fix-requested` label is an intent marker, not acceptance or dispatch.
The maintainer first accepts the current scope through the Labels picker,
resolves independent gates, and assigns or explicitly routes the work.
The former label-only `@copilot` dispatcher is removed: it could start work
without validating current acceptance, `human-only`, blockers or holds.
The review-feedback workflow receives read-only contents and pull-requests
write permission. It creates only a deduplicated request comment; it does not
approve, merge, bypass checks, or grant issue implementation acceptance.
Copilot's resulting changes still pass ordinary quality gates and human review;
findings that should not be applied must be explained on the pull request.

## Shared issue lifecycle and classification

[`issue-lifecycle.yml`](../.github/workflows/issue-lifecycle.yml) is the sole
issue-stage and admission writer. It consumes the shared Prow/lifecycle conductor
from `projectbluefin/actions@v1`, using the default-branch
[`issue-policy.json`](../.github/issue-policy.json) and constrained
[`prow.yaml`](../.github/prow.yaml), rather than another copy of the bot.
See the [local lifecycle procedure](skills/issue-lifecycle/SKILL.md).

The shared engine preserves deterministic descriptive intake: explicit bug,
feature and documentation shapes seed a kind; quality and guide findings retain
their lane labels; an ACMM criterion needs both its structured title and criterion
field. Existing primary kinds are not overwritten. Classification never grants
acceptance or automatically requests implementation. Unknown or conflicting
classification stays gated for a maintainer instead of being guessed.
The catalog protects `kind/tech-debt` and `source:agent` as unmanaged operational
signals while live operator configuration remains unconfirmed. Their assignments
and definitions are preserved, not used as classification. Ordinary debt uses
non-colliding `kind/debt`, including quiet historical migration.

Prow runs first for authorized issue comments; lifecycle reconciliation follows
even after partial Prow failure. Prow changes descriptive kinds/areas and negative
holds only, never stage acceptance, independent human gates, reviews or merges.
Upstream `/kind` would remove every other `kind/*`, so Prow preflight refuses it
when a protected operational kind is assigned. Use the native Labels picker to
change only managed primary kinds, leaving operational signals/gates unchanged.
Scheduled repair is labels-only. Manual previews expose proposed labels,
comments and reporter requests before applying; migrations remain quiet and
archive full historical assignments before retirement.

Open issues carry one stage: `needs-triage`, `triage/needs-information`,
`triage/accepted`, `awaiting-release`, or `needs-verification`. Current human
acceptance applies to the current scope; classification and assignment are
separate decisions, and independent gates stay in force. PR progress uses
native assignment, reviews, checks and the merge queue, not issue-stage labels.
A merged fix is not delivered until its actual Homebrew package and any
image-installed helpers reach the affected installation; reporters verify the
named version through ordinary replies.

[`issue-policy-preview.yml`](../.github/workflows/issue-policy-preview.yml)
runs for policy-related PRs or manual dispatch with read-only repository
permissions and no secrets. It previews reviewed caller data through the shared
first-party interface; it does not apply labels, dispatch implementation or
activate production. Production remains the managed `@v1` lifecycle caller.

Reusable implementation and review prompts are available in the
[agent prompt catalog](prompts/index.md). They are aids only; repository
instructions and human review remain authoritative.

## Maintainer-triggered Claude review

`.github/workflows/claude-code-review.yml` lets a maintainer request a
read-only Claude review for a pull request:

```bash
gh workflow run claude-code-review.yml \
  --ref main \
  -f pull_request_number=123
```

The workflow runs only when dispatched from the default branch. It checks out
that trusted base branch and reads the selected pull request through
`gh pr view` and `gh pr diff`; it does not check out the pull request head or
execute project code. Claude receives a token that can only read contents and
pull requests, a static prompt that treats all pull request content as
untrusted data, and tools limited to repository and pull request reads.

Claude returns a schema-validated comment body to a separate publishing job.
That job never receives the Anthropic credential or a checkout; its fixed,
commit-pinned script has only pull request comment permission and rejects
malformed or oversized output before posting. Claude therefore never receives
a write-capable token and cannot approve, merge, release, or deploy.

A repository administrator must configure the `ANTHROPIC_API_KEY` Actions
secret before the first review:

```bash
gh secret set ANTHROPIC_API_KEY --repo projectbluefin/chairlift
```

Until the secret exists, a dispatch records a notice and exits without
running Claude. Review comments are advisory evidence only; the ordinary
required checks and human maintainer approval remain authoritative.
