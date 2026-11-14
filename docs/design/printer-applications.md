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

Physical printing is not established by service readiness. HPLIP plugin
consent remains an interactive image-side web flow, not a headless ChairLift
action. The network trade-off is recorded in
[ADR-0020](../adr/0020-printer-apps-loopback-only-on-moving-stable-tag.md).

## USB printer passthrough

Each family row has a **USB Printer** picker: "No USB printer" or one of the
connected printers, named by the maker and model it reports. The choice is saved
as the printer's stable identity — USB vendor ID, product ID and serial number —
in the `usb-printers` key of the `io.projectbluefin.chairlift.printers`
GSettings schema, keyed by family. A device path such as
`/dev/bus/usb/001/008` changes on every replug or reboot, so it is never saved.

Every route that writes a unit — turning a family on, choosing a printer, and a
Printer Drivers update — resolves the saved identity against
`/sys/bus/usb/devices` and adds `AddDevice=-/dev/bus/usb/<bus>/<device>` for the
one printer that matches. The path is resolved only when the unit is written,
while the unit starts at every login with the path last written. A replugged or
rebooted printer therefore gets its new path the next time the family is turned
on, its printer is chosen again, or Printer Drivers refreshes it. Until then the
`-` prefix (`podman-systemd.unit(5)`: add the device only if it exists) keeps a
printer that is gone from stopping the family. It does not stop a different USB
device that has since been given the old bus and device numbers from being
passed through. Re-resolving the identity at login, or a udev rule that gives
each printer a stable device link, would close that and is a follow-up.
Choosing a printer while the
family is on restarts it through the verified refresh path; "No USB printer"
removes the `AddDevice=` line. If the saved printer is not connected, or two
identical printers without serial numbers cannot be told apart, the unit carries
no device, the family still starts, and its row says the USB printer is not
connected.

One printer has one owner: a printer chosen for one family is refused for
another, by identity, so it stays taken while unplugged. The choice applies to
the family's default application only. The picker re-reads the connected
printers when the pointer or keyboard focus reaches it; there is no timer or
hotplug watch. Saving honours dry-run, and no privileged route is involved:
the choice is a user setting and the unit stays rootless. Device permissions
inside the container are not configured here.

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
