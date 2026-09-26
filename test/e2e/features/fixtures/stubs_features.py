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
    ("Steam", "com.valvesoftware.Steam", "app"),
    ("ProtonUp-Qt", "net.davidotek.pupgui2", "app"),
    ("Protontricks", "com.github.Matoking.protontricks", "app"),
    ("GOverlay", "io.github.benjamimgois.goverlay", "app"),
    ("MangoHud", "org.freedesktop.Platform.VulkanLayer.MangoHud", "runtime"),
    ("Flatseal", "com.github.tchx84.Flatseal", "app"),
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


def _listing(components):
    return "".join(f"{name}\t{app_id}\t1.0\n" for name, app_id, _ in components)


def _fake_flatpak(context, user=(), system=(), list_fails=False):
    """A flatpak whose four gaming listings return the given components."""
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
  list) {list_branch}
    ;;
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
    """Every gaming component installed in the user scope."""
    _fake_flatpak(context, user=GAMING_COMPONENTS)


@stub("features-gaming-partial")
def gaming_partial(context):
    """Only Steam installed: one core component of two, so gaming is off."""
    _fake_flatpak(context, user=GAMING_COMPONENTS[:1])


@stub("features-gaming-system")
def gaming_system(context):
    """Every gaming component preinstalled system-wide by the image."""
    _fake_flatpak(context, system=GAMING_COMPONENTS)


@stub("features-gaming-unlistable")
def gaming_unlistable(context):
    """A flatpak that cannot list any installation."""
    _fake_flatpak(context, list_fails=True)


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
