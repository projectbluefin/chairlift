"""Features destination steps: Developer Mode, selective Gaming and Printers.

The Developer Mode switch shows the invoking account's real developer-group
membership (internal/ublue reads it from the OS, and no stub can change it),
so a CI runner in `docker` starts with the switch on while the Dakota
container user starts with it off. Steps record the state the page restored
and derive the expected helper command from it, so the same scenario asserts
dx-enable on one host and dx-disable on the other.
"""

import json
import os

from behave import step, then

import chairlift_atspi as atspi
from apps import expander_header, expander_rows, focus_by_tab
from stubs_features import CALLS_LOG, GAMING_COMPONENTS, MICRO_MARKER, account_is_developer
from stubs_printers import quadlet_dir

UBLUE_HELPER = "/usr/bin/chairlift-helper"
PULP_ID = "org.gnome.gitlab.cheywood.Pulp"
OPML_PATH = os.path.join(".local", "share", "chairlift", "developer-feeds.opml")


NEGATIVE_WINDOW = 3.0


# ---------------------------------------------------------------- helpers


def _content(context):
    if context.app is None:
        raise AssertionError("ChairLift is not running in this scenario")
    return atspi.page_root(context.app)


def _texts(context):
    return atspi.all_text_under(_content(context))


def _log(context):
    try:
        with open(context.log_path, encoding="utf-8", errors="replace") as handle:
            return handle.read()
    except (OSError, AttributeError):
        return ""


def _journal(context):
    try:
        with open(context.journal_path, encoding="utf-8") as handle:
            return [json.loads(line) for line in handle if line.strip()]
    except FileNotFoundError:
        return []


def _calls(context):
    try:
        with open(os.path.join(context.scenario_dir, CALLS_LOG), encoding="utf-8") as handle:
            return [line.split() for line in handle if line.strip()]
    except FileNotFoundError:
        return []


def _switch(context, row):
    target = atspi.row_containing(_content(context), row)
    return atspi.find(target, lambda n: atspi.role(n) == "switch", f"a switch in the {row!r} row")


def _developer_initial(context):
    initial = getattr(context, "features_developer_initial", None)
    if initial is None:
        raise AssertionError("record the Developer Mode state first")
    return initial


def _developer_action(context):
    return "dx-disable" if _developer_initial(context) else "dx-enable"


def _wait_for_text(context, predicate, what):
    found = atspi.poll(lambda: next((t for t in _texts(context) if predicate(t)), None))
    assert found, f"{what} never appeared"
    return found


def _never(predicate, what, window=NEGATIVE_WINDOW):
    """Watch for window seconds and fail the moment predicate holds."""
    hit = atspi.poll(predicate, timeout=window)
    assert not hit, f"unexpectedly observed {what}: {hit!r}"


# ---------------------------------------------------------------- Developer Mode


@step("the Developer Mode switch shows this account's developer-group membership")
def step_developer_restored(context):
    expected = account_is_developer()
    def restored():
        switch = _switch(context, "Developer Mode")
        return atspi.sensitive(switch) and bool(atspi.checked(switch)) == expected
    assert atspi.poll(restored), "Developer Mode never became ready with the real account state"
    row = atspi.row_containing(_content(context), "Developer Mode")
    context.features_developer_text = atspi.all_text_under(row)
    context.features_developer_initial = expected


@then("the Developer Mode change is journalled as a dry run of the fixed helper")
def step_developer_journal(context):
    action = _developer_action(context)
    entries = atspi.poll(lambda: [e for e in _journal(context) if e.get("action") == action])
    assert entries, f"journal never recorded {action!r}; journal is {_journal(context)}"
    everything = _journal(context)
    assert len(everything) == 1, f"one toggle must journal one privileged action, got {everything}"
    entry = entries[0]
    assert entry.get("suppressed") == "dry-run", f"{action} was not suppressed as a dry run: {entry}"
    want = ["pkexec", UBLUE_HELPER, action, "--dry-run"]
    assert entry.get("would_run") == want, f"journalled argv {entry.get('would_run')} != {want}"
    assert not entry.get("args"), f"{action} passed arguments across the pkexec boundary: {entry}"


@then("the Developer Mode switch returns to its restored state")
def step_developer_reverted(context):
    initial = _developer_initial(context)
    def settled():
        switch = _switch(context, "Developer Mode")
        return bool(atspi.checked(switch)) == initial and atspi.sensitive(switch)
    assert atspi.poll(settled), "Developer Mode never restored its observed state"
    row = atspi.row_containing(_content(context), "Developer Mode")
    assert atspi.all_text_under(row) == context.features_developer_text, "dry run changed observed Developer Mode presentation"


@then("the Developer Mode preview toast is shown")
def step_developer_toast(context):
    verb = "disabled" if _developer_initial(context) else "enabled"
    wanted = f"[DRY-RUN] Preview: developer mode would be {verb} — no changes made"
    _wait_for_text(context, lambda t: t == wanted, repr(wanted))


