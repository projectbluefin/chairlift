"""Prelaunch stubs for the Features page: Developer tools, Gaming, and the
Custom Command Menu that Developer Mode drives.

Every fake appends its own name and argv to <scenario>/features-calls.log, so
a scenario can prove what ChairLift asked a tool for: the reads a page restore
needs, and never a state-changing subcommand while --dry-run is in force.

The gaming stubs stand in for `flatpak list`, which internal/gaming reads in
four queries (user/system x app/runtime) to decide the Gaming switch. Every
other flatpak subcommand answers with nothing and exit 0, so the Apps and
Updates pages that share the fake see an empty, healthy host.
"""

import grp
import json
import os

from stubs import fake_executable, stub

CALLS_LOG = "features-calls.log"

# internal/gaming's stack, in install order, with the listing kind each
# component is published as (MangoHud is a runtime extension).
GAMING_COMPONENTS = (
    ("ProtonPlus", "com.vysp3r.ProtonPlus", "app"),
    ("Protontricks", "com.github.Matoking.protontricks", "app"),
    ("GOverlay", "io.github.benjamimgois.goverlay", "app"),
    ("MangoHud", "org.freedesktop.Platform.VulkanLayer.MangoHud", "runtime"),
)

# ubluehelper.DevGroups: membership in any one is "developer mode on".
DEVELOPER_GROUPS = ("docker", "incus-admin", "libvirt", "dialout")

DEVMENU_PATH = "/org/gnome/shell/extensions/custom-command-list/"


def account_is_developer():
    """What internal/ublue will report for the account running the suite.

    ChairLift reads supplementary group names for the invoking user, which
    no stub can change. A CI runner is commonly in `docker`; the Dakota
    container user is in none. Scenarios derive their expectation from this
    instead of assuming either host.
    """
    names = set()
    for gid in os.getgroups():
        try:
            names.add(grp.getgrgid(gid).gr_name)
        except KeyError:
            continue
    return bool(names & set(DEVELOPER_GROUPS))


def _recorder(context):
    log = os.path.join(context.scenario_dir, CALLS_LOG)
    return f'printf "%s\\n" "$(basename "$0") $*" >> "{log}"\n'


# The runtime branch the stubbed `flatpak info`/`remote-info` report for any
# app (Steam or Protontricks), which MangoHud is installed in.
STEAM_PLATFORM_BRANCH = "26.08"


def _listing(components):
    """`flatpak list --columns=name,application,version,branch` rows."""
    return "".join(
        f"{name}\t{app_id}\t1.0\t{STEAM_PLATFORM_BRANCH if kind == 'runtime' else 'stable'}\n"
        for name, app_id, kind in components
    )


def _fake_flatpak(context, user=(), system=(), list_fails=False, remotes=("flathub", "flatpark")):
    """A flatpak whose four gaming listings return the given components.

    `remotes` is the system remote list: Bluefin-family images ship
    Flathub and FlatPark, which Compass installs from."""
    remote_rows = "".join(f"{name}\\t\\n" for name in remotes)
    listings = {}
    for scope, components in (("user", user), ("system", system)):
        for kind in ("app", "runtime"):
            path = os.path.join(context.scenario_dir, f"flatpak-{scope}-{kind}.txt")
            with open(path, "w", encoding="utf-8") as handle:
                handle.write(_listing([c for c in components if c[2] == kind]))
            listings[(scope, kind)] = path

    if list_fails:
        list_branch = 'echo "error: No installations available" >&2; exit 1'
    else:
        list_branch = f"""
    scope=system; kind=app
    for arg in "$@"; do
      case "$arg" in
        --user) scope=user ;;
        --runtime) kind=runtime ;;
      esac
    done
    case "$scope-$kind" in
      user-app) cat "{listings[("user", "app")]}" ;;
      user-runtime) cat "{listings[("user", "runtime")]}" ;;
      system-app) cat "{listings[("system", "app")]}" ;;
      system-runtime) cat "{listings[("system", "runtime")]}" ;;
    esac"""

    fake_executable(
        context,
        "flatpak",
        _recorder(context)
        + f"""
case "$1" in
  --version) echo "Flatpak 1.16.0" ;;
  remotes) printf "{remote_rows}" ;;
  list) {list_branch}
    ;;
  info|remote-info) echo "org.freedesktop.Platform/x86_64/{STEAM_PLATFORM_BRANCH}" ;;
esac
exit 0
""",
    )


@stub("features-gaming-none")
def gaming_none(context):
    """A host with none of the gaming stack installed."""
    _fake_flatpak(context)


@stub("features-gaming-installed")
def gaming_installed(context):
    """Every gaming component installed system-wide, where ChairLift puts it."""
    _fake_flatpak(context, system=GAMING_COMPONENTS)


@stub("features-gaming-unlistable")
def gaming_unlistable(context):
    """A flatpak that cannot list any installation."""
    _fake_flatpak(context, list_fails=True)


# internal/compass's application, as `flatpak list` reports it.
COMPASS = ("Compass", "org.tunaos.compass", "app")


