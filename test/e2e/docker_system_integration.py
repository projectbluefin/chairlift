"""Live system Docker and PolicyKit proof in the private Dakota fixture.

The fixture image ships no Docker. The real fixed helper downloads Homebrew's
signed formula index and the docker-engine bottle set itself, verifies them,
and installs a root-owned runtime under /var/lib/chairlift/docker, so this
gate proves the production install path rather than a baked package. The
enclosing rootless Podman namespace owns both native managers, HOME, the
system Docker socket, and all daemon data; no host bus/socket is used.
"""

import grp
import hashlib
import http.client
import json
import os
from pathlib import Path
import platform
import pty
import pwd
import re
import select
import shlex
import shutil
import signal
import socket
import struct
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE / "features" / "lib"))
os.environ.pop("NO_AT_BRIDGE", None)

import chairlift_atspi as atspi  # noqa: E402
import wayland_remote  # noqa: E402

READY = "System Docker is ready for this account."
NEEDS_LOGIN = "Log out and back in to use Docker with your new access."
INSTALLABLE = ("Enables Developer Mode first if needed, then installs Docker from verified Homebrew "
               "packages for all accounts. Administrator approval is required.")
INSTALLED_OFF = ("Enables Developer Mode first if needed, then turns on system Docker for all accounts. "
                 "Administrator approval is required.")
FIXTURE_PASSWORD = b"chairlift-private-fixture-only\n"
SOCKET = Path("/var/run/docker.sock")
VOLUME = "chairlift-system-integration"

# internal/dockerbrew's fixed layout and daemon-side formula set.
RUNTIME_ROOT = Path("/var/lib/chairlift/docker")
CURRENT = RUNTIME_ROOT / "current"
FORMULAE = ["docker-engine", "containerd", "runc", "tini", "libpathrs", "libseccomp"]
BINARIES = {"dockerd", "docker-proxy", "containerd", "containerd-shim-runc-v2", "runc", "docker-init"}
LIBRARIES = {"libpathrs.so.0", "libseccomp.so.2"}
UNIT_DIR = Path("/etc/systemd/system")
IMAGE_UNIT = Path("/usr/lib/systemd/system/docker.service")
MANAGED_MARKER = "# Managed by chairlift-helper"
LOADERS = {"x86_64": "/lib64/ld-linux-x86-64.so.2", "aarch64": "/lib/ld-linux-aarch64.so.1"}
ARCHES = {"x86_64": "x86_64_linux", "aarch64": "arm64_linux"}
RUNTIME_LINE = "chairlift-helper: docker runtime "
EXEC_LINE = "chairlift-helper: exec "

# The v26.10.10 rootless install docker_fixture.sh planted in this HOME.
ROOTLESS_UNIT = Path.home() / ".config" / "systemd" / "user" / "docker.service"


