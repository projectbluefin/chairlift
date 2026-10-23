"""Prelaunch stubs for the Apps destination (applications_page).

The Apps page reads the installed Homebrew inventory, the installed Flatpak
inventory, and configured Brewfiles. PATH stubs serve a fixed inventory and
record calls.

Every stub records its argv, one invocation per line, in
<scenario>/<program>-calls.log. Under --dry-run ChairLift must never execute
a state-changing command, so the log is also the evidence that a mutation was
only previewed.
"""

import json
import os

from stubs import fake_executable, stub

# The installed inventory `brew info --installed --json=v2` reports. Only
# the fields internal/homebrew.parsePackagesJSON reads are populated, plus
# the qualified name and the tap, which are what tell a tap package from a
# homebrew/core one.
FORMULAE = [
    {"name": "jq", "full_name": "jq", "tap": "homebrew/core", "installed": [{"version": "1.7.1", "installed_on_request": True}], "pinned": False},
    {"name": "ripgrep", "full_name": "ripgrep", "tap": "homebrew/core", "installed": [{"version": "14.1.1", "installed_on_request": True}], "pinned": True},
    # Installed dependency is real inventory, but not a requested tool.
    {"name": "libunistring", "full_name": "libunistring", "tap": "homebrew/core", "installed": [{"version": "1.3", "installed_on_request": False}], "pinned": False},
    # A formula brew knows about but has no installed keg for is skipped.
    {"name": "not-installed", "full_name": "not-installed", "tap": "homebrew/core", "installed": [], "pinned": False},
]
CASKS = [
    {"token": "visual-studio-code", "full_token": "visual-studio-code", "tap": "homebrew/core", "version": "1.104.0", "installed": "1.104.0"},
]

# `flatpak list --<scope> --app --columns=name,application,version,branch,origin` rows.
FLATPAK_USER = (("Firefox", "org.mozilla.firefox", "140.0", "stable", "flathub"),)
FLATPAK_SYSTEM = (("Text Editor", "org.gnome.TextEditor", "48.0", "stable", "flathub"),)

# The Flatpak applications the Apps page finds already installed, and the
# remote each came from. This is the query `brew bundle` itself uses.
FLATPAK_INSTALLED = (("app.drey.Damask", "flathub"),)

APP_COLLECTIONS = {
    # A collection Bluefin ships: its title and summary come from
    # internal/views/bundleview's curated table, never from the file.
    "fonts-dev.Brewfile": (
        "# Developer Fonts\n"
        'brew "font-fira-code"\n'
        'cask "font-jetbrains-mono"\n'
        'cask "font-hack"\n'
    ),
    # Extra Wallpapers super-bundle: artwork plus its retired
    # wallpaper-slideshow sibling, merged into one row by bundleview.Members.
    "artwork.Brewfile": (
        'cask "bluefin-wallpapers"\n'
    ),
    "wallpaper-slideshow.Brewfile": (
        'brew "wallpaper-slideshow"\n'
    ),
    # Full Desktop Suite (defaults unchecked)
    "full-desktop.Brewfile": (
        'flatpak "org.gnome.Calculator"\n'
        'flatpak "app.drey.Damask"\n'
        'flatpak "org.gnome.Notes"\n'
    ),
    # An administrator's own collection: humanized id, readable comment.
    "team-tools.Brewfile": (
        "# tools our team relies on every day\n"
        'brew "just"\n'
    ),
    # An administrator's collection that names a tap. The tap is not
    # trusted, so installing one of its entries has to trust the package
    # on its own first.
    "tap-tools.Brewfile": (
        'tap "anomalyco/tap"\n'
        'brew "anomalyco/tap/opencode-v2"\n'
        'brew "gh"\n'
    ),
}


def calls_log(context, program):
    return os.path.join(context.scenario_dir, f"{program}-calls.log")


def _record(context, program):
    return f'printf \'%s\\n\' "$*" >> "{calls_log(context, program)}"\n'


