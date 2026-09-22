# 0015 — Avatars convert to PNG before AccountsService SetIconFile

- **Status:** Proposed
- **Date:** 2026-09-21

## Context

The dinosaur avatar catalogue in `projectbluefin/website` (`public/characters/`)
is stored entirely as WebP files (`VP8L` lossless, 1024x1024 square). Applying
one of these as a user avatar requires setting it with the desktop display
manager (GDM) and session shell (GNOME Shell).

AccountsService (`org.freedesktop.Accounts`) owns cached icon state in
`/var/lib/AccountsService/icons/<username>`. The interface specification in
`/usr/share/dbus-1/interfaces/org.freedesktop.Accounts.User.xml` explicitly
requires PNG: `SetIconFile` takes "The absolute filename of a png file to use as
the user's icon" (line 428) and the `IconFile` property documents "The filename
of a png file containing the user's icon" (line 900). Furthermore, AccountsService
imposes a strict 1 MiB file-size ceiling in `user_change_icon_file_authorized_cb`
(`if (size > 1048576) { ... "file '%s' is too large to be used as an icon" }`).
`SetIconFile` copies the file directly into `/var/lib/AccountsService/icons/`; it
does not transcode, resize, or compress.

GNOME and GDM display managers expect avatars formatted as 512×512 PNG images.
Source dinosaur illustrations in `projectbluefin/website` are 1024×1024 lossless
WebP files. Even if WebP support exists in some desktop layers, passing a raw
1024×1024 WebP violates the D-Bus interface contract, risks exceeding the 1 MiB
hard cap upon raw expansion, and scales poorly on lock screens.

Additionally, ChairLift is built with `CGO_ENABLED=0` and carries no in-process
D-Bus bindings (`go.mod` has no `godbus` or D-Bus client library). Calling
`org.freedesktop.Accounts.User.SetIconFile` must maintain ChairLift's existing
architectural posture: CLI tool seams out-of-process, zero CGO dependencies.

## Decision

1. **Convert to PNG in memory before applying:** ChairLift fetches the WebP asset,
   decodes it using `golang.org/x/image/webp` (a pure-Go, CGO-free package), and
   encodes it to PNG format using the standard library `image/png`.
2. **Standard icon dimensions:** The image is scaled to 512x512 pixels prior to
   encoding to match standard GDM/AccountsService icon sizing.
3. **Dispatch via CLI:** The generated PNG is saved to a secure user-owned
   temporary file (`~/.cache/chairlift/avatar-staging.png`) and passed to
   AccountsService via `busctl` or `gdbus`:
   ```bash
   busctl call org.freedesktop.Accounts \
     /org/freedesktop/Accounts/User$UID \
     org.freedesktop.Accounts.User SetIconFile s "/path/to/avatar.png"
   ```
4. **Privilege boundary unviolated:** AccountsService handles permissions using
   PolicyKit action `org.freedesktop.accounts.change-own-user-data`, which defaults
   to `allow_active` (authorized without password for the active local user).
   ChairLift's fixed-path `pkexec` helper boundary (`chairlift-ublue-helper`) is
   neither touched nor extended.
5. **Fall-back to `~/.face`:** For environments where AccountsService D-Bus is
   unreachable or disabled, the same PNG is also copied to `~/.face.icon` and
   `~/.face`.

## Consequences

- Remote WebP images are transcoded into compliant 512×512 PNG files that strictly
  honor the AccountsService interface specification and stay well below the 1 MiB cap.
- `golang.org/x/image` is added to `go.mod` as a pure-Go dependency (`CGO_ENABLED=0`
  remains intact).
- Avatar application is fully testable using pure-Go unit tests with loopback HTTP
  seams and mock command runners without requiring root or real desktop accounts.

## Alternatives considered

- **Passing raw WebP to `SetIconFile`:** Rejected because the D-Bus interface
  specification explicitly mandates PNG (`SetIconFile` takes a PNG file), because
  AccountsService rejects uncompressed images over 1 MiB, and because raw 1024×1024
  dimensions exceed standard display avatar scaling.
- **Using `godbus` in-process:** Rejected to preserve ChairLift's zero-dbus-library
  footprint and avoid mixing runtime D-Bus connection lifecycles into puregotk GTK
  event loops.

## References

- Shapes: [specs/dinosaur-avatars.md](../specs/dinosaur-avatars.md)
- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md),
  [ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md)
