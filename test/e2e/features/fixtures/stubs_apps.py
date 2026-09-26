"""Prelaunch stubs for the Apps destination (applications_page).

The Apps page reads its inventories from `brew` and `flatpak` and opens the
software catalog with `gtk-launch`. Each stub below replaces one of those
tools with a small shell script first on the scenario's PATH, so every row
the page draws comes from a fixed catalog instead of whatever the runner
happens to have installed. A PATH `brew` wins over the Linuxbrew fallback
(internal/homebrew.ExecutablePath), which the Dakota container and GitHub's
runners both ship.

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
    # A formula brew knows about but has no installed keg for is skipped.
    {"name": "not-installed", "installed": [], "pinned": False},
]
CASKS = [
    {"token": "visual-studio-code", "version": "1.104.0", "installed": "1.104.0"},
]

# What `brew search --formula|--cask <query>` matches, by substring.
SEARCH_FORMULAE = ("chezmoi", "lazydocker", "lazygit")
SEARCH_CASKS = ("lazyterm",)

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


def _brew(context, fail=()):
    """A brew serving the fixed catalog above.

    fail names the read operations ("info", "search") that exit 1 the way
    brew does when its API or a tap cannot be read.
    """
    formulae = json.dumps({"formulae": FORMULAE, "casks": []})
    casks = json.dumps({"formulae": [], "casks": CASKS})
    info_fail = "echo 'Error: Failed to load the API' >&2; exit 1\n" if "info" in fail else ""
    search_fail = "echo 'Error: Could not reach the Homebrew API' >&2; exit 1\n" if "search" in fail else ""
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
search)
    {search_fail}case "$2" in
    --formula) names="{' '.join(SEARCH_FORMULAE)}" ;;
    --cask) names="{' '.join(SEARCH_CASKS)}" ;;
    *) names="" ;;
    esac
    found=0
    for name in $names; do
        case "$name" in *"$3"*) echo "$name"; found=1 ;; esac
    done
    if [ "$found" = 0 ]; then
        echo "Error: No formulae or casks found for \\"$3\\"." >&2
        exit 1
    fi
    exit 0 ;;
outdated) echo '{{"formulae":[],"casks":[]}}'; exit 0 ;;
tap-info) echo '[]'; exit 0 ;;
esac
exit 0
"""
    fake_executable(context, "brew", script)


def _rows(rows):
    return "".join(f"{name}\t{app_id}\t{version}\n" for name, app_id, version in rows)


@stub("apps-brew")
def apps_brew(context):
    """Homebrew with two formulae (one pinned), one cask, and a search catalog."""
    _brew(context)


@stub("apps-brew-unreadable")
def apps_brew_unreadable(context):
    """Homebrew whose installed-package listing fails."""
    _brew(context, fail=("info",))


@stub("apps-brew-search-broken")
def apps_brew_search_broken(context):
    """Homebrew whose search cannot reach its API."""
    _brew(context, fail=("search",))


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


@stub("apps-gtk-launch")
def apps_gtk_launch(context):
    """gtk-launch that records which desktop ID it was asked to open."""
    fake_executable(context, "gtk-launch", _record(context, "gtk-launch") + "exit 0\n")


@stub("apps-gtk-launch-missing")
def apps_gtk_launch_missing(context):
    """gtk-launch for a desktop ID that is not installed: it exits 1."""
    fake_executable(
        context,
        "gtk-launch",
        _record(context, "gtk-launch") + 'echo "gtk-launch: no such application $1" >&2\nexit 1\n',
    )


@stub("apps-collections")
def apps_collections(context):
    """Two app collections in <scenario>/bundles, the apps-bundles fixture's path."""
    directory = os.path.join(context.scenario_dir, "bundles")
    os.makedirs(directory, exist_ok=True)
    for filename, body in APP_COLLECTIONS.items():
        with open(os.path.join(directory, filename), "w", encoding="utf-8") as handle:
            handle.write(body)
