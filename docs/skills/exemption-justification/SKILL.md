---
name: exemption-justification
description: Use when adding, keeping, or reviewing an exemption entry in a repository gate.
version: 1.1.0
last_updated: 2026-10-04
tags:
  - testing
  - authorization
metadata:
  type: reference
---

# Verify the reason for every gate exemption

Use when writing or reviewing an allowlist, skip list, or exemption in a
repository-wide gate, especially a claim that another test covers the site.

## Procedure

1. Open the named test and trace its actual input set. A string hit in a
   plausible test is not evidence that it scans the exempted file or asserts
   the relevant property. `TestPageBuildersUsePurePresentations` in
   `internal/views/pageview/wiring_test.go` reads its listed view sources;
   that is not a scan of `internal/views/pageview/pageview.go` itself.
2. Prefer adopting the canonical owner and deleting the exemption over
   maintaining a reason for a second owner. The live escalation gate,
   `internal/installcheck/pkexecowner_test.go`, scans non-test Go under
   `internal/` and `cmd/`; only `internal/pkexec/pkexec.go` may spell the
   program literal. It has no per-call-site exemptions.
3. If an exemption must remain, state the concrete constraint and verify the
   reason against source. Name security-sensitive sites explicitly rather
   than burying their privilege relevance behind a generic comment.

For narrowly authorized allowlist edits, also read
[frozen-allowlists](../frozen-allowlists/SKILL.md).

## Verification

Read the exempting gate and purported replacement check together, then have
the integration owner run the relevant gate. For escalation ownership:
`go test ./internal/installcheck -run '^TestPkexecCommandHasOneOwner$'`.
Ensure a negative fixture at the exempted site fails the claimed check.

**Learned from:** PR #50's initial review proposed an exemption for
`pageview.MaintenanceCommand` because a sibling wiring test allegedly asserted
on that call site's source text. It did not: the cited retired-fragment check
looked for absence in the Maintenance view instead. The revision used
`pkexec.Command` at the caller and removed the exemption, leaving the map and
its staleness machinery unnecessary. This is historical review evidence, not
a claim that the pull request remains open or that the owner package is absent.