@then("the Custom Command Menu change is previewed for the two developer entries only")
def step_devmenu_preview(context):
    # The fake's current tuples match the restored state and its distro
    # defaults are Terminal visible, Containers hidden (stubs_features.devmenu),
    # so each direction resets one managed key and writes the other.
    if _developer_initial(context):
        wanted = (
            "[DRY-RUN] would set Custom Command Menu command1 visible=false",
            "[DRY-RUN] would reset Custom Command Menu command2 to default",
        )
    else:
        wanted = (
            "[DRY-RUN] would reset Custom Command Menu command1 to default",
            "[DRY-RUN] would set Custom Command Menu command2 visible=true",
        )
    for line in wanted:
        assert atspi.poll(lambda: line in _log(context)), f"chairlift.log never contained {line!r}"
    log = _log(context)
    assert "Custom Command Menu command3" not in log, "Developer Mode touched an entry it does not manage"


@then("the Developer feed onboarding never starts")
def step_feeds_never(context):
    opml = os.path.join(context.home, OPML_PATH)

    def started():
        log = _log(context)
        for marker in (PULP_ID, "developer feeds OPML", "[developerfeeds]"):
            if marker in log:
                return f"log line mentioning {marker!r}"
        if os.path.exists(opml):
            return f"staged catalog {opml}"
        installs = [c for c in _calls(context) if c[:2] == ["flatpak", "install"]]
        return installs or None

    _never(started, "developer feed onboarding")


# ---------------------------------------------------------------- fakes


@then('the fake {tool} was never asked to "{subcommand}"')
def step_fake_never(context, tool, subcommand):
    calls = [c for c in _calls(context) if len(c) > 1 and c[0] == tool and c[1] == subcommand]
    assert not calls, f"{tool} received {subcommand!r}: {calls}"


# ---------------------------------------------------------------- Gaming


def _gaming_checkbox(context, name):
    row = atspi.row_containing(_content(context), name)
    return atspi.find(row, lambda n: atspi.role(n) == "check box", "gaming selection for " + name)


@step('I select the "{name}" gaming component')
def step_select_gaming(context, name):
    assert atspi.poll(lambda: atspi.sensitive(_gaming_checkbox(context, name))), "gaming inventory never settled"
    choice = _gaming_checkbox(context, name)
    if not atspi.checked(choice):
        # Dakota's GtkCheckButton is focusable but exposes no AT-SPI action.
        # Use the same bounded keyboard traversal as the editor expander.
        focus_by_tab(context, choice, f"the {name!r} gaming selection")
        atspi.press("space")
    assert atspi.poll(lambda: atspi.checked(_gaming_checkbox(context, name))), "gaming selection was not applied"


@then("no gaming component is selected")
def step_no_gaming_selection(context):
    assert atspi.poll(lambda: atspi.sensitive(_gaming_checkbox(context, "Steam"))), "gaming inventory never settled"
    for name, _, _ in GAMING_COMPONENTS:
        assert not atspi.checked(_gaming_checkbox(context, name)), "hidden default selection: " + name


@then('each gaming component says "{status}"')
def step_gaming_component_status(context, status):
    for name, _, _ in GAMING_COMPONENTS:
        def observed():
            row = atspi.row_containing(_content(context), name)
            return any(text.endswith(status) for text in atspi.all_text_under(row))
        assert atspi.poll(observed), name + " never reported " + status


@then('the gaming preview {operation:w} only "{name}" {preposition:w} the {scope:w} scope')
def step_gaming_selected_preview(context, operation, name, preposition, scope):
    verb = {"installs": "install", "removes": "uninstall"}[operation]
    if scope not in ("system", "user"):
        raise NotImplementedError(f"unknown Flatpak scope {scope!r}")
    chosen = next(app_id for title, app_id, _ in GAMING_COMPONENTS if title == name)
    expected = f"[DRY-RUN] Would execute: flatpak {verb} -y --{scope} {chosen}"
    assert atspi.poll(lambda: expected in _log(context)), "chosen app was not previewed"
    other = "user" if scope == "system" else "system"
    assert f"flatpak {verb} -y --{other} {chosen}" not in _log(context), f"chosen app was mutated in the {other} scope"
    for _, app_id, _ in GAMING_COMPONENTS:
        if app_id != chosen:
            for each in ("user", "system"):
                assert f"flatpak {verb} -y --{each} {app_id}" not in _log(context), "unchosen app was mutated: " + app_id


@then("the developer editor choices match the documented catalog")
def step_developer_editors(context):
    expected = ["Dev Container CLI", "VSCode Stable", "VSCode Insiders", "VSCodium", "Antigravity", "JetBrains Toolbox", "Neovim", "Helix", "Vim", "Micro"]
    for title in expected:
        assert atspi.poll(lambda: atspi.row_containing(_content(context), title)), "missing tool choice: " + title
    # A row and its label both publish the title. Count catalog rows, not
    # every text-bearing accessibility node, and reject extra IDE choices.
    titles = expander_rows(expander_header(context, "IDEs and terminal editors", "Developer"))
    assert titles == expected, f"developer editor rows {titles} != {expected}"