class AuthenticationAgent:
    """A real PolicyKit agent, bound to the actual GUI PID on a private PTY."""

    def __init__(self, pid, transcript):
        self.subject = pid
        self.path = transcript
        self.transcript = b""
        self.pending = b""
        self.prompts = 0

    def __enter__(self):
        ready, notify = os.pipe()
        os.set_inheritable(notify, True)
        self.pid, self.terminal = pty.fork()
        if self.pid == 0:
            os.close(ready)
            os.execvp("pkttyagent", ["pkttyagent", "--process", str(self.subject), "--notify-fd", str(notify)])
        os.close(notify)
        try:
            assert select.select([ready], [], [], 20)[0], "PolicyKit agent did not register"
            assert os.read(ready, 1) == b"", "unexpected agent readiness payload"
        except BaseException:
            self.__exit__(None, None, None)
            raise
        finally:
            os.close(ready)
        return self

    def pump(self, eof=False):
        if not select.select([self.terminal], [], [], 0.1)[0]:
            return False
        data = os.read(self.terminal, 4096)
        assert data, "authentication agent exited before the action settled"
        self.transcript += data
        self.pending += data
        prompt = re.search(rb"password:\s*", self.pending, re.IGNORECASE)
        if prompt is None:
            return False
        self.pending = self.pending[prompt.end():]
        self.prompts += 1
        # EOF uses the real agent; its observed exit may be refusal, not dismissal.
        os.write(self.terminal, b"\x04" if eof else FIXTURE_PASSWORD)
        return True

    def end_input(self):
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if self.pump(eof=True):
                assert b"dx-enable" in self.transcript, self.transcript
                return
        raise AssertionError(f"real authentication prompt missing: {self.transcript!r}")

    def authenticate_until(self, done, timeout=120):
        # An inactive-session auth_admin action can prompt separately for both
        # dx-enable and docker-enable. Keep the agent alive and answer every
        # actual prompt until the entire helper chain has settled.
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if done():
                return
            self.pump()
        raise AssertionError(f"authenticated action did not settle: {self.transcript!r}")

    def __exit__(self, *_):
        self.path.write_bytes(self.transcript.replace(FIXTURE_PASSWORD.strip(), b"[fixture password]"))
        try:
            os.kill(self.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        os.waitpid(self.pid, 0)
        os.close(self.terminal)


def helper_records(journal):
    return [json.loads(line) for line in journal.read_text().splitlines() if line.strip()]


def outcomes(journal):
    return [record for record in helper_records(journal) if "outcome" in record]


def expected_executed(action, account):
    """The helper-reported argv for a successful fixed-helper action on this fixture.

    docker-enable takes the Homebrew branch: Dakota ships no image docker.service,
    no docker group, and no SELinux, so restorecon never runs and groupadd does.
    A fresh install changed the runtime, so try-restart is reported even though
    the service was not yet running (it is a no-op then).
    """
    if action == "dx-enable":
        # docker does not exist yet: usermod is attempted, fails, and is skipped.
        return [["usermod", "-aG", group, account] for group in ("docker", "incus-admin", "libvirt", "dialout")]
    if action == "docker-enable":
        return [
            ["groupadd", "-f", "-r", "docker"],
            ["systemctl", "daemon-reload"],
            ["systemctl", "try-restart", "docker.service"],
            ["usermod", "-aG", "docker", account],
            ["systemctl", "enable", "--now", "docker.socket"],
            ["systemctl", "start", "docker.service"],
        ]
    if action == "docker-disable":
        return [["systemctl", "disable", "--now", "docker.socket", "docker.service"]]
    raise AssertionError(f"unexpected helper action {action}")


def assert_helpers(journal, actions):
    records = helper_records(journal)
    attempts = [record for record in records if "would_run" in record]
    results = outcomes(journal)
    assert len(records) == len(actions) * 2, records
    assert [record["action"] for record in attempts] == actions, records
    assert [record["action"] for record in results] == actions, records
    for record, action in zip(attempts, actions):
        assert record["would_run"] == ["pkexec", "/usr/bin/chairlift-helper", action], records
        assert not record.get("args"), record
    assert results[0]["outcome"] in {"cancelled", "refused"}, records
    assert all(record["outcome"] == "succeeded" for record in results[1:]), records
    account = pwd.getpwuid(os.getuid()).pw_name
    # The helper's reported execution is evidence, not independent process proof.
    for record in results:
        expected = expected_executed(record["action"], account) if record["outcome"] == "succeeded" else []
        assert record.get("executed", []) == expected, record


def run(*args, check=True):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False)
    print(f"$ {' '.join(map(str, args))}\n{result.stdout}", flush=True)
    if check and result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {args}:\n{result.stdout}")
    return result


def unit_states():
    return {unit: run("systemctl", "show", unit, "--property=LoadState,ActiveState,FragmentPath").stdout
            for unit in ("docker.socket", "docker.service")}


def assert_units(active):
    states = unit_states()
    for unit, state in states.items():
        assert "LoadState=loaded" in state and f"ActiveState={active}" in state, states
        assert f"FragmentPath={UNIT_DIR / unit}" in state, f"{unit} is not ChairLift's managed unit: {states}"


def assert_units_missing():
    states = unit_states()
    for state in states.values():
        assert "LoadState=not-found" in state and "ActiveState=inactive" in state, states
    for unit in ("docker.socket", "docker.service"):
        assert not (UNIT_DIR / unit).exists(), f"{unit} already exists before any install"
    assert not IMAGE_UNIT.exists(), "this fixture must exercise the Homebrew install branch, not image units"


