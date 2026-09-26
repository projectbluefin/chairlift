"""Shared steps for driving ChairLift through the accessibility tree.

Destination features should reuse these before adding their own. A step that
only one destination needs belongs in a steps/<destination>.py module beside
this one; a step two destinations need belongs here.

Conventions:

* Page content is searched outside the navigation sidebar, so a label the
  sidebar also shows cannot satisfy a content assertion.
* Every lookup polls until its deadline; no step sleeps a fixed time.
* Names in quotes are accessible names exactly as an assistive technology
  announces them, which for GTK is normally the visible label.
"""

import json

from behave import given, step, then, when

import chairlift_atspi as atspi


# ---------------------------------------------------------------- lookups


def read_log(context):
    try:
        with open(context.log_path, "r", encoding="utf-8", errors="replace") as handle:
            return handle.read()
    except (AttributeError, OSError):
        return ""


def app(context):
    if context.app is None:
        raise AssertionError("ChairLift is not running in this scenario (@no-app?)")
    return context.app


def content(context):
    return atspi.page_root(app(context))


def nav_item(context, title):
    for index, item in enumerate(context.navigation):
        if item["title"] == title:
            return index, item
    raise AssertionError(
        f"{title!r} is not a navigation page; pages are {[i['title'] for i in context.navigation]}"
    )


def current_dialog(context, timeout=atspi.DEFAULT_TIMEOUT):
    """The most recently presented dialog.

    Libadwaita presents AdwDialog and AdwAlertDialog inside the main window
    (a "dialog"/"alert" node) when it fits and as a separate top-level frame
    when it does not, as AdwAboutDialog does on the suite's display. Both
    places are searched; an in-window dialog wins.
    """
    def lookup():
        found = None
        for node in atspi.descendants(app(context), only_showing=True):
            if atspi.role(node) in ("dialog", "alert"):
                found = node
        if found is not None:
            return found
        extra = [
            w for w in atspi.children(app(context))
            if atspi.showing(w) and atspi.sidebar_path(w) is None
        ]
        return extra[-1] if extra else None

    dialog = atspi.poll(lookup, timeout=timeout)
    if dialog is None:
        raise AssertionError(f"no dialog is showing after {timeout}s")
    return dialog


def selected_rows(context):
    return [
        (index, atspi.label_text(row))
        for index, row in enumerate(atspi.sidebar_rows(app(context)))
        if atspi.selected(row)
    ]


def text_present(root, wanted, exact=False):
    for value in atspi.all_text_under(root):
        if (value == wanted) if exact else (wanted in value):
            return True
    return False


def wait_for_text(context, root, wanted, present=True, timeout=atspi.DEFAULT_TIMEOUT, exact=False):
    ok = atspi.poll(lambda: text_present(root, wanted, exact) == present, timeout=timeout)
    if not ok:
        verb = "never appeared" if present else "never went away"
        raise AssertionError(f"{wanted!r} {verb} within {timeout}s")


# ---------------------------------------------------------------- lifecycle


@given("ChairLift is running")
def step_running(context):
    app(context)


@then('the application log contains "{text}"')
def step_log_contains(context, text):
    ok = atspi.poll(lambda: text in read_log(context), timeout=atspi.DEFAULT_TIMEOUT)
    assert ok, f"chairlift.log never contained {text!r}"


@then('the application log does not contain "{text}"')
def step_log_lacks(context, text):
    assert text not in read_log(context), f"chairlift.log unexpectedly contains {text!r}"


# ---------------------------------------------------------------- navigation


@step('I open the "{title}" page')
def step_open_page(context, title):
    """Navigate with the Alt+number accelerator navigation advertises."""
    index, _ = nav_item(context, title)
    atspi.press(f"<Alt>{index + 1}")
    step_page_shown(context, title)


