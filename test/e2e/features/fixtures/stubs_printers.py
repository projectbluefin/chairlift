"""Prelaunch stubs for the Printers group on the Features page.

Printer applications are rootless quadlets under the scenario HOME's
~/.config/containers/systemd, driven with `systemctl --user`. The group's
scenarios assert that rendering it — one row per family, each off — runs no
systemctl and writes no quadlet, so the one stub here is a
recording systemctl: without it "systemctl was never run" would hold
vacuously.
"""

import os

from stubs import stub
from stubs_agents import recorder


def quadlet_dir(context):
    return os.path.join(context.home, ".config", "containers", "systemd")


@stub("printers")
def printers(context):
    """A host whose systemctl records every call and answers nothing."""
    recorder(context, "systemctl")


@stub("printers-running")
def printers_running(context):
    """Ghostscript enabled and running: its quadlet exists and systemctl
    answers the service active, so the row offers its working Manage button
    while the other families stay off (#671)."""
    directory = quadlet_dir(context)
    os.makedirs(directory, exist_ok=True)
    unit = os.path.join(directory, "chairlift-printer-ghostscript.container")
    with open(unit, "w", encoding="utf-8") as handle:
        handle.write(
            "[Container]\n"
            "Image=ghcr.io/projectbluefin/ghostscript-printer-app:stable\n"
            "PublishPort=127.0.0.1:18010:18010\n"
        )
    recorder(context, "systemctl", body='case "$2" in is-active) echo active ;; esac\nexit 0\n')
