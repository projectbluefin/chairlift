#!/usr/bin/env bash
# Run the behave AT-SPI suite inside the native Dakota environment.
#
# Usage: test/e2e/dakota_atspi.sh [behave tag expression]
#   e.g. test/e2e/dakota_atspi.sh @maintenance
#
# CHAIRLIFT_ATSPI_NO_BUILD=1 skips `make build-e2e schemas`, for several
# runs sharing one prebuilt binary (concurrent builds race on one output).
# CHAIRLIFT_ATSPI_BUILD_DIR (default build) names a build directory holding
# e2e/chairlift, for testing a binary built elsewhere.
# CHAIRLIFT_ATSPI_KNOWN_ISSUES=1 also runs @known_issue scenarios.
#
# For contributors on a Bluefin/Dakota host. The host itself cannot run the
# suite directly: /usr/share/chairlift/config.yml outranks every configuration
# fixture, and the suite must never touch the live desktop session. This runs
# the same Go gate CI runs (TestATSPIBehaveSuite) in
# ghcr.io/projectbluefin/dakota:testing with:
#   - the checkout at /workspace,
#   - /usr/share/chairlift masked by an empty tmpfs,
#   - Homebrew mounted read-only for Xvfb (brew install xorg-server),
#   - the host's Go toolchain mounted read-only,
#   - a venv from test/e2e/requirements-atspi.txt (created on first use),
#   - no host session bus, display, Wayland socket, or runtime directory.
#
# Artifacts land in build/atspi/<tags>/ (JUnit, behave.log, per-scenario
# logs, and tree.txt/screen.xwd for failed scenarios), one directory per tag
# expression so concurrent runs do not collide.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
IMAGE="${CHAIRLIFT_DAKOTA_IMAGE:-ghcr.io/projectbluefin/dakota:testing}"
BREW="${HOMEBREW_PREFIX:-/home/linuxbrew/.linuxbrew}"
CACHE="${XDG_CACHE_HOME:-$HOME/.cache}"
REQUIREMENTS="$ROOT/test/e2e/requirements-atspi.txt"
GOROOT="$(go env GOROOT)"
GOMODCACHE="$(go env GOMODCACHE)"
GOCACHE="$(go env GOCACHE)"
mkdir -p "$GOMODCACHE" "$GOCACHE"
TAGS="${1:-}"

command -v podman >/dev/null || { echo "podman is required" >&2; exit 1; }
[ -x "$BREW/bin/Xvfb" ] || { echo "Xvfb not found under $BREW; brew install xorg-server" >&2; exit 1; }

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
        -v "$CACHE:$CACHE" -v "$ROOT:/workspace:ro" "$IMAGE" \
        sh -c "python3 -m venv --system-site-packages '$VENV.partial' && '$VENV.partial/bin/python' -m pip install -q --require-hashes -r /workspace/test/e2e/requirements-atspi.txt" \
        || { rm -rf "$VENV.partial"; exit 1; }
    # The suite only ever runs `python -m behave`, which resolves the venv
    # from its own location, so the rename is safe.
    mv "$VENV.partial" "$VENV"
fi

if [ -z "${CHAIRLIFT_ATSPI_NO_BUILD:-}" ]; then
    make -C "$ROOT" build-e2e schemas >/dev/null
fi

RUN="$(printf '%s' "${TAGS:-all}" | tr -cs 'A-Za-z0-9' '-' | sed 's/^-//; s/-$//')"
BUILD_DIR="${CHAIRLIFT_ATSPI_BUILD_DIR:-build}"
OUT="build/atspi/$RUN"
rm -rf "${ROOT:?}/$OUT"
mkdir -p "$ROOT/$OUT"

exec podman run --rm --pull=missing --userns=keep-id --security-opt label=disable \
    --tmpfs /tmp:rw,mode=1777 \
    --tmpfs /usr/share/chairlift:ro,notmpcopyup \
    -v "$ROOT:/workspace" \
    -v "$BREW:$BREW:ro" \
    -v "$CACHE:$CACHE" \
    -v "$GOMODCACHE:$GOMODCACHE" \
    -v "$GOCACHE:$GOCACHE" \
    -v "$GOROOT:$GOROOT:ro" \
    -e HOME=/tmp/home \
    -e PATH="$GOROOT/bin:/usr/bin:/usr/sbin:$BREW/bin" \
    -e GOMODCACHE="$GOMODCACHE" -e GOCACHE="$GOCACHE" \
    -e GOTOOLCHAIN=local \
    -e CHAIRLIFT_E2E_BUILD_DIR="/workspace/$BUILD_DIR" \
    -e CHAIRLIFT_SCHEMA_DIR=/workspace/build/schemas \
    -e CHAIRLIFT_ATSPI_PYTHON="$VENV/bin/python" \
    -e CHAIRLIFT_ATSPI_OUT="/workspace/$OUT" \
    -e CHAIRLIFT_ATSPI_TAGS="$TAGS" \
    -e CHAIRLIFT_REQUIRE_ATSPI=1 \
    -e CHAIRLIFT_ATSPI_KNOWN_ISSUES="${CHAIRLIFT_ATSPI_KNOWN_ISSUES:-}" \
    -w /workspace "$IMAGE" \
    sh -c 'mkdir -p "$HOME" && go test -count=1 -v -timeout 40m -run "TestATSPI" ./test/e2e'
