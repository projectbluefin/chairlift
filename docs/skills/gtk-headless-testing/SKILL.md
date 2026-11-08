---
name: gtk-headless-testing
description: Use when placing headless tests or driving the Dakota behave AT-SPI suite.
---

# GTK testing

## Unit-test placement

puregotk loads GTK/GLib/graphene at package initialization, before test bodies or
skip decisions. Keep packages importing it directly or transitively test-free.
Extract decidable logic to pure-Go packages under `internal/`; `cmd/` is outside
the enforced unit scope. CI selects `^Test[^I]` and skips `Integration`, so
ordinary tests must avoid both reserved name shapes. Measure coverage with the
same filter, not an unfiltered local run.

## Run the real surface

Use `make e2e`, `make e2e-atspi [ATSPI_TAGS=@tag]`, or `make screenshots`.
All enter the reviewed Dakota image through `test/e2e/dakota.sh`; host
prerequisites are Go and rootless Podman. The pin and roll procedure are in
[digest pinning](../multi-arch-digest-pinning/SKILL.md).

Never run GTK tests against the live desktop, `/run/user/<uid>`, default host
bus, or portals. Never stop/mask/unmount host portals. Use the existing private
HOME, 0700 runtime directory, `dbus-run-session`, and `wayland_session.sh` with
`GDK_DEBUG=no-portals`. The harness uses headless Mutter, native Wayland input,
and PipeWire captures—no X11. Generic Debian/Ubuntu containers do not reproduce
Bluefin's GTK stack. Dry-run fixtures are not live service/hardware proof.

The fixture container masks `/usr/share/chairlift` and `/proc/cmdline` so image
config and the host's composefs boot do not override scenario fixtures. Keep
real shipped configuration visible for live QA outside this fixture suite.
Release builds must not reuse writable caches from E2E jobs.

## Add or debug an accessibility scenario

- Add scenarios under `test/e2e/features/`; shared steps belong in
  `steps/common.py`, destination-specific steps in that destination's file.
- `environment.py` creates a fresh dry-run process per scenario. Fixtures use
  `@config.<name>`, `@env.KEY=VALUE`, `@stub.<name>`, and `@no-app`.
  Stub tags apply left-to-right; do not turn their ordered list into a set.
- Give a new feature its own first tag and add it to exactly one shard in
  `.github/workflows/test.yml`. Do not reuse another feature's first tag on a
  scenario. `CHAIRLIFT_ATSPI_KNOWN_ISSUES=1` includes known-issue scenarios.
- Inspect `build/atspi/<tags|all>/`: `behave.log`, JUnit XML, `mutter.log`, and
  scenario `chairlift.log`, `journal.jsonl`, failure `tree.txt` and `screen.png`.
  Read the actual tree before changing lookups. A skipped gate is not proof.
- Activate through AT-SPI actions or keyboard, not coordinates: Wayland does
  not disclose window position. `common.content` is a query pseudo-root;
  traverse with `atspi.search_nodes`, not node-only helpers.
- Assert the control's own accessible name, not a descendant-text fallback.
  A menu-button wrapper may have no action while its toggle child does.
  Dialogs may appear inside the window or as another top-level frame.
- Assert collapsed defaults before helpers expand the section. Test saved
  enabled preferences too. Identify setup bookends by page/actions, not prose.
- Stub every external reader relevant to an assertion, including Homebrew's
  fallback path. Keep inventories distinct from description queries. A
  dry-run journal proves privileged suppression only; use recording launcher
  stubs to prove an unprivileged application was not launched.
- Keep `dogtail.tree` imports lazy for behave's display-free dry-run gate.
  Preserve explicit `GSETTINGS_BACKEND=memory`; never redirect fixtures into
  the user's desktop settings store.

## Harness failures

Use the existing session/input helpers rather than another runner:

- Compositor/PipeWire sockets need short paths (Unix limit: 108 bytes).
  `WAYLAND_DISPLAY` is absolute because scenarios use separate runtime dirs.
- Dakota's container loader cache may omit Mesa; `wayland_session.sh` supplies
  its library path for software rendering. A host-root container or an
  unsupported runtime mount is not a reason to weaken isolation.
- dogtail chooses its backend at import. `wayland_remote.py` initializes the
  Mutter virtual keyboard, suppresses pointer warm-up, and holds the D-Bus
  connection until queued input is dispatched. Reuse those timings.
- Cached venv Python symlinks may resolve only inside Dakota. Check them there;
  replace an unusable venv without nesting a partial directory inside it.
- Send SIGTERM first so application shutdown flushes coverage. Drain all
  descendants in the test's private session before deleting its HOME: workers
  may have separate process groups. Never signal the runner's shared session.
- For live GNOME QA, check that the guest screen is unlocked and inspect the
  captured frames. Successful actions or focus do not prove a visible window.
- On a host without Podman, the pinned image still runs the real suite and
  `scripts/blueprints.sh`'s compiler: export it with `crane export` into a
  directory and enter it through an unprivileged user and mount namespace,
  reproducing `dakota.sh`'s mounts and masks. Enter with `pivot_root`, not
  `chroot`: the kernel refuses a new user namespace inside a chroot, and the
  second one is needed to run as an ordinary uid, since headless Mutter
  refuses uid 0. Give that uid a passwd entry or `dbus-daemon` fails to start.
  This substitutes for the container only; CI's `dakota.sh` run is the gate.

## Maintaining the native harness

- Free hosted runner space with the existing setup-runner action before Go
  setup or Dakota pulls: cleanup removes the hosted tool cache. Keep rootless
  Podman storage unchanged and disable the action's Podman update.
- Use one state-word behave handler for checkbox state/sensitivity; overlapping
  literal and parameterized steps prevent feature discovery. Reuse generic
  button steps instead of adding collection-specific duplicates.
- Collections install selected entries, not checked collection headers. Test
  empty defaults, installed insensitive entries, Select All and partial reads;
  preserve deliberate selections across refresh and explicit setup navigation.
- Enter Developer Mode detail before looking up Docker/WSL controls, including
  live Docker drivers. Game Mode scenarios exercise image switching only,
  without inventorying or changing gaming Flatpaks.
- Agent Mode may adopt an already-running daemon only after proving current
  loaded-unit/process identity. A current file or HTTP response alone is not
  adoption. Exercise interrupted stamp/reload paths using a private manager.
