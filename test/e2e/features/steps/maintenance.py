"""Steps for the Maintenance page and its Recovery detail.

Shared vocabulary (navigation, buttons, rows, dialogs, the action journal)
comes from common.py; this module adds only what the Maintenance destination
needs: reaching the Recovery detail by keyboard, leaving it again, and
checking what the fake package tools from fixtures/stubs_maintenance.py were
asked to do.

common.py is executed by behave rather than imported, so importing it here
would register every shared step twice; the few lookups needed are repeated
privately instead.
"""

import os

from behave import step, then

import chairlift_atspi as atspi

MAINTENANCE = "Maintenance"
RECOVERY = "Recovery"
# The Recovery entry on the Maintenance page is a plain activatable
# AdwActionRow; the free-space row is what tells the two pages apart.
MAINTENANCE_ONLY_ROW = "Free up space"
MAX_TAB_STOPS = 40


def _app(context):
    if context.app is None:
        raise AssertionError("ChairLift is not running in this scenario (@no-app?)")
    return context.app


def _content(context):
    return atspi.page_root(_app(context))


def _selected_sidebar_titles(context):
    return [atspi.label_text(r) for r in atspi.sidebar_rows(_app(context)) if atspi.selected(r)]


def _shows_label(root, title):
    return any(atspi.name(n) == title for n in atspi.search_nodes(root))


def _back_buttons(context):
    """Header-bar back buttons on the content side of the window.

    Matched by the "Back" prefix rather than the full name, so a scenario
    that is about the name can report what it actually is.
    """
    return atspi.find_all(
        _content(context),
        lambda n: atspi.is_button(n) and atspi.label_text(n).startswith("Back"),
    )


def _tool_calls(context):
    try:
        with open(os.path.join(context.scenario_dir, "tool-calls.log"), encoding="utf-8") as handle:
            return [line.rstrip("\n") for line in handle if line.strip()]
    except FileNotFoundError:
        return []


# ---------------------------------------------------------------- Recovery


@step("I open the Recovery detail")
def step_open_recovery(context):
    """Shift+Tab to the Maintenance page's Recovery row and press Return.

    The row publishes no AT-SPI action and GTK 4 does not honour
    Component.GrabFocus, so keyboard traversal is how an assistive
    technology user reaches it. Traversal runs backwards: Recovery is the
    page's last row, and Shift+Tab from the first sidebar row (where focus
    rests after the Alt+number shortcut) leaves the sidebar at once. Forward
    Tab would first walk the sidebar, and every sidebar Tab stop moves the
    row selection away from the page being shown (a shell defect reported
    separately), so the traversal refuses to continue once that happens.
    """
    row = atspi.row_containing(_content(context), RECOVERY)
    for _ in range(MAX_TAB_STOPS):
        if atspi.poll(lambda: atspi.focused(row), timeout=0.3):
            break
        selected = _selected_sidebar_titles(context)
        if selected != [MAINTENANCE]:
            raise AssertionError(f"keyboard traversal moved the sidebar selection to {selected}")
        atspi.press("<Shift>Tab")
    else:
        raise AssertionError(f"keyboard focus never reached the {RECOVERY!r} row in {MAX_TAB_STOPS} Shift+Tab presses")
    atspi.press("Return")
    step_recovery_shown(context)


@then("the Recovery detail is shown")
def step_recovery_shown(context):
    def settled():
        return (
            _selected_sidebar_titles(context) == [MAINTENANCE]
            and _shows_label(_content(context), RECOVERY)
            and not _shows_label(_content(context), MAINTENANCE_ONLY_ROW)
            and bool(_back_buttons(context))
        )

    assert atspi.poll(settled), (
        f"Recovery detail not shown: sidebar selection {_selected_sidebar_titles(context)}, "
        f"back buttons {atspi.describe(_back_buttons(context))}"
    )


@step("I go back from the Recovery detail")
def step_recovery_back(context):
    buttons = atspi.poll(lambda: _back_buttons(context))
    assert buttons, "the Recovery detail shows no back button"
    atspi.activate(buttons[0])


@then('the Recovery back button is named "{wanted}"')
def step_recovery_back_named(context, wanted):
    buttons = atspi.poll(lambda: _back_buttons(context))
    assert buttons, "the Recovery detail shows no back button"
    got = atspi.label_text(buttons[0])
    assert got == wanted, f"the Recovery back button is announced as {got!r}, want {wanted!r}"


# ---------------------------------------------------------------- journal


@then("the journalled action carries no argument")
def step_journal_no_argument(context):
    """Nothing but the command word crossed the pkexec boundary."""
    entry = context.journal_entry
    assert not entry.get("args"), f"journalled {entry.get('action')!r} carries arguments {entry.get('args')}"


# ---------------------------------------------------------------- stubbed tools


@then('the stubbed "{tool}" answered a read-only query')
def step_tool_consulted(context, tool):
    """The fake on PATH is the program the application actually runs."""
    ok = atspi.poll(lambda: any(line.split(" ", 1)[0] == tool for line in _tool_calls(context)))
    assert ok, f"the stubbed {tool!r} was never run; calls were {_tool_calls(context)}"


@then('the stubbed "{tool}" never ran "{subcommand}"')
def step_tool_never_ran(context, tool, subcommand):
    fake = os.path.join(context.stub_bin, tool)
    assert os.access(fake, os.X_OK), f"no stubbed {tool!r} is first on PATH; add its @stub tag"
    prefix = f"{tool} {subcommand}"
    ran = [line for line in _tool_calls(context) if line == prefix or line.startswith(prefix + " ")]
    assert not ran, f"the stubbed {tool!r} was asked to {subcommand!r}: {ran}"
