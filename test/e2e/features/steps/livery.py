"""Steps for the Livery page (features/livery.feature).

The page is three AdwPreferencesGroups (plus the Profile Picture group) whose
rows repeat titles — both the panel and the dock have a "Rotate at Login"
row — so these steps scope every lookup to a section, the group GTK publishes
as a "grouping" named by the section title. Selection rows and chooser
results are activatable list rows with no AT-SPI action, so they are driven
the way a keyboard user drives them: focus the row, press Return.
"""

import os
import re

from behave import step, then

import chairlift_atspi as atspi

LIVERY_SCHEMA = "io.projectbluefin.chairlift.livery"
EXTENSION_SCHEMA = "org.gnome.shell.extensions.custom-command-list"
ROTATION_UNIT = "chairlift-livery-rotate.service"
CALLS = "livery-calls.log"

# Command lines that change state. Reads (gsettings get/list-recursively,
# dconf read, systemctl show/is-active) are how the page loads and are fine.
MUTATING = (
    re.compile(r"^gsettings (set|reset|reset-recursively) "),
    re.compile(r"^dconf (write|reset|load|update) "),
    re.compile(r"^gtk-update-icon-cache\b"),
    re.compile(r"^kwriteconfig6\b"),
    re.compile(r"^systemctl .*\b(enable|disable|daemon-reload|start|stop|restart)\b"),
)


# ---------------------------------------------------------------- lookups


def _app(context):
    if context.app is None:
        raise AssertionError("ChairLift is not running in this scenario (@no-app?)")
    return context.app


def _content(context):
    return atspi.page_root(_app(context))


def _log(context):
    try:
        with open(context.log_path, encoding="utf-8", errors="replace") as handle:
            return handle.read()
    except OSError:
        return ""


def _section(context, title, timeout=atspi.DEFAULT_TIMEOUT):
    return atspi.find(
        _content(context),
        lambda n: atspi.role(n) == "grouping" and atspi.name(n) == title,
        f"a Livery section titled {title!r}",
        timeout=timeout,
    )


def _row(root, title, timeout=atspi.DEFAULT_TIMEOUT):
    return atspi.row_containing(root, title, timeout=timeout)


def _section_row(context, row, section):
    return _row(_section(context, section), row)


def _switch(context, row, section):
    return atspi.find(
        _section_row(context, row, section),
        lambda n: atspi.role(n) == "switch",
        f"a switch in the {row!r} row of {section!r}",
    )


def _dialog(context, timeout=atspi.DEFAULT_TIMEOUT):
    """The in-window AdwDialog, which GTK publishes with the dialog role.

    Toasts are published as "alert" nodes, so alerts are not dialogs here.
    """
    def lookup():
        found = None
        for node in atspi.descendants(_app(context), only_showing=True):
            if atspi.role(node) == "dialog":
                found = node
        return found

    dialog = atspi.poll(lookup, timeout=timeout)
    if dialog is None:
        raise AssertionError(f"no dialog is showing after {timeout}s")
    return dialog


def _dialog_rows(dialog):
    return [
        n for n in atspi.descendants(dialog, only_showing=True)
        if atspi.role(n) in atspi.ROW_ROLES
    ]


def _row_title(row):
    return atspi.label_text(row)


def _has_focus(node):
    return any(atspi.focused(n) for n in atspi.descendants(node, only_showing=True))


def _keyboard_activate(context, scope, target_title, what, presses=120):
    """Reach an action-less activatable row by keyboard and press Return.

    GTK 4 does not implement AT-SPI's Component.GrabFocus for these rows, so
    focus travels the way a keyboard user moves it: Tab until focus enters
    one of the rows under scope, then Up/Down within that list to the row
    whose title is target_title.
    """
    for _ in range(presses):
        rows = [r for r in atspi.descendants(scope(), only_showing=True) if atspi.role(r) in atspi.ROW_ROLES]
        titles = [_row_title(r) for r in rows]
        if target_title not in titles:
            raise AssertionError(f"{what} is not showing; rows are {titles}")
        target = titles.index(target_title)
        if atspi.focused(rows[target]):
            atspi.press("Return")
            return
        current = next((i for i, r in enumerate(rows) if _has_focus(r)), None)
        if current is None:
            atspi.press_and_settle(context.app, "Tab")
        elif current == target:
            # Focus is on a control inside the row; Return would operate that
            # control rather than activate the row.
            atspi.press("<Shift>Tab")
            if not atspi.poll(lambda: atspi.focused(rows[target]), timeout=1):
                raise AssertionError(f"keyboard focus reached a control inside {what}, not the row")
        else:
            atspi.press_and_settle(context.app, "Down" if current < target else "Up")
    raise AssertionError(f"keyboard focus never reached {what} in {presses} key presses")


