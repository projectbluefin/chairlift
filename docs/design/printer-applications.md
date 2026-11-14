# Printer Applications

[`internal/printerapp`](../../internal/printerapp/) owns rootless Podman
quadlets and their `systemctl --user` lifecycle. The Features page exposes the
families defined in [`printerapp.go`](../../internal/printerapp/printerapp.go);
that table owns images, ports and names. Configuration may hide Printers; its
host capability floor requires Podman. Observing a row does not install or start
anything.

## Network boundary

PAPPL's web administration is unauthenticated. Units publish only
`127.0.0.1:<port>:<port>`, never host networking or a LAN address. IPP and web
administration are available only on this computer; DNS-SD inside the container
does not advertise a usable LAN printer. There is no pkexec route, privileged
container or image layering.

Physical printing and USB passthrough are not established by service readiness;
ChairLift does not configure device passthrough in the UI. Plumbing in
`internal/printerapp` supports dedicated device passthrough by adding
`AddDevice=/dev/bus/usb/<bus>/<device>` when `DevicePath` is specified and
present, with one owner per device. HPLIP plugin consent remains an interactive
image-side web flow, not a headless ChairLift action. The network trade-off is recorded in
[ADR-0020](../adr/0020-printer-apps-loopback-only-on-moving-stable-tag.md).

## Family inventory and images

Enable and refresh verify the family's configured image with cosign against its
publishing workflow identity before writing a unit. The unit runs the verified
digest, not a tag resolved again after verification. Missing cosign or rejected
verification fails closed. Identity and verification details live in
[`internal/printerapp`](../../internal/printerapp/); the decision is
[ADR-0023](../adr/0023-verify-printer-images-before-installing.md).

Pins advance through the Updates page's Printer Drivers source. It checks
enabled families, preserves other families' results when one cannot be checked,
and retries failed refreshes without replaying successful ones. There is no
background schedule or `AutoUpdate=registry` on new units. For an older unit
still carrying that setting, turn the family off and on to install a verified
pin, or apply its offered update. ChairLift does not disable a shared Podman
timer used by unrelated containers.

## Lifecycle and failure semantics

Quadlets live in the account's configuration directory under
`containers/systemd`; persistent printer data lives under
`$XDG_DATA_HOME/chairlift/printers/<family>/<name>`, defaulting to
`~/.local/share/chairlift/printers/<family>/<name>`. Relative `XDG_DATA_HOME`
values are ignored, as required by the
[XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/).
Existing `~/printer-workspaces` data is not moved or deleted. Existing quadlets
keep their paths until enable or refresh rewrites them; users must move any
printer data they want to retain before switching an existing printer to the
new location.

Enable, refresh and disable serialize per application. Verification failure
leaves the existing unit and service untouched. A later service failure removes
a newly created unit, but does not restore the old contents of an existing unit.
Disable removes the quadlet only after the service is known stopped; unreadable
or active state preserves it. Disabling retains images and printer state.
Dry-run logs intended mutations without verification, writes or service commands.

## Observed readiness and GTK wiring

A unit's presence means configured, not ready. The row separately observes
systemd and classifies failures using service properties, journal output and the
installed image. A failed configured service stays switchable off. Systemd
active is not proof of working hardware, IPP health or successful printing.

[`printers_page.go`](../../internal/views/printers_page.go) owns GTK wiring;
[`pageview/printers.go`](../../internal/views/pageview/printers.go) owns row copy.
Runtime queries run off-thread; failures and previews retain known state rather
than inventing a successful enable or disable. Inspect the generated user
service's journal when the row reports a service failure.
