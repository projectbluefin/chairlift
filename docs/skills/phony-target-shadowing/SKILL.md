---
name: phony-target-shadowing
description: Use when adding or auditing a Make target whose name could collide with a directory in this repository.
version: 1.0.0
last_updated: 2026-09-18
tags:
  - build
  - ci
metadata:
  type: reference
---

# A Make target missing from `.PHONY` that shares its name with a directory runs nothing and still exits 0

**When it applies:** Adding a target to `Makefile`, reviewing a change that
adds one, or running a documented build/gate command — `make test`, `make
fmt`, `make lint` — and reading its output. It applies with full force to any
target name the repository already has a directory for: `test/` (which holds
`test/e2e/`), `docs/`, and `build/`.

**What to do:** Put every target that does not produce a file of its own name
into a `.PHONY` list. No target in this `Makefile` produces a file of its own
name, so the inventory at the top of the file must cover all of them; targets
added later in the file (`screenshots`, `ci`) instead declare their own
`.PHONY:` line immediately above themselves. Follow whichever of those two
shapes is local to the target you are adding; do not add a target without
one.

Treat a missing `.PHONY` entry as a defect, not a style nit, whenever the
target name could collide with a real directory. The `test` target is the
proof: `test/` exists (it holds `test/e2e/`), so while `test` was undeclared
make considered the target already satisfied, printed `make: 'test' is up to
date.`, and ran no `go test` at all. `fmt` and `lint` were equally
unprotected and worked only by the accident that no `fmt/` or `lint/`
directory exists. `make -d -n <target>` is the way to confirm a target really
runs: it prints either `update target 'test' due to: target is .PHONY` or a
decision that the existing file already satisfies it.

The second half of the rule is about reading gate output. `make test` exited
0 the entire time it was doing nothing, so an exit-status check could never
have caught it. When a gate command's output does not look like the work the
command claims to do — no test lines, no package names, just `up to date` —
read the output rather than trusting the status code. `AGENTS.md`'s build and
test section documents `make test` as "`go test ./...`", which is what the
recipe says and what the target now actually does.

**Learned from:** a 2026-09-18 PR-triage session that ran `make test` as a
post-merge gate and got `'test' is up to date` instead of a test run. The
original `.PHONY` line listed `all build build-e2e run clean deps tidy
install uninstall e2e` and omitted `test`, `fmt`, and `lint` entirely. Note
that `make ci` was never affected: it has its own `.PHONY: ci` and runs the
enforced `./internal/...` tests directly, which is why the no-op survived
unnoticed.
