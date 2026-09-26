"""Prelaunch stubs for the Maintenance page and its Recovery detail.

Every fake tool appends its argv to <scenario>/tool-calls.log, so a scenario
can prove a mutation stayed a dry-run preview: the fake was consulted for
reads, and never received the state-changing subcommand.
"""

import json
import os

from stubs import fake_executable, stub

CALLS_LOG = "tool-calls.log"

# `bootc status --format json` on a host that keeps a previous deployment.
# The versions and timestamps are what the Roll Back row reads its subtitle
# from (pageview.BootcRollbackRow).
BOOTC_STATUS_WITH_ROLLBACK = {
    "spec": {"image": {"image": "ghcr.io/projectbluefin/dakota:latest", "transport": "registry"}},
    "status": {
        "booted": {
            "image": {
                "image": {"image": "ghcr.io/projectbluefin/dakota:latest", "transport": "registry"},
                "version": "44.20260920",
                "timestamp": "2026-09-20T06:00:00Z",
                "imageDigest": "sha256:" + "b" * 64,
            },
            "pinned": False,
        },
        "staged": None,
        "rollback": {
            "image": {
                "image": {"image": "ghcr.io/projectbluefin/dakota:latest", "transport": "registry"},
                "version": "44.20260913",
                "timestamp": "2026-09-13T06:00:00Z",
                "imageDigest": "sha256:" + "a" * 64,
            },
            "pinned": False,
        },
    },
}


def _recorder(context):
    """Shell prologue that records the fake's own name and argv."""
    log = os.path.join(context.scenario_dir, CALLS_LOG)
    return f'printf "%s\\n" "$(basename "$0") $*" >> "{log}"\n'


def _fake_bootc(context, status):
    path = os.path.join(context.scenario_dir, "bootc-status.json")
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(status, handle)
    fake_executable(
        context,
        "bootc",
        _recorder(context)
        + f"""
case "$1" in
  status) cat "{path}" ;;
  upgrade) echo "No changes in: ghcr.io/projectbluefin/dakota:latest" ;;
esac
exit 0
""",
    )


@stub("maintenance_bootc_rollback")
def maintenance_bootc_rollback(context):
    """A bootc host that still keeps the previous deployment, so Recovery offers Roll Back."""
    _fake_bootc(context, BOOTC_STATUS_WITH_ROLLBACK)


@stub("maintenance_bootc_no_rollback")
def maintenance_bootc_no_rollback(context):
    """A freshly installed bootc host: a booted deployment and nothing to return to."""
    status = json.loads(json.dumps(BOOTC_STATUS_WITH_ROLLBACK))
    status["status"]["rollback"] = None
    _fake_bootc(context, status)


@stub("maintenance_package_tools")
def maintenance_package_tools(context):
    """Homebrew, Flatpak and Distrobox that are installed and hold nothing.

    Read-only queries answer with empty inventories; anything else only
    records its argv. The application's --dry-run gate must keep every
    cleanup and removal command from reaching these fakes at all.
    """
    empty_json = '{"formulae":[],"casks":[]}'
    fake_executable(
        context,
        "brew",
        _recorder(context)
        + f"""
case "$1" in
  --version) echo "Homebrew 4.6.0" ;;
  --prefix) echo "/home/linuxbrew/.linuxbrew" ;;
  outdated|info) echo '{empty_json}' ;;
  tap-info) echo '[]' ;;
esac
exit 0
""",
    )
    fake_executable(
        context,
        "flatpak",
        _recorder(context)
        + """
case "$1" in
  --version) echo "Flatpak 1.16.1" ;;
esac
exit 0
""",
    )
    fake_executable(context, "distrobox", _recorder(context) + "exit 0\n")


@stub("maintenance_registry_unreachable")
def maintenance_registry_unreachable(context):
    """No route to any image registry.

    Go's default transport honours HTTPS_PROXY; pointing it at a closed
    loopback port makes every registry request fail at once, without the
    scenario depending on (or making) an outbound request.
    """
    for key in ("HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"):
        context.launch_env[key] = "http://127.0.0.1:9"
    for key in ("NO_PROXY", "no_proxy"):
        context.launch_env.pop(key, None)
