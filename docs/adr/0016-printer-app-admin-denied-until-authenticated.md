# 0016 — Printer application administration is denied until authenticated

<!--
Filename: 0016-printer-app-admin-denied-until-authenticated.md
ADRs are immutable once Accepted. To reverse one, write a new ADR and set the
old one's Status to "Superseded by NNNN".
-->

- **Status:** Superseded by [0020](0020-printer-apps-loopback-only-on-moving-stable-tag.md)
- **Date:** 2026-09-25

## Context

The printer-application epic ([#328](https://github.com/projectbluefin/chairlift/issues/328))
must expose an IPP printing endpoint that LAN clients can reach and discover
through DNS-SD, while the PAPPL web administration the same application carries
must not silently become a LAN service. Issue
[#338](https://github.com/projectbluefin/chairlift/issues/338) asks whether
administration can be bound to loopback separately from IPP, and if not, what
default applies. ChairLift drives these applications as rootless quadlets, so
the network surface of the container is ChairLift's to shape — but the
administration behavior is the application's, and it had to be determined
against the real framework before any unit could be enabled. The facts below
were verified on 2026-09-25 against upstream sources and the one published
image.

**One listener serves both IPP and the web interface.** PAPPL hands every
client — IPP and web alike — to the same set of listeners created by
`papplSystemAddListeners` (michaelrsweet/pappl `pappl/system.h`,
`pappl/client.c`); the web interface lives on the same port as IPP. There is no
per-purpose listener split: an application cannot bind administration to
loopback while IPP serves the LAN. Any per-interface separation would have to
be bolted on outside the application.

**IPP-transport administration is already remote-deny by default.** For
operations that check authorization (`papplClientIsAuthorized` →
`_papplClientIsAuthorizedForGroup`, pappl 1.1.0 `pappl/client-auth.c`), local
clients are always allowed, and remote clients get `HTTP_STATUS_FORBIDDEN`
unless an authentication service or callback is configured; remote access with
an auth service additionally requires encryption. This default is correct and
must not be weakened.

**The web interface does not share that default.** `papplClientHTMLAuthorize`
(pappl 1.1.0 `pappl/client-webif.c:449-450`) returns *authorized* when no auth
service, no auth callback, and no password hash is set. A PAPPL system with no
configured credential serves its full web administration to any client that can
reach the listener.

**The retrofit applications ship with no credential.** pappl-retrofit's
system callback creates the system with `PAPPL_SOPTIONS_WEB_INTERFACE` (plus
`WEB_LOG`, `WEB_NETWORK`, `WEB_SECURITY`, `WEB_TLS`) and sets no password, no
auth service, and no admin group (OpenPrinting/pappl-retrofit
`pappl-retrofit/pappl-retrofit.c:4563-4568`, 4778-4784). It *accepts*
`-o auth-service=…`, `-o admin-group=…`, and
`-o server-options=no-web-interface,…`, but nothing sets them.

**The published image cannot be handed a credential.** The only published
family image, `ghcr.io/projectbluefin/ghostscript-printer-app:10.07.1-1`
(multi-architecture OCI index; per-arch digests `sha256:9f647903…` amd64 and
`sha256:fef69f82…` arm64; verified via GHCR manifest requests), runs
`files/container-entrypoint.sh`, which builds a fixed argument list —
`-o log-file=…` and, when `PORT` is set, `-o server-port=$PORT` — and never
forwards extra arguments or any auth-related environment. The other three
families have no published repository at all (`tags/list` on
`projectbluefin/{ps,hplip,gutenprint}-printer-app` returns `NAME_UNKNOWN`).

## Decision

Access control for printer applications is authorization, not binding. PAPPL
cannot separate administration from IPP at the listener level, so ChairLift
treats the *default-deny of remote administration* as the enable condition:
an application may be enabled only when its web administration is either
authenticated (an auth service / admin group / set password) or absent
(`server-options=no-web-interface`). ChairLift does not enable an image whose
web administration is reachable without a credential, regardless of how the
digest is pinned.

Specifically:

- The IPP printing endpoint stays LAN-reachable, and the application runs on
  host networking so DNS-SD and IPP actually work (a published loopback or
  bridge port advertises an address clients cannot use). IPP-transport
  administration keeps PAPPL's own remote-deny default.
- Enabling a printer application verifies the image's *configuration surface*
  — that the entrypoint can receive the admin/auth settings ChairLift's unit
  sets — in addition to verifying the digest. Today's ghostscript entrypoint
  forwards only `PORT` and a log file, so it cannot be enabled on a
  LAN-facing surface yet; an image-side change is required first.
- The four families use distinct units, state volumes, and fixed non-colliding
  ports — Ghostscript 18010, PostScript 18020, HPLIP 18030, Gutenprint 18050 —
  as inventoried in [design/printer-applications.md](../design/printer-applications.md).
- Units stay rootless and never `--privileged`; device passthrough stays
  per-device and scoped, and hardware-dependent behavior is recorded as
  unverified until a physical device exists. Disable/rollback preserves the
  prior verified index so a family can be repointed without drift.

## Consequences

Enabling the ghostscript family is blocked on the image gaining a way to
receive administration settings — the entrypoint must forward an auth option
or an env-var equivalent, or the image must ship with the web interface
disabled. Until then the enable path must refuse the real image on a
LAN-facing surface rather than silently expose unauthenticated web admin; the
readiness surface ([#331](https://github.com/projectbluefin/chairlift/issues/331))
must present this as an actionable, non-enabled state.

The verified surface, not the digest alone, becomes part of the acceptance
evidence for the quadlet lifecycle: a unit that starts is not a unit that is
safe, and "no admin control is exposed unexpectedly" is a claim the enable
path must be able to make per family.

Host networking moves DNS-SD correctness in but adds an open question the
lifecycle must document: the application's in-container `avahi-daemon` and a
host `avahi-daemon` compete for the same mDNS socket, which cannot be verified
without a real host ([#329](https://github.com/projectbluefin/chairlift/issues/329)).

A user who wants the web UI must opt in with a credential configured — the
default is no web administration, not unauthenticated web administration.

## Alternatives considered

- **Loopback-only port publishing** (what the lifecycle PR rendered first):
  rejected — it breaks the epic's outcome entirely (no LAN client can print,
  and DNS-SD advertises an unreachable address) while merely hiding the admin
  surface by disabling the whole service for the network.
- **TLS only, no credential**: rejected — PAPPL upgrades connections to TLS,
  but `papplClientHTMLAuthorize` still authorizes every web client when no
  password/auth service is set; encryption without authorization is not a
  boundary.
- **Host firewall or reverse proxy in front of the application**: rejected for
  now — it adds a host-level component ChairLift does not own and must then
  keep in sync with four families' ports; PAPPL's own authorization is the
  boundary the application itself supports.
- **Injecting a password through the PAPPL state file**: rejected —
  `papplSystemLoadState` does restore a password hash, but the salted-hash
  format is an internal contract and crafting it from ChairLift would be
  fragile and version-coupled.

## References

- Shapes: [design/printer-applications.md](../design/printer-applications.md)
- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md) (rootless
  quadlets stay off the pkexec path)
- Issues: [#328](https://github.com/projectbluefin/chairlift/issues/328),
  [#329](https://github.com/projectbluefin/chairlift/issues/329),
  [#330](https://github.com/projectbluefin/chairlift/issues/330),
  [#331](https://github.com/projectbluefin/chairlift/issues/331),
  [#338](https://github.com/projectbluefin/chairlift/issues/338)
- Upstream sources verified 2026-09-25: michaelrsweet/pappl v1.1.0
  (`pappl/client-auth.c`, `pappl/client-webif.c`), OpenPrinting/pappl-retrofit
  (`pappl-retrofit/pappl-retrofit.c`), projectbluefin/ghostscript-printer-app
  (`files/container-entrypoint.sh`), and the published
  `ghcr.io/projectbluefin/ghostscript-printer-app` image config.
