"""Accessibility-tree helpers shared by ChairLift's behave steps.

Modeled on projectbluefin/testsuite's dogtail usage: the application is
looked up by name on the accessibility bus, controls are found by role and
accessible name, and state is read from the tree rather than from pixels.

Everything here polls instead of sleeping. The accessibility bridge answers
asynchronously and a loaded CI runner is slow, so every lookup takes a
deadline and retries until it passes or the deadline expires.
"""

import time

# dogtail.tree runs checkForA11y() at import time, which reads
# org.gnome.desktop.interface toolkit-accessibility through GSettings. The
# runner uses GSETTINGS_BACKEND=memory, so that key is always false and
# dogtail would print its complaint to stdout and exit 1. The application
# publishes through GTK_A11Y=atspi regardless of that key.
from dogtail.config import config

config.checkForA11y = False

# dogtail.tree and dogtail.rawinput connect to the accessibility bus when
# imported, so they are imported on first use: behave's --dry-run loads these
# steps on hosts with no bus at all.


def _tree():
    from dogtail import tree

    return tree


def _rawinput():
    from dogtail import rawinput

    return rawinput

# Lookups here poll with their own deadlines (see poll), so dogtail's own
# retry knobs are not used; only the input pacing is set.
config.actionDelay = 0.2
config.defaultDelay = 0.2
config.typingDelay = 0.03

DEFAULT_TIMEOUT = 10.0

# g_get_application_name() defaults to the program name, but a build that sets
# the product name or the application ID would publish that instead.
APPLICATION_NAMES = ("chairlift", "Control Center", "io.projectbluefin.chairlift")

# GTK 4 maps GtkListBox to "list box" and each row to "list item"; older
# bridges report "list" and "table cell".
LIST_ROLES = ("list box", "list")
ROW_ROLES = ("list item", "table cell")
LABEL_ROLES = ("label", "heading", "static")
BUTTON_ROLES = ("push button", "toggle button", "menu button", "button")
TOGGLE_ROLES = ("toggle button", "switch", "check box")
TEXT_ROLES = ("text", "entry", "search box", "password text")
DIALOG_ROLES = ("dialog", "alert", "frame", "window", "file chooser")


class TreeError(AssertionError):
    """A lookup or state assertion against the tree failed."""


def poll(predicate, timeout=DEFAULT_TIMEOUT, interval=0.1):
    """Return predicate()'s first truthy value, or its last value at timeout.

    An exception from predicate is retried, because the tree mutates while it
    is read and nested lookups raise while a node is still missing. If the
    final attempt still raises, that exception is raised as a TreeError instead
    of being reported as a falsy timeout: a step bug or a dropped bus must not
    read the same as a slow widget.
    """
    deadline = time.monotonic() + timeout
    while True:
        last_error = None
        try:
            result = predicate()
        except Exception as error:  # the tree mutates under us; retry
            result = None
            last_error = error
        if result:
            return result
        if time.monotonic() >= deadline:
            if last_error is not None:
                raise TreeError(
                    f"still failing after {timeout}s: {last_error!r}"
                ) from last_error
            return result
        time.sleep(interval)


def safe(getter, default=None):
    try:
        return getter()
    except Exception:
        return default


def role(node):
    return safe(lambda: node.roleName, "") or ""


def name(node):
    return (safe(lambda: node.name, "") or "").strip()


def description(node):
    return (safe(lambda: node.description, "") or "").strip()


def text(node):
    """The node's text content, for roles that implement the text interface."""
    return (safe(lambda: node.text, "") or "").strip()


def children(node):
    return safe(lambda: list(node.children), []) or []


def showing(node):
    return bool(safe(lambda: node.showing, False))


def sensitive(node):
    return bool(safe(lambda: node.sensitive, False))


def checked(node):
    return safe(lambda: node.checked, None)


def selected(node):
    return bool(safe(lambda: node.selected, False))


def focused(node):
    return bool(safe(lambda: node.focused, False))


def focusable(node):
    return bool(safe(lambda: node.focusable, False))


def actions(node):
    return safe(lambda: dict(node.actions), {}) or {}


def descendants(node, prune=None, only_showing=False):
    """Yield every node under node, depth first, including node.

    prune(n) returning true skips n's subtree. only_showing skips subtrees
    whose root is not showing, which is how hidden stack pages are ignored.
    """
    stack = [node]
    while stack:
        current = stack.pop()
        if only_showing and current is not node and not showing(current):
            continue
        yield current
        if prune is not None and current is not node and prune(current):
            continue
        stack.extend(reversed(children(current)))