@step('I select "{title}" in the sidebar')
def step_select_sidebar(context, title):
    """Move sidebar focus to the row with the arrow keys and press Return.

    This is how a keyboard user activates a row. GTK publishes list rows
    without an action and without screen coordinates on X11, so neither an
    AT-SPI action nor a pointer click is available.
    """
    rows = atspi.sidebar_rows(app(context))
    titles = [atspi.label_text(r) for r in rows]
    if title not in titles:
        raise AssertionError(f"no sidebar row named {title!r}; rows are {titles}")
    target = titles.index(title)
    focused = [i for i, r in enumerate(rows) if atspi.focused(r)]
    if not focused:
        selected = [i for i, r in enumerate(rows) if atspi.selected(r)]
        if not selected or not atspi.safe(lambda: rows[selected[0]].grabFocus() or True, False):
            raise AssertionError("no sidebar row has keyboard focus to start from")
        focused = selected
    delta = target - focused[0]
    for _ in range(abs(delta)):
        atspi.press("Down" if delta > 0 else "Up")
    ok = atspi.poll(lambda: atspi.focused(atspi.sidebar_rows(app(context))[target]), timeout=5)
    assert ok, f"keyboard focus never reached the {title!r} row"
    atspi.press("Return")
    step_page_shown(context, title)


@then('the "{title}" page is shown')
def step_page_shown(context, title):
    def settled():
        rows = selected_rows(context)
        if len(rows) != 1 or rows[0][1] != title:
            return None
        return text_present(content(context), title, exact=True)

    if not atspi.poll(settled):
        raise AssertionError(
            f"expected {title!r} selected and titled; selected rows are {selected_rows(context)}"
        )


@then("the sidebar lists every navigation page in order")
def step_sidebar_inventory(context):
    want = [item["title"] for item in context.navigation]
    assert want, "the navigation inventory passed to the suite is empty"
    got = atspi.poll(
        lambda: [atspi.label_text(r) for r in atspi.sidebar_rows(app(context))] == want
        and [atspi.label_text(r) for r in atspi.sidebar_rows(app(context))]
    )
    rows = [atspi.label_text(r) for r in atspi.sidebar_rows(app(context))]
    assert got, f"sidebar rows {rows} != navigation inventory {want}"


@then("every navigation page is reachable by its Alt+number shortcut")
def step_every_accelerator(context):
    for item in context.navigation:
        step_open_page(context, item["title"])


@then("every navigation page is reachable from the sidebar")
def step_every_row(context):
    for item in reversed(context.navigation):
        step_select_sidebar(context, item["title"])


# ---------------------------------------------------------------- keyboard


@step('I press "{combo}"')
def step_press(context, combo):
    atspi.press(combo)


@step('I type "{value}"')
def step_type(context, value):
    atspi.type_text(value)


# ---------------------------------------------------------------- buttons


@step('I click the "{label}" button')
def step_click_button(context, label):
    atspi.activate(atspi.find_button(content(context), label))


@step('I click the "{label}" button in the "{row}" row')
def step_click_row_button(context, label, row):
    target = atspi.row_containing(content(context), row)
    atspi.activate(atspi.find_button(target, label))


@then('the "{label}" button is shown')
def step_button_shown(context, label):
    atspi.find_button(content(context), label)


@then('the "{label}" button is not shown')
def step_button_absent(context, label):
    gone = atspi.poll(lambda: not atspi.find_all(content(context), lambda n: atspi.is_button(n, label)))
    assert gone, f"a button named {label!r} is still showing"


@then('the "{label}" button is {state:w}')
def step_button_state(context, label, state):
    if state not in ("sensitive", "insensitive"):
        raise NotImplementedError(f"unknown button state {state!r}")
    want = state == "sensitive"
    button = atspi.find_button(content(context), label)
    ok = atspi.poll(lambda: atspi.sensitive(atspi.find_button(content(context), label)) == want)
    assert ok, f"button {label!r} is {'insensitive' if atspi.sensitive(button) is False else 'sensitive'}"


@then('the "{label}" button in the "{row}" row is {state:w}')
def step_row_button_state(context, label, row, state):
    want = state == "sensitive"

    def check():
        target = atspi.row_containing(content(context), row, timeout=1)
        return atspi.sensitive(atspi.find_button(target, label, timeout=1)) == want

    assert atspi.poll(check), f"button {label!r} in row {row!r} never became {state}"


