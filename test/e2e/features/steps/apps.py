"""Steps for the Homebrew-only Apps destination and shared keyboard row helpers."""

import os

from behave import step, then

import chairlift_atspi as atspi
from common import content, read_log, text_present

# Bound keyboard traversal so an inaccessible control fails clearly.
MAX_TABS = 80


# ---------------------------------------------------------------- lookups


def group(context, title, timeout=atspi.DEFAULT_TIMEOUT):
    """The showing AdwPreferencesGroup titled title."""
    return atspi.find(
        content(context),
        lambda n: atspi.role(n) == "grouping" and atspi.name(n) == title,
        f"a group titled {title!r}",
        timeout,
    )


# Shared with Features: its IDE catalog remains an expander.
def expander_header(context, title, group_title, timeout=atspi.DEFAULT_TIMEOUT):
    """The header row of the expander titled title inside group_title.

    AdwExpanderRow publishes an unnamed outer row wrapping a header row that
    carries the title; the header is the one that takes keyboard focus.
    """
    return atspi.find(
        group(context, group_title, timeout),
        lambda n: atspi.role(n) in atspi.ROW_ROLES and atspi.name(n) == title,
        f"an expander titled {title!r} in {group_title!r}",
        timeout,
    )


def expander_rows(header):
    """The child rows an expander currently shows, by title.

    The header's outer row holds the header and, once expanded, the revealed
    list of child rows.
    """
    outer = header.parent.parent.parent
    titles = []
    for node in atspi.descendants(outer, only_showing=True):
        if node is outer or atspi.role(node) not in atspi.ROW_ROLES:
            continue
        if atspi.name(node) and atspi.name(node) != atspi.name(header):
            titles.append(atspi.name(node))
    return titles

@step('I expand the "{title}" list under "{group_title}"')
def step_expand(context, title, group_title):
    """Focus the expander's header and press Return until its rows show.

    An expander whose inventory is still loading (a search in flight, a
    list still counting) has expansion disabled and ignores Return, so the
    key is pressed again for as long as no row has appeared. Revealing rows
    is synchronous, so a press that did expand is never undone by a retry.
    """
    for _ in range(10):
        header = expander_header(context, title, group_title)
        focus_by_tab(context, header, f"the {title!r} list under {group_title!r}")
        atspi.press("Return")
        if atspi.poll(lambda: expander_rows(expander_header(context, title, group_title, timeout=1)), timeout=1.5):
            return
    raise AssertionError(f"the {title!r} list under {group_title!r} showed no rows after expanding")




def focus_by_tab(context, target, what):
    """Press Tab until target has keyboard focus."""
    for _ in range(MAX_TABS):
        if atspi.focused(target):
            return
        atspi.press("Tab")
        if atspi.poll(lambda: atspi.focused(target), timeout=0.3):
            return
    focused = [
        f"{atspi.role(n)}:{atspi.label_text(n)!r}"
        for n in atspi.descendants(context.app, only_showing=True)
        if atspi.focused(n)
    ]
    raise AssertionError(f"{MAX_TABS} Tab presses never focused {what}; focus is on {focused}")


def calls(context, program):
    try:
        with open(os.path.join(context.scenario_dir, f"{program}-calls.log"), encoding="utf-8") as handle:
            return [line.rstrip("\n") for line in handle if line.strip()]
    except FileNotFoundError:
        return []


def toast_shown(context, wanted):
    """A toast is a label in the window's toast overlay, outside the page."""
    window = atspi.main_window(context.app)
    return text_present(window, wanted)


# ---------------------------------------------------------------- groups


@then('the Apps page shows the "{title}" group')
def step_group_shown(context, title):
    group(context, title)


@then('the Apps page does not show the "{title}" group')
def step_group_absent(context, title):
    root = content(context)
    gone = atspi.poll(
        lambda: not atspi.find_all(root, lambda n: atspi.role(n) == "grouping" and atspi.name(n) == title)
    )
    assert gone, f"the Apps page still shows a group titled {title!r}"


