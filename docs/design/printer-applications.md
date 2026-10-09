# Printer Applications

Living design document. The administration boundary is recorded in
[ADR-0016](../adr/0016-printer-app-admin-denied-until-authenticated.md);
[ADR-0001](../adr/0001-fixed-path-pkexec-privilege-boundary.md) explains why
this user-scoped integration stays outside the privileged helpers.

## Current ownership and enable condition

[`internal/printerapp`](../../internal/printerapp/printerapp.go) owns rootless
Podman quadlets under `~/.config/containers/systemd` and their
`systemctl --user` lifecycle. The Features page builds one default application
per entry returned by `printerapp.Families()`: **Ghostscript, HPLIP, and
Gutenprint**. There is no PostScript entry or per-device setup UI. ADR-0016's
four-family inventory records the earlier design, not the current row set.

`features_page.printers_group` is enabled in both
[`config.yml`](../../config.yml) and the
[built-in defaults](../../internal/config/config.go). The
[capability floor](../../internal/capability/capability.go) requires `podman`
on `$PATH`; configuration may hide the group but cannot supply a missing tool.
Neither building the group nor observing state installs or starts a unit.

**Every new enable is refused.** `printerapp.CanEnable(Family)` currently
returns `ErrAdminUnauthenticated` for every family. `Enable(ctx, app)` calls
that predicate before its dry-run branch, file writes, or systemctl commands.
A digest pin, a working Podman executable, or a dry-run request cannot bypass
this lock. The UI keeps the family visible with its switch off and insensitive
and explains that its administration page cannot yet be secured.

The exception is an **already-present unit**: it remains controllable so the
user can turn it off. Its presence means configured, not ready. The readiness
model below distinguishes starting, running, and failed service observations.

## Network and administration boundary

[`RenderUnit`](../../internal/printerapp/printerapp.go) uses `Network=host`,
not `PublishPort`. IPP and DNS-SD are intended to reach the LAN through the
host's address. Host networking is not an administration security boundary:
PAPPL serves IPP and its web interface on the same listener. ADR-0016 records
the framework observations and the decision that web administration must be
**authenticated or absent** before any application is enabled. Loopback-only
publishing would prevent LAN printing rather than secure administration.

