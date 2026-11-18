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

The release workflow runs the same AT-SPI shards as pull requests and waits for
every shard before publishing. A timeout without a failing-scenario summary may
be the suite cap; compare `ATSPI_TIMEOUT` and the workflow timeout before
diagnosing a flake.

5. Propagate to the Homebrew tap. GoReleaser publishes only the GitHub
   release; `projectbluefin/homebrew-tap`'s `autobump.yml` runs
   `brew bump --casks --open-pr` on a daily 00:15 UTC cron or on
   `workflow_dispatch`. To deliver immediately:
   `gh workflow run autobump.yml --repo projectbluefin/homebrew-tap`.
   Mergeraptor opens the bump PR, `brew test-bot` validates it, and
   `auto-merge-bumps.yml` squash-merges it when the diff touches only
   version/sha256 lines and no hold label is present.
6. Verify user reach, not just publication. The cask at the tap's `main`
   head must carry the new version with sha256 values equal to the
   release's signed `checksums.txt`;
   `brew info --cask projectbluefin/tap/chairlift` must report old → new,
   and `brew fetch --cask projectbluefin/tap/chairlift` must download and
   checksum-verify the artifact. `cosign verify-blob` with the identity
   regexp from the release header proves the archives came from the
   tagged workflow.

Traps observed in practice: the bare cask token `chairlift` is ambiguous
across frostyard/tap, ublue-os/tap, and projectbluefin/tap — always fully
qualify it. `git fetch origin --tags` exits 1 when remote `dev` or
historical alpha tags would clobber local ones; main and stable tags still
fetch. The binary has no `--version` flag; verify installs through Caskroom
and brew state, and read the calendar version in About. When the primary
checkout carries unrelated work, run `make bump` from a disposable
`git worktree add --detach <dir> origin/main`; its HEAD and clean-tree
checks pass there without disturbing the work in progress.

**Learned from:** the 2026-11-18 v26.11.8 release, propagated by dispatching
the tap autobump and verified through cosign, brew fetch, and a live cask
upgrade.