@then("every visible action control has an accessible name and an action")
def step_controls_accessible(context):
    controls = atspi.find_all(
        content(context),
        lambda n: atspi.role(n) in atspi.BUTTON_ROLES and atspi.focusable(n) and atspi.sensitive(n),
    )
    nameless = [atspi.role(c) for c in controls if not atspi.label_text(c)]
    inert = [atspi.label_text(c) for c in controls if not atspi.actions(c)]
    assert not nameless, f"{len(nameless)} action controls have no accessible name: {nameless}"
    assert not inert, f"action controls expose no action: {inert}"


@then("every visible toggle reports its state")
def step_toggles_readable(context):
    toggles = atspi.find_all(content(context), lambda n: atspi.role(n) in atspi.TOGGLE_ROLES)
    unreadable = [atspi.label_text(t) for t in toggles if atspi.checked(t) is None]
    assert not unreadable, f"toggles with no readable state: {unreadable}"


@then("every navigation page exposes accessible controls")
def step_every_page_controls(context):
    for item in context.navigation:
        step_open_page(context, item["title"])
        step_controls_accessible(context)
        step_toggles_readable(context)


# ---------------------------------------------------------------- switches


def find_switch(context, row):
    target = atspi.row_containing(content(context), row)
    return atspi.find(
        target,
        lambda n: atspi.role(n) in ("switch", "toggle button", "check box"),
        f"a switch in the {row!r} row",
    )


@step('I toggle the switch in the "{row}" row')
def step_toggle_switch(context, row):
    atspi.activate(find_switch(context, row))


@then('the switch in the "{row}" row is {state:w}')
def step_switch_state(context, row, state):
    if state not in ("on", "off"):
        raise NotImplementedError(f"unknown switch state {state!r}")
    want = state == "on"
    ok = atspi.poll(lambda: bool(atspi.checked(find_switch(context, row))) == want)
    assert ok, f"switch in {row!r} never turned {state}"


# ---------------------------------------------------------------- text


@then('I see "{text}"')
def step_see(context, text):
    wait_for_text(context, content(context), text)


@then('I do not see "{text}"')
def step_not_see(context, text):
    wait_for_text(context, content(context), text, present=False)


@then('the "{row}" row says "{text}"')
def step_row_says(context, row, text):
    def check():
        target = atspi.row_containing(content(context), row, timeout=1)
        return text_present(target, text)

    assert atspi.poll(check), f"row {row!r} never said {text!r}"


@step('I type "{value}" into the "{field}" field')
def step_type_into(context, value, field):
    entry = atspi.find(
        content(context),
        lambda n: atspi.role(n) in atspi.TEXT_ROLES
        and (atspi.name(n) == field or atspi.description(n) == field),
        f"a text field named {field!r}",
    )
    entry.grabFocus()
    atspi.type_text(value)


# ---------------------------------------------------------------- dialogs


@then('a dialog titled "{title}" is shown')
def step_dialog_shown(context, title):
    def check():
        dialog = current_dialog(context, timeout=1)
        return text_present(dialog, title, exact=True) and dialog

    dialog = atspi.poll(check)
    assert dialog, f"no showing dialog is titled {title!r}"
    context.dialog = dialog


@then('the dialog says "{text}"')
def step_dialog_says(context, text):
    wait_for_text(context, current_dialog(context), text)


@step('I choose "{label}" in the dialog')
def step_dialog_choose(context, label):
    dialog = current_dialog(context)
    atspi.activate(atspi.find_button(dialog, label))


@then("no dialog is shown")
def step_no_dialog(context):
    def gone():
        try:
            current_dialog(context, timeout=0)
        except AssertionError:
            return True
        return False

    assert atspi.poll(gone), "a dialog is still showing"


# ---------------------------------------------------------------- menus


@step('I open the main menu')
def step_open_menu(context):
    button = atspi.find(
        atspi.main_window(app(context)),
        lambda n: atspi.is_button(n, "Main Menu"),
        "the Main Menu button",
    )
    atspi.activate(button)
    items = atspi.poll(lambda: menu_items(context))
    assert items, "the main menu opened no menu items"


def menu_items(context):
    return [n for n in atspi.descendants(app(context), only_showing=True) if atspi.role(n) == "menu item"]


