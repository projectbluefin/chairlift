---
name: factory-onboarding
description: Use when submitting through ChairLift's merge queue or publishing a stable release.
---

# Factory and release operations

Follow [CONTRIBUTING.md](../../../CONTRIBUTING.md) and Common's
[Prow workflow](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md).
Check the live issue/PR, repository ruleset and assignment before changing
state. Hive assignment is not maintainer acceptance. Never self-approve,
remove human holds, bypass review, or treat credentials as merge authority.
Do not enqueue without an explicit current instruction from the session owner.

## Merge queue

Prow uses `lgtm`, `approved`, and absence of `hold`; approvers come from
`OWNERS`. `gh pr merge` enqueues rather than merges. The `merge_group` run
revalidates the candidate against current `main`; the required aggregate is
**Tests Passed**. A green PR run is not evidence that the queue candidate passed.

Queued PRs may remain `OPEN` with `autoMergeRequest=null`. Inspect the actual
queue rather than inferring membership from those fields. To remove an entry,
use GraphQL `dequeuePullRequest(input: {pullRequestId: ...})`, not
`gh pr merge --disable-auto` (that cancels auto-merge, not queue membership).
Once merged, an entry cannot be dequeued; settle gates before enqueueing.

Automation-created PRs use the existing MergeRaptor App credentials;
`GITHUB_TOKEN` writes do not trigger the checks required for admission.
Do not add PATs or create credentials to work around an authority gap.

## Stable release cutover

1. Wait for human-approved work to complete the queue. Fetch `origin/main`;
   verify a clean checkout and green CI for that exact head. Never tag a feature
   branch, retag a published release, or release to conceal failed checks.
2. `./scripts/next-version.sh` inspects the next stable `vYY.MM.N` tag without
   creating it. `N` starts at 1 each month; prereleases do not advance it.
   `make bump` creates the stable tag. There is no prerelease mode.
3. Validate publication config with `goreleaser check`. The existing tag
   workflow owns builds, SBOMs, signatures and publication; do not substitute
   locally uploaded archives.
4. Observe release workflow success and the published non-draft, non-prerelease
   assets for both architectures. Homebrew GUI publication does not deliver
   image-installed helpers/policies; those ship through the OS image.

The release workflow runs AT-SPI serially, unlike PR shards. A timeout without
a failing-scenario summary may be the suite cap; compare `atspiTimeout` and the
workflow timeout before diagnosing a flake.