def assert_managed_units():
    service = (UNIT_DIR / "docker.service").read_text()
    socket_unit = (UNIT_DIR / "docker.socket").read_text()
    for name, text in (("docker.service", service), ("docker.socket", socket_unit)):
        assert text.splitlines()[0] == MANAGED_MARKER, f"{name} lacks the managed marker:\n{text}"
        assert "linuxbrew" not in text, f"{name} points root at the user-writable Homebrew prefix:\n{text}"
    assert f"ExecStart={CURRENT}/bin/dockerd -H fd://" in service, service
    assert "ListenStream=/run/docker.sock" in socket_unit and "SocketGroup=docker" in socket_unit, socket_unit


def rootless_state():
    def show(verb):
        return run("systemctl", "--user", verb, "docker.service", check=False).stdout.strip()
    return show("is-active"), show("is-enabled")


def elf_dynamic(path):
    """PT_INTERP, DT_NEEDED, and DT_RPATH/DT_RUNPATH of a little-endian ELF64 file.

    Returns None for a file that is not ELF. Only what the loader acts on is
    read: Go binaries embed /home/linuxbrew build paths in plain strings,
    which never reach the loader and are deliberately ignored.
    """
    data = path.read_bytes()
    if data[:4] != b"\x7fELF":
        return None
    assert data[4] == 2 and data[5] == 1, f"{path}: expected little-endian ELF64"
    phoff, = struct.unpack_from("<Q", data, 0x20)
    phentsize, phnum = struct.unpack_from("<HH", data, 0x36)
    loads, interp, dynamic = [], None, None
    for index in range(phnum):
        p_type, _, p_offset, p_vaddr, _, p_filesz, _, _ = struct.unpack_from("<IIQQQQQQ", data, phoff + index * phentsize)
        if p_type == 1:
            loads.append((p_vaddr, p_offset, p_filesz))
        elif p_type == 3:
            interp = data[p_offset:p_offset + p_filesz].split(b"\0", 1)[0].decode()
        elif p_type == 2:
            dynamic = (p_offset, p_filesz)
    needed, rpaths = [], []
    if dynamic is not None:
        entries, strtab = [], None
        for offset in range(dynamic[0], dynamic[0] + dynamic[1], 16):
            tag, value = struct.unpack_from("<qQ", data, offset)
            if tag == 0:
                break
            if tag == 5:
                strtab = next(off + value - vaddr for vaddr, off, size in loads if vaddr <= value < vaddr + size)
            entries.append((tag, value))
        def string(value):
            assert strtab is not None, f"{path}: dynamic strings without DT_STRTAB"
            return data[strtab + value:].split(b"\0", 1)[0].decode()
        for tag, value in entries:
            if tag == 1:
                needed.append(string(value))
            elif tag in (15, 29):
                rpaths.append(string(value))
    return interp, needed, rpaths


def assert_root_owned(path):
    status = path.lstat()
    assert status.st_uid == 0 and status.st_gid == 0, f"{path} is not root:root"
    if not path.is_symlink():
        assert status.st_mode & 0o022 == 0, f"{path} is group/other writable: {oct(status.st_mode)}"


def runtime_evidence():
    """Prove the runtime is root-owned and loads nothing from Homebrew's prefix."""
    machine = platform.machine()
    assert CURRENT.is_symlink(), f"{CURRENT} is not the atomic current-set symlink"
    set_id = os.readlink(CURRENT)
    assert re.fullmatch(r"[0-9a-f]{16}", set_id), f"current points at {set_id!r}, not a set ID"
    root = RUNTIME_ROOT.resolve()
    tree = CURRENT.resolve()
    assert tree == root / set_id, (tree, root, set_id)
    assert_root_owned(root)
    assert set(os.listdir(root)) <= {"current", set_id, ".lock"}, f"stale runtime sets were kept: {os.listdir(root)}"
    manifest = json.loads((CURRENT / "manifest.json").read_text())
    assert manifest["arch"] == ARCHES[machine], manifest
    assert [bottle["name"] for bottle in manifest["bottles"]] == FORMULAE, manifest
    for bottle in manifest["bottles"]:
        assert bottle["url"] == f"https://ghcr.io/v2/homebrew/core/{bottle['name']}/blobs/sha256:{bottle['sha256']}", bottle
    assert {entry.name for entry in (tree / "bin").iterdir()} == BINARIES, sorted((tree / "bin").iterdir())
    assert {entry.name for entry in (tree / "lib").iterdir()} == LIBRARIES, sorted((tree / "lib").iterdir())
    hashes, loader = {}, LOADERS[machine]
    for directory, dirnames, filenames in os.walk(tree):
        for name in dirnames + filenames:
            path = Path(directory) / name
            assert_root_owned(path)
            assert not path.is_symlink(), f"{path}: installed runtime must hold regular files only"
        assert_root_owned(Path(directory))
        for name in filenames:
            path = Path(directory) / name
            hashes[str(path.relative_to(tree))] = hashlib.sha256(path.read_bytes()).hexdigest()
            parsed = elf_dynamic(path)
            if parsed is None:
                continue
            interp, needed, rpaths = parsed
            assert interp in (None, loader), f"{path}: PT_INTERP {interp!r}, want {loader}"
            assert all(rpath == "$ORIGIN/../lib" for rpath in rpaths), f"{path}: RPATH/RUNPATH {rpaths}"
            for value in [interp or "", *needed, *rpaths]:
                assert "linuxbrew" not in value and "@@HOMEBREW" not in value, f"{path}: loader reaches Homebrew: {value}"
            for library in needed:
                assert "/" not in library, f"{path}: absolute DT_NEEDED {library}"
    for binary in BINARIES:
        assert os.access(tree / "bin" / binary, os.X_OK), binary
    return set_id, manifest, hashes


