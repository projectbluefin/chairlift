# AGENTS

ChairLift is a GTK4/Libadwaita system-management GUI for
[Bluefin](https://projectbluefin.io) and other bootc images, written in idiomatic Go using
[puregotk](https://codeberg.org/puregotk/puregotk) bindings — **no CGO**. GTK,
Libadwaita, and GLib shared libraries are loaded at runtime via `dlopen`. The UI
is YAML-configuration-driven; feature groups toggle on and off per host.

## Build, test, lint

The app builds pure-Go (`CGO_ENABLED=0`); the race detector needs CGO.

- `make build` — builds `build/chairlift`, `build/chairlift-updex-helper`, and
  `build/chairlift-ublue-helper` (all `CGO_ENABLED=0`).
- `make test` — `go test ./...`. Every target in the Makefile is a command
  that produces no file of its own name, so every one must be declared
  `.PHONY`. This is not a style nit: the repository has a `test/` directory,
  and while the `test` target was undeclared make considered it already
  satisfied — `make test` printed `'test' is up to date` and ran nothing,
  exiting 0. `internal/installcheck`'s
  `TestMakefilePhonyCoversEveryTarget` now holds the full target inventory in
  both directions, so adding a target without declaring it fails CI.
- `make fmt` — `gofmt -s -w .`.
- `make lint` — `golangci-lint run`.
- `make ci` — runs every **host-independent** CI gate, in CI's order (go.mod
  tidy check, `go vet`, gofmt check, lint, unit tests, race detector, build).
  The build step reproduces CI's `linux/amd64` + `linux/arm64` matrix into
  `build/ci-linux-<arch>/`, then rebuilds natively, so a cross-arch-only
  compile failure cannot pass locally and break CI. Run it before pushing;
  the mill's deep gate calls this exact target. Codecov's remote project status
  additionally rejects coverage regressions greater than one percentage point;
  it has no fixed coverage target and cannot be mirrored locally.
- `make e2e` — builds both executables, checks the application's real
  `--help` surface, starts the dry-run GTK window under a private D-Bus/Xvfb
  session, stages `make install`, and executes the installed privileged
  helper's rejection paths. Startup polls the three readiness log markers for
  up to 30 seconds, requires one additional second of process stability, and
  terminates the private process group as soon as the smoke check passes.
  Terminating it is not the end of the story: startup's Homebrew readers are
  grandchildren, and the Homebrew runner starts them in new process groups.
  The smoke test launches a private **session** (`Setsid: true`), scans `/proc`
  for surviving members of that session even when their process-group IDs
  differ, kills them, and only then removes its temporary `HOME`. A
  process-group-only scan missed those workers and intermittently failed
  `t.TempDir()` cleanup with `directory not empty`. Never scan or signal the
  test runner's shared session. The drain cleanup is registered *after*
  `t.TempDir()` so it runs before the directory removal. A test that launches
  a private session and lends it a temporary directory owes the same drain.
  The harness also isolates `XDG_RUNTIME_DIR` to a private 0700 directory and
  sets `GDK_DEBUG=no-portals`. Never run the GTK binary or dry-run tests directly
  against the developer's live `/run/user/<uid>` or host session bus; ad-hoc runs
  must use an isolated container or `env -u DBUS_SESSION_BUS_ADDRESS dbus-run-session`
  with an isolated runtime directory. Never stop, mask, or unmount host desktop portals.
  The E2E suite requires GTK4, Libadwaita, `dbus-run-session`, and `xvfb-run`; the hosted E2E job
  installs those runtime dependencies explicitly because ordinary unit-test
  hosts intentionally do not carry them.
  With `E2E_COVERDIR` set, the GUI's counters reach it only because
  `cmd/chairlift` handles `SIGTERM`/`SIGINT` by quitting the application on
  the main thread, so `Run` returns and `main` exits normally; a process that
  dies by signal never flushes `GOCOVERDIR`, which left the `e2e` Codecov
  flag at 0% for every GTK package (issue #306). The dry-run smoke test
  asserts the `main: application exited` marker after its `SIGTERM`, so a
  regression fails `make e2e`. Keep the harnesses sending `SIGTERM` first and
  `SIGKILL` only on timeout.
- `make install`'s default `PREFIX` is `/usr` — the only prefix under which
  the installed PolicyKit policy files land where `polkitd` reads them
  (`/usr/share/polkit-1/actions`) and the updex helper's installed
  path matches its fixed `pkexec` exec-path annotation (see the privilege
  boundary invariant below). It installs maintainer defaults at
  `/usr/share/chairlift/config.yml` and must never install or overwrite the
  administrator-owned `/etc/chairlift/config.yml`. GoReleaser publishes both
  the self-contained `projectbluefin-chairlift` package and the mutually exclusive
  `projectbluefin-chairlift-system-integration` companion for user-scoped GUI
  installs; every nFPM entry carrying policies must retain the same fixed
  paths. Published packages declare their mandatory runtime dependencies
  (issue #89): the full package carries its GTK4, Libadwaita, and Bash names
  per format via `overrides` — the distro package names differ across
  deb/rpm/apk, and GoReleaser's overrides merge replaces rather than appends
  a base-level list — while the integration package declares none, because it
  ships no GUI, desktop entry, or wrapper script. `internal/installcheck`'s
  `TestGoreleaserDeclaresMandatoryRuntimeDependencies` holds both halves.

CI (`.github/workflows/test.yml`) filters tests with `-run "^Test[^I]"
-skip "Integration"`. That filter excludes *any* test whose name begins `TestI`
— not only `TestIntegration` — or contains `Integration` anywhere. Those two
name shapes are reserved for tests that require a real environment (a live
`brew`, `flatpak`, `bootc`, or GTK display), and such tests live under
`test/e2e/`, which `make e2e` runs unfiltered. That placement is the whole
reservation: no enforced gate runs `./internal/...` unfiltered, so a reserved
name under `internal/` is executed by no gate at all — the unit-test step
skips it and the E2E step never looks at that directory. The local `make
test` convenience target does run `go test ./...` unfiltered and will
execute such a test on a developer's machine, which is precisely how the
shape survives review: it passes locally and is never selected in CI.

Inside `internal/`, therefore, a reserved name is always an accident, and
`internal/installcheck`'s `TestNoInternalTestNameIsExcludedByTheCIFilter`
rejects it. The accident is easy to make, because plain unit-test names such
as `TestIsValid`, `TestInitConfig`, or `TestIndexOf` all start with `TestI`;
name them so the first letter after `Test` is the subject (see the
GTK-headless and gated-test-placement skills below). When that gate was added
it found nine such tests across `internal/distrobox`, `internal/gaming`,
`internal/version`, `internal/installcheck`, and a since-removed OS update
provider — among
them the goreleaser test this file and ADR-0006 both cite as enforcing the
system-integration package split; its name matched `-skip "Integration"`, so the
filtered unit-test step never selected it.

The separately invoked tests under `test/e2e/` are outside the
`./internal/...` unit-test scope by design. They are enforced by the E2E
workflow's explicit `make e2e` step; do not assume adding a test outside
`internal/` is enough without that dedicated gate.

There are no generated files and no codegen step; everything under version
control is hand-written Go, YAML, and data assets.

## Repository invariants

An agent must not break these:

- **Privilege boundary.** State-changing operations that require root go
  through `pkexec` (PolicyKit) with fixed, installed polkit policies and fixed
  helper binaries only: `pkexec /usr/libexec/bootc-update-stage` (action
  `io.projectbluefin.chairlift.bootc.stage`),
  `pkexec /usr/bin/chairlift-updex-helper` (`internal/updex.HelperPath`, actions
  `io.projectbluefin.chairlift.updex.{enable-feature,disable-feature,update}`), and
  `pkexec /usr/bin/chairlift-ublue-helper` (`internal/ublue.HelperPath`,
  actions `io.projectbluefin.chairlift.ublue.*` — see the helper-extension
  invariant below for the full subcommand list)
  — always that fixed absolute path, matching the
  `org.freedesktop.policykit.exec.path` annotation, with the updex subcommand
  matching `org.freedesktop.policykit.exec.argv1`. The helper must strictly
  reject unsupported argv because PolicyKit does not validate arguments after
  action selection. ChairLift ships no passwordless PolicyKit rules; normal
  administrator authentication applies. Homebrew tap trust (`brew trust`) is
  deliberately per-user and does **not** use pkexec, and neither does gaming
  mode, whose components are all user-scope Flatpaks. Do not add arbitrary
  privileged command execution, broaden what pkexec runs, or route new
  mutations around the fixed helper/policy pair.
- **Neither an image reference nor a username crosses the ublue pkexec
  boundary.** `chairlift-ublue-helper` receives a channel word only, and
  derives the concrete `bootc switch` target itself from the read-only image
  descriptor plus the channel table; it derives the account to modify from
  the `PKEXEC_UID` pkexec sets, never from argv. Accepting either as an
  argument would let an authenticated caller switch the machine to an
  arbitrary image, or add an arbitrary account to the privileged developer
  groups. `internal/ublue`'s test asserts that no argument crossing the
  boundary contains a `/`.
- **Custom Command Menu integration is user-scoped and unprivileged.**
  `internal/devmenu` manages the Custom Command Menu extension
  (`org.gnome.shell.extensions.custom-command-list`) visibility for Terminal and
  Containers through out-of-process `dconf` CLI calls in the
  user session. Neither menu tuple values nor extension commands cross pkexec.
  Mutations apply only after a confirmed live developer-state transition, never
  on view construction, state restore, failed authentication, or dry-run.
  Distro defaults are preserved via reset when matching, while `visible=false`
  is enforced as an override when the distro default is visible.
- **The release-channel table is keyed on the image, never on the tag alone.**
  `internal/imageinfo`'s `imageChannelMap` records, per registry path, which
  tags are stable streams, which are testing streams, and how each maps to
  the other. The entries were verified against GHCR by manifest request; the
  comment above the map carries the observed 200/404 results. Collapsing it
  back into a tag-keyed map reintroduces two references that do not exist
  (`ghcr.io/ublue-os/bluefin:testing` and
  `ghcr.io/projectbluefin/bluefin-lts:lts-testing`), which is a failed `bootc
  switch` on a user's OS rather than a cosmetic bug. Other images are added
  through a `channels.yml` override, which is read only from
  `/etc/chairlift/channels.yml` and `/usr/share/chairlift/channels.yml` —
  never the working directory, because the privileged helper resolves its
  switch target through the same table. The graphics-driver variant table
  lives in the same file's `drivers:` section for the same reason: a driver
  switch is resolved by the same helper, so it must not be configurable from
  a user-writable path, and keeping both tables in one file removes any way
  for the GUI and the helper to load different ones.
- **The unified update run composes; it does not add a privileged route.**
  `internal/updateflow` is the pure coordinator for the one-action update run:
  it owns the phases, the primary action, and the per-source state for the
  four sources (`applications`, `developer-tools`, `system-components`,
  `operating-system`), and it executes nothing itself — every provider is an
  `updateflow.Provider` whose production value in `internal/updateproviders`
  wraps the existing `internal/flatpak`, `internal/homebrew`,
  `internal/updex`, and `internal/bootc` entry points.
  `internal/views/updatepresent` is the equally pure presentation layer: it
  maps one immutable snapshot to a title, description, banner, and action
  label, so the shell's copy is testable on a headless host.
  `internal/views/update_shell.go` is widget wiring only — it holds no update
  state of its own and decides nothing the coordinator or the presenter
  already decided. Keep those three layers separate; do not move a phase
  decision into the widget file or a string into the coordinator.
  The operating-system source must keep going through `internal/bootc`'s
  staging path. Adding a
  `bootc upgrade` route to `chairlift-ublue-helper` would break both the
  staging-ownership invariant below and the system-integration package's
  fixed-path contract. The run's only privileged surface of its own is
  `restart`: `updateflow.ActionRestart` is set when the snapshot reaches
  `PhaseRestartRequired`, `updatepresent` renders it as a destructive
  "Restart now" button, and `UpdateShell.StartRestart` calls `ublue.Restart`.
  That phase is reached only when a source genuinely reports a restart is
  required — the stage script is idempotent and exits 0 on an already-current
  system, so a successful OS source is not by itself evidence anything
  changed.
- **New privileged operations extend the ublue helper; they do not add a
  binary.** `chairlift-ublue-helper` carries nine subcommands
  (`channel-switch`, `dx-enable`, `dx-disable`, `restart`, `rollback`,
  `auto-updates-enable`, `auto-updates-disable`, `driver-switch`,
  `factory-reset`), each selected by exactly one PolicyKit
  action. Every one takes a fixed argv or a word validated against a closed
  set: no image reference, no username, no systemd unit, no delay, and no
  rollback or reset target crosses the boundary, because each
  would be a value an authenticated caller controls. `factory-reset` is the
  extreme case and the shape to copy: it is the most destructive privileged
  action ChairLift offers, so both the program (`bootc`) and its entire argv
  (`ubluehelper.FactoryResetArgs`, the fixed
  `install reset --experimental --apply`) are spelled in the helper, and the
  GUI sends nothing but the command word — a factory reset has exactly one
  target, the image already booted, so there is nothing for a caller to name.
  `rollback` is the same shape with an even shorter argv.
  `internal/ubluehelper`'s tests assert
  this per command, and the e2e boundary test asserts the installed binary
  rejects each shape. `cmd/chairlift-ublue-helper`'s dispatch carries a
  `default` arm that exits non-zero: a command the parser accepts and the
  switch does not handle would otherwise exit 0 having done nothing, which
  the GUI cannot tell apart from a privileged action that worked. The
  accepted half is asserted separately, because `cmd/` is outside the
  `./internal/...` unit gate: `test/e2e/helper_commands_test.go` runs every
  command in `ubluehelper.SupportedCommands` and
  `updexhelper.SupportedCommands` through the staged binary with
  `--dry-run` and asserts the arm's own output, and derives its own
  completeness from those two sets — a new subcommand fails that gate until
  it has an accepted-command case, not only a rejection case.
- **Every navigable page has a committed screenshot and a walkthrough entry.**
  `make screenshots` regenerates `docs/screenshots/` from the real
  application; `docs/walkthrough.md` is the user-facing tour built from them.
  `internal/installcheck`'s walkthrough tests run in `make ci` and fail when a
  page has no screenshot, when the document does not reference one, when a
  screenshot is orphaned, when a capture byproduct is committed, when a
  supported image in `imageinfo.KnownImages()` is not named, or when any group
  in `config.SchemaGroups` has no walkthrough entry. That last check is the
  forcing function: the group-to-phrase table is hand-written but its
  completeness is derived from the config schema, so a feature added to an
  *existing* page — which is how the unified update shell, Automatic updates,
  and Roll Back all landed — cannot slip through undocumented. The check is deliberately referential rather
  than a pixel comparison: font hinting and GTK point releases move pixels, so
  regenerating and diffing per push would churn the repository for no signal.
  Adding a page or a user-facing feature means running `make screenshots` and
  extending `docs/walkthrough.md` in the same change.
  The screenshot runner must write its reset-group override as
  `config.dev.yml` beside the tagged binary: that is the first relative
  candidate, ahead of the checkout's own `config.dev.yml` and any
  executable-adjacent `config.yml`. The E2E walkthrough requires the
  `views: reset group built` marker so a plausible screenshot cannot hide
  the opt-in Reset rows silently.
  The E2E workflows also pass `CHAIRLIFT_SCHEMA_DIR=build/schemas` to the
  capture script; without it, the real Livery page shows a missing-schema
  toast that obscures screenshots even though every page capture test passes.
- **The `chairlift_e2e` stub surface is capped and centralized.** Four
  behaviors are stubbed so the screenshot walkthrough can render features a CI
  runner cannot have: the image descriptor (`CHAIRLIFT_IMAGE_INFO`), the
  unattended-update timer state (`CHAIRLIFT_AUTO_UPDATES`), the graphics
  hardware (`CHAIRLIFT_GPU_VENDORS`), and the host capability set that floors
  visibility (`CHAIRLIFT_CAPABILITIES`). Every stub must be
  read in `internal/app/imageinfo_override_e2e.go` and nowhere else, behind
  the `chairlift_e2e` tag that only `make e2e` sets, with a no-op counterpart
  in `imageinfo_override.go`.
  `internal/installcheck.TestDescriptorOverrideStaysBehindTheE2EBuildTag`
  enforces both halves, asserts this rule names every stubbed variable, and
  must gain each new variable's name. Adding a stub
  means adding it to that one file and that one test — never a second tagged
  file, and never an untagged read. A stub may only affect a read-only,
  display-side classification: anything a privileged helper consults must
  keep resolving its own source of truth, because the helper is built without
  the tag.
- **OS staging execution has one owner.** `internal/stageexec` is the pure-Go
  leaf package that owns the progress event contract, merged stdout/stderr
  streaming, direct-child cancellation, error classification, completion event,
  and channel closure for `internal/bootc`.
  The provider package retains its fixed path, host detection, dry-run logging,
  and public error adapters; do not copy the process loop back into it.
- **The escalation program name has one owner.** `internal/pkexec.Command` is
  the only place the literal `pkexec` is spelled in Go code; every provider
  (`internal/bootc`, `internal/ublue`, `internal/updex`)
  and `internal/views/pageview` names it instead of declaring a private copy.
  `internal/helperexec` and `internal/stageexec` keep taking the program name
  as an injected parameter — that is their test seam — but production callers
  always pass `pkexec.Command`. `internal/installcheck`'s
  `TestPkexecCommandHasOneOwner` parses every non-test file under `internal/`
  and `cmd/` and fails on any other occurrence; it takes no exemptions.
- **System-integration split.** The
  `projectbluefin-chairlift-system-integration` nFPM package contains the fixed-path
  updex and ublue helpers, the bootc, updex, and ublue PolicyKit policies, package-maintainer
  config, and the channel-table example, but not the GUI or an OS staging
  implementation. Distributions pairing it with a user-scoped ChairLift install
  must provide their trusted stage helper at `/usr/libexec/bootc-update-stage`
  before enabling `bootc_updates_group`. Do not
  make the privileged path configurable from ChairLift's user-writable
  configuration.
- **GTK main-thread safety.** All external tool calls run in goroutines; every
  UI update marshals back to the GTK main thread via
  `snowkit`'s `sgtk.RunOnMainThread(...)`. Never touch a widget directly from a
  worker goroutine.
- **GObject constructor properties cross the native ABI.** Pass native
  `GoPointer()` values for object-valued `gobject.NewObject` properties, not
  Go wrapper addresses, and terminate the C variadic property list with
  `uintptr(0)`. The wrong pointer emitted a GLib critical on every window
  launch; the E2E dry-run startup now uses `G_DEBUG=fatal-criticals` so the
  actual binary fails instead of only logging it.
- **Streamed command output renders bounded.** A stage helper prints an
  unbounded number of lines, so a view may not answer one line with one
  `sgtk.RunOnMainThread` callback creating one permanent row: that queues a
  callback per line and leaks a heavyweight widget per line, which is the
  frozen window of issue #81. The bootc staging handler renders through
  `stageProgressSink`, which coalesces a burst into one callback with
  `internal/views/progresslog` and caps the expander at
  `progresslog.DefaultLimit` rows with `rowset.Tracker.TrimTo`; the Details
  subtitle comes from `pageview.StagingLogSubtitle`, so a window that hid
  older lines says so instead of reading like a complete log.
  `internal/views/progresslog`'s wiring test reads `updates_page.go` and
  rejects a return to the per-line shape. Any future view that renders a
  stream of external output owes the same two caps.
- **Headless view coverage stays puregotk-free.** `internal/views` cannot host
  a test binary on ordinary CI hosts. Shared row text, page status, os-release
  parsing, help-link ordering, and maintenance-command selection live in the
  pure `internal/views/pageview` package; its wiring test must continue to
  cover all seven page builders.
- **Navigation behavior has one authority.** Page order, titles, icons, and
  advertised/registered accelerators live in the pure
  `internal/navigation` package. It also decides page visibility from static
  group configuration: omit a functional page when all of its builder-backed
  groups are disabled, always retain Help, and compact Alt+number over visible
  pages. Mouse activation and window navigation actions must both call
  `Window.navigateToPage`, which applies the complete `navigation.Resolve`
  transition (visible-row index, visible child, title, and collapsed-layout
  content reveal). The app and shortcuts dialog must use the window's same
  visible inventory. Do not reintroduce a second page or shortcut inventory in
  `internal/window` or `internal/app`. The inventory is seven pages, in this
  order: Updates, Apps, Agents, Features, Livery, Maintenance, Help. Two
  details in it are easy to get wrong. The sidebar title for
  `applications_page` is "Apps", not "Applications" — the page name and the
  title are separate fields and only `internal/navigation` reconciles them.
  And there is no System page: "about this computer" belongs to GNOME
  Settings, which every host running this application already ships, so the
  system-version readout and the release-channel switch live on Updates,
  beside the thing that changes them.
- **The host capability floor has one owner.** `internal/capability` is the
  puregotk-free authority for what this host can back a page or group with,
  and its probes are non-blocking only (`exec.LookPath`, `os.Stat`, environment
  reads), because page-level resolution runs synchronously on the GTK main
  thread during `buildUI`. A capability is the presence of a backing tool or
  asset, never a runtime state: a gate that needs a query (`bootc status`,
  updex's feature store, `uupd.timer`'s systemd state) stays asynchronous in
  its view and builds a hidden shell. Capability is a floor — configuration may
  subtract from it and never add to it — so its composed predicate is the one
  `navigation.VisibleItems` and the view builders share; do not reintroduce a
  second availability probe in a view. The prerequisites table is total over
  `config.SchemaGroups` in both directions, enforced by
  `internal/installcheck`'s `TestCapabilityPrerequisitesMatchConfigSchema`, so
  a new config group is classified in the same change that adds it.
- **Setup filters choices, not whole pages.** `internal/firstrun` snapshots
  the shared composed capability floor for at most three optional tasks:
  Appearance, Apps, and Update Preferences. Each choice retains its original
  page/group policy references; Update Preferences includes `features_page`'s
  `features_group`. Nil fails closed, empty tasks disappear, and returned
  snapshots do not expose mutable model state. Next/Back emit no settings or
  feature operation; Skip and intentional Dismiss emit the same disposition,
  preserving an existing completion. The welcome entry is not a decision step.
  Dedicated controls and dismissal persistence wiring belong to issue #225;
  do not claim the pure model implements those GTK behaviors.
- **The Homebrew executable has one resolution.** `internal/homebrew.ExecutablePath`
  is the only place ChairLift decides which `brew` it means: the `brew` that
  `$PATH` resolves, or `/home/linuxbrew/.linuxbrew/bin/brew` when `$PATH` has
  none. That fallback is the path `data/chairlift-wrapper.sh` evaluates
  `shellenv` from, so a launch through the wrapper finds `brew` on `$PATH` and
  a direct binary launch — the desktop entry, or `chairlift` run from a shell —
  does not, even though the same Homebrew is installed. Visibility
  (`IsInstalled`, and through it every view that hides or disables a Homebrew
  affordance) and execution (`runBrewCommandCtx`, and through it every brew
  command ChairLift issues) both read it, so a host whose Homebrew is reachable
  only at the fallback is reported as installed *and* actually driven. A `brew`
  on `$PATH` wins over the fallback. `internal/capability`'s Homebrew floor
  resolves through `homebrew.ResolveExecutable`, so the floor, `IsInstalled`,
  and the exec paths share one answer. Do not reintroduce a second resolution: no
  bare `"brew"` at an exec site, and no private copy of the fallback path.
- **Homebrew update actions preserve known state.** Per-package upgrades and
  the top-level metadata update use `internal/views/actionstate` gates before
  spawning work. Failures and dry-run previews restore their controls without
  changing rows or counts. A live package success removes its row, decrements
  the count/badge, and refreshes; a failed refresh preserves that last known
  row/count state instead of replacing it with an invented zero.
- **Homebrew application actions are typed and refresh-safe.** Search queries
  both formula and cask namespaces and carries the result kind into
  `brew install [--cask]`. Search and installed-package refreshes use separate
  `actionstate.RefreshGate` generations; stale workers must not replace newer
  rows. Confirmed installs use an `actionstate.Gate`, restore controls on
  failure/dry-run, and refresh installed rows only after a live success.
  Installed formula/cask rows likewise confirm uninstall, formula rows confirm
  pin/unpin, and every row shares one gate across its mutation controls so
  actions cannot overlap. A live success completes the old controls and starts
  a generation-guarded inventory refresh; failure or dry-run restores them.
- **A visible retryable control must reset its action gate.**
  `actionstate.Gate.Complete` permanently rejects future starts; reserve it for
  controls that become permanently unavailable after live success. Update All,
  driver switching, Powerwash, and Factory Reset restore their buttons after
  a run, so they reset their gates even after failure or dry-run. Roll Back is
  different: `bootc rollback` toggles the selected deployment, so a successful
  live click completes its gate and leaves its button insensitive; only a
  failure or preview resets it. `internal/views/actionstate`'s wiring tests
  guard both lifetimes.
  Both the dedicated bootc stage action and Update All's OS phase refresh
  the badge and changelog's Compare references from the new status rather
  than leaving Compare disabled until restart.
  A changed pinned image pair clears old diff rows; an in-flight comparison
  for the old pair must not render after the refresh.
- **Update badge counts have one state owner.** Bootc, Flatpak, and Homebrew
  counts live in the pure `internal/views/badgestate` package. Verified
  refreshes replace a provider's count, while a failed bootc status read keeps
  the last known count through `SetObserved` rather than inventing zero.
  Successful row removals decrement without going negative, and the displayed
  total is always the sum of all three providers. Do not restore independent integer fields in `UserHome`.
- **Config-driven visibility is real.** Any group can be disabled in config
  (`config.IsGroupEnabled(page, group)`), so its widgets may never be
  constructed. Code that runs after an async action must not assume a widget
  from another group exists — nil-guard cross-group widget access. In
  particular, `brew_bundles_group` is independent of `brew_group`; bundle
  discovery and installs must not assume the formulae/casks expanders exist.
- **Configuration precedence fails closed.** Only a missing candidate advances
  to the next configuration search path. The first file that exists is
  authoritative: read, YAML, or schema errors must disable every configurable
  group, emit the `CONFIGURATION ERROR` diagnostic, and remain visible in the
  UI as a persistent toast until the file is fixed and ChairLift is restarted.
  The legacy `system_page` input is a narrow compatibility exception to the
  current page inventory: validate its four historical groups before moving
  `bootc_status_group` and `channel_group` into Updates. Current non-null
  fields win; retired information/health groups have no runtime effect.
  This must not add a navigable page or relax unknown-name or sudo validation.
- **CI actions are immutable.** Every external `uses:` reference under
  `.github/workflows/` must use a full 40-character commit SHA. Keep the
  human-readable version or source ref in a trailing comment and update both
  intentionally. Local actions referenced with `./` are exempt. The
  `internal/installcheck` workflow scan enforces this across every workflow.
- **The merge queue gates on one context, and that context waits for every
  other job.** `main` merges through a merge queue, which validates a
  candidate on a `gh-readonly-queue/main/pr-<n>-<sha>` ref — a `merge_group`
  event that neither `push` nor `pull_request` fires for. `test.yml` declares
  it, deliberately unfiltered, because `github.ref` there is the queue ref and
  a `branches: [main]` filter would match nothing and silently return the
  queue to merging unvalidated heads. The ruleset requires the aggregating
  `Tests Passed` job rather than the individual jobs, whose names change with
  the matrix; it carries `if: always()` because GitHub counts a skipped
  required check as a passing one. Adding a job to `test.yml` means adding it
  to that job's `needs` —
  `internal/installcheck`'s `TestMergeQueueGateWaitsForEveryTestJob` fails
  otherwise — and renaming the job means editing the ruleset in the same
  change.
- **Every privileged dispatch point journals, unconditionally.** `internal/ublue.runHelper`
  and `internal/updex.runHelper` call `journal.Record` on every invocation, dry-run
  or live, before doing anything else. This is not a `chairlift_e2e` stub: with
  `$CHAIRLIFT_ACTION_JOURNAL` unset — every ordinary run — it costs one atomic
  load and does nothing else, so it ships in every released binary. Do not gate
  a new privileged call behind a helper that bypasses `runHelper`; the journal's
  value is that it is genuinely one choke point for every privileged action,
  not most of them.
- **Desktop notifications stay rare.** `internal/notify` sends exactly one:
  the unified update run's completion (`notify.UpdateAllComplete`, sent from
  `UpdateShell.notifyUpdateComplete`), because it is the one action long enough a user may
  have stepped away. A toggle or switch completes in view and already has a
  toast; do not add a second notification for the same instant event.
- **Developer-mode feed onboarding is opt-in, follows the enable, and never
  reverses it.** `dx_group`'s `install_pulp` and `stage_feeds` both default to
  `false`, and they are the only optional work the Developer Mode switch does
  beyond the privileged group promotion. `startDeveloperFeedSetup`
  (`internal/views/features_page.go`) runs them from the same success branch
  `openDeveloperOnboarding` uses, behind
  `actionmsg.DeveloperFeedSetupPlan(dryRun, enabled, succeeded, …)`: a
  `--dry-run` preview, a disable, a page restore, and a failed helper call all
  produce an empty plan and start no worker. Everything is user-scope — a
  Flatpak in the invoking account and one file under `~/.local/share/chairlift`
  — so no `pkexec` route is involved, Pulp's sandboxed store is never written
  to, and the feedback never claims a subscription was imported. The two
  optional outcomes are reported separately from the permission change, which
  has already succeeded: a failed install is a failed install, it rolls nothing
  back, and it must not read as a failed enable. Disabling Developer Mode is a
  clean no-op — it removes neither Pulp nor the staged file nor anything the
  user imported. The worker reads its plan from config on the main thread
  before it starts, is admitted one at a time by `developerFeedGate`, and
  reaches the toast only through `sgtk.RunOnMainThread` behind a nil guard.
- **Enhanced Troubleshooting reads state, it does not infer it.**
  `internal/troubleshoot` ports Bluefin's `ujust probe` into one row, from
  `ublue-os/tap`: `linux-mcp-server` (which pulls `block-goose-cli`) plus the
  `goose-linux` cask. `goose-mcp-setup` exits 0 without writing anything when
  a Goose configuration already exists, so readiness must come from finding
  the `linux-tools` extension in `~/.config/goose/config.yaml` — never from
  the script's exit code, and never from the packages being installed. It is
  an action row, not a switch: turning it off would mean either leaving Goose
  calling a removed binary or rewriting a file another tool owns. ChairLift
  reads `GOOSE_PROVIDER` and never writes it; the row must keep naming the
  provider, because the default the setup script writes sends system details
  to Google and "AI assistant" alone implies otherwise. Everything is a
  user-scope Homebrew install and the MCP tools are read-only, so nothing
  here touches pkexec. `brew tap` is in `stateChangingCommands`, without
  which it would run for real under `--dry-run`, including during
  `make screenshots`.
- **The staged-update changelog never fetches on its own.** `internal/sbom`
  is pure — parse, diff, version ordering — with the registry round-trip
  behind the `FetchFunc` seam, so no gated test makes an outbound request
  (`fetch_test.go` drives a loopback `httptest` registry instead). Discovery
  must keep both paths: GHCR answers `/v2/<repo>/referrers/<digest>` with
  404 for these images, and the SBOM is only reachable through the
  specification's fallback tag (`sha256-<hex>`) — verified 2026-08-17. What
  that referrer serves is Syft JSON despite the `application/vnd.spdx+json`
  artifact type, so `Parse` accepts both shapes and errors rather than
  returning an empty map when it recognizes neither; a silent zero-package
  parse renders a blank changelog with nothing in the chain reporting a
  failure, which is a bug finupdate shipped. The diff runs only when the user
  presses Compare, because each side is tens of megabytes.
- **The dated-build catalog reads the registry, and reads it read-only.**
  `internal/registrytags` is the leaf package behind the rollback calendar
  (ADR-0013): `Client.Tags` lists a repository through the registry's
  `Link: rel="next"` pagination, `ParseBuild` reads the day out of the tag
  name, and `Client.Tag` resolves one tag to its digest and its
  `org.opencontainers.image.created` timestamp. Every request goes through the
  `Client.HTTP` transport, the same seam `internal/sbom` uses, so no gate in
  `make ci` makes an outbound request — its tests drive a loopback `httptest`
  registry that models GHCR's pagination, its 404 `MANIFEST_UNKNOWN`, and the
  fact that the response's `Content-Type` header, not the body's `mediaType`
  field, is the media-type authority (GHCR omits `mediaType` on some dated-tag
  manifests — verified 2026-09-22). Two rules keep it safe to grow: nothing it
  returns may reach a privileged path — a `bootc switch` target is still
  `internal/imageinfo`'s tables and only those (ADR-0011), and a pin is a
  separate decision because no image reference crosses the ublue pkexec
  boundary — and the catalog is never baked, cached to disk, or served stale,
  because a catalog that is not the registry's is the failure this design
  exists to avoid. A failed read is returned to the caller, never cached and
  never replaced by a previous answer. `Catalog` caches in process only,
  bounded by `MaxEntries` and expiring at `TTL`, and its callers run off the
  GTK main thread, so it must stay safe for concurrent readers. Its one
  caller is the Recovery page's **Published versions** row
  (`internal/views/versions.go`), which reads only when the user presses
  Check, lists one row per day of the running stream
  (`pageview.PublishedVersions` drops other streams' aliases), and removes the
  last list when a read fails rather than leaving it standing as current.
- **Agent Mode is one switch on its own page, runs llmman as a user unit,
  and is unprivileged.** It lives on `agents_page`, built by
  `internal/views/agents_page.go`, as that page's single group
  (`agents_group`, floored on Homebrew). ADR-0015 is the contract.
  `internal/aistack` owns exactly three artifacts and nothing else: the
  generated Brewfile it hands to `brew bundle install` (tap `llmmanorg/tap`,
  formula `llmmanorg/tap/llmman` unless an `llmman` already resolves, and
  the `ai.jan.Jan` Flatpak on x86_64 only), the systemd **user** unit
  `~/.config/systemd/user/chairlift-llmman.service`, and the environment.d
  fragment `~/.config/environment.d/10-chairlift-llmman.conf`
  (`OLLAMA_HOST=127.0.0.1:17434`, nothing else — no `OPENAI_BASE_URL` or
  `OPENAI_API_KEY`). llmman owns models and engine selection; Homebrew owns
  the binary; lifecycle is ChairLift's, never `brew services`. The unit's
  `ExecStart` is the absolute `llmman` path resolved after install (`$PATH`,
  then beside `homebrew.ExecutablePath()`); no Homebrew prefix is spelled.
  It binds `LLMMAN_HOST=127.0.0.1:17434` and carries the literal
  `LLMMAN_SHELL=off` and `LLMMAN_NOHISTORY=1`; no `LLMMAN_ORIGINS` is set
  until Jan's exact origin is verified, and wildcard CORS is forbidden.
  Enable runs `llmman serve --pull-only` before writing anything so an
  engine that cannot be fetched fails the switch; readiness is a bounded
  `GET /llmman/node`, never `systemctl is-active` alone. Every mutation is
  behind `dryrun.Enabled()`. Disabling removes only the unit and the
  fragment — binaries, Jan, and models stay — and must preserve both when
  `systemctl --user disable --now` fails and a follow-up `is-active` check
  cannot prove the service stopped; removing the unit while the service is
  still active makes the switch lie and removes the user's management
  handle. Do not give it a pkexec route, and do not reintroduce a
  container-image or vendor/stack matrix.
- **Livery shadows icon-theme names, and the theme it writes into is not
  always hicolor.** `internal/livery` sets three marks — the app-grid button
  (`view-app-grid-symbolic`), the panel menu button
  (`PanelIconName(id)`, i.e. `chairlift-livery-<id>-symbolic`, via the Custom
  Command Menu extension's `menuicon-setting`), and the Files application
  (`org.gnome.Nautilus`) — by
  installing an SVG into the user's icon theme and referencing it by bare
  name. A GNOME panel icon is a themed *name*, never a path: the extension
  builds `new St.Icon({icon_name: …})`, so an absolute path there renders
  nothing. Which theme directory receives the override is per surface and is
  load-bearing, because XDG resolves the current theme and its parents before
  falling back to hicolor: a name Adwaita already ships can only be shadowed
  inside `~/.local/share/icons/Adwaita`, while a name it does not ship
  (`org.gnome.Nautilus`) works from hicolor. Getting this backwards produces a
  write that succeeds and an icon that never changes;
  `TestAppGridOverrideTargetsTheAdwaitaTheme` holds both cases.
  Every write ends in `gtk-update-icon-cache -f -t`, without which GTK trusts an existing
  `icon-theme.cache` and never sees the new file; `-t` is
  `--ignore-theme-index`, which is what lets a user theme directory with no
  `index.theme` of its own work. The app-grid button is reached through
  dash-to-dock's fallback: `appIcons.js` requests
  `view-app-grid-${sessionMode}-symbolic`, which exists nowhere, after saving
  the base `view-app-grid-symbolic` as `fallbackIconName`. Nothing here edits
  a `.desktop` file: desktop entries replace rather than merge, and
  Nautilus's carries ~240 localized names, a MimeType list, and a
  `[Desktop Action]` group a generated override would silently drop. The
  Files mark is one icon per *application*, so it changes Files in the dash,
  app grid, window switcher, and notifications — the UI says so rather than
  claiming "dock". The dock's catalog is **not** the foundation catalog: it is
  every CNCF project publishing a color icon, from an embedded
  `assets/cncf-projects.txt` manifest that stores each artwork's real path.
  Thirty of the 214 do not follow `<id>-icon-color.svg` — cilium ships
  `cilium_icon-color.svg`, kubeflow-notebooks a bare `icon-color.svg` — so
  deriving the filename 404s on exactly those;
  `TestCNCFPathsAreNotDerivedFromIDs` pins it. Artwork is fetched on demand
  through the same `Fetch` seam, never vendored: 214 color SVGs is megabytes
  nobody needs until they pick one. The app grid's catalog works the same way
  over simpleicons.org's 3,461 brands, and its slugs come from that project's
  generated `slugs.md` rather than a reimplementation of its title-to-slug
  rules — those rules turn ".NET" into `dotnet` and "Write.as" into
  `writedotas`, and a hand-written transform scored 24 of 25 on a random
  sample, which across the catalog is a hundred brands that would 404 for
  whoever picked them. Both catalogs are searched through one chooser dialog
  built once and re-presented, for the callback-table reason above. The foundation marks therefore ship in one
  rendition only, symbolic; the color plates they once carried for the dock
  went with the catalog change. The panel catalog's *default* depends on the
  booted image: `DefaultFoundationID` returns the Open Gaming Collective's
  mark when `imageinfo.Info.IsGaming()` is true, because that is whose work
  the gaming images ship. It is applied in `resolveID` at load, so switching
  images adopts the new default without overwriting a selection the user made
  — `TestADeliberateSelectionSurvivesOnAGamingImage` holds that distinction.
  Whether this branding should be an org-wide convention rather than one
  application's default is projectbluefin/common#1156; if that lands "no",
  the gaming default goes and the entry becomes an ordinary choice.
  **The panel's icon name varies per selection, and that is not cosmetic.**
  GSettings emits no `changed::` when a key is written with the value it
  already holds, and the extension refreshes its indicator only on that
  signal (`extension.js:314`). A single fixed name with swapped file contents
  therefore left the previous mark on screen until the shell restarted — the
  write succeeded, the file changed, and nothing happened. `gsettings monitor`
  reported two change events for three writes when one repeated a value.
  Writing a different name per selection makes the value genuinely change;
  `TestPanelIconNameVariesWithSelection` pins it, and `removePanelIcons`
  sweeps the marks earlier selections left behind. The `chairlift-livery-`
  prefix does second duty as the "ours" test, so `CapturePanelOverrides` can
  refuse to record one of ChairLift's own names as the user's previous icon
  without persisting a flag to say so.
- **Connect GTK signals once, at page-build time — never inside a refresh
  path.** puregotk routes every `Connect*` through `purego.NewCallbackFnPtr`,
  which caches by the *address* of the func variable and draws from a fixed
  table: `maxCB = 2000`, with a hard `panic` when it fills and nothing ever
  releasing a slot. A closure created per row inside a function that reruns
  therefore burns slots until the application dies. The Livery page's project
  search hit this directly — six result rows rebuilt on every keystroke — and
  is why it connects one `GtkListBox::row-activated` for the page's lifetime
  and maps the activated row's index into the result set it last drew, instead
  of giving each row its own handler. Note this rule is not yet met
  everywhere: `applications_page.go` and `updates_page.go` connect per-row
  callbacks inside refresh paths that rerun on every search, which is the same
  latent panic under heavy use. Do not add new instances, and prefer fixing
  one when you are already editing that code.
- **Switch rows use `gtk.Switch` with `ConnectStateSet`, not a generic
  `notify`.** `AdwSwitchRow` exposes no change-specific signal in these
  bindings, and the generic `notify` fires for every property — sensitivity,
  title, subtitle. A page that drives mutations from it writes settings while
  restoring its own saved state, which is how the Livery page came to write to
  dconf on load, twice, before it was moved to the pattern
  `features_page.go` already used. `GtkSwitch::state-set` fires only when the
  active state changes, and `gtk_switch_set_active` is a no-op when the value
  is unchanged, so a programmatic restore is silent.
- **Reverting a Livery mark resets the key; it does not write the old value
  back.** dconf is layered, and on Bluefin the panel icon comes from a distro
  default in `/etc/dconf/db/distro.d`, not from the user and not from the
  extension's schema (whose default is `utilities-terminal-symbolic`). So
  `gsettings get` answers `ublue-logo-symbolic` while the user layer is empty.
  Detecting whether the user set anything is harder than it looks, and two
  obvious primitives are wrong: **neither `dconf read` nor `dconf dump` is
  user-layer-only** — both resolve through `/etc/dconf/db/distro`, so on a
  Bluefin host they happily report `ublue-logo-symbolic` when the user layer
  is empty. A live experiment established the discriminator: compare
  `dconf read KEY` against `dconf read -d KEY`, which is the value the key
  resolves to with the user layer removed. Equal means no user value, so
  revert resets; different means a genuine override, so revert restores that
  exact string. `CapturePanelOverrides` uses that comparison and
  `ClearPanelSettings` acts on it; `TestUserValueIgnoresADistroDefault` pins
  the case that was broken. Capturing the merged value instead — which an
  earlier version did — made revert write the distro default back as a user
  value, pinning the mark forever and overriding any later change to the
  distro layer. A user who has deliberately set the same string as the default
  is indistinguishable and harmless, since resetting leaves them the value
  they chose. Reads go through the `gsettings` tool rather than an
  in-process binding on purpose: `g_settings_new()` on an unknown schema id
  aborts the process, so an in-process read would turn "extension not
  installed" into a crash at startup.
- **A newly installed mark is made visible by touching the applications
  directory, not by reloading anything.** GNOME Shell caches icon textures by
  name in `StTextureCache`, so writing the SVG and refreshing the theme cache
  leaves the dash drawing the mark it already had. `RefreshShellIcons` bumps
  the mtime of `$XDG_DATA_HOME/applications`, which fires `GAppInfoMonitor`
  and makes the shell re-resolve application icons; verified live on Wayland,
  that covers the app-grid glyph as well as the Files mark. Three heavier
  levers were tried and rejected: disabling and re-enabling dash-to-dock does
  not work at all (the rebuilt widgets are handed the same cached texture) and
  is dangerous besides, because the disable persists to GSettings and a crash
  mid-reload leaves a Bluefin user with no dock across reboots; toggling
  `org.gnome.desktop.interface icon-theme` does work but mutates global
  appearance state with its own failure window, and restoring it naively pins
  a user-layer override — which happened once during testing and had to be
  reset; `org.gnome.Shell.Eval` and `ReloadExtension` are gated to unsafe-mode
  since GNOME 41 and refuse the call. The panel needs none of this: its
  extension redraws on `changed::menuicon-setting`.
- **Livery rotation runs at login, and ChairLift's GSettings schemas hold
  preferences only.** The rotation unit is a `Type=oneshot`
  `WantedBy=graphical-session.target` user unit written to the user's
  `~/.config/systemd/user`, the same unprivileged posture as Agent Mode's
  llmman unit; it invokes `chairlift --rotate-livery`, which short-circuits before
  `app.New()` so a headless service never opens a display. Login, not logout:
  an abrupt logout does not fire a hook, so a logout-triggered rotation would
  fall back to leaving the previous mark — exactly the outcome the feature
  exists to avoid. Idempotence comes from `last-rotation-token`, the graphical
  session's `ActiveEnterTimestampMonotonic`, so re-running the unit cannot
  double-advance — and because that token is recorded even for a failed pass,
  the dock's fetch is retried in-process (`rotateRetryDelays`) while the
  failure still looks like a network that is not up yet: a user manager cannot
  order against `network-online.target`, so a login that beats connectivity
  would otherwise rotate nothing and say so only in the journal. Only the two foundation sections rotate; the app-grid mark
  is the user's own brand and is set once. ChairLift ships GSettings schemas:
  `io.projectbluefin.chairlift.livery` (appearance preferences),
  `io.projectbluefin.chairlift.updates` (user source toggles for updates), and
  `io.projectbluefin.chairlift.firstrun` (onboarding disposition) —
  holding only preferences with no file on disk to infer them from;
  `make install` recompiles the schema cache and `make schemas` builds them
  for a source tree. Do not add keys for state that can be observed.
- **The Livery page must not write on load, and its network call stays behind
  a seam.** These bindings expose only the generic `notify` signal, which
  fires for sensitivity and subtitle changes too, so restoring saved state
  would otherwise look like a user edit. `applyLiveryState` assigns
  `liveryState` — seeded with the *resolved* combo ids, since an empty stored
  value resolves to index 0 and maps back to `cncf` — before touching a
  widget, holds `liverySuppress` across the restore, and arms `liveryLoaded`
  only at the end, including on the failure path. `internal/livery.Fetch` is
  the page's single network round trip, following `internal/sbom`'s shape so
  the package's tests point it at a loopback `httptest` server and no gated
  test makes an outbound request; slugs are validated against a closed
  character set before the request, so user text never reaches the URL path.
  Every mutating path — icon writes, cache refresh, `gsettings set`, and the
  rotation unit — is gated behind `dryrun.Enabled()`, because
  `make screenshots` runs the real application with `--dry-run`; artwork is
  still resolved under dry-run so a missing custom file or unknown brand is
  still reported.
- **Routine cleanup has one key and one owner.** The Maintenance page offers
  a single "Free up space" action, gated by `maintenance_freespace_group`.
  That key is `internal/updateproviders.CleanupGroup`, the same constant
  gating the update run's post-update maintenance step
  (`updateproviders.NewMaintenance`), so one configuration switch
  governs both surfaces — a second key would let one surface clean while the
  other claimed the feature was disabled. The action composes
  `internal/updateproviders`' typed step inventory; do not reintroduce
  per-package-manager cleanup buttons, which asked the user to know which
  package manager owned their wasted disk space. The wording and the
  result-summarisation rules live in `internal/views/cleanupview`, which
  exists to enforce one thing: never claim more than happened. An absent
  provider was skipped, a dismissed authentication cleaned nothing, and a
  reclaimed-bytes figure appears only when both free-space readings succeeded
  and the difference clears `MinReportableBytes`.
- **Powerwash and Factory Reset are opt-in and always confirmed.**
  `reset_group` (maintenance_page) ships `enabled: false` in config.yml, the
  same default as `maintenance_cleanup_group`, because both actions are
  irreversible. Its group is titled "Recovery" rather than anything
  resembling cleanup, so a person hunting for disk space does not press it.
  Neither may run without the `AdwAlertDialog` confirmation in
  `internal/views/reset.go` first — that dialog's title and body come from
  `pageview.PowerwashConfirmation`/`FactoryResetConfirmation`, which is where
  the `--experimental` disclosure for Factory Reset's `bootc install reset`
  argv lives; do not move that text inline where it stops being tested.
  Powerwash needs no privilege (both steps run in the invoking account, like
  gaming mode); Factory Reset is the new `factory-reset` action on
  `chairlift-ublue-helper` and takes no argument, since it has exactly one
  target — the image already booted.
- **The product name and the code name are different strings, and only one of
  them has an owner.** The application ships in Bluefin as **Control Center**;
  ChairLift remains the code name for the repository, the Go module, the
  binaries, the wrapper, the package names, and the `io.projectbluefin.chairlift`
  application ID. That ID is fixed by the polkit `exec.path` annotations and the
  install prefix, so it never moves (ADR-0012). Every user-visible spelling of
  the product name resolves through `internal/branding.AppName` — window title,
  navigation page, About dialog, the About menu item, the Help description, the
  update-run completion notification, and `config.LoadError.ToastMessage`, the fail-closed
  configuration toast. `branding` imports nothing on purpose: `internal/config`
  and `internal/notify` both need the constant, and the constant's first home,
  `internal/views/pageview`, transitively pulls in `internal/sbom`,
  `internal/homebrew`, and `internal/troubleshoot`.
  `internal/installcheck`'s `TestDisplayNameHasOneOwner` parses every non-test
  file under `internal/` and `cmd/` and requires **every** string literal
  containing the code name to justify itself — structurally (a `/` makes it a
  path or URL; the application ID; a `CHAIRLIFT_` variable; a `chairlift-`
  binary or unit; a `usage: chairlift` line) or by an explicit
  `codeNameExemptions` entry stating why no user reads it, which
  `TestCodeNameExemptionsAreAllLive` then rejects once stale. The gate is an
  allowlist rather than a match on display APIs because the shape-matching
  version missed two live user-visible strings in a row: a struct field literal
  (`notify.Notification{Body: …}`) and a `fmt.Sprintf` format string (the
  configuration toast). Do not narrow it back to call sites.
  `TestDesktopEntryMatchesTheDisplayName` holds the other half:
  `data/io.projectbluefin.chairlift.desktop`'s `Name=` must equal the constant,
  `Type`/`Icon` must be correct, no key may repeat, exactly one registered main
  category may appear (two makes the app show twice in the menu), and
  `GenericName`/`Comment`/`Keywords` must be present, because GNOME Shell and
  KRunner search those keys — AppStream metainfo does not feed shell search, and
  this repository ships none. Screenshots are the unguarded edge: the
  walkthrough check is referential, not pixel-based, so a title-bar change means
  regenerating `docs/screenshots/` deliberately, from the E2E job's
  `walkthrough-screenshots` artifact with the capture byproducts stripped.

## Documentation

All documentation lives in the `docs/` tree, in frostyard/core's
four-category shape (core ADR-0025; the former `yeti/` AI-docs directory is
folded in). `docs/README.md` carries the category table, the index of every
doc, and the conventions — new docs start from their category's
`TEMPLATE.md`, and adding a doc means indexing it there:

- `docs/adr/` — why: repo-local decisions, immutable once accepted. Org-wide
  decisions go to frostyard/core instead, per
  [docs/org-adrs.md](docs/org-adrs.md).
- `docs/design/` — how it fits together: living architecture docs. The entry
  point is `docs/design/overview.md` (formerly `yeti/OVERVIEW.md`); read it
  and `docs/design/package-managers.md` (formerly `yeti/package-managers.md`)
  for architecture, patterns, and decision rationale before working. Write
  them to be maximally useful to an AI agent understanding the codebase —
  detailed architecture and rationale rather than user-facing guides.
- `docs/specs/` — exact contracts, changed only alongside implementing code.
- `docs/plans/` — phased plans with "Done when" outcomes.

After any change to source code, update relevant documentation in `AGENTS.md`,
`README.md`, and `docs/`. A task is not complete without reviewing
and updating relevant documentation. For behavior, configuration, dependency,
or install-layout changes, also follow
`docs/documentation-consistency.md`; current-state claims must be checked
against source/config/go.mod rather than copied from historical plans.

**.knowledge/ directory** is the repository's cross-session knowledge index.
Read `.knowledge/README.md` before working so prior corrections, handoffs,
durable lessons, and architecture guidance are discovered from their canonical
locations instead of duplicated into competing stores.

**.memory/ directory** is the repository's committed correction store for AI
agents. Read `.memory/README.md` and any learning artifacts in that directory
before working. Record verified corrections there when a session establishes
that a prior belief about ChairLift was wrong, and promote stable rules into
this file, `docs/skills/`, or `docs/design/` as appropriate. Never record
secrets or personal data because the directory is version-controlled.

## Agent read order

Before planning, implementing, or reviewing a change, read:

1. [`AGENTS.md`](AGENTS.md) for ChairLift-specific build commands, ownership,
   and application invariants.
2. [`docs/SKILL.md`](docs/SKILL.md), then the matching package in
   [`docs/skills/`](docs/skills/) selected by
   [`docs/skills/index.md`](docs/skills/index.md).
3. Common's linked factory-onboarding and agentic-model documentation when work
   crosses repositories, uses Hive, affects labels, or needs a human decision
   gate.

The former `docs/agents/skills/*.md` paths are compatibility aliases only; the
canonical agent knowledge base is `docs/skills/`.

## Project Bluefin factory

ChairLift's product and safety rules remain local. Project Bluefin Common is
the linked sidecar authority for cross-repository factory process; do not copy
its policy into this file. For local navigation, start at
[`docs/factory/README.md`](docs/factory/README.md).

| Topic | Common source |
| --- | --- |
| Factory onboarding | [`docs/skills/factory-onboarding.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/factory-onboarding.md) |
| Agentic operating model | [`docs/factory/agentic-model.md`](https://github.com/projectbluefin/common/blob/main/docs/factory/agentic-model.md) |
| Human decision gates | [`docs/skills/human-gates.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/human-gates.md) |
| Issue lifecycle and labels | [`docs/skills/label-workflow.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md) |
| Skill improvement | [`docs/skills/skill-improvement.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/skill-improvement.md) |
| Commit attribution | [`docs/contributing/style-guide.md`](https://github.com/projectbluefin/common/blob/main/docs/contributing/style-guide.md) and Common's `AGENTS.md` PR rules |
| Merge queue mechanics (local) | [`docs/skills/factory-onboarding/SKILL.md`](docs/skills/factory-onboarding/SKILL.md) |

Every completed factory task has two outputs: the requested repository change
and a knowledge decision. Preserve a durable lesson in the closest canonical
package under `docs/skills/`, or record a verified correction in `.memory/`.
Banned stale-artifact patterns: no committed session notes, no append-only
changelog/status files, and no "append here" instructions. ChairLift's normal
PR and review rules remain in force; Common's `common`-only direct-push
exception does not apply here.

Two of those imports are load-bearing often enough to name here, without
restating the policy behind them. First, an AI-authored commit carries **both**
attribution trailers — `Assisted-by: <Model> via GitHub Copilot` and
`Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>` — which
is what `.github/pull_request_template.md` already asks a submitter to confirm;
a single `Assisted-by:` naming some other runtime does not satisfy it. Second,
this repository merges through a merge queue, so `gh pr merge` enqueues rather
than merges and the gate question has to be settled before that call; the local
mechanics, including the GraphQL `dequeuePullRequest` escape hatch and its
narrow window, are in
[`docs/skills/factory-onboarding/SKILL.md`](docs/skills/factory-onboarding/SKILL.md).

## Org-wide decisions

Org-level conventions this repo follows are recorded as ADRs in
frostyard/core — see [docs/org-adrs.md](docs/org-adrs.md) for the list that
binds this repo. Change the ADR (in core) before changing behavior it covers.
