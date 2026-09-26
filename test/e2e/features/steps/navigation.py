"""Steps for sidebar keyboard behavior (navigation.feature)."""

from behave import step

import chairlift_atspi as atspi


@step('keyboard focus is on the "{title}" sidebar row')
def step_focus_sidebar_row(context, title):
    """Walk the focus chain with Tab until the titled sidebar row has focus.

    GTK 4 does not implement AT-SPI Component.GrabFocus, so Tab is the only
    way to place keyboard focus. Entering the list lands on its selected row.
    """
    atspi.focus_by_tab(
        context.app,
        lambda n: atspi.role(n) in atspi.ROW_ROLES
        and atspi.label_text(n) == title
        and atspi.is_sidebar(atspi.safe(lambda: n.parent)),
        f"the {title!r} sidebar row",
    )


@step('keyboard focus is on a sidebar row other than "{title}"')
def step_focus_elsewhere_in_sidebar(context, title):
    """Proves the keys moved focus through the list, so a selection that
    stayed put is the fix and not a keypress that went nowhere."""
    def focused_titles():
        return [
            atspi.label_text(row)
            for row in atspi.sidebar_rows(context.app)
            if atspi.focused(row)
        ]

    ok = atspi.poll(lambda: [t for t in focused_titles() if t != title], timeout=5)
    assert ok, f"sidebar focus is on {focused_titles()}, not on a row other than {title!r}"