# The main menu's items in model order, as internal/window/window.go's
# buildMenuButton appends them. Used only while GTK publishes the popover's
# items without names (see the @known_issue scenario in shell.feature); an
# item that has a name is always matched by it instead.
MAIN_MENU_ORDER = ("Preferences", "Setup Assistant…", "Keyboard Shortcuts", "About Control Center")
MAIN_MENU_SHORTCUTS = {"Keyboard Shortcuts": "Control+?"}


def menu_item(context, label):
    items = atspi.poll(lambda: menu_items(context))
    assert items, "no menu is open"
    for item in items:
        if atspi.label_text(item) == label:
            return item
    shortcut = MAIN_MENU_SHORTCUTS.get(label)
    if shortcut:
        for item in items:
            if atspi.attributes(item).get("keyshortcuts") == shortcut:
                return item
    if len(items) == len(MAIN_MENU_ORDER) and label in MAIN_MENU_ORDER:
        return items[MAIN_MENU_ORDER.index(label)]
    raise AssertionError(f"no menu item {label!r}; items are {[atspi.label_text(i) for i in items]}")


@step('I choose "{label}" from the menu')
def step_choose_menu(context, label):
    atspi.activate(menu_item(context, label))


@then('the menu offers "{label}"')
def step_menu_offers(context, label):
    item = menu_item(context, label)
    assert atspi.actions(item), f"menu item {label!r} exposes no action"


@then("the menu has {count:d} operable items")
def step_menu_count(context, count):
    items = atspi.poll(lambda: menu_items(context))
    operable = [i for i in items or [] if atspi.actions(i) and atspi.sensitive(i)]
    assert len(operable) == count, f"menu has {len(operable)} operable items, want {count}"


@then("every menu item has an accessible name")
def step_menu_named(context):
    items = atspi.poll(lambda: menu_items(context)) or []
    nameless = [index for index, item in enumerate(items) if not atspi.label_text(item)]
    assert not nameless, f"menu items at positions {nameless} have no accessible name"


@then('a window titled "{title}" is shown')
def step_window_shown(context, title):
    context.window = atspi.top_level(app(context), title)


@then('the "{title}" window lists "{text}"')
def step_window_lists(context, title, text):
    wait_for_text(context, atspi.top_level(app(context), title), text)


@step('I close the "{title}" window')
def step_close_window(context, title):
    window = atspi.top_level(app(context), title)
    closers = atspi.find_all(window, lambda n: atspi.is_button(n, "Close"))
    if closers:
        atspi.activate(closers[0])
    else:
        atspi.press("Escape")
    gone = atspi.poll(
        lambda: not any(atspi.name(w) == title and atspi.showing(w) for w in atspi.children(app(context)))
    )
    assert gone, f"window {title!r} is still showing"


@then("the shortcuts window lists every navigation shortcut")
def step_shortcuts_complete(context):
    window = atspi.top_level(app(context), "Keyboard Shortcuts")
    missing = [s for s in context.shortcuts if not text_present(window, s, exact=True)]
    assert not missing, f"shortcuts window is missing {missing}"


# ---------------------------------------------------------------- journal


def journal(context):
    try:
        with open(context.journal_path, "r", encoding="utf-8") as handle:
            return [json.loads(line) for line in handle if line.strip()]
    except FileNotFoundError:
        return []


@then('the action journal records "{action}" as {suppressed}')
def step_journal_records(context, action, suppressed):
    def check():
        return [
            e for e in journal(context)
            if e.get("action") == action and e.get("suppressed") == suppressed
        ]

    entries = atspi.poll(check)
    assert entries, f"journal has no {action!r} entry suppressed={suppressed!r}; journal is {journal(context)}"
    context.journal_entry = entries[-1]


@then('the journalled command is "{argv}"')
def step_journal_argv(context, argv):
    got = " ".join(context.journal_entry.get("would_run") or [])
    assert got == argv, f"journalled command {got!r} != {argv!r}"


@then("the action journal is empty")
def step_journal_empty(context):
    entries = journal(context)
    assert not entries, f"expected no privileged actions, journal has {entries}"


@then('the action journal has no "{action}" entry')
def step_journal_lacks(context, action):
    entries = [e for e in journal(context) if e.get("action") == action]
    assert not entries, f"unexpected {action!r} journal entries: {entries}"
