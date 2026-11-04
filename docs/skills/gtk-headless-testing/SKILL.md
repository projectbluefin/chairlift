---
name: gtk-headless-testing
description: Use when deciding where tests can run without puregotk or GTK libraries, or when writing or debugging the behave AT-SPI suite under test/e2e/features.
version: 2.1.5
last_updated: 2026-11-04
tags:
  - testing
  - gtk
metadata:
  type: reference
---

# Never put a `_test.go` in a package that imports puregotk

**When it applies:** Adding or reviewing any Go test anywhere in this repo,
especially for `internal/views`, `internal/window`, or `internal/app`.

**The hard constraint (read this twice):** puregotk resolves GTK / Libadwaita /
GLib / **graphene** shared libraries by `dlopen` **at package-init time** — it
panics during package initialization if the libraries (or their pkg-config
metadata) are absent. The mill's gate host and GitHub's CI runners are
**headless and have none of these libraries**. Therefore:

> A test binary for *any* package that imports puregotk — directly or
> transitively — **panics before a single test function runs**. It is not about
> what the test body does. Merely having a `_test.go` file in such a package
> builds a test binary whose startup dies with, e.g.:
>
> ```
> panic: Path for library: graphene not found ...
>   codeberg.org/puregotk/puregotk/v4/graphene/graphene-box.go:305
> FAIL github.com/projectbluefin/chairlift/internal/views
> ```

`internal/views`, `internal/window`, and `internal/app` all import puregotk, so
they must stay **test-free** (that is why they show `[no test files]`). This is
an existing repo convention — do not break it by adding a test there, no matter
how carefully the test avoids constructing widgets or calling
`sgtk.RunOnMainThread`. Guarding, nil-checking, or constructing a zero-value
`&UserHome{}` does **not** save you: the panic is at init, before your code.

**The LOCAL TRAP that hides this:** a dev machine with GTK/graphene installed
(e.g. via linuxbrew) will run these tests *green*, and `make ci` will pass
locally, because puregotk finds the `.so` files. CI then fails because it has
nothing to load. **Never trust local success for a test in a puregotk package.**
The only safe signal is: the package does not import puregotk at all.

**What to do instead — extract the pure logic:**

- Move the decidable, widget-free logic into a small package that imports **no
  puregotk** (only `fmt`, `strings`, domain packages like `internal/homebrew`,
  etc.), and unit-test it there. Example from issue #57: the untrusted-tap
  upgrade message lives in `internal/views/trustmsg` (imports only `fmt`) and is
  table-tested headlessly; `internal/views` calls `trustmsg.UpgradeMessage(...)`.
- Test in the puregotk-free package, following the model of the existing
  `internal/bootc` and `internal/homebrew` tests (pure functions, table-driven).
- Name tests `Test…` (not `TestI…`, no "Integration") so they run under CI's
  `-run "^Test[^I]" -skip "Integration"` filter. That filter is **not** an
  escape hatch for GTK-needing tests — a skipped test still lives in its
  package's binary, which still panics at init. Skipping does not help; moving
  the logic out does.

**Widget-bound methods are not headlessly unit-testable.** A guard like
`if uh.someExpander == nil { return }` on a method of a puregotk-holding struct
(`UserHome`) cannot be tested without importing `views`. Cover it by the fix
itself plus the compliance review — do not add a `views`-package test for it.
If a behavior genuinely needs a test, that is a signal to extract its decidable
core into a puregotk-free package.

**Invariants that must stay in the GTK package are enforced by a static AST
scan in `internal/installcheck`.** Some rules — e.g. "every destructive action
reachable from `internal/views` passes through a confirmation path" (the
Powerwash/Factory Reset invariant in AGENTS.md) — live in the widget-wiring code
and cannot be extracted without losing the very wiring they guard. Rather than
add a `_test.go` to a puregotk package, write a headless scan in
`internal/installcheck` (pure, under `internal/...`, so the gates run it) that
parses the views sources with `go/parser` and asserts the invariant holds. The
model is `TestDestructiveActionsRequireConfirmation`: it walks each `UserHome` method that calls a destructive run and fails unless
that method both shows an `adw.NewAlertDialog` and gates on the `"confirm"`
response. Parse each file with `parser.ParseFile` over the directory's `.go`
files (`parser.ParseDir` is deprecated since Go 1.25). If a guard can be expressed as a source-level assertion,
it belongs here, not in a GTK test binary.