def label_text(node):
    """The node's own name, else the first non-empty name beneath it.

    A row built from an AdwActionRow frequently leaves the row itself unnamed
    and carries the title on a label inside it.
    """
    own = name(node)
    if own:
        return own
    for child in descendants(node):
        if child is node:
            continue
        found = name(child) or text(child)
        if found:
            return found
    return ""


def all_text_under(root):
    """Every showing name, description, and text under root, for 'I see' checks."""
    parts = []
    for child in search_nodes(root):
        for value in (name(child), description(child), text(child)):
            if value:
                parts.append(value)
    return parts


def find_application(timeout=30.0):
    def lookup():
        for app in children(_tree().root):
            if name(app) in APPLICATION_NAMES:
                return app
        return None

    app = poll(lookup, timeout=timeout, interval=0.25)
    if app is None:
        published = [name(a) for a in children(_tree().root)]
        raise TreeError(
            f"no ChairLift application on the accessibility bus; tried "
            f"{list(APPLICATION_NAMES)}, bus publishes {published}"
        )
    return app


def main_window(app):
    """The top-level window that holds the navigation sidebar."""
    for window in children(app):
        if sidebar_path(window) is not None:
            return window
    raise TreeError(f"no main window; top-level children are {describe(children(app))}")


def top_level(app, title, timeout=DEFAULT_TIMEOUT):
    """A showing top-level window or dialog named title."""
    def lookup():
        for window in children(app):
            if name(window) == title and showing(window):
                return window
        return None

    found = poll(lookup, timeout=timeout)
    if found is None:
        raise TreeError(
            f"timed out after {timeout}s waiting for a window named {title!r}; "
            f"top-level children are {describe(children(app))}"
        )
    return found


def is_sidebar(node):
    """A list whose children are rows is a sidebar candidate.

    Identified by shape because the list itself is unnamed. Content pages
    also contain row lists, so only the first such list in tree order is the
    sidebar; see sidebar_path.
    """
    if role(node) not in LIST_ROLES:
        return False
    return any(role(child) in ROW_ROLES for child in children(node))


def sidebar_path(window):
    """The child-index path from window to the sidebar list, or None.

    A path, not a node, because dogtail builds a fresh wrapper on every
    children access, so identity comparisons between two walks never match.
    """
    stack = [(window, ())]
    while stack:
        node, path = stack.pop()
        if path and not showing(node):
            continue
        if is_sidebar(node):
            return path
        kids = children(node)
        for index in range(len(kids) - 1, -1, -1):
            stack.append((kids[index], path + (index,)))
    return None


def sidebar(app):
    window = main_window(app)
    path = sidebar_path(window)
    if path is None:
        return None
    node = window
    for index in path:
        node = children(node)[index]
    return node


def sidebar_rows(app):
    bar = safe(lambda: sidebar(app))
    if bar is None:
        return []
    return [child for child in children(bar) if role(child) in ROW_ROLES]


def content_nodes(app):
    """Showing nodes in the main window outside the sidebar."""
    window = main_window(app)
    skip = sidebar_path(window)
    stack = [(window, ())]
    while stack:
        node, path = stack.pop()
        if path and not showing(node):
            continue
        if skip is not None and path == skip:
            continue
        yield node
        kids = children(node)
        for index in range(len(kids) - 1, -1, -1):
            stack.append((kids[index], path + (index,)))


def page_root(app):
    """A pseudo-root whose descendants are the content side of the window.

    Steps that search "the page" use this so a label that also appears in the
    sidebar cannot satisfy a content assertion.
    """
    return _ContentRoot(app)


class _ContentRoot:
    def __init__(self, app):
        self.app = app


def search_nodes(root, only_showing=True):
    if isinstance(root, _ContentRoot):
        return content_nodes(root.app)
    return descendants(root, only_showing=only_showing)


def describe(nodes):
    return [f"{role(n)}:{name(n)!r}" for n in nodes]


def find(root, predicate, what, timeout=DEFAULT_TIMEOUT, only_showing=True):
    """Return the first node under root matching predicate, or raise."""
    def lookup():
        for node in search_nodes(root, only_showing):
            if predicate(node):
                return node
        return None

    found = poll(lookup, timeout=timeout)
    if found is None:
        raise TreeError(f"timed out after {timeout}s waiting for {what}")
    return found


def find_all(root, predicate, only_showing=True):
    return [n for n in search_nodes(root, only_showing) if predicate(n)]


def is_button(node, wanted=None):
    """An operable button named wanted.

    GtkMenuButton publishes an action-less "button" wrapper around the
    toggle button that actually carries the click action; only the operable
    node counts, so the wrapper never shadows it.
    """
    if role(node) not in BUTTON_ROLES or not actions(node):
        return False
    return wanted is None or label_text(node) == wanted


def find_button(root, wanted, timeout=DEFAULT_TIMEOUT):
    return find(root, lambda n: is_button(n, wanted), f"a button named {wanted!r}", timeout)


