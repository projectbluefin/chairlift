"""Prelaunch stubs for the Livery page (features/livery.feature).

Every program internal/livery may run (its allowedCommands: gsettings, dconf,
gtk-update-icon-cache, kwriteconfig6, systemctl) is replaced by a recorder
that appends its argv to <scenario>/livery-calls.log, so a scenario can prove
what the page did and did not run. The recorders' behaviour is steered by
files in the scenario directory, so the stubs below compose in any order:

    livery-extension-missing   the Custom Command Menu extension's schema is
                               absent (otherwise it is answered as installed,
                               the way Bluefin ships it)
    livery-seed.sed            sed script applied to ChairLift's own schema
                               listing: the "saved" Livery state at launch
    livery-user-panel-icon     the user-layer value dconf reports for the
                               extension's icon key (otherwise none: the key
                               resolves to the distro default)
    livery-background-current  the picture-uri GNOME's background schema
                               reports (otherwise the real gsettings answers)
    livery-background-broken   every read of GNOME's background schema fails,
                               the way a hung or broken dconf does

The real gsettings still answers ChairLift's own schema (GSETTINGS_BACKEND is
memory, so nothing persists), and the real systemctl/dconf answer everything
the Livery page does not own, so other pages behave as they would unstubbed.
"""

import os
import shutil
import struct
import zlib

from stubs import fake_executable, stub

CALLS = "livery-calls.log"
SEED = "livery-seed.sed"
EXTENSION_MISSING = "livery-extension-missing"
USER_PANEL_ICON = "livery-user-panel-icon"
BACKGROUND_CURRENT = "livery-background-current"
BACKGROUND_BROKEN = "livery-background-broken"
BACKGROUND_SCHEMA = "org.gnome.desktop.background"

EXTENSION_SCHEMA = "org.gnome.shell.extensions.custom-command-list"
EXTENSION_PATH = "/org/gnome/shell/extensions/custom-command-list"
LIVERY_SCHEMA = "io.projectbluefin.chairlift.livery"
# What a Bluefin host's distro dconf layer answers for the extension.
DISTRO_PANEL_ICON = "'ublue-logo-symbolic'"
DISTRO_PANEL_MODE = "2"


def _real(program):
    """The program the launch PATH would find without the stub directory."""
    return shutil.which(program, path=os.environ.get("PATH", "")) or ""


def _record(context):
    calls = os.path.join(context.scenario_dir, CALLS)
    return f'printf \'%s\\n\' "$(basename "$0") $*" >> "{calls}"\n'


def _install(context):
    if getattr(context, "livery_stubs_installed", False):
        return
    context.livery_stubs_installed = True
    scenario = context.scenario_dir
    record = _record(context)
    real_gsettings = _real("gsettings")
    open(os.path.join(scenario, CALLS), "a").close()
    open(os.path.join(scenario, SEED), "a").close()

    fake_executable(
        context,
        "gsettings",
        record
        + f"""
REAL="{real_gsettings}"
if [ "$2" = "{EXTENSION_SCHEMA}" ]; then
    if [ -e "{scenario}/{EXTENSION_MISSING}" ]; then
        echo "No such schema \\"{EXTENSION_SCHEMA}\\"" >&2
        exit 1
    fi
    case "$1 $3" in
        "get menuicon-setting") echo "{DISTRO_PANEL_ICON}"; exit 0 ;;
        "get menuoptions-setting") echo "{DISTRO_PANEL_MODE}"; exit 0 ;;
    esac
    exit 0
fi
if [ "$2" = "{BACKGROUND_SCHEMA}" ]; then
    if [ -e "{scenario}/{BACKGROUND_BROKEN}" ]; then
        echo "error: the settings backend is unreachable" >&2
        exit 1
    fi
    if [ "$1 $3" = "get picture-uri" ] && [ -s "{scenario}/{BACKGROUND_CURRENT}" ]; then
        cat "{scenario}/{BACKGROUND_CURRENT}"
        exit 0
    fi
fi
[ -n "$REAL" ] || {{ echo "gsettings is not installed" >&2; exit 1; }}
if [ "$1 $2" = "list-recursively {LIVERY_SCHEMA}" ]; then
    out=$("$REAL" "$@") || {{ printf '%s\\n' "$out"; exit 1; }}
    printf '%s\\n' "$out" | sed -f "{scenario}/{SEED}"
    exit 0
fi
exec "$REAL" "$@"
""",
    )

    real_dconf = _real("dconf")
    fake_executable(
        context,
        "dconf",
        record
        + f"""
case "$1 $2 $3" in
    "read {EXTENSION_PATH}/menuicon-setting ")
        if [ -s "{scenario}/{USER_PANEL_ICON}" ]; then cat "{scenario}/{USER_PANEL_ICON}"
        else echo "{DISTRO_PANEL_ICON}"; fi
        exit 0 ;;
    "read -d {EXTENSION_PATH}/menuicon-setting") echo "{DISTRO_PANEL_ICON}"; exit 0 ;;
    "read {EXTENSION_PATH}/menuoptions-setting "|"read -d {EXTENSION_PATH}/menuoptions-setting")
        echo "{DISTRO_PANEL_MODE}"; exit 0 ;;
esac
REAL="{real_dconf}"
[ -n "$REAL" ] && exec "$REAL" "$@"
exit 0
""",
    )

    # Only the Livery page runs these two, and only to write; the recorder is
    # the whole stub.
    for program in ("gtk-update-icon-cache", "kwriteconfig6"):
        fake_executable(context, program, record + "exit 0\n")

    # systemctl is recorded only where it exists, so a runner without it
    # keeps answering "not installed" to the pages that probe for it.
    real_systemctl = _real("systemctl")
    if real_systemctl:
        fake_executable(context, "systemctl", record + f'exec "{real_systemctl}" "$@"\n')


