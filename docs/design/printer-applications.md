# Printer Applications

<!--
Design docs are LIVING documents: update them in place so they always describe
the system as it is. Rationale does NOT live here — it lives in ADRs, linked
below. If you find yourself writing "because", consider whether it belongs in
an ADR.
-->

Living document. Rationale: [ADR-0016](../adr/0016-printer-app-admin-denied-until-authenticated.md).

This document covers ChairLift's support for the
[FSDK Printer Applications](https://github.com/projectbluefin/chairlift/issues/328):
rootless Podman quadlets, one unit per printer, for the Ghostscript,
PostScript, HPLIP, and Gutenprint driver families.

**Current state (2026-10-04):** the quadlet lifecycle
([#334](https://github.com/projectbluefin/chairlift/pull/334)) has merged and
the Features page renders the locked **Printers** group
([#397](https://github.com/projectbluefin/chairlift/pull/397)). Upstream
entrypoints (ghostscript PR #68, hplip PR #52, gutenprint PR #59) accept
`PRINTER_APP_SERVER_OPTIONS=no-web-interface` (mapped to `-o server-options=no-web-interface`),
while `PRINTER_APP_AUTH_SERVICE` exits 78 (fail-closed) because the shared base
builds PAPPL with `--disable-libpam`. Under ADR-0016, ChairLift's quadlet unit
writes `Environment=PRINTER_APP_SERVER_OPTIONS=no-web-interface` and never sets
auth knobs. Families are gated by `AdminSurface` and remain locked in `CanEnable`
until their pinned digests are bumped to verified images (gutenprint `5.3.6-4.2`
is releasing; ghostscript `10.07.1-3` and hplip `3.26.4-1` are pending review).
Unlocked families have no web administration page; printers are discovered via
DNS-SD / reached over IPP and added from GNOME Settings. For HPLIP, printers
requiring proprietary plugins are not supported yet ([#331](https://github.com/projectbluefin/chairlift/issues/331)).
This page records the contracted network and access surface, the family
inventory, and the verified image state.

## Overview

One enabled printer is one rootless unit in the invoking user's
`~/.config/containers/systemd`, driven with `systemctl --user` — the same
lifecycle as the local-AI stack, and equally off the pkexec path. Four driver
families exist; each family is one pinned image, and each printer within a
family is one app with its own unit, port, and state volume, so one logical
device has exactly one owner and one DNS-SD advertisement.

```
LAN client ── IPP ──► host :18010 ──► chairlift-printer-ghostscript (rootless quadlet)
                 ▲                       │ PAPPL: IPP + web interface on one listener
                 └── DNS-SD (host network)└─ web admin: denied until authenticated
```

## Network surface

The application runs on **host networking**. Two reasons, both verified:

- DNS-SD: the application advertises itself with the `avahi-daemon` it starts
  in-container. Under a bridge network that advertisement points at an address
  LAN clients cannot reach; under host networking it advertises the host.
- IPP: clients print to the application's port. Host networking puts PAPPL's
  listener on the host directly, so `PublishPort` is not used and cannot be
  mistaken for a boundary.

The consequence is that the web interface is on the same LAN-reachable port as
IPP — PAPPL serves both on one set of listeners and cannot bind administration
to loopback separately. The boundary is therefore authorization, not binding:
IPP-transport administration already refuses remote clients without an
authentication service, and web administration must be authenticated or
disabled before ChairLift enables the unit (ADR-0016). Because PAPPL is built
with `--disable-libpam` in the shared base container, authenticated web
administration is unavailable. ChairLift therefore disables web administration
entirely (`Environment=PRINTER_APP_SERVER_OPTIONS=no-web-interface`), leaving
only IPP printing and DNS-SD discovery active on host networking. Current
pinned digests predate the entrypoint changes; families unlock as their pins
are updated to verified images.

## Family inventory

The contracted identities for the family-default app of each family. Distinct
container names, state volumes, and fixed non-colliding ports are the
enable-time acceptance for [#338](https://github.com/projectbluefin/chairlift/issues/338);
additional apps per family get their own non-colliding ports and volumes from
the same lifecycle.

| Family | Unit / container | Port | State volume |
| --- | --- | --- | --- |
| Ghostscript | `chairlift-printer-ghostscript` | 18010 | `~/printer-workspaces/ghostscript/ghostscript` |
| PostScript | `chairlift-printer-ps` | 18020 | `~/printer-workspaces/ps/ps` |
| HPLIP | `chairlift-printer-hplip` | 18030 | `~/printer-workspaces/hplip/hplip` |
| Gutenprint | `chairlift-printer-gutenprint` | 18050 | `~/printer-workspaces/gutenprint/gutenprint` |

Ports are above the privileged range and well below the ephemeral range; two
enabled families must not collide, and the lifecycle documents how a collision
is surfaced rather than silently retrying
([#329](https://github.com/projectbluefin/chairlift/issues/329)).

## Image state

Verified against GHCR on 2026-10-01.

Each family is a multi-architecture OCI index pinned by both its
application-version tag and its index digest; the digest is what the quadlet
`Image=` line carries, so a re-pushed tag cannot change the image under a
running unit.

| Family | Tag | Index digest |
| --- | --- | --- |
| Ghostscript | `10.07.1-2` | `sha256:82487bd81925b824f16d79a50b4237230d00429fca7761454299a8a4393368cc` |
| HPLIP | `3.26.4` | `sha256:1f81f507ce603f19eebb83fdcdc5b7de7bc7f52f728e9626c2c1224ea7477de8` |
| Gutenprint | `5.3.6-4.1` | `sha256:3ca46b65bba16e258d7f93582beb9ccdf71a8b4e450b9d01f8cb545a945b93a1` |
| PostScript | — | unpublished |

- **Ghostscript `10.07.1-2`** — supersedes `10.07.1-1`; the index builds on
  FSDK `26.08.1` (tag `v10.07.1-2`, commit `f667ca71…`, source
  `registry-actions.yml`) with per-arch amd64 `sha256:50164e6a…` and arm64
  `sha256:080cf267…`. Its signature and SLSA provenance verify with `cosign`,
  and the attached SPDX-2.3 SBOM lists 855 packages.
- **HPLIP `3.26.4`** — per-arch amd64 `sha256:da6db24b…`, arm64
  `sha256:bdb91c52…`, also FSDK `26.08.1`.
- **Gutenprint `5.3.6-4.1`** — per-arch amd64 `sha256:82e062ac…`, arm64
  `sha256:0ce3849d…`, FSDK `26.08.1`. The multi-architecture index is the
  current tag; do not use the orphan `5.3.6-4-aarch64` tag.
- **PostScript** — `projectbluefin/ps-printer-app` still has no published
  GHCR repository, so there is no image to pin.

Every published image is **one monolithic layer** (177–223 MB) plus a 34-byte
metadata layer. There is no shared base separate from the application, so
enabling N families costs N full pulls until
[fsdk-containers#342](https://github.com/projectbluefin/fsdk-containers/issues/342)
lands; a host with several families enabled repeats the bulk of each pull.

All three images run as user 65532 with
`catatonit -- bash /usr/libexec/<family>-printer-app/container-entrypoint` and
an environment of `PATH`, `container=podman`, and `HOME` only. Verified
upstream changes (ghostscript PR #68, hplip PR #52, gutenprint PR #59) update
the entrypoints to accept `PRINTER_APP_SERVER_OPTIONS=no-web-interface` (passed
to PAPPL as `-o server-options=no-web-interface`). Any other server option exits
64. Setting `PRINTER_APP_AUTH_SERVICE` exits 78 because PAPPL was compiled
without PAM (`--disable-libpam`). Setting `PRINTER_APP_ADMIN_GROUP` exits 78
unless `AUTH_SERVICE` is set.

The current pinned digests predate these PRs. Runtime checks on 2026-10-01
started all three pinned images on `--network none`, with no published ports,
and `PRINTER_APP_SERVER_OPTIONS=no-web-interface`. Every application's local
HTTP listener returned **200**, not the required 404: these pins ignore the
option, so `CanEnable` continues to refuse every family until a verified pin
is adopted. The test containers were removed after observation; no unauthenticated
listener reached the LAN.

Ghostscript's index and its attached SBOM were independently verified against
the exact keyless signing identity
`https://github.com/projectbluefin/ghostscript-printer-app/.github/workflows/registry-actions.yml@refs/tags/v10.07.1-2`
and the GitHub Actions OIDC issuer. Transparency-log and SLSA provenance checks
passed, both amd64 and arm64 were present, and the signed SPDX SBOM contained
855 packages. Supply-chain verification does not override the administration
lock or establish physical printing, USB, or mDNS interoperability.

## Operational notes

- **Enable/disable is not pull/delete.** Disabling stops the service and
  removes the quadlet but leaves the pulled image and the per-app state volume
  untouched; rollback repoints a family at its prior verified index.
- **Unverified without hardware** (recorded as such, not claimed):
  - Actual printing through a physical device, and per-device passthrough
    scoping ([#330](https://github.com/projectbluefin/chairlift/issues/330)
    still owns device assignment).
  - The in-container `avahi-daemon` under host networking vs a host
    `avahi-daemon`: both compete for the mDNS socket; the collision behavior
    must be documented by the lifecycle, not discovered by a user.
  - Restart survival: the permitted IPP/DNS-SD surface must come back after
    `systemctl --user restart` with the same port and name, which the
    deterministic unit shape is designed for but has not been demonstrated
    against the real image.
- **Failure surfacing and diagnostics** ([#331](https://github.com/projectbluefin/chairlift/issues/331)):
  Readiness queries and failure classification (`printerapp.Diagnose` / `printerapp.ProbeDiagnostics`)
  evaluate systemd `is-active`, `Result`, and `SubState` properties, journal logs, and container image presence:
  - **Missing Podman**: floored at host capability level; the group is omitted or row reports Podman is not installed.
  - **Failed rootless device access**: detected via permission denied / access denied logs on `/dev/usb/lp*` or `/dev/bus/usb/*`; the row instructs checking USB permissions and `lp` group membership.
  - **Unavailable image**: detected via container pull failure or missing image in registry/local storage; the row advises checking network and registry connectivity.
  - **Plugin verification failure**: detected via GPG/checksum verification failure logs from HP's proprietary driver plugin verifier; the row notes the signature failure.
  - **Service crash**: detected via systemd signal, core dump, or fatal exception status; the row directs the user to `journalctl --user` and restart.
  Every failure is rendered as an actionable state, never a false enabled indicator.
- **HPLIP plugin consent**: HPLIP's proprietary plugin consent is an interactive opt-in flow served exclusively within PAPPL's web interface at `/plugin`. Because web administration is disabled (`no-web-interface`), this endpoint is absent; automated or host-driven plugin consent remains unmet, and printers requiring proprietary plugins are not supported yet ([#331](https://github.com/projectbluefin/chairlift/issues/331)).

## References

- Rationale: [ADR-0016](../adr/0016-printer-app-admin-denied-until-authenticated.md)
- Related: [ADR-0001](../adr/0001-fixed-path-pkexec-privilege-boundary.md)
- Built in: lifecycle PR
  [#334](https://github.com/projectbluefin/chairlift/pull/334) (merged)
- Upstream: [pappl](https://github.com/michaelrsweet/pappl),
  [pappl-retrofit](https://github.com/OpenPrinting/pappl-retrofit),
  [ghostscript-printer-app](https://github.com/projectbluefin/ghostscript-printer-app)
