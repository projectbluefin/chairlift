---
name: documentation-reconciliation
description: Use when reconciling current documentation, retiring plans, checking links, or citing an ADR.
version: 2.2.0
last_updated: 2026-10-07
tags:
  - documentation
  - consistency
metadata:
  type: reference
---

# Reconcile claims with their live owners

Use for documentation updates and stale-document cleanup. Follow
[`docs/documentation-consistency.md`](../../documentation-consistency.md) for
the source-backed claim checklist; load subsystem design docs only as needed.

## Procedure

1. **Map authority.** Read local `AGENTS.md`, `.knowledge/README.md`, and
   `.memory/README.md`. Root `README.md` is human-owned: report needed changes
   to its maintainer, while updating relevant `AGENTS.md`, `CONFIG.md`, and
   `docs/` claims within the task's ownership.
2. **Trace the claim.** Check schema/defaults in `internal/config` and
   `config.yml`, routes in `internal/navigation`, behavior in the live caller,
   versions in `go.mod`, and install/release claims in `Makefile`,
   `.goreleaser.yaml`, and PolicyKit files. Built-in defaults and shipped
   overrides may differ; name both rather than treating one as the other.
3. **Reconcile every restatement.** Search current docs for the old term,
   symbol, count, default, and fallback. Rewrite contradictory sentences rather
   than adding accurate prose beside them. A test pinning retired prose is not
   authority to retain it: update the source-backed gate with the doc change.
4. **Classify plans against live work.** Compare each plan's unresolved scope
   with source and the current issue/PR, not its filename or checked boxes.
   Keep a plan only when it describes active, unmet scope. An open issue whose
   implementation already landed is not proof the plan remains active; an
   unimplemented active acceptance criterion is not permission to delete it.
   Remove implemented, superseded, or abandoned plans after preserving any
   durable contract in its canonical spec/design/skill. Git retains retired
   artifacts; use a pinned history link only when historical evidence matters.
5. **Preserve decisions.** Resolve ADR filename, subject, status and approved
   amendments/supersessions before citing its number. Accepted decisions remain
   history; bibliographic repairs do not change their policy. Explain other drift
   in living docs and route substantive changes through maintainer acceptance.
   Do not infer a supersession or discard an approved amendment from a stale tree.
6. **Repair discovery.** Update the owning index and every inbound link when
   removing a doc. Current instructions point at live source or canonical
   docs, not retired plans or compatibility aliases. Common is a linked sidecar;
   verify the current default-branch baseline and local source wiring before
   documenting factory adoption. ChairLift's issues and pull requests are
   driven by Prow from `.github/workflows/prow.yml` and `OWNERS`; link Common's
   workflow page rather than restating it.
   Do not infer a generated catalog, frontmatter migration, universal Hive
   scheduling guarantee or direct-push exception from sidecar conventions.

## Verification

For the integration owner, after all documentation edits land:

- Search current docs and tests for removed paths and obsolete claims. Include
  source-path mentions in inline code, not only Markdown links.
- Check local link targets and anchors, router/catalog reachability, canonical
  skill frontmatter, and compatibility alias targets. Example focused gate:
  `go test ./internal/installcheck -run '^TestFactoryDocumentationContract$'`.
- Run `make ci` once. Read `internal/installcheck/documentation_test.go` and
  `docsourcepaths_test.go` when retiring docs so neither gate demands stale
  prose or validates history as if it were live implementation.
- Report reviewed-but-unchanged files, source evidence, unresolved active
  scopes, human-owned README corrections, and immutable ADR discrepancies.

For closed inventories, derive consistency checks from the source owner
(archive contents, helper commands, policy actions, or schema) and cover every
entry. A prose count or a search for one expected string is not totality.

**Learned from:** issue #60's mill run, plan round 1 — the plan added overlay
prose to `CONFIG.md`'s Notes section but left three pre-existing sentences
claiming "all features enabled" applies unconditionally (lines 13, 153-154),
even though `defaultConfig()` ships `maintenance_cleanup_group` disabled. The
reviewer rejected the plan (medium severity) because those exact-checkable
factual errors were left untouched. Round 2 fixed it by widening the chunk to
rewrite all three locations and adding grep-based acceptance criteria.

## Reconcile decision identity before citing a numbered ADR

Resolve a claimed ADR number against the actual `docs/adr/` filename, subject,
and status. Issue prose and even a historical heading can carry a stale number:
ChairLift's capability decision is the `0014-capability-driven-visibility-as-a-floor.md`
file although its accepted text retains a `0013` heading; the `0013` file is
rollback catalog reads, and `0015` is Agent Mode. Link the actual file and
explain the discrepancy in the living architecture documentation. Do not
rewrite accepted decisions, invent a missing ADR, or reuse an occupied number.
Changing an accepted decision requires a new superseding decision and maintainer
acceptance. Keep proposed route placement separate from current UI claims.
