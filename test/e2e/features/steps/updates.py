"""Steps for the Updates destination (updates.feature).

The destination is the status-first update shell (internal/views/
update_shell.go): a header of wordmark, primary action, and one status line
(plus a detail line for failures), whose text comes from
internal/views/updatepresent, and one row per update source.
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


# The status line carries this accessible description (update_shell.go), which
# is how it is told apart from a source row saying the same words, such as
# "Up to date" or "Checking for updates…".
STATUS_DESCRIPTION = "Update status"

# Every label updatepresent gives the primary action.
PRIMARY_LABELS = ("Check again", "Update all", "Try again", "Retry failed")


def _status_nodes(context):
    return atspi.find_all(content(context), lambda n: atspi.description(n) == STATUS_DESCRIPTION)


def _status_line(context):
    """The one status line on screen, or None when there is not exactly one."""
    nodes = _status_nodes(context)
    if len(nodes) != 1:
        return None
    return atspi.name(nodes[0]) or atspi.text(nodes[0])


@step('the Updates status reads "{line}"')
def step_status_reads(context, line):
    ok = atspi.poll(lambda: _status_line(context) == line)
    assert ok, f"the Updates status reads {_status_line(context)!r}, want {line!r}"


@step('the Updates status starts with "{prefix}"')
def step_status_starts_with(context, prefix):
    ok = atspi.poll(lambda: (_status_line(context) or "").startswith(prefix))
    assert ok, f"the Updates status reads {_status_line(context)!r}, want it to start with {prefix!r}"


@then("the Updates status sits directly under the primary action")
def step_status_under_primary(context):
    def ordered():
        nodes = list(atspi.search_nodes(content(context)))
        primary = [i for i, n in enumerate(nodes) if any(atspi.is_button(n, label) for label in PRIMARY_LABELS)]
        status = [i for i, n in enumerate(nodes) if atspi.description(n) == STATUS_DESCRIPTION]
        if len(primary) != 1 or len(status) != 1 or status[0] < primary[0]:
            return False
        # Only the primary's own label may stand between them: no title,
        # description, or other text sits between the action and its status.
        action = atspi.label_text(nodes[primary[0]])
        between = nodes[primary[0] + 1 : status[0]]
        return not any(atspi.role(n) == "label" and atspi.name(n) not in ("", action) for n in between)

    assert atspi.poll(ordered), "the status line does not directly follow the primary action"


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


@then("the Updates progress bar is {visibility:w}")
def step_progress_visibility(context, visibility):
    assert visibility in ("shown", "hidden")
    assert atspi.poll(
        lambda: bool(atspi.find_all(content(context), lambda n: atspi.role(n) == "progress bar"))
        == (visibility == "shown")
    ), f"Updates progress bar is not {visibility}"


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


@then('the update for "{title}" is listed once with an accessible action')
def step_single_visible_update(context, title):
    def check():
        rows = atspi.find_all(
            content(context),
            lambda node: atspi.role(node) in atspi.ROW_ROLES and any(
                atspi.name(child) == title for child in atspi.descendants(node, only_showing=True)
            ),
        )
        return len(rows) == 1 and any(
            atspi.is_button(node, "Update") for node in atspi.descendants(rows[0], only_showing=True)
        )
    assert atspi.poll(check), f"{title!r} is duplicated or its Update action is hidden"


@step("software source discovery is reachable again")
def step_sources_reachable(context):
    os.unlink(_state(context, "brew-sources.fail"))


@step("the Flatpak update check is allowed to finish")
def step_release_flatpak_check(context):
    with open(_state(context, "flatpak-remote-ls-user.release"), "w", encoding="utf-8"):
        pass
