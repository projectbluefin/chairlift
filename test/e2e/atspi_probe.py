#!/usr/bin/env python3
"""Read ChairLift's navigation, control, and dialog state out of AT-SPI.

Usage: atspi_probe.py <page-name> <page-title> [<page-name> <page-title>...]

The name/title pairs arrive in sidebar order from atspi_navigation_test.go,
which reads both from internal/navigation. Page N is reached with the Alt+N
accelerator navigation itself advertises, so this script never carries its
own copy of the page inventory and cannot drift from the application's. The
title is passed in rather than read back from the selected row so that a
wrong selection cannot quietly redirect the content-area check at whatever
page was actually shown.

Everything the probe observes is written to stdout as tab-separated records
that the Go side parses and judges. The probe deliberately makes no
assertions of its own: keeping the expectations in Go keeps one source of
truth for what a correct transition is, and keeps the comparison in the
language that already owns navigation.Resolve.

Records emitted:

    ROW     index=<n>       name=<accessible name>  selected=<0|1>
    PAGE    page=<name>     selected_index=<n>      selected_name=<name>
            selected_count=<n>      content_title_labels=<n>
    CONTROLS page=<name>    control_count=<n>       nameless_count=<n>
            inoperable_count=<n>    toggle_count=<n>        unreadable_toggle_count=<n>
    MENU_BUTTON name=<name> role=<role>             showing=<0|1>
    POPOVER item_count=<n>
    DIALOG_SHORTCUTS name=<name> focused=<0|1>      has_updates=<0|1> has_quit=<0|1>
    DIALOG_ABOUT name=<name> focused=<0|1>          announces_app=<0|1>
    DONE

A probe that cannot get far enough to emit records exits non-zero with a
diagnosis on stderr.

Modeled on the dogtail usage in projectbluefin/testsuite: an application
looked up by name on the accessibility bus, keyboard-only interaction
through dogtail.rawinput, and state read from the tree rather than from
pixels.
"""

import sys
import time

try:
    # dogtail.tree runs checkForA11y() at import time, which reads
    # org.gnome.desktop.interface toolkit-accessibility through GSettings.
    # The runner uses GSETTINGS_BACKEND=memory, so that key is always false,
    # and dogtail would print its complaint to stdout and exit 1 before the
    # probe starts. The application publishes its tree through GTK_A11Y=atspi
    # regardless of that key, so the check has to be switched off first.
    from dogtail.config import config

    config.checkForA11y = False
    from dogtail import rawinput, tree
except ImportError as error:  # pragma: no cover - reported to the Go side
    print(f"dogtail is not importable: {error}", file=sys.stderr)
    sys.exit(3)

# The names GTK can publish for this application on the accessibility bus.
# g_get_application_name() defaults to the program name, but a build that
# sets the product name or the application ID would publish that instead, so
# all three are tried before giving up.
APPLICATION_NAMES = ("chairlift", "Control Center", "io.projectbluefin.chairlift")

# Roles GtkListBox and its rows take on the accessibility bus. GTK 4 maps
# GtkListBox to "list box" and each row to "list item"; older bridges have
# been seen to report plain "list" and "table cell", so both are accepted
# rather than making the probe's correctness depend on the bridge version.
LIST_ROLES = ("list box", "list")
ROW_ROLES = ("list item", "table cell")
LABEL_ROLES = ("label", "heading", "static")
ACTION_ROLES = ("push button", "toggle button", "menu button", "button")
TOGGLE_ROLES = ("toggle button", "switch", "check box")


