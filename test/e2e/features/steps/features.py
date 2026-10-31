"""Features destination steps: Developer Mode, Gaming Mode and Printers.

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
from stubs_features import (
    CALLS_LOG, GAMING_COMPONENTS, STEAM_PLATFORM_BRANCH, account_is_developer,
    docker_held_failure, docker_inactive, docker_no_manager,
)
from apps import expander_header, expander_rows, focus_by_tab
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


def _assert_parent_preview(context, count, child_action):
    actions = [child_action]
    if child_action == "docker-enable" and not _developer_initial(context):
        actions.insert(0, "dx-enable")
    entries = _journal(context)
    expected = actions * count
    assert [entry.get("action") for entry in entries] == expected, \
        f"expected ordered helper previews {expected}, got {entries}"
    for entry in entries:
        action = entry["action"]
        assert entry.get("suppressed") == "dry-run", f"helper was not a preview: {entry}"
        assert entry.get("would_run") == ["pkexec", UBLUE_HELPER, action, "--dry-run"], entry
        assert not entry.get("args") and not entry.get("outcome"), entry


def _wait_for_text(context, predicate, what):
    found = atspi.poll(lambda: next((t for t in _texts(context) if predicate(t)), None))
    assert found, f"{what} never appeared"
    return found


def _never(predicate, what, window=NEGATIVE_WINDOW):
    """Watch for window seconds and fail the moment predicate holds."""
    hit = atspi.poll(predicate, timeout=window)
    assert not hit, f"unexpectedly observed {what}: {hit!r}"


# ---------------------------------------------------------------- package disclosures


FEATURE_SWITCH_ROWS = ("Developer Mode", "WSL Mode", "Enable Docker", "Gaming Mode", "Compass")


def _mutation_calls(context):
    mutations = []
    for call in _calls(context):
        if not call:
            continue
        tool, args = call[0], call[1:]
        if tool == "systemctl" and args[:1] == ["--user"]:
            args = args[1:]
        if not args:
            continue
        if args[0] in {
            "install", "uninstall", "upgrade", "tap", "trust", "write", "reset",
            "create", "start", "stop", "shutdown", "remove", "autostart", "enable", "disable",
        } or (tool == "brew" and args[:2] in (["bundle", "install"], ["bundle", "dump"])):
            mutations.append(call)
    return mutations


def _disclosure_header(context, title):
    group = "Gaming" if title == "Gaming Mode" else "Developer"
    return expander_header(context, title, group)


def _disclosure_rows(context, title):
    non_packages = {"Virtual Machine Engine", "Developer access", "developer-feeds.opml", "Dakota dev flatpaks"}
    return [name for name in expander_rows(_disclosure_header(context, title)) if name not in non_packages]


@step("I record the Features switches and mutation evidence")
def step_record_feature_state(context):
    step_developer_restored(context)
    for title in FEATURE_SWITCH_ROWS:
        assert atspi.poll(lambda: atspi.sensitive(_switch(context, title))), \
            f"{title!r} never became ready before disclosure inspection"
    context.features_switch_snapshot = {
        title: bool(atspi.checked(_switch(context, title))) for title in FEATURE_SWITCH_ROWS
    }
    context.features_journal_snapshot = _journal(context)
    context.features_calls_snapshot = _mutation_calls(context)
    context.features_log_snapshot = len(_log(context))


@then("each Features switch is named and separately keyboard reachable")
def step_feature_switch_accessibility(context):
    for title in FEATURE_SWITCH_ROWS:
        switch = _switch(context, title)
        assert atspi.name(switch) == title, f"{title!r} switch has accessible name {atspi.name(switch)!r}"
        assert atspi.checked(switch) is not None, f"{title!r} switch publishes no state"
        assert atspi.actions(switch), f"{title!r} switch exposes no native action"
        focus_by_tab(context, switch, f"the separate {title!r} switch")


@step('I expand the "{title}" package disclosure with the keyboard')
def step_expand_packages(context, title):
    header = _disclosure_header(context, title)
    focus_by_tab(context, header, f"the {title!r} disclosure header")
    atspi.press("Return")
    assert atspi.poll(lambda: _disclosure_rows(context, title)), \
        f"{title!r} did not reveal its packages"


@then('the "{title}" package disclosure shows exactly')
def step_packages_exact(context, title):
    want = [row["package"] for row in context.table]
    assert atspi.poll(lambda: _disclosure_rows(context, title) == want), \
        f"{title!r} packages are {_disclosure_rows(context, title)}, want {want}"


@then("the developer feed disclosure shows its manual-import file location")
def step_feed_catalog_location(context):
    row = atspi.row_containing(_content(context), os.path.basename(OPML_PATH))
    expected = os.path.join(context.home, OPML_PATH)
    assert any(expected in text for text in atspi.all_text_under(row)), f"catalog location {expected!r} is not disclosed"


@then('the "{title}" package disclosure is informational')
def step_packages_informational(context, title):
    header = _disclosure_header(context, title)
    outer = header.parent.parent.parent
    for package in _disclosure_rows(context, title):
        row = atspi.row_containing(outer, package)
        controls = atspi.find_all(
            row, lambda node: atspi.role(node) in (*atspi.TOGGLE_ROLES, *atspi.BUTTON_ROLES)
        )
        assert not controls, f"package {package!r} offers individual mutation controls: {atspi.describe(controls)}"
        if title == "Enable Docker":
            assert "Provided by system image" in atspi.all_text_under(row), \
                f"{package!r} is not labelled as system-image provenance"


@then("expanding disclosures changes no Features switch and requests no mutation")
def step_disclosures_passive(context):
    def changed():
        current = {title: bool(atspi.checked(_switch(context, title))) for title in FEATURE_SWITCH_ROWS}
        if current != context.features_switch_snapshot:
            return current
        if _journal(context) != context.features_journal_snapshot:
            return _journal(context)
        if _mutation_calls(context) != context.features_calls_snapshot:
            return _mutation_calls(context)
        appended = _log(context)[context.features_log_snapshot:]
        return appended if "[DRY-RUN]" in appended else None

    _never(changed, "a mutation from package expansion")


@step('I choose the "{engine}" WSL engine')
def step_choose_wsl_engine(context, engine):
    choices = {"Built-in": "Home", "Lima": "End"}
    row = atspi.row_containing(_content(context), "Virtual Machine Engine")
    focus_by_tab(context, row, "the Virtual Machine Engine row")
    atspi.press("space")
    atspi.press(choices[engine])
    atspi.press("Return")


# ---------------------------------------------------------------- setup activity


@then("Setup activity is the first Features preference group")
def step_setup_first(context):
    activity = atspi.find(_content(context), lambda node: atspi.role(node) == "grouping"
                          and atspi.name(node) == "Setup activity", "the Setup activity group")
    groups = [atspi.name(node) for node in atspi.children(activity.parent)
              if atspi.role(node) == "grouping" and atspi.showing(node) and atspi.name(node)]
    assert groups and groups[0] == "Setup activity", f"Features group order is {groups}"


@then("Setup activity shows these stage statuses")
def step_setup_statuses(context):
    wanted = [(row["stage"], row["status"]) for row in context.table]

    def statuses_match():
        group = atspi.find(_content(context), lambda node: atspi.role(node) == "grouping"
                           and atspi.name(node) == "Setup activity", "Setup activity", timeout=1)
        for stage, status in wanted:
            row = atspi.row_containing(group, stage, timeout=1)
            if status not in atspi.all_text_under(row):
                return False
        return True

    assert atspi.poll(statuses_match), f"Setup activity did not show {wanted}"


@then("Setup activity has no running progress or fabricated percentage")
def step_setup_settled(context):
    def settled():
        group = atspi.find(_content(context), lambda node: atspi.role(node) == "grouping"
                           and atspi.name(node) == "Setup activity", "Setup activity", timeout=1)
        return not atspi.find_all(group, lambda node: atspi.role(node) == "progress bar")

    assert atspi.poll(settled), "Setup activity kept its running progress after the preview"
    group = _groups(context, "Setup activity")[0]
    assert not any("%" in text for text in atspi.all_text_under(group)), "Setup activity invented a percentage"


@then("Setup activity shows running progress without a percentage")
def step_setup_running(context):
    group = atspi.find(_content(context), lambda node: atspi.role(node) == "grouping"
                       and atspi.name(node) == "Setup activity", "Setup activity")
    atspi.find(group, lambda node: atspi.role(node) == "progress bar", "running setup progress")
    assert not any("%" in text for text in atspi.all_text_under(group)), "Setup activity invented a percentage"


@then("the Developer access activity matches this account's required promotion")
def step_implicit_access_activity(context):
    group = _groups(context, "Setup activity")[0]
    access = atspi.find_all(group, lambda node: atspi.role(node) in atspi.ROW_ROLES
                            and atspi.name(node) == "Developer access")
    if _developer_initial(context):
        assert not access, "an already enabled account was offered Developer access again"
    else:
        row = atspi.row_containing(group, "Developer access")
        assert "Preview" in atspi.all_text_under(row), "implicit access was not marked as a preview"


@then("Setup activity has no Developer access stage")
def step_no_access_stage(context):
    group = _groups(context, "Setup activity")[0]
    assert not atspi.find_all(group, lambda node: atspi.role(node) in atspi.ROW_ROLES
                             and atspi.name(node) == "Developer access"), \
        "stopping a child tool requested a Developer Mode change"


@then("the direct Developer activity matches the previewed action")
def step_direct_developer_activity(context):
    want = ["Developer access"]
    if not _developer_initial(context):
        want.append("Dev Container CLI")
    group = _groups(context, "Setup activity")[0]
    rows = atspi.find_all(group, lambda node: atspi.role(node) in atspi.ROW_ROLES
                         and atspi.name(node) and atspi.name(node) != "Setup progress")
    assert [atspi.name(row) for row in rows] == want, atspi.describe(rows)
    for row in rows:
        assert "Preview" in atspi.all_text_under(row), f"{atspi.name(row)!r} was not marked Preview"


@then("the Docker activity contains only this run's stages")
def step_docker_stage_inventory(context):
    want = ["Tools and service", "Verify readiness", "VS Code setup"]
    if not _developer_initial(context):
        want = ["Developer access", *want, "Dev Container CLI"]

    def titles():
        group = _groups(context, "Setup activity")[0]
        return [atspi.name(node) for node in atspi.descendants(group, only_showing=True)
                if atspi.role(node) in atspi.ROW_ROLES and atspi.name(node)
                and atspi.name(node) != "Setup progress"]

    assert atspi.poll(lambda: titles() == want), f"Setup activity rows are {titles()}, want {want}"



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


@then("the Dev Container CLI install is previewed only when enabling")
def step_devcontainer_preview(context):
    line = "[DRY-RUN] Would execute: brew install devcontainer"
    if _developer_initial(context):
        # Disabling must not install anything; give an install time to appear.
        assert not atspi.poll(lambda: line in _log(context), timeout=2), \
            "disabling Developer Mode previewed a Dev Container CLI install"
    else:
        assert atspi.poll(lambda: line in _log(context)), f"chairlift.log never contained {line!r}"
    try:
        with open(os.path.join(context.scenario_dir, "brew-default-calls.log"), encoding="utf-8") as handle:
            brew_calls = [line.split() for line in handle if line.strip()]
    except FileNotFoundError:
        brew_calls = []
    assert not [c for c in brew_calls if c[:1] == ["install"]], "a dry run executed brew install"


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


# ---------------------------------------------------------------- System Docker


@step("Docker's system manager becomes unavailable during an engine refresh")
def step_docker_manager_disappears(context):
    docker_no_manager(context)
    step_expand_packages(context, "WSL Mode")
    step_choose_wsl_engine(context, "Lima")


@then("system Docker {operation:w} is previewed {count:d} time")
@then("system Docker {operation:w} is previewed {count:d} times")
def step_docker_preview(context, operation, count):
    action = {"enable": "docker-enable", "disable": "docker-disable"}[operation]
    assert atspi.poll(lambda: len([entry for entry in _journal(context)
                                  if entry.get("action") == action]) == count), \
        f"expected {count} {action} previews: {_journal(context)}"
    _assert_parent_preview(context, count, action)
    output = _log(context)
    for retired in ("dockerd-rootless-setuptool.sh", "brew install docker", "brew install rootlesskit",
                    "brew install slirp4netns", "brew install lazydocker", "brew install dive"):
        assert retired not in output, f"system Docker used a retired download/setup path: {retired}"


@step("Docker's system-unit check is held before failing")
def step_hold_docker_check(context):
    docker_held_failure(context)
    context.docker_calls_before_hold = len(_calls(context))


@step("I release Docker's failed system-unit check")
def step_release_docker_check(context):
    calls = [call for call in _calls(context)[context.docker_calls_before_hold:]
             if call[:2] == ["systemctl", "show"]]
    expected = [["systemctl", "show", unit, "--property=LoadState,ActiveState"]
                for unit in ("docker.service", "docker.socket", "docker.service")]
    assert calls == expected, f"held read was not Verify after both prerequisites: {calls}"
    with open(os.path.join(context.scenario_dir, "release-docker-status"), "w", encoding="utf-8"):
        pass


@step("Docker's system-unit check is available again")
def step_restore_docker_check(context):
    docker_inactive(context)


@then("VS Code setup is previewed without editing user settings")
def step_vscode_preview(context):
    code = os.path.join(context.stub_bin, "code")
    extensions = ("ms-vscode-remote.remote-containers", "ms-vscode-remote.remote-ssh", "ms-azuretools.vscode-containers")
    for extension in extensions:
        line = f"[DRY-RUN] would execute: {code} [--install-extension {extension}]"
        assert atspi.poll(lambda: line in _log(context)), f"missing editor preview: {line}"
    assert not [call for call in _calls(context) if call[0] == "code"], "preview executed the native editor CLI"
    settings = os.path.join(context.home, ".config", "Code", "User", "settings.json")
    if hasattr(context, "vscode_original"):
        with open(settings, "rb") as handle:
            assert handle.read() == context.vscode_original, "preview modified existing JSONC"
    else:
        assert not os.path.lexists(settings), "preview created native editor settings"
    assert not os.path.lexists(context.launch_env["SETUP_CHECKER_FILE"]), "preview recorded successful editor setup"


@then("the editor preview failure reports that no Docker changes were made")
def step_vscode_preview_failure(context):
    feedback = _wait_for_text(context, lambda text: text.startswith("[DRY-RUN] VS Code setup needs attention;")
                              and "no Docker changes made" in text, "truthful editor preview failure")
    assert len(feedback) < 180, "editor preview feedback is not a short message"
    for diagnostic in ("read VS Code setup version:", "invalid character", "not JSON", "setup_versioning.json"):
        assert not any(diagnostic in text for text in _texts(context)), "raw editor diagnostic leaked into the UI"
    assert not any("Docker access is enabled" in text for text in _texts(context)), "preview claimed Docker access changed"
    assert not [call for call in _calls(context) if call[0] == "code"], "failed preview executed native code"
    settings = os.path.join(context.home, ".config", "Code", "User", "settings.json")
    assert not os.path.lexists(settings), "failed preview created editor settings"
    with open(context.launch_env["SETUP_CHECKER_FILE"], encoding="utf-8") as handle:
        assert handle.read() == "not JSON", "failed preview rewrote setup state"


@then("Developer onboarding is previewed once per implicit promotion")
def step_implicit_devcontainer_preview(context):
    expected = 0 if _developer_initial(context) else 2
    marker = "[DRY-RUN] Would execute: brew install devcontainer"
    assert sum(line.endswith(marker) for line in _log(context).splitlines()) == expected, \
        "implicit promotion repeated or skipped its Dev Container CLI follow-through"


# ---------------------------------------------------------------- fakes


@then('the fake {tool} was never asked to "{subcommand}"')
def step_fake_never(context, tool, subcommand):
    calls = [c for c in _calls(context) if len(c) > 1 and c[0] == tool and c[1] == subcommand]
    assert not calls, f"{tool} received {subcommand!r}: {calls}"


@then("the Features CLI fixtures received no mutation commands")
def step_feature_tools_passive(context):
    mutations = _mutation_calls(context)
    assert not mutations, f"a preview executed mutation commands: {mutations}"


# ---------------------------------------------------------------- Gaming


@then('the gaming preview {operation:w} all components {preposition:w} the system scope')
def step_gaming_preview_all(context, operation, preposition):
    verb, want_preposition = {"installs": ("install", "into"), "removes": ("uninstall", "from")}[operation]
    if preposition != want_preposition:
        raise NotImplementedError(f"{operation} pairs with {want_preposition!r}, not {preposition!r}")
    for _, app_id, kind in GAMING_COMPONENTS:
        ref = app_id
        if kind == "runtime":
            ref = f"{app_id}//{STEAM_PLATFORM_BRANCH}"
        expected = f"[DRY-RUN] Would execute: flatpak {verb} -y --system {ref}"
        assert atspi.poll(lambda: expected in _log(context)), f"{app_id} was not previewed: {expected!r}"


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

# pageview.PrinterAppSubtitle for printerapp.StateOff.
PRINTER_OFF_SUBTITLE = "Turn on to use these printers from this computer."


def _printer_rows(context):
    """Every list row inside the Printers group, in display order."""
    groups = _groups(context, PRINTERS_GROUP)
    assert groups, f"no {PRINTERS_GROUP!r} group on the Features page"
    return atspi.find_all(groups[0], lambda n: atspi.role(n) in atspi.ROW_ROLES)


@then("the Printers group offers exactly these rows, each off and ready to turn on")
def step_printer_rows_off(context):
    want = [row["row"] for row in context.table]
    assert atspi.poll(lambda: _groups(context, PRINTERS_GROUP)), f"no {PRINTERS_GROUP!r} group on the Features page"
    group = _groups(context, PRINTERS_GROUP)[0]
    for title in want:
        row = atspi.row_containing(group, title)
        switch = atspi.find(row, lambda n: atspi.role(n) == "switch", f"a switch in the {title!r} row")
        # Shown, off, and offered — never a false enabled indicator.
        assert atspi.checked(switch) is False, f"{title!r} switch is not off"
        assert atspi.sensitive(switch), f"{title!r} switch does not accept input"
        texts = atspi.all_text_under(row)
        assert PRINTER_OFF_SUBTITLE in texts, f"{title!r} row does not say what turning it on does: {texts}"
    got = [atspi.label_text(r) for r in _printer_rows(context)]
    assert len(got) == len(want), f"Printers group rows {got} != {want}"


@then("no printer quadlet was written")
def step_no_quadlet(context):
    directory = quadlet_dir(context)
    written = sorted(os.listdir(directory)) if os.path.isdir(directory) else []
    assert not written, f"quadlets were written under {directory}: {written}"
