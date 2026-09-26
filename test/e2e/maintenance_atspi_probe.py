#!/usr/bin/env python3
"""Exercise Maintenance page cleanup and recovery confirmation dialogs via AT-SPI.

The probe reports what it observes as tab-separated records. Assertions live
in maintenance_atspi_test.go, alongside pageview contracts.
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

from atspi_probe import (
    APPLICATION_NAMES,
    accessible_name,
    descendants,
    find_application,
    find_sidebar_rows,
    is_selected,
)


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

def is_sensitive(node):
    try:
        states = getattr(node, "states", [])
        if states:
            from dogtail.atspi import STATE_SENSITIVE
            return STATE_SENSITIVE in states
    except Exception:
        pass
    try:
        return bool(node.sensitive)
    except Exception:
        pass
    return False


def named_node(root, name, roles=None):
    for node in descendants(root):
        if name_of(node) != name:
            continue
        if roles and role_of(node) not in roles:
            continue
        return node
    return None


def find_button_in(root, name):
    for node in descendants(root):
        if name_of(node) == name and role_of(node) in ("push button", "button"):
            return node
    return None


def wait_for(root, name, timeout, roles=None):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        node = named_node(root, name, roles)
        if node is not None:
            return node
        time.sleep(0.25)
    raise RuntimeError(f"timed out waiting for accessible element {name!r}")


def wait_for_condition(predicate, timeout, desc="condition"):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            if predicate():
                return True
        except Exception:
            pass
        time.sleep(0.25)
    raise RuntimeError(f"timed out waiting for {desc}")


def find_dialog(root, title=None):
    for node in descendants(root):
        if role_of(node) in ("dialog", "alert", "window"):
            if title is None or name_of(node) == title:
                return node
        if role_of(node) in ("dialog", "alert"):
            if title is not None:
                for child in descendants(node):
                    if name_of(child) == title:
                        return node
    return None


def wait_for_dialog(root, title, timeout):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        d = find_dialog(root, title)
        if d is not None:
            return d
        time.sleep(0.25)
    raise RuntimeError(f"timed out waiting for dialog {title!r}")


def wait_for_dialog_dismissal(title, timeout):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if find_dialog(tree.root, title) is None:
            return True
        time.sleep(0.25)
    raise RuntimeError(f"timed out waiting for dialog {title!r} to dismiss")


def dialog_body(dialog):
    texts = []
    excluded = {
        "Cancel",
        "Remove Everything",
        "Factory Reset",
        name_of(dialog),
    }
    for node in descendants(dialog):
        name = name_of(node)
        if name and name not in excluded and role_of(node) in ("label", "static", "heading", "text"):
            texts.append(name)
    # Join with space and escape tab/newlines so it is safe in tab-separated records
    body = " ".join(texts)
    return body.replace("\t", " ").replace("\n", " ").strip()


def activate(node):
    """Invoke the AT-SPI action, falling back to dogtail's node activation."""
    action = getattr(node, "doActionNamed", None)
    if action is not None:
        for act in ("click", "activate", "press"):
            try:
                action(act)
                return
            except Exception:
                pass
    click = getattr(node, "click", None)
    if click is not None:
        try:
            click()
            return
        except Exception:
            pass
    raise RuntimeError(f"{name_of(node)!r} could not be activated")


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

    # 1. Navigate to Maintenance destination
    # Find Maintenance sidebar row and wait for it to be selected
    sidebar_rows = find_sidebar_rows(app)
    maint_row = None
    maint_index = -1
    for idx, r in enumerate(sidebar_rows):
        if accessible_name(r) == "Maintenance":
            maint_row = r
            maint_index = idx
            break

    if maint_row is not None:
        rawinput.keyCombo(f"<Alt>{maint_index + 1}")
    else:
        maintenance = wait_for(app, "Maintenance", 30)
        activate(maintenance)

    # Poll row selection state instead of time.sleep(0.5)
    wait_for_condition(
        lambda: any(accessible_name(r) == "Maintenance" and is_selected(r) for r in find_sidebar_rows(app)),
        10,
        desc="Maintenance sidebar row selection",
    )
    emit("PAGE", name="Maintenance", selected=1)

    # 2. Storage Clean Up action button interaction
    cleanup_btn = wait_for(app, "Clean up", 15, roles=("push button", "button"))
    emit("BUTTON", name="Clean up", role=role_of(cleanup_btn), sensitive=int(is_sensitive(cleanup_btn)))
    activate(cleanup_btn)

    # Wait for cleanupview.BusyLabel ("Cleaning up…") or insensitive state
    wait_for_condition(
        lambda: not is_sensitive(cleanup_btn) or "Cleaning up" in name_of(cleanup_btn),
        10,
        desc="Clean up busy state",
    )
    emit("STATE", name="Clean up", status="busy")

    # Wait for completion: button becomes sensitive again and label returns to "Clean up"
    wait_for_condition(
        lambda: is_sensitive(cleanup_btn) and name_of(cleanup_btn) == "Clean up",
        30,
        desc="Clean up completion",
    )
    emit("STATE", name="Clean up", status="completed", sensitive=1)

    # 3. Powerwash confirmation flow
    # The button starts as "Remove…" in the Powerwash row
    powerwash_btn = wait_for(app, "Remove…", 15, roles=("push button", "button"))
    emit("BUTTON", name="Remove…", role=role_of(powerwash_btn))

    activate(powerwash_btn)
    # PowerwashConfirmation title is "Remove Everything I Installed?"
    pw_dialog = wait_for_dialog(tree.root, "Remove Everything I Installed?", 15)
    pw_observed_title = name_of(pw_dialog)
    pw_observed_body = dialog_body(pw_dialog)
    pw_cancel = find_button_in(pw_dialog, "Cancel")
    pw_confirm = find_button_in(pw_dialog, "Remove Everything")
    emit(
        "DIALOG",
        type="powerwash",
        title=pw_observed_title,
        body=pw_observed_body,
        has_cancel=int(pw_cancel is not None),
        has_confirm=int(pw_confirm is not None),
    )

    # Cancel dismisses dialog without execution
    activate(pw_cancel)
    wait_for_dialog_dismissal("Remove Everything I Installed?", 10)
    emit("DIALOG_CANCELLED", type="powerwash", dismissed=1)

    # Re-click to confirm
    activate(powerwash_btn)
    pw_dialog = wait_for_dialog(tree.root, "Remove Everything I Installed?", 15)
    pw_confirm = find_button_in(pw_dialog, "Remove Everything")
    activate(pw_confirm)
    wait_for_dialog_dismissal("Remove Everything I Installed?", 10)

    # Wait for Powerwash busy state: insensitive or "Removing…"
    wait_for_condition(
        lambda: not is_sensitive(powerwash_btn) or "Removing" in name_of(powerwash_btn),
        10,
        desc="Powerwash busy state",
    )
    # Wait for Powerwash completion: insensitive -> sensitive, label restored to "Remove…"
    wait_for_condition(
        lambda: is_sensitive(powerwash_btn) and name_of(powerwash_btn) == "Remove…",
        30,
        desc="Powerwash completion",
    )
    emit("DIALOG_CONFIRMED", type="powerwash", status="completed")

    # 4. Factory Reset confirmation flow
    # The button starts as "Reset…" in the Factory Reset row
    reset_btn = wait_for(app, "Reset…", 15, roles=("push button", "button"))
    emit("BUTTON", name="Reset…", role=role_of(reset_btn))

    activate(reset_btn)
    # FactoryResetConfirmation title is "Factory Reset This System?"
    fr_dialog = wait_for_dialog(tree.root, "Factory Reset This System?", 15)
    fr_observed_title = name_of(fr_dialog)
    fr_observed_body = dialog_body(fr_dialog)
    fr_cancel = find_button_in(fr_dialog, "Cancel")
    fr_confirm = find_button_in(fr_dialog, "Factory Reset")
    emit(
        "DIALOG",
        type="factory_reset",
        title=fr_observed_title,
        body=fr_observed_body,
        has_cancel=int(fr_cancel is not None),
        has_confirm=int(fr_confirm is not None),
    )

    # Cancel dismisses dialog without execution
    activate(fr_cancel)
    wait_for_dialog_dismissal("Factory Reset This System?", 10)
    emit("DIALOG_CANCELLED", type="factory_reset", dismissed=1)

    # Re-click to confirm
    activate(reset_btn)
    fr_dialog = wait_for_dialog(tree.root, "Factory Reset This System?", 15)
    fr_confirm = find_button_in(fr_dialog, "Factory Reset")
    activate(fr_confirm)
    wait_for_dialog_dismissal("Factory Reset This System?", 10)

    # Wait for Factory Reset busy state: insensitive or "Resetting…"
    wait_for_condition(
        lambda: not is_sensitive(reset_btn) or "Resetting" in name_of(reset_btn),
        10,
        desc="Factory Reset busy state",
    )
    # Wait for completion: sensitive, label restored to "Reset…"
    wait_for_condition(
        lambda: is_sensitive(reset_btn) and name_of(reset_btn) == "Reset…",
        30,
        desc="Factory Reset completion",
    )
    emit("DIALOG_CONFIRMED", type="factory_reset", status="completed")

    emit("DONE")
    return 0


if __name__ == "__main__":
    sys.exit(main())