@step('Homebrew stops listing "{formula}"')
def step_brew_stops_listing(context, formula):
    # Only the features-developer-micro stub models a listing that changes.
    if formula != "micro":
        raise NotImplementedError(f"no stub models removing {formula!r}")
    os.remove(os.path.join(context.scenario_dir, MICRO_MARKER))


@then("the Gaming inventory is read again after the change")
def step_gaming_reread(context):
    # One inventory at page build, one inside Enable/Disable, one from the
    # refresh that follows: the runtime listing is internal/gaming's alone.
    def reread():
        return sum(1 for c in _calls(context) if c[:4] == ["flatpak", "list", "--user", "--runtime"]) >= 3

    assert atspi.poll(reread), f"gaming state was not re-read after the change; calls {_calls(context)}"


@then('the switch in the "{row}" row {verdict:w} input')
def step_switch_sensitivity(context, row, verdict):
    if verdict not in ("accepts", "refuses"):
        raise NotImplementedError(f"unknown verdict {verdict!r}")
    want = verdict == "accepts"
    ok = atspi.poll(lambda: atspi.sensitive(_switch(context, row)) == want)
    assert ok, f"switch in {row!r} never {'became' if want else 'stopped being'} operable"


# ---------------------------------------------------------------- visibility


def _groups(context, title):
    return atspi.find_all(_content(context), lambda n: atspi.role(n) == "grouping" and atspi.name(n) == title)


@then('the Features page shows a "{title}" group')
def step_group_shown(context, title):
    assert atspi.poll(lambda: _groups(context, title)), f"no {title!r} group on the Features page"


@then('the Features page shows no "{title}" group')
def step_group_absent(context, title):
    groups = _groups(context, title)
    assert not groups, f"the Features page shows a {title!r} group"


@then('the "{row}" row offers no switch')
def step_row_no_switch(context, row):
    target = atspi.row_containing(_content(context), row)
    switches = atspi.find_all(target, lambda n: atspi.role(n) in atspi.TOGGLE_ROLES)
    assert not switches, f"row {row!r} offers a switch"


@then("the Features destination is hidden or says why it offers nothing")
def step_features_not_blank(context):
    rows = [atspi.label_text(r) for r in atspi.sidebar_rows(context.app)]
    if "Features" not in rows:
        return
    # Alt+number is compacted over the visible pages, so the row index is
    # the accelerator.
    atspi.press(f"<Alt>{rows.index('Features') + 1}")
    assert atspi.poll(
        lambda: any(atspi.selected(r) and atspi.label_text(r) == "Features" for r in atspi.sidebar_rows(context.app))
    ), "the Features page never opened"
    # GTK names the destination's navigation page after it; that page also
    # carries the header bar, so its title and window buttons are not content.
    chrome = {"Features", "Minimize", "Maximize", "Close",
              "Minimize the window", "Maximize the window", "Close the window"}

    def page_text():
        pages = _groups(context, "Features")
        return [t for p in pages for t in atspi.all_text_under(p) if t not in chrome]

    assert atspi.poll(lambda: _groups(context, "Features")), "no Features page is showing"
    page = atspi.poll(page_text, timeout=5)
    assert page, "the Features page is advertised in the sidebar but shows nothing at all"


# ---------------------------------------------------------------- Printers


PRINTERS_GROUP = "Printers"

# pageview.PrinterAppSubtitle for printerapp.StateBlocked: the ADR-0016
# condition as a person meets it.
PRINTER_BLOCKED_SUBTITLE = "Can't be turned on until its settings page can be password-protected."


def _printer_rows(context):
    """Every list row inside the Printers group, in display order."""
    groups = _groups(context, PRINTERS_GROUP)
    assert groups, f"no {PRINTERS_GROUP!r} group on the Features page"
    return atspi.find_all(groups[0], lambda n: atspi.role(n) in atspi.ROW_ROLES)


@then("the Printers group offers exactly these rows, each off and locked")
def step_printer_rows_locked(context):
    want = [row["row"] for row in context.table]
    assert atspi.poll(lambda: _groups(context, PRINTERS_GROUP)), f"no {PRINTERS_GROUP!r} group on the Features page"
    group = _groups(context, PRINTERS_GROUP)[0]
    for title in want:
        row = atspi.row_containing(group, title)
        switch = atspi.find(row, lambda n: atspi.role(n) == "switch", f"a switch in the {title!r} row")
        # ADR-0016: shown, off, and locked — never a switch that silently
        # does nothing, never a false enabled indicator.
        assert atspi.checked(switch) is False, f"{title!r} switch is not off"
        assert not atspi.sensitive(switch), f"{title!r} switch accepts input while its family is blocked"
        texts = atspi.all_text_under(row)
        assert PRINTER_BLOCKED_SUBTITLE in texts, f"{title!r} row does not say why it is locked: {texts}"
    got = [atspi.label_text(r) for r in _printer_rows(context)]
    assert len(got) == len(want), f"Printers group rows {got} != {want}"


@then("no printer quadlet was written")
def step_no_quadlet(context):
    directory = quadlet_dir(context)
    written = sorted(os.listdir(directory)) if os.path.isdir(directory) else []
    assert not written, f"quadlets were written under {directory}: {written}"
