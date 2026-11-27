"""Explicit setup uses the main window's existing destinations and controls."""
import os
from behave import step, then
import chairlift_atspi as atspi

FIRSTRUN_SCHEMA = "io.projectbluefin.chairlift.firstrun"


def read_log(context):
    with open(context.log_path, "r", encoding="utf-8", errors="replace") as handle:
        return handle.read()


@then('the setup wizard shows "{title}"')
def wizard_page(context, title):
    def visible():
        root = atspi.page_root(context.app)
        texts = atspi.all_text_under(root)
        if title == "Welcome":
            return (
                "Welcome to" in texts
                and atspi.find_all(root, lambda n: atspi.is_button(n, "Dismiss Setup"))
            )
        if title == "Conclusion":
            return (
                "You Are Ready" in texts
                and atspi.find_all(root, lambda n: atspi.is_button(n, "Finish"))
            )
        return (
            title in texts
            and (title != "Features" or "Developer Mode" in texts)
            and atspi.find_all(root, lambda n: atspi.is_button(n, "Dismiss Setup"))
        )
    assert atspi.poll(visible), f"wizard never showed {title!r}"


@step('I use the setup "{label}" button')
def wizard_button(context, label):
    atspi.activate(atspi.find_button(context.app, label))


@then('the setup "{label}" button is {state:w}')
def wizard_button_state(context, label, state):
    button = atspi.find_button(context.app, label)
    assert atspi.sensitive(button) == (state == "sensitive")


@then('the setup has only one "Back" button')
def wizard_single_back(context):
    buttons = atspi.find_all(context.app, lambda node: atspi.is_button(node, "Back"))
    assert len(buttons) == 1, "native sidebar Back must not compete with setup navigation"


@then("the setup wizard is not shown")
def wizard_absent(context):
    assert atspi.poll(lambda: not atspi.find_all(context.app, lambda n: atspi.is_button(n, "Dismiss Setup")))


@then("the setup dry run would record disposition {value:w}")
def step_would_record(context, value):
    line = f"[DRY-RUN] would set {FIRSTRUN_SCHEMA} disposition={value}"
    assert atspi.poll(lambda: line in read_log(context)), f"missing {line!r}"


@then("the setup dry run would record no disposition")
def step_would_record_nothing(context):
    assert f"[DRY-RUN] would set {FIRSTRUN_SCHEMA} disposition=" not in read_log(context)


@then("the setup dry run would record the completed version")
def step_would_record_version(context):
    assert atspi.poll(lambda: f"[DRY-RUN] would set {FIRSTRUN_SCHEMA} completed-version=" in read_log(context))


@then('the setup bookend displays the "{brand}" logo')
def step_setup_bookend_displays_logo(context, brand):
    def logo_showing():
        root = atspi.page_root(context.app)
        images = atspi.find_all(root, lambda n: atspi.role(n) == "image" and atspi.name(n) == brand)
        return bool(images)

    assert atspi.poll(logo_showing), f"setup bookend logo {brand!r} not found"


INTRO_MEDIA = "intro.mp4"
INTRO_CALLS = "intro-video-calls.log"


def intro_calls(context):
    """The argv lines the scenario's fake gio recorded, if any."""
    path = os.path.join(context.scenario_dir, INTRO_CALLS)
    try:
        with open(path, encoding="utf-8", errors="replace") as handle:
            return [line for line in handle.read().splitlines() if line]
    except FileNotFoundError:
        return []


@then('the setup offers a "{label}" button')
def wizard_button_offered(context, label):
    assert atspi.poll(
        lambda: bool(atspi.find_all(context.app, lambda n: atspi.is_button(n, label)))
    ), f"setup does not offer a {label!r} button"


@then('the setup does not offer a "{label}" button')
def wizard_button_not_offered(context, label):
    assert atspi.poll(
        lambda: not atspi.find_all(context.app, lambda n: atspi.is_button(n, label))
    ), f"setup offers a {label!r} button but should not"


@then("the intro video opened in the default handler")
def intro_video_opened(context):
    media = os.path.join(context.scenario_dir, INTRO_MEDIA)
    expected = f"open {media}"
    assert atspi.poll(lambda: expected in intro_calls(context)), (
        f"the assistant never opened {expected!r}; gio saw {intro_calls(context)}"
    )


@then("the intro video opened again from the button")
def intro_video_opened_again(context):
    def twice():
        return sum(1 for line in intro_calls(context) if line.startswith("open ")) >= 2

    assert atspi.poll(twice), (
        f"the button did not re-open the video; gio saw {intro_calls(context)}"
    )
