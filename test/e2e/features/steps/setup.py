"""Explicit setup uses the main window's existing destinations and controls."""
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
        return (
            title in texts
            and (title != "Features" or "Developer Mode" in texts)
            and atspi.find_all(root, lambda n: atspi.is_button(n, "Dismiss setup"))
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
    assert atspi.poll(lambda: not atspi.find_all(context.app, lambda n: atspi.is_button(n, "Dismiss setup")))


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
