---
name: gtk-headless-testing
description: Use when deciding where tests can run without puregotk or GTK libraries, or when writing or debugging the behave AT-SPI suite under test/e2e/features.
version: 2.0.0
last_updated: 2026-10-01
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
`run_atspi.sh` owns a private Xvfb (`-displayfd`, so parallel runs never
collide) and one private `dbus-run-session`; `features/environment.py`
launches a fresh `--dry-run` ChairLift **per scenario** with its own HOME,
`XDG_RUNTIME_DIR`, config fixture, and `$CHAIRLIFT_ACTION_JOURNAL`. Scenario
tags select fixtures: `@config.<name>` (`fixtures/config/<name>.yml`, default
`everything`), `@env.KEY=VALUE`, `@stub.<name>` (`fixtures/stubs_<dest>.py`),
`@no-app`, `@known_issue.<N>`. Shared steps are in `steps/common.py`; a step
only one destination needs goes in `steps/<destination>.py`.

Intentional malformed-input fixtures must stay malformed. Exclude their exact
paths from syntax-lint hooks, not the entire fixture directory or hook.

**Run it.** `make e2e-atspi [ATSPI_TAGS=@tag]` locally and in CI; both run
`test/e2e/dakota_atspi.sh`, which runs the Go gate in
`ghcr.io/projectbluefin/dakota:testing` with `CHAIRLIFT_REQUIRE_ATSPI=1` (a
missing stack fails instead of skipping). Not on the runner's Ubuntu: its
Libadwaita 1.5 publishes an AdwPreferencesGroup as an unnamed panel (only the
inner list carries the title), where Dakota publishes a `grouping` named by
the title — 63 scenarios passed on Dakota and failed on Ubuntu for that and
similar version drift. `make e2e` skips `TestATSPIBehaveSuite` but keeps the
behave dry-run check. Artifacts:
`build/atspi/<tag|all>/` locally, the `atspi-results` artifact in CI —
`behave.log`, JUnit XML, and per scenario `chairlift.log`, `journal.jsonl`,
and on failure `tree.txt` (the accessibility tree) and `screen.xwd`
(`ffmpeg -i screen.xwd x.png`). Read `tree.txt` before guessing at a lookup.

**Traps, each learned the hard way:**

- **The host cannot run the suite.** `/usr/share/chairlift/config.yml` on a
  Bluefin host outranks every `config.dev.yml` fixture; `before_all` refuses
  to start rather than test the wrong file, and `environment.py` also checks
  the `Loaded config from <fixture>` marker. The container masks the
  directory with `--tmpfs …:notmpcopyup` — plain `--tmpfs` copies the image's
  file into the tmpfs.
- **Containers inherit the host kernel command line, not its deployments.**
  A composefs host's `/proc/cmdline` makes the real bootc reader look for
  `/sysroot/state/deploy` even inside the fixture container, bypassing its
  fake `bootc` and hiding System version. `dakota_atspi.sh` binds `/dev/null`
  read-only over `/proc/cmdline` so fixture tests use their own bootc answers.
  This is test isolation only; a booted Dakota VM must keep its real cmdline.
- **GTK 4 on X11 publishes no screen coordinates.** `position` is `None`, so
  nothing can be clicked by position. Activate through AT-SPI actions
  (`atspi.activate`) or the keyboard; list rows, which have no action, are
  reached with arrow keys from the focused row and Return.
- **GtkMenuButton is two nodes.** An action-less `button` wraps the `toggle
  button` carrying `click`. `is_button` requires an action so the wrapper
  never shadows it.
- **Popover menu items are nameless** on this stack (`@known_issue.347`);
  `menu_item()` falls back to the `keyshortcuts` attribute, then model order.
