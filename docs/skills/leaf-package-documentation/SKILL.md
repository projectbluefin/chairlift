---
name: leaf-package-documentation
description: Use when documentation enumerates outcomes of a leaf package.
version: 1.0.0
last_updated: 2026-10-01
tags:
  - documentation
  - packages
metadata:
  type: reference
---

# Leaf-package doc prose must enumerate every distinct outcome it covers, not summarize with "every"/"all"

**When it applies:** Writing or reviewing the `docs/design/overview.md` /
`docs/design/package-managers.md` (formerly `yeti/`) prose that describes
what a new puregotk-free leaf
package's exported functions decide (following the
`updatepresent`/`featurestatus`/`actionmsg` pattern from
`docs/skills/gtk-headless-testing/SKILL.md`), when that package has more than
one distinct branch or outcome — success/zero/failure, singular/plural,
expandable/not.

**What to do:** Name each outcome and which exported function produces it,
rather than compressing them into a vague summary phrase like "handles every
outcome" or "covers all cases." A reviewer checks this prose against the
package's actual branches and rejects a summary that could be true of a
package that silently drops a case. Also state explicitly, if true, that the
call site composes no subtitle/description text of its own — that claim is
what proves the leaf package, not the view, owns the decision. Write the
sentence as a small enumeration (e.g. "`GroupDescriptionCheckFailed` when the
check itself failed, and `GroupDescription` when it completed with zero
features updatable or with updates found") instead of one abstract adjective.

**Learned from:** issue #67's mill run, chunk 1 review — the
`yeti/OVERVIEW.md` sentence for the new `internal/views/featurestatus`
package said only that `checkFeatureUpdates` applies it "on every outcome,"
without naming the failure/zero/found branches or stating the view composes
no text of its own. The reviewer flagged it (medium) as insufficient; the
sweep fixed it by spelling out each outcome and its producing function.

When a pure model precedes its GTK adapter, document both boundaries. A model
emitting a disposition is not evidence that the dialog persists it, and a choice
referencing a delivered backend is not evidence that the dialog renders its
control. For setup (#224/#225), name the model's zero-step exit, intermediate
navigation, terminal completion, skip and intentional-dismissal outcomes, then
state which callbacks remain owned by the adapter ticket. This prevents model
tests from being reported as desktop interaction coverage.

When consolidating several update surfaces, trace every inventory and badge
writer before preserving the old adapters. A secondary status reader must not
overwrite a coordinator's aggregate with its own subset. Document one live
owner for counts, the preservation rule on failed reads, and which actions
request that owner's refresh; remove obsolete leaf-package catalog entries
alongside the readers they served.
