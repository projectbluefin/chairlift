#!/usr/bin/env bash
# Run ChairLift's behave AT-SPI suite (test/e2e/features) headless.
#
# Usage: run_atspi.sh <chairlift-binary> <output-dir> [behave arguments...]
#
# The suite follows projectbluefin/testsuite's shape — behave features and
# steps driving the application through dogtail — but runs against a private
# Xvfb display and a private D-Bus session instead of a GNOME Shell VM, so it
# can gate every pull request on a stock GitHub runner.
#
# This script owns the display and the bus. features/environment.py owns the
# application: it launches a fresh ChairLift per scenario inside the bus this
# script provides, with its own HOME, XDG_RUNTIME_DIR, configuration fixture,
# and action journal, and stops it afterwards.
#
# Outputs under <output-dir>: junit/ (behave's JUnit XML), behave.log (the
# pretty-printed run), and scenarios/<slug>/ (per-scenario chairlift.log,
# journal.jsonl, and on failure tree.txt and screen.xwd).
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
FEATURES="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/features"
[ -d "$FEATURES" ] || { echo "features directory $FEATURES is missing" >&2; exit 1; }

WIDTH="${CHAIRLIFT_ATSPI_WIDTH:-1400}"
HEIGHT="${CHAIRLIFT_ATSPI_HEIGHT:-900}"

cleanup() {
    if [ -n "${XVFB_PID:-}" ]; then
        kill "$XVFB_PID" 2>/dev/null || true
        wait "$XVFB_PID" 2>/dev/null || true
    fi
    rm -f "${DISPLAY_FILE:-}"
}
trap cleanup EXIT

# -displayfd picks a free display number and reports it once the server
# accepts clients, so parallel runs (several contributors' suites, or the
# walkthrough beside this one) never collide and no readiness loop is needed.
DISPLAY_FILE="$(mktemp)"
Xvfb -displayfd 3 -screen 0 "${WIDTH}x${HEIGHT}x24" -nolisten tcp 3>"$DISPLAY_FILE" &
XVFB_PID=$!
for _ in $(seq 1 100); do
    [ -s "$DISPLAY_FILE" ] && break
    kill -0 "$XVFB_PID" 2>/dev/null || { echo "Xvfb exited before reporting a display" >&2; exit 1; }
    sleep 0.1
done
[ -s "$DISPLAY_FILE" ] || { echo "Xvfb never reported a display" >&2; exit 1; }
export DISPLAY=":$(tr -d '[:space:]' <"$DISPLAY_FILE")"

# Isolation from the developer's desktop. GTK 4 prefers Wayland whenever
# WAYLAND_DISPLAY is set, which would put the window on the live compositor
# where neither Xvfb nor dogtail's XTest input can reach it.
unset WAYLAND_DISPLAY XAUTHORITY AT_SPI_BUS_ADDRESS || true
export GDK_BACKEND=x11
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

if [ -n "${CHAIRLIFT_SCHEMA_DIR:-}" ]; then
    export GSETTINGS_SCHEMA_DIR="$CHAIRLIFT_SCHEMA_DIR"
fi

export CHAIRLIFT_ATSPI_APP="$APP"
export CHAIRLIFT_ATSPI_OUT="$OUTDIR"

PYTHON="${CHAIRLIFT_ATSPI_PYTHON:-python3}"

# One session bus for behave and every application it launches: the
# accessibility bus is reached through it, so two sessions would leave the
# steps looking at an empty tree.
set +e
dbus-run-session -- "$PYTHON" -m behave "$FEATURES" \
    --no-capture --no-capture-stderr \
    --format pretty --outfile "$OUTDIR/behave.log" \
    --format progress \
    --junit --junit-directory "$OUTDIR/junit" \
    "$@"
status=$?
set -e
exit "$status"
