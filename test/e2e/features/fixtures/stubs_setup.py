"""Prelaunch stubs for the Setup Assistant (setup.feature).

A stub runs before ChairLift starts and only touches the scenario's own
directory; the application still runs with --dry-run.
"""

import os

from stubs import fake_executable, stub

MEDIA_NAME = "intro.mp4"
CALLS_NAME = "intro-video-calls.log"


@stub("setup-intro-video")
def setup_intro_video(context):
    """An image-provided introduction video and a gio that records its argv.

    Stands in for two things the scenario cannot reach: the OS image's
    /usr/share/chairlift/intro.mp4, which the Dakota harness masks with a
    read-only tmpfs, and the desktop's default handler, whose real invocation
    would open a player. The fake gio records the argv the assistant asked it
    to open, so the scenario can assert the hand-off itself.
    """
    media = os.path.join(context.scenario_dir, MEDIA_NAME)
    with open(media, "wb") as handle:
        handle.write(b"\x00" * 16)
    fake_executable(
        context,
        "gio",
        f"""
printf '%s\\n' "$*" >> '{context.scenario_dir}/{CALLS_NAME}'
exit 0
""",
    )
    # The chairlift_e2e build honors this override (see internal/app); it is
    # read only under --dry-run, which every scenario runs.
    context.launch_env["CHAIRLIFT_INTRO_VIDEO"] = media
