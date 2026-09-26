"""Steps for the Apps destination (applications_page).

The page is built from AdwPreferencesGroups holding AdwExpanderRows. Two of
those expanders share the title "Applications" (Flatpak apps and Homebrew
casks), so every lookup here is scoped to its group's title first.

GTK 4 does not implement AT-SPI Component.GrabFocus (it returns false and
focus stays put) and publishes list rows without an action, so an expander
is opened the way a keyboard user opens it: Tab until its header row has
focus, then Return. Text is entered through the EditableText interface and
committed with Return once Tab has reached the entry.
"""

import os

from behave import step, then

import chairlift_atspi as atspi
from common import content, read_log, text_present

# Tab presses allowed while walking focus to a control. The Apps page has
# fewer than 40 focus stops even with every expander open.
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


def search_entry(context):
    return atspi.find(
        group(context, "Find more apps and tools"),
        lambda n: atspi.role(n) in atspi.TEXT_ROLES,
        "the Homebrew search entry",
    )


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


# ---------------------------------------------------------------- expanders


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


@then('the "{title}" list under "{group_title}" says "{text}"')
def step_expander_says(context, title, group_title, text):
    def check():
        header = expander_header(context, title, group_title, timeout=1)
        return text_present(header, text, exact=True)

    assert atspi.poll(check), f"the {title!r} list under {group_title!r} never said {text!r}"


@then('the "{title}" list under "{group_title}" shows exactly')
def step_expander_rows(context, title, group_title):
    want = [row["title"] for row in context.table]

    def check():
        return expander_rows(expander_header(context, title, group_title, timeout=1)) == want

    if not atspi.poll(check):
        got = expander_rows(expander_header(context, title, group_title))
        raise AssertionError(f"the {title!r} list under {group_title!r} shows {got}, want {want}")


# ---------------------------------------------------------------- search


@step('I search Homebrew for "{query}"')
def step_search(context, query):
    """Enter the query and commit it with Return, as a keyboard user does.

    The entry's AT-SPI "activate" action does not emit GtkSearchEntry's
    ::activate, which is what starts a search, so the entry is reached with
    Tab and the query committed with Return.
    """
    entry = search_entry(context)
    editable = entry.get_editable_text_iface()
    assert editable is not None, "the Homebrew search entry is not editable"
    assert editable.set_text_contents(query), "the Homebrew search entry refused the query"
    assert atspi.poll(lambda: atspi.text(search_entry(context)) == query), "the query never reached the entry"
    focus_by_tab(context, search_entry(context), "the Homebrew search entry")
    atspi.press("Return")


@then("the Homebrew search field has an accessible name")
def step_search_named(context):
    entry = search_entry(context)
    assert atspi.name(entry) or atspi.description(entry), (
        "the Homebrew search entry has neither an accessible name nor a description"
    )


# ---------------------------------------------------------------- keyboard rows


@step('I open the "{title}" row with the keyboard')
def step_open_row(context, title):
    row = atspi.row_containing(content(context), title)
    focus_by_tab(context, row, f"the {title!r} row")
    atspi.press("Return")


# ---------------------------------------------------------------- icon buttons


@step('I click the remove button in the "{row}" row')
def step_click_remove(context, row):
    target = atspi.row_containing(content(context), row)
    buttons = atspi.find_all(target, lambda n: atspi.role(n) in atspi.BUTTON_ROLES and atspi.actions(n))
    assert len(buttons) == 1, f"row {row!r} has {len(buttons)} operable buttons, want 1"
    context.apps_remove_button = buttons[0]
    atspi.activate(buttons[0])


@then('the remove button in the "{row}" row has an accessible name')
def step_remove_named(context, row):
    target = atspi.row_containing(content(context), row)
    buttons = atspi.find_all(target, lambda n: atspi.role(n) in atspi.BUTTON_ROLES and atspi.actions(n))
    assert buttons, f"row {row!r} has no operable button"
    nameless = [atspi.description(b) for b in buttons if not atspi.name(b)]
    assert not nameless, f"row {row!r} has icon buttons with no accessible name (descriptions: {nameless})"


# ---------------------------------------------------------------- toasts


@then('a toast on the Apps page says "{text}"')
def step_toast(context, text):
    assert atspi.poll(lambda: toast_shown(context, text)), f"no toast ever said {text!r}"


# ---------------------------------------------------------------- stub evidence


@then('Homebrew was never asked to "{command}"')
def step_brew_never(context, command):
    ran = [line for line in calls(context, "brew") if line.split(" ", 1)[0] == command]
    assert not ran, f"brew ran {command!r} for real: {ran}"


@then('Homebrew was asked to "{argv}"')
def step_brew_asked(context, argv):
    assert atspi.poll(lambda: argv in calls(context, "brew")), (
        f"brew was never run as {argv!r}; calls were {calls(context, 'brew')}"
    )


@then('Flatpak was never asked to "{command}"')
def step_flatpak_never(context, command):
    ran = [line for line in calls(context, "flatpak") if line.split(" ", 1)[0] == command]
    assert not ran, f"flatpak ran {command!r} for real: {ran}"


@then('gtk-launch was asked to open "{desktop_id}"')
def step_gtk_launch(context, desktop_id):
    assert atspi.poll(lambda: desktop_id in calls(context, "gtk-launch")), (
        f"gtk-launch was never asked for {desktop_id!r}; calls were {calls(context, 'gtk-launch')}"
    )


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
