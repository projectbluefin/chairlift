"""behave hooks: launch a fresh ChairLift per scenario and tear it down.

run_atspi.sh provides the private Xvfb display and the private D-Bus session
this process runs in. Each scenario gets its own application process with its
own HOME, XDG_RUNTIME_DIR, configuration fixture and action journal, so a
scenario cannot leak state into the next one.

Scenario tags select fixtures:

    @config.<name>   Load features/fixtures/config/<name>.yml as the
                     configuration (default: everything). The file is written
                     as config.dev.yml beside a staged copy of the binary,
                     the first relative candidate config.Load searches.
    @env.KEY=VALUE   Override one launch environment variable, e.g.
                     @env.CHAIRLIFT_GPU_VENDORS=0x8086 for an Intel-only host.
    @stub.<name>     Run a prelaunch stub from fixtures/stubs.py against the
                     scenario before ChairLift starts (fake executables on
                     PATH, pre-seeded files in HOME).
    @no-app          Do not launch ChairLift for this scenario.
    @known_issue.<N> A confirmed defect tracked by issue N. Skipped unless
                     CHAIRLIFT_ATSPI_KNOWN_ISSUES=1, so the scenario stays
                     written and runnable while the fix is outstanding.

The application always runs with --dry-run.
"""

import json
import os
import re
import shutil
import signal
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "lib"))
sys.path.insert(0, os.path.join(HERE, "fixtures"))

import chairlift_atspi as atspi  # noqa: E402
import stubs  # noqa: E402

READY_MARKERS = (
    "Running in dry-run mode",
    "ChairLift activated",
    "app: window presented",
)
STARTUP_TIMEOUT = 60.0
SHUTDOWN_TIMEOUT = 15.0
DEFAULT_CONFIG = "everything"
CLOSED_PROXY = "http://127.0.0.1:9"

# The display-side stubs the chairlift_e2e build honors
# (internal/app/imageinfo_override_e2e.go), set so every Bluefin-family row
# renders on a runner that is not a Bluefin system. Scenarios override one
# with @env.KEY=VALUE.
DEFAULT_IMAGE_INFO = {
    "image-name": "dakota",
    "image-tag": "latest",
    "image-ref": "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota",
    "image-vendor": "projectbluefin",
    "image-flavor": "main",
}
DEFAULT_STUBS = {
    "CHAIRLIFT_AUTO_UPDATES": "enabled,active",
    "CHAIRLIFT_GPU_VENDORS": "0x8086,0x10de",
    "CHAIRLIFT_CAPABILITIES": "image-descriptor,flatpak,brew,podman,bootc-stage",
}


def slug(value):
    return re.sub(r"[^A-Za-z0-9]+", "-", value).strip("-").lower()[:80] or "scenario"


def before_all(context):
    context.app_binary = os.environ["CHAIRLIFT_ATSPI_APP"]
    context.out_dir = os.environ["CHAIRLIFT_ATSPI_OUT"]
    context.fixtures_dir = os.path.join(HERE, "fixtures")
    context.navigation = json.loads(os.environ.get("CHAIRLIFT_NAVIGATION", "[]"))
    context.shortcuts = json.loads(os.environ.get("CHAIRLIFT_SHORTCUTS", "[]"))
    context.expected_app_name = os.environ.get("CHAIRLIFT_APP_NAME", "Control Center")
    context.staged = {}
    for trusted in ("/etc/chairlift/config.yml", "/usr/share/chairlift/config.yml"):
        if os.path.exists(trusted):
            raise RuntimeError(
                f"{trusted} exists and outranks every configuration fixture; run the "
                "suite on a host or container without it (see docs/skills/gtk-headless-testing)"
            )


def staged_binary(context, config_name):
    """A copy of the binary beside the chosen configuration fixture.

    One copy per fixture, reused across scenarios. A copy rather than the
    build directory itself, because the walkthrough writes its own
    config.dev.yml beside build/e2e/chairlift and suites must not depend on
    which ran first.
    """
    if config_name in context.staged:
        return context.staged[config_name]
    source = os.path.join(context.fixtures_dir, "config", f"{config_name}.yml")
    if not os.path.exists(source):
        raise RuntimeError(f"unknown configuration fixture @config.{config_name}: {source}")
    directory = os.path.join(context.out_dir, "bin", config_name)
    os.makedirs(directory, exist_ok=True)
    binary = os.path.join(directory, "chairlift")
    shutil.copy2(context.app_binary, binary)
    shutil.copy2(source, os.path.join(directory, "config.dev.yml"))
    context.staged[config_name] = (binary, os.path.join(directory, "config.dev.yml"))
    return context.staged[config_name]


