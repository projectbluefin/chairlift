---
name: removal-verification
description: Use when proving a removed identifier is absent with repository-wide grep.
version: 1.1.0
last_updated: 2026-10-04
tags:
  - verification
  - removal
metadata:
  type: reference
---

# Scope absence checks to the contract being removed

Use when proving a retired identifier, key, flag, or document is no longer part
of the shipped surface or current instructions.

## Procedure

1. State the search scope: shipped source, configuration, current docs, or
   executable tests. Exclude Git internals and historical/generated task
   artifacts from a current-behavior assertion. Do not assume a retired tool's
   artifact directory is still part of the live workflow.
2. Search every live caller and restatement, including inline source-path
   mentions and indexes when deleting a document. A retained historical lesson
   may name the removed thing without making it a supported current feature.
3. Check regression evidence separately. When the acceptance criterion demands
   literal absence even from tests, assert the exact surviving set rather than
   spelling the removed member. This also catches a reintroduction under a
   different name. Otherwise let an explicit rejection test name the old input
   and exclude that intentional evidence from the shipped-surface search.

## Verification

Report the search's actual paths and exclusions, then have the integration
owner run the affected regression gates. For doc deletion, follow
[documentation-reconciliation](../documentation-reconciliation/SKILL.md) and
validate inbound links, source-path mentions, and catalog entries. A zero-hit
search is useful only when it covers the intended surface.

**Learned from:** issue #58's mill run — plan round 1 was rejected because
its grep-based removal criterion walked `.mill/spec.md`/`plan.md`. Round 2's
fix (an explicit path allowlist) was itself rejected because the allowlist
still included `internal/` wholesale, which the same chunk's new
`config_test.go` also lives under and was written to name the removed key.
Round 3 converged only after fixing both at once: excluding `.mill/`
structurally instead of allowlisting, and rewriting the test as an exact-set
assertion that never spells the removed key's name.
