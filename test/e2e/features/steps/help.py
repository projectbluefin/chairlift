"""Steps for the Help destination (help.feature).

Help's resource rows are activatable AdwActionRows. GTK publishes list rows
with no AT-SPI action and no screen coordinates on X11, so a row is operated
the way a keyboard user operates it: focus it, press Return.

The stubs these steps read back live in fixtures/stubs_help.py.
"""

import os

from behave import step, then

import chairlift_atspi as atspi
import stubs_help

RESOURCES_GROUP = "Help & Resources"
TROUBLESHOOT_ROW = "Enhanced Troubleshooting"


def _content(context):
    if context.app is None:
        raise AssertionError("ChairLift is not running in this scenario (@no-app?)")
    return atspi.page_root(context.app)


def _record(context, name):
    try:
        with open(os.path.join(context.scenario_dir, name), "r", encoding="utf-8") as handle:
            return [line.rstrip("\n") for line in handle]
    except FileNotFoundError:
        return []


def _read_log(context):
    try:
        with open(context.log_path, "r", encoding="utf-8", errors="replace") as handle:
            return handle.read()
    except OSError:
        return ""


def _group(context, title):
    """The showing preferences group titled title, or None."""
    for node in atspi.search_nodes(_content(context)):
        if atspi.role(node) == "grouping" and atspi.name(node) == title:
            return node
    return None


def _rows(group):
    return [n for n in atspi.descendants(group, only_showing=True) if atspi.role(n) in atspi.ROW_ROLES]


def _row_labels(row):
    return [
        atspi.name(n)
        for n in atspi.descendants(row, only_showing=True)
        if n is not row and atspi.role(n) in atspi.LABEL_ROLES and atspi.name(n)
    ]


def _resource_rows(context):
    group = _group(context, RESOURCES_GROUP)
    if group is None:
        return None
    return [tuple(_row_labels(row)[:2]) for row in _rows(group)]


def _troubleshoot_subtitle(context):
    try:
        row = atspi.row_containing(_content(context), TROUBLESHOOT_ROW, timeout=1)
    except atspi.TreeError:
        return None
    labels = [label for label in _row_labels(row) if label != TROUBLESHOOT_ROW]
    # The first remaining label is the subtitle; the rest are the button's.
    return labels[0] if labels else None


# ---------------------------------------------------------------- resources


@then("the Help resources are, in order")
def step_resources_in_order(context):
    want = [(r["title"], r["url"]) for r in context.table]
    got = atspi.poll(lambda: (_resource_rows(context) == want) and want)
    assert got, f"Help resource rows {_resource_rows(context)} != {want}"


@then("the Help page has no resource links")
def step_no_resources(context):
    # Settle on the page first: the Diagnostics group is always built.
    assert atspi.poll(lambda: _group(context, "Diagnostics")), "the Help page never finished building"
    rows = _resource_rows(context)
    assert rows is None, f"the {RESOURCES_GROUP!r} group is showing with rows {rows}"


def _focus_row_in_group(context, group_title, title, attempts=40):
    """Move keyboard focus to the row titled title in a group, as a keyboard user would.

    GTK 4 does not implement AT-SPI grabFocus for list rows, so this tabs
    into the group's list and then walks it with the arrow keys. Rows are
    matched by title, not position: an AdwExpanderRow publishes its header
    as a list item nested inside the group's own list item, so one title
    can name two nested rows.
    """
    trace = []
    for _ in range(attempts):
        group = _group(context, group_title)
        assert group is not None, f"no {group_title!r} group is showing"
        rows = _rows(group)
        titles = [(_row_labels(r) or [None])[0] for r in rows]
        assert title in titles, f"no row titled {title!r} in {group_title!r}; rows are {titles}"
        focused = [i for i, r in enumerate(rows) if atspi.focused(r)]
        if focused and titles[focused[0]] == title:
            return rows[focused[0]]
        if focused:
            atspi.press("Down" if titles.index(title) > focused[0] else "Up")
        else:
            trace.append(
                [
                    f"{atspi.role(n)}:{atspi.label_text(n)!r}"
                    for n in atspi.search_nodes(_content(context))
                    if atspi.focused(n)
                ]
            )
            atspi.press("Tab")
    raise AssertionError(f"keyboard focus never reached the {title!r} row in {group_title!r}; focus went {trace}")


@step('I open the "{title}" Help link')
def step_open_link(context, title):
    assert atspi.poll(lambda: _group(context, RESOURCES_GROUP)), f"no {RESOURCES_GROUP!r} group is showing"
    _focus_row_in_group(context, RESOURCES_GROUP, title)
    atspi.press("Return")


