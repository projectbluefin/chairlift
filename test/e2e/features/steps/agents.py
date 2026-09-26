"""Steps for the Agents page (features/agents.feature).

Agent Mode is unprivileged: the switch writes a systemd *user* unit and an
environment.d fragment, and "Use another machine" keeps ChairLift's own peer
store beside llmman's configuration. Under --dry-run none of those files may
be written and none of llmman, systemctl, or `brew bundle` may run; the
steps here read the scenario HOME and the recording stubs in
fixtures/stubs_agents.py to prove it.
"""

import os

from behave import step, then

import chairlift_atspi as atspi
import stubs_agents
from common import app, content, current_dialog, read_log, text_present

AGENT_MODE_ROW = "Run AI models on this computer"


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
        "peer store": stubs_agents.peer_store_path(context),
    }


def snapshot(context):
    return {label: read_bytes(path) for label, path in artifacts(context).items()}


def peer_row(context, address):
    return atspi.row_containing(content(context), address)


def focus_by_tab(context, root, predicate, what, limit=80):
    """Move keyboard focus with Tab until a node under root matching predicate has it.

    GTK 4's AT-SPI bridge does not implement Component.GrabFocus, so focus can
    only be moved the way a keyboard user moves it.
    """
    def focused_match():
        return atspi.find_all(root, lambda n: predicate(n) and atspi.focused(n))

    for _ in range(limit):
        found = atspi.poll(focused_match, timeout=0.3, interval=0.05)
        if found:
            return found[0]
        atspi.press("Tab")
    raise AssertionError(f"Tab never moved keyboard focus to {what}")


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


@then('the application log shows the peer key would be set without revealing "{key}"')
def step_log_key(context, key):
    llmman = os.path.join(context.stub_bin, "llmman")
    wanted = f"[DRY-RUN] would set llmman aggregation.api_key via {llmman} config set"
    ok = atspi.poll(lambda: wanted in read_log(context))
    assert ok, f"chairlift.log never contained {wanted!r}"
    assert key not in read_log(context), "chairlift.log contains the peer key"


# ---------------------------------------------------------------- stubs


@then('llmman was never asked to "{args}"')
def step_llmman_never(context, args):
    hits = [line for line in calls(context, "llmman") if line.startswith(args)]
    assert not hits, f"llmman ran {hits}"


@then('brew was never asked to "{args}"')
def step_brew_never(context, args):
    hits = [line for line in calls(context, "brew") if line.startswith(args)]
    assert not hits, f"brew ran {hits}"


@then("systemctl was never run")
def step_systemctl_never(context):
    ran = calls(context, "systemctl")
    assert not ran, f"systemctl ran {ran}"


@then("the llmman node endpoint was probed")
def step_node_probed(context):
    ok = atspi.poll(lambda: "/llmman/node" in calls(context, "node-requests"))
    assert ok, "nothing requested /llmman/node from the node stub"


# ---------------------------------------------------------------- peers


@step("I open the Agents add-peer dialog")
def step_open_add_peer(context):
    """Tab to the "Add a peer…" row and press Return, as a keyboard user does.

    AdwActionRow publishes no AT-SPI action and GTK 4 on X11 publishes no
    coordinates, so the row can only be activated from the keyboard.
    """
    focus_by_tab(
        context,
        content(context),
        lambda n: atspi.role(n) in atspi.ROW_ROLES and atspi.name(n) == "Add a peer…",
        "the Add a peer… row",
    )
    atspi.press("Return")


@step('I enter "{value}" as the Agents peer address')
def step_enter_peer(context, value):
    dialog = current_dialog(context)
    entry = focus_by_tab(
        context, dialog, lambda n: atspi.role(n) in atspi.TEXT_ROLES, "the peer address entry"
    )
    atspi.type_text(value)
    assert atspi.poll(lambda: atspi.text(entry) == value), (
        f"peer address entry reads {atspi.text(entry)!r}, not {value!r}"
    )


@step('I enter "{value}" as the Agents peer key and press Return')
def step_enter_key(context, value):
    focus_by_tab(
        context, content(context), lambda n: atspi.role(n) == "password text", "the peer key entry"
    )
    # A password entry does not publish its text, so the typed key cannot be
    # read back; the log step that follows proves whether it arrived.
    atspi.type_text(value)
    atspi.press("Return")


@step('I press the remove button in the Agents peer "{address}" row')
def step_remove_peer(context, address):
    """The row's only push button is its trash icon (published nameless, #355)."""
    row = peer_row(context, address)
    button = atspi.find(
        row,
        lambda n: atspi.role(n) in atspi.BUTTON_ROLES and "click" in atspi.actions(n),
        f"a remove button in the {address!r} row",
    )
    atspi.activate(button)


@then('the Agents peer "{address}" switch is {state:w}')
def step_peer_switch(context, address, state):
    if state not in ("on", "off"):
        raise NotImplementedError(f"unknown switch state {state!r}")
    want = state == "on"

    def check():
        row = peer_row(context, address)
        switch = atspi.find(row, lambda n: atspi.role(n) == "switch", "a peer switch", timeout=1)
        return bool(atspi.checked(switch)) == want and atspi.sensitive(switch)

    assert atspi.poll(check), f"peer {address!r} switch never settled {state}"


@then('the Agents page lists no peer "{address}"')
def step_no_peer_row(context, address):
    def gone():
        return not text_present(content(context), address, exact=True)

    assert atspi.poll(gone), f"a peer row {address!r} is showing"


# ---------------------------------------------------------------- navigation


@then('the sidebar has no "{title}" row')
def step_sidebar_lacks(context, title):
    rows = atspi.poll(lambda: atspi.sidebar_rows(app(context)))
    titles = [atspi.label_text(r) for r in rows or []]
    assert titles, "the sidebar published no rows"
    assert title not in titles, f"sidebar lists {title!r}: {titles}"
