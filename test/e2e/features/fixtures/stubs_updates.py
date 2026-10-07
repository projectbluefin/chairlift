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

import copy
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
# The update check queries each remote separately (issue #471): every
# installation has flathub, which its installed refs come from, and with
# flatpak-leftover-remote also test-center, which is unreachable and serves
# nothing installed.
remotes)
    echo flathub
    [ -f "$state/flatpak-leftover-remote" ] && echo test-center
    exit 0 ;;
list)
    case "$*" in *--columns=origin*) echo flathub ;; esac
    exit 0 ;;
remote-ls)
    for arg in "$@"; do remote=$arg; done
    if [ "$remote" = test-center ]; then
        echo "error: Unable to load summary from remote test-center: Could not resolve hostname" >&2
        exit 1
    fi
    scope=system
    for arg in "$@"; do [ "$arg" = "--user" ] && scope=user; done
    if [ -f "$state/flatpak-remote-ls-$scope.hold" ]; then
        # Held until a step releases it (bounded, so a broken scenario
        # cannot hang the run), not for a fixed time: a fixed delay raced
        # application startup on a slow runner.
        for _ in $(seq 1 600); do [ -f "$state/flatpak-remote-ls-$scope.release" ] && break; sleep 0.1; done
    fi
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
tap-info)
    if [ -f "$state/brew-tap-info.fail" ]; then
        cat "$state/brew-tap-info.fail" >&2
        exit 1
    fi
    if [ -f "$state/brew-sources.fail" ]; then cat "$state/brew-sources.fail" >&2; exit 1; fi
    if [ -f "$state/brew-taps.json" ]; then cat "$state/brew-taps.json"; else echo "[]"; fi
    exit 0 ;;
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
    """Flatpak whose user update query waits for a step to release it, then finds Firefox.

    Holds the shell in its checking phase until `the Flatpak update check is
    allowed to finish`, so the phase is observed however slow startup is."""
    write_state(context, "flatpak-remote-ls-user", FIREFOX_UPDATE)
    write_state(context, "flatpak-remote-ls-user.hold", "")
    _install_flatpak(context)


@stub("updates-flatpak-check-fails")
def flatpak_check_fails(context):
    """Flatpak whose update queries both fail because the remote's host cannot be resolved."""
    for scope in ("user", "system"):
        write_state(
            context,
            f"flatpak-remote-ls-{scope}.fail",
            "error: Unable to load summary from remote flathub: While fetching "
            "https://dl.flathub.org/repo/summary.idx: [6] Could not resolve hostname\n",
        )
    _install_flatpak(context)


@stub("updates-flatpak-check-fails-locally")
def flatpak_check_fails_locally(context):
    """Flatpak whose update queries both fail for a reason that is not the network."""
    for scope in ("user", "system"):
        write_state(
            context,
            f"flatpak-remote-ls-{scope}.fail",
            "error: Unable to load summary from remote flathub: "
            "GPG verification enabled, but no summary signatures found\n",
        )
    _install_flatpak(context)


@stub("updates-flatpak-leftover-remote")
def flatpak_leftover_remote(context):
    """Flatpak with Firefox pending on flathub beside a leftover, unreachable
    test-center remote that no installed ref comes from (issue #471)."""
    write_state(context, "flatpak-remote-ls-user", FIREFOX_UPDATE)
    write_state(context, "flatpak-leftover-remote", "")
    _install_flatpak(context)


@stub("updates-brew-one-outdated")
def brew_one_outdated(context):
    """Homebrew with one outdated formula (jq 1.7.1)."""
    _install_brew(context, JQ_OUTDATED)


@stub("updates-brew-current")
def brew_current(context):
    """Homebrew installed with nothing outdated."""
    _install_brew(context, NOTHING_OUTDATED)


@stub("updates-brew-trust-check-fails")
def brew_trust_check_fails(context):
    write_state(context, "brew-tap-info.fail", "Error: Broken pipe\n")
    _install_brew(context, NOTHING_OUTDATED)


@stub("updates-brew-sources-fail")
def brew_sources_fail(context):
    _install_brew(context, NOTHING_OUTDATED)
    write_state(context, "brew-sources.fail", "source list unavailable\n")


@stub("updates-brew-untrusted")
def brew_untrusted(context):
    _install_brew(context, NOTHING_OUTDATED)
    write_state(context, "brew-taps.json", json.dumps([{"name": "vendor/tap", "trusted": False}]))
    receipt_dir = os.path.join(state_dir(context), "brew-prefix", "Cellar", "example", "1.0")
    os.makedirs(receipt_dir, exist_ok=True)
    with open(os.path.join(receipt_dir, "INSTALL_RECEIPT.json"), "w", encoding="utf-8") as handle:
        json.dump({"source": {"tap": "vendor/tap"}}, handle)
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


@stub("updates-bootc-staged")
def bootc_staged(context):
    """A deployment awaiting restart, with no newer image to download."""
    bootc_booted(context)
    status = copy.deepcopy(BOOTC_STATUS)
    staged = copy.deepcopy(status["status"]["booted"])
    staged["image"]["version"] = "42.20261002.0"
    staged["image"]["imageDigest"] = "sha256:" + "a" * 64
    status["status"]["staged"] = staged
    write_state(context, "bootc-status.json", json.dumps(status))


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
