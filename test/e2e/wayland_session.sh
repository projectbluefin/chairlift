#!/usr/bin/env bash
# Run a command inside a private, headless native Wayland session.
#
# Usage: wayland_session.sh <command> [args...]
#
# Call it inside a private D-Bus session (`dbus-run-session -- wayland_session.sh
# ...`) with XDG_RUNTIME_DIR pointing at a private 0700 directory. The script
# starts, in that runtime directory:
#   - PipeWire and WirePlumber, which Mutter's ScreenCast API streams through
#     (wayland_remote.py's screenshot reads one frame from it);
#   - Mutter as a headless Wayland compositor (--no-x11) with one virtual
#     monitor of CHAIRLIFT_WAYLAND_SIZE (default 1280x800), the same compositor
#     GNOME on Bluefin runs, with its RemoteDesktop API for keyboard input.
# It then runs the command with GDK_BACKEND=wayland, XDG_SESSION_TYPE=wayland
# (dogtail picks its Wayland input path from it when imported), and
# WAYLAND_DISPLAY set to the compositor's absolute socket path, and stops
# everything afterwards.
#
# The socket path is absolute on purpose. A behave scenario launches the
# application with a per-scenario XDG_RUNTIME_DIR, and libwayland resolves a
# bare WAYLAND_DISPLAY name against whichever runtime directory the client
# has. An absolute path names this session's compositor and nothing else, so
# a developer's live compositor is unreachable whatever the client inherits.
#
# GTK and Mutter render in software (llvmpipe): CI runners have no GPU.
set -euo pipefail

[ "$#" -gt 0 ] || { echo "usage: wayland_session.sh <command> [args...]" >&2; exit 2; }
[ -n "${XDG_RUNTIME_DIR:-}" ] || { echo "XDG_RUNTIME_DIR must name a private runtime directory" >&2; exit 2; }
[ -n "${DBUS_SESSION_BUS_ADDRESS:-}" ] || { echo "run wayland_session.sh inside dbus-run-session" >&2; exit 2; }
case "$XDG_RUNTIME_DIR" in
    /run/user/*) echo "refusing the live session's runtime directory $XDG_RUNTIME_DIR" >&2; exit 2 ;;
esac
mkdir -p "$XDG_RUNTIME_DIR"
chmod 0700 "$XDG_RUNTIME_DIR"

SIZE="${CHAIRLIFT_WAYLAND_SIZE:-1280x800}"
NAME="chairlift-wayland-$$"
SOCKET="$XDG_RUNTIME_DIR/$NAME"
LOGDIR="${CHAIRLIFT_WAYLAND_LOG_DIR:-$XDG_RUNTIME_DIR}"
mkdir -p "$LOGDIR"

# Dakota keeps Mesa (libgallium, the DRI and GBM backends) under
# /usr/lib/<triplet>/GL/default/lib, which a booted system registers with the
# dynamic loader but the container image's /etc/ld.so.cache does not list.
# Without it Mutter's GPU-less renderer cannot load llvmpipe and crashes.
for gl in /usr/lib/*/GL/default/lib; do
    [ -d "$gl" ] || continue
    export LD_LIBRARY_PATH="$gl${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
    [ -d "$gl/dri" ] && export LIBGL_DRIVERS_PATH="$gl/dri"
    [ -d "$gl/gbm" ] && export GBM_BACKENDS_PATH="$gl/gbm"
done
export LIBGL_ALWAYS_SOFTWARE=1

PIDS=()
cleanup() {
    for ((i = ${#PIDS[@]} - 1; i >= 0; i--)); do
        kill "${PIDS[i]}" 2>/dev/null || true
    done
    for pid in "${PIDS[@]}"; do
        wait "$pid" 2>/dev/null || true
    done
}
trap cleanup EXIT

# The infrastructure runs without the caller's G_DEBUG: a test that makes
# GLib criticals fatal for the application must not turn a compositor
# warning into a crash of the session it is being tested in.
infra() { env -u G_DEBUG -u DISPLAY -u WAYLAND_DISPLAY "$@"; }

infra pipewire >"$LOGDIR/pipewire.log" 2>&1 &
PIDS+=($!)
infra wireplumber >"$LOGDIR/wireplumber.log" 2>&1 &
PIDS+=($!)
infra mutter --headless --wayland --no-x11 --virtual-monitor "$SIZE" \
    --wayland-display="$NAME" >"$LOGDIR/mutter.log" 2>&1 &
MUTTER_PID=$!
PIDS+=("$MUTTER_PID")

for _ in $(seq 1 200); do
    [ -S "$SOCKET" ] && break
    kill -0 "$MUTTER_PID" 2>/dev/null || { echo "mutter exited before opening $SOCKET:" >&2; cat "$LOGDIR/mutter.log" >&2; exit 1; }
    sleep 0.05
done
[ -S "$SOCKET" ] || { echo "mutter never opened $SOCKET:" >&2; cat "$LOGDIR/mutter.log" >&2; exit 1; }

unset DISPLAY || true
export WAYLAND_DISPLAY="$SOCKET"
export GDK_BACKEND=wayland
export XDG_SESSION_TYPE=wayland

set +e
"$@"
status=$?
set -e
exit "$status"
