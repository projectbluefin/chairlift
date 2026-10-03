"""Native Wayland input and screen capture for the headless Mutter session.

Input is dogtail's own: dogtail 2.1 ships MutterInputBackend
(dogtail.hermetic.mutter), which drives org.gnome.Mutter.RemoteDesktop in
place of gnome-ponytail-daemon, so dogtail.rawinput's keyCombo and typeText
work unchanged on Wayland. install_input() swaps it in once per process.
dogtail chooses its Wayland branch from XDG_SESSION_TYPE at import time;
wayland_session.sh exports XDG_SESSION_TYPE=wayland before any of this runs.

Capture is the one thing dogtail's hermetic mode leaves out on purpose (it
runs without PipeWire): screenshot() records one frame of the virtual
monitor through org.gnome.Mutter.ScreenCast and GStreamer's pipewiresrc.

Usable as a library and as a command:

    python wayland_remote.py key '<Alt>2' Escape
    python wayland_remote.py type 'search text'
    python wayland_remote.py screenshot out.png
"""

import os
import subprocess
import sys
import time

SC = "org.gnome.Mutter.ScreenCast"

_installed = False

# Seconds between starting the RemoteDesktop session and the first key, and
# the no-op Shift_L taps sent across that wait.
KEYBOARD_SETTLE = 0.5
SETTLE_TAPS = 5
SHIFT_L = 0xFFE1


def install_input():
    """Point dogtail.rawinput at Mutter's RemoteDesktop API, once."""
    global _installed
    if _installed:
        return
    if "wayland" not in os.environ.get("XDG_SESSION_TYPE", ""):
        raise RuntimeError("XDG_SESSION_TYPE must be wayland before dogtail is imported; run inside wayland_session.sh")
    import gi

    gi.require_version("Gdk", "3.0")
    from gi.repository import Gdk

    # rawinput resolves key names to keycodes through the default GDK
    # display's keymap, and nothing in a headless test process opens one.
    if Gdk.Display.get_default() is None:
        Gdk.Display.open(os.environ["WAYLAND_DISPLAY"])
    # MutterInputBackend opens each RemoteDesktop session with zero-length
    # pointer motions to absorb pointer events Mutter 50 drops while its
    # virtual devices come up (dogtail measured the count on 50.x only). On
    # Dakota's Mutter 51 those motions cost the keyboard events that follow:
    # <Alt>3 navigated with the warm-up at 0 and was lost at dogtail's default
    # of 6. This suite never moves the pointer, so it does not warm it up.
    # Read when dogtail.hermetic.mutter is imported, hence before the import.
    os.environ["DOGTAIL_WARM_UP_EVENTS"] = "0"
    from dogtail.hermetic.mutter import install

    # Start the session now rather than on the first key, and give Mutter
    # time to bring its virtual keyboard up: it does so asynchronously after
    # Start() and offers no readiness signal, and a key sent before then
    # vanishes. The first key of every behave run was lost without this
    # (agents.feature:18, 3 of 3 runs).
    # A lone Shift tap changes nothing on screen, so a few spread across the
    # wait absorb whatever Mutter drops on a slower host than the one the wait
    # was measured on.
    backend = install()
    backend.connectMonitor()
    for _ in range(SETTLE_TAPS):
        backend.generateKeysymEvent(SHIFT_L)
        time.sleep(KEYBOARD_SETTLE / SETTLE_TAPS)
    _installed = True


def press(combo):
    """Press one key combination in dogtail's keyCombo syntax, e.g. '<Alt>2'."""
    install_input()
    from dogtail import rawinput

    rawinput.keyCombo(combo)


def type_text(text):
    """Type text one character at a time."""
    install_input()
    from dogtail import rawinput

    rawinput.typeText(text)


def screenshot(path, timeout=20):
    """Write one PNG frame of the virtual monitor to path."""
    from gi.repository import Gio, GLib

    bus = Gio.bus_get_sync(Gio.BusType.SESSION, None)

    def call(object_path, interface, method, args=None, reply=None):
        return bus.call_sync(
            SC, object_path, interface, method, args,
            GLib.VariantType(reply) if reply else None, Gio.DBusCallFlags.NONE, -1, None,
        )

    session = call("/org/gnome/Mutter/ScreenCast", SC, "CreateSession", GLib.Variant("(a{sv})", ({},)), "(o)").unpack()[0]
    try:
        stream = call(
            session, SC + ".Session", "RecordMonitor",
            GLib.Variant("(sa{sv})", ("", {"cursor-mode": GLib.Variant("u", 0)})), "(o)",
        ).unpack()[0]
        loop = GLib.MainLoop()
        node = {}

        def added(_conn, _sender, _path, _iface, _signal, params, _data):
            node["id"] = params.unpack()[0]
            loop.quit()

        subscription = bus.signal_subscribe(
            None, SC + ".Stream", "PipeWireStreamAdded", stream, None, Gio.DBusSignalFlags.NONE, added, None,
        )
        GLib.timeout_add_seconds(timeout, loop.quit)
        call(session, SC + ".Session", "Start")
        loop.run()
        bus.signal_unsubscribe(subscription)
        if "id" not in node:
            raise RuntimeError(f"Mutter never announced a PipeWire stream within {timeout}s")
        subprocess.run(
            [
                "gst-launch-1.0", "-q",
                "pipewiresrc", f"path={node['id']}", "num-buffers=1", "always-copy=true",
                "!", "videoconvert", "!", "pngenc", "!", "filesink", f"location={path}",
            ],
            check=True, timeout=timeout, capture_output=True,
        )
    finally:
        try:
            call(session, SC + ".Session", "Stop")
        except GLib.Error:
            pass  # the stream may already be gone


# Mutter tears a RemoteDesktop session down the moment its D-Bus client
# disconnects ("D-Bus client with active sessions vanished"), discarding key
# events it has not dispatched yet. A one-shot command therefore stays
# connected this long after its last event; a behave run is one process and
# never pays it.
DISPATCH_GRACE = 0.5


def main(argv):
    if len(argv) < 2 or argv[0] not in ("key", "type", "screenshot"):
        sys.stderr.write(__doc__)
        return 2
    command, args = argv[0], argv[1:]
    if command == "key":
        for combo in args:
            press(combo)
        time.sleep(DISPATCH_GRACE)
    elif command == "type":
        type_text(" ".join(args))
        time.sleep(DISPATCH_GRACE)
    else:
        screenshot(args[0])
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
