---
name: self-referential-assertions
description: Use when writing or reviewing a test whose expected value is computed rather than written down.
version: 1.0.0
last_updated: 2026-09-18
tags:
  - testing
  - review
metadata:
  type: reference
---

# A test whose expected value comes from the code or the host state under test cannot fail, so it defends nothing

**When it applies:** Writing or reviewing any test that computes its `want`
instead of stating it — by calling the same helper the exported function
calls, by re-evaluating the expression the function under test evaluates, by
using a production constant as its own reference, or by reading the same host
file or path the implementation reads. It is most tempting for the thin
entry-point wrappers this repo is full of: `internal/sysupdate`'s
`ReadUpdateCheck`/`GetStatus`, `internal/bootc`'s `StageScriptAvailable` and
`DefaultContext`, and anything whose real input is a fixed absolute path
outside the repository.

**What to do:** The expected value must come from somewhere the
implementation cannot reach: a literal, a fixture written by the test, or a
value pushed in through an injected seam. Apply the mutation question before
accepting the test — *if I gutted this function to return its zero values,
would this test still pass?* If the answer is yes, the test is worthless:
either delete it, or give the production code a seam it can be driven
through. The idiom this repo already uses is an unexported `...From(path)`
variant that takes the path as a parameter, with the exported function
supplying the fixed constant — `internal/sysupdate/status.go`'s
`readUpdateCheckFrom(path)` behind `ReadUpdateCheck()`, and
`readStagedUpdateFrom` behind `ReadStagedUpdate()`. Tests drive the
`...From` variant with a `t.TempDir()` fixture and assert literal fields;
the fixed-path constants are pinned separately against string literals
(`TestStateFilePathsAreTheSnosiContract`).

Host-branched expectations are the subtle form of the same defect. A test
that derives `wantOK` from host state passes identically on every machine
where the feature is absent, so on a CI runner it asserts only the degenerate
"not present" branch and the entire function body could be deleted without a
failure. Pinning a constant against itself is the trivial form: comparing a
computed timeout to `DefaultTimeout` cannot detect a change to
`DefaultTimeout`. Assert the literal `30 * time.Minute`, or do not write the
test.

**Learned from:** a triage session over PRs #134 and #122. In #134,
`internal/sysupdate/readers_test.go`'s `TestExportedReadersOnTheFixedPaths`
compared `ReadUpdateCheck()` against `readUpdateCheckFrom(UpdateCheckPath)`,
which is verbatim that exported function's whole body — `f(x) == f(x)`.
`TestGetStatusComposesBothReaders` compared `GetStatus()`'s fields against
the exact expressions `GetStatus` evaluates; off a snosi host every side is
nil, so a zero `Status{}` passed too. Two rollback tests derived their
expected value from the host's own `/usr/lib/os-release` — the same
`osReleasePath` constant `RollbackVersion` reads — so on any runner they
asserted only `("", false)`. In #122,
`internal/bootc/entrypoints_test.go`'s
`TestStageScriptAvailableTracksTheFixedScriptPath` computed `want` by
re-running `os.Stat(StageScriptPath)`, comparing `false == false` on any
runner without `/usr/libexec/bootc-update-stage`, and
`TestDefaultContextCarriesTheDefaultTimeout` used `DefaultTimeout` as its own
reference. All four were removed before merge; none exists in the tree today.