- **AdwAboutDialog is a separate top-level frame** at the suite's window
  size; `current_dialog` searches in-window `dialog`/`alert` nodes first, then
  extra top-level frames.
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
  Agent Mode stamps the successful systemd `InvocationID` in its owned unit
  only after reload/restart; missing or mismatched stamps retry. Observe that
  ID and the actual page readiness on a private user manager. Do not add a
  root read route: even same-UID `/proc/<pid>/environ` reads may be denied.
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
1. In Go E2E tests (`test/e2e/e2e_test.go`), allocate a private `0700` directory under `t.TempDir()` and pass `XDG_RUNTIME_DIR=<dir>` in `cmd.Env`.
2. In shell scripts (`test/e2e/capture_walkthrough.sh`, `test/e2e/run_atspi.sh`), export `XDG_RUNTIME_DIR="$OUTDIR/runtime"` (mode `0700`).
3. Always export `GDK_DEBUG=no-portals` in every harness.
4. Always force `GDK_BACKEND=x11` and clear `WAYLAND_DISPLAY`: GTK 4 prefers Wayland whenever `WAYLAND_DISPLAY` is set, so a harness launched from a desktop session otherwise opens the window on the live compositor instead of Xvfb.
5. Automated enforcement is maintained by `internal/installcheck/e2e_portal_isolation_test.go`.
6. Never execute `systemctl --user mask`, `stop`, or unmount commands against host desktop portals.

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

### Running the AT-SPI suite with Dakota

`make e2e-atspi` (see "The behave AT-SPI suite" above). It needs podman, Go,
and Homebrew's `xorg-server` on the host, mounts the host's Go toolchain and
Homebrew read-only, and creates its venv from `test/e2e/requirements-atspi.txt`
with the container's interpreter. `CHAIRLIFT_ATSPI_KNOWN_ISSUES=1` also runs
`@known_issue` scenarios.
This fixture suite uses Xvfb; it is not a live Wayland desktop walkthrough.
For Wayland diagnosis on ghost, read testing-lab's
`docs/reference/workflow-reference.md` and `docs/skills/argo-workflows/patterns.md`
first. Dakota's VM install path is documented as blocked by its missing UKI.
Reuse `run-container-tests`' nested systemd/GDM target, headless GNOME Shell,
test-user linger, and `qecore-headless --session-type wayland`; do not invent
another disk installer or substitute Xvfb for a requested Wayland session.

### Generating Walkthrough Screenshots with Lima + Dakota

When host runtime libraries or session portals cannot run the GTK capture harness directly:
1. Launch the `dakota-fedora` Lima VM (`limactl start dakota-fedora`).
2. Ensure VM Homebrew has the required tools: `brew install go xdotool xdpyinfo xorg-server libxmu libxkbfile pkgconf` (Homebrew lacks `xwd`, so build `xwd-1.0.9` into `~/xtools`).
3. Run `make screenshots` inside `ghcr.io/projectbluefin/dakota:testing` via Podman with `--userns=keep-id`, mapping `--tmpfs /tmp:rw,mode=1777`, mounting the source tree to `/workspace`, and masking `/usr/share/chairlift` with an empty directory (`--tmpfs /usr/share/chairlift:notmpcopyup`) so the packaged config does not override the test suite's `config.dev.yml`.
4. Copy the resulting PNGs out via `limactl copy`.

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

## Empty status panels and announcements

For compact layouts, hide an empty `AdwStatusPage`; clearing its strings
leaves internal padding in the allocation. Keep visibility decisions in the
pure presenter and retain panels with an action or active progress. Emit
phase announcements from a widget that stays mapped, such as the shell's
toast overlay, rather than the panel being hidden.

A staged-deployment fixture needs both a fake `bootc status` with a non-null
staged entry and the fixed stage-helper marker. The Dakota AT-SPI container
binds `/usr/bin/false` at `/usr/libexec/bootc-update-stage` for detection only;
the suite remains in `--dry-run`. Its masked `/proc/cmdline` ensures the
fake `bootc` is used rather than reading the host's composefs deployment.
Verify the staged state and the return to a visible pending-update panel;
an unstaged screenshot or a string-search test cannot prove either layout.
