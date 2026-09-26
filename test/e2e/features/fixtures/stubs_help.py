"""Prelaunch stubs for the Help destination (help.feature).

Everything the Help page reads from the host is stubbed here so its
assertions do not depend on what the runner happens to have installed:

* xdg-open and gtk-launch, which the resource rows and the Start Session
  button spawn, record their argv into the scenario directory instead of
  opening a browser or an application.
* brew records every invocation, so a scenario can prove Enhanced
  Troubleshooting's dry-run setup never reached Homebrew.
* The Goose/linux-mcp-server binaries and Goose's configuration file are what
  internal/troubleshoot.Detect reads; each state is seeded explicitly. The
  developer's mounted Homebrew ships real goose and linux-mcp-server
  binaries, so every troubleshooting stub first strips PATH entries that
  provide them.
"""

import os

from stubs import fake_executable, stub

URL_RECORD = "xdg-open.calls"
LAUNCH_RECORD = "gtk-launch.calls"
BREW_RECORD = "brew.calls"

# The programs internal/troubleshoot.Detect looks up on $PATH, plus the
# setup script, so a stripped PATH cannot let a host copy leak in.
TROUBLESHOOT_PROGRAMS = ("linux-mcp-server", "goose", "goose-desktop", "goose-mcp-setup")


def _recorder(context, program, record, exit_code=0):
    path = os.path.join(context.scenario_dir, record)
    fake_executable(
        context,
        program,
        f"""
printf '%s\\n' "$*" >> '{path}'
exit {exit_code}
""",
    )


@stub("help-xdg-open")
def xdg_open(context):
    """xdg-open that records the URL it was asked to open and succeeds."""
    _recorder(context, "xdg-open", URL_RECORD)


@stub("help-xdg-open-fails")
def xdg_open_fails(context):
    """xdg-open on a session with no URL handler: records, then exits 4."""
    _recorder(context, "xdg-open", URL_RECORD, exit_code=4)


@stub("help-gtk-launch")
def gtk_launch(context):
    """gtk-launch that records the desktop ID it was asked to start."""
    _recorder(context, "gtk-launch", LAUNCH_RECORD)


@stub("help-brew")
def brew(context):
    """A Homebrew that records every call and reports nothing installed.

    First on PATH, so internal/homebrew.ExecutablePath resolves it ahead of
    the mounted /home/linuxbrew install.
    """
    _recorder(context, "brew", BREW_RECORD)


def _strip_host_troubleshoot_tools(context):
    """Drop PATH entries (other than the stub bin) providing Goose tools."""
    entries = context.launch_env.get("PATH", "").split(os.pathsep)
    kept = []
    for entry in entries:
        if entry != context.stub_bin and any(
            os.access(os.path.join(entry, program), os.X_OK) for program in TROUBLESHOOT_PROGRAMS
        ):
            continue
        kept.append(entry)
    if context.stub_bin not in kept:
        kept.insert(0, context.stub_bin)
    context.launch_env["PATH"] = os.pathsep.join(kept)
    # The fallback brew lives in the stripped directory; keep brew itself
    # reachable through the stub bin rather than losing Homebrew entirely.
    brew(context)


@stub("help-no-goose")
def no_goose(context):
    """A Homebrew host with none of the troubleshooting pieces installed."""
    _strip_host_troubleshoot_tools(context)


def _install(context, *programs):
    _strip_host_troubleshoot_tools(context)
    for program in programs:
        fake_executable(context, program, "exit 0\n")


@stub("help-goose-cli")
def goose_cli(context):
    """linux-mcp-server and the goose CLI installed, no desktop app."""
    _install(context, "linux-mcp-server", "goose")


@stub("help-goose-all")
def goose_all(context):
    """linux-mcp-server, the goose CLI, and the Goose desktop app installed."""
    _install(context, "linux-mcp-server", "goose", "goose-desktop")


def _write_goose_config(context, body):
    directory = os.path.join(context.home, ".config", "goose")
    os.makedirs(directory, exist_ok=True)
    with open(os.path.join(directory, "config.yaml"), "w", encoding="utf-8") as handle:
        handle.write(body)


LINUX_TOOLS = """\
extensions:
  linux-tools:
    enabled: true
    type: stdio
    cmd: linux-mcp-server
    name: linux-tools
"""

# Goose configuration files that are present but do not wire the
# linux-tools extension, each the way a real file can fail to.
UNWIRED_CONFIGS = {
    # goose-mcp-setup exits 0 without touching an existing configuration,
    # leaving only whatever extensions were there before.
    "other-extension": """\
GOOSE_PROVIDER: gemini-cli
extensions:
  developer:
    enabled: true
    type: builtin
    cmd: developer
""",
    "disabled": """\
GOOSE_PROVIDER: gemini-cli
extensions:
  linux-tools:
    enabled: false
    type: stdio
    cmd: linux-mcp-server
""",
    "no-command": """\
GOOSE_PROVIDER: gemini-cli
extensions:
  linux-tools:
    enabled: true
    type: stdio
""",
    # A comment mentioning linux-tools is not an extension entry.
    "mention-only": """\
GOOSE_PROVIDER: gemini-cli
# linux-tools: cmd: linux-mcp-server
extensions: {}
""",
    "malformed": """\
GOOSE_PROVIDER: gemini-cli
extensions: [linux-tools
  cmd: linux-mcp-server
""",
}

# GOOSE_PROVIDER values the row must name, keyed by the stub suffix.
PROVIDERS = {
    "gemini": "gemini-cli",
    "ollama": "ollama",
    "anthropic": "anthropic",
    "none": None,
}


def _register_wired(suffix, provider):
    @stub(f"help-goose-wired-{suffix}")
    def wired(context):
        head = f"GOOSE_PROVIDER: {provider}\n" if provider else ""
        _write_goose_config(context, head + LINUX_TOOLS)

    wired.__doc__ = f"Goose configured with linux-tools and GOOSE_PROVIDER={provider!r}."


def _register_unwired(suffix, body):
    @stub(f"help-goose-unwired-{suffix}")
    def unwired(context):
        _write_goose_config(context, body)

    unwired.__doc__ = f"Goose configuration present but not wired ({suffix})."


for _suffix, _provider in PROVIDERS.items():
    _register_wired(_suffix, _provider)
for _suffix, _body in UNWIRED_CONFIGS.items():
    _register_unwired(_suffix, _body)
