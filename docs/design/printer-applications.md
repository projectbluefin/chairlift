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

**Current state (2026-09-30):** the quadlet lifecycle
([#334](https://github.com/projectbluefin/chairlift/pull/334)) has merged and
the Features page renders the locked **Printers** group
([#397](https://github.com/projectbluefin/chairlift/pull/397)). Three of the
four driver families — Ghostscript, HPLIP, and Gutenprint — have a published,
digest-pinned image, but no image can yet receive the administration settings
ADR-0016 requires, so `CanEnable` refuses every family and every switch is off
and insensitive; PostScript has no published image. This page records the
contracted network and access surface, the family inventory, and the verified
image state.

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
disabled before ChairLift enables the unit (ADR-0016). Today's published
images forward only `PORT` and a log file from their entrypoints, so none can
be configured to meet that condition — the images must change first.

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
an environment of `PATH`, `container=podman`, and `HOME` only. Their
entrypoints honor one knob, `PORT` (passed to PAPPL as `server-port`), and
forward no other options: extra arguments are ignored, and there is no env
path to `server-options`, `auth-service`, or `admin-group` — the three
settings pappl-retrofit itself supports. The upstream source changes are not
evidence that these immutable pins contain them. Runtime checks on 2026-10-01
started all three pinned images on `--network none`, with no published ports,
and `PRINTER_APP_SERVER_OPTIONS=no-web-interface`. Every application's local
HTTP listener returned **200**, not the required 404: these pins ignore the
option, so `CanEnable` continues to refuse every family. The test containers
were removed after observation; no unauthenticated listener reached the LAN.

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
- **Failure surfacing** ([#331](https://github.com/projectbluefin/chairlift/issues/331)):
  a missing image, a refused admin configuration, or a crashing service is an
  actionable, non-enabled state — never a false enabled indicator.

## References

- Rationale: [ADR-0016](../adr/0016-printer-app-admin-denied-until-authenticated.md)
- Related: [ADR-0001](../adr/0001-fixed-path-pkexec-privilege-boundary.md)
- Built in: lifecycle PR
  [#334](https://github.com/projectbluefin/chairlift/pull/334) (merged)
- Upstream: [pappl](https://github.com/michaelrsweet/pappl),
  [pappl-retrofit](https://github.com/OpenPrinting/pappl-retrofit),
  [ghostscript-printer-app](https://github.com/projectbluefin/ghostscript-printer-app)
