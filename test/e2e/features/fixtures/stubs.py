"""Prelaunch stubs selected with @stub.<name> scenario tags.

A stub prepares the scenario before ChairLift starts: fake executables in
context.stub_bin (first on PATH), pre-seeded files under context.home, or
extra launch environment in context.launch_env. Stubs only ever touch the
scenario's own directory; the application still runs with --dry-run, so no
stub is needed to keep a mutation from happening.

Register a stub with @stub("name") in a stubs_<destination>.py module beside
this one; every such module is imported automatically. Keep each stub small
and say what real behaviour it stands in for.
"""

import importlib
import os
import pkgutil
import stat

_REGISTRY = {}
_LOADED = []


def stub(stub_name):
    def register(function):
        if stub_name in _REGISTRY:
            raise RuntimeError(f"stub {stub_name!r} registered twice")
        _REGISTRY[stub_name] = function
        return function

    return register


def _load_destination_stubs():
    if _LOADED:
        return
    _LOADED.append(True)
    here = os.path.dirname(os.path.abspath(__file__))
    for module in pkgutil.iter_modules([here]):
        if module.name.startswith("stubs_"):
            importlib.import_module(module.name)


def apply(stub_name, context):
    _load_destination_stubs()
    try:
        function = _REGISTRY[stub_name]
    except KeyError:
        raise RuntimeError(
            f"unknown stub @stub.{stub_name}; registered: {sorted(_REGISTRY)}"
        ) from None
    function(context)


def names():
    _load_destination_stubs()
    return sorted(_REGISTRY)


def fake_executable(context, program, script):
    """Write an executable shell script named program into the scenario PATH."""
    path = os.path.join(context.stub_bin, program)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write("#!/bin/sh\n" + script.lstrip())
    os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    return path