@then("the Apps groups are ordered exactly")
def step_group_order(context):
    want = [row["title"] for row in context.table]

    def titles():
        root = group(context, want[0], timeout=1).parent
        return [atspi.name(node) for node in atspi.children(root)
                if atspi.role(node) == "grouping" and atspi.name(node)]

    assert atspi.poll(lambda: titles() == want), f"Apps groups are {titles()}, want {want}"


@then('the sidebar omits "{title}"')
def step_sidebar_omits(context, title):
    titles = [atspi.label_text(row) for row in atspi.sidebar_rows(context.app)]
    assert title not in titles, f"sidebar still lists {title!r}: {titles}"

# ---------------------------------------------------------------- visible lists


def group_rows(context, title):
    root = group(context, title, timeout=1)
    return [atspi.name(node) for node in atspi.descendants(root, only_showing=True)
            if atspi.role(node) in atspi.ROW_ROLES and atspi.name(node)]


@then('the "{title}" apps group says "{text}"')
def step_group_says(context, title, text):
    assert atspi.poll(lambda: text_present(group(context, title, timeout=1), text, exact=True)), \
        f"the {title!r} group never said {text!r}"


@then('the "{title}" apps group shows exactly')
def step_group_rows(context, title):
    want = [row["title"] for row in context.table]
    assert atspi.poll(lambda: group_rows(context, title) == want), \
        f"the {title!r} group shows {group_rows(context, title)}, want {want}"


# ---------------------------------------------------------------- keyboard rows


@step('I open the "{title}" row with the keyboard')
def step_open_row(context, title):
    row = atspi.row_containing(content(context), title)
    focus_by_tab(context, row, f"the {title!r} row")
    atspi.press("Return")


# ---------------------------------------------------------------- collection lists

# An app collection's expander is the same widget across rows, so the row
# containing its title is the same node every other helper already finds;
# its child rows are the packages the Brewfile would install. The expander
# is the only one of its kind inside "App Collections" — Features uses
# another, but a different title — so the row_containing lookup is exact.
@then('the "{title}" list shows exactly')
def step_expander_rows_exact(context, title):
    want = [row["title"] for row in context.table]
    header = expander_header(context, title, "App Collections", timeout=1)
    assert atspi.poll(lambda: expander_rows(header) == want, timeout=2.0), \
        f"the {title!r} list shows {expander_rows(header)}, want {want}"


# ---------------------------------------------------------------- entry checkboxes

# A collection entry is a row with the Brewfile token as its title and a
# checkbox on its right edge. The row is inside the collection's expander,
# so it only exists once the collection is expanded.
def _entry_row(context, token):
    return atspi.row_containing(content(context), token)


def _entry_checkbox(context, token):
    row = _entry_row(context, token)
    return atspi.find(
        row,
        lambda n: atspi.role(n) in atspi.TOGGLE_ROLES,
        "the entry checkbox for " + token,
    )


@then('the "{token}" entry checkbox is {state:w}')
def step_entry_checkbox_state(context, token, state):
    if state not in ("checked", "unchecked"):
        raise NotImplementedError(f"unknown checkbox state {state!r}")
    want = state == "checked"
    cb = _entry_checkbox(context, token)
    assert atspi.poll(lambda: bool(atspi.checked(cb)) == want), \
        f"the entry checkbox for {token!r} is not {state}"


@then('the "{token}" entry checkbox is insensitive')
def step_entry_checkbox_insensitive(context, token):
    cb = _entry_checkbox(context, token)
    assert atspi.poll(lambda: not atspi.sensitive(cb)), \
        f"the entry checkbox for {token!r} is still sensitive"


@step('I check the "{token}" entry')
def step_check_entry(context, token):
    """Toggle the entry's checkbox on with the keyboard."""
    cb = _entry_checkbox(context, token)
    if atspi.checked(cb):
        return
    focus_by_tab(context, cb, f"the entry checkbox for {token!r}")
    atspi.press("space")
    assert atspi.poll(lambda: bool(atspi.checked(_entry_checkbox(context, token)))), \
        f"the entry checkbox for {token!r} did not become checked"


