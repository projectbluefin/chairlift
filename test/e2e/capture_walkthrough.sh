#!/usr/bin/env bash
# Drive ChairLift through every page and capture one screenshot per page.
#
# Usage: capture_walkthrough.sh <chairlift-binary> <output-dir> <page-name>...
#
# Writes <output-dir>/<n>-<page>.xwd for each page, plus chairlift.log.
#
# The page names are passed in by walkthrough_test.go, sourced from
# internal/navigation so the script cannot drift from the application's real
# page order: page N is reached with the Alt+N accelerator navigation itself
# advertises.
#
# The application always runs with --dry-run. Every screenshot therefore
# reflects a session in which no state-changing operation could execute: the
# release-channel switch, developer mode, and gaming mode all short-circuit
# before pkexec or Flatpak. This is a rendering and navigation check, not a
# test of the mutations themselves.
#
# Modeled on tuna-os/gtk-office-suite's tests/gui/capture_walkthrough.sh —
# private Xvfb display, private D-Bus session, keyboard-only interaction —
# adapted to ChairLift's Go/puregotk stack and its existing readiness log
# markers.
set -euo pipefail

APP="${1:?usage: capture_walkthrough.sh <chairlift-binary> <output-dir> <page>...}"
OUTDIR="${2:?usage: capture_walkthrough.sh <chairlift-binary> <output-dir> <page>...}"
shift 2
PAGES=("$@")
[ ${#PAGES[@]} -gt 0 ] || { echo "no pages requested" >&2; exit 2; }

mkdir -p "$OUTDIR"
OUTDIR="$(cd "$OUTDIR" && pwd)"

WIDTH="${CHAIRLIFT_WALKTHROUGH_WIDTH:-1400}"
HEIGHT="${CHAIRLIFT_WALKTHROUGH_HEIGHT:-900}"
# A display number unlikely to collide with a developer's own session.
DISPLAY_NUM="${CHAIRLIFT_WALKTHROUGH_DISPLAY:-:97}"

LOG="$OUTDIR/chairlift.log"
: > "$LOG"

cleanup() {
    if [ -n "${APP_PID:-}" ]; then
        kill -TERM "-$APP_PID" 2>/dev/null || kill -TERM "$APP_PID" 2>/dev/null || true
        wait "$APP_PID" 2>/dev/null || true
    fi
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

# The same environment the dry-run startup smoke test uses, so the two agree
# on what a clean headless launch looks like.
export LANG=C LC_ALL=C NO_AT_BRIDGE=1 GTK_A11Y=none GSETTINGS_BACKEND=memory
# The Livery page reads its selections through `gsettings`, which needs
# ChairLift's schema on the search path. A source build has not run
# `make install`, so without this the page would capture its
# "settings unavailable" state instead of its controls. The memory backend
# above keeps every read on defaults, so the shot is deterministic.
if [ -n "${CHAIRLIFT_SCHEMA_DIR:-}" ]; then
  export GSETTINGS_SCHEMA_DIR="$CHAIRLIFT_SCHEMA_DIR"
fi
export HOME="$OUTDIR/home"
mkdir -p "$HOME"

# Render the Bluefin-family rows (release channel, developer mode, gaming)
# even though the runner is not a Bluefin system. Without this the
# walkthrough could only ever capture those rows hidden, and would verify
# nothing about them.
#
# The override is honored only in --dry-run, and only by the unprivileged
# read in internal/ublue — the privileged helper always resolves the real
# /usr/share/ublue-os/image-info.json. Dakota on its stable stream is used
# because it is the case where every row is both visible and switchable.
# Show the Powerwash / Factory Reset rows. reset_group ships disabled — both
# actions are irreversible — but the walkthrough exists to document every
# feature, including the ones an administrator has to opt into. Config, not a
# build-tag stub, is the intended mechanism: internal/config resolves a
# relative candidate alongside the executable first, so a file dropped next
# to the e2e binary applies to this capture only and leaves the shipped
# config.yml untouched.
cat > "$(dirname "$APP")/config.yml" <<'YAML'
maintenance_page:
  reset_group:
    enabled: true
YAML

if [ -z "${CHAIRLIFT_IMAGE_INFO:-}" ]; then
    CHAIRLIFT_IMAGE_INFO="$OUTDIR/image-info.json"
    cat > "$CHAIRLIFT_IMAGE_INFO" <<'JSON'
{
  "image-name": "dakota",
  "image-tag": "latest",
  "image-ref": "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota",
  "image-vendor": "projectbluefin",
  "image-flavor": "main"
}
JSON
fi
export CHAIRLIFT_IMAGE_INFO

# Render the automatic-updates switch even though the runner has no
# uupd.timer, for the same reason. The value is the pair of systemctl answers
# autoupdate.Classify consumes; "enabled,active" is the switch-on state.
: "${CHAIRLIFT_AUTO_UPDATES:=enabled,active}"
# An NVIDIA card on an Intel laptop, so the graphics-driver row renders with
# a switch to offer. 0x10de is NVIDIA's PCI vendor ID, 0x8086 Intel's.
: "${CHAIRLIFT_GPU_VENDORS:=0x8086,0x10de}"
export CHAIRLIFT_GPU_VENDORS

export CHAIRLIFT_AUTO_UPDATES


dbus-run-session -- "$APP" --dry-run >>"$LOG" 2>&1 &
APP_PID=$!

# Poll the application's own readiness markers — the same three the dry-run
# smoke test waits for — instead of guessing at a startup duration.
ready=0
for _ in $(seq 1 300); do
    if grep -q "Running in dry-run mode" "$LOG" \
        && grep -q "ChairLift activated" "$LOG" \
        && grep -q "app: window presented" "$LOG"; then
        ready=1
        break
    fi
    if ! kill -0 "$APP_PID" 2>/dev/null; then
        echo "ChairLift exited before becoming ready:" >&2
        cat "$LOG" >&2
        exit 1
    fi
    sleep 0.1
done
[ "$ready" = 1 ] || { echo "ChairLift did not become ready in 30s:" >&2; cat "$LOG" >&2; exit 1; }

# Give the compositor-free display a moment to finish the first paint. This
# is the one unavoidable fixed wait: there is no "drawn" signal to poll for
# from outside the process.
sleep 2

WINDOW="$(xdotool search --sync --onlyvisible --name . | head -n 1 || true)"
[ -n "$WINDOW" ] || { echo "no visible ChairLift window found on $DISPLAY" >&2; exit 1; }
xdotool windowactivate --sync "$WINDOW" 2>/dev/null || true

# Record the window's geometry so the Go side can crop the captured root
# image down to just the application. Reading it from the running window
# rather than hardcoding it keeps the crop correct if the default window size
# ever changes.
xdotool getwindowgeometry --shell "$WINDOW" > "$OUTDIR/window-geometry.env"

index=0
for page in "${PAGES[@]}"; do
    index=$((index + 1))
    # navigation compacts Alt+<number> over the visible pages in order, so
    # the Nth requested page is always Alt+N.
    xdotool key --window "$WINDOW" --clearmodifiers "alt+$index"
    sleep 1
    # xwd, not ImageMagick's import: import only captures when ImageMagick
    # was built with the X11 delegate, which is not guaranteed. xwd ships
    # with the same X utilities this script already needs, and
    # walkthrough_test.go decodes its output directly.
    xwd -root -silent -display "$DISPLAY" -out "$OUTDIR/$index-$page.xwd"
    echo "captured $index-$page.xwd"
done

echo "walkthrough complete"
