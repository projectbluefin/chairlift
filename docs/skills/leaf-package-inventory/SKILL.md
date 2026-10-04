---
name: leaf-package-inventory
description: Use when adding a package to an exact documentation enumeration.
version: 1.1.0
last_updated: 2026-10-04
tags:
  - documentation
  - inventory
metadata:
  type: reference
---

# A new puregotk-free leaf package must update its enumeration docs in the same chunk that adds it

**When it applies:** Planning or reviewing any chunk that adds a new
puregotk-free leaf package under `internal/views` (following the
`internal/views/trustmsg`/`internal/views/actionmsg`/`internal/views/rowset`
pattern from [gtk-headless-testing](../gtk-headless-testing/SKILL.md)) — or,
more generally, adds a new member to any collection that
`docs/design/overview.md` or `docs/design/package-managers.md` documents by an exact
count or exhaustive list
("two small, puregotk-free packages...", a diagram enumerating every leaf).

**What to do:** Those docs state an exact enumeration, not just descriptive
prose, so adding a new member makes the existing sentence factually wrong the
moment the new package exists — even if the new package's own subsection is
also added. Update relevant `AGENTS.md` and `docs/` claims in the same chunk;
root `README.md` is human-owned, so report needed corrections without editing it.
A chunk that adds the package but defers the enumeration fix leaves the tree
self-contradictory and will be rejected on plan review. Fix the
stale count/list in the *same* chunk that introduces the new member, not a
later one — put the "does this new thing exist" doc edits in the chunk that
creates it, and reserve any *behavioral* doc edits (how callers now use it)
for whichever chunk actually wires in that behavior, so neither chunk claims a
change that hasn't landed yet.

**Learned from:** issue #66's mill run, plan round 1 — a chunk added
`internal/views/rowset` but left `yeti/package-managers.md`'s "two small,
puregotk-free packages" sentence and `yeti/OVERVIEW.md`'s leaf-package diagram
unchanged until a later chunk; the reviewer rejected it (medium) for leaving
the enumeration stale. Round 2 fixed it by moving the "package exists" doc
edits into the same chunk that adds the package, while keeping call-site
behavior prose in the chunk that wires it in.
