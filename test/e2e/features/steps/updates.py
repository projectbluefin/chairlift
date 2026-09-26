"""Steps for the Updates destination (updates.feature).

The destination is the status-first update shell (internal/views/
update_shell.go): a status page whose title, description, and one primary
action come from internal/views/updatepresent, and one row per update source.
The fake tools behind these steps live in fixtures/stubs_updates.py.
"""

import os

from behave import step, then

import chairlift_atspi as atspi


# behave executes every steps module itself, so importing steps/common.py
# would register its steps a second time; these two lookups are repeated.
def content(context):
    if context.app is None:
        raise AssertionError("ChairLift is not running in this scenario (@no-app?)")
    return atspi.page_root(context.app)


def text_present(root, wanted, exact=False):
    return any((value == wanted) if exact else (wanted in value) for value in atspi.all_text_under(root))


# Every title updatepresent.Snapshot can give the status page. Exactly one is
# on screen at a time, so asserting one also asserts no stale phase remains.
PHASE_TITLES = (
    "Checking for updates",
    "Updates available",
    "System is up to date",
    "Unable to check for updates",
    "Installing updates",
    "Some updates could not be installed",
    "Restart required",
)

# Every label updatepresent gives the primary action.
PRIMARY_LABELS = ("Check again", "Update all", "Try again", "Retry failed", "Restart now")


def _shown_phase_titles(context):
    return [title for title in PHASE_TITLES if text_present(content(context), title, exact=True)]


@step('the Updates status reads "{title}"')
def step_status_reads(context, title):
    assert title in PHASE_TITLES, f"{title!r} is not a status title updatepresent can show"
    ok = atspi.poll(lambda: _shown_phase_titles(context) == [title])
    assert ok, f"status titles on screen are {_shown_phase_titles(context)}, want only {title!r}"


@then("the Updates page offers no primary action")
def step_no_primary(context):
    def none_shown():
        return not atspi.find_all(
            content(context), lambda n: any(atspi.is_button(n, label) for label in PRIMARY_LABELS)
        )

    assert atspi.poll(none_shown), "a primary update action is still offered"


@then('the Updates page offers only the "{label}" action')
def step_only_primary(context, label):
    assert label in PRIMARY_LABELS, f"{label!r} is not a primary action updatepresent can offer"

    def offered():
        return sorted(
            {
                wanted
                for wanted in PRIMARY_LABELS
                for n in atspi.find_all(content(context), lambda n, w=wanted: atspi.is_button(n, w))
            }
        )

    ok = atspi.poll(lambda: offered() == [label])
    assert ok, f"primary actions offered are {offered()}, want only {label!r}"


# ---------------------------------------------------------------- sidebar badge


def _updates_badge(context):
    for row in atspi.sidebar_rows(context.app):
        labels = [
            atspi.name(n)
            for n in atspi.descendants(row, only_showing=True)
            if atspi.role(n) in atspi.LABEL_ROLES and atspi.name(n)
        ]
        if "Updates" in labels:
            return [label for label in labels if label != "Updates"]
    raise AssertionError("the sidebar has no Updates row")


@step('the Updates sidebar badge shows "{count}"')
def step_badge_shows(context, count):
    ok = atspi.poll(lambda: _updates_badge(context) == [count])
    assert ok, f"Updates sidebar badge is {_updates_badge(context)}, want [{count!r}]"


@step("the Updates sidebar row shows no badge")
def step_badge_hidden(context):
    ok = atspi.poll(lambda: _updates_badge(context) == [])
    assert ok, f"Updates sidebar row still shows {_updates_badge(context)}"


# ---------------------------------------------------------------- fake tools


def _state(context, name):
    return os.path.join(context.updates_state, name)


def _calls(context, program):
    try:
        with open(_state(context, f"{program}.calls"), encoding="utf-8") as handle:
            return [line.rstrip("\n") for line in handle]
    except FileNotFoundError:
        return []


@step("Flatpak now offers an update for Firefox")
def step_flatpak_offers_firefox(context):
    with open(_state(context, "flatpak-remote-ls-user"), "w", encoding="utf-8") as handle:
        handle.write("Firefox\torg.mozilla.firefox\t131.0\n")


@step("the Flatpak remote is reachable again")
def step_flatpak_reachable(context):
    for scope in ("user", "system"):
        try:
            os.remove(_state(context, f"flatpak-remote-ls-{scope}.fail"))
        except FileNotFoundError:
            pass


@then('the {program:w} tool was never asked to "{subcommand}"')
def step_tool_never_ran(context, program, subcommand):
    ran = [call for call in _calls(context, program) if call.split(" ", 1)[0] == subcommand]
    assert not ran, f"{program} received {ran}"


# ---------------------------------------------------------------- switches


@then('the switch in the "{row}" row can be toggled again')
def step_switch_sensitive(context, row):
    def check():
        target = atspi.row_containing(content(context), row, timeout=1)
        switches = [
            n for n in atspi.descendants(target, only_showing=True)
            if atspi.role(n) in ("switch", "toggle button", "check box")
        ]
        return bool(switches) and all(atspi.sensitive(n) for n in switches)

    assert atspi.poll(check), f"the switch in {row!r} stayed insensitive"


@then("the action journal holds exactly {count:d} entry")
@then("the action journal holds exactly {count:d} entries")
def step_journal_count(context, count):
    try:
        with open(context.journal_path, encoding="utf-8") as handle:
            entries = [line for line in handle if line.strip()]
    except FileNotFoundError:
        entries = []
    assert len(entries) == count, f"journal holds {len(entries)} entries, want {count}: {entries[:6]}"


# ---------------------------------------------------------------- expanders


@step('I expand the "{title}" row in the "{group}" group')
def step_expand_row(context, title, group):
    """Open an AdwExpanderRow from the keyboard.

    Expander headers publish no AT-SPI action and no coordinates, so the row
    is reached with Tab (GTK 4 does not implement AT-SPI GrabFocus) and
    toggled with space, as a keyboard user would. The group title disambiguates rows that share a
    title, such as the two "Available updates" expanders."""
    section = atspi.find(
        content(context),
        lambda n: atspi.role(n) == "grouping" and atspi.name(n) == group,
        f"a group titled {group!r}",
    )
    header = atspi.find(
        section,
        lambda n: atspi.role(n) in atspi.ROW_ROLES and atspi.name(n) == title and atspi.focusable(n),
        f"a focusable {title!r} row in {group!r}",
    )
    # The expander only expands once its inventory has loaded.
    loaded = atspi.poll(lambda: not text_present(header, "Checking…"))
    assert loaded, f"{title!r} in {group!r} is still checking"
    for _ in range(80):
        if atspi.focused(header):
            break
        atspi.press("Tab")
    assert atspi.focused(header), f"Tab never reached {title!r} in {group!r}"
    atspi.press("space")