def row_containing(root, title, timeout=DEFAULT_TIMEOUT):
    """The innermost list row that shows a label named title."""
    def lookup():
        best = None
        for node in search_nodes(root):
            if role(node) in ROW_ROLES and any(
                name(child) == title for child in descendants(node, only_showing=True)
            ):
                best = node  # depth-first: later matches are nested deeper
        return best

    found = poll(lookup, timeout=timeout)
    if found is None:
        raise TreeError(f"timed out after {timeout}s waiting for a row titled {title!r}")
    return found


def activate(node):
    """Invoke node's primary action the way an assistive technology would."""
    available = actions(node)
    for action in ("click", "activate", "press", "toggle", "jump"):
        if action in available:
            node.doActionNamed(action)
            return action
    raise TreeError(
        f"{role(node)} {label_text(node)!r} exposes no primary action; has {sorted(available)}"
    )


def attributes(node):
    return safe(lambda: dict(node.get_attributes()), {}) or {}


def is_toast(node):
    """An AdwToast: published as an "alert" holding a "Dismiss" button.

    AdwAlertDialog can share the alert role, so the Dismiss button is what
    tells a toast from a dialog.
    """
    if role(node) != "alert":
        return False
    return any(
        role(child) in BUTTON_ROLES and name(child) == "Dismiss"
        for child in descendants(node, only_showing=True)
    )


def focus_by_tab(app, predicate, what, limit=80, backwards=False):
    """Press Tab until a node matching predicate reports focus, and return it.

    GTK 4 does not implement AT-SPI Component.GrabFocus, so grabFocus() never
    moves focus; walking the focus chain is the only reliable way to put
    keyboard focus on a particular widget.
    """
    key = "<Shift>Tab" if backwards else "Tab"

    def matching():
        node = focused_node(app)
        return node if node is not None and predicate(node) else None

    if matching() is not None:
        return matching()
    for _ in range(limit):
        press_and_settle(app, key)
        found = matching()
        if found is not None:
            return found
    raise TreeError(f"keyboard focus never reached {what} after {limit} Tab presses")


def focused_node(app):
    """The showing node that holds keyboard focus, or None."""
    for node in descendants(app, only_showing=True):
        if focused(node):
            return node
    return None


def press_and_settle(app, combo, timeout=2.0):
    """Press a focus-moving key and wait until focus has actually moved.

    Checking once after a fixed pause lets a loop on a slow runner press again
    before the first press lands, stepping past its target. If focus does not
    move within timeout (the end of a chain, a key that does not move focus
    there), the caller's own check decides what that means.
    """
    before = focused_node(app)
    press(combo)
    poll(lambda: (lambda now: now is not None and not same_node(now, before))(focused_node(app)), timeout=timeout)


def same_node(a, b):
    """Whether two wrappers name the same accessible.

    dogtail builds a fresh wrapper per lookup, so identity never matches;
    role, name and index in parent together identify a node well enough to
    tell that focus moved.
    """
    if a is None or b is None:
        return a is b
    return (
        role(a) == role(b)
        and name(a) == name(b)
        and safe(lambda: a.indexInParent) == safe(lambda: b.indexInParent)
        and name(safe(lambda: a.parent)) == name(safe(lambda: b.parent))
    )


def set_text(node, value):
    """Replace an entry's contents through the EditableText interface."""
    editable = safe(lambda: node.get_editable_text_iface())
    if editable is None:
        raise TreeError(f"{role(node)} {label_text(node)!r} is not editable")
    editable.set_text_contents(value)


def press(combo):
    _rawinput().keyCombo(combo)


def type_text(value):
    _rawinput().typeText(value)


def dump(node, stream, depth=0, limit=40):
    """Write node's subtree as an indented outline, for failure artifacts."""
    if depth > limit:
        return
    states = []
    for label, value in (
        ("showing", showing(node)),
        ("sensitive", sensitive(node)),
        ("focused", focused(node)),
        ("selected", selected(node)),
        ("focusable", focusable(node)),
    ):
        if value:
            states.append(label)
    state = checked(node)
    if state is not None and role(node) in TOGGLE_ROLES:
        states.append("checked" if state else "unchecked")
    extra = []
    if description(node):
        extra.append(f"desc={description(node)!r}")
    if role(node) in TEXT_ROLES and text(node):
        extra.append(f"text={text(node)!r}")
    acts = sorted(actions(node))
    if acts:
        extra.append(f"actions={acts}")
    stream.write(
        f"{'  ' * depth}{role(node)} {name(node)!r} [{','.join(states)}] {' '.join(extra)}\n"
    )
    for child in children(node):
        dump(child, stream, depth + 1, limit)
