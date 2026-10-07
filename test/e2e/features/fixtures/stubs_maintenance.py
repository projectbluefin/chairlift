"""Prelaunch stubs for the Maintenance page and its Powerwash detail.

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
    """A bootc host that still keeps the previous deployment, so Powerwash offers Roll Back."""
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


@stub("maintenance_powerwash_inventory")
def maintenance_powerwash_inventory(context):
    """Flatpak and Distrobox that each hold something for Powerwash to remove.

    Applies @stub.maintenance_package_tools first and replaces its empty
    Flatpak and Distrobox fakes: behave hands tags over as an unordered set,
    so a scenario cannot rely on listing one stub after another. Powerwash
    reads the user installation and the container list before removing, so
    only a non-empty inventory previews a removal.
    """
    maintenance_package_tools(context)
    fake_executable(
        context,
        "flatpak",
        _recorder(context)
        + """
case "$*" in
  --version) echo "Flatpak 1.16.1" ;;
  "list --user --app "*) printf 'Firefox\\torg.mozilla.firefox\\t128.0\\n' ;;
esac
exit 0
""",
    )
    fake_executable(
        context,
        "distrobox",
        _recorder(context)
        + """
case "$1" in
  list)
    echo "ID           | NAME                 | STATUS             | IMAGE"
    echo "2f3a9c1b0d4e | fedora               | Up 2 hours         | registry.fedoraproject.org/fedora-toolbox:41"
    ;;
esac
exit 0
""",
    )
