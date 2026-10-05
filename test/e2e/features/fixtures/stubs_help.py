"""Prelaunch stubs for the Help destination (help.feature).

Everything the Help page reads from the host is stubbed here so its
assertions do not depend on what the runner happens to have installed:

* xdg-open, which the resource rows spawn, records its argv into the
  scenario directory instead of opening a browser.

Troubleshooting's Goose row lives on the Agents page; its stubs are in
fixtures/stubs_agents.py.
"""

import os

from stubs import fake_executable, stub

URL_RECORD = "xdg-open.calls"


def _recorder(context, program, record, exit_code=0):
    path = os.path.join(context.scenario_dir, record)
    fake_executable(
        context,
        program,
        f"""
printf '%s\\n' "$*" >> '{path}'
exit {exit_code}
""",
    )


@stub("help-xdg-open")
def xdg_open(context):
    """xdg-open that records the URL it was asked to open and succeeds."""
    _recorder(context, "xdg-open", URL_RECORD)


@stub("help-xdg-open-fails")
def xdg_open_fails(context):
    """xdg-open on a session with no URL handler: records, then exits 4."""
    _recorder(context, "xdg-open", URL_RECORD, exit_code=4)
