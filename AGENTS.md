# ChairLift — agent contract

ChairLift ships as **Control Center**, a configuration-driven GTK4/Libadwaita
GUI for Bluefin and other bootc images. It is idiomatic Go using puregotk;
native libraries load at runtime, production builds use `CGO_ENABLED=0`.

Read only source and documentation relevant to the task. Start with the
[architecture map](docs/design/overview.md) when ownership is unclear;
[optional task guides](docs/SKILL.md) cover unusual tooling. Routine fixes do
not require a skill update, knowledge lookup, documentation tour, or new doc.

## Commands and verification

- `make build` builds the GUI and both fixed privileged helpers.
- `make ci` runs host-independent checks, filtered internal unit/race tests,
  and linux/amd64 + linux/arm64 builds. Run before submitting source changes.
- Focused tests: `go test ./internal/<package> -run '^Test[^I]' -skip Integration`.
  Ordinary unit tests must not start with `TestI` or contain `Integration`;
  those names are reserved for the separately invoked `test/e2e` suite.
- Never add tests to packages importing puregotk, directly or transitively:
  their binaries initialize GTK before tests run. Put decidable logic in pure
  `internal/` packages, not `cmd/`, which the unit gate does not run.
- `make e2e`, `make e2e-atspi`, and `make screenshots` use the pinned native
  Dakota container through `test/e2e/dakota.sh`, with private HOME, runtime,
  bus, and headless Mutter Wayland session. Never test against the live
  desktop, `/run/user/<uid>`, host portals, or a generic Debian/Ubuntu image.
- `make blueprints` regenerates committed `internal/ui/*.ui` from `.blp`;
  `make blueprints-check` checks drift. Never edit generated `.ui` by hand.
- Every Make command target must be `.PHONY`.
- Report exact checks and unverified behavior. Do not claim hardware, live
  privileged outcomes, or visual correctness from unit tests or dry runs.

## Non-negotiable implementation boundaries

- Privileged writes use only existing fixed `pkexec` helpers and matching
  PolicyKit actions: `/usr/libexec/bootc-update-stage`,
  `/usr/bin/chairlift-updex-helper`, `/usr/bin/chairlift-helper`.
  No arbitrary commands, configurable privileged paths, passwordless rules,
  image references or usernames in helper argv. Extend the existing helper's
  closed command vocabulary, validate all argv, and cover accepted and rejected
  installed dispatch in E2E. Helpers derive targets and the invoking account.
- `internal/pkexec` owns escalation naming and dismissed-auth classification;
  `internal/helperexec` owns invocation journaling. OS staging goes through
  `internal/bootc` and `internal/stageexec`, never a second upgrade route.
  Reads must not introduce authentication prompts.
- Root must never execute or load from the user-owned Homebrew prefix.
  `internal/dockerbrew` verifies signed metadata and bottle hashes for the
  fixed system Docker helper. Flatpak uses its own authorization, not pkexec;
  new app installs use existing system remotes, never silently add a remote.
- All external tools run off the GTK thread. Marshal widget changes through
  `sgtk.RunOnMainThread`. Nil-guard accesses across independently disabled
  groups. Failed reads, previews, and stale workers must not erase known state.
- `internal/navigation` owns routes, titles, icons, and shortcuts; navigate
  through `Window.navigateToPage`. `internal/capability` owns the non-blocking
  host floor; config may subtract capabilities, never add them. Config errors
  fail closed; only an absent candidate advances the search path.
- `internal/updateflow.Coordinator` alone owns update state and badges;
  `updateproviders` adapts existing operations, `updatepresent` owns copy,
  and the GTK shell renders snapshots. Deployment changes, restart, and cleanup
  share its mutation admission. Reuse action/refresh/row gates rather
  than adding parallel owners. Retryable controls reset their gates.
- Game Mode changes system images only; it never manages gaming Flatpaks.
- All mutations honor `dryrun.Enabled()`. Preview is not live success.
  Destructive actions require confirmation; preserve user-owned files/settings
  and observed package scopes. Package presence is not service readiness.
- Connect GTK signals once at construction, not per refresh. Reuse shared
  `buttonRoute`/`dialogRoute` handlers; purego's callback table is bounded.
  Coalesce streamed output and cap retained rows through `progresslog`/`rowset`.
- Static trees are Blueprint with Go `ui.Bind` tags; connect signals in Go.
  Keep the libadwaita 1.6 floor or supply a version-gated fallback. Object-valued
  constructor properties use native `GoPointer()` and a `uintptr(0)` terminator.
  User-visible product naming comes from `internal/branding.AppName`.
- E2E-only display stubs stay exclusively in
  `internal/app/imageinfo_override_e2e.go` behind `chairlift_e2e`:
  `CHAIRLIFT_IMAGE_INFO`, `CHAIRLIFT_AUTO_UPDATES`, `CHAIRLIFT_GPU_VENDORS`,
  `CHAIRLIFT_CAPABILITIES`. Privileged helpers never trust these overrides.

## Scope, documentation, and delivery

- Keep work scoped. Leave unrelated user changes alone. Root `README.md` is
  human-owned; agents do not edit it.
- Update an existing guide only when the change makes it inaccurate. Prefer
  source links over copied APIs, inventories, dependency versions, or test
  output. Do not append issue histories, session notes, correction logs,
  completed plans, or mandatory learning artifacts. Git and issues hold history.
- Preserve accepted [ADRs](docs/adr/); new decisions supersede old ones.
  [CONFIG.md](CONFIG.md) owns configuration reference;
  [the walkthrough](docs/walkthrough.md) owns the screen tour. User-facing UI
  changes update the relevant scenario and affected screenshots, not every doc.
- Target `main` through a PR and native Prow review/merge queue. Never grant
  yourself acceptance/approval, remove human holds, self-merge, or bypass checks.
  `OWNERS` is generated; do not edit it. See [contribution workflow](CONTRIBUTING.md)
  and Common's [Prow workflow](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md).
- AI commits carry both `Assisted-by: <Model> via GitHub Copilot` and
  `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>`.
- Release only reviewed, clean, CI-green `origin/main`, never a feature branch.
  `make bump` tags stable `vYY.MM.N`, starting at 1 each month. Homebrew ships
  the GUI; fixed helpers/policies arrive separately through the OS image.
- Third-party workflow actions use reviewed full commit SHAs; shared
  `projectbluefin/actions` interfaces use managed `@v1`. Cross-repo writes use
  existing provisioned App credentials, never PATs or new secrets. `ublue-os/*`
  is read-only. Do not copy cross-repository factory policy into local docs.
