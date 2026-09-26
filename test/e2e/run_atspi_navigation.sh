#!/usr/bin/env bash
# Drive ChairLift through every page with accessibility enabled and record
# what the AT-SPI tree reported.
#
# Usage: run_atspi_navigation.sh <chairlift-binary> <output-dir> \
#            <page-name> <page-title> [<page-name> <page-title>...]
#
# Writes <output-dir>/atspi-results.txt (the probe's records),
# chairlift.log (the application's output) and probe.log (the probe's
# stderr).
#
# The name/title pairs are passed in by atspi_navigation_test.go, sourced
# from internal/navigation, so the script cannot drift from the
# application's real page order.
#
# The application always runs with --dry-run, so no state-changing operation
# can execute while the tree is read.
#
# Relationship to capture_walkthrough.sh: that script shares this one's
# private Xvfb display, private D-Bus session and keyboard-only interaction,
# and deliberately runs with a11y switched off (GTK_A11Y=none,
# NO_AT_BRIDGE=1) for hermeticity, as ADR-0008 describes for the startup
# contract. Accessibility cannot be asserted with the bridge disabled, so
# this script turns it on — and keeps the hermeticity by putting the
# accessibility bus inside the same private session bus the application
# gets, never on the developer's or runner's own session.
set -euo pipefail

APP="${1:?usage: run_atspi_navigation.sh <chairlift-binary> <output-dir> [<page> <title>... | --probe <probe-script> [args...]]}"
OUTDIR="${2:?usage: run_atspi_navigation.sh <chairlift-binary> <output-dir> [<page> <title>... | --probe <probe-script> [args...]]}"
shift 2

PROBE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/atspi_probe.py"
PROBE_ARGS=()

