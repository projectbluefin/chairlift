"""Prelaunch stubs for the Apps destination (applications_page).

The Apps page reads Homebrew inventories and configured Brewfiles. PATH stubs
serve a fixed inventory and record calls; one scenario supplies Flatpak apps
to prove the Homebrew-only page never lists them.

Every stub records its argv, one invocation per line, in
<scenario>/<program>-calls.log. Under --dry-run ChairLift must never execute
a state-changing command, so the log is also the evidence that a mutation was
only previewed.
"""

import json
import os

from stubs import fake_executable, stub

# The installed inventory `brew info --installed --json=v2` reports. Only
# the fields internal/homebrew.parsePackagesJSON reads are populated.
FORMULAE = [
    {"name": "jq", "installed": [{"version": "1.7.1", "installed_on_request": True}], "pinned": False},
    {"name": "ripgrep", "installed": [{"version": "14.1.1", "installed_on_request": True}], "pinned": True},
    # Installed dependency is real inventory, but not a requested tool.
    {"name": "libunistring", "installed": [{"version": "1.3", "installed_on_request": False}], "pinned": False},
    # A formula brew knows about but has no installed keg for is skipped.
    {"name": "not-installed", "installed": [], "pinned": False},
]
CASKS = [
    {"token": "visual-studio-code", "version": "1.104.0", "installed": "1.104.0"},
]

# `flatpak list --<scope> --app --columns=name,application,version` rows.
FLATPAK_USER = (("Firefox", "org.mozilla.firefox", "140.0"),)
FLATPAK_SYSTEM = (("Text Editor", "org.gnome.TextEditor", "48.0"),)

APP_COLLECTIONS = {
    # A collection Bluefin ships: its title and summary come from
    # internal/views/bundleview's curated table, never from the file.
    "cli.Brewfile": (
        '# Bluefin terminal environment\n'
        'brew "just"\n'
    ),
    "fonts.Brewfile": (
        'cask "font-noto-sans"\n'
    ),
    "fonts-dev.Brewfile": (
        "# Developer Fonts\n"
        'brew "font-fira-code"\n'
        'cask "font-jetbrains-mono"\n'
        'cask "font-hack"\n'
    ),
    # Artwork is retired; the discovered slideshow identifier uses managed setup.
    "artwork.Brewfile": (
        'cask "bluefin-wallpapers"\n'
    ),
    "wallpaper-slideshow.Brewfile": (
        '# Legacy image payload: ChairLift must not execute it.\n'
        'flatpak "app.drey.Damask"\n'
        'cask "ublue-os/tap/bluefin-wallpapers-extra"\n'
    ),
    # Full Desktop Suite (defaults unchecked)
    "full-desktop.Brewfile": (
        'flatpak "org.gnome.Calculator"\n'
    ),
    # An administrator's own collection: humanized id, readable comment.
    "team-tools.Brewfile": (
        "# tools our team relies on every day\n"
        'brew "just"\n'
    ),
}


def calls_log(context, program):
    return os.path.join(context.scenario_dir, f"{program}-calls.log")


def _record(context, program):
    return f'printf \'%s\\n\' "$*" >> "{calls_log(context, program)}"\n'