def assert_daemon_from_runtime(manifest):
    pid = run("systemctl", "show", "docker.service", "--property=MainPID", "--value").stdout.strip()
    assert pid not in ("", "0"), "docker.service has no main process"
    argv = Path(f"/proc/{pid}/cmdline").read_bytes().split(b"\0")
    assert argv[0].decode() == f"{CURRENT}/bin/dockerd", f"system dockerd is not ChairLift's runtime: {argv}"
    engine = next(bottle for bottle in manifest["bottles"] if bottle["name"] == "docker-engine")
    return engine["version"]


class DockerHTTP(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX)
        self.sock.settimeout(5)
        self.sock.connect(str(SOCKET))


def assert_old_session_denied():
    connection = DockerHTTP("docker", timeout=5)
    try:
        connection.request("GET", "/_ping")
    except PermissionError:
        return
    finally:
        connection.close()
    raise AssertionError("the old session unexpectedly gained Docker socket access")


def docker_request(path, payload=None):
    # This driver keeps its original groups. A real sg child acquires the same
    # new group as the relaunched GUI; no chmod, mock socket or sudo is involved.
    args = ["curl", "--silent", "--show-error", "--fail-with-body", "--unix-socket", str(SOCKET),
            "--write-out", "\n%{http_code}"]
    if payload is not None:
        args += ["--header", "Content-Type: application/json", "--data", json.dumps(payload)]
    args += ["http://docker" + path]
    result = run("sg", "docker", "-c", shlex.join(args)).stdout
    body, status = result.rsplit("\n", 1)
    assert status == ("201" if payload is not None else "200"), result
    return body


def assert_socket_stopped():
    # systemd may retain the socket pathname after closing its listener.
    # Probe from the new-group session, so permission denial cannot fake stop.
    probe = """import errno, socket, sys
try:
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(sys.argv[1])
except OSError as error:
    if error.errno not in (errno.ENOENT, errno.ECONNREFUSED):
        raise
    print(f"Disabled system socket rejects connections: {error}")
else:
    raise AssertionError("disabled Docker still accepts socket connections")
"""
    run("sg", "docker", "-c", shlex.join([sys.executable, "-c", probe, str(SOCKET)]))


def stop(process):
    if process.poll() is None:
        process.send_signal(signal.SIGTERM)
    try:
        process.wait(timeout=15)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()


def run_docker_update(output, manifest):
    """Run the fixed helper's docker-update through real PolicyKit from this driver.

    pkexec authorizes its parent, this process, so the agent binds to our PID.
    The runtime is already current, so the signed index is fetched and verified
    again and the helper must change nothing.
    """
    transcript = output / "docker-update.txt"
    with transcript.open("wb") as stream, AuthenticationAgent(os.getpid(), output / "auth-update.txt") as agent:
        update = subprocess.Popen(["pkexec", "/usr/bin/chairlift-helper", "docker-update"],
                                  stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT)
        try:
            agent.authenticate_until(lambda: update.poll() is not None, timeout=600)
        finally:
            try:
                stop(update)
            except PermissionError:
                pass  # A still-running setuid helper is reported by the assertion below.
    text = transcript.read_text()
    print(f"$ pkexec /usr/bin/chairlift-helper docker-update\n{text}", flush=True)
    assert update.returncode == 0, f"docker-update failed ({update.returncode}):\n{text}"
    lines = text.splitlines()
    assert "docker runtime changed=false" in lines, f"a current runtime was replaced:\n{text}"
    reported = [line.removeprefix(RUNTIME_LINE) for line in lines if line.startswith(RUNTIME_LINE)]
    assert len(reported) == 1 and json.loads(reported[0]) == manifest, f"helper did not report the installed manifest:\n{text}"
    assert not any(line.startswith(EXEC_LINE) for line in lines), f"a no-op update ran system commands:\n{text}"


