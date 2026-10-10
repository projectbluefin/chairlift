"""Prelaunch stubs for the Agents page (features/agents.feature).

Agent Mode's state is read from three places (internal/aistack.Observe and
Healthy): whether an llmman executable resolves, whether ChairLift's user
unit exists, and whether 127.0.0.1:17434/llmman/node answers. Each stub here
controls one of them inside the scenario, so the page renders a chosen state
regardless of what the host or container ships.

Every fake executable records its argv to <scenario>/calls/<program>, which
is how scenarios prove a dry-run never reached llmman, systemctl mutations, or brew bundle.

The loopback node endpoint follows community PR #372's approach (mendezr): a
small Python HTTP server answering /llmman/node with a fixed memory figure.
It is spawned with the scenario's launch environment, so environment.py's
journal-path sweep kills it with the application's other descendants.
"""

import os
import socket
import subprocess
import sys
import textwrap
import time

from stubs import fake_executable, stub

NODE_HOST = "127.0.0.1"
NODE_PORT = 17434
# 16 GiB: the memory figure the node stub reports on /llmman/node. Nothing
# in ChairLift reads it since the model picker was removed (#568); it only
# keeps the stub's response shaped like a real llmman node.
NODE_MEMORY = 17179869184

# Where the dogfooded Homebrew lives in the Dakota container and on GitHub's
# runners. Its bin directory ships a real llmman, so it is removed from PATH
# and a recording brew stands in; homebrew.ExecutablePath then resolves the
# fake, and aistack.Executable's "beside brew" fallback finds only what the
# scenario put in its own bin directory.
HOMEBREW_BIN_PARTS = ("/home/linuxbrew/", "/linuxbrew/.linuxbrew")

# Defensive guard: the Agents page makes no non-loopback requests since the
# model picker was removed (#568), but pointing every HTTP(S)_PROXY at a
# refused listener makes any future one fail fast instead of depending on
# the network. Go's ProxyFromEnvironment never proxies loopback, so the
# node stub stays reachable.
BLACKHOLE_PROXY = "http://127.0.0.1:9"

# What internal/troubleshoot.Detect resolves for the Goose row.
GOOSE_PROGRAMS = ("linux-mcp-server", "goose-desktop", "llmman")


def profile_root(context):
    """ChairLift's own Goose profile (internal/troubleshoot.DefaultProfile)."""
    return os.path.join(context.launch_env["XDG_DATA_HOME"], "chairlift", "troubleshooting")


def calls_dir(context):
    path = os.path.join(context.scenario_dir, "calls")
    os.makedirs(path, exist_ok=True)
    return path


def recorder(context, program, body="exit 0\n"):
    log = os.path.join(calls_dir(context), program)
    fake_executable(
        context,
        program,
        f'printf "%s\\n" "$*" >> "{log}"\n' + textwrap.dedent(body),
    )


def unit_path(context):
    return os.path.join(context.home, ".config", "systemd", "user", "chairlift-llmman.service")


def fragment_path(context):
    return os.path.join(context.home, ".config", "environment.d", "10-chairlift-llmman.conf")


def alias_path(context):
    return os.path.join(context.scenario_dir, "llmman-active-alias")


@stub("agents.host")
def host(context):
    """A Homebrew host with no llmman, no unit, and no internet.

    Recording brew and systemctl stand in for the real ones; the host's own
    Homebrew directory is taken off PATH so its llmman cannot resolve.
    """
    env = context.launch_env
    kept = [
        part for part in env.get("PATH", "").split(os.pathsep)
        if part and not any(marker in part for marker in HOMEBREW_BIN_PARTS)
    ]
    env["PATH"] = os.pathsep.join(kept)
    for key in ("HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"):
        env[key] = BLACKHOLE_PROXY
    for key in ("NO_PROXY", "no_proxy"):
        env.pop(key, None)
    # Goose's pieces may also sit outside Homebrew on a developer's host;
    # internal/troubleshoot.Detect must see only what a scenario installs.
    kept = [
        part for part in kept
        if part == context.stub_bin
        or not any(os.access(os.path.join(part, program), os.X_OK) for program in GOOSE_PROGRAMS)
    ]
    env["PATH"] = os.pathsep.join(kept)
    recorder(context, "brew")
    recorder(context, "systemctl")
    calls_dir(context)


