---
name: fail-open-tables-need-a-totality-gate
description: Use when a lookup table's default for an unlisted key is permissive.
version: 1.0.0
last_updated: 2026-09-22
tags:
  - testing
  - schema
metadata:
  type: reference
---

# A fail-open lookup table needs a schema totality gate, because exercising it cannot reveal a missing entry

**When it applies:** Adding or reviewing a hand-written table that classifies
the entries of an authoritative collection — `config.SchemaGroups`' page/group
pairs, a page inventory, a command list — where the lookup's answer for a key
the table does *not* list is the permissive one (`true`, "supported",
"allowed"), usually on purpose, because failing closed would make a typo or a
gap silently hide UI. `internal/capability`'s prerequisites table is one:
`Set.Supports` reports an unclassified pair as supported so that a missing
entry cannot hide a group at runtime. `internal/navigation`'s group slices are
the same shape with a different default.

**What to do:** Recognise that the permissive default makes the omission
invisible to every test that exercises the lookup: a key the table forgot and
a key the table deliberately classified as "no requirement" produce the
identical answer, so no unit test over the table's own function can tell them
apart. The mistake then surfaces only where the missing classification
mattered — a host that lacks the backing tool, a target that needs the entry —
which is exactly where the gate does not run. So pair the table with a
set-equality gate against the authoritative collection, comparing **both**
directions (an entry the schema declares but the table omits, and an entry the
table names that the schema does not declare), and assert non-vacuity on both
sides so an empty table cannot pass. Report the two failure shapes — missing
and duplicated — as separate messages, since they need different fixes. Keep
the permissive default; it is the right runtime answer, and the gate is what
carries the completeness obligation instead. When the collection is the page
and group grammar, the gate belongs in `internal/installcheck` beside
`TestNavigationGroupsMatchConfigSchema` and `TestCapabilityPrerequisitesMatchConfigSchema`,
not in the table's own package: it is a cross-package contract, and the table's
package would have to import the owner to state it.

**Learned from:** issue #204's design of `internal/capability`. `Supports`
returns supported for an unclassified pair so a typo cannot hide a group, which
left the prerequisites table's totality over `config.SchemaGroups` provable by
nothing inside the package; `TestCapabilityPrerequisitesMatchConfigSchema` and
`TestEveryConfigurableGroupIsClassifiedOnce` in `internal/installcheck` close
it in both directions. The sibling case was already in the tree:
`internal/navigation` restates the same grammar and is held to it the same way,
and before `navigationschema_test.go` existed the two copies could diverge with
every gate still green.