def parse_tags(tags):
    config_name = DEFAULT_CONFIG
    env = {}
    stub_names = []
    launch = True
    for tag in tags:
        if tag.startswith("config."):
            config_name = tag[len("config."):]
        elif tag.startswith("env.") and "=" in tag:
            key, value = tag[len("env."):].split("=", 1)
            env[key] = value
        elif tag.startswith("stub."):
            stub_names.append(tag[len("stub."):])
        elif tag == "no-app":
            launch = False
    return config_name, env, stub_names, launch


def before_scenario(context, scenario):
    # Feature-level tags first so a scenario's own @config/@env wins.
    feature_tags = list(scenario.feature.tags)
    tags = feature_tags + [t for t in scenario.effective_tags if t not in feature_tags]
    known = [t for t in tags if t.startswith("known_issue.")]
    if known and not os.environ.get("CHAIRLIFT_ATSPI_KNOWN_ISSUES"):
        issues = ", ".join("#" + t.split(".", 1)[1] for t in known)
        scenario.skip(f"known issue {issues}; set CHAIRLIFT_ATSPI_KNOWN_ISSUES=1 to run it")
        context.app_process = None
        context.app = None
        return
    config_name, overrides, stub_names, launch = parse_tags(tags)

    # behave discards attributes set during a scenario when it ends, so the
    # counter lives on the run-wide userdata instead.
    index = context.config.userdata.get("chairlift_scenario_index", 0) + 1
    context.config.userdata["chairlift_scenario_index"] = index
    context.scenario_dir = os.path.join(context.out_dir, "scenarios", f"{index:03d}-{slug(scenario.name)}")
    home = os.path.join(context.scenario_dir, "home")
    runtime = os.path.join(context.scenario_dir, "runtime")
    stub_bin = os.path.join(context.scenario_dir, "bin")
    for directory in (home, runtime, stub_bin):
        os.makedirs(directory, exist_ok=True)
    os.chmod(runtime, 0o700)

    image_info = os.path.join(context.scenario_dir, "image-info.json")
    with open(image_info, "w", encoding="utf-8") as handle:
        json.dump(DEFAULT_IMAGE_INFO, handle)

    env = dict(os.environ)
    env.update(DEFAULT_STUBS)
    env.update(
        {
            "HOME": home,
            "XDG_RUNTIME_DIR": runtime,
            "XDG_CONFIG_HOME": os.path.join(home, ".config"),
            "XDG_DATA_HOME": os.path.join(home, ".local", "share"),
            "XDG_CACHE_HOME": os.path.join(home, ".cache"),
            "CHAIRLIFT_IMAGE_INFO": image_info,
            "CHAIRLIFT_ACTION_JOURNAL": os.path.join(context.scenario_dir, "journal.jsonl"),
            "PATH": stub_bin + os.pathsep + env.get("PATH", ""),
        }
    )
    # No scenario reaches the internet: every proxy-aware client (Go's
    # net/http, curl under brew) is pointed at a closed loopback port, while
    # loopback stubs stay reachable. A scenario that needs a remote answers
    # it with a loopback stub.
    for key in ("HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY", "all_proxy"):
        env[key] = CLOSED_PROXY
    env["NO_PROXY"] = env["no_proxy"] = "localhost,127.0.0.1,::1"
    env.update(overrides)
    context.launch_env = env
    context.stub_bin = stub_bin
    context.home = home
    context.journal_path = env["CHAIRLIFT_ACTION_JOURNAL"]
    context.app_process = None
    context.app = None

    # Every scenario gets an inert brew first on PATH, so neither the
    # runner's Homebrew nor a developer's (reachable through the fallback path
    # internal/homebrew.ExecutablePath checks) leaks inventory into a run. A
    # @stub that needs brew behaviour overwrites it.
    stubs.default_brew(context)
    for stub_name in stub_names:
        stubs.apply(stub_name, context)

    if not launch:
        return

    binary, config_path = staged_binary(context, config_name)
    context.config_path = config_path
    launch_app(context, binary)


