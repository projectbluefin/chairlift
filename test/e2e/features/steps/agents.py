"""Steps for the Agents page (features/agents.feature).

Agent Mode is unprivileged: the switch writes a systemd user unit and an
environment.d fragment. Under --dry-run neither file may be written and no
llmman, systemctl, or brew bundle mutation may run. Steps read the scenario
HOME and recording stubs to prove it.
"""

import os
import shlex
import subprocess

from behave import step, then

import chairlift_atspi as atspi
import stubs_agents
from stubs_features import CALLS_LOG as FEATURES_CALLS_LOG
from common import app, content, read_log

AGENT_MODE_ROW = "Agent Mode"


@step("I dismiss the model chooser with Escape")
def step_dismiss_model_chooser(context):
    atspi.press("Escape")


def agent_mode_switch(context):
    row = atspi.row_containing(content(context), AGENT_MODE_ROW)
    return atspi.find(row, lambda n: atspi.role(n) == "switch", "the Agent Mode switch")


def read_bytes(path):
    try:
        with open(path, "rb") as handle:
            return handle.read()
    except FileNotFoundError:
        return None


def calls(context, program):
    data = read_bytes(os.path.join(stubs_agents.calls_dir(context), program))
    return [] if data is None else data.decode("utf-8", "replace").splitlines()


def artifacts(context):
    return {
        "unit": stubs_agents.unit_path(context),
        "environment fragment": stubs_agents.fragment_path(context),
    }


def snapshot(context):
    return {label: read_bytes(path) for label, path in artifacts(context).items()}



# ---------------------------------------------------------------- state


@step("I note the Agents files on disk")
def step_note_files(context):
    """Remember every Agents artifact as it stands, to compare after an action."""
    context.agents_files = snapshot(context)


@then("the Agents files on disk are unchanged")
def step_files_unchanged(context):
    before = context.agents_files
    after = snapshot(context)
    changed = [label for label in before if before[label] != after[label]]
    assert not changed, f"dry-run changed {changed}: before={before} after={after}"


@then('the Agent Mode {artifact} does not exist')
def step_artifact_absent(context, artifact):
    path = artifacts(context)[artifact]
    assert not os.path.exists(path), f"{artifact} {path} was written"


@step("this machine registers with Hive")
def step_register_with_hive(context):
    """The user follows the Registration Guide outside ChairLift."""
    stubs_agents.write_file(
        stubs_agents.registration_path(context),
        "HIVE_HUB=https://example.com/api/contribute/ws\n",
    )


@then("the Agent Mode switch settles {state:w} and sensitive")
def step_switch_settles(context, state):
    """The switch is back in a known state and operable again after an action."""
    if state not in ("on", "off"):
        raise NotImplementedError(f"unknown switch state {state!r}")
    want = state == "on"

    def settled():
        switch = agent_mode_switch(context)
        return bool(atspi.checked(switch)) == want and atspi.sensitive(switch)

    assert atspi.poll(settled), (
        f"Agent Mode switch never settled {state} and sensitive "
        f"(checked={atspi.checked(agent_mode_switch(context))}, "
        f"sensitive={atspi.sensitive(agent_mode_switch(context))})"
    )

@then("the model chooser is {state:w}")
def step_model_chooser_state(context, state):
    if state not in ("sensitive", "insensitive"):
        raise NotImplementedError(f"unknown chooser state {state!r}")
    want = state == "sensitive"

    def settled():
        row = atspi.row_containing(content(context), "Recommended Presets", timeout=1)
        button = atspi.find_button(row, "Choose…", timeout=1)
        # Dakota reports local sensitivity, not effective GTK sensitivity:
        # any disabled widget ancestor makes this button unavailable.
        node = button
        while node is not None and atspi.role(node) != "application":
            if not atspi.sensitive(node):
                return not want
            node = node.parent
        return want

    assert atspi.poll(settled), f"the model chooser never became {state}"


@then('the application log shows Agent Mode would {verb:w} its unit and fragment')
def step_log_would(context, verb):
    unit = stubs_agents.unit_path(context)
    fragment = stubs_agents.fragment_path(context)
    if verb == "write":
        wanted = (
            "[DRY-RUN] would install llmmanorg/tap/llmman, run llmman serve --pull-only, "
            f"write {unit} and {fragment}, and start chairlift-llmman.service"
        )
    elif verb == "remove":
        wanted = f"[DRY-RUN] would stop chairlift-llmman.service and remove {unit} and {fragment}"
    else:
        raise NotImplementedError(f"unknown Agent Mode verb {verb!r}")
    ok = atspi.poll(lambda: wanted in read_log(context))
    assert ok, f"chairlift.log never contained {wanted!r}"


# ---------------------------------------------------------------- stubs


def asked(lines, args):
    """The recorded invocations whose leading words are exactly args.

    A plain prefix match let "tap" match ChairLift's read-only startup
    `brew tap-info --installed --json`, so a scenario failed or passed on
    whether that read had landed before the step ran.
    """
    words = args.split()
    return [line for line in lines if line.split()[: len(words)] == words]


@then('llmman was never asked to "{args}"')
def step_llmman_never(context, args):
    hits = asked(calls(context, "llmman"), args)
    assert not hits, f"llmman ran {hits}"


@then('brew was never asked to "{args}"')
def step_brew_never(context, args):
    hits = asked(calls(context, "brew"), args)
    assert not hits, f"brew ran {hits}"