@stub("features-compass-installed")
def compass_installed(context):
    """Compass installed system-wide, where ChairLift puts it."""
    _fake_flatpak(context, system=(COMPASS,))


@stub("features-compass-no-remote")
def compass_no_remote(context):
    """A host whose system installation has Flathub but no FlatPark."""
    _fake_flatpak(context, remotes=("flathub",))


def _tuple(label, command, icon, visible):
    return f"('{label}', '{command}', '{icon}', {'true' if visible else 'false'})"


@stub("features-devmenu")
def devmenu(context):
    """The Custom Command Menu extension with Bluefin-style entries.

    command1/command2 are the two developer entries internal/devmenu manages;
    their current visibility matches the account's real developer state, as
    it would on a host where Developer Mode last applied it. command3 is an
    unmanaged entry. `dconf read -d` answers the distro defaults: Terminal
    visible, Containers hidden, so whichever direction a toggle goes, one
    managed key is a reset and the other a write.
    """
    visible = account_is_developer()
    dump = "\n".join(
        [
            "[/]",
            "command1=" + _tuple("Terminal", "ptyxis --new-window", "utilities-terminal-symbolic", visible),
            "command2=" + _tuple("Containers", "flatpak run io.podman_desktop.PodmanDesktop", "podman-symbolic", visible),
            "command3=" + _tuple("Software", "gnome-software", "system-software-install-symbolic", True),
            "",
        ]
    )
    defaults = {
        "command1": _tuple("Terminal", "ptyxis --new-window", "utilities-terminal-symbolic", True),
        "command2": _tuple("Containers", "flatpak run io.podman_desktop.PodmanDesktop", "podman-symbolic", False),
        "command3": _tuple("Software", "gnome-software", "system-software-install-symbolic", True),
    }
    dump_path = os.path.join(context.scenario_dir, "dconf-dump.txt")
    with open(dump_path, "w", encoding="utf-8") as handle:
        handle.write(dump)
    default_cases = "\n".join(
        f'      "{DEVMENU_PATH}{key}") echo "{value}" ;;' for key, value in defaults.items()
    )
    fake_executable(
        context,
        "dconf",
        _recorder(context)
        + f"""
case "$1" in
  dump)
    [ "$2" = "{DEVMENU_PATH}" ] && cat "{dump_path}"
    ;;
  read)
    if [ "$2" = "-d" ]; then
      case "$3" in
{default_cases}
      esac
    fi
    ;;
esac
exit 0
""",
    )


def _write_descriptor(context, descriptor):
    path = os.path.join(context.scenario_dir, "features-image-info.json")
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(descriptor, handle)
    context.launch_env["CHAIRLIFT_IMAGE_INFO"] = path


@stub("features-gaming-image")
def gaming_image(context):
    """A dakota-gaming descriptor: the image ships Steam itself."""
    _write_descriptor(
        context,
        {
            "image-name": "dakota-gaming",
            "image-tag": "latest",
            "image-ref": "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota-gaming",
            "image-vendor": "projectbluefin",
            "image-flavor": "gaming",
        },
    )


@stub("features-no-descriptor")
def no_descriptor(context):
    """A host with no ublue-os image descriptor (every non-Bluefin system)."""
    context.launch_env["CHAIRLIFT_IMAGE_INFO"] = os.path.join(context.scenario_dir, "absent-image-info.json")


@stub("features-developer")
def developer_options(context):
    """Running WSL guests and image-provided, inactive system Docker units."""
    fake_executable(context, "nsl", _recorder(context) + """
case "$1" in
  list) printf '%s\n' 'VM\tSTATE\tIMAGE\tRESOURCES\tDATA DISK' 'shared\trunning\t-\t4 CPUs, 8 GiB\t20 GiB' '' 'MACHINE\tSTATE\tIMAGE\tTIER\tDEFAULT' 'debian\trunning\tdebian:13\tshared\t*' ;;
  run) exit 0 ;;
  doctor) exit 0 ;;
esac
exit 0
""")
    fake_executable(context, "limactl", _recorder(context) + """
case "$1" in
  list) echo '{"name":"ubuntu","status":"Running"}' ;;
  shell) exit 0 ;;
esac
exit 0
""")
    docker_inactive(context)
    fake_executable(context, "brew", _recorder(context) + """
case "$1" in
  --version) echo 'Homebrew 5.0.0' ;;
  info) echo '{"formulae":[],"casks":[]}' ;;
esac
exit 0
""")
    fake_executable(context, "code", _recorder(context) + "exit 97\n")
    context.launch_env["SETUP_CHECKER_FILE"] = os.path.join(context.home, ".local", "share", "ublue", "setup_versioning.json")


@stub("features-developer-packages")
def developer_packages(context):
    """A local optional collection, parsed through the real Brewfile reader."""
    directory = os.path.join(context.scenario_dir, "bundles")
    os.makedirs(directory, exist_ok=True)
    path = os.path.join(directory, "dakota-dev-flatpaks.Brewfile")
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(
            '# Developer applications\n'
            'brew "just"\n'
            'cask "visual-studio-code"\n'
            'flatpak "io.podman_desktop.PodmanDesktop"\n'
        )