def launch_app(context, binary):
    context.log_path = os.path.join(context.scenario_dir, "chairlift.log")
    log = open(context.log_path, "wb")
    context.app_process = subprocess.Popen(
        [binary, "--dry-run"],
        cwd=context.scenario_dir,
        env=context.launch_env,
        stdout=log,
        stderr=subprocess.STDOUT,
        # Its own process group, so teardown reaches the Homebrew readers
        # startup spawns without signalling behave.
        start_new_session=True,
    )
    log.close()

    deadline = time.monotonic() + STARTUP_TIMEOUT
    while True:
        output = read_log(context)
        if all(marker in output for marker in READY_MARKERS):
            break
        if context.app_process.poll() is not None:
            raise RuntimeError(f"ChairLift exited before becoming ready:\n{output}")
        if time.monotonic() > deadline:
            raise RuntimeError(f"ChairLift did not become ready in {STARTUP_TIMEOUT}s:\n{output}")
        time.sleep(0.1)

    # Load from the fixture, or the scenario is testing some other file.
    expected = f"Loaded config from {context.config_path}"
    failed_closed = "CONFIGURATION ERROR" in output and context.config_path in output
    if expected not in output and not failed_closed:
        raise RuntimeError(f"ChairLift did not load {context.config_path}:\n{output}")

    # Presenting the window and registering its widgets on the accessibility
    # bus are separate events; wait for the sidebar rather than sleeping.
    context.app = atspi.find_application(timeout=STARTUP_TIMEOUT)
    if not atspi.poll(lambda: atspi.sidebar_rows(context.app), timeout=STARTUP_TIMEOUT):
        raise RuntimeError("the main window never published its navigation sidebar")


def read_log(context):
    try:
        with open(context.log_path, "r", encoding="utf-8", errors="replace") as handle:
            return handle.read()
    except OSError:
        return ""


def after_scenario(context, scenario):
    if not hasattr(context, "journal_path"):
        return
    if scenario.status == "failed" and context.app is not None:
        capture_failure(context)
    stop_app(context)
    if scenario.status == "failed":
        # behave prints the step traceback; the log is what explains it.
        sys.stdout.write(f"\n--- chairlift.log ({context.scenario_dir}) ---\n")
        sys.stdout.write(read_log(context)[-8000:])
        sys.stdout.write("\n")


def capture_failure(context):
    try:
        with open(os.path.join(context.scenario_dir, "tree.txt"), "w", encoding="utf-8") as handle:
            atspi.dump(context.app, handle)
    except Exception as error:  # a failed dump must not mask the real failure
        sys.stdout.write(f"accessibility tree dump failed: {error}\n")
    xwd = shutil.which("xwd")
    if xwd:
        subprocess.run(
            [xwd, "-root", "-silent", "-out", os.path.join(context.scenario_dir, "screen.xwd")],
            check=False,
            timeout=20,
        )


def stop_app(context):
    process = getattr(context, "app_process", None)
    if process is None:
        return
    if process.poll() is None:
        try:
            # SIGTERM first: ChairLift quits on the main thread and exits
            # normally, which is what flushes GOCOVERDIR counters.
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            process.wait(timeout=SHUTDOWN_TIMEOUT)
        except subprocess.TimeoutExpired:
            pass
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except (ProcessLookupError, PermissionError):
        pass
    try:
        process.wait(timeout=SHUTDOWN_TIMEOUT)
    except subprocess.TimeoutExpired:
        pass
    # The Homebrew runner starts its readers in new process groups, so the
    # group kill above misses them. Every descendant inherits this
    # scenario's unique journal path, which identifies them without touching
    # anything else in the session.
    kill_scenario_processes(context.journal_path)
    context.app_process = None
    context.app = None


def kill_scenario_processes(journal_path):
    marker = f"CHAIRLIFT_ACTION_JOURNAL={journal_path}".encode()
    own = os.getpid()
    for entry in os.listdir("/proc"):
        if not entry.isdigit() or int(entry) == own:
            continue
        try:
            with open(f"/proc/{entry}/environ", "rb") as handle:
                if marker not in handle.read().split(b"\0"):
                    continue
            os.kill(int(entry), signal.SIGKILL)
        except (OSError, ProcessLookupError):
            continue
