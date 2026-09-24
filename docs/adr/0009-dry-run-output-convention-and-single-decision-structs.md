# 0009 — Gate dry-run behavior through fixed [DRY-RUN] prefixes and single decision structs

- **Status:** Accepted
- **Date:** 2026-08-12
- **Revised:** 2026-09-20 — rule 1 corrected to describe `internal/dryrun`,
  the single-authority design that replaced the per-wrapper
  `SetDryRun`/`IsDryRun` fan-out this rule originally documented (the
  decision itself, and rules 2-3, are unchanged; see Alternatives below).
- **Amended:** 2026-09-24 — the native A/B staging provider and its dry-run
  test were removed (#272); the enforcement list below no longer cites them.

## Context

ChairLift's `--dry-run` flag must guarantee two things at once: no
state-changing command runs, and the UI never *claims* a change happened.
Those are separate failure modes — a wrapper can correctly skip its command
while the view still shows "installed", removes a row, or confirms a switch
flip. When the toast text and the UI mutation are gated by two independent
`if dryRun` conditionals in a GTK page builder, they can drift, and
[ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md) means the
page builder itself is untestable, so the drift would be invisible to CI.

## Decision

Three rules, applied uniformly:

1. **One authority carries the flag.** `internal/dryrun` holds the single
   process-wide preview flag (`dryrun.Set`, `dryrun.Enabled`,
   `internal/dryrun/dryrun.go`). `app.New()` calls `dryrun.Set` once at
   startup and every integration — including `internal/views`, for
   configured custom maintenance scripts — reads `dryrun.Enabled()` rather
   than keeping a flag of its own. The earlier design gave each wrapper its
   own `SetDryRun`/`IsDryRun` and made `app.New()` fan out one setter call
   per package; that was fail-open, because an integration whose setter was
   never registered executed real mutations while the user believed
   `--dry-run` was active. Integrations still skip their state-changing
   commands entirely under preview (e.g. `helperexec.Run` returns before
   ever invoking pkexec, `internal/helperexec/helperexec.go:131-139`).
2. **Dry-run output is prefix-fixed.** Skipped executions log
   `[DRY-RUN] would execute: ...`-style lines, and preview toasts begin with
   `[DRY-RUN] Preview:` and end with `— no changes made`
   (`internal/views/actionmsg/actionmsg.go`), so dry-run output is
   greppable and unmistakable in logs and on screen.
3. **One decision, never two conditionals.** Where a dry-run-aware handler
   would also mutate UI state on success, the toast and the mutation derive
   from a single tested decision struct returned by
   `internal/views/actionmsg`: `ScriptDecision.Execute` (whether a custom
   maintenance script runs at all), `BundleInstallDecision.Complete`,
   `TapTrustDecision.MutateUI`, and `FeatureToggleDecision.Confirm`. The
   view computes `dryrun.Enabled()` exactly once, builds the decision, and
   branches solely on it for both the mutation and the toast; the caller
   must not independently recompute the condition. Actions with no second
   mutation to gate (upgrade, cleanup, Brewfile dump, bootc/sysupdate stage
   toasts, …) use plain string functions instead — the wrapper already made
   and tested the skip decision.

## Consequences

- A table-driven test in `actionmsg_test.go` proves the mutation gate and
  its toast cannot disagree, on a GTK-less host, for every gated action.
- `[DRY-RUN]` is a stable vocabulary: users, docs, and the E2E readiness
  marker ("Running in dry-run mode",
  [ADR-0008](0008-e2e-readiness-is-a-log-marker-contract.md)) can rely on
  it, and rewording a preview toast is a contract change with a test to
  update.
- Adding a new state-changing action requires classifying it: decision
  struct (it also mutates UI) or plain string (it does not). The `actionmsg`
  package documentation records the classification per function.
- Defense in depth is intentional: the updex helper binary also honors
  `--dry-run` even though the wrapper never reaches it in dry-run mode.

## Alternatives considered

- **A single global dry-run flag:** originally rejected — wrappers are
  independently usable and independently tested; each package owning its
  flag kept its tests hermetic, at the cost of one `SetDryRun` call per
  wrapper in `app.New()`. Superseded by rule 1 above (2026-09-20):
  `internal/dryrun` centralizes the flag, and wrapper tests call
  `dryrun.Set`/`dryrun.Enabled()` directly, so hermeticity is unaffected —
  the per-wrapper design's fail-open failure mode (a package whose setter
  was never registered ran for real under `--dry-run`) is what changed the
  calculus.
- **Two conditionals per handler (one for toast, one for mutation):**
  rejected — this is the drift bug the decision structs exist to prevent;
  it shipped real inconsistencies (e.g. dry-run tap trust removing rows)
  before the structs were introduced.
- **Deriving the toast from the mutation result after the fact:** rejected —
  under dry-run there is no mutation result to inspect; the decision has to
  be made once, up front, and shared.

## References

- Shapes: [design/overview.md — Dry-run mode](../design/overview.md#dry-run-mode),
  [design/package-managers.md — View-layer toast and decision helpers](../design/package-managers.md#view-layer-toast-and-decision-helpers-internalviewsactionmsg-internalviewstrustmsg)
- Builds on: [ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md)
- Enforced by: `internal/views/actionmsg/actionmsg_test.go`, the wrapper
  packages' dry-run tests (`internal/homebrew`, `internal/flatpak`,
  `internal/bootc/stage_test.go`, `internal/updex/updex_test.go`), and the wiring tests of
  [ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md)
