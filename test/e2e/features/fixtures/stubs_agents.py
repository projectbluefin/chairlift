"""Prelaunch stubs for the Agents page (features/agents.feature).

Agent Mode's state is read from three places (internal/aistack.Observe and
Healthy): whether an llmman executable resolves, whether ChairLift's user
unit exists, and whether 127.0.0.1:17434/llmman/node answers. Each stub here
controls one of them inside the scenario, so the page renders a chosen state
regardless of what the host or container ships.

Every fake executable records its argv to <scenario>/calls/<program>, which
is how scenarios prove a dry-run never reached llmman, systemctl, or brew.

The loopback node endpoint follows community PR #372's approach (mendezr): a
small Python HTTP server answering /llmman/node with a fixed memory figure.
It is spawned with the scenario's launch environment, so environment.py's
journal-path sweep kills it with the application's other descendants.
"""

import json
import os
import socket
import subprocess
import sys
import textwrap
import time

from stubs import fake_executable, stub

NODE_HOST = "127.0.0.1"
NODE_PORT = 17434
# 16 GiB: every family in aistack.OfflineCatalog has an eligible entry.
NODE_MEMORY = 17179869184

# Where the dogfooded Homebrew lives in the Dakota container and on GitHub's
# runners. Its bin directory ships a real llmman, so it is removed from PATH
# and a recording brew stands in; homebrew.ExecutablePath then resolves the
# fake, and aistack.Executable's "beside brew" fallback finds only what the
# scenario put in its own bin directory.
HOMEBREW_BIN_PARTS = ("/home/linuxbrew/", "/linuxbrew/.linuxbrew")

# Nothing on this page may need the internet to render deterministically.
# Model-family resolution tries huggingface.co before its offline catalog;
# a refused proxy makes that fail at once. Go's ProxyFromEnvironment never
# proxies loopback, so the node stub stays reachable.
BLACKHOLE_PROXY = "http://127.0.0.1:9"


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


def peer_store_path(context):
    return os.path.join(context.home, ".local", "share", "chairlift", "agent-mode-peers.json")


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


NODE_SERVER = r'''
import json, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

log_path, memory = sys.argv[1], int(sys.argv[2])


class Node(BaseHTTPRequestHandler):
    def do_GET(self):
        with open(log_path, "a", encoding="utf-8") as log:
            log.write(self.path + "\n")
        if self.path != "/llmman/node":
            self.send_error(404)
            return
        body = json.dumps({"memory": memory, "loaded": {}, "stored": {}}).encode()
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
        [sys.executable, script, os.path.join(calls_dir(context), "node-requests"), str(NODE_MEMORY)],
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


# One reachable peer (the node stub), one refused address, one disabled.
SEEDED_PEERS = [
    {"address": "localhost:17434", "enabled": True},
    {"address": "127.0.0.1:9", "enabled": True},
    {"address": "10.0.0.5", "enabled": False},
]


@stub("agents.peers")
def peers(context):
    """ChairLift's peer store already lists three machines."""
    write_file(peer_store_path(context), json.dumps({"peers": SEEDED_PEERS}, indent=2) + "\n")