def docker_inactive(context):
    """Both image-provided system units are readable and stopped."""
    fake_executable(context, "systemctl", _recorder(context) + """
case "$1" in
  show) printf '%s\\n' 'LoadState=loaded' 'ActiveState=inactive' ;;
  *) exit 1 ;;
esac
""")


def docker_held_failure(context):
    """Read both SetDocker prerequisites, then hold/fail its Verify read."""
    release = os.path.join(context.scenario_dir, "release-docker-status")
    reads = os.path.join(context.scenario_dir, "docker-prerequisite-reads")
    fake_executable(context, "systemctl", _recorder(context) + f"""
case "$1" in
  show)
    count=0
    [ ! -f "{reads}" ] || count=$(cat "{reads}")
    count=$((count + 1))
    printf '%s\\n' "$count" > "{reads}"
    if [ "$count" -le 2 ]; then
      printf '%s\\n' 'LoadState=loaded' 'ActiveState=inactive'
      exit 0
    fi
    while [ ! -f "{release}" ]; do sleep 0.1; done
    echo 'Failed to connect to system scope bus: No medium found' >&2
    exit 1
    ;;
  *) exit 1 ;;
esac
""")


@stub("features-docker-active")
def docker_active(context):
    """Both system units run, but the fixed system socket does not respond."""
    fake_executable(context, "systemctl", _recorder(context) + """
case "$1" in
  show) printf '%s\\n' 'LoadState=loaded' 'ActiveState=active' ;;
  *) exit 1 ;;
esac
""")


@stub("features-docker-no-manager")
def docker_no_manager(context):
    """A failed, unprivileged read of the system manager."""
    fake_executable(context, "systemctl", _recorder(context) + """
echo 'Failed to connect to system scope bus: No medium found' >&2
exit 1
""")


@stub("features-docker-missing")
def docker_missing(context):
    """The system image has neither required Docker unit."""
    fake_executable(context, "systemctl", _recorder(context) + """
case "$1" in
  show) printf '%s\\n' 'LoadState=not-found' 'ActiveState=inactive' ;;
  *) exit 1 ;;
esac
""")


@stub("features-docker-masked")
def docker_masked(context):
    """A loaded service cannot make an inactive masked socket usable."""
    fake_executable(context, "systemctl", _recorder(context) + """
case "$1 $2" in
  'show docker.service') printf '%s\\n' 'LoadState=loaded' 'ActiveState=inactive' ;;
  'show docker.socket') printf '%s\\n' 'LoadState=masked' 'ActiveState=inactive' ;;
  *) exit 1 ;;
esac
""")


@stub("features-docker-active-missing-socket")
def docker_active_missing_socket(context):
    """A running service remains stoppable when its paired unit is missing."""
    fake_executable(context, "systemctl", _recorder(context) + """
case "$1 $2" in
  'show docker.service') printf '%s\\n' 'LoadState=loaded' 'ActiveState=active' ;;
  'show docker.socket') printf '%s\\n' 'LoadState=not-found' 'ActiveState=inactive' ;;
  *) exit 1 ;;
esac
""")


@stub("features-docker-second-read-fails")
def docker_second_read_fails(context):
    """One active unit is not an observed state when its paired read fails."""
    fake_executable(context, "systemctl", _recorder(context) + """
case "$1 $2" in
  'show docker.service') printf '%s\\n' 'LoadState=loaded' 'ActiveState=active' ;;
  'show docker.socket') echo 'Failed to read docker.socket' >&2; exit 1 ;;
  *) exit 1 ;;
esac
""")


@stub("features-vscode-settings")
def vscode_settings(context):
    """An existing native VS Code JSONC configuration must remain untouched."""
    context.vscode_original = b'// Keep my preferences\n{\n  "editor.fontSize": 17,\n}\n'
    path = os.path.join(context.home, ".config", "Code", "User", "settings.json")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as handle:
        handle.write(context.vscode_original)


@stub("features-vscode-bad-marker")
def vscode_bad_marker(context):
    """A malformed record must not become a false Docker-success toast."""
    path = os.path.join(context.home, ".local", "share", "ublue", "setup_versioning.json")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write("not JSON")
    context.launch_env["SETUP_CHECKER_FILE"] = path

def _shell_extensions(context, enabled, missing=False):
    installed = "" if missing else "tailscale-gnome-qs@tailscale-qs.github.io\nsyncthing-toggle@projectbluefin.io"
    fake_executable(
        context,
        "gnome-extensions",
        _recorder(context) + f"""
case "$*" in
  list) printf '%s\\n' '{installed}' ;;
  'list --enabled') printf '%s\\n' '{enabled}' ;;
  *) exit 1 ;;
esac
""",
    )


@stub("features-extensions")
def shell_extensions(context):
    _shell_extensions(context, "tailscale-gnome-qs@tailscale-qs.github.io")


@stub("features-extensions-reversed")
def shell_extensions_reversed(context):
    _shell_extensions(context, "syncthing-toggle@projectbluefin.io")


@stub("features-extensions-missing")
def shell_extensions_missing(context):
    _shell_extensions(context, "", missing=True)
