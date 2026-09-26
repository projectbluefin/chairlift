"""Steps for the fail-closed configuration and configuration-driven visibility
suite (config.feature).

The shared inventory steps in common.py compare against the full navigation
list; these assert exact reduced inventories instead, because a broken or
narrowing configuration is precisely what changes the sidebar.
"""

import time

from behave import step, then

import chairlift_atspi as atspi

# The persistent AdwToast is published as an "alert" in the main window.
TOAST_ROLE = "alert"
# How many Tab presses may pass before a row is declared unreachable by
# keyboard. A preferences page has a few dozen focus stops at most.
TAB_LIMIT = 40


def _app(context):
    if context.app is None:
        raise AssertionError("ChairLift is not running in this scenario (@no-app?)")
    return context.app


def _content(context):
    return atspi.page_root(_app(context))


def _titles(value):
    return [part.strip() for part in value.split(",") if part.strip()]


def _sidebar_titles(context):
    return [atspi.label_text(row) for row in atspi.sidebar_rows(_app(context))]


def _error_toasts(context):
    """Showing alert nodes in the main window whose text is the config diagnostic."""
    found = []
    for node in atspi.content_nodes(_app(context)):
        if atspi.role(node) != TOAST_ROLE:
            continue
        for child in atspi.descendants(node, only_showing=True):
            text = atspi.name(child) or atspi.text(child)
            if text.startswith("Configuration error: "):
                found.append((node, text))
                break
    return found


def _toast_message(context, timeout=atspi.DEFAULT_TIMEOUT):
    toasts = atspi.poll(lambda: _error_toasts(context), timeout=timeout)
    if not toasts:
        alerts = [atspi.label_text(n) for n in atspi.content_nodes(_app(context)) if atspi.role(n) == TOAST_ROLE]
        raise AssertionError(f"no configuration error toast is showing; alerts are {alerts}")
    assert len(toasts) == 1, f"expected one configuration error toast, found {[t for _, t in toasts]}"
    return toasts[0]


def _focus_row_by_tab(context, title):
    """Move keyboard focus onto the row titled title with Tab.

    GTK 4 refuses AT-SPI grabFocus on nested preference rows, and the rows
    have neither an action nor screen coordinates, so Tab traversal is the
    only way a keyboard user (or this suite) reaches them.
    """
    def focused_row():
        row = atspi.row_containing(_content(context), title, timeout=1)
        return row if atspi.focused(row) else None

    for _ in range(TAB_LIMIT):
        if focused_row() is not None:
            return focused_row()
        atspi.press("Tab")
        # Poll briefly after each press rather than checking once after a
        # fixed pause, so slow focus movement is not stepped over.
        found = atspi.poll(focused_row, timeout=0.5)
        if found:
            return found
    raise AssertionError(f"Tab never moved keyboard focus onto the {title!r} row")


# ---------------------------------------------------------------- sidebar


@then('the sidebar lists exactly "{titles}"')
def step_sidebar_exactly(context, titles):
    want = _titles(titles)
    ok = atspi.poll(lambda: _sidebar_titles(context) == want)
    assert ok, f"sidebar rows {_sidebar_titles(context)} != {want}"


@then("the shortcuts window lists exactly these page shortcuts")
def step_shortcuts_exactly(context):
    """Pair each "Go to <page>" label with the accelerator label after it."""
    want = [(row["page"], row["shortcut"]) for row in context.table]
    window = atspi.top_level(_app(context), "Keyboard Shortcuts")

    def pairs():
        labels = [
            atspi.name(n)
            for n in atspi.descendants(window, only_showing=True)
            if atspi.role(n) in atspi.LABEL_ROLES and atspi.name(n)
        ]
        found = []
        for index, label in enumerate(labels):
            if label.startswith("Go to ") and index + 1 < len(labels):
                found.append((label[len("Go to "):], labels[index + 1]))
        return found

    ok = atspi.poll(lambda: pairs() == want)
    assert ok, f"shortcuts window pairs {pairs()} != {want}"


# ---------------------------------------------------------------- toast


@then('the configuration error toast reports a "{kind}" error: "{detail}"')
def step_toast_reports(context, kind, detail):
    """The toast is config.LoadError.ToastMessage for the fixture that failed."""
    _, message = _toast_message(context)
    prefix = f"Configuration error: config {kind} error: {context.config_path}: "
    suffix = (
        "All feature groups are disabled. Fix the configuration file and restart "
        f"{context.expected_app_name}."
    )
    assert message.startswith(prefix), f"toast {message!r} does not start with {prefix!r}"
    assert message.endswith(suffix), f"toast {message!r} does not end with {suffix!r}"
    assert detail in message[len(prefix):-len(suffix)], f"toast {message!r} does not name {detail!r}"


@then('the configuration error toast states "{detail}" once')
def step_toast_cause_once(context, detail):
    _, message = _toast_message(context)
    count = message.count(detail)
    assert count == 1, f"toast names {detail!r} {count} times: {message!r}"


@then("the configuration error toast is still shown after {seconds:d} seconds")
def step_toast_persists(context, seconds):
    _toast_message(context)
    time.sleep(seconds)
    _toast_message(context, timeout=0.5)


@step("I dismiss the configuration error toast")
def step_toast_dismiss(context):
    toast, _ = _toast_message(context)
    atspi.activate(atspi.find_button(toast, "Dismiss"))


@then("no configuration error toast is shown")
def step_toast_absent(context):
    ok = atspi.poll(lambda: not _error_toasts(context))
    assert ok, f"a configuration error toast is still showing: {[t for _, t in _error_toasts(context)]}"


# ---------------------------------------------------------------- rows


@step('I expand the "{title}" row with the keyboard')
def step_expand_row(context, title):
    _focus_row_by_tab(context, title)
    atspi.press("Return")


@then('the "{row}" source row does not say "{text}"')
def step_source_row_lacks(context, row, text):
    target = atspi.row_containing(_content(context), row)
    said = [v for v in atspi.all_text_under(target) if text in v]
    assert not said, f"row {row!r} says {said}"


@then('the feature availability list {verdict:w} "{title}"')
def step_availability_verdict(context, verdict, title):
    """includes/omits a row in Help's expanded "Why is something missing?" list.

    The expander must be open (a named row besides its header is showing), so
    "omits" cannot pass against a collapsed or unrendered list.
    """
    if verdict not in ("includes", "omits"):
        raise ValueError(f"verdict must be includes or omits, not {verdict!r}")
    group = atspi.find(
        _content(context),
        lambda n: atspi.role(n) == "grouping" and atspi.name(n) == "Feature availability",
        "the Feature availability group",
    )

    def listed_titles():
        return [
            atspi.name(n)
            for n in atspi.descendants(group, only_showing=True)
            if atspi.role(n) in atspi.ROW_ROLES and atspi.name(n)
        ]

    expanded = atspi.poll(lambda: len(listed_titles()) > 1)
    assert expanded, f"the feature availability list is not expanded: {listed_titles()}"
    titles = listed_titles()
    if verdict == "includes":
        assert title in titles, f"feature availability list {titles} does not list {title!r}"
    else:
        assert title not in titles, f"feature availability list {titles} lists {title!r}"