@stub("agents.llmman")
def llmman(context):
    """An installed llmman that records every call.

    `config get aliases.bluefin-active` answers with the scenario's alias
    file when one was seeded, as llmman prints a configured value.
    """
    alias = alias_path(context)
    recorder(
        context,
        "llmman",
        f"""
        if [ "$1 $2 $3" = "config get aliases.bluefin-active" ] && [ -f "{alias}" ]; then
            cat "{alias}"
        fi
        exit 0
        """,
    )


@stub("agents.alias")
def alias(context):
    """llmman's active-model alias points at a known model."""
    with open(alias_path(context), "w", encoding="utf-8") as handle:
        handle.write("unsloth/Qwen3-8B-GGUF:Q4_K_M\n")


def write_file(path, content):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(content)


@stub("agents.unit")
def unit(context):
    """Agent Mode was turned on earlier: ChairLift's unit and fragment exist."""
    write_file(
        unit_path(context),
        "[Unit]\nDescription=Agent Mode local model server (llmman)\n\n"
        "[Service]\nExecStart=/usr/bin/false serve\n",
    )
    write_file(fragment_path(context), "OLLAMA_HOST=127.0.0.1:17434\n")


@stub("agents.goose")
def goose(context):
    """Goose Desktop and linux-mcp-server are installed.

    Nothing is written under HOME: a session runs in a profile ChairLift
    writes at launch, so there is no Goose configuration to seed.
    """
    goose_server(context)
    goose_desktop(context)


@stub("agents.goose-server")
def goose_server(context):
    """linux-mcp-server is installed; the Goose desktop app is not."""
    recorder(context, "linux-mcp-server")


@stub("agents.goose-desktop")
def goose_desktop(context):
    """The Goose desktop app is installed; linux-mcp-server is not."""
    recorder(context, "goose-desktop")


DEVMENU_PATH = "/org/gnome/shell/extensions/custom-command-list/"


def _tuple(label, command, icon, visible):
    return f"('{label}', '{command}', '{icon}', {'true' if visible else 'false'})"


WEB_LINK_COMMAND = "xdg-open https://ask.projectbluefin.io"
# The entry as Bluefin's distro layer ships it (projectbluefin/common#1396).
WRAPPER_COMMAND = "/home/linuxbrew/.linuxbrew/bin/chairlift-wrapper --ask-bluefin"


def _devmenu(context, command, slot="command11", order=None):
    """The Custom Command Menu extension with an Ask Bluefin entry running command.

    order, when given, is the distro layer's command-order; None leaves the key
    unset so the schema default (every slot) applies."""
    entry = _tuple("Ask Bluefin", command, "", True)
    defaults = {slot: entry}
    if order is not None:
        defaults["command-order"] = order
    dump = "\n".join(["[/]"] + [f"{key}={value}" for key, value in sorted(defaults.items())] + [""])
    dump_path = os.path.join(context.scenario_dir, "dconf-dump.txt")
    with open(dump_path, "w", encoding="utf-8") as handle:
        handle.write(dump)
    default_cases = "\n".join(
        f'      "{DEVMENU_PATH}{key}") echo "{value}" ;;' for key, value in defaults.items()
    )
    dconf_log = os.path.join(calls_dir(context), "dconf")
    fake_executable(
        context,
        "dconf",
        f'printf "%s\\n" "$*" >> "{dconf_log}"\n'
        + f"""
case "$1" in
  dump)
    [ "$2" = "{DEVMENU_PATH}" ] && cat "{dump_path}"
    ;;
  read)
    if [ "$2" = "-d" ]; then
      case "$3" in
{default_cases}
      esac
    fi
    ;;
esac
exit 0
""",
    )


