---
name: fixture-path-fidelity
description: Use when a test writes a fixture to the same on-disk path the code under test reads from an external tool's layout.
version: 1.0.0
last_updated: 2026-09-18
tags:
  - testing
  - homebrew
metadata:
  type: reference
---

# A fixture written to the path the implementation reads proves nothing about that path, so a wrong layout stays green forever

**When it applies:** Writing or reviewing a test for code that reads another
tool's on-disk layout — Homebrew's Cellar and Caskroom receipts in
`internal/homebrew/trust.go`, a systemd unit directory, a container quadlet
path, an os-release file. The tell is that the test constructs the fixture
with the same `filepath.Join` components the implementation uses, under a
`t.TempDir()`.

**What to do:** Treat the fixture path as part of the contract under test,
not as scaffolding. It must be justified against the *external tool's* own
source or documentation, never against the implementation you are testing.
Cite that upstream origin in a comment next to the path, the way
`installedCasksByTap` does: its doc comment names
`<prefix>/Caskroom/<token>/.metadata/INSTALL_RECEIPT.json` as "the path
`Cask::Tab.create` writes", because Homebrew's `Cask::Tab.create` sets
`tabfile = cask.metadata_main_container_path/FILENAME` and
`metadata_main_container_path` is `caskroom_path.join(".metadata")`. A test
that agrees with the code only proves the two agree; if both are wrong,
`os.ReadFile` returns ENOENT on every real host, the function returns an
empty map, and the suite never notices.

In review, "the test was updated to match the new code's path" is a red
flag, not a routine diff hunk. When a change moves a fixture to a new
location, ask which upstream fact moved with it; if the answer is only "the
implementation changed", the test has stopped being an independent check.

Separately, an assertion keyed on a value no fixture ever produces is
vacuous. `if _, ok := byTap["stale-org/tap"]; ok { ... }` passes whether the
absence is real behaviour or a total failure to read anything. Assert the
whole expected map with `reflect.DeepEqual`, as `TestCasksGroupedByTap` now
does, so a silently empty result fails; express "this input is skipped" with
a fixture that genuinely exists and is expected to be absent from the
result, like the receipt-less `broke` cask directory.

**Learned from:** PR #107 on `internal/homebrew/trust.go`. The change
correctly replaced a glob over versioned Caskroom metadata with a single
receipt read, but as submitted it read
`<prefix>/Caskroom/<token>/INSTALL_RECEIPT.json`, one level above the real
`.metadata/` container. `TestCasksGroupedByTap` passed only because its
fixtures were written to the same wrong path — and the PR's own base version
of that test already had the correct layout, so the change *moved* the
fixture up a level to follow the code. On any real host every cask would
have been skipped: the reported bug would be unfixed and the
previously-working tap attribution lost as well. The same file's "no
receipt" case was asserted against `byTap["stale-org/tap"]`, a key no
fixture wrote. Both are fixed on `main`: `trust.go:103` joins `.metadata`,
and `trust_test.go` asserts the complete map.