def _texts(root):
    return [v for n in atspi.descendants(root, only_showing=True) for v in (atspi.name(n), atspi.text(n)) if v]


def _calls(context):
    try:
        with open(os.path.join(context.scenario_dir, CALLS), encoding="utf-8") as handle:
            return [line.rstrip("\n") for line in handle if line.strip()]
    except OSError:
        return []


def _home_files(context, *parts):
    base = os.path.join(context.home, *parts)
    found = []
    for directory, _, files in os.walk(base):
        found.extend(os.path.join(directory, f) for f in files)
    return found


# ---------------------------------------------------------------- sections


@then('the Livery "{section}" section is shown')
def step_section_shown(context, section):
    _section(context, section)


@then('the Livery "{section}" section is not shown')
def step_section_hidden(context, section):
    # The page is populated asynchronously; the panel section is hidden only
    # once the extension probe has answered, so wait for the other sections'
    # controls to become operable first.
    atspi.poll(
        lambda: any(
            atspi.sensitive(n)
            for n in atspi.find_all(_content(context), lambda n: atspi.role(n) == "switch")
        )
    )
    gone = atspi.poll(
        lambda: not atspi.find_all(
            _content(context), lambda n: atspi.role(n) == "grouping" and atspi.name(n) == section
        ),
        timeout=5,
    )
    assert gone, f"the Livery {section!r} section is still showing"


@then('the Livery page has finished loading')
def step_page_loaded(context):
    """refreshLiveryState has applied: the Files switch, which applyLiveryState
    always makes sensitive, is operable."""
    ok = atspi.poll(lambda: atspi.sensitive(_switch(context, "Customize the Files Icon", "Dock Livery")))
    assert ok, "the Livery page never finished loading its saved state"


# ---------------------------------------------------------------- switches and rows


@step('I toggle the "{row}" switch in the Livery "{section}" section')
def step_toggle(context, row, section):
    switch = _switch(context, row, section)
    assert atspi.poll(lambda: atspi.sensitive(switch)), f"the {row!r} switch in {section!r} is insensitive"
    atspi.activate(switch)


@then('the "{row}" switch in the Livery "{section}" section is {state:w}')
def step_switch_state(context, row, section, state):
    checks = {
        "on": lambda s: bool(atspi.checked(s)),
        "off": lambda s: not atspi.checked(s),
        "sensitive": atspi.sensitive,
        "insensitive": lambda s: not atspi.sensitive(s),
    }
    if state not in checks:
        raise NotImplementedError(f"unknown switch state {state!r}")
    ok = atspi.poll(lambda: checks[state](_switch(context, row, section)))
    assert ok, f"the {row!r} switch in {section!r} never became {state}"


@then('the "{row}" row in the Livery "{section}" section is {state:w}')
def step_row_state(context, row, section, state):
    if state not in ("sensitive", "insensitive"):
        raise NotImplementedError(f"unknown row state {state!r}")
    want = state == "sensitive"
    ok = atspi.poll(lambda: atspi.sensitive(_section_row(context, row, section)) == want)
    assert ok, f"the {row!r} row in {section!r} never became {state}"


@then('the "{row}" row in the Livery "{section}" section says "{text}"')
def step_row_says(context, row, section, text):
    ok = atspi.poll(lambda: text in _texts(_section_row(context, row, section)))
    assert ok, (
        f"the {row!r} row in {section!r} never said {text!r}; "
        f"it says {_texts(_section_row(context, row, section))}"
    )


@step('I activate the "{row}" row in the Livery "{section}" section')
def step_activate_row(context, row, section):
    target = _section_row(context, row, section)
    assert atspi.poll(lambda: atspi.sensitive(target)), f"the {row!r} row in {section!r} is insensitive"
    _keyboard_activate(context, lambda: _section(context, section), row, f"the {row!r} row in {section!r}")


# ---------------------------------------------------------------- chooser


@then('the Livery chooser titled "{title}" is shown')
def step_chooser_shown(context, title):
    ok = atspi.poll(lambda: title in _texts(_dialog(context, timeout=1)))
    assert ok, f"no Livery chooser titled {title!r} is showing"