def _brew(context, fail=(), installed_collections=()):
    """A brew serving the fixed catalog above.

    fail names read operations that exit 1 when brew cannot read its API.
    installed_collections names the Brewfiles `brew bundle check` reports
    satisfied; every other collection reports unmet dependencies (exit 1),
    as brew does for a collection nobody installed.
    """
    formulae = json.dumps({"formulae": FORMULAE, "casks": []})
    casks = json.dumps({"formulae": [], "casks": CASKS})
    info_fail = "echo 'Error: Failed to load the API' >&2; exit 1\n" if "info" in fail else ""
    satisfied = ""
    if installed_collections:
        patterns = "|".join(f"*/{name}" for name in installed_collections)
        satisfied = f'case "$*" in {patterns}) exit 0 ;; esac\n        '
    script = _record(context, "brew") + f"""
case "$1" in
--version) echo "Homebrew 4.6.0"; exit 0 ;;
--prefix) echo "{context.scenario_dir}/brew-prefix"; exit 0 ;;
--caskroom) echo "{context.scenario_dir}/brew-prefix/Caskroom/bluefin-wallpapers-extra"; exit 0 ;;
info)
    {info_fail}case "$*" in
    'info --json=v2 --formula just') echo '{{"formulae":[{{"name":"just","desc":"Save and run project commands"}}]}}' ;;
    'info --json=v2 --cask ublue-os/tap/bluefin-wallpapers-extra'|'info --json=v2 --cask bluefin-wallpapers-extra')
        echo '{{"formulae":[],"casks":[{{"token":"bluefin-wallpapers-extra","full_token":"ublue-os/tap/bluefin-wallpapers-extra","desc":"Extra Bluefin backgrounds","version":"fixture","installed":"fixture"}}]}}' ;;
    *--formula*) cat <<'EOF'
{formulae}
EOF
    ;;
    *--cask*) cat <<'EOF'
{casks}
EOF
    ;;
    esac
    exit 0 ;;
outdated) echo '{{"formulae":[],"casks":[]}}'; exit 0 ;;
tap-info) echo '[]'; exit 0 ;;
tap) echo 'ublue-os/tap'; exit 0 ;;
bundle)
    if [ "$2" = check ]; then
        {satisfied}echo "brew bundle can't satisfy your Brewfile's dependencies." >&2
        exit 1
    fi
    exit 0 ;;
esac
exit 0
"""
    fake_executable(context, "brew", script)


def _rows(rows):
    return "".join(f"{name}\t{app_id}\t{version}\n" for name, app_id, version in rows)


@stub("apps-brew")
def apps_brew(context):
    """Homebrew with two requested formulae (one pinned) and one cask."""
    _brew(context)


@stub("apps-brew-team-tools-installed")
def apps_brew_team_tools_installed(context):
    """Homebrew on which the Team tools collection is already installed."""
    _brew(context, installed_collections=("team-tools.Brewfile",))


@stub("apps-brew-developer-installed")
def apps_brew_developer_installed(context):
    """Both relocated collections are already satisfied on this host."""
    _brew(context, installed_collections=("cli.Brewfile", "fonts-dev.Brewfile"))



@stub("apps-brew-unreadable")
def apps_brew_unreadable(context):
    """Homebrew whose installed-package listing fails."""
    _brew(context, fail=("info",))


@stub("apps-flatpak")
def apps_flatpak(context):
    """Flatpak with one app installed for the user and one for everyone."""
    _flatpak(context, FLATPAK_SYSTEM)



def _flatpak(context, system_rows, manual_folders=(), active_source="wallhaven"):
    script = _record(context, "flatpak") + f"""
case "$1" in
--version) echo "Flatpak 1.16.1"; exit 0 ;;
search)
    case "$3" in
    org.gnome.Calculator) printf 'org.gnome.Calculator\tPerform arithmetic calculations\n' ;;
    io.github.tunix.valhalla) printf 'io.github.tunix.valhalla\tValhalla wallpaper slideshow\n' ;;
    *) echo "No matches found" ;;
    esac
    exit 0 ;;
list)
    case "$*" in
    *--columns=application)
        cat <<'EOF'
{''.join(app_id + chr(10) for _, app_id, _ in system_rows)}EOF
        ;;
    *--runtime*) exit 0 ;;
    *--user*) cat <<'EOF'
{_rows(FLATPAK_USER)}EOF
    ;;
    *--system*) cat <<'EOF'
{_rows(system_rows)}EOF
    ;;
    esac
    exit 0 ;;
run)
    case "$*" in
    *' get io.github.tunix.valhalla manual-folders') echo '{json.dumps(list(manual_folders))}'; exit 0 ;;
    *' get io.github.tunix.valhalla active-source') echo "'{active_source}'"; exit 0 ;;
    *' --command=test io.github.tunix.valhalla -r '*)
        for path in "$@"; do :; done
        test -r "$path"
        exit $? ;;
    esac
    echo "unexpected sandbox command: $*" >&2
    exit 64 ;;
esac
exit 0
"""
    fake_executable(context, "flatpak", script)


@stub("apps-collections")
def apps_collections(context):
    """Image-style collections, including a legacy Damask slideshow Brewfile."""
    directory = os.path.join(context.scenario_dir, "bundles")
    os.makedirs(directory, exist_ok=True)
    for filename, body in APP_COLLECTIONS.items():
        with open(os.path.join(directory, filename), "w", encoding="utf-8") as handle:
            handle.write(body)
