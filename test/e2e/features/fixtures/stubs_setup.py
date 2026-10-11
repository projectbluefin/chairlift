"""Prove the setup preview never invokes the desktop launcher."""
import os
from stubs import fake_executable, stub


@stub("setup-launch-recorder")
def setup_launch_recorder(context):
    calls = os.path.join(context.scenario_dir, "setup-launch-calls.log")
    open(calls, "w").close()
    fake_executable(context, "gtk-launch", f"printf '%s\\n' \"$*\" >> '{calls}'\nexit 1\n")
