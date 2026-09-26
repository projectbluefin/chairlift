"""Prelaunch stubs for the Updates destination (updates.feature).

The update shell's sources are decided by real tools: `flatpak` for
Applications, `brew` for Developer tools, and `bootc` for the system-version
readout. Each fake here answers the read-only queries ChairLift makes from a
state directory inside the scenario (context.updates_state), so a scenario can
change what the next check discovers, and appends every argv it receives to
<program>.calls there, so a scenario can prove a dry-run never reached it.

Mutating subcommands (flatpak update, brew update/upgrade) are never expected
to arrive: ChairLift runs with --dry-run and gates them before exec. The fakes
exit 0 for them anyway and only record the call.
"""

import json
import os

from stubs import fake_executable, stub

FIREFOX_UPDATE = "Firefox\torg.mozilla.firefox\t131.0\n"
JQ_OUTDATED = {
    "formulae": [
        {"name": "jq", "installed_versions": ["1.7.1"], "current_version": "1.8.0", "pinned": False}
    ],
    "casks": [],
}
NOTHING_OUTDATED = {"formulae": [], "casks": []}

BOOTC_STATUS = {
    "spec": {"image": {"image": "ghcr.io/projectbluefin/dakota:latest", "transport": "registry"}},
    "status": {
        "booted": {
            "image": {
                "image": {"image": "ghcr.io/projectbluefin/dakota:latest", "transport": "registry"},
                "version": "42.20260920.0",
                "timestamp": "2026-09-20T06:00:00Z",
                "imageDigest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
            },
            "pinned": False,
        },
        "staged": None,
        "rollback": None,
    },
}


def state_dir(context):
    path = getattr(context, "updates_state", None)
    if path is None:
        path = os.path.join(context.scenario_dir, "updates-state")
        os.makedirs(path, exist_ok=True)
        context.updates_state = path
    return path


def write_state(context, name, content):
    with open(os.path.join(state_dir(context), name), "w", encoding="utf-8") as handle:
        handle.write(content)


def _install_flatpak(context):
    state = state_dir(context)
    fake_executable(
        context,
        "flatpak",
        f"""
state='{state}'
printf '%s\\n' "$*" >> "$state/flatpak.calls"
case "$1" in
--version) echo "Flatpak 1.16.1"; exit 0 ;;
remote-ls)
    scope=system
    for arg in "$@"; do [ "$arg" = "--user" ] && scope=user; done
    if [ -f "$state/flatpak-remote-ls-$scope.delay" ]; then sleep "$(cat "$state/flatpak-remote-ls-$scope.delay")"; fi
    if [ -f "$state/flatpak-remote-ls-$scope.fail" ]; then
        cat "$state/flatpak-remote-ls-$scope.fail" >&2
        exit 1
    fi
    cat "$state/flatpak-remote-ls-$scope" 2>/dev/null
    exit 0 ;;
esac
exit 0
""",
    )
    for scope in ("user", "system"):
        path = os.path.join(state, f"flatpak-remote-ls-{scope}")
        if not os.path.exists(path):
            write_state(context, f"flatpak-remote-ls-{scope}", "")


def _install_brew(context, outdated):
    state = state_dir(context)
    write_state(context, "brew-outdated.json", json.dumps(outdated))
    fake_executable(
        context,
        "brew",
        f"""
state='{state}'
printf '%s\\n' "$*" >> "$state/brew.calls"
case "$1" in
--version) echo "Homebrew 4.6.0"; exit 0 ;;
--prefix) echo "$state/brew-prefix"; exit 0 ;;
outdated) cat "$state/brew-outdated.json"; exit 0 ;;
tap-info) echo "[]"; exit 0 ;;
info) echo '{{"formulae":[],"casks":[]}}'; exit 0 ;;
esac
exit 0
""",
    )


@stub("updates-flatpak-one-update")
def flatpak_one_update(context):
    """Flatpak with one pending user-installation update (Firefox 131.0)."""
    write_state(context, "flatpak-remote-ls-user", FIREFOX_UPDATE)
    _install_flatpak(context)


@stub("updates-flatpak-current")
def flatpak_current(context):
    """Flatpak installed with nothing to update in either installation."""
    _install_flatpak(context)


@stub("updates-flatpak-slow-check")
def flatpak_slow_check(context):
    """Flatpak whose user update query takes four seconds, then finds Firefox.

    Holds the shell in its checking phase long enough to observe it."""
    write_state(context, "flatpak-remote-ls-user", FIREFOX_UPDATE)
    write_state(context, "flatpak-remote-ls-user.delay", "4")
    _install_flatpak(context)


@stub("updates-flatpak-check-fails")
def flatpak_check_fails(context):
    """Flatpak whose update queries both fail, as with an unreachable remote."""
    for scope in ("user", "system"):
        write_state(context, f"flatpak-remote-ls-{scope}.fail", "error: Unable to load summary from remote flathub\n")
    _install_flatpak(context)


@stub("updates-brew-one-outdated")
def brew_one_outdated(context):
    """Homebrew with one outdated formula (jq 1.7.1)."""
    _install_brew(context, JQ_OUTDATED)


@stub("updates-brew-current")
def brew_current(context):
    """Homebrew installed with nothing outdated."""
    _install_brew(context, NOTHING_OUTDATED)


@stub("updates-bootc-booted")
def bootc_booted(context):
    """A bootc host booted from dakota 42.20260920.0 with nothing staged."""
    state = state_dir(context)
    write_state(context, "bootc-status.json", json.dumps(BOOTC_STATUS))
    fake_executable(
        context,
        "bootc",
        f"""
state='{state}'
printf '%s\\n' "$*" >> "$state/bootc.calls"
case "$1" in
status) cat "$state/bootc-status.json"; exit 0 ;;
upgrade) echo "No changes in: ghcr.io/projectbluefin/dakota:latest"; exit 0 ;;
esac
exit 1
""",
    )


@stub("updates-image-dakota-stable")
def image_dakota_stable(context):
    """The image descriptor of dakota:stable, the stream that publishes an
    NVIDIA variant, so an NVIDIA host is offered a driver switch."""
    path = context.launch_env["CHAIRLIFT_IMAGE_INFO"]
    with open(path, encoding="utf-8") as handle:
        info = json.load(handle)
    info["image-tag"] = "stable"
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(info, handle)