def _brew(
    context,
    fail=(),
    installed_collections=(),
    extra_formulae=(),
    extra_casks=(),
    trusted_taps=(),
    tap_installed=(),
):
    """A brew serving the fixed catalog above.

    fail names read operations that exit 1 when brew cannot read its API.
    installed_collections names the Brewfiles `brew bundle check` reports
    satisfied; every other collection reports unmet dependencies (exit 1),
    as brew does for a collection nobody installed.
    extra_formulae and extra_casks add installed packages, so a scenario can
    show a collection entry that is already on the system.
    trusted_taps names the taps the trust store holds; every other tap is
    untrusted, which is what an entry from a third-party tap looks like
    before a person trusts it.
    tap_installed names the taps that are already cloned.
    """
    formulae = json.dumps({"formulae": FORMULAE + list(extra_formulae), "casks": []})
    casks = json.dumps({"formulae": [], "casks": CASKS + list(extra_casks)})
    trust = json.dumps(
        {
            "taps": list(trusted_taps),
            "formulae": [],
            "casks": [],
            "commands": [],
        }
    )
    info_fail = "echo 'Error: Failed to load the API' >&2; exit 1\n" if "info" in fail else ""
    satisfied = ""
    if installed_collections:
        patterns = "|".join(f"*/{name}" for name in installed_collections)
        satisfied = f'case "$*" in {patterns}) exit 0 ;; esac\n        '
    installed = " ".join(tap_installed)
    script = _record(context, "brew") + f"""
case "$1" in
--version) echo "Homebrew 4.6.0"; exit 0 ;;
--prefix) echo "{context.scenario_dir}/brew-prefix"; exit 0 ;;
info)
    {info_fail}case "$*" in
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
tap-info)
    case "$*" in
    *--installed*) echo '[]' ;;
    *) case " {installed} " in
       *" $3 "*) printf '[{{"name":"%s","installed":true,"trusted":true}}]\n' "$3" ;;
       *) printf '[{{"name":"%s","installed":false,"trusted":false}}]\n' "$3" ;;
       esac ;;
    esac
    exit 0 ;;
trust)
    if [ "$2" = "--json=v1" ]; then
        printf '%s\n' '{trust}'
        exit 0
    fi
    exit 0 ;;
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
    return "".join(f"{name}\t{app_id}\t{version}\t{branch}\t{origin}\n" for name, app_id, version, branch, origin in rows)


def _origin_rows(rows):
    return "".join(f"{app_id}\t{origin}\n" for app_id, origin in rows)


@stub("apps-brew")
def apps_brew(context):
    """Homebrew with two requested formulae (one pinned) and one cask."""
    _brew(context)


@stub("apps-brew-team-tools-installed")
def apps_brew_team_tools_installed(context):
    """Homebrew on which the Team tools collection is already installed."""
    _brew(
        context,
        extra_formulae=[
            {"name": "just", "full_name": "just", "tap": "homebrew/core", "installed": [{"version": "1.36.0", "installed_on_request": True}], "pinned": False},
        ],
    )


@stub("apps-brew-wallpapers-installed")
def apps_brew_wallpapers_installed(context):
    """Homebrew on which both Extra Wallpapers member Brewfiles are installed."""
    _brew(
        context,
        extra_formulae=[
            {"name": "wallpaper-slideshow", "full_name": "wallpaper-slideshow", "tap": "homebrew/core", "installed": [{"version": "1.0.0", "installed_on_request": True}], "pinned": False},
        ],
        extra_casks=[
            {"token": "bluefin-wallpapers", "full_token": "bluefin-wallpapers", "tap": "homebrew/core", "installed": "1.0.0"},
        ],
    )


@stub("apps-brew-wallpapers-partly-installed")
def apps_brew_wallpapers_partly_installed(context):
    """Homebrew holding artwork but not its wallpaper-slideshow sibling.

    Team tools is installed too, so its later "(Installed)" subtitle proves
    the Extra Wallpapers check already ran.
    """
    _brew(
        context,
        extra_casks=[
            {"token": "bluefin-wallpapers", "full_token": "bluefin-wallpapers", "tap": "homebrew/core", "installed": "1.0.0"},
        ],
        extra_formulae=[
            {"name": "just", "full_name": "just", "tap": "homebrew/core", "installed": [{"version": "1.36.0", "installed_on_request": True}], "pinned": False},
        ],
    )


@stub("apps-brew-unreadable")
def apps_brew_unreadable(context):
    """Homebrew whose installed-package listing fails."""
    _brew(context, fail=("info",))


@stub("apps-brew-tap-trusted")
def apps_brew_tap_trusted(context):
    """Homebrew on which the administrator's tap is trusted wholesale."""
    _brew(context, trusted_taps=("anomalyco/tap",), tap_installed=("anomalyco/tap",))


@stub("apps-flatpak")
def apps_flatpak(context):
    """Flatpak with one app installed for the user and one for everyone."""
    script = _record(context, "flatpak") + f"""
case "$1" in
--version) echo "Flatpak 1.16.1"; exit 0 ;;
list)
    case "$*" in
    *--runtime*) exit 0 ;;
    *--columns=application,origin*) cat <<'EOF'
{_origin_rows(FLATPAK_INSTALLED)}EOF
    ;;
    *--user*) cat <<'EOF'
{_rows(FLATPAK_USER)}EOF
    ;;
    *--system*) cat <<'EOF'
{_rows(FLATPAK_SYSTEM)}EOF
    ;;
    esac
    exit 0 ;;
esac
exit 0
"""
    fake_executable(context, "flatpak", script)


@stub("apps-collections")
def apps_collections(context):
    """Two app collections in <scenario>/bundles, the apps-bundles fixture's path."""
    directory = os.path.join(context.scenario_dir, "bundles")
    os.makedirs(directory, exist_ok=True)
    for filename, body in APP_COLLECTIONS.items():
        with open(os.path.join(directory, filename), "w", encoding="utf-8") as handle:
            handle.write(body)