def poll(predicate, timeout=5.0, interval=0.1):
    """Poll predicate until it returns truthy or timeout expires."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        res = predicate()
        if res:
            return res
        time.sleep(interval)
    return predicate()


def find_application():
    """Return the ChairLift application node, or exit with a diagnosis."""
    seen = []
    for name in APPLICATION_NAMES:
        try:
            return tree.root.application(name)
        except Exception:  # dogtail raises SearchError, but be liberal here
            seen.append(name)

    published = []
    try:
        published = [child.name for child in tree.root.children]
    except Exception as error:
        print(f"cannot enumerate the accessibility bus: {error}", file=sys.stderr)
        sys.exit(4)

    print(
        "no ChairLift application on the accessibility bus; "
        f"tried {seen}, bus publishes {published}",
        file=sys.stderr,
    )
    sys.exit(4)


def descendants(node):
    """Yield every node under `node`, depth first, including `node`."""
    yield node
    try:
        children = node.children
    except Exception:
        return
    for child in children:
        yield from descendants(child)


def accessible_name(node):
    """Return the row's own name, or the first non-empty name beneath it.

    A GtkListBox row built from an AdwActionRow frequently leaves the row
    itself unnamed and carries the title on a label inside it. Treating that
    as "blank" would report an accessibility failure the application does
    not have, so the first named descendant stands in.
    """
    if node.name:
        return node.name
    for child in descendants(node):
        if child is node:
            continue
        if child.name:
            return child.name
    return ""


def is_selected(node):
    try:
        return bool(node.selected)
    except Exception:
        return False


def is_sidebar(node):
    """Report whether `node` is the navigation sidebar list.

    A list that contains rows is the sidebar; the identification is by shape
    rather than by accessible name because the list itself is unnamed.
    """
    try:
        if node.roleName not in LIST_ROLES:
            return False
        return any(child.roleName in ROW_ROLES for child in node.children)
    except Exception:
        return False


def find_sidebar_rows(app):
    """Return the rows of the first list that has any, in tree order."""
    for node in descendants(app):
        if is_sidebar(node):
            return [child for child in node.children if child.roleName in ROW_ROLES]
    return []


def count_content_title_labels(node, title):
    """Count labels named `title` outside the navigation sidebar.

    The sidebar row for a page carries the page title too, so a tree-wide
    count could not tell "the content area shows this page" apart from "the
    sidebar lists this page". The sidebar subtree is therefore never
    descended into.

    The exclusion is a traversal rule rather than a set of already-visited
    nodes on purpose: dogtail builds a fresh Node wrapper on every `children`
    access, so two walks of the same tree yield equal-but-distinct objects
    and any identity-based exclusion would silently match nothing.
    """
    try:
        role = node.roleName
    except Exception:
        return 0
    if is_sidebar(node):
        return 0

    count = 1 if role in LABEL_ROLES and node.name == title else 0
    try:
        children = node.children
    except Exception:
        return count
    for child in children:
        count += count_content_title_labels(child, title)
    return count


def emit(kind, **fields):
    parts = [kind] + [f"{key}={value}" for key, value in fields.items()]
    print("\t".join(parts))
    sys.stdout.flush()


def probe_page_controls(app, page_name):
    action_controls = []
    toggles = []
    for node in descendants(app):
        if getattr(node, "showing", False) and getattr(node, "visible", True):
            if getattr(node, "focusable", False) and node.roleName in ACTION_ROLES:
                action_controls.append(node)
            if node.roleName in TOGGLE_ROLES:
                toggles.append(node)

    nameless = [c for c in action_controls if not (c.name or "").strip()]
    inoperable = [c for c in action_controls if not getattr(c, "actions", {})]
    unreadable_toggles = [t for t in toggles if getattr(t, "checked", None) is None]

    emit(
        "CONTROLS",
        page=page_name,
        control_count=len(action_controls),
        nameless_count=len(nameless),
        inoperable_count=len(inoperable),
        toggle_count=len(toggles),
        unreadable_toggle_count=len(unreadable_toggles),
    )


def probe_menus_and_dialogs(app):
    menu_button = None
    for node in descendants(app):
        if node.name == "Main Menu" and node.roleName == "toggle button":
            menu_button = node
            break

    if not menu_button:
        emit("MENU_BUTTON", name="", role="", showing=0)
        return

    emit(
        "MENU_BUTTON",
        name=menu_button.name,
        role=menu_button.roleName,
        showing=int(bool(getattr(menu_button, "showing", False))),
    )

    menu_button.doActionNamed("click")

    def get_menu_items():
        return [node for node in descendants(app) if node.roleName == "menu item"]

    menu_items = poll(lambda: get_menu_items() or None) or []
    emit("POPOVER", item_count=len(menu_items))

    # Keyboard Shortcuts dialog
    # Locate by accessible name if exposed; fall back to menu model position (item 2)
    # when GTK menu items are rendered nameless on the accessibility bus.
    shortcuts_item = None
    for item in menu_items:
        if item.name == "Keyboard Shortcuts":
            shortcuts_item = item
            break
    if not shortcuts_item and len(menu_items) >= 4:
        shortcuts_item = menu_items[2]

    shortcuts_name = ""
    shortcuts_focused = 0
    shortcuts_has_updates = 0
    shortcuts_has_quit = 0

    if shortcuts_item:
        shortcuts_item.doActionNamed("click")

        def get_shortcuts_win():
            for child in getattr(app, "children", []):
                if child.name == "Keyboard Shortcuts":
                    return child
            return None

        shortcuts_win = poll(get_shortcuts_win)
        if shortcuts_win:
            shortcuts_name = shortcuts_win.name
            shortcuts_focused = int(any(getattr(d, "focused", False) for d in descendants(shortcuts_win)))
            shortcut_names = {d.name for d in descendants(shortcuts_win) if d.name}
            shortcuts_has_updates = int("Go to Updates" in shortcut_names)
            shortcuts_has_quit = int("Quit" in shortcut_names)

            close_btn = None
            for d in descendants(shortcuts_win):
                if d.name == "Close" and d.roleName == "push button":
                    close_btn = d
                    break
            if close_btn:
                close_btn.doActionNamed("click")
            else:
                rawinput.keyCombo("Escape")

            # Poll for the Shortcuts window to be dismissed
            poll(lambda: not get_shortcuts_win())

    emit(
        "DIALOG_SHORTCUTS",
        name=shortcuts_name,
        focused=shortcuts_focused,
        has_updates=shortcuts_has_updates,
        has_quit=shortcuts_has_quit,
    )

    # Re-find and click menu button for About dialog
    menu_button = None
    for node in descendants(app):
        if node.name == "Main Menu" and node.roleName == "toggle button":
            menu_button = node
            break
    if menu_button:
        menu_button.doActionNamed("click")

    menu_items = poll(lambda: get_menu_items() or None) or []
    about_item = None
    for item in menu_items:
        if item.name and item.name.startswith("About"):
            about_item = item
            break
    if not about_item and len(menu_items) >= 4:
        about_item = menu_items[3]

    about_name = ""
    about_focused = 0
    about_announces_app = 0

    if about_item:
        about_item.doActionNamed("click")

        def get_about_win():
            for child in getattr(app, "children", []):
                if child.name != "Control Center":
                    return child
            return None

        about_win = poll(get_about_win)
        if about_win:
            about_name = about_win.name
            about_focused = int(any(getattr(d, "focused", False) for d in descendants(about_win)))
            about_announces_app = int(any("Control Center" in (d.name or "") for d in descendants(about_win)))

            close_btn = None
            for d in descendants(about_win):
                if d.name == "Close" and d.roleName == "push button":
                    close_btn = d
                    break
            if close_btn:
                close_btn.doActionNamed("click")
            else:
                rawinput.keyCombo("Escape")

            # Poll for the About window to be dismissed
            poll(lambda: not get_about_win())

    emit(
        "DIALOG_ABOUT",
        name=about_name,
        focused=about_focused,
        announces_app=about_announces_app,
    )

def main(argv):
    arguments = argv[1:]
    if not arguments or len(arguments) % 2 != 0:
        print(
            "usage: atspi_probe.py <page-name> <page-title> "
            "[<page-name> <page-title>...]",
            file=sys.stderr,
        )
        return 2
    pages = list(zip(arguments[0::2], arguments[1::2]))

    # Give the bridge time to answer on a loaded CI runner without turning
    # every miss into a multi-minute hang.
    config.searchBackoffDuration = 0.5
    config.searchCutoffCount = 20
    config.actionDelay = 0.5
    config.defaultDelay = 0.5
    config.typingDelay = 0.05
    config.logDebugToFile = False
    config.logDebugToStdOut = False

    app = find_application()

    rows = find_sidebar_rows(app)
    if not rows:
        print("no navigation sidebar rows in the accessibility tree", file=sys.stderr)
        return 5

    for index, row in enumerate(rows):
        emit(
            "ROW",
            index=index,
            name=accessible_name(row),
            selected=int(is_selected(row)),
        )

    for index, (page, title) in enumerate(pages):
        # navigation compacts Alt+<number> over the visible pages in order,
        # so the Nth requested page is always Alt+N.
        rawinput.keyCombo(f"<Alt>{index + 1}")

        rows = find_sidebar_rows(app)
        selected = [
            (position, accessible_name(row))
            for position, row in enumerate(rows)
            if is_selected(row)
        ]
        selected_index, selected_name = selected[0] if len(selected) == 1 else (-1, "")

        emit(
            "PAGE",
            page=page,
            selected_index=selected_index,
            selected_name=selected_name,
            selected_count=len(selected),
            content_title_labels=count_content_title_labels(app, title),
        )

        probe_page_controls(app, page)

    probe_menus_and_dialogs(app)

    emit("DONE")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
