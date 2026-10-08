# AGENTS

ChairLift is a GTK4/Libadwaita system-management GUI for
[Bluefin](https://projectbluefin.io) and other bootc images, written in idiomatic Go using
[puregotk](https://codeberg.org/puregotk/puregotk) bindings — **no CGO**. GTK,
Libadwaita, and GLib shared libraries are loaded at runtime via `dlopen`. The UI
is YAML-configuration-driven; feature groups toggle on and off per host.

## Build, test, lint

The app builds pure-Go (`CGO_ENABLED=0`); the race detector needs CGO.

- `make build` — builds `build/chairlift`, `build/chairlift-updex-helper`, and
  `build/chairlift-helper` (all `CGO_ENABLED=0`).
- `make bump` — tags the next stable calendar point release `vYY.MM.N`, with
  N starting at 1 each month (for example `v26.10.1`, then `v26.10.2`). No
  alpha/prerelease option remains; historical prerelease tags are ignored by
  the sequence. Tag only clean, reviewed, CI-green `origin/main`, never a
  feature branch; GoReleaser publishes full releases (`prerelease: false`).
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
  the mill's deep gate calls this exact target.
- `make e2e` — builds both executables and runs `./test/e2e` (except the
  behave suite, below) **inside `ghcr.io/projectbluefin/dakota:testing`**
  through `test/e2e/dakota.sh`: the application's real `--help` surface, the
  dry-run GTK window, the screenshot walkthrough, a staged `make install`, and
  the installed privileged helpers' accepted and rejected argv. Every E2E
  target that needs a display — `make e2e`, `make e2e-atspi`,
  `make screenshots` — runs through that one script and nowhere else, so the
  host supplies only podman and Go. The script mounts the checkout at its own
  host path (every absolute path a target hands in stays valid), masks
  `/usr/share/chairlift` and `/proc/cmdline`, mounts the host Go toolchain
  read-only with shared module and build caches, and builds the suite's venv
  from `test/e2e/requirements-atspi.txt` with the image's interpreter.
  **There is no X11 anywhere in the harness.** Each GUI runs under
  `test/e2e/wayland_session.sh` inside a private `dbus-run-session`: headless
  Mutter (`--headless --no-x11 --virtual-monitor`), the compositor Bluefin
  runs, plus PipeWire and WirePlumber for its ScreenCast API. The script
  exports `GDK_BACKEND=wayland`, `XDG_SESSION_TYPE=wayland`, unsets
  `DISPLAY`, and sets `WAYLAND_DISPLAY` to the compositor's **absolute**
  socket path: a behave scenario launches the application with its own
  `XDG_RUNTIME_DIR`, so a bare socket name would resolve somewhere else, and
  an absolute path also makes a developer's live compositor unreachable. It
  refuses a `/run/user/*` runtime directory. The compositor's and PipeWire's
  sockets live in a short private `/tmp/chairlift-wl.*` directory rather than
  the test's runtime directory, because a Unix socket path is limited to 108
  bytes and a shard's artifact path overflows it; screenshot clients reach
  PipeWire through `PIPEWIRE_RUNTIME_DIR`. Dakota's container image does
  not list Mesa's `GL/default/lib` in `/etc/ld.so.cache`, and without it
  Mutter's GPU-less renderer cannot load llvmpipe and segfaults; the script
  puts that directory on the loader path and renders in software, as CI
  runners have no GPU. Keyboard input and screenshots go through
  `test/e2e/features/lib/wayland_remote.py`: input is dogtail 2.1's own
  `MutterInputBackend` (`dogtail.hermetic.mutter`, Mutter's RemoteDesktop
  API) driving `dogtail.rawinput`, with its pointer warm-up disabled because
  on Mutter 51 those motions swallowed the keys that followed; frames are one
  PipeWire buffer from Mutter's ScreenCast API. Input starts its
  RemoteDesktop session up front and waits briefly before the first key,
  because Mutter brings the virtual keyboard up asynchronously and drops a key
  sent before then; a one-shot input command also holds its D-Bus connection
  briefly after the last key, because Mutter drops undispatched events when a
  RemoteDesktop client vanishes.
  `internal/installcheck`'s `TestE2EHarnessIsolatesRuntimeAndPortals` holds
  every harness to the session script and `TestE2EHarnessHasNoX11Dependency`
  rejects X11 tooling in every executable file under `test/e2e` and
  `.github` and in the `Makefile` (prose is exempt, so docs can explain why).
  Startup polls the three readiness log markers for up to 30 seconds,
  requires one additional second of process stability, and terminates the
  private process group as soon as the smoke check passes.
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
  Every harness also isolates `XDG_RUNTIME_DIR` to a private 0700 directory
  and sets `GDK_DEBUG=no-portals`. Never run the GTK binary or dry-run tests
  directly against the developer's live `/run/user/<uid>` or host session
  bus. When testing in containers, never use Ubuntu or generic Debian
  containers — always use the official native Bluefin/Dakota environment
  (`ghcr.io/projectbluefin/dakota:testing`), which `test/e2e/dakota.sh` does.
  Never stop, mask, or unmount host desktop portals.
  With `E2E_COVERDIR` set, the GUI's counters reach it only because
  `cmd/chairlift` handles `SIGTERM`/`SIGINT` by quitting the application on
  the main thread, so `Run` returns and `main` exits normally; a process that
  dies by signal never flushes `GOCOVERDIR`, which left E2E coverage
  at 0% for every GTK package (issue #306). The dry-run smoke test
  asserts the `main: application exited` marker after its `SIGTERM`, so a
  regression fails `make e2e`. Keep the harnesses sending `SIGTERM` first and
  `SIGKILL` only on timeout.
- **Every destination is driven through AT-SPI, on Dakota.** `make e2e-atspi`
  runs `TestATSPIBehaveSuite` — the behave + dogtail suite in
  `test/e2e/features/`, in projectbluefin/testsuite's shape — through
  `test/e2e/dakota.sh` like every E2E test, under the headless Mutter session
  above. It runs on Dakota because what the tree announces depends on the
  GTK/Libadwaita release: Ubuntu's Libadwaita 1.5 publishes preference groups
  differently from what Bluefin ships, and 63 scenarios failed there that pass
  on Dakota. `make e2e` skips that one test because it alone takes most of the
  E2E time (it still runs the behave dry-run check for undefined steps).
  `features/environment.py` launches a fresh `--dry-run` ChairLift per
  scenario with its own HOME, runtime directory, config fixture
  (`@config.<name>`), stubs (`@stub.<name>`), an inert `brew`, a closed
  proxy, and an action journal; the page and shortcut inventories come from
  `internal/navigation`. The release and nightly workflows run
  `make e2e-atspi` after `make e2e`; the test workflow runs it as a separate
  `atspi` job split into parallel shards — behave tag expressions over the
  feature files' first tags, balanced by measured scenario time — and
  `internal/installcheck`'s `TestATSPIShardsCoverEveryFeatureOnce` fails when
  a feature is in no shard or in two, or when a scenario carries another
  feature's first tag (behave selects by effective tags), so a new feature
  cannot silently stop running on pull requests (add its tag to a shard). Both
  E2E jobs restore the suite venv through `.github/actions/dakota-venv`, keyed
  on the image's Python and the pinned requirements. `dakota.sh` sets
  `CHAIRLIFT_REQUIRE_ATSPI=1` so a missing stack fails rather than skips, and
  failed scenarios upload their accessibility tree and a `screen.png` in
  `atspi-results-<shard>` (`build/atspi/<tags|all>/`). Dakota harnesses
  source `test/e2e/dakota-image.sh` for the reviewed digest pin; CI rejects
  image overrides. Update the pin through the multi-arch-digest-pinning skill,
  never by restoring a floating tag. A user-facing feature lands with its
  scenario; a confirmed defect is written as a scenario tagged
  `@known_issue.<N>` rather than left untested. Never run the suite on a live
  session. The `gtk-headless-testing` skill carries the traps.
- `make install`'s default `PREFIX` is `/usr` — the only prefix under which
  the installed PolicyKit policy files land where `polkitd` reads them
  (`/usr/share/polkit-1/actions`) and the updex helper's installed
  path matches its fixed `pkexec` exec-path annotation (see the privilege
  boundary invariant below). It installs maintainer defaults at
  `/usr/share/chairlift/config.yml` and must never install or overwrite the
  administrator-owned `/etc/chairlift/config.yml`. ChairLift is distributed
  only through Homebrew: the cask installs GoReleaser's release archive,
  `chairlift_<version>_linux_<arch>.tar.gz`, and there are no deb, rpm, or
  apk packages. That archive also carries the privileged pieces — both
  helpers and the bootc, updex, and ublue PolicyKit policies in `data/` — so an OS image that
  wants the privileged features installs the helpers at `/usr/bin/` and the
  policies at `/usr/share/polkit-1/actions/` from it, and provides
  `/usr/libexec/bootc-update-stage` itself. `internal/installcheck`'s
  `TestGoreleaserArchivesCarryTheInstallSurface` holds every archive to that
  inventory, with the helpers under the file names their fixed `HelperPath`
  constants expect.

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
them a since-removed goreleaser packaging test this file cited as
enforcement; its name matched `-skip "Integration"`, so the filtered
unit-test step never selected it.

The separately invoked tests under `test/e2e/` are outside the
`./internal/...` unit-test scope by design. They are enforced by the E2E
workflow's explicit `make e2e` step; do not assume adding a test outside
`internal/` is enough without that dedicated gate.

There are no generated files and no codegen step; everything under version
control is hand-written Go, YAML, and data assets.

## Repository invariants

- **Desktop integration switches reflect GNOME state.**
  `internal/shellextensions` allows only the Tailscale QS and Bluefin Sync Folder
  UUIDs. It uses unprivileged `gnome-extensions` commands, reads without writing
  on load, and re-reads after changes instead of treating exit 0 as proof.
  `desktop_integrations_group` stays discoverable on unsupported hosts with
  insensitive switches. Dry-run skips mutation and restores observed state.
- **Legacy desktop launcher cleanup runs off the main thread.** Older frostyard
  installs left `~/.local/share/applications/org.frostyard.ChairLift.desktop`. At
  startup ChairLift asynchronously removes that file if and only if it is a
  regular file with `Type=Application` and an `Exec` whose program basename is
  `chairlift` or `chairlift-wrapper`; non-matching files, symlinks, and missing
  files are left untouched, and dry-run logs what would be removed without
  deleting.

An agent must not break these:

- **Privilege boundary.** State-changing operations that require root go
  through `pkexec` (PolicyKit) with fixed, installed polkit policies and fixed
  helper binaries only: `pkexec /usr/libexec/bootc-update-stage` (action
  `io.projectbluefin.chairlift.bootc.stage`),
  `pkexec /usr/bin/chairlift-updex-helper` (`internal/updex.HelperPath`, actions
  `io.projectbluefin.chairlift.updex.{enable-feature,disable-feature,update}`), and
  `pkexec /usr/bin/chairlift-helper` (`internal/ublue.HelperPath`,
  actions `io.projectbluefin.chairlift.ublue.*` — see the helper-extension
  invariant below for the full subcommand list)
  — always that fixed absolute path, matching the
  `org.freedesktop.policykit.exec.path` annotation, with the updex subcommand
  matching `org.freedesktop.policykit.exec.argv1`. The helper must strictly
  reject unsupported argv because PolicyKit does not validate arguments after
  action selection. ChairLift ships no passwordless PolicyKit rules; normal
  administrator authentication applies. Homebrew tap trust (`brew trust`) is
  deliberately per-user and does **not** use pkexec. Neither do gaming mode
  or the Developer Mode Pulp reader: both are **system-scope** Flatpaks
  (`flatpak install/uninstall -y --system`, #503), because Bluefin's policy is
  system-wide Flatpaks and its images configure Flathub only as a system
  remote, where a `--user` install cannot resolve a ref (#501). The `flatpak`
  CLI authorizes those operations itself through Flatpak's own PolicyKit
  actions (`org.freedesktop.Flatpak.app-install`, `runtime-install`, and their
  `-uninstall` counterparts); ChairLift must not wrap them in `pkexec`, add a
  helper subcommand or PolicyKit action for them, or add a user Flathub
  remote. Do not add arbitrary
  privileged command execution, broaden what pkexec runs, or route new
  mutations around the fixed helper/policy pair.
- **Neither an image reference nor a username crosses the ublue pkexec
  boundary.** `chairlift-helper` receives a validated channel, driver, or day
  word only, and derives the concrete `bootc switch` target itself from the read-only image
  descriptor plus the channel table; it derives the account to modify from
  the `PKEXEC_UID` pkexec sets, never from argv. Accepting either as an
  argument would let an authenticated caller switch the machine to an
  arbitrary image, or add an arbitrary account to the privileged developer
  groups. `internal/ublue`'s test asserts that no argument crossing the
  boundary contains a `/`.
- **Custom Command Menu integration is user-scoped and unprivileged.**
  `internal/devmenu` manages the Custom Command Menu extension
  (`org.gnome.shell.extensions.custom-command-list`) visibility for Terminal,
  Containers, and Ask Bluefin through out-of-process `dconf` CLI calls in the
  user session. Neither menu tuple values nor extension commands cross pkexec.
  Developer mutations apply only after a confirmed live developer-state
  transition, while Ask Bluefin visibility is toggled by user preference on the
  Agents page, never on view construction, state restore, failed
  authentication, or dry-run. Distro defaults are preserved via reset when
  matching, while `visible=false` is enforced as an override when the distro
  default is visible. The Ask Bluefin entry is the distro's: `IsAskBluefin`
  recognizes it by its label plus one of three commands — the
  `xdg-open https://ask.projectbluefin.io` web link, `chairlift --ask-bluefin`,
  and `/home/linuxbrew/.linuxbrew/bin/chairlift-wrapper --ask-bluefin`, which
  Bluefin's distro layer ships (projectbluefin/common#1396) because an
  extension's command runs without Homebrew on `$PATH`. ChairLift changes only
  that entry's visibility and never rewrites its command; a slot whose command
  the user changed is not claimed. The extension renders only slots listed in
  its `command-order` key, so Ask Bluefin reads as shown only when its tuple is
  visible **and** its slot is listed (Dakota moves the entry to `command12`
  while inheriting an order of 1..11). Showing it appends a missing slot to
  `command-order`, preserving the rest of the order, under the same
  reset-when-matching-the-distro-default rule; hiding it changes only the
  tuple's visible flag.
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
  `internal/updex`, and `internal/bootc` entry points. Flatpak post-apply
  reconciliation verifies that the specific pending refs applied for each
  executed scope are no longer listed post-apply, so newly appeared updates
  published between check and apply do not mark the run unchanged.
  `internal/views/updatepresent` is the equally pure presentation layer: it
  maps one immutable snapshot to one status line (plus a detail line only
  for the two failure phases) and an action label, so the shell's copy is
  testable on a headless host. The header reads wordmark → primary action
  (or the progress bar in its place) → status line; do not reintroduce a
  title and description between the wordmark and the action.
  `internal/views/update_shell.go` is widget wiring only — it holds no update
  state of its own and decides nothing the coordinator or the presenter
  already decided. Keep those three layers separate; do not move a phase
  decision into the widget file or a string into the coordinator.
  Its indeterminate progress bar pulses from one reusable GLib callback while
  checking or updating, not from provider snapshot arrivals; idle and disposal
  remove the timer. A quiet external command must not freeze the indicator.
  The shell hands `Coordinator.Check` each source's `updateflow.Policy` from
  `internal/window`'s `sourcePolicy`, which keeps `Configured`
  (`Config.IsGroupEnabled`) and `Supported` (the capability floor) apart:
  their conjunction is `effectiveEnabled`, but a source the host cannot back
  must read "Not available on this computer", never "Disabled by
  administrator". Do not collapse them back into one boolean map. The rest
  of the Updates page — automatic updates, system version, dedicated staging
  and Compare, unverified sources, and Advanced channel/driver controls — is
  `buildUpdatesPage`'s preferences page, mounted by `buildContentArea` beneath
  the shell's source rows through `UpdateShell.SetSecondaryContent`. Per-source
  rows belong to the shell; without the mount none of the secondary controls
  is reachable.
  A provider that learns only during `Check` that the host cannot back it
  (the operating-system source finding bootc not booted) wraps
  `updateflow.ErrUnavailable`; the coordinator then shows the source as not
  available and excludes it, rather than failing the whole check and
  printing the internal error as the page's description.
  The operating-system source must keep going through `internal/bootc`'s
  staging path. Adding a
  `bootc upgrade` route to `chairlift-helper` would break both the
  staging-ownership invariant below and the fixed-path contract an OS image
  relies on when it installs the helpers. The run's only privileged surface of its own is
  `restart`, offered only from the Operating system row once the snapshot
  reaches `PhaseRestartRequired` (whose page-level action is
  `updateflow.ActionNone`), where the header's one status line reads
  "Restart to finish updating". A header with nothing to show is hidden
  through `updatepresent.Presentation.ShowStatus`, not merely cleared, so it
  adds no spacing. Phase announcements (each phase's status line) come from
  the shell's visible toast overlay.
  The Operating system row carries a "Deployment staged" subtitle and a
  "Restart now" suffix that calls `UpdateShell.StartRestart` and in turn
  `ublue.Restart`. That
  phase is reached only when a source genuinely reports a restart is
  required — staging an already-current system completes without staging
  anything (on composefs `bootc.StageUpdate` asks the registry first and does
  not run the script, whose `bootc upgrade` fails there instead of exiting 0),
  so a successful OS source is not by itself evidence anything changed.
- **New privileged operations extend the ublue helper; they do not add a
  binary.** `chairlift-helper` carries fourteen subcommands
  (`channel-switch`, `dx-enable`, `dx-disable`, `restart`, `rollback`,
  `auto-updates-enable`, `auto-updates-disable`, `driver-switch`,
  `factory-reset`, `pin`, `unpin`, `kvm-enable`, `docker-enable`,
  `docker-disable`), each selected by exactly one PolicyKit
  action. Every one takes a fixed argv or a word validated against a closed
  set: no image reference, no username, no systemd unit, no delay, and no
  rollback or reset target crosses the boundary, because each
  would be a value an authenticated caller controls. `factory-reset` is the
  extreme case and the shape to copy: it is the most destructive privileged
  action ChairLift offers, so both the program (`bootc`) and its entire argv
  (`ubluehelper.FactoryResetArgs`, the fixed
  `install reset --experimental`, never `--apply`, which bootc documents as
  always rebooting at once) are spelled in the helper, and the
  GUI sends nothing but the command word — a factory reset has exactly one
  target, the image already booted, so there is nothing for a caller to name.
  `rollback` is the same shape with an even shorter argv.
  `pin <YYYYMMDD>` accepts only a real day no later than today UTC; `unpin`
  accepts no argument. `ubluehelper.PinArgs` recovers the booted stream and
  checks `imageinfo.KnownStream` before deriving a target (ADR-0017). Live
  pin tries the hyphenated tag first, then the dotted tag only on a registry
  404; any other error refuses. Unpin requires a dated booted tag and verifies
  the recovered stream. Both enforce container signature policy. Dry runs
  derive without registry access; both commands require a valid channel table.
  The GUI no longer calls `pin` or `unpin` (#522 withdrew the Powerwash
  calendar); the commands and their PolicyKit actions stay for a later
  surface.
  Channel switch, driver switch, pin, and unpin all run through the helper's
  `switchImage`, which falls back to the fixed `bootc rollback` when a
  composefs `bootc switch` refuses a target that is the rollback deployment
  (unpin's usual case); `internal/installcheck`'s
  `TestHelperImageSwitchesGoThroughSwitchImage` holds that wiring.
  `internal/ubluehelper`'s tests assert the argv validation
  per command, and the e2e boundary test asserts the installed binary
  rejects each shape. `cmd/chairlift-helper`'s dispatch carries a
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
  extending `docs/walkthrough.md` in the same change. One capture is not a
  page: `0-setup.png`, the explicit setup flow's Features start, which the
  runner takes first by launching with `--dry-run --setup` and dismisses
  with Escape before the page walk; the orphan check counts it beside the
  pages, and the walkthrough opens with it.
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
  The same package owns what a dismissed authentication looks like
  (`IsAuthDismissed`, `MessageIsAuthDismissed`: exit 126 or pkexec's
  "Request dismissed"; 127 is a real failure) and the brief
  `pkexec.CancelledMessage` ("Authentication cancelled") shown in its place.
  `Window.ShowErrorToast` turns a message carrying that text into the brief
  toast instead of a persistent raw-stderr error. A view whose failure toast
  is fixed text without the helper's output — Download, early updates, and
  the graphics-driver switch on Updates — never carries the text, so it
  classifies the error itself through `pageview.PrivilegedFailureToast`; a
  configured `sudo` maintenance script's runner captures no stderr, so
  `actionmsg.MaintenanceScriptFailure` does the same. The view still restores
  its control on that path.
- **Privileged integration ships in the release archive.** The Homebrew cask
  installs the GUI in user scope and cannot place root-owned files, so the
  release archive also carries the fixed-path updex and ublue helpers, the
  bootc, updex, and ublue PolicyKit policies, maintainer config, and the
  channel-table example. An OS image that wants the privileged features
  installs the helpers at `/usr/bin/` and the policies at
  `/usr/share/polkit-1/actions/` from that archive, and must provide its
  trusted stage helper at `/usr/libexec/bootc-update-stage` before enabling
  `bootc_updates_group`; ChairLift ships no OS staging implementation. Do not
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
- **Action feedback must remain visible.** Long-running cleanup, Developer,
  Gaming, Agent Mode, troubleshooting, and Livery switch operations use a
  native spinner built once, stopped on every completion path on the GTK
  thread. New result toasts use high priority so a persistent older error
  cannot hide the current action's error or confirmation; older errors remain
  queued for dismissal.
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
  `internal/navigation` package. It also decides route visibility from static
  group configuration: omit a functional page when all of its builder-backed
  groups are disabled, always retain Help, and compact Alt+number over visible
  pages. Mouse activation, window navigation actions, and the Powerwash
  detail's entry row and Back button must all call `Window.navigateToPage`,
  which applies the complete `navigation.Resolve` transition against
  `Window.navRoutes` (`navigation.VisibleRoutes`: the visible primaries plus
  the details they offer) — visible-row index, visible child, title,
  collapsed-layout content reveal, and the `Back` primary recorded in
  `Window.backRoute`. Only `navigateToPage` may move the sidebar selection: it
  records the row in `Window.shownRow`, and one `row-selected` handler,
  connected at build time, re-selects that row because `GtkListBox` selects
  whichever row gains focus (Tab, arrow keys) without emitting
  `row-activated`. The app and shortcuts dialog must use the window's same
  visible inventory. Do not reintroduce a second page or shortcut inventory in
  `internal/window` or `internal/app`. **The inventory holds two kinds of
  route.** A *primary* is a sidebar destination with a row, an Alt+number, and
  a shortcuts-dialog entry; there are seven, in this order: Updates, Apps,
  Agents, Features, Livery, Maintenance, Help. A *detail* is a focused screen
  reached from the primary named by its `Parent`: it has no row and no
  accelerator — `Shortcuts` and `Bindings` skip it structurally, so it can
  never be advertised or registered by accident — and `Resolve` enters it only
  when its ancestor is visible and the caller both offers and vouches for it,
  otherwise falling back to that ancestor and then to Help. A known primary
  the caller cannot enter — disabled by configuration, floored out by
  capability, or never built — has no ancestor and resolves straight to Help
  with `ok=true`; `Resolve` rejects only a name the inventory does not declare
  (#343). Powerwash is the
  live detail: a content-stack child of Maintenance, registered in the
  window's built pages so `Resolve` can enter it, whose row stays selected
  while it is shown, with `Back` resolving the recorded primary through
  `navigateToPage` rather than naming Maintenance itself. **A route's configuration
  identity is never inferred from its display name.** Each route carries
  page-qualified `Refs`, not a group list under a route-owned page field,
  because one destination can consume several namespaces at once: Powerwash's
  rollback controls are gated by `bootc_updates_group` on `updates_page` while
  its reset controls are gated by `reset_group` on `maintenance_page`. Keep
  those refs page-qualified and keep them held to `config.SchemaPages()` /
  `config.SchemaGroups(page)` by `internal/installcheck`. Two details in the
  inventory are easy to get wrong. The sidebar title for `applications_page`
  is "Apps", not "Applications" — the page name and the title are separate
  fields and only `internal/navigation` reconciles them. And there is no System
  page: "about this computer" belongs to GNOME Settings, which every host
  running this application already ships, so the system-version readout and the
  release-channel switch live on Updates, beside the thing that changes them.
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
  Per-control availability inside a group is not a capability: a
  helper-backed control reads `ublue.Status.Supports`, which `Detect` fills
  from the installed PolicyKit actions for `/usr/bin/chairlift-helper`,
  alongside the descriptor-derived fields the views already use.
- **First run is explicit and reuses the real pages.** Ordinary activation
  never reads onboarding disposition or starts a wizard. `--first-run`,
  `--setup`/`-s`, and the setup menu action share `PresentFirstRun`.
  `internal/firstrun.Pages` filters the existing visible navigation inventory
  into Features, Apps, Agents, Livery order; no welcome or update-preferences
  step is added. The window hides its sidebar during the flow and builds
  Back/Next/Finish/Dismiss controls once. Every step navigates through
  `Window.navigateToPage` and uses the page's existing widgets, gates, and
  operations; there is no dialog copy or `SetupHost` bridge. Each step then
  calls `UserHome.ScrollPageToTop`, because a page keeps its scroll position
  and a step entered mid-page hid its own heading.
  Next and Back perform no configuration mutation. Repeated explicit
  requests preserve the current step; an empty eligible inventory opens no
  flow. Finish records completed plus version, intentional dismissal records
  skip without demoting completed, and a crash records nothing. Disposition
  writes remain off the GTK main thread and are previews under `--dry-run`.
  `test/e2e/features/setup.feature` covers both explicit flags, normal launch,
  navigation, and dismissal; `firstrun.Keys` remain held to the shipped schema.
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
  Resolving brew is not enough for its children: a cask preflight runs tools
  beside brew by bare name (`rpm2cpio | cpio` for `goose-linux`), and a
  direct launch gives them no Homebrew on `$PATH`. `runBrewCommandAt` and
  the Goose launch's `llmman launch` therefore run with
  `homebrew.WithBrewPath`, which puts brew's directory first on `PATH`.
- **Homebrew update actions preserve known state.** Per-package upgrades and
  the top-level metadata update use `internal/views/actionstate` gates before
  spawning work. Failures and dry-run previews restore their controls without
  changing rows or counts. A live package success removes its row, decrements
  the count/badge, and refreshes; a failed refresh preserves that last known
  row/count state instead of replacing it with an invented zero.
- **Apps is Homebrew-only and refresh-safe.** Its only runtime configuration
  groups are `brew_bundles_group` and `brew_group`: app collections come first,
  installed casks and explicitly requested formulae next, then the Brewfile
  exporter (also gated by `brew_group`). No Flatpak inventory, external catalog
  launcher, or package search belongs here. Installed-package refreshes use
  `actionstate.RefreshGate` generations; stale workers must not replace newer rows.
  Installed formula/cask rows likewise confirm uninstall, formula rows confirm
  pin/unpin, and every row shares one gate across its mutation controls so
  actions cannot overlap. A live success completes the old controls and starts
  a generation-guarded inventory refresh; failure or dry-run restores them.
  Row gates come from the list's `actionstate.RowGates`, so a rebuild waits
  while any row action (or its confirmation) is running instead of replacing
  its busy controls with idle ones, and reloads once the last one settles.
  ChairLift's own cask (`pageview.IsSelfCask`) stays listed without an
  Uninstall button, so the page cannot delete the running application.
  Package-list export likewise holds an `actionstate.Gate`, shows a spinner
  and `Exporting…`, and restores the Export action after every outcome.
  A destructive `run*` entry point added to `internal/views` belongs in
  `internal/installcheck`'s `TestDestructiveActionsRequireConfirmation` inventory.
- **A visible retryable control must reset its action gate.**
  `actionstate.Gate.Complete` permanently rejects future starts; reserve it for
  controls that become permanently unavailable after live success. Driver
  switching, Powerwash, and Factory Reset restore their buttons after a run,
  so they reset their gates even after failure or dry-run. (The unified
  update run's primary action holds no `Gate`: `updateflow.Coordinator`
  admits one mutation at a time and the shell re-renders its button from each
  snapshot.) Roll Back is
  different: `bootc rollback` toggles the selected deployment, so a successful
  live click completes its gate and leaves its button insensitive; only a
  failure or preview resets it. A live success then swaps in a **Restart now**
  button (when the helper supports `restart`), because a queued rollback is
  not a staged deployment and the Updates page offers no restart for it
  (#520). `internal/views/actionstate`'s wiring tests guard both lifetimes.
  Dedicated staging, a live unified run whose operating-system source
  completed, and a live channel or driver switch refresh the changelog's
  Compare references from observed status (a switch also re-checks, so the
  Operating system row offers Restart now).
  `UserHome.OnUpdateFinished` refreshes the installed Homebrew inventory when
  its source completed and Compare when the OS source completed — after a
  unified run and after a live single-row update alike; the coordinator
  snapshot remains the only badge owner.
  A changed pinned image pair clears old diff rows; an in-flight comparison
  for the old pair must not render after the refresh.
- **Update inventory and badge have one state owner.** The pure
  `internal/updateflow.Coordinator` owns all source inventory and the badge;
  the shell renders snapshots and manual update actions — and the channel and
  driver switches, which replace the OS — share its admission.
  Failed observations preserve confirmed state, and previews mutate none of it.
  Do not restore separate counts or a provider-status owner in `UserHome`.
- **Developer options remain discoverable without privileged support.** WSL
  Mode defaults to nsl (an Ubuntu machine inside systemd-vmspawn and
  QEMU/KVM) with Lima (Ubuntu LTS VM) as an alternative backend, both with an
  explicit `/dev/kvm` permission floor; the fixed `kvm-enable` action grants
  access to the invoking account, effective after a new login. nsl's managed
  machine is `ubuntu`, or the `debian` machine an older ChairLift created
  (`devtools.ParseNSLList`); WSL Mode reports, starts, and probes that one,
  creates `ubuntu` only when neither exists (never a second machine), and turns
  off with `nsl stop <machine>` on that machine only, which keeps data
  (`nsl shutdown` would stop the user's own machines too). The backend
  choice is not stored: `wsl_backend` sets the default, and the first read
  follows an existing Lima machine when no managed nsl machine exists
  (`devtools.ResolveBackend`). A running machine stays stoppable even when
  the start prerequisites are unmet. Docker uses the
  fixed enable/disable actions for its system daemon and requires actual socket
  readiness for this session, not installed CLI tools alone. Missing installed
  helper actions leave the affected switches insensitive with an explanation
  rather than hiding the options. IDE and terminal-editor installs are
  individually selected, including one JetBrains Toolbox entry. Gaming selects
  typed application/runtime refs, installs missing ones system-wide, and
  keeps partial failures visible. Its inventory reads both scopes, so a copy
  an earlier release installed per-user counts as present and is not
  reinstalled. Remove Selected uninstalls each *selected* component from
  exactly the scopes it is observed in — the user copy with `--user`, the
  system copy with `--system` — and nothing unselected; its confirmation says
  system-wide copies go for every account. A system copy the OS image
  declares it ships (`/usr/share/flatpak/preinstall.d`,
  `/etc/flatpak/preinstall.d`, or
  `/usr/share/ublue-os/homebrew/system-flatpaks.Brewfile` — Flatseal on
  Bluefin and Dakota) is left in place and reported as such, so undoing
  gaming mode never removes a distro default for every account; an
  unreadable declaration fails the removal closed. A component with a
  removable copy left is a failure, not a removal.
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
  The retired `maintenance_page` groups (`maintenance_brew_group`,
  `maintenance_flatpak_group`, `maintenance_optimization_group`),
  `updates_page` groups (`update_all_group`, `sysupdate_updates_group`), and
  `features_page` groups (`ai_group` with its retired `ai_images`/`ai_model`
  fields, `troubleshooting_group`) carried by pre-26.09 Bluefin releases
  (v0.12.x) and removed by the 26.09-alpha Control Center reorganisation, and
  the `applications_page` groups retired by Homebrew-only Apps
  (`applications_installed_group`, `flatpak_user_group`,
  `flatpak_system_group`, `brew_search_group`, still in projectbluefin/common's
  shipped `config.yml`), are accepted by the same per-page rule, validated for shape/typo/sudo, and
  stripped prior to runtime decoding so they never re-enable removed behavior.
  `troubleshooting_group` first supplies omitted/null `help_page` fields,
  then moves on to `agents_page` with the old Help address.
  This must not add a navigable page or relax unknown-name or sudo validation.
- **CI action authority.** Third-party `.github/workflows/` actions use full
  40-character commit SHAs with reviewed version comments. Local `./` actions
  are exempt. Shared `projectbluefin/actions` production interfaces use managed
  `@v1`. Native review, actual main CI and released-source guards still
  precede writes. The installcheck scan enforces these boundaries across every
  workflow.
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
- **Every privileged dispatch point journals, unconditionally.**
  `internal/ublue.runHelper` and `internal/updex.runHelper` delegate to
  `internal/helperexec.Run`, which records every invocation before execution
  and then records its live outcome (`succeeded`, `refused`, `failed`,
  `timed-out`, or `cancelled`). The `executed` argv is helper-reported evidence
  parsed from the fixed helper's output, not independent execution proof.
  Dry-run records a suppressed invocation with no outcome record.
  This is not a `chairlift_e2e` stub: with
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
  produce an empty plan and start no worker. Pulp is a system-scope Flatpak
  from the system Flathub remote (already present in either scope means no
  install), authorized by Flatpak's own PolicyKit as above, and the catalog is
  one file under `~/.local/share/chairlift` — so no `pkexec` route is
  involved, Pulp's sandboxed store is never written
  to, and the feedback never claims a subscription was imported. The two
  optional outcomes are reported separately from the permission change, which
  has already succeeded: a failed install is a failed install, it rolls nothing
  back, and it must not read as a failed enable. Disabling Developer Mode is a
  clean no-op — it removes neither Pulp nor the staged file nor anything the
  user imported. The worker reads its plan from config on the main thread
  before it starts, is admitted one at a time by `developerFeedGate`, and
  reaches the toast only through `sgtk.RunOnMainThread` behind a nil guard.
- **Troubleshooting runs Goose in ChairLift's own profile, never the user's.**
  The Agents page's **Troubleshooting** group (`agents_page`
  `troubleshooting_group`, floored on Homebrew; `internal/views/troubleshoot.go`)
  holds the Goose row and the "Show Ask Bluefin in menu" switch; the Ask
  Bluefin menu entry and `--ask-bluefin` are paths into it, not its name. It is the
  one Goose surface: Help no longer has an Troubleshooting group,
  and `help_page.troubleshooting_group` is still accepted and migrated to
  `agents_page` (`internal/config/legacy.go` `legacyGroups`). `internal/troubleshoot`
  is the engine. A session runs in a dedicated profile under
  `$XDG_DATA_HOME/chairlift/troubleshooting` (`troubleshoot.Profile`):
  `GOOSE_PATH_ROOT` points Goose's config, data, and state at `goose/`, and
  `XDG_CONFIG_HOME` points the desktop app's Electron profile and
  single-instance lock at `desktop/`, where the user's `mimeapps.list` and
  `dconf` are symlinked so links and settings still behave. `~/.config/goose`
  is never read or written, and nothing reads
  `/usr/share/ublue-os/goose/config.yaml`. `agentmode.Launch` rewrites the
  profile atomically immediately before every launch (`Profile.Write`,
  dry-run gated): `RenderConfig` enables exactly `linux-tools` — the
  absolute `linux-mcp-server` with `--toolset FIXED --host-mode LOCAL_ONLY
  --no-search-for-ssh-key` — and `bluefin-knowledge`, the streamable-HTTP
  `https://mcp.projectbluefin.io/mcp` limited to `search_knowledge`; every
  other extension already in the profile file is carried over disabled, and
  Goose 1.53's platform extensions are written disabled; `hints.md` becomes
  the profile's `.goosehints`. It then runs `llmman launch goose-desktop
  --model bluefin-active` (`troubleshoot.Command`) with those two variables
  and `homebrew.WithBrewPath`; llmman hands Goose its provider through the
  environment, so no provider, model, or key is written by anyone. Readiness
  (`internal/agentmode`) is therefore the packages, not a file:
  `linux-mcp-server` and `goose-desktop` resolve to absolute paths, the host
  is x86_64 (the only architecture Goose Desktop is published for), and
  Agent Mode's daemon answers with a selected model. Packages are judged
  first, because a missing package is the one state the row resolves
  itself: its button becomes **Set Up**, which runs `troubleshoot.Setup`
  (tap `ublue-os/tap` only when something is missing, `linux-mcp-server`,
  then `cpio` before the `goose-linux` cask, whose preflight pipes the RPM
  through a `cpio` Bluefin does not ship; `ErrUnsupported` off x86_64).
  Each `ublue-os/tap` package is trusted by its qualified name right before
  its install (`homebrew.TrustFormula`/`TrustCask`, only when brew reports
  the tap untrusted) — never the tap wholesale.
  When Goose is already running in the profile (its `SingletonLock` names a
  live process on this host and was written during this boot, so a reused
  pid after a reboot does not count), `agentmode.Launch` opens Goose the ordinary
  way instead — `troubleshoot.ReopenCommand` starts `goose-desktop` in the
  same profile and Goose's lock hands it to the running session, so no
  second Goose starts; llmman would refuse a launch while that lock is held.
  GNOME's focus-stealing prevention may answer with a "Goose is ready"
  notification rather than raising the window (observed in the lab on a
  minimized window); nothing passes an activation token. A fresh launch
  holds the row busy until Goose holds that lock, exits, or 15 seconds pass,
  and the toast says whether it started or handed off
  (`agentmode.LaunchResult`, `pageview.GooseLaunchToast`), never that a
  window opened: a Goose that cannot draw (#544) still holds the profile.
  Both launches send Goose's output to ChairLift's stderr. Copy never
  claims a session's questions stay on this computer: knowledge searches go
  online. No pkexec route is involved, every install is user-scope Homebrew,
  and dry-run writes and installs nothing. Keep `brew tap` in Homebrew's
  `stateChangingCommands` so previews never tap for real.
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
- **The registry tag reader is read-only, and the GUI offers no calendar.**
  `internal/registrytags` (ADR-0013) lists a repository through the
  registry's `Link: rel="next"` pagination (`Client.Tags`), reads the day out
  of a tag name (`ParseBuild`), and resolves one tag to its digest and its
  `org.opencontainers.image.created` timestamp (`Client.Tag`). Every request
  goes through the `Client.HTTP` transport, the same seam `internal/sbom`
  uses, so no gate in `make ci` makes an outbound request — its tests drive a
  loopback `httptest` registry that models GHCR's pagination, its 404
  `MANIFEST_UNKNOWN`, and the fact that the response's `Content-Type` header,
  not the body's `mediaType` field, is the media-type authority (GHCR omits
  `mediaType` on some dated-tag manifests — verified 2026-09-22). No
  registry-supplied string may become a privileged switch target: the
  helper's pin and unpin call only `Client.Tag` to verify targets already
  derived from the descriptor, channel table, and validated day (ADR-0017),
  discarding the response data. A failed read is returned to the caller,
  never cached and never replaced by a previous answer. The Powerwash
  page's published-versions calendar (Published versions with per-day Pin,
  and Return to stream) is withdrawn (#522): the dated tags it listed exist
  only for the deprecated `latest` stream, so Powerwash offers Roll Back
  only, and the in-process `Catalog` cache and the `ublue.Pin`/`ublue.Unpin`
  client wrappers went with it. The helper's `pin`/`unpin` commands and their
  PolicyKit actions remain. The GUI's one remaining registry reader is
  `internal/bootc.CheckUpdate`, which calls `Client.Tag` on composefs hosts
  (below) and only compares digests.
- **Reading OS state never needs a password.** bootc 1.16 refuses
  `bootc status` and `bootc upgrade --check` without root, which left the
  operating-system source "not available" on every Dakota host (#381).
  `internal/bootc/composefs.go` answers both from world-readable state when
  the kernel booted a composefs deployment (`composefs=` on `/proc/cmdline`):
  deployment origins under `/sysroot/state/deploy`, the staged marker in
  `/run/composefs`, `/usr/lib/os-release`, and the registry digest of the
  booted tag. Only other hosts run the bootc commands. Do not add a pkexec
  route for reads: a prompt on every launch is the failure this removes, and
  staging keeps its existing fixed `bootc-update-stage` path.
- **Agent Mode is local, unprivileged, and reports observed readiness.**
  `agents_page` has one Agent Mode switch, visible model and preset controls,
  a "Models and Chat" row that opens llmman's own web UI
  (`aistack.WebUIURL`) while Agent Mode is ready, and the local API address;
  the Troubleshooting group below it is the invariant above. Unready model and
  launch controls stay visible and insensitive instead of disappearing.
  Peer/offload controls and their backend are removed; this surface manages
  this computer only.
  `internal/aistack` owns three artifacts: the generated installation Brewfile,
  `~/.config/systemd/user/chairlift-llmman.service`, and
  `~/.config/environment.d/10-chairlift-llmman.conf`. The fragment contains
  only `OLLAMA_HOST=127.0.0.1:17434`; no OpenAI key or endpoint is exported.
  llmman owns models and engine selection; Homebrew owns its binary. When
  llmman is missing, installing it taps `llmmanorg/tap` and trusts exactly
  `brew trust --formula llmmanorg/tap/llmman` before the bundle loads it,
  when `brew tap-info --json` reports the tap untrusted (Homebrew before 6
  has no tap trust and no `brew trust`, so nothing runs there):
  Homebrew refuses a formula from an untrusted tap, and turning Agent Mode
  on is consent to that one formula, not the tap.
  The unit uses the absolute resolved executable, binds loopback, and sets
  `LLMMAN_SHELL=off`, `LLMMAN_NOHISTORY=1`, and empty `LLMMAN_PEERS=` to
  override any legacy aggregation settings. Wildcard CORS is forbidden.
  Enable fetches the engine before writing the unit and readiness requires a
  bounded successful JSON-object response from `/llmman/node`, never unit
  presence or `systemctl is-active` alone. Toggle and preset mutations share
  one action gate; generation checks reject stale readiness and alias reads.
  Selecting a model saves its canonical `hf.co/...` alias target, restarts the
  owned user unit because llmman reads aliases only at startup, and waits for
  readiness before confirming selection. A missing alias means no selected
  model, not an error or an invented default.
  Startup reconciles only an existing owned unit against `RenderUnit` plus the
  exact live systemd `InvocationID`, committed as a unit comment only after
  successful reload/restart. An already-written file is not proof the daemon
  adopted it: missing or mismatched invocation stamps retry the migration.
  Enable, model selection, and migration share that restart/stamp boundary;
  current verified invocations do not restart, disabled hosts stay disabled,
  and dry-run writes none. Failure preserves the unstamped management unit and
  best-effort stops the old daemon. Live startup allows ten seconds for health
  after migration; preview retains its single bounded probe. Unrelated llmman
  configuration is never rewritten.
  Failure renders newly observed state, not the inverse of an optimistic flip.
  Every mutation respects `dryrun.Enabled()`. Disable removes only the owned
  unit and fragment, keeping binaries and models. A failed stop that
  cannot prove the service inactive preserves both files and the management
  handle. There is no pkexec route, Homebrew service, or container stack.
  Goose Desktop (`ublue-os/tap/goose-linux`) is the Agent Mode desktop GUI,
  launched by `agentmode.Launch` as described under Troubleshooting above.
  `chairlift --ask-bluefin` dispatches to Goose Desktop when all prerequisites
  are met — the launch, which writes the profile, runs off the GTK main
  thread — or presents Control Center on the Agents page naming the missing
  prerequisite, with identical behavior for
  cold and running instances.
- **Contribute to Bluefin launches the contributor appliance in a terminal through `ujust`.**
  `agents_page` offers a "Contribute to Bluefin" action row that runs read-only preflight off the GTK thread (`internal/contribute.Preflight`), at build and again each time the group is shown (one `ConnectMap` handler, generation-guarded and passive while a session holds the gate), checking `xdg-terminal-exec`, `ujust` on PATH, `ujust --summary` containing the `contribute` recipe, `podman` on PATH, and the Hive registration file at `${HIVE_CONTRIBUTE_REGISTRATION:-$HOME/.config/hive/contributor.env}`. When preflight fails, an actionable subtitle explains the missing requirement (for missing registration, a **Registration Guide** button opens `https://github.com/projectbluefin/contribute#configuration`; the URL is never spelled out as unclickable subtitle text) and leaves the button insensitive; a requirement fixed outside ChairLift is picked up the next time Agents is shown. Ready actions launch `xdg-terminal-exec ujust contribute` via `launcher.Run`, reporting failures asynchronously; when the session ends or fails to start, the button returns through a fresh preflight rather than a blind re-enable. Previews under `--dry-run` log only and launch nothing.
- **Printer applications are rootless quadlets, locked until their
  administration is authenticated, and never a false enabled indicator.**
  `internal/printerapp` writes one `.container` quadlet per driver family
  under `~/.config/containers/systemd` and drives it with `systemctl --user`
  in the invoking account; nothing here is privileged, and no `pkexec` route,
  helper subcommand, or PolicyKit action may be added for it. Each family is
  a digest-pinned multi-architecture index from the projectbluefin
  `*-printer-app` repositories, on host networking so IPP and DNS-SD reach
  the LAN; a pin bump needs the cosign/attestation verification #393 asks for
  first. ADR-0016 is the enable condition: an application may be enabled only
  when its web administration is authenticated or absent, and
  `printerapp.CanEnable` is that condition as a predicate — `Enable` calls
  it before its dry-run branch, and the Features page's **Printers** group
  (`printers_group`, floored on the `Podman` capability, i.e. `podman` on
  `$PATH`; `internal/views/printers_page.go`) renders a refused family as an
  actionable, non-enabled state: the row is shown, the switch is off *and*
  insensitive, and the subtitle says the administration page cannot be
  secured until the image accepts an administrator credential. No published
  image accepts one yet (ghostscript-printer-app#65, hplip-printer-app#51,
  gutenprint-printer-app#57), so today every family is locked. The contract
  those issues specify — the entrypoint reading `PRINTER_APP_AUTH_SERVICE`,
  `PRINTER_APP_ADMIN_GROUP`, and `PRINTER_APP_SERVER_OPTIONS` — may be named
  in comments and docs as what will be wired, but no ChairLift code reads or
  writes those names until an image ships them; and do not turn the lock
  into a hidden group or a switch that fails on every flip. Readiness and
  failure classification on the row come from
  `printerapp.Observe`/`ProbeActive`/`ProbeDiagnostics`/`Diagnose` —
  `systemctl --user is-active`'s state *word*, systemd Result/SubState
  properties, journal logs, and container image presence probes, off the main
  thread — never from the unit file's presence alone, and a present unit is
  never locked, so turning a family off always stays possible. Failure modes
  — missing Podman, rootless device access failure, unavailable image, plugin
  verification failure, and service crash — are classified as actionable
  non-enabled or failed states, never a false enabled indicator. A failed
  disable keeps the unit because the service could not be proven stopped.
  Hardware behaviour — printing through a device, USB passthrough, mDNS
  coexistence — is unverified and unwired; say so rather than claim it.
- **Livery remains one primary with independent task groups.** Profile
  Picture, App Launcher Icon, supported Top Bar Icon, and Files Icon keep their
  existing `livery_page` config keys and one built control set. Never add a
  wallpaper entry until `wallpapers_group` has a working builder (#200); do not
  clone the icon controls behind a second overview or a new sidebar route.

- **Livery shadows icon-theme names, and the theme it writes into is not
  always hicolor.** GNOME's app-grid button (`view-app-grid-symbolic`), panel
  menu (`PanelIconName(id)`, i.e. `chairlift-livery-<id>-symbolic`, via the
  Custom Command Menu extension's `menuicon-setting`), and Files application
  (`org.gnome.Nautilus`) are icon-theme overrides. KDE Plasma's app grid is
  the Kickoff applet: `internal/livery` scans the user's
  `plasma-org.kde.plasma.desktop-appletsrc` for every
  `plugin=org.kde.plasma.kickoff` section, installs the mark as
  `chairlift-livery-app-grid` in hicolor, then writes that icon name to each
  applet's nested `Configuration/General/icon` with `kwriteconfig6`. Its repeated
  `--group` arguments are required for KConfig's nested group semantics, and
  `kwriteconfig6` must remain in `allowedCommands`. When clearing the mark, it
  deletes the `icon` key across Kickoff applets that still point at
  `chairlift-livery-app-grid`, falling back to Kickoff's default icon. If no
  Kickoff applet is
  present/readable, the app-grid section reports unavailable and stays
  insensitive rather than claiming a write succeeded. KDE's Files name is
  `org.kde.dolphin`; KDE has no panel mark. A GNOME panel icon is a themed
  *name*, never a path: the extension builds `new St.Icon({icon_name: …})`, so
  an absolute path there renders nothing. Which theme directory receives the
  override is per surface and is load-bearing, because XDG resolves the
  current theme and its parents before falling back to hicolor: a name
  Adwaita already ships can only be shadowed inside
  `~/.local/share/icons/Adwaita`, while a name it does not ship
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
  path.** puregotk turns every `Connect*` callback into a purego trampoline
  cached by the *address* of the func variable and drawn from a fixed table:
  `maxCB = 2000`, with a hard `panic` when it fills. Nothing releases a slot
  when its widget is destroyed; `glib.UnrefCallback` exists but is only safe
  after the handler is disconnected, which no wrapper does. A closure created
  per row inside a function that reruns therefore burns slots until the
  application dies. Two patterns avoid it. The Livery page's project search
  connects one `GtkListBox::row-activated` for the page's lifetime and maps
  the activated row's index into the result set it last drew. Button rows in
  `applications_page.go` and `updates_page.go` use `buttonRoute`
  (`internal/views/widgets.go`): one `::clicked` func variable per list, held
  in a `UserHome` field, with `internal/views/signalroute` mapping each
  emitting button's address to its action; the list's `clear()` runs beside
  its row tracker's `Clear`, and a row removed on its own calls `forget`.
  Their confirmation dialogs share one `dialogRoute`, whose actions run once.
  A new per-row or per-dialog callback in a refresh path must use one of
  these, not a fresh closure.
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
  would otherwise rotate nothing and say so only in the journal. The unit's
  `ExecStart` must survive upgrades: `os.Executable` resolves a cask install
  to `<prefix>/Caskroom/chairlift/<version>/chairlift`, which the next
  `brew upgrade` deletes, so `livery.unitExecutable` names the prefix's
  `bin/` link instead when it resolves to the same file, deriving the prefix
  from the executable's path rather than `$PATH` (#491).
  Only the two foundation sections rotate; the app-grid mark
  is the user's own brand and is set once. ChairLift ships GSettings schemas:
  `io.projectbluefin.chairlift.livery` (appearance preferences),
  `io.projectbluefin.chairlift.updates` (user source toggles for updates), and
  `io.projectbluefin.chairlift.firstrun` (onboarding disposition) —
  holding only preferences with no file on disk to infer them from;
  `make install` recompiles the schema cache and `make schemas` builds them
  for a source tree. Do not add keys for state that can be observed.
- **Desktop settings have one backend across GUI and rotation.** Before GUI
  construction or headless rotation, `cmd/chairlift` calls
  `deskenv.ConfigureSettingsModules`: it discovers the installed native dconf
  module under `/usr/lib64`, `/usr/lib`, or the current-architecture multiarch
  directory and appends its module directory to `GIO_EXTRA_MODULES`. Existing
  entries and explicit `GSETTINGS_BACKEND` selections are preserved. This keeps
  Homebrew GLib and child settings tools reading the desktop's dconf store
  rather than a separate keyfile store; memory-backed fixtures stay isolated.
- **The Livery page must not write on load, and its network call stays behind
  a seam.** These bindings expose only the generic `notify` signal, which
  fires for sensitivity and subtitle changes too, so restoring saved state
  would otherwise look like a user edit. `applyLiveryState` assigns
  `liveryState` — seeded with the *resolved* combo ids, since an empty stored
  value resolves to index 0 and maps back to `cncf` — before touching a
  widget, holds `liverySuppress` across the restore, and arms `liveryLoaded`
  only at the end, including on the failure path. Its one load-time write is
  `livery.ReconcileRotationUnit`, which rewrites an existing rotation unit
  only when its `ExecStart` names a versioned Caskroom binary; it never
  creates a unit, touches a setting, or repoints any other path, and it is
  dry-run gated like every install. `internal/livery.Fetch` is
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
  irreversible. Its group is titled "Powerwash" rather than anything
  resembling cleanup, so a person hunting for disk space does not press it.
  Neither may run without the `AdwAlertDialog` confirmation in
  `internal/views/reset.go` first — that dialog's title and body come from
  `pageview.PowerwashConfirmation`/`FactoryResetConfirmation`, which is where
  the `--experimental` disclosure for Factory Reset's `bootc install reset`
  argv lives; do not move that text inline where it stops being tested.
  Powerwash needs no privilege (both steps run in the invoking account);
  Factory Reset is the new `factory-reset` action on
  `chairlift-helper` and takes no argument, since it has exactly one
  target — the image already booted. It is not offered on a composefs host
  (`bootc.ComposefsBooted`): `bootc install reset` needs OSTree storage and
  fails there after authentication.
- **The product name and the code name are different strings, and only one of
  them has an owner.** The application ships in Bluefin as **Control Center**;
  ChairLift remains the code name for the repository, the Go module, the
  binaries, the wrapper, the release archive, and the `io.projectbluefin.chairlift`
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

Documentation lives in `docs/`, with the local four-category layout from
frostyard/core ADR-0025. `docs/README.md` is the contributor index. Follow
Project Bluefin Common's task routing, canonical-source links, and progressive
disclosure; keep ChairLift's product and safety contracts local. New docs start
from their category's `TEMPLATE.md` and are indexed in `docs/README.md`:

- `docs/adr/` — why: repo-local decisions, immutable once accepted. Org-wide
  decisions go to frostyard/core instead, per
  [docs/org-adrs.md](docs/org-adrs.md).
- `docs/design/` — how it fits together: living architecture docs. Read
  `docs/design/overview.md` and `docs/design/package-managers.md` before
  architecture or provider changes. Keep current ownership, invariants, and
  failure modes here; link decision rationale rather than repeating it.
- `docs/specs/` — exact implemented contracts; reconcile stale descriptions
  against the code, and change behavior only alongside its implementation.
- `docs/plans/` — active implementation plans with an owning issue, explicit
  status, and executable "Done when" outcomes. Remove completed or superseded
  plans after their contracts and lessons are in the living docs and skills.
  Git history preserves retired plans; speculative ideas belong in issues,
  not a second backlog in the documentation tree.

After any change to source code, update relevant documentation in `AGENTS.md`
and `docs/`. Only humans edit `README.md`; AI agents must never modify or rewrite
`README.md`. A task is not complete without reviewing and updating relevant
documentation. For behavior, configuration, dependency, or install-layout
changes, also follow `docs/documentation-consistency.md`; current-state claims
must be checked against source/config/go.mod rather than copied from historical
plans.

Documentation cleanup must inspect every inbound link before removing an
artifact. Preserve accepted ADR decisions; repair a retired-plan reference
with a revision-pinned history link, not a rewritten decision. Verify local
links and cited source paths after reconciliation. ChairLift's issues and pull
requests are driven by Prow ([`.github/workflows/prow.yml`](.github/workflows/prow.yml),
approvers in [`OWNERS`](OWNERS)). Link Common's workflow page; do not copy it or
bypass native review/merge controls.

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
3. Common's linked agentic-model documentation when work crosses
   repositories, uses Hive, affects labels, or needs a human decision gate.

The former `docs/agents/skills/*.md` paths are compatibility aliases only; the
canonical agent knowledge base is `docs/skills/`.

## Project Bluefin factory

ChairLift's product and safety rules remain local. Project Bluefin Common is
the linked sidecar authority for cross-repository factory process; do not copy
its policy into this file. For local navigation, start at
[`docs/factory/README.md`](docs/factory/README.md).

Issues and pull requests are driven by Prow: `/kind`, `/triage accepted`,
`/lgtm`, `/approve` and the other commands, with approvers from
[`OWNERS`](OWNERS) and labels from the org config in `projectbluefin/.project`.
Read [how issues and PRs work here](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md)
before changing labels or state. `OWNERS` is generated; do not edit it here.

| Topic | Common source |
| --- | --- |
| Agentic operating model | [`docs/factory/agentic-model.md`](https://github.com/projectbluefin/common/blob/main/docs/factory/agentic-model.md) |
| Human decision gates | [`docs/skills/human-gates.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/human-gates.md) |
| Issues, PRs, labels and Prow commands | [`docs/skills/label-workflow.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md) |
| Skill improvement | [`docs/skills/skill-improvement.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/skill-improvement.md) |
| Commit attribution | [`docs/contributing/style-guide.md`](https://github.com/projectbluefin/common/blob/main/docs/contributing/style-guide.md) and Common's `AGENTS.md` PR rules |
| Merge queue mechanics (local) | [`docs/skills/factory-onboarding/SKILL.md`](docs/skills/factory-onboarding/SKILL.md) |

Every completed factory task has two outputs: the requested repository change
and a knowledge decision. Preserve a durable lesson in the closest canonical
package under `docs/skills/`, or record a verified correction in `.memory/`.
Banned stale-artifact patterns: no committed session notes, no append-only
changelog/status files, and no "append here" instructions. ChairLift's normal
PR and review rules remain in force; cross-repository factory guidance does
not authorize a direct push or bypass of ChairLift's native merge controls.

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
