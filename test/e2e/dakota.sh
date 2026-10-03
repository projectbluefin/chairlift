#!/usr/bin/env bash
# Run test/e2e's Go tests inside the native Dakota environment.
#
# Usage: test/e2e/dakota.sh [go test flags...]
#   e.g. test/e2e/dakota.sh -v -run '^TestApplicationStartsInDryRun$'
#
# Every E2E test that needs a display runs here, never on the host:
# `make e2e`, `make e2e-atspi`, and `make screenshots` all call this script.
# What GTK, Libadwaita, and the accessibility tree do depends on the release
# Bluefin ships, so the tests run on that release, in the pinned Dakota
# image from dakota-image.sh, under a private headless Mutter
# (wayland_session.sh) and a private D-Bus session, with:
#   - the checkout mounted at its own host path, so every absolute path a
#     Makefile target hands in (build directory, schema directory, coverage
#     directory, artifact directories) means the same thing inside;
#   - /usr/share/chairlift masked by an empty tmpfs, so the image's packaged
#     configuration never outranks a test fixture;
#   - /proc/cmdline masked, so a composefs host's kernel arguments never make
#     the bootc reader look for deployments the container does not have;
#   - /usr/libexec/bootc-update-stage replaced by /usr/bin/false, so the
#     image's real stage helper is never what a fixture's update run reaches;
#   - the host's Go toolchain mounted read-only, with its module and build
#     caches shared;
#   - a venv from test/e2e/requirements-atspi.txt (created on first use);
#   - no host session bus, display, Wayland socket, or runtime directory.
#
# CHAIRLIFT_DAKOTA_IMAGE overrides the image for a local experiment only;
# dakota-image.sh rejects an override in CI.
#
# The image is trusted with a writable checkout and Go caches. Release
# binaries are built by goreleaser in a separate job from a fresh checkout,
# but its actions/setup-go step shares the e2e job's default cache key, so a
# Go build or module cache written inside the image can be restored into that
# build until the goreleaser job sets cache: false. Otherwise the only output
# that leaves a Dakota run for the repository is release-screenshots.yml's
# PNGs, which land through a reviewed pull request.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT/test/e2e/dakota-image.sh"
CACHE="${XDG_CACHE_HOME:-$HOME/.cache}"
REQUIREMENTS="$ROOT/test/e2e/requirements-atspi.txt"
GOROOT="$(go env GOROOT)"
GOMODCACHE="$(go env GOMODCACHE)"
GOCACHE="$(go env GOCACHE)"
mkdir -p "$CACHE" "$GOMODCACHE" "$GOCACHE"

command -v podman >/dev/null || { echo "podman is required" >&2; exit 1; }

# The venv is keyed on the pinned requirements and the image's interpreter,
# so a bumped pin or a Python upgrade in the Dakota image gets a fresh venv
# instead of silently running against the old one.
IMAGE_PYTHON="$(podman run --rm --pull=missing "$IMAGE" python3 --version)"
VENV_KEY="$(printf '%s\n%s\n' "$IMAGE_PYTHON" "$(sha256sum "$REQUIREMENTS" | cut -d' ' -f1)" | sha256sum | cut -c1-16)"
VENV="$CACHE/chairlift-atspi-venv-$VENV_KEY"
if [ ! -x "$VENV/bin/python" ]; then
    # Built with the container's interpreter so its site-packages (PyGObject,
    # pyatspi) are the ones the suite sees; into a temporary directory first
    # so an interrupted build never leaves a half-made venv under the key.
    rm -rf "$VENV.partial"
    podman run --rm --pull=missing --userns=keep-id --security-opt label=disable \
        --tmpfs /tmp:rw,mode=1777 -e HOME=/tmp \
        -v "$CACHE:$CACHE" -v "$ROOT:$ROOT:ro" "$IMAGE" \
        sh -c "python3 -m venv --system-site-packages '$VENV.partial' && '$VENV.partial/bin/python' -m pip install -q --require-hashes -r '$REQUIREMENTS'" \
        || { rm -rf "$VENV.partial"; exit 1; }
    # The suite only ever runs `python -m behave`, which resolves the venv
    # from its own location, so the rename is safe.
    mv "$VENV.partial" "$VENV"
fi

MOUNTS=(
    -v "$ROOT:$ROOT"
    -v "$CACHE:$CACHE"
    -v "$GOMODCACHE:$GOMODCACHE"
    -v "$GOCACHE:$GOCACHE"
    -v "$GOROOT:$GOROOT:ro"
)
ENVIRONMENT=(
    -e HOME=/tmp/home
    -e PATH="$GOROOT/bin:/usr/bin:/usr/sbin"
    -e GOMODCACHE="$GOMODCACHE" -e GOCACHE="$GOCACHE"
    -e GOTOOLCHAIN=local
    -e CHAIRLIFT_ATSPI_PYTHON="$VENV/bin/python"
    -e CHAIRLIFT_REQUIRE_ATSPI=1
)
# Directories and settings a Makefile target hands in. A directory outside the
# checkout is mounted at its own path, so the absolute path stays valid; a
# path handed in under two names (GOCOVERDIR and E2E_COVERDIR) is mounted once.
MOUNTED=()
for name in CHAIRLIFT_E2E_BUILD_DIR CHAIRLIFT_SCHEMA_DIR CHAIRLIFT_WALKTHROUGH_DIR \
    CHAIRLIFT_ATSPI_OUT GOCOVERDIR E2E_COVERDIR; do
    value="${!name:-}"
    [ -n "$value" ] || continue
    case "$value" in
        "$ROOT"|"$ROOT"/*) ;;
        *)
            mkdir -p "$value"
            seen=0
            for m in "${MOUNTED[@]+"${MOUNTED[@]}"}"; do
                if [ "$m" = "$value" ]; then seen=1; fi
            done
            if [ "$seen" = 0 ]; then
                MOUNTS+=(-v "$value:$value")
                MOUNTED+=("$value")
            fi
            ;;
    esac
    ENVIRONMENT+=(-e "$name=$value")
done
for name in CHAIRLIFT_ATSPI_TAGS CHAIRLIFT_ATSPI_KNOWN_ISSUES; do
    [ -n "${!name:-}" ] && ENVIRONMENT+=(-e "$name=${!name}")
done

exec podman run --rm --pull=missing --userns=keep-id --security-opt label=disable \
    --tmpfs /tmp:rw,mode=1777 \
    --mount type=bind,source=/dev/null,destination=/proc/cmdline,ro \
    --mount type=bind,source=/usr/bin/false,destination=/usr/libexec/bootc-update-stage,ro \
    --tmpfs /usr/share/chairlift:ro,notmpcopyup \
    "${MOUNTS[@]}" "${ENVIRONMENT[@]}" \
    -w "$ROOT" "$IMAGE" \
    sh -c 'mkdir -p "$HOME" && exec go test "$@" ./test/e2e' sh "$@"