def _seed(context, values):
    """Report values as ChairLift's saved Livery settings at launch."""
    _install(context)
    with open(os.path.join(context.scenario_dir, SEED), "a", encoding="utf-8") as handle:
        for key, value in values.items():
            prefix = LIVERY_SCHEMA.replace(".", r"\.") + " " + key
            handle.write(f"s|^\\({prefix}\\) .*|\\1 {value}|\n")


@stub("livery-tools")
def livery_tools(context):
    """Record every Livery command; the panel extension is installed."""
    _install(context)


@stub("livery-no-extension")
def livery_no_extension(context):
    """The Custom Command Menu extension is not installed on this host."""
    _install(context)
    open(os.path.join(context.scenario_dir, EXTENSION_MISSING), "w").close()


@stub("livery-user-panel-icon")
def livery_user_panel_icon(context):
    """The user set their own panel icon, over the distro default."""
    _install(context)
    with open(os.path.join(context.scenario_dir, USER_PANEL_ICON), "w", encoding="utf-8") as handle:
        handle.write("'starred-symbolic'\n")


@stub("livery-saved-state")
def livery_saved_state(context):
    """A user who configured every section in an earlier session."""
    _seed(
        context,
        {
            "app-grid-enabled": "true",
            "app-grid-slug": "'gitlab'",
            "panel-enabled": "true",
            "panel-foundation": "'gnome'",
            "panel-rotate": "true",
            "dock-enabled": "true",
            "dock-foundation": "'prometheus'",
            "dock-rotate": "true",
        },
    )


@stub("livery-panel-on")
def livery_panel_on(context):
    """The panel mark was turned on in an earlier session."""
    _seed(context, {"panel-enabled": "true"})


@stub("livery-dock-on")
def livery_dock_on(context):
    """The Files mark was turned on in an earlier session."""
    _seed(context, {"dock-enabled": "true"})


@stub("livery-app-grid-on")
def livery_app_grid_on(context):
    """The app-grid surface was enabled before the current preview."""
    _seed(context, {"app-grid-enabled": "true"})


@stub("livery-offline")
def livery_offline(context):
    """No artwork service is reachable: every HTTPS fetch is refused.

    Go's default transport honours the proxy variables, so pointing them at
    a closed loopback port makes simpleicons.org and cncf/artwork fail fast
    and identically on every runner.
    """
    for key in ("HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"):
        context.launch_env[key] = "http://127.0.0.1:9"
    for key in ("NO_PROXY", "no_proxy"):
        context.launch_env.pop(key, None)


def _png():
    """A valid 1x1 PNG, so GdkPixbuf has a real picture to scale."""

    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    header = struct.pack(">IIBBBBB", 1, 1, 8, 2, 0, 0, 0)
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", header)
        + chunk(b"IDAT", zlib.compress(b"\x00\x33\x66\x99"))
        + chunk(b"IEND", b"")
    )


# The shape a ublue-os wallpaper cask installs under $XDG_DATA_HOME on GNOME
# (see ublue-os/homebrew-tap's bluefin-wallpapers): pictures in
# backgrounds/<collection> and one gnome-background-properties entry per
# picture, with "~" already replaced. The collection directory is the
# fixture's own, so the image's real catalogs in XDG_DATA_DIRS never merge
# into it and its picture count stays exact.
COLLECTION = "chairlift-e2e"
WALLPAPERS = (
    ("01-bluefin.png", "Bluefin 01 - January"),
    ("02-bluefin.png", "Bluefin 02 - February"),
)


def _install_wallpaper_channel(context):
    data_home = context.launch_env["XDG_DATA_HOME"]
    pictures = os.path.join(data_home, "backgrounds", COLLECTION)
    catalog = os.path.join(data_home, "gnome-background-properties")
    os.makedirs(pictures, exist_ok=True)
    os.makedirs(catalog, exist_ok=True)
    for file_name, name in WALLPAPERS:
        picture = os.path.join(pictures, file_name)
        with open(picture, "wb") as handle:
            handle.write(_png())
        with open(os.path.join(catalog, file_name.replace(".png", ".xml")), "w", encoding="utf-8") as handle:
            handle.write(
                '<?xml version="1.0"?>\n<!DOCTYPE wallpapers SYSTEM "gnome-wp-list.dtd">\n'
                f'<wallpapers><wallpaper deleted="false"><name>{name}</name>'
                f"<filename>{picture}</filename><options>zoom</options></wallpaper></wallpapers>\n"
            )
    return os.path.join(pictures, WALLPAPERS[0][0])


@stub("livery-wallpaper-channels")
def livery_wallpaper_channels(context):
    """A wallpaper collection is installed and its January picture is current."""
    _install(context)
    current = _install_wallpaper_channel(context)
    with open(os.path.join(context.scenario_dir, BACKGROUND_CURRENT), "w", encoding="utf-8") as handle:
        handle.write(f"'file://{current}'\n")


@stub("livery-background-broken")
def livery_background_broken(context):
    """GNOME's background settings cannot be read on this host."""
    _install(context)
    open(os.path.join(context.scenario_dir, BACKGROUND_BROKEN), "w").close()
