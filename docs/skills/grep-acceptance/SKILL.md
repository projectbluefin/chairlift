---
name: grep-acceptance
description: Use when grep-based acceptance criteria involve subtests or cross-chunk names.
version: 1.1.0
last_updated: 2026-10-04
tags:
  - planning
  - grep
metadata:
  type: reference
---

# Grep-based acceptance criteria over test names must account for subtests and for other chunks' own criteria

**When it applies:** Planning or reviewing any chunk whose acceptance
criteria include (a) a grep-based count of `go test -list`/`RUN` lines
expected to change by some exact number, or (b) a `grep -P '\bWORD\b'`-style
"this name must not appear" check — especially in a multi-chunk plan where a
later chunk's acceptance criteria also grep for text containing that same
word (e.g. documenting the convention by naming the pattern in prose).

**What to do:** `go test -list` lists matching top-level tests; it does not run
their bodies or enumerate runtime `t.Run` subtests. Verbose execution (`-v`)
prints parent and subtest RUN lines, so a RUN-line delta can exceed the number
of renamed functions. Decide which output the criterion counts, then compute
the delta from that output (or the concrete subtests), not function names.
For `\b`-anchored greps: GNU grep's `\b` treats an ellipsis character
and most punctuation as a word boundary, so a criterion like
`grep -P '\bTestInstall\b'` returning nothing can directly conflict with a
sibling chunk's requirement that some doc literally contain the string
`TestInstall…` (three dots) as a worked example — both are technically true
readings of "the old name must be gone" vs. "the doc must explain the
mistake," but they cannot both hold over the same tree. When a plan spans
multiple chunks, cross-check every grep-based acceptance criterion against
every other chunk's criteria for the same identifiers before finalizing —
don't verify each chunk's checks only in isolation.

**Learned from:** issue #76's mill run, plan round 2 — round 2 was rejected
twice: once because a RUN-line-count criterion assumed +5 lines for five
renamed tests but two of them had two subtests each (actual delta +9), and
once because c1's `grep -P '\bTestInstall\b'` (must return nothing) directly
contradicted c2's requirement that `AGENTS.md` name the exact accident
`TestInstall…`, since GNU grep's word-boundary matches at the ellipsis.