**Learned from:** issue #57's first mill run — a `_test.go` added to
`internal/views` passed locally (linuxbrew had graphene) but panicked on CI at
graphene load, failing the Unit Tests and Race jobs. Fixed by extracting the
pure message to `internal/views/trustmsg` and removing the views-package test.

## The behave AT-SPI suite

**When it applies:** Adding or changing a test that drives ChairLift through
the accessibility tree — anything under `test/e2e/features/`.

**Shape.** projectbluefin/testsuite's behave + dogtail pattern, minus the VM:
`run_atspi.sh` runs behave inside one private `dbus-run-session` and
`wayland_session.sh` — headless Mutter, the compositor Bluefin runs, with no
X server anywhere; `features/environment.py`
launches a fresh `--dry-run` ChairLift **per scenario** with its own HOME,
`XDG_RUNTIME_DIR`, config fixture, and `$CHAIRLIFT_ACTION_JOURNAL`. Scenario
tags select fixtures: `@config.<name>` (`fixtures/config/<name>.yml`, default
`everything`), `@env.KEY=VALUE`, `@stub.<name>` (`fixtures/stubs_<dest>.py`),
`@no-app`, `@known_issue.<N>`. Shared steps are in `steps/common.py`; a step
only one destination needs goes in `steps/<destination>.py`.