ChairLift does not presently render credentials or web-disable settings. The
image-side contracts requested in
[ghostscript-printer-app#65](https://github.com/projectbluefin/ghostscript-printer-app/issues/65),
[hplip-printer-app#51](https://github.com/projectbluefin/hplip-printer-app/issues/51),
and [gutenprint-printer-app#57](https://github.com/projectbluefin/gutenprint-printer-app/issues/57)
name `PRINTER_APP_AUTH_SERVICE`, `PRINTER_APP_ADMIN_GROUP`, and
`PRINTER_APP_SERVER_OPTIONS`. These are prospective entrypoint inputs, not
current ChairLift environment settings. `RenderUnit` writes only `PORT`.
An upstream source change or a newly published tag does not alter the
immutable images selected here and does not make `CanEnable` return success.

There is no pkexec route, helper subcommand, PolicyKit action, privileged
container, or host image layering for printer applications. Device passthrough
is not rendered. Physical printing, USB assignment, mDNS coexistence with a
host Avahi daemon, and restart survival against physical hardware remain
unverified; the lifecycle and UI tests do not establish those outcomes.

## Family inventory and shipped pins

The source of truth is the `families` table in
[`printerapp.go`](../../internal/printerapp/printerapp.go). These are the
**shipped defaults**, not a live GHCR publication catalog. `Family.Image()`
uses `Repo + "@" + Digest` when a digest is present; the version label does
not replace that immutable reference.

| Family | Version label | Pinned image digest |
| --- | --- | --- |
| Ghostscript | `10.07.1-2` | `sha256:82487bd81925b824f16d79a50b4237230d00429fca7761454299a8a4393368cc` |
| HPLIP | `3.26.4` | `sha256:1f81f507ce603f19eebb83fdcdc5b7de7bc7f52f728e9626c2c1224ea7477de8` |
| Gutenprint | `5.3.6-4.1` | `sha256:3ca46b65bba16e258d7f93582beb9ccdf71a8b4e450b9d01f8cb545a945b93a1` |

Each repository is `ghcr.io/projectbluefin/<family>-printer-app`. A pin bump
requires digest, signature, and attestation verification;
follow the [multi-arch digest-pinning skill](../skills/multi-arch-digest-pinning/SKILL.md)
and the supply-chain requirement in [AGENTS.md](../../AGENTS.md). Signature
verification does not substitute for the administration enable condition.

`Select(family)` returns the default `App{Family: family, Name: family.ID}`.
Its quadlet filename is the unit/container identity below plus `.container`;
its service name is that identity plus `.service`.

| Family | Unit / container identity | Host port | Preserved host state directory |
| --- | --- | --- | --- |
| Ghostscript | `chairlift-printer-ghostscript` | 18010 | `~/printer-workspaces/ghostscript/ghostscript` |
| HPLIP | `chairlift-printer-hplip` | 18030 | `~/printer-workspaces/hplip/hplip` |
| Gutenprint | `chairlift-printer-gutenprint` | 18050 | `~/printer-workspaces/gutenprint/gutenprint` |

The package's `App` API can also name an application within a family. Such an
app gets a sanitized family/name unit identity, its own state directory, and a
port derived by FNV hash in 18000–18999. This is deterministic, **not a
collision-free allocator**; there is no port registry or automatic collision
retry. The shipped UI uses only the three fixed default ports. The historical
PostScript port 18020 in ADR-0016 is not a currently rendered family.

The volume is rendered as
`%h/printer-workspaces/<family>/<name>:/var/lib/<family>-printer-app:z`.
`UserNS=keep-id:uid=65532,gid=65532` maps the invoking account into the
container. The quadlet requests `Restart=on-failure`, `RestartSec=10`, and
`WantedBy=default.target`; these are unit declarations, not proof that a
particular printer works after restart.

`ApplyOverrides(images)` is a package API that accepts known family IDs and
non-empty image references, separating tag or digest forms. It is not wired
to a configuration key or application startup in the current tree, and it
performs no signature or authentication verification. It cannot unlock
`CanEnable`; do not document a configurable printer mirror as a shipped UI
feature.

HPLIP's proprietary plugin consent remains an interactive image-side web
flow at `/plugin`, not a ChairLift lifecycle action. No headless consent flag
or environment contract is wired here. Diagnostic recognition of plugin
verification failures does not grant consent or install a plugin.

## Lifecycle and failure semantics

`Enable(ctx, app)` is the public gate. Its internal lifecycle, covered through
injected test seams, atomically writes the quadlet, creates the state directory,
runs `daemon-reload`, and starts the generated service. A failed reload/start
removes a unit created by that call, but preserves a pre-existing unit. This
internal success path is not currently reachable through public `Enable`,
because the administration condition is not met.

`Disable(ctx, app)` stops the service, removes only its quadlet, then reloads
the user manager. If stop fails, it queries `is-active`; only `inactive`,
`failed`, or `unknown` proves removal is safe. An active/transitional or
unreadable result returns an error and preserves the unit. Pulled images and
`~/printer-workspaces` remain untouched. There is no implemented printer
rollback operation or previous-pin store.

Dry-run disable logs the intended stop/removal and changes nothing. Public
dry-run enable still refuses the administration condition rather than
previewing an unsafe operation. Systemctl calls have a five-minute package
budget; the view bounds a whole mutation to ten minutes.

## Observed readiness and GTK wiring

[`printerapp_readiness.go`](../../internal/printerapp/printerapp_readiness.go)
separates cheap facts from runtime queries:

- `Observe(app, capable)` reports the capability floor, `CanEnable` verdict,
  and unit presence without spawning a process.
- `ProbeActive(ctx, app)` reads the single state word from
  `systemctl --user is-active`. A nonzero exit is normal for inactive/failed
  states; a missing or multi-word answer is a query failure, not readiness.
- `WaitSettled(ctx, app, limit)` polls activating/reloading states until they
  settle or the bound expires. It returns probe failures, not an invented
  running state.
- `Resolve(Facts)` delegates to the pure `Diagnose(DiagnosticInput)` classifier.
  `State.On()` describes a configured application independently of readiness:
  a present unit remains on even when its service is failed.

| Facts | State | Switch meaning |
| --- | --- | --- |
| Capability absent | unavailable | Group omitted by the normal view floor |
| No unit, enable condition refused | blocked | Off and locked, explanation visible |
| No unit, enable condition met | off | Off and enableable |
| Unit present, not checked or activating/reloading | starting | Configured on, still starting |
| Unit present, active | ready | Configured on and systemd reports active |
| Unit present, other state or failed probe | generic or classified failure | Configured on but not known running; disable remains possible |

[`printerapp_diagnostics.go`](../../internal/printerapp/printerapp_diagnostics.go)
adds `ProbeDiagnostics(ctx, app, capable)`, the view's current runtime probe.
It re-observes cheap facts, returns immediately for a missing unit/capability,
and treats an `active` state word as ready. Activating/reloading remain starting.
For other states or a failed active query it reads systemd
`SubState,Result,ExecMainStatus`, the latest 50 user-journal lines, and
`podman image exists <pinned-image>` in the invoking account. Secondary commands
are bounded to ten seconds each inside the caller's deadline; their errors are
best-effort observations, not propagated as a successful readiness check.

The pure classifier applies failure precedence in this order:

| State | Observed signal | Row guidance |
| --- | --- | --- |
| `StateFailedPlugin` | Plugin/signature/checksum verification messages | Explain failed proprietary plugin verification |
| `StateFailedDeviceAccess` | Device/USB references plus permission errors, or recognized device-open failures | Check USB permissions, group membership or udev rules |
| `StateFailedCrash` | Signal/core-dump result, high exit status or crash messages | Inspect the user journal |
| `StateFailedImage` | Pull/manifest/image failure messages; missing local image only when paired with a failed pull observation | Check network and registry access |
| `StateFailed` | None of those signals | Not known running |

`SubState` is collected but currently does not select a failure class. A failed
local image probe alone does not establish a registry failure. These are log
classifications, not physical-device or HTTP/IPP health probes: systemd active
does not certify successful printing or administration authentication. Every
failure state keeps the configured switch on and disableable, while showing
the failure explicitly; an on switch is never the readiness indicator.

[`printers_page.go`](../../internal/views/printers_page.go) builds rows and
signals once, with one `actionstate.Gate` per family and a `guardedSwitch` so
programmatic restoration cannot trigger a new mutation. Initial readiness uses
`ProbeDiagnostics`; successful live mutations re-probe through it before
rendering their confirmed state. Readiness and lifecycle commands run off the
GTK thread; row updates use `sgtk.RunOnMainThread`. Initial probes have a
fifteen-second bound and yield to an action owning the row. A successful live
enable would settle for up to a minute. Failure and preview restore the row's
last displayed state; a failed disable never claims the service stopped.
Copy comes from [`pageview/printers.go`](../../internal/views/pageview/printers.go),
and `actionmsg.PrinterApp` pairs live/preview feedback with the confirmation rule.

## Verification

The deterministic lifecycle/readiness/diagnostic tests inject unit directories,
systemctl and secondary-command runners; they do not start real printer
containers. Run the headless contract checks with the CI filter:

```sh
go test ./internal/printerapp ./internal/views/pageview ./internal/views/actionmsg -run '^Test[^I]' -skip Integration
```

The native Features surface is covered by
[`features.feature`](../../test/e2e/features/features.feature), including
locked rows, config/capability omission, an existing failed unit, and dry-run
disable. Run it only through the isolated Dakota Wayland harness:

```sh
make e2e-atspi ATSPI_TAGS=@features
```

Use `make ci` for the complete host-independent gate. These checks verify
ChairLift's shipped state and boundary; they do not unlock an image or certify
hardware behaviour.