def main(binary, output):
    output = Path(output)
    account = pwd.getpwuid(os.getuid()).pw_name
    assert_units_missing()
    assert not SOCKET.exists(), "fixture already has a Docker socket"
    assert not RUNTIME_ROOT.exists(), "fixture already has a Docker runtime"
    try:
        grp.getgrnam("docker")
    except KeyError:
        pass
    else:
        raise AssertionError("fixture must start without a docker group")
    assert shutil.which("dockerd") is None and shutil.which("docker") is None, "fixture image must not ship Docker"
    assert shutil.which("code") is None, "this fixture explicitly covers missing native VS Code"
    assert shutil.which("brew") is None and not Path("/home/linuxbrew/.linuxbrew/bin/brew").exists(), "live system gate must not install unrelated Homebrew tools"
    assert "dockerd-rootless.sh" in ROOTLESS_UNIT.read_text(), "fixture did not plant the rootless unit"
    assert rootless_state() == ("active", "enabled"), "fixture rootless Docker is not running"
    staged = output / "chairlift"
    shutil.copy2(binary, staged)
    journal = output / "journal.jsonl"
    journal.write_text("")
    env = dict(os.environ)
    env.update(
        CHAIRLIFT_ACTION_JOURNAL=str(journal),
        XDG_CONFIG_HOME=str(Path(env["HOME"]) / ".config"),
        XDG_DATA_HOME=str(Path(env["HOME"]) / ".local" / "share"),
        XDG_CACHE_HOME=str(Path(env["HOME"]) / ".cache"),
    )
    if env.get("CHAIRLIFT_SCHEMA_DIR"):
        env["GSETTINGS_SCHEMA_DIR"] = env["CHAIRLIFT_SCHEMA_DIR"]
    log_path = output / "chairlift.log"
    log_path.write_text("")
    process = None
    subject = None
    app = None
    def launch(new_login=False, docker_gid=None):
        nonlocal process, subject, app
        offset = log_path.stat().st_size if log_path.exists() else 0
        pidfile = output / "new-login-app.pid"
        command = [str(staged)]
        if new_login:
            pidfile.unlink(missing_ok=True)
            command = ["sg", "docker", "-c", f"echo $$ > {shlex.quote(str(pidfile))}; exec {shlex.quote(str(staged))}"]
        with log_path.open("ab") as log:
            process = subprocess.Popen(command, cwd=output, env=env, stdout=log, stderr=subprocess.STDOUT)
        def started():
            assert process.poll() is None, f"ChairLift exited:\n{log_path.read_text()}"
            return "app: window presented" in log_path.read_text()[offset:]
        assert atspi.poll(started, timeout=60), "ChairLift never presented its window"
        log = log_path.read_text()[offset:]
        assert "Running in dry-run mode" not in log, "live integration accidentally ran in preview"
        assert f"Loaded config from {(output / 'config.dev.yml').resolve()}" in log
        subject = int(pidfile.read_text()) if new_login else process.pid
        if new_login:
            status = Path(f"/proc/{subject}/status").read_text()
            gids = [int(value) for line in status.splitlines() if line.startswith(("Gid:", "Groups:")) for value in line.split()[1:]]
            assert docker_gid in gids, "sg did not give the actual GUI Docker group access"
            (output / "new-login-process-status.txt").write_text(status)
        app = atspi.find_application(timeout=60)
        rows = atspi.poll(lambda: atspi.sidebar_rows(app))
        index = next(i for i, row in enumerate(rows, 1) if atspi.label_text(row) == "Features")
        atspi.press(f"<Alt>{index}")

    def docker_row():
        return atspi.row_containing(atspi.page_root(app), "Enable Docker", timeout=1)

    def toggle():
        return atspi.find(docker_row(), lambda node: atspi.role(node) == "switch", "Docker switch", timeout=1)

    def developer_toggle():
        row = atspi.row_containing(atspi.page_root(app), "Developer Mode", timeout=1)
        return atspi.find(row, lambda node: atspi.role(node) == "switch", "Developer Mode switch", timeout=1)

    def stage_has(title, text):
        group = atspi.find(atspi.page_root(app), lambda node: atspi.role(node) == "grouping" and atspi.name(node) == "Setup activity", "Setup activity", timeout=1)
        row = atspi.row_containing(group, title, timeout=1)
        return text in atspi.all_text_under(row)

    try:
        launch()
        assert atspi.poll(lambda: atspi.sensitive(toggle()) and not atspi.checked(toggle()) and INSTALLABLE in atspi.all_text_under(docker_row())), "not-installed system Docker was not offered as installable"
        assert atspi.poll(lambda: atspi.sensitive(developer_toggle()) and not atspi.checked(developer_toggle())), "fixture must start with Developer Mode off"
        with AuthenticationAgent(subject, output / "auth-blocked.txt") as agent:
            atspi.activate(toggle())
            agent.end_input()
            assert atspi.poll(lambda: atspi.sensitive(toggle()) and not atspi.checked(toggle()) and atspi.sensitive(developer_toggle()) and not atspi.checked(developer_toggle()), timeout=45), "blocked promotion did not restore both controls"
        assert_helpers(journal, ["dx-enable"])
        assert_units_missing()
        assert not SOCKET.exists(), "blocked promotion activated Docker"
        assert not RUNTIME_ROOT.exists(), "blocked authentication installed a runtime"
        assert rootless_state() == ("active", "enabled"), "blocked authentication retired rootless Docker"
        refusal = outcomes(journal)[0]
        if refusal.get("exit_code") == 127:
            assert refusal["outcome"] == "refused" and "Not authorized" in log_path.read_text(), refusal
            expected_status = "Failed"
        else:
            assert refusal.get("exit_code") == 126 or "Request dismissed" in log_path.read_text(), refusal
            expected_status = "Canceled"
        assert atspi.poll(lambda: stage_has("Developer access", expected_status)), f"authentication refusal did not report {expected_status}"
        wayland_remote.screenshot(str(output / "docker-auth-blocked.png"))

        with AuthenticationAgent(subject, output / "auth-enabled.txt") as agent:
            atspi.activate(toggle())
            # docker-enable downloads and verifies the signed index and bottles.
            agent.authenticate_until(lambda: len(outcomes(journal)) == 3, timeout=900)
            assert agent.prompts >= 1, "enable never authenticated through the real agent"
        assert_helpers(journal, ["dx-enable", "dx-enable", "docker-enable"])
        assert atspi.poll(lambda: atspi.checked(toggle()) and atspi.sensitive(toggle()) and NEEDS_LOGIN in atspi.all_text_under(docker_row()), timeout=60), "new group grant was not shown as awaiting login"
        assert atspi.poll(lambda: stage_has("Verify readiness", "Awaiting new login")), "verification invented readiness for the old session"
        assert atspi.poll(lambda: stage_has("VS Code setup", "Skipped")), "missing native code was not reported as a skipped optional step"
        assert "VS Code setup couldn't finish" not in atspi.all_text_under(atspi.main_window(app)), "a skipped editor was reported as a failure"
        assert_units("active")
        assert_managed_units()
        set_id, manifest, runtime = runtime_evidence()
        (output / "docker-runtime-manifest.json").write_text(json.dumps(manifest, indent=2))
        engine_version = assert_daemon_from_runtime(manifest)
        assert SOCKET.stat().st_uid == 0 and SOCKET.stat().st_gid == grp.getgrnam("docker").gr_gid, "socket is not system-owned Docker"
        assert account in grp.getgrnam("docker").gr_mem, "docker-enable did not grant the account"
        assert_old_session_denied()
        assert atspi.checked(developer_toggle()), "confirmed parent promotion was lost"
        # The v26.10.10 rootless daemon is retired, not deleted: the user's unit file stays.
        assert rootless_state() == ("inactive", "disabled"), f"rootless Docker kept running: {rootless_state()}"
        assert ROOTLESS_UNIT.exists(), "rootless cleanup deleted the user's unit file"
        wayland_remote.screenshot(str(output / "docker-awaiting-login.png"))

        stop(process)
        assert atspi.poll(lambda: not any(atspi.name(node) in atspi.APPLICATION_NAMES for node in atspi.children(atspi._tree().root)), timeout=20), "old application remained after shutdown"
        docker_gid = grp.getgrnam("docker").gr_gid
        launch(new_login=True, docker_gid=docker_gid)
        assert atspi.poll(lambda: atspi.checked(toggle()) and atspi.sensitive(toggle()) and READY in atspi.all_text_under(docker_row()), timeout=60), "new session did not observe real Docker readiness"
        assert docker_request("/_ping").strip() == "OK", "real system daemon did not answer HTTP 200 OK"
        info = json.loads(docker_request("/info"))
        assert not any(option.startswith("name=rootless") for option in info["SecurityOptions"]), info
        assert info["DockerRootDir"] == "/var/lib/docker", info
        assert re.match(rf"{re.escape(engine_version)}(\b|$)", info["ServerVersion"]), \
            f"daemon {info['ServerVersion']} is not the installed docker-engine {engine_version}"
        (output / "docker-info.json").write_text(json.dumps(info, indent=2))
        volume = json.loads(docker_request("/volumes/create", {"Name": VOLUME}))
        assert volume["Name"] == VOLUME and volume["Mountpoint"] == f"/var/lib/docker/volumes/{VOLUME}/_data", volume
        (output / "docker-volume.json").write_text(json.dumps(volume, indent=2))
        assert atspi.checked(developer_toggle()), "new login lost Developer Mode"
        assert_helpers(journal, ["dx-enable", "dx-enable", "docker-enable"])
        wayland_remote.screenshot(str(output / "docker-ready.png"))

        with AuthenticationAgent(subject, output / "auth-disabled.txt") as agent:
            atspi.activate(toggle())
            agent.authenticate_until(lambda: len(outcomes(journal)) == 4)
        assert atspi.poll(lambda: not atspi.checked(toggle()) and atspi.sensitive(toggle()) and INSTALLED_OFF in atspi.all_text_under(docker_row()), timeout=60), "GUI did not observe system Docker disable"
        assert_units("inactive")
        assert_socket_stopped()
        assert_units("inactive")  # A connection must not socket-activate Docker.
        assert runtime_evidence() == (set_id, manifest, runtime), "disable changed the installed runtime"
        assert_managed_units()
        assert atspi.checked(developer_toggle()) and atspi.sensitive(developer_toggle()), "Docker disable demoted Developer Mode"
        assert_helpers(journal, ["dx-enable", "dx-enable", "docker-enable", "docker-disable"])
        assert account in grp.getgrnam("docker").gr_mem, "disable removed account access"
        wayland_remote.screenshot(str(output / "docker-disabled.png"))

        run_docker_update(output, manifest)
        assert runtime_evidence() == (set_id, manifest, runtime), "a no-op docker-update changed the runtime"
        assert_units("inactive")  # Updating must not start a daemon the user turned off.
        print(f"LIVE DOCKER PROOF: auth {refusal['outcome']} (exit {refusal['exit_code']}, setup {expected_status}) blocks child -> "
              f"dx-enable then docker-enable installs verified Homebrew docker-engine {engine_version} as root-owned set {set_id} "
              "with managed units, a new docker group, and retired rootless Docker -> awaiting new login with VS Code skipped -> "
              "real sg login observes HTTP 200 rootful system Docker -> disable retains runtime, access and parent -> "
              "docker-update reports changed=false; root fixture verifies retained volume", flush=True)
    except BaseException:
        if app is not None:
            try:
                with (output / "tree.txt").open("w") as dump:
                    atspi.dump(atspi.main_window(app), dump)
                wayland_remote.screenshot(str(output / "failure.png"))
            except Exception as error:
                print(f"failure accessibility/screenshot unavailable: {error}", flush=True)
        raise
    finally:
        run("journalctl", "--unit", "docker.service", "--unit", "docker.socket", "--no-pager", check=False)
        if process is not None:
            if subject is not None and subject != process.pid:
                try:
                    os.kill(subject, signal.SIGTERM)
                except ProcessLookupError:
                    pass
            stop(process)


if __name__ == "__main__":
    main(*sys.argv[1:])
