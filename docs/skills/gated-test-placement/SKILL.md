---
name: gated-test-placement
description: Use when adding tests or measuring coverage, and deciding what the gate really runs.
version: 1.0.0
last_updated: 2026-09-18
tags:
  - testing
  - ci
metadata:
  type: reference
---

# Gates only run tests under `internal/...` — tests elsewhere are never enforced

**When it applies:** Adding or reviewing any new `_test.go` file, or a plan
that proposes one, anywhere in this repo — especially under `cmd/` or the
repo root.

**What to do:** Every gate that runs Go tests — `make ci` (the deep gate),
`.github/workflows/test.yml`'s Unit Tests and Race jobs, and `gates_chunk`'s
test line in `.mill/config.json` — scopes `go test` to `./internal/...`
specifically, not `./...`. (Plain `make test`, used only for ad hoc local
runs, does use `./...`, but that target is never what CI or the mill's gates
invoke — passing it locally proves nothing about what actually gets
enforced.) A test file placed in `cmd/chairlift-updex-helper`, `cmd/chairlift`,
or any other package outside `internal/` will build and even show green if
you run it directly with `go test ./...`, but no gate — local chunk gate,
deep gate, or GitHub CI — will ever execute it. It silently protects nothing.

If a spec requires regression coverage for logic that currently lives in a
`cmd/` package (e.g. a helper binary's subcommand dispatch), extract the
decidable logic into an `internal/` package the `cmd/` package calls into,
and put the table-driven test there. This mirrors the fix already required by
the `gtk-headless-tests` skill for puregotk packages — decidable logic
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
in both the `Unit Tests` job — whose run also produces the `coverage.out`
uploaded to Codecov — and the `Race Detection` job. A bare `go test -cover`
counts statements executed by tests no gate runs, so it reports a higher
number about a different artifact; `codecov.yml`'s `project.default`
(`target: auto`, 1% threshold) scores the filtered profile, so an unfiltered
figure cannot predict that status. Establish the baseline the same way —
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
keeps the count at zero, so filtered and unfiltered runs under `internal/`
now select the same tests and report the same number.

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