@step('I uncheck the "{token}" entry')
def step_uncheck_entry(context, token):
    cb = _entry_checkbox(context, token)
    if not atspi.checked(cb):
        return
    focus_by_tab(context, cb, f"the entry checkbox for {token!r}")
    atspi.press("space")
    assert atspi.poll(lambda: not atspi.checked(_entry_checkbox(context, token))), \
        f"the entry checkbox for {token!r} did not become unchecked"


# ---------------------------------------------------------------- collection actions

# The collection's "Select All" button carries the collection's name, so
# six of them on one page stay distinguishable.
def _select_all_button(context, title):
    return atspi.find_button(content(context), f"Select All in {title}")


@step('I click the "Select All in {title}" button')
def step_click_select_all(context, title):
    atspi.activate(_select_all_button(context, title))


@then('the "Select All in {title}" button is {state:w}')
def step_select_all_state(context, title, state):
    if state not in ("sensitive", "insensitive"):
        raise NotImplementedError(f"unknown button state {state!r}")
    want = state == "sensitive"
    assert atspi.poll(lambda: atspi.sensitive(_select_all_button(context, title)) == want), \
        f'the "Select All in {title}" button never became {state}'


@then('the "{row}" row does not say "{text}"')
def step_row_does_not_say(context, row, text):
    target = atspi.row_containing(content(context), row)
    assert not text_present(target, text), f"row {row!r} says {text!r}"

# ---------------------------------------------------------------- toasts


@then('a toast on the Apps page says "{text}"')
def step_toast(context, text):
    assert atspi.poll(lambda: toast_shown(context, text)), f"no toast ever said {text!r}"


# ---------------------------------------------------------------- stub evidence


@then('Homebrew was never asked to "{command}"')
def step_brew_never(context, command):
    # Leading words, so "bundle install" is told apart from the read-only
    # "bundle check" each collection row runs on load.
    words = command.split()
    ran = [line for line in calls(context, "brew") if line.split()[: len(words)] == words]
    assert not ran, f"brew ran {command!r} for real: {ran}"


@then('Homebrew was asked to "{command}"')
def step_brew_asked(context, command):
    words = command.split()

    def ran():
        return [line for line in calls(context, "brew") if line.split()[: len(words)] == words]

    assert atspi.poll(ran), f"brew was never asked to {command!r}"


@then('Flatpak was never asked to "{command}"')
def step_flatpak_never(context, command):
    ran = [line for line in calls(context, "flatpak") if line.split(" ", 1)[0] == command]
    assert not ran, f"flatpak ran {command!r} for real: {ran}"


@then('Flatpak was asked to "{command}"')
def step_flatpak_asked(context, command):
    def ran():
        return [line for line in calls(context, "flatpak") if line.split(" ", 1)[0] == command]

    assert atspi.poll(ran), f"flatpak was never asked to {command!r}"



@then('the application log previews "{command}" exactly once')
def step_log_previews_once(context, command):
    marker = f"[DRY-RUN] Would execute: {command}"

    def lines():
        return [line for line in read_log(context).splitlines() if line.endswith(marker)]

    assert atspi.poll(lambda: lines()), f"chairlift.log never previewed {command!r}"
    got = lines()
    assert len(got) == 1, f"chairlift.log previewed {command!r} {len(got)} times: {got}"


@then('the application log previews no "{program:w} {command:w}"')
def step_log_previews_none(context, program, command):
    marker = f"[DRY-RUN] Would execute: {program} {command}"
    got = [line for line in read_log(context).splitlines() if marker in line]
    assert not got, f"chairlift.log previewed {program} {command}: {got}"


@then('the home directory has no "{name}"')
def step_home_lacks(context, name):
    path = os.path.join(context.home, name)
    assert not os.path.exists(path), f"{path} was written under --dry-run"