@then("the systemctl tool was never asked to mutate")
def step_systemctl_no_mutations(context):
    # Developer Mode legitimately observes docker.service at startup. Permit
    # reads, but reject every service/environment mutation and unknown verb.
    read_commands = {"show", "is-active", "is-enabled", "status", "list-units", "list-unit-files"}
    mutations = {
        "daemon-reload", "enable", "disable", "restart", "stop", "start",
        "unset-environment", "mask", "unmask", "set-environment",
    }
    unexpected = []
    recorded = calls(context, "systemctl")
    feature_log = read_bytes(os.path.join(context.scenario_dir, FEATURES_CALLS_LOG))
    if feature_log is not None:
        recorded += [
            line.removeprefix("systemctl ")
            for line in feature_log.decode("utf-8", "replace").splitlines()
            if line.startswith("systemctl ")
        ]
    for line in recorded:
        command = next((arg for arg in shlex.split(line) if not arg.startswith("-")), "")
        if command in mutations or command not in read_commands:
            unexpected.append(line)
    assert not unexpected, f"systemctl ran mutations or unexpected commands: {unexpected}"


@then("the llmman node endpoint was probed")
def step_node_probed(context):
    ok = atspi.poll(lambda: "/llmman/node" in calls(context, "node-requests"))
    assert ok, "nothing requested /llmman/node from the node stub"


@then('dconf was never asked to "{args}"')
def step_dconf_never(context, args):
    hits = asked(calls(context, "dconf"), args)
    assert not hits, f"dconf ran {hits}"


@step('I run a second invocation with "{args}"')
def step_second_invocation(context, args):
    binary = getattr(context, "current_binary", context.app_binary)
    cmd = [binary, *shlex.split(args)]
    result = subprocess.run(cmd, env=context.launch_env, cwd=context.scenario_dir, timeout=10)
    assert result.returncode == 0, f"second invocation failed ({result.returncode}): {result}"


# ---------------------------------------------------------------- navigation


@then('the sidebar has no "{title}" row')
def step_sidebar_lacks(context, title):
    rows = atspi.poll(lambda: atspi.sidebar_rows(app(context)))
    titles = [atspi.label_text(r) for r in rows or []]
    assert titles, "the sidebar published no rows"
    assert title not in titles, f"sidebar lists {title!r}: {titles}"


# ---------------------------------------------------------------- troubleshooting

TROUBLESHOOT_GROUP = "Troubleshooting"


def _groups(context):
    return [
        atspi.name(node)
        for node in atspi.search_nodes(content(context))
        if atspi.role(node) == "grouping" and atspi.name(node)
    ]


def _dry_run_lines(context, marker):
    found = []
    for line in read_log(context).splitlines():
        at = line.find(marker)
        if at != -1:
            found.append(line[at + len(marker):])
    return found


@then("the Agents page groups are, in order")
def step_groups_in_order(context):
    # The window and page titles publish as groupings too; keep the named ones.
    want = [row["group"] for row in context.table]

    def got():
        return [name for name in _groups(context) if name in want]

    ok = atspi.poll(lambda: got() == want)
    assert ok, f"Agents page groups {_groups(context)} != {want}"


@then("the Agents page has no Troubleshooting group")
def step_troubleshoot_absent(context):
    # Settle on the page first: Agent Mode's row is built with it.
    atspi.row_containing(content(context), AGENT_MODE_ROW)
    assert TROUBLESHOOT_GROUP not in _groups(context), f"the {TROUBLESHOOT_GROUP!r} group is showing: {_groups(context)}"


@then("the Goose setup previewed exactly")
def step_goose_setup_previewed(context):
    """The Homebrew dry-run lines Set Up logged, in order, and no others."""
    want = [row["command"] for row in context.table]

    def previews():
        return [
            "brew " + line
            for line in _dry_run_lines(context, "[DRY-RUN] Would execute: brew ")
            if line.split(" ", 1)[0] in ("tap", "install")
        ]

    ok = atspi.poll(lambda: previews() == want)
    assert ok, f"setup previews {previews()} != {want}"


@then("the Goose session previewed its profile and launch")
def step_goose_session_previewed(context):
    root = os.path.join(stubs_agents.profile_root(context), "goose", "config")
    write = f"{os.path.join(root, 'config.yaml')} and {os.path.join(root, '.goosehints')}"
    launch = f"{os.path.join(context.stub_bin, 'llmman')} launch goose-desktop --model bluefin-active"

    def writes():
        return [line for line in _dry_run_lines(context, "[DRY-RUN] would write ") if "goose" in line]

    def launches():
        return [line for line in _dry_run_lines(context, "[DRY-RUN] would execute: ") if "goose" in line]

    ok = atspi.poll(lambda: writes() == [write] and launches() == [launch])
    assert ok, f"session previews: writes {writes()} != [{write!r}], launches {launches()} != [{launch!r}]"


@then("no Goose session was previewed")
def step_no_goose_session(context):
    launches = [line for line in _dry_run_lines(context, "[DRY-RUN] would execute: ") if "goose" in line]
    assert not launches, f"a session launch was previewed: {launches}"


@then("the Goose profile was not written")
def step_goose_profile_absent(context):
    root = stubs_agents.profile_root(context)
    assert not os.path.exists(root), f"{root} exists after a dry run"
