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
    "fonts-dev.Brewfile": (
        "# Developer Fonts\n"
        'brew "font-fira-code"\n'
        'cask "font-jetbrains-mono"\n'
        'cask "font-hack"\n'
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
tap-info) echo '[]'; exit 0 ;;
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


@stub("apps-brew-unreadable")
def apps_brew_unreadable(context):
    """Homebrew whose installed-package listing fails."""
    _brew(context, fail=("info",))


@stub("apps-flatpak")
def apps_flatpak(context):
    """Flatpak with one app installed for the user and one for everyone."""
    script = _record(context, "flatpak") + f"""
case "$1" in
--version) echo "Flatpak 1.16.1"; exit 0 ;;
list)
    case "$*" in
    *--runtime*) exit 0 ;;
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