if [ "${1:-}" = "--probe" ]; then
    shift
    [ $# -gt 0 ] || { echo "--probe requires a script path" >&2; exit 2; }
    PROBE="$1"
    shift
    PROBE_ARGS=("$@")
else
    PAIRS=("$@")
    [ ${#PAIRS[@]} -gt 0 ] || { echo "no pages requested" >&2; exit 2; }
    [ $((${#PAIRS[@]} % 2)) -eq 0 ] || { echo "pages must be <name> <title> pairs" >&2; exit 2; }
    PROBE_ARGS=("${PAIRS[@]}")
fi
mkdir -p "$OUTDIR"
OUTDIR="$(cd "$OUTDIR" && pwd)"

WIDTH="${CHAIRLIFT_ATSPI_WIDTH:-1400}"
HEIGHT="${CHAIRLIFT_ATSPI_HEIGHT:-900}"
# A display number unlikely to collide with a developer's own session, and
# distinct from the screenshot walkthrough's so the two can run together.
DISPLAY_NUM="${CHAIRLIFT_ATSPI_DISPLAY:-:96}"

LOG="$OUTDIR/chairlift.log"
PROBE_LOG="$OUTDIR/probe.log"
RESULTS="$OUTDIR/atspi-results.txt"
: > "$LOG"
: > "$PROBE_LOG"
: > "$RESULTS"

cleanup() {
    if [ -n "${XVFB_PID:-}" ]; then
        kill "$XVFB_PID" 2>/dev/null || true
        wait "$XVFB_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT

Xvfb "$DISPLAY_NUM" -screen 0 "${WIDTH}x${HEIGHT}x24" -nolisten tcp &
XVFB_PID=$!
export DISPLAY="$DISPLAY_NUM"

# Wait for the display to accept clients rather than sleeping a fixed time.
for _ in $(seq 1 100); do
    if xdpyinfo >/dev/null 2>&1; then break; fi
    sleep 0.1
done
xdpyinfo >/dev/null 2>&1 || { echo "Xvfb on $DISPLAY_NUM never became ready" >&2; exit 1; }

export LANG=C LC_ALL=C GSETTINGS_BACKEND=memory GDK_DEBUG=no-portals
# The one deliberate difference from the walkthrough's environment: the
# accessibility bridge must be loaded, and the toolkit must publish to it,
# or there is no tree to read. G_DEBUG=fatal-criticals is left off here
# because the bridge's own warnings on a runner without a session
# accessibility service would abort the application before the probe could
# say anything useful about it.
export GTK_A11Y=atspi
unset NO_AT_BRIDGE || true

if [ -n "${CHAIRLIFT_SCHEMA_DIR:-}" ]; then
  export GSETTINGS_SCHEMA_DIR="$CHAIRLIFT_SCHEMA_DIR"
fi
: "${CHAIRLIFT_CAPABILITIES:=image-descriptor,flatpak,brew,podman,bootc-stage}"
export CHAIRLIFT_CAPABILITIES
export HOME="$OUTDIR/home"
export XDG_CONFIG_HOME="$HOME/.config"
export XDG_DATA_HOME="$HOME/.local/share"
export XDG_CACHE_HOME="$HOME/.cache"
export XDG_RUNTIME_DIR="$OUTDIR/runtime"
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME"
mkdir -p "$XDG_RUNTIME_DIR"
chmod 0700 "$XDG_RUNTIME_DIR"
[ -r "$PROBE" ] || { echo "probe $PROBE is missing" >&2; exit 1; }

# The application and the probe must share one accessibility bus, which is
# reached through one session bus. Running both inside a single
# dbus-run-session is what guarantees that; two sessions would leave the
# probe looking at an empty bus.
export CHAIRLIFT_ATSPI_APP="$APP"
export CHAIRLIFT_ATSPI_LOG="$LOG"
export CHAIRLIFT_ATSPI_PROBE="$PROBE"
export CHAIRLIFT_ATSPI_PROBE_LOG="$PROBE_LOG"
export CHAIRLIFT_ATSPI_RESULTS="$RESULTS"
export CHAIRLIFT_ATSPI_OUTDIR="$OUTDIR"

dbus-run-session -- bash -eu -o pipefail -c '
    PRELAUNCH_PID=
    if [ -n "${CHAIRLIFT_ATSPI_PRELAUNCH_HOOK:-}" ]; then
        if [ -f "$CHAIRLIFT_ATSPI_PRELAUNCH_HOOK" ]; then
            # shellcheck disable=SC1090
            source "$CHAIRLIFT_ATSPI_PRELAUNCH_HOOK"
        else
            echo "pre-launch hook $CHAIRLIFT_ATSPI_PRELAUNCH_HOOK not found" >&2
            exit 1
        fi
    fi

    "$CHAIRLIFT_ATSPI_APP" --dry-run >>"$CHAIRLIFT_ATSPI_LOG" 2>&1 &
    APP_PID=$!

    finish() {
        if [ -n "${PRELAUNCH_PID:-}" ]; then
            kill -TERM "$PRELAUNCH_PID" 2>/dev/null || true
            wait "$PRELAUNCH_PID" 2>/dev/null || true
        fi
        kill -TERM "$APP_PID" 2>/dev/null || true
        wait "$APP_PID" 2>/dev/null || true
    }
    trap finish EXIT
    # Poll the application own readiness markers — the same three the
    # dry-run smoke test waits for — instead of guessing at a startup
    # duration.
    ready=0
    for _ in $(seq 1 300); do
        if grep -q "Running in dry-run mode" "$CHAIRLIFT_ATSPI_LOG" \
            && grep -q "ChairLift activated" "$CHAIRLIFT_ATSPI_LOG" \
            && grep -q "app: window presented" "$CHAIRLIFT_ATSPI_LOG"; then
            ready=1
            break
        fi
        if ! kill -0 "$APP_PID" 2>/dev/null; then
            echo "ChairLift exited before becoming ready:" >&2
            cat "$CHAIRLIFT_ATSPI_LOG" >&2
            exit 1
        fi
        sleep 0.1
    done
    [ "$ready" = 1 ] || { echo "ChairLift did not become ready in 30s:" >&2; cat "$CHAIRLIFT_ATSPI_LOG" >&2; exit 1; }

    # Presenting the window and registering its widgets on the
    # accessibility bus are separate events, and only the first has a log
    # marker. This is the one unavoidable fixed wait; the probe still
    # retries its own lookups on top of it.
    sleep 2

    python3 "$CHAIRLIFT_ATSPI_PROBE" "$@" \
        >"$CHAIRLIFT_ATSPI_RESULTS" 2>"$CHAIRLIFT_ATSPI_PROBE_LOG"
' probe "${PROBE_ARGS[@]}"

echo "atspi probe complete"
