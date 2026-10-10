# 0020 — Printer applications run loopback-only on a moving stable tag

- **Status:** Accepted
- **Date:** 2026-10-09
- **Supersedes:** [ADR-0016](0016-printer-app-admin-denied-until-authenticated.md)

## Context

ADR-0016 ran printer applications on host networking and refused every enable
until an image could be handed an administration credential, because PAPPL
serves IPP and an unauthenticated web administration page on one listener. No
published image gained that knob, so every family stayed locked and the
feature was unusable. The units were also pinned to hard-coded digests that
only a ChairLift release could move, so a driver fix in a `*-printer-app`
repository never reached users on its own.

## Decision

1. **Loopback only.** `RenderUnit` drops `Network=host` and publishes the
   application's port as `PublishPort=127.0.0.1:<port>:<port>`, keeping
   `Environment=PORT=<port>` so PAPPL listens on the published container port.
   The unauthenticated web administration is never reachable from the LAN, so
   ADR-0016's enable gate and locked state are removed.
2. **Moving tag.** Built-in families run
   `ghcr.io/projectbluefin/<id>-printer-app:stable`, a tag each image
   repository's release workflow advances.
3. **Auto-update.** Each unit carries `AutoUpdate=registry`, and `Enable`
   runs `systemctl --user enable --now podman-auto-update.timer` after a
   successful start, rolling a fresh install back if that fails.

## Consequences

- IPP and the web administration are reachable only from this computer
  (`http://localhost:<port>/`, `ipp://localhost:<port>/ipp/print`). LAN clients
  cannot print to or administer them, and the in-container DNS-SD
  advertisement is not visible on the host or the LAN.
- `podman-auto-update.timer` pulls each new `:stable` release and restarts the
  unit; there is no per-release ChairLift change.
- An administrator override stays possible: `printerapp.ApplyOverrides`
  repoints a family at a mirror by tag or pinned `repo@sha256:…`. No config
  key wires it yet; that is a separate change.

## References

- Supersedes: [ADR-0016](0016-printer-app-admin-denied-until-authenticated.md)
- Shapes: [design/printer-applications.md](../design/printer-applications.md)
