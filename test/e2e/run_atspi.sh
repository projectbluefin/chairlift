#!/usr/bin/env bash
# Run ChairLift's behave AT-SPI suite (test/e2e/features) headless.
#
# Usage: run_atspi.sh <chairlift-binary> <output-dir> [behave arguments...]
#
# The suite follows projectbluefin/testsuite's shape — behave features and
# steps reading the application through dogtail — but runs against a private
# headless Mutter (test/e2e/wayland_session.sh) and a private D-Bus session
# instead of a GNOME Shell VM, so it can gate every pull request on a stock
# GitHub runner. Keyboard input goes through Mutter's RemoteDesktop API
# (features/lib/wayland_remote.py), as on a real Bluefin session.
#
# This script owns the compositor and the bus. features/environment.py owns
# the application: it launches a fresh ChairLift per scenario inside the bus
# this script provides, with its own HOME, XDG_RUNTIME_DIR, configuration
# fixture, and action journal, and stops it afterwards.
#
# Outputs under <output-dir>: junit/ (behave's JUnit XML), behave.log (the
# pretty-printed run), and scenarios/<slug>/ (per-scenario chairlift.log,
# journal.jsonl, and on failure tree.txt and screen.png).
#
# The application always runs with --dry-run, so no state-changing operation
# can execute while the tree is driven.
set -euo pipefail

APP="${1:?usage: run_atspi.sh <chairlift-binary> <output-dir> [behave args...]}"
OUTDIR="${2:?usage: run_atspi.sh <chairlift-binary> <output-dir> [behave args...]}"
shift 2

mkdir -p "$OUTDIR"
OUTDIR="$(cd "$OUTDIR" && pwd)"
APP="$(cd "$(dirname "$APP")" && pwd)/$(basename "$APP")"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FEATURES="$HERE/features"
[ -d "$FEATURES" ] || { echo "features directory $FEATURES is missing" >&2; exit 1; }

export CHAIRLIFT_WAYLAND_SIZE="${CHAIRLIFT_ATSPI_WIDTH:-1400}x${CHAIRLIFT_ATSPI_HEIGHT:-900}"

# Isolation from the developer's desktop: a private runtime directory and
# HOME, and no inherited accessibility bus. wayland_session.sh replaces any
# inherited WAYLAND_DISPLAY with its own compositor's absolute socket.
unset AT_SPI_BUS_ADDRESS || true
export LANG=C LC_ALL=C GSETTINGS_BACKEND=memory GDK_DEBUG=no-portals
# The accessibility bridge must be loaded, and the toolkit must publish to
# it, or there is no tree to read. The bus it publishes on lives inside the
# private dbus-run-session below, never on the developer's session.
export GTK_A11Y=atspi
unset NO_AT_BRIDGE || true
export XDG_RUNTIME_DIR="$OUTDIR/runtime"
mkdir -p "$XDG_RUNTIME_DIR"
chmod 0700 "$XDG_RUNTIME_DIR"
export HOME="$OUTDIR/home"
mkdir -p "$HOME"
export CHAIRLIFT_WAYLAND_LOG_DIR="$OUTDIR"

if [ -n "${CHAIRLIFT_SCHEMA_DIR:-}" ]; then
    export GSETTINGS_SCHEMA_DIR="$CHAIRLIFT_SCHEMA_DIR"
fi

export CHAIRLIFT_ATSPI_APP="$APP"
export CHAIRLIFT_ATSPI_OUT="$OUTDIR"

PYTHON="${CHAIRLIFT_ATSPI_PYTHON:-python3}"

# One session bus for the compositor, behave, and every application it
# launches: the accessibility bus and Mutter's input API are both reached
# through it, so two sessions would leave the steps looking at an empty tree.
set +e
dbus-run-session -- "$HERE/wayland_session.sh" "$PYTHON" -m behave "$FEATURES" \
    --no-capture --no-capture-stderr \
    --format pretty --outfile "$OUTDIR/behave.log" \
    --format progress \
    --junit --junit-directory "$OUTDIR/junit" \
    "$@"
status=$?
set -e
exit "$status"
