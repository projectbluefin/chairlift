"""Prelaunch stubs for the Printers group on the Features page.

Printer applications are rootless quadlets under the scenario HOME's
~/.config/containers/systemd, driven with `systemctl --user`. The group's
scenarios assert that rendering it — three rows, each off — runs no
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