@step('I search the Livery chooser for "{query}"')
def step_chooser_search(context, query):
    dialog = _dialog(context)
    entry = atspi.find(
        dialog, lambda n: atspi.role(n) in atspi.TEXT_ROLES, "the chooser's search field"
    )
    atspi.safe(lambda: entry.grabFocus())
    atspi.poll(lambda: atspi.focused(entry), timeout=5)
    atspi.type_text(query)
    # search-changed is debounced by GtkSearchEntry; wait for the text to land.
    assert atspi.poll(lambda: atspi.text(entry) == query), f"the search field never read {query!r}"


def _offered(context):
    return [_row_title(r) for r in _dialog_rows(_dialog(context, timeout=1))]


@then('the Livery chooser offers "{name}"')
def step_chooser_offers(context, name):
    ok = atspi.poll(lambda: name in _offered(context))
    assert ok, f"the chooser never offered {name!r}; it offers {_offered(context)}"


@then('the Livery chooser does not offer "{name}"')
def step_chooser_lacks(context, name):
    ok = atspi.poll(lambda: name not in _offered(context))
    assert ok, f"the chooser still offers {name!r}"


@then('the Livery chooser offers only "{name}"')
def step_chooser_only(context, name):
    ok = atspi.poll(lambda: _offered(context) == [name])
    assert ok, f"the chooser offers {_offered(context)}, want only {name!r}"


@then('the Livery chooser says "{text}"')
def step_chooser_says(context, text):
    ok = atspi.poll(lambda: any(text in v for v in _texts(_dialog(context, timeout=1))))
    assert ok, f"the chooser never said {text!r}"


@then('the Livery chooser does not say "{text}"')
def step_chooser_not_says(context, text):
    ok = atspi.poll(lambda: not any(text in v for v in _texts(_dialog(context, timeout=1))), timeout=3)
    assert ok, f"the chooser says {text!r}"


@step('I pick "{name}" in the Livery chooser')
def step_chooser_pick(context, name):
    def lookup():
        for row in _dialog_rows(_dialog(context, timeout=1)):
            if _row_title(row) == name:
                return row
        return None

    row = atspi.poll(lookup)
    assert row is not None, f"the chooser offers no {name!r}; it offers {_offered(context)}"
    _keyboard_activate(context, lambda: _dialog(context), name, f"the {name!r} result")


@then("the Livery chooser is closed")
def step_chooser_closed(context):
    def gone():
        return not [
            n for n in atspi.descendants(_app(context), only_showing=True)
            if atspi.role(n) == "dialog"
        ]

    assert atspi.poll(gone), "the Livery chooser is still showing"


@then("the Livery chooser is still open")
def step_chooser_open(context):
    _dialog(context, timeout=2)


# ---------------------------------------------------------------- toasts


@then('a Livery error toast says "{text}"')
def step_toast(context, text):
    ok = atspi.poll(
        lambda: any(text in v for v in _texts(atspi.main_window(_app(context)))),
        timeout=15,
    )
    assert ok, f"no toast said {text!r}"


# ---------------------------------------------------------------- dry-run evidence


def _would_set(schema, key, value):
    return f"[DRY-RUN] would set {schema} {key}={value}"


@then("the Livery dry run would set {key:S} to {value}")
def step_would_set(context, key, value):
    line = _would_set(LIVERY_SCHEMA, key, value)
    ok = atspi.poll(lambda: line in _log(context))
    assert ok, f"chairlift.log never contained {line!r}"


@then("the Livery dry run would not set {key:S}")
def step_would_not_set(context, key):
    prefix = f"[DRY-RUN] would set {LIVERY_SCHEMA} {key}="
    assert prefix not in _log(context), f"chairlift.log unexpectedly contains {prefix!r}"


@then('the Livery dry run would install the "{icon}" icon in the "{theme}" theme')
def step_would_install(context, icon, theme):
    # XDG_DATA_HOME is the scenario's, so the target must be inside HOME.
    # The context directory (apps, actions) is the surface's own business.
    theme_dir = os.path.join(context.launch_env["XDG_DATA_HOME"], "icons", theme)
    pattern = re.compile(
        r"\[DRY-RUN\] would install [1-9]\d* bytes at " + re.escape(theme_dir)
        + r"/scalable/[a-z]+/" + re.escape(icon) + r"\.svg and refresh the "
        + re.escape(theme) + r" icon cache"
    )
    ok = atspi.poll(lambda: pattern.search(_log(context)))
    assert ok, f"chairlift.log never announced installing {icon}.svg under {theme_dir}"


