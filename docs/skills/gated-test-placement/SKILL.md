---
name: gated-test-placement
description: Use when adding tests or measuring coverage, and deciding what the gate really runs.
version: 1.1.0
last_updated: 2026-10-04
tags:
  - testing
  - ci
metadata:
  type: reference
---

# Unit gates run internal tests; E2E coverage has a separate gate

**When it applies:** Adding or reviewing any new `_test.go` file, or a plan
that proposes one, anywhere in this repo — especially under `cmd/` or the
repo root.

**What to do:** `make ci` and the workflow's unit/race jobs scope Go tests to
`./internal/...`, not `./...`. Plain `make test` is a local convenience target
over `./...`; passing it does not prove CI selects the new test. Put decidable
regressions in a pure `internal/` package. A test under `cmd/` or the root is
not selected by the unit gate.

The deliberate exception is `test/e2e/`, explicitly run by `make e2e` and
`make e2e-atspi` on Dakota. Installed helper dispatch coverage belongs there
when it must drive the shipped executable: `test/e2e/helper_commands_test.go`
checks accepted commands as well as the boundary's rejected argv. Verify a
dedicated workflow invokes the target before calling any outside-internal test
enforced. See [gtk-headless-testing](../gtk-headless-testing/SKILL.md).

If regression coverage targets decidable logic in a `cmd/` package (for example,
helper argv parsing), extract it into an `internal/` package the executable
calls, and put the table-driven test there. Test the executable's dispatch
separately through its installed E2E surface. This mirrors
[gtk-headless-testing](../gtk-headless-testing/SKILL.md): decidable logic
belongs in a small, dependency-light `internal/` package regardless of which
constraint (puregotk dlopen, or gate test scope) is pushing it out of the
package where it's easiest to write.

**The same filter decides what a coverage number means.** Measure with the
gate's own command:

```
go test ./internal/<pkg>/ -run "^Test[^I]" -skip "Integration" -cover
```

That is the filter `Makefile`'s `ci` target applies to both its `unit tests`
and `race detector` steps, and the one `.github/workflows/test.yml` applies
in both the `Unit Tests` job and the `Race Detection` job. A bare `go test -cover`
counts statements executed by tests no gate runs, so it reports a higher
number about a different artifact. Establish the baseline the same way —
remove the new test file, re-run the identical filtered command, restore it,
run it again — rather than quoting a remembered or previously-reported
"before". A coverage claim is a factual claim about a gate's output and
carries the same evidence standard as any other claim in a PR body. The two
failures compound: an excluded test both protects nothing and inflates the
unfiltered number used to argue it does. That gap was measurable in this tree
until 2026-09-18: the since-removed A/B-partition update package reported 83.2%
filtered against 87.4% unfiltered, and `internal/distrobox` 83.3% against
91.7%, because three of that package's `TestIsNativeAB*` tests and
`TestIsInstalledReflectsPATH` all began `TestI`. Those nine excluded tests —
across `internal/distrobox`, `internal/gaming`, that A/B-partition package,
`internal/version`, and `internal/installcheck` — have since been renamed, and
`internal/installcheck`'s `TestNoInternalTestNameIsExcludedByTheCIFilter`
keeps reserved names out of `internal/`. Continue measuring with the gate's
filter rather than assuming an unfiltered local run is equivalent.

**Learned from:** issue #56's mill run, plan round 2 — a plan added
`cmd/chairlift-updex-helper/main_test.go` for helper subcommand coverage; the
reviewer objected that no gate exercises tests outside `./internal/...`. The
revised plan moved the logic into `internal/updexhelper` instead, which is
covered by `make ci` and CI.

The measurement half comes from a 2026-09-18 triage batch of three
agent-authored coverage PRs (#122, #125, #134), each reporting a delta from a
bare `go test -cover`. Every one overstated it, and two additionally shipped
tests named `TestIsBootcBooted*`, `TestIsInstalledEndToEnd`, and
`TestIsInstalledCachedRunsOnce` — covering the very functions the PR bodies
headlined — which the filter excluded outright. Re-measured with the gated
command after trimming: `internal/bootc` 68.2% -> 79.5% against a claimed
76.8% -> 95.5%; `internal/stageexec` 78.4% -> 80.4% against a claimed
73.0% -> 82.5%; the since-removed A/B-partition package 69.7% -> 83.2% against a
claimed 73.9% -> 95.8%; `internal/homebrew` 73.2% -> 85.5%, the one claim that
survived measurement roughly intact.