@stub("agents.devmenu")
def devmenu(context):
    """The Custom Command Menu extension with the web-link Ask Bluefin entry."""
    _devmenu(context, WEB_LINK_COMMAND)


@stub("agents.devmenu.wrapper")
def devmenu_wrapper(context):
    """The Ask Bluefin entry as the distro ships it, through chairlift-wrapper."""
    _devmenu(context, WRAPPER_COMMAND)


@stub("agents.devmenu.unlisted")
def devmenu_unlisted(context):
    """Dakota's layout: Ask Bluefin moved to command12, visible, while the
    inherited command-order lists only 1..11, so the menu omits it."""
    _devmenu(context, WEB_LINK_COMMAND, slot="command12", order="[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]")



NODE_SERVER = r'''
import json, sys
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

log_path, memory, alias_path = sys.argv[1], int(sys.argv[2]), Path(sys.argv[3])


class Node(BaseHTTPRequestHandler):
    def do_GET(self):
        with open(log_path, "a", encoding="utf-8") as log:
            log.write(self.path + "\n")
        if self.path != "/llmman/node":
            self.send_error(404)
            return
        stored = {}
        if alias_path.exists():
            alias = alias_path.read_text(encoding="utf-8").strip()
            if alias:
                # llmman keeps the supplied alias but canonicalizes node keys.
                canonical = alias if alias.startswith("hf.co/") else "hf.co/" + alias
                stored[canonical] = {}
        body = json.dumps({"memory": memory, "loaded": {}, "stored": stored}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


ThreadingHTTPServer.allow_reuse_address = True
ThreadingHTTPServer(("127.0.0.1", 17434), Node).serve_forever()
'''


def node_answers():
    try:
        with socket.create_connection((NODE_HOST, NODE_PORT), timeout=0.2):
            return True
    except OSError:
        return False


@stub("agents.node")
def node(context):
    """llmman's /llmman/node answers on loopback, as a ready daemon does."""
    if node_answers():
        # The previous scenario's server is being swept; wait it out rather
        # than asserting against a stranger's endpoint.
        deadline = time.monotonic() + 5
        while node_answers() and time.monotonic() < deadline:
            time.sleep(0.1)
        if node_answers():
            raise RuntimeError(f"{NODE_HOST}:{NODE_PORT} is already served by another process")
    script = os.path.join(context.scenario_dir, "llmman-node.py")
    with open(script, "w", encoding="utf-8") as handle:
        handle.write(NODE_SERVER)
    log = open(os.path.join(context.scenario_dir, "llmman-node.log"), "wb")
    process = subprocess.Popen(
        [sys.executable, script, os.path.join(calls_dir(context), "node-requests"),
         str(NODE_MEMORY), alias_path(context)],
        env=context.launch_env,
        stdout=log,
        stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    log.close()
    deadline = time.monotonic() + 10
    while not node_answers():
        if process.poll() is not None or time.monotonic() > deadline:
            process.kill()
            raise RuntimeError(f"the llmman node stub never listened on {NODE_HOST}:{NODE_PORT}")
        time.sleep(0.05)


def registration_path(context):
    return os.path.join(context.home, ".config", "hive", "contributor.env")


@stub("agents.contribute.ready")
def contribute_ready(context):
    """Preflight passes: xdg-terminal-exec, ujust with contribute, podman, and registration exist."""
    recorder(context, "xdg-terminal-exec")
    recorder(
        context,
        "ujust",
        """
        if [ "$1" = "--summary" ]; then
            echo "benchmark update contribute clean-system"
            exit 0
        fi
        exit 0
        """,
    )
    recorder(context, "podman")
    write_file(registration_path(context), "HIVE_HUB=https://example.com/api/contribute/ws\n")


@stub("agents.contribute.noreg")
def contribute_noreg(context):
    """Preflight passes except missing registration."""
    recorder(context, "xdg-terminal-exec")
    recorder(
        context,
        "ujust",
        """
        if [ "$1" = "--summary" ]; then
            echo "benchmark update contribute clean-system"
            exit 0
        fi
        exit 0
        """,
    )
    recorder(context, "podman")