@then("the Livery dry run would install no icon")
def step_would_install_nothing(context):
    assert "[DRY-RUN] would install" not in _log(context), "the dry run announced an icon install"


@then('the Livery dry run would point the panel at "{icon}"')
def step_would_point_panel(context, icon):
    line = (
        f"[DRY-RUN] would set {EXTENSION_SCHEMA} menuicon-setting={icon}"
        " and menuoptions-setting=2"
    )
    ok = atspi.poll(lambda: line in _log(context))
    assert ok, f"chairlift.log never contained {line!r}"


@then("the Livery dry run would write and enable the rotation unit")
def step_would_write_unit(context):
    path = os.path.join(context.launch_env["XDG_CONFIG_HOME"], "systemd", "user", ROTATION_UNIT)
    line = f"[DRY-RUN] would write {path} and enable it"
    ok = atspi.poll(lambda: line in _log(context))
    assert ok, f"chairlift.log never contained {line!r}"


@then("the Livery page announced no dry-run change")
def step_no_dry_run(context):
    # Give a late notify-driven handler the chance to misfire before judging.
    atspi.poll(lambda: False, timeout=2)
    lines = [
        line for line in _log(context).splitlines()
        if "[DRY-RUN]" in line and (LIVERY_SCHEMA in line or "icon" in line or ROTATION_UNIT in line
                                    or EXTENSION_SCHEMA in line or "Kickoff" in line)
    ]
    assert not lines, f"loading the Livery page announced changes: {lines}"


@then("no Livery fetch was attempted")
def step_no_fetch(context):
    failures = [line for line in _log(context).splitlines() if re.search(r"livery: (fetching|setting)", line)]
    assert not failures, f"the Livery page fetched artwork: {failures}"


# ---------------------------------------------------------------- host side effects


@then("no Livery command changed any setting")
def step_no_mutating_calls(context):
    calls = _calls(context)
    bad = [c for c in calls if any(p.search(c) for p in MUTATING)]
    assert not bad, f"state-changing commands ran: {bad}"


@then('the Livery page ran "{command}"')
def step_ran(context, command):
    ok = atspi.poll(lambda: command in _calls(context))
    assert ok, f"{command!r} never ran; recorded calls: {_calls(context)}"


@then("no icon was written under the home directory")
def step_no_icons(context):
    written = _home_files(context, ".local", "share", "icons")
    assert not written, f"icon files were written: {written}"


@then("no rotation unit was written under the home directory")
def step_no_unit(context):
    path = os.path.join(context.launch_env["XDG_CONFIG_HOME"], "systemd", "user", ROTATION_UNIT)
    assert not os.path.exists(path), f"{path} was written"


@then("no profile picture was staged under the home directory")
def step_no_avatar(context):
    written = _home_files(context, ".cache", "chairlift")
    assert not written, f"profile picture files were written: {written}"


# ---------------------------------------------------------------- profile picture


@then('the Livery "{label}" button is {state:w}')
def step_dialog_button_state(context, label, state):
    if state not in ("sensitive", "insensitive"):
        raise NotImplementedError(f"unknown button state {state!r}")
    want = state == "sensitive"

    def check():
        buttons = [
            n for n in atspi.descendants(_dialog(context, timeout=1), only_showing=True)
            if atspi.role(n) in atspi.BUTTON_ROLES and atspi.label_text(n) == label
        ]
        return buttons and all(atspi.sensitive(b) == want for b in buttons)

    assert atspi.poll(check), f"the chooser's {label!r} button never became {state}"


@step('I press the "{label}" button in the Livery "{section}" section')
def step_press_section_button(context, label, section):
    button = atspi.find(
        _section(context, section),
        lambda n: atspi.is_button(n) and (atspi.name(n) == label or label in _texts(n)),
        f"a button showing {label!r} in {section!r}",
    )
    atspi.activate(button)


@then('the "{label}" button in the Livery "{section}" section is announced as "{announced}"')
def step_button_announced(context, label, section, announced):
    button = atspi.find(
        _section(context, section),
        lambda n: atspi.is_button(n) and label in _texts(n),
        f"a button showing {label!r} in {section!r}",
    )
    assert atspi.name(button) == announced, (
        f"the button showing {label!r} is announced as {atspi.name(button)!r}, want {announced!r}"
    )
