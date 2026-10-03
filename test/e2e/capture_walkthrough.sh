#!/usr/bin/env bash
# Drive ChairLift through every page and capture one screenshot per page.
#
# Usage: capture_walkthrough.sh <chairlift-binary> <output-dir> <page-name>...
#
# Run it inside wayland_session.sh (walkthrough_test.go does), with
# CHAIRLIFT_WAYLAND_SIZE set to the window's default size: Mutter then fits
# the window to the virtual monitor, so each capture is exactly the window.
#
# Writes <output-dir>/<n>-<page>.png for each page, plus chairlift.log.
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
# Keyboard-only interaction, like tuna-os/gtk-office-suite's
# tests/gui/capture_walkthrough.sh: keys go through Mutter's RemoteDesktop API
# and frames come from its ScreenCast API (features/lib/wayland_remote.py).
set -euo pipefail

APP="${1:?usage: capture_walkthrough.sh <chairlift-binary> <output-dir> <page>...}"
OUTDIR="${2:?usage: capture_walkthrough.sh <chairlift-binary> <output-dir> <page>...}"
shift 2
PAGES=("$@")
[ ${#PAGES[@]} -gt 0 ] || { echo "no pages requested" >&2; exit 2; }
# wayland_session.sh exports its private compositor's absolute socket path.
# A bare name (wayland-0) or a socket under /run/user is a live session's
# compositor, which this script would otherwise drive and capture.
case "${WAYLAND_DISPLAY:-}" in
    /run/user/*) echo "refusing the live session's compositor $WAYLAND_DISPLAY; run capture_walkthrough.sh inside wayland_session.sh" >&2; exit 2 ;;
    /*) ;;
    *) echo "run capture_walkthrough.sh inside wayland_session.sh" >&2; exit 2 ;;
esac

mkdir -p "$OUTDIR"
OUTDIR="$(cd "$OUTDIR" && pwd)"
REMOTE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/features/lib/wayland_remote.py"
# Keys go through dogtail's Mutter input backend, so the interpreter needs
# dogtail: the suite's venv when the caller names it.
PYTHON="${CHAIRLIFT_ATSPI_PYTHON:-python3}"
remote() { "$PYTHON" "$REMOTE" "$@"; }

LOG="$OUTDIR/chairlift.log"
: > "$LOG"

cleanup() {
    if [ -n "${APP_PID:-}" ]; then
        kill -TERM "$APP_PID" 2>/dev/null || true
        wait "$APP_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT

# The same environment the dry-run startup smoke test uses, so the two agree
# on what a clean headless launch looks like. wayland_session.sh has already
# pointed the application at its private compositor. G_DEBUG is set on the
# application's own command line below, so the capture helpers are not
# aborted by a critical the application is not responsible for.
export LANG=C LC_ALL=C NO_AT_BRIDGE=1 GTK_A11Y=none GSETTINGS_BACKEND=memory GDK_DEBUG=no-portals
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
# build-tag stub, is the intended mechanism. config.dev.yml is the first
# relative candidate; use its executable-adjacent copy so a checkout's own
# config.dev.yml cannot shadow the reset-group override. This changes only
# the tagged e2e binary's configuration, never the shipped config.yml.
cat > "$(dirname "$APP")/config.dev.yml" <<'YAML'
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
: "${CHAIRLIFT_CAPABILITIES:=image-descriptor,flatpak,brew,podman,bootc-stage}"
export CHAIRLIFT_CAPABILITIES


# Setup is explicit, including on a new account. The existing-page wizard
# starts at Features with its navigation footer; Escape dismisses it before
# the ordinary sidebar walk. Disposition writes are previews only.
G_DEBUG=fatal-criticals "$APP" --dry-run --setup >>"$LOG" 2>&1 &
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

# Give the compositor a moment to finish the first paint. This is the one
# unavoidable fixed wait: there is no "drawn" signal to poll for from outside
# the process.
sleep 2

# Capture the Features step and wizard footer under the stable setup name,
# which the walkthrough and installcheck referential gate both reference.
remote screenshot "$OUTDIR/0-setup.png"
echo "captured 0-setup.png"
remote key Escape
sleep 1

index=0
for page in "${PAGES[@]}"; do
    index=$((index + 1))
    # navigation compacts Alt+<number> over the visible pages in order, so
    # the Nth requested page is always Alt+N.
    remote key "<Alt>$index"
    sleep 1
    remote screenshot "$OUTDIR/$index-$page.png"
    echo "captured $index-$page.png"
done

echo "walkthrough complete"