Keep state-word assertions in one behave step handler. A pattern such as
`entry checkbox is {state:w}` already matches `insensitive`; a second literal
step for that word prevents every feature from loading with `AmbiguousStep`.
The shared handler must assert checked, unchecked, and sensitivity states,
not drop the less common assertion to make step discovery pass.
Likewise, reuse `common.py`'s generic button click/state steps for Select All
instead of registering narrower button patterns that overlap them. Behave's
[parse matcher](https://github.com/behave/behave/blob/main/docs/tutorial.rst)
extracts named parameters from one definition; specialize the handler's state
dispatch, not the step registry. Context7 source: `/behave/behave`.

Intentional malformed-input fixtures must stay malformed. Exclude their exact
paths from syntax-lint hooks, not the entire fixture directory or hook.

**Run it.** `make e2e-atspi [ATSPI_TAGS=@tag]` locally and in CI; both run
`test/e2e/dakota.sh`, which runs the Go gate in
`ghcr.io/projectbluefin/dakota:testing` with `CHAIRLIFT_REQUIRE_ATSPI=1` (a
missing stack fails instead of skipping). Not on the runner's Ubuntu: its
Libadwaita 1.5 publishes an AdwPreferencesGroup as an unnamed panel (only the
inner list carries the title), where Dakota publishes a `grouping` named by
the title — 63 scenarios passed on Dakota and failed on Ubuntu for that and
similar version drift. `make e2e` skips `TestATSPIBehaveSuite` but keeps the
behave dry-run check. Artifacts:
`build/atspi/<tag|all>/` locally, one `atspi-results-<shard>` artifact per
shard in CI —
`behave.log`, JUnit XML, `mutter.log`, and per scenario `chairlift.log`,
`journal.jsonl`, and on failure `tree.txt` (the accessibility tree) and
`screen.png`. Read `tree.txt` before guessing at a lookup.

**Input and screenshots are native Wayland.** `atspi.press`/`type_text` call
`dogtail.rawinput` through dogtail 2.1's own `MutterInputBackend`
(`dogtail.hermetic.mutter`, Mutter's RemoteDesktop API), installed by
`features/lib/wayland_remote.py`; Dakota ships the `ponytail` Python module
but not `gnome-ponytail-daemon`, so dogtail's default Wayland path cannot
work there. Screenshots are one PipeWire frame from Mutter's ScreenCast API.

**Traps, each learned the hard way:**

- **Free runner space before installing Go or pulling Dakota.** Every
  Dakota-consuming workflow job uses
  `projectbluefin/actions/bootc-build/setup-runner@v1` with
  `storage-backend: remove-software` and `update-podman: "false"` before
  `actions/setup-go`. The shared cleanup removes `/opt/hostedtoolcache`;
  putting it inside `dakota-venv`, after Go setup, deletes the toolchain the
  harness needs. Cleanup applies to E2E, every AT-SPI shard, Docker,
  releases, nightly compliance, and both screenshot capture workflows.
  Do not retry image-extraction `no space left on device` failures without
  first correcting runner capacity. Source: the shared
  [setup-runner action](https://github.com/projectbluefin/actions/blob/v1/bootc-build/setup-runner/action.yml).
  Step ordering and shared filesystems: Context7
  `/websites/github_en_actions`,
  [jobs and steps](https://docs.github.com/en/actions/get-started/understand-github-actions).
- **A cached Dakota venv is not a host executable.** Its `bin/python` can
  symlink to an absolute interpreter path that exists only inside the pinned
  image. A host `test -x` then rejects a working cache on Ubuntu, and a plain
  `mv partial existing-directory` nests another venv instead of publishing it.
  `dakota.sh` checks executability inside Dakota with the cache mounted
  read-only, and replaces an unusable cache with a non-nesting rename after
  the new environment is complete. The ordinary and live Docker E2E runs
  deliberately reuse that same cache in one job.
- **The host cannot run the suite.** `/usr/share/chairlift/config.yml` on a
  Bluefin host outranks every `config.dev.yml` fixture; `before_all` refuses
  to start rather than test the wrong file, and `environment.py` also checks
  the `Loaded config from <fixture>` marker. The container masks the
  directory with `--tmpfs …:notmpcopyup` — plain `--tmpfs` copies the image's
  file into the tmpfs.
- **Containers inherit the host kernel command line, not its deployments.**
  A composefs host's `/proc/cmdline` makes the real bootc reader look for
  `/sysroot/state/deploy` even inside the fixture container, bypassing its
  fake `bootc` and hiding System version. `dakota.sh` binds `/dev/null`
  read-only over `/proc/cmdline` so fixture tests use their own bootc answers.
  This is test isolation only; a booted Dakota VM must keep its real cmdline.
- **A Wayland client is never told where its window is.** AT-SPI reports
  every window at (0, 0) and `position` is useless, so nothing can be
  clicked by position. Activate through AT-SPI actions (`atspi.activate`) or
  the keyboard; list rows, which have no action, are reached with arrow keys
  from the focused row and Return.
- **Dakota's container loader cache does not list Mesa.** Mesa lives in
  `/usr/lib/<triplet>/GL/default/lib`, which a booted system registers but
  the image's `/etc/ld.so.cache` does not; headless Mutter then cannot load
  llvmpipe and segfaults in `meta_renderer_egl_set_renderer_gpu_data`.
  `wayland_session.sh` puts the directory on the loader path. Under
  `--userns=keep-id` the container cannot run `ldconfig` itself.
- **A Unix socket path is limited to 108 bytes.** Mutter refuses to start
  (`socket path ... exceeds 108 bytes`) when its socket would sit in a
  shard's runtime directory under `build/atspi/<tag expression>/`.
  `wayland_session.sh` keeps the compositor's and PipeWire's sockets in a
  short `/tmp/chairlift-wl.*` directory instead.
- **A new feature file needs a CI shard.** The test workflow runs the suite
  as tag-expression shards; add the feature's first tag (the tag block above
  `Feature:`) to one of them in `.github/workflows/test.yml`, and never reuse
  another feature's first tag on a scenario, or
  `TestATSPIShardsCoverEveryFeatureOnce` fails.
- **dogtail picks X11 or Wayland when it is imported.** `rawinput` reads
  `XDG_SESSION_TYPE` at import time and, unless it says `wayland`, sends
  XTest events that do nothing under Mutter. `wayland_session.sh` exports it
  before any Python starts; setting it later has no effect.
- **dogtail's pointer warm-up eats keys on Mutter 51.** `MutterInputBackend`
  opens each session with zero-length pointer motions measured on Mutter 50;
  on 51 the keys that follow were lost. `wayland_remote` sets
  `DOGTAIL_WARM_UP_EVENTS=0`; the suite never moves the pointer.
- **Mutter drops undispatched input when its client disconnects** ("D-Bus
  client with active sessions vanished" in `mutter.log`). A one-shot input
  command must stay connected briefly after its last key, as
  `wayland_remote.py`'s command line does.
- **The first key after a RemoteDesktop session starts is lost.** Mutter
  brings the session's virtual keyboard up asynchronously after `Start()`
  and offers no readiness signal; `agents.feature:18`, the first scenario to
  press a key, failed 3 of 3 runs until `wayland_remote.install_input` started
  the session up front and waited `KEYBOARD_SETTLE` before the first key.
- **GtkMenuButton is two nodes.** An action-less `button` wraps the `toggle
  button` carrying `click`. `is_button` requires an action so the wrapper
  never shadows it.
- **Popover menu items are nameless** on this stack (`@known_issue.347`);
  `menu_item()` falls back to the `keyshortcuts` attribute, then model order.
- **AdwAboutDialog is a separate top-level frame** at the suite's window
  size; `current_dialog` searches in-window `dialog`/`alert` nodes first, then
  extra top-level frames.
- **The content pseudo-root is not an AT-SPI node.** `common.content` returns
  `_ContentRoot`, so traverse it with `atspi.search_nodes`, not `descendants`.
  Named `grouping` nodes also include window and page wrappers. For exact
  preference-group order, locate a known group and compare its shared parent's
  grouping children; do not slice off a guessed number of wrappers. See the
  Apps ordering scenario in `steps/apps.py`.
  Compact sections without a visible group heading may publish an unnamed
  grouping around their named expander header. The shared `apps.group` lookup
  resolves either form and returns the nearest owning group; ordering checks
  must likewise use the header for unnamed sections rather than omitting them.
- **Collapsed defaults need a passive assertion.** Interaction helpers may
  expand a section before finding its controls, but the assertion that a section
  starts collapsed must not use that helper. Exercise saved enabled switches as
  well as empty preferences: restoring an enabled setting must not expose the
  whole section. Livery's compact-section scenario checks each header before
  expanding it and verifies that expansion writes no settings or icon files.
- **A moved control needs every driver migrated.** Docker and the Developer
  Mode switch live on the Developer Mode detail, not the Features overview.
  The live `docker_system_integration.py` driver must enter that detail on
  every launch, including its new-group login, using the same keyboard row
  navigation and Back-to-Features readiness assertion as the behave suite.
  Searching the overview for the old switch fails before any helper action;
  a dry-run behave pass alone does not cover the separate live driver.
- **Execute generated startup argv, including its second run.** Pinned
  Dakota's uutils cp 0.12.0 accepts the first `cp -PT --remove-destination`
  from current to absent active, then rejects the existing symlink-to-directory
  destination on restart. Static unit-string assertions cannot catch that.
  `TestDockerRuntimeActivation` executes the generated locked `ln -P -fT`
  activation repeatedly and verifies serialization against the install lock.
  Keep actual link metadata and SELinux enforcement in live failure artifacts;
  neither a disabled host nor reading policy proves enforcing execution.
- **An off switch cannot provide a stop prerequisite.** A running Docker
  daemon that this account cannot access reads off. Legacy-runtime migration
  must not tell that person to toggle it off or merely Retry. Preserve the
  runtime admission refusal and surface the existing fixed terminal disable
  command as a visible prerequisite, explicitly warning that it stops running
  containers. Test the exported provider's real helperexec failure path and
  the rendered row, not only the helper's stderr.
- **Never read tags from `scenario.effective_tags`.** It is a set, so
  `@stub.a @stub.b` ran `b` before `a` and a refining stub was overwritten by
  its base. `environment.py`'s `before_scenario` builds the list from
  `feature.tags`, the rule's tags and `scenario.tags` in written order, so
  stubs apply left to right and a later stub may replace an earlier one's fake.
- **A switch cannot be moved back inside its own `state-set` emission.**
  `guardedSwitch.set` called synchronously from the user-change handler is
  overridden when the emission finishes, so a refused toggle stays on. Revert
  through `sgtk.RunOnMainThread` (the refused branch of `onChannelToggled`).
- **dogtail.tree connects to the bus at import.** The helper library imports
  it lazily so `TestATSPIFeaturesHaveNoUndefinedSteps` (behave `--dry-run`)
  needs no display. dogtail's `checkForA11y` must be off before import: the
  suite uses `GSETTINGS_BACKEND=memory`.
- **Homebrew GLib can silently use a different settings store.** A keyfile
  write from Homebrew `gsettings` is not proof that native GNOME or a user
  rotation service sees it. `deskenv.ConfigureSettingsModules` runs before
  GUI construction and headless rotation, appending an installed native
  `libdconfsettings.so` module directory to `GIO_EXTRA_MODULES`. It preserves
  explicit `GSETTINGS_BACKEND=memory`, so isolated fixtures still use memory.
  For real preference proof, observe the value through the desktop's native
  tool as well as the app's tool environment; never override a memory fixture
  to reach the live user's store.
  A CGO-disabled launcher also calls native `g_setenv` before constructing GTK;
  changing Go's environment alone is insufficient for native library readers.
- **A current unit file is not a current daemon.** Reproduce interrupted user
  service migrations with the replacement file already written and the old
  daemon still running. Native HTTP health does not prove policy adoption.
  Agent Mode accepts a matching live `InvocationID`, or a loaded current unit
  (`NeedDaemonReload=no`) whose main process started strictly after its mtime.
  Proven adoption re-stamps without restarting; unproven policy takes the
  reload/restart path. A matching stamp left dirty by an older release gets
  only `daemon-reload` when the manager successfully reports
  `NeedDaemonReload=yes`; failed observations do not invent reload state.
  Every invocation-stamp write must then `daemon-reload`, even though it changes
  only a comment: its new mtime otherwise invalidates adoption after an automatic
  service restart. Failed stamp-commit reloads remove the new stamp.
  Model `NeedDaemonReload` from actual file writes in test doubles rather than
  pinning it to `no`, and test automatic invocation changes after both adoption
  and restart. Observe the ID, reload state and page readiness on a private user
  manager. Do not add a root read route: even same-UID `/proc/<pid>/environ`
  reads may be denied.
- **behave drops scenario-scoped context attributes** at scenario end; run-wide
  counters live in `context.config.userdata`.
- **Homebrew readers escape the process group.** Teardown kills the group,
  then every process whose environment carries the scenario's unique journal
  path; the Go gate drains the whole session before removing the output.
- **Real `brew` is reachable** on GHA runners and in the container through
  the fallback path; stub every external tool an assertion depends on with a
  fake first on `PATH`.
- **A skipping gate proves nothing.** Before the E2E job installed the stack,
  the suite skipped in CI and a real failure on `main` went unnoticed.
- **Assert a control's own name, not `label_text`.** `label_text` falls back
  to descendant text, so a button that publishes an empty accessible name
  still passes. On GTK 4.24 a button built with `gtk_button_new_with_label`
  and then given another child (`SetChild`) reports an empty name even with
  an explicit `LABEL` property; build such buttons with `gtk.NewButton()`
  (#508, every Apps collection Install button).
- **An `AdwActionRow` activatable widget is named by the row title.**
  `SetActivatableWidget` adds a `LABELLED_BY` relation to the title, so a
  "Choose…" button in an "Avatar" row (whose title describes the current
  picture, e.g. "No Picture Set") announces itself as "No Picture Set" — a
  name without its visible label (WCAG 2.5.3). Call
  `ResetRelation(gtk.AccessibleRelationLabelledByValue)` after
  `SetActivatableWidget`, and have the scenario click the button by its
  visible label.

**Learned from:** #366/#375 (turning the probe on in CI) and #357's Wave 0,
which replaced the one-probe-per-feature TSV harness — every community PR
(#372, #373) had forked the runner script and conflicted — with this suite.

## Host Desktop & Portal Isolation for E2E Testing

**The rule:** Never run the GTK binary, `make e2e`, or `make screenshots` directly against the developer's live desktop session, `/run/user/<uid>`, or default session bus.

**Why:** Headless GTK tests running under `dbus-run-session` without an isolated runtime environment inherit the host's `$XDG_RUNTIME_DIR` (`/run/user/<uid>`). When GTK or Flatpak commands run, they can reach desktop portals (`xdg-desktop-portal`, `xdg-document-portal`) over the session bus. This risks unmounting or disconnecting `xdg-document-portal`'s FUSE mount at `/run/user/<uid>/doc`, causing host Flatpak sandboxes (`bwrap`) to immediately fail with:
```
bwrap: Can't find source path /run/user/<uid>/doc/by-app/<app>: No such file or directory
```

**Required harness isolation:**
1. Launch every GTK process inside `dbus-run-session -- test/e2e/wayland_session.sh`. It refuses a `/run/user/*` runtime directory, starts its own headless Mutter, and hands GTK that compositor's **absolute** socket in `WAYLAND_DISPLAY` with `GDK_BACKEND=wayland` and `DISPLAY` unset, so neither a live compositor nor a bare socket name resolved against another runtime directory can be reached.
2. In Go E2E tests (`test/e2e/e2e_test.go`, `test/e2e/walkthrough_test.go`), allocate a private `0700` directory and pass `XDG_RUNTIME_DIR=<dir>` in `cmd.Env`; in shell scripts (`test/e2e/run_atspi.sh`), export `XDG_RUNTIME_DIR="$OUTDIR/runtime"` (mode `0700`).
3. Always export `GDK_DEBUG=no-portals` in every harness.
4. Never add an X server or X client to the harness. `internal/installcheck/e2e_portal_isolation_test.go` enforces items 1–3 and rejects X11 tooling in every executable file under `test/e2e` and `.github` and in the `Makefile`; Markdown is exempt so documentation can explain the history.
5. Never execute `systemctl --user mask`, `stop`, or unmount commands against host desktop portals.

## Container Testing Environment for ChairLift

**The rule:** When testing ChairLift locally in containers, **NEVER** use Ubuntu or generic Debian containers. Always use the official native Bluefin/Dakota environment (`ghcr.io/projectbluefin/dakota:testing`) with the standard Homebrew tooling and environment.
**Why:** ChairLift is specifically built for the Project Bluefin ecosystem. Generic Debian/Ubuntu container environments do not reproduce the Bluefin/Dakota filesystem layout, configuration paths, packaged tooling, system integration, or Homebrew setup. Testing or generating captures in generic Debian/Ubuntu containers produces inaccurate results, missing icons or themes, and incorrect capability evaluations.

- The gated Dakota harness uses the reviewed digest in
  `test/e2e/dakota-image.sh`; follow the multi-arch-digest-pinning skill to
  update it. For a local live-QA experiment with a floating override, pull it
  first and record its digest and creation time. Such an override is rejected
  in CI and does not validate the committed pin.
- Keep the shipped `/usr/share/chairlift/config.yml` visible during live QA.
  Mask it only for explicit configuration-fixture tests.
- `chairlift_atspi.page_root` is a query pseudo-root, not an AT-SPI node.
  Traverse it through `search_nodes` when writing evidence, or dump a real
  window node; passing it directly to `dump` yields an empty-looking tree.

### Running the E2E suites with Dakota

`make e2e`, `make e2e-atspi`, and `make screenshots` all run through
`test/e2e/dakota.sh`. It needs podman and Go on the host, mounts the checkout
at its own path and the host's Go toolchain read-only, and creates the venv
from `test/e2e/requirements-atspi.txt` with the container's interpreter.
It mounts `GOCACHE` and `GOMODCACHE` read-write, so the release workflow's
goreleaser job sets `cache: false` on `actions/setup-go`: caches the image
wrote must never reach release binaries.
`CHAIRLIFT_ATSPI_KNOWN_ISSUES=1` also runs `@known_issue` scenarios.
This fixture suite runs on bare headless Mutter; it is not a live GNOME Shell
desktop walkthrough. For live-shell QA, read the target lab repository's local
router and current workflow before selecting its supported isolated Wayland
runner. Reuse that workflow rather than inventing another disk installer; keep
its deployment flags and installation prerequisites in the lab's source docs.
One harness accommodation exists for that runner: in a GNOME Shell session,
Mutter's focus-stealing prevention leaves each new window unfocused, so
`environment.py` activates it through `Shell.Eval` after launch. On bare
Mutter there is no `org.gnome.Shell` on the bus, and the step is skipped.

### Generating walkthrough screenshots

`make screenshots` captures inside Dakota itself, so a host only needs podman
and Go. On a host without podman, run it from the `dakota-fedora` Lima VM
(`limactl start dakota-fedora`, `brew install go` in the VM) and copy the PNGs
out with `limactl copy`.

## Documentation source

GNOME Shell nested-session isolation: Context7
`/git_gitlab_gnome_org/gnome_gnome-shell`,
[`docs/building-and-running.md`](https://gitlab.gnome.org/GNOME/gnome-shell/-/blob/main/docs/building-and-running.md).
The lab's current Wayland runner remains the source for its deployment flags.

GLib/GIO settings modules: Context7 `/gnome/glib`,
[`Running GIO applications`](https://docs.gtk.org/gio/running.html).
Configure `GIO_EXTRA_MODULES` before GSettings is used, preserve any explicit
`GSETTINGS_BACKEND`, and synchronize the native environment with
`g_setenv ("GIO_EXTRA_MODULES", modules, TRUE)`. Module-loading variables are
not an elevated-privilege route: the GUI loads only the installed native
system backend, and the fixed pure-Go privileged helpers do not load GIO.

Systemd invocation identity: Context7 `/systemd/systemd`,
[`systemctl`](https://www.freedesktop.org/software/systemd/man/latest/systemctl.html).
`systemctl --user show chairlift-llmman.service --property=InvocationID --value`
observes the service invocation; `daemon-reload` alone does not restart it.
Unit-file comment writes still change the fragment mtime, so the manager must
reload after invocation-stamp writes to clear `NeedDaemonReload`.
llmman's [peer configuration source](https://github.com/llmmanorg/llmman/blob/main/src/config.rs)
uses an explicitly set empty `LLMMAN_PEERS` before saved aggregation settings.

## GNOME extension controls

Stub `gnome-extensions` on the scenario's PATH for extension-switch scenarios.
Use both a normal and reversed enabled list: the reversed list catches a view
that paints its suggested defaults instead of the user's actual preferences.
Assert that opening the page and toggling in dry-run never execute `enable` or
`disable`; keep real command failure and UUID allowlist tests in the pure-Go
provider. A successful CLI exit alone is not proof of changed GNOME state, so
production handlers must reload and compare before confirming the switch.

## Empty status headers and announcements

Keep status-header visibility decisions in the pure presenter, hide a header
box that has nothing to show rather than only clearing its strings, and retain
it with an action or active progress. (An `AdwStatusPage` is worse still:
clearing its strings leaves its internal padding allocated.) Emit phase
announcements from a widget that stays mapped, such as the shell's toast
overlay. When a status line can repeat a row's words ("Up to date"), give it
an accessible description and find it by that in the suite, not by text.

A staged-deployment fixture needs both a fake `bootc status` with a non-null
staged entry and the fixed stage-helper marker. The Dakota AT-SPI container
binds `/usr/bin/false` at `/usr/libexec/bootc-update-stage` for detection only;
the suite remains in `--dry-run`. Its masked `/proc/cmdline` ensures the
fake `bootc` is used rather than reading the host's composefs deployment.
Verify the staged state and the return to a visible pending-update panel;
an unstaged screenshot or a string-search test cannot prove either layout.

## Launch-and-exit setup actions

For a final action that launches another app and exits, assert process exit and
its normal-exit log before inspecting saved previews; the accessibility tree is
gone. Prove unprivileged launch suppression with a recording `gtk-launch` stub
and an empty calls file: a preview log and the privileged action journal alone
cannot prove that no desktop app launched. Keep the native quit action disabled
across save/launch, not only `close-request`, so Ctrl+Q cannot interrupt it.

## Collections hosted outside Apps

Exercise a collection on its owning page with Apps inventory disabled as well
as enabled. Share the install gate, but never assume the Apps widgets exist.
Keep discovery fixtures in the scenario directory and assert that page opening
runs only bundle checks, while preview actions preserve the observed subtitle.
Use `KDE` only to exercise Plasma: it switches
other Livery sections to their Plasma paths.

## Asynchronous collection metadata

When a package subtitle changes after expanding a collection, the AT-SPI
scenario should wait for the final subtitle and assert the metadata command.
Keep package-name assertions separate: a visible identifier alone does not
prove that the description lookup completed. Stub description queries separately
from installed inventory queries so uninstalled collection entries are covered.

## Setup intent and asynchronous work

Keep pending first-run choices separate from installed state. A late status
probe may update an untouched default, but must never override an explicit
choice. Bind an apply completion to the selection object of that wizard
session, not merely its step index: dismissing and reopening setup can otherwise
let an old callback advance the new session. Refresh installed state when setup
ends; Apps may have changed packages while setup was open.

For Bluefin CLI, test selection transitions in `internal/firstrun` and file and
package lifecycle behavior in `internal/bluefincli`. Smoke the embedded Bash
payload with real Starship, direnv, mise, and zoxide binaries in an isolated HOME
and XDG directories: evaluate prompt hooks, load an allowed `.envrc`, render a
prompt, change directories with `z`, and source twice without duplicate hooks.
Those checks do not prove GTK navigation. If Dakota cannot start because its
Podman socket is unavailable, report the desktop gate as blocked, not passed.

In a full GDM/GNOME lab VM, disable idle locking and verify
`org.gnome.ScreenSaver.GetActive` is false before native-input checks. Inspect
the captured frames: application focus and successful AT-SPI actions can persist
behind the lock screen and do not prove a visible GUI session. Unlock through
the guest's privileged control connection; do not assume the test user has
passwordless sudo.