@then('xdg-open was asked to open "{url}"')
def step_url_opened(context, url):
    ok = atspi.poll(lambda: _record(context, stubs_help.URL_RECORD) == [url])
    assert ok, f"xdg-open calls {_record(context, stubs_help.URL_RECORD)} != [{url!r}]"


@then("the Help page is still responsive")
def step_still_responsive(context):
    assert context.app_process.poll() is None, "ChairLift exited"
    assert atspi.poll(lambda: _group(context, "Diagnostics")), "the Help page stopped publishing its content"


# ---------------------------------------------------------------- troubleshooting


@then('the Enhanced Troubleshooting status is "{text}"')
def step_troubleshoot_status(context, text):
    ok = atspi.poll(lambda: _troubleshoot_subtitle(context) == text)
    assert ok, f"Enhanced Troubleshooting subtitle is {_troubleshoot_subtitle(context)!r}, want {text!r}"


@then("the Enhanced Troubleshooting group is not shown")
def step_troubleshoot_absent(context):
    assert atspi.poll(lambda: _group(context, "Diagnostics")), "the Help page never finished building"
    assert _group(context, TROUBLESHOOT_ROW) is None, "the Enhanced Troubleshooting group is showing"


@then('Goose was launched as "{desktop_id}"')
def step_goose_launched(context, desktop_id):
    ok = atspi.poll(lambda: _record(context, stubs_help.LAUNCH_RECORD) == [desktop_id])
    assert ok, f"gtk-launch calls {_record(context, stubs_help.LAUNCH_RECORD)} != [{desktop_id!r}]"


@then('the troubleshooting setup never ran "brew {verb}"')
def step_brew_never(context, verb):
    calls = [c for c in _record(context, stubs_help.BREW_RECORD) if c.split(" ", 1)[0] == verb]
    assert not calls, f"brew ran {verb!r} for real: {calls}"


@then('the troubleshooting setup previewed exactly')
def step_setup_previewed(context):
    """The dry-run preview lines, in order, and no other setup step."""
    want = [row["command"] for row in context.table]
    candidates = (
        "[DRY-RUN] Would execute: brew tap ",
        "[DRY-RUN] Would execute: brew install ",
        "[DRY-RUN] would execute: goose-mcp-setup",
    )

    def previews():
        got = []
        for line in _read_log(context).splitlines():
            for prefix in candidates:
                at = line.find(prefix)
                if at != -1:
                    got.append(line[at + len("[DRY-RUN] "):].split(": ", 1)[1])
        return got

    ok = atspi.poll(lambda: previews() == want)
    assert ok, f"setup previews {previews()} != {want}"


@then("Goose's configuration file was not written")
def step_goose_config_untouched(context):
    path = os.path.join(context.home, ".config", "goose", "config.yaml")
    assert not os.path.exists(path), f"{path} exists after a dry-run setup"


# ---------------------------------------------------------------- feature availability

AVAILABILITY_GROUP = "Feature availability"
AVAILABILITY_EXPANDER = "Why is something missing?"


def _availability(context):
    """{feature title: subtitle} for the rows the expanded explainer lists."""
    group = _group(context, AVAILABILITY_GROUP)
    if group is None:
        return None
    listed = {}
    for row in _rows(group):
        labels = _row_labels(row)
        if labels and labels[0] != AVAILABILITY_EXPANDER:
            listed[labels[0]] = labels[1] if len(labels) > 1 else ""
    return listed


@step("I ask the Help page why something is missing")
def step_expand_availability(context):
    assert atspi.poll(lambda: _group(context, AVAILABILITY_GROUP)), f"no {AVAILABILITY_GROUP!r} group is showing"
    _focus_row_in_group(context, AVAILABILITY_GROUP, AVAILABILITY_EXPANDER)
    atspi.press("Return")
    assert atspi.poll(lambda: _availability(context)), "the explainer never listed a missing feature"


@then('the Help page explains "{feature}" with "{reason}"')
def step_availability_reason(context, feature, reason):
    ok = atspi.poll(lambda: (_availability(context) or {}).get(feature) == reason)
    assert ok, f"explainer rows {_availability(context)} do not give {feature!r} the reason {reason!r}"


@then('the Help page does not call "{feature}" missing')
def step_availability_absent(context, feature):
    listed = _availability(context) or {}
    assert feature not in listed, f"{feature!r} is explained as missing: {listed}"

