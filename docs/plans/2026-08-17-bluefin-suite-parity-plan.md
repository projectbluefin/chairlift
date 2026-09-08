# Plan: Bluefin suite parity

Brings the functionality of [bluefinctl](https://github.com/projectbluefin/bluefinctl)
(Textual TUI) and [finupdate](https://github.com/tuna-os/finupdate) (GTK4/Rust)
into ChairLift, so that one GTK4/Libadwaita application covers system
management on Bluefin, Bluefin LTS, and Dakota as well as on Snow Linux.

Two constraints shape every phase and are not negotiable per-item:

- **GNOME HIG, simple surface.** The goal is the *capability*, not the source
  application's control count. Where bluefinctl or finupdate expose a
  knob-per-behavior, ChairLift exposes the smallest control that gets the job
  done. The reference app is GNOME Software — hero status, list of pending
  changes, drill-down for detail — as identified by finupdate's own
  [HIG audit](https://github.com/tuna-os/finupdate/blob/main/docs/GNOME-HIG-AUDIT.md).
  A ported feature that arrives as six switches has not been ported correctly.
- **Every phase is proven by automated checks, and documented visually.**
  `make ci` for logic, `make e2e` for the installed boundary, and the
  screenshot walkthrough for the rendered surface. Each phase must also add
  its feature to [docs/walkthrough.md](../walkthrough.md) with a screenshot
  from `make screenshots`; `make ci` fails when a page or named feature is
  missing from it. Per
  [acceptance-criteria-need-automated-checks-not-inspection](../agents/skills/acceptance-criteria-need-automated-checks-not-inspection.md),
  "verified by inspection" does not close a phase.

## Feature matrix

Source of each capability, and where it stands. "Already" means ChairLift
covered it before this plan.

| Capability | bluefinctl | finupdate | Status |
| --- | :---: | :---: | --- |
| OS image staging with live progress + log | ✅ | ✅ | Already (`internal/bootc`, `internal/stageexec`) |
| Native A/B (systemd-sysupdate) staging | — | — | Already (`internal/sysupdate`) |
| Flatpak list / update / uninstall | ✅ | ✅ | Already (`internal/flatpak`) |
| Homebrew install / search / upgrade / pin / bundles | ✅ | ✅ | Already (`internal/homebrew`) |
| Homebrew tap trust | — | — | Already (`internal/homebrew/trust.go`) |
| System info, os-release, health | ✅ | — | Already (`system_page.go`) |
| Updex features | — | — | Already (`internal/updex`) |
| Testing-channel switch | ✅ | ✅ | **Done** — `internal/imageinfo`, `internal/ublue` |
| Developer mode (dx groups) | ✅ | — | **Done** — `internal/ubluehelper` |
| Gaming mode | — | — | **Done** — `internal/gaming` (new; no upstream) |
| Per-image release-channel table + override | — | partial | **Done** — `channels.yml` |
| **Update All** (OS + Flatpak + Brew, one action) | ✅ `bctl update` | ✅ hero button | **Done** — `internal/updateall` |
| Restart to apply / reboot prompt | ✅ | ✅ | **Done** — `restart` helper subcommand |
| Rollback to previous deployment | ✅ calendar | ✅ | **Done** — `rollback` helper subcommand |
| Automatic background updates (uupd timer) | ✅ | ✅ | **Done** — `internal/autoupdate`, one switch |
| Image variant selection (`-nvidia`) | — | ✅ rebase dialog | **Done** — `internal/imageinfo` driver table, one hardware-driven offer |
| Pin to dated tag / unpin to stream | — | ✅ | **Later** — needs a live registry tag listing in the GUI; see Later/ideas |
| GPU detection | ✅ | ✅ | **Done** — `internal/gpu`, PCI vendor IDs from sysfs |
| Action journal of privileged operations | — | ✅ | **Done** — `internal/journal`, wired into both privileged dispatch points |
| Desktop notifications | ✅ `notify-send` | ✅ `GNotification` | **Done** — `internal/notify`, Update All completion only |
| Update strategy / focus mode / per-layer switches | ✅ | — | **Not porting** — the option sprawl the HIG constraint rules out. Phase 3's single switch replaces it. |
| Reboot-on-logout / scheduled reboot window | ✅ | ✅ "Restart Tonight" | **Deferred** — needs a systemd user unit contract; revisit after Phase 3 |
| Powerwash / Factory Reset | — | ✅ | **Done** — `internal/powerwash`, `factory-reset` helper action, both opt-in and confirmed |
| AI / GPU container stacks | ✅ | — | **Approved 2026-08-17** — Phase 7. Must arrive as one "AI stack" choice per detected GPU, not a 30-entry catalog |
| Changelog / SBOM diff between images | — | ✅ | **Approved 2026-08-17** — Phase 8. Needs an offline-testable SPDX parser seam; the network call cannot be in the gated tests |
| D-Bus progress publishing | — | ✅ | **Not porting** — exists to feed finupdate's GNOME Shell extension; no consumer here |
| Dev tools installer (docker, Lima/WSL, editors, VMs) | ✅ | — | **Not porting as recipes** — ChairLift already has Brew search/install, Flatpak install, and configurable `brew_bundles_group` `bundles_paths`. The curated list belongs in a shipped bundle file, not ~500 lines of per-tool install code. |
| GNOME Control Center panel | — | ✅ | **Not porting** — finupdate ships a C panel + patches against gnome-control-center; out of scope for a standalone app |

## Phase 1 — Update All (medium) — ✅ landed

One button that brings the whole system up to date, matching both tools'
headline feature, with a single restart prompt at the end.

Composition, not new privilege: the OS phase calls the existing
`internal/bootc` staging path (`pkexec /usr/libexec/bootc-update-stage`,
action `io.projectbluefin.chairlift.bootc.stage`), because
[AGENTS.md](../../AGENTS.md) fixes both "OS staging execution has one owner"
and the system-integration package's fixed-path contract. Flatpak and Homebrew
phases are already unprivileged. The only new privileged surface is
`systemctl reboot`.

- New pure `internal/updateall` sequencing the phases and owning the
  aggregate progress/outcome contract, with the per-provider work delegated
  to `internal/bootc`, `internal/flatpak`, and `internal/homebrew`.
- New `restart` subcommand on the existing `chairlift-ublue-helper`, with a
  matching action in `data/io.projectbluefin.chairlift.ublue.policy` — not a
  third helper binary.
- One `updates_page` group: a single primary action plus per-phase status.
- **Done when:** the walkthrough captures the Updates page showing the Update
  All group with a non-empty plan, and `make ci` + `make e2e` are green.
  **Met.** The walkthrough asserts the `views: update all group built` marker
  names a non-zero phase count, and captures the rendered group. The run's
  own behavior — ordering, failure isolation, cancellation, and the restart
  decision — is covered by `internal/updateall`'s table tests rather than by
  screenshot, because a dry-run run completes faster than a frame can be
  captured and racing it would make the check flaky.

## Phase 2 — Rollback (small) — ✅ landed

- `bootc rollback` via a new `chairlift-ublue-helper` subcommand + action.
- Presented as a single row naming the deployment being returned to, mirroring
  `pageview.SysupdateRollbackSubtitle`'s existing grammar. Not bluefinctl's
  rollback calendar.
- **Done when:** the walkthrough captures the rollback row naming the previous
  deployment on a host with one, and its absence on a host without.
  **Met** for the absent case, which is what the CI runner is: the row stays
  hidden and the walkthrough's Updates frame shows the System Updates group
  without it. The present case is covered by `pageview`'s table test over the
  version/timestamp matrix and by the boundary test asserting the helper
  rejects any caller-supplied rollback target.

## Phase 3 — Automatic background updates (small) — ✅ landed

- One switch: on/off, backed by `systemctl enable --now uupd.timer` /
  `disable` through a new helper subcommand + action.
- Hidden entirely when `uupd` is absent, the same availability pattern as
  `updex` and the Bluefin-family groups.
- Rendered inside `update_all_group` and sharing its config key, so disabling
  that group hides the switch too. A separate key could be enabled while
  `update_all_group` was disabled, leaving the page claiming a group it does
  not draw.
- **Done when:** the walkthrough captures the switch reflecting real timer
  state, and the switch is absent on a host without `uupd`. **Met.** The
  runner has no `uupd.timer`, so the walkthrough exercises both: unstubbed it
  logs `automatic updates unavailable` and shows no switch, and with the
  `chairlift_e2e`-tagged probe stub it captures the switch in its on state.
  Every systemd state `Classify` can see is covered by its own table test.

## Phase 4 — Image variants (medium) — ✅ landed

- Extend `internal/imageinfo`'s per-image table with variant suffixes
  (`-nvidia`, `-nvidia-open`, `-dx`), keeping the same discipline: **verify
  against the registry before mapping to a tag**. finupdate's `KNOWN_FAMILIES`
  was verified 2026-05-30 and records that `dakota-dx` does not exist; treat
  it as a starting hypothesis, not as fact.
- GPU detection to decide which variant to offer.
- Pin to a dated tag / unpin to the stream.
- **Done when:** a table test covers the variant matrix for all three images
  against registry-verified references, and the walkthrough captures the
  variant row. **Met.** `internal/imageinfo`'s driver table records the
  manifest responses per image *and stream*, `internal/gpu` covers the
  hardware matrix including hybrid laptops, and the walkthrough captures the
  Graphics Driver row offering a switch on a stubbed NVIDIA machine.
- Ownership recorded in
  [ADR-0011](../adr/0011-chairlift-owns-bluefin-family-rebasing.md).

## Phase 5 — Audit and notification (small) — ✅ landed

- `internal/journal` records every privileged dispatch — dry-run or live —
  from the one choke point both helpers already share (`runHelper`), mirroring
  finupdate's `action_journal.rs`. Disabled (a couple of atomic loads) unless
  `$CHAIRLIFT_ACTION_JOURNAL` is set, so it ships in the released binary
  rather than being a `chairlift_e2e` stub.
- `internal/notify` sends exactly one desktop notification: Update All's
  completion. Every other action already completes in view with a toast; a
  second notification for the same instant event would be noise, which is
  what the simple-interface constraint rules out.
- **Done when:** a unit test proves the journal captures the exact argv a
  privileged action would run, without granting privilege. **Met** —
  `TestRunHelperJournalsEveryInvocation` and
  `TestRunHelperJournalsDryRunAsSuppressed` in `internal/ublue`.

## Phase 6 — Powerwash and Factory Reset (medium) — ✅ landed

Approved 2026-08-17. Both are irreversible, which sets the bar for how they
are presented and tested.

- Powerwash: `flatpak uninstall --user --all` (via new
  `internal/flatpak.RemoveAllUser`) plus `distrobox rm --all --force` (via the
  new minimal `internal/distrobox` package). No privilege required — it is
  all user-scope, like gaming mode. Sequenced by the pure
  `internal/powerwash.Runner`, in the same shape as `internal/updateall`.
- Factory Reset: `bootc install reset --experimental --apply` through the new
  `factory-reset` action on the existing `chairlift-ublue-helper`. The
  `--experimental` flag is surfaced in the confirmation dialog's body, not
  hidden, and `pageview.TestFactoryResetConfirmationNamesExperimental` holds
  it there.
- Both behind an `AdwAlertDialog` with a destructive-styled confirm, per the
  HIG's rule that destructive dialogs are reserved for non-undoable actions.
  Both live in a new `reset_group` (`maintenance_page`) that ships
  `enabled: false` by default, the same as `maintenance_cleanup_group`.
- **Done when:** the walkthrough captures both confirmation dialogs, and the
  e2e boundary test asserts the helper rejects a factory reset invocation
  carrying any argument. **Partially met** — the boundary test is in place
  (`ublue_factory_reset_with_a_flag`, `ublue_factory_reset_with_extra_argument`
  in `test/e2e/e2e_test.go`), and `reset_group` ships disabled by default, so
  the walkthrough correctly does not capture it: driving a confirm dialog to
  a screenshot would mean clicking through a real destructive action inside
  the capture harness, which the walkthrough deliberately avoids doing for
  any mutation (see Phase 1's note on why the Updating state is not
  screenshotted). The dialog's exact text is table-tested in `pageview`
  instead, which is the assertion that actually matters: that the
  `--experimental` disclosure survives.

## Phase 7 — AI stacks (medium)

Approved 2026-08-17, with the constraint that it arrives as one choice per
detected GPU rather than bluefinctl's full quadlet catalog.

- GPU detection selects the stack; the user sees a single switch, not a
  vendor/stack matrix.
- Quadlet units installed user-scope under `~/.config/containers/systemd/`.
- **Done when:** a table test covers stack selection for Nvidia, AMD, Intel,
  and no-GPU hosts, and the walkthrough captures the row on a stubbed GPU.

✅ landed. Met. `internal/aistack` selects one RamaLama image per detected
accelerator — all four cases plus the hybrid laptop are covered by
`TestSelectCoversEveryHardwareCase` — and the Features screenshot shows the
row resolving to the CUDA stack from the walkthrough's stubbed Intel+NVIDIA
hybrid, asserted by the `views: ai stack group built` marker.

The catalog reduction went further than planned: rather than one stack per
vendor directory, there is one runtime for every host. bluefinctl has no
Intel or CPU stack at all, so a per-vendor port would have left those hosts
with an empty page.

Site overrides were added in the same phase, on request: `ai_images` and
`ai_model` in `config.yml` pin the image and model, and the graphics-driver
variant table gained a `drivers:` section in the root-only `channels.yml`
(the driver switch is resolved by the privileged helper, so it cannot take
configuration from a user-writable path).

## Phase 8 — Changelog / SBOM diff (large)

Approved 2026-08-17.

- SPDX parsing and package diffing behind a pure package with the network
  fetch as a seam, so the gated tests never make a request.
- Presented as a drill-down from the staged-update row, not as a top-level
  page.
- **Done when:** the diff is table-tested against checked-in SPDX fixtures,
  and the walkthrough captures the drill-down.

✅ landed. Partially met, and the shortfall is the screenshot.

Met: `internal/sbom` parses both SBOM shapes, diffs them into
upgraded/downgraded/added/removed plus an explicit "order unknown" bucket,
and is table-tested against checked-in fixtures in both Syft and SPDX form.
The registry fetch sits behind the `FetchFunc` seam and is separately covered
against a loopback `httptest` registry, which is what pins the two behaviors
found while building it: GHCR 404s the referrers API for these images (the
fallback tag is load-bearing, not a nicety), and what it serves under the
SPDX artifact type is Syft JSON.

Not met: the walkthrough does not capture the drill-down. It lives inside
`bootc_updates_group`, which is hidden unless the host is booted from a bootc
deployment *and* ships the stage script — the capture runner is neither, so
the whole System Updates group is absent from `3-updates.png`. Capturing it
would need a fourth `chairlift_e2e` stub standing in for `bootc status`. The
stub-surface invariant would permit it — the stage helper is built without
the tag and resolves its own state, so a GUI-side status stub stays
display-only — but the surface is capped deliberately, and growing it on the
staging package to gain one frame is not a trade worth making. (Contrast
`reset_group`, which the capture enables through ordinary configuration
rather than a stub, and which is therefore in the frame.) The row's text and every diff category are covered by
`pageview.ChangelogSections`/`ChangelogSummary` table tests instead, and
`docs/walkthrough.md` says plainly why the group is missing from the frame.

## Later / ideas

- Reboot-on-logout and scheduled reboot windows, if the systemd user unit
  contract can be made as simple as one switch.
- A shipped Brew bundle expressing bluefinctl's curated developer tool list,
  replacing its per-tool install recipes.
- **Pin to a dated tag / unpin to the stream.** finupdate offers this from its
  rebase dialog. It needs a live `/v2/<repo>/tags/list` call to populate the
  dated tags, which puts a network round-trip behind a GUI control and cannot
  be covered by the gated tests without a fixture server. Worth doing, but as
  its own phase with a recorded decision about where the network call lives.

## Open questions

- ~~**Powerwash, Factory Reset, AI/GPU stacks, SBOM changelog:** port or
  drop?~~ **Resolved 2026-08-17:** all three approved. They are Phases 6–8.
- ~~**Is ChairLift the intended home for Bluefin-family system management, or
  is finupdate?**~~ **Resolved 2026-08-17:** ChairLift owns rebasing. Recorded
  as [ADR-0011](../adr/0011-chairlift-owns-bluefin-family-rebasing.md).

## References

- Implements: [design/overview.md](../design/overview.md)
- Sources: [bluefinctl](https://github.com/projectbluefin/bluefinctl),
  [finupdate](https://github.com/tuna-os/finupdate)
