#!/usr/bin/env python3
"""Exercise Agent Mode's model preset flow through the AT-SPI tree.

The probe reports what it observes as tab-separated records. Assertions live
in agent_presets_atspi_test.go, alongside the model-family contract.
"""

import sys
import time

try:
    from dogtail.config import config

    config.checkForA11y = False
    from dogtail import rawinput, tree
except ImportError as error:  # pragma: no cover - Go test reports this prerequisite
    print(f"dogtail is not importable: {error}", file=sys.stderr)
    sys.exit(3)

APPLICATION_NAMES = ("chairlift", "Control Center", "io.projectbluefin.chairlift")


def descendants(node):
    """Yield a node and its accessible descendants, tolerating stale proxies."""
    yield node
    try:
        children = node.children
    except Exception:
        return
    for child in children:
        yield from descendants(child)


def name_of(node):
    try:
        return node.name or ""
    except Exception:
        return ""


def role_of(node):
    try:
        return node.roleName or ""
    except Exception:
        return ""


def named_node(root, name, roles=None):
    for node in descendants(root):
        if name_of(node) != name:
            continue
        if roles and role_of(node) not in roles:
            continue
        return node
    return None


LIST_ROLES = ("list box", "list")
ROW_ROLES = ("list item", "table cell")


def is_sidebar(node):
    try:
        if role_of(node) not in LIST_ROLES:
            return False
        return any(role_of(child) in ROW_ROLES for child in node.children)
    except Exception:
        return False


def find_sidebar_rows(app):
    for node in descendants(app):
        if is_sidebar(node):
            return [child for child in node.children if role_of(child) in ROW_ROLES]
    return []

def find_application():
    for name in APPLICATION_NAMES:
        try:
            return tree.root.application(name)
        except Exception:
            pass
    try:
        published = [name_of(child) for child in tree.root.children]
    except Exception as error:
        raise RuntimeError(f"cannot enumerate accessibility bus: {error}") from error
    raise RuntimeError(
        f"ChairLift is absent from AT-SPI (tried {APPLICATION_NAMES}; bus publishes {published})"
    )


def wait_for(root, name, timeout, roles=None):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        node = named_node(root, name, roles)
        if node is not None:
            return node
        time.sleep(0.25)
    raise RuntimeError(f"timed out waiting for accessible element {name!r}")


def wait_for_selected(node, timeout=10):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if bool(getattr(node, "selected", False)):
            return True
        time.sleep(0.25)
    return bool(getattr(node, "selected", False))


def wait_for_name_containing(root, marker, timeout):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        for node in descendants(root):
            name = name_of(node)
            if marker in name:
                return name
        time.sleep(0.25)
    raise RuntimeError(f"timed out waiting for accessible name containing {marker!r}")


def activate(node, app=None):
    """Invoke the AT-SPI action, falling back to dogtail's node activation.

    GTK 4 sidebar rows (`list item`) do not expose a click action over AT-SPI.
    When activating a sidebar row, find its index among the sidebar's list items
    and send `<Alt>{index + 1}` via rawinput, or select it via dogtail.
    """
    if role_of(node) in ROW_ROLES and app is not None:
        rows = find_sidebar_rows(app)
        if node in rows:
            index = rows.index(node)
            rawinput.keyCombo(f"<Alt>{index + 1}")
            return
    action = getattr(node, "doActionNamed", None)
    if action is not None:
        action("click")
        return
    select = getattr(node, "select", None)
    if select is not None:
        select()
        return
    click = getattr(node, "click", None)
    if click is None:
        raise RuntimeError(f"{name_of(node)!r} has no accessible click action")
    click()


def emit(kind, **fields):
    print("\t".join([kind] + [f"{key}={value}" for key, value in fields.items()]), flush=True)


def main():
    config.searchBackoffDuration = 0.25
    config.searchCutoffCount = 20
    config.actionDelay = 0.2
    config.defaultDelay = 0.2
    config.typingDelay = 0.05
    config.logDebugToFile = False
    config.logDebugToStdOut = False

    app = find_application()

    # Navigate by the accessible sidebar item so the scenario does not carry
    # a second page-order inventory or depend on a physical keyboard layout.
    agents = wait_for(app, "Agents", 30)
    activate(agents, app=app)
    selected = wait_for_selected(agents, 10)
    emit("PAGE", name="Agents", selected=int(selected))
    mode = wait_for(app, "Agent Mode", 10)
    emit("CONTROL", name="Agent Mode", role=role_of(mode))
    readiness = wait_for_name_containing(app, "Ready.", 30)
    emit("STATUS", name=readiness)
    for title in ("Active Model", "Recommended Presets"):
        row = wait_for(app, title, 30)
        emit("PRESET", name=title, role=role_of(row))

    switch = wait_for(app, "Switch…", 10)
    emit("BUTTON", name="Switch…", role=role_of(switch))
    activate(switch)

    dialog = wait_for(tree.root, "Switch Model Preset", 10)
    emit("DIALOG", name="Switch Model Preset", role=role_of(dialog))
    families = (
        "Qwen (Recommended)",
        "Mistral / Ministral",
        "Gemma",
        "DeepSeek",
        "GPT-OSS",
    )
    choice_nodes = {}
    for family in families:
        choice = wait_for(app, family, 5, roles=("push button", "button"))
        choice_nodes[family] = choice
        emit("CHOICE", name=family, role=role_of(choice))

    activate(choice_nodes["Gemma"])
    emit("SELECTED", family="Gemma")

    # Model resolution is live-first with a bundled fallback. Wait for the
    # dry-run decision marker instead of assuming either network path's timing.
    if len(sys.argv) != 2:
        raise RuntimeError("usage: agent_presets_atspi_probe.py <chairlift-log>")
    log_path = sys.argv[1]
    deadline = time.monotonic() + 60
    marker = "would configure alias bluefin-active to unsloth/gemma-3"
    while time.monotonic() < deadline:
        try:
            with open(log_path, encoding="utf-8") as log_file:
                if marker in log_file.read():
                    active_model = wait_for(app, "Active Model", 5)
                    model_name = wait_for_name_containing(
                        app, "unsloth/gemma-3", 10
                    )
                    emit("MODEL", name=model_name)
                    emit("ACTIVATED", family="Gemma")
                    emit("DONE")
                    return 0
        except FileNotFoundError:
            pass
        time.sleep(0.25)
    raise RuntimeError(f"Gemma dry-run activation was not observed in {log_path}")


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(f"Agent Mode AT-SPI probe failed: {error}", file=sys.stderr)
        sys.exit(1)
