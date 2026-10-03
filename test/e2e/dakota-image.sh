#!/usr/bin/env bash
# Shared image selection for Dakota E2E/release harnesses. Source before
# invoking podman. Roll procedure: docs/skills/multi-arch-digest-pinning/SKILL.md.
# Resolved from ghcr.io/projectbluefin/dakota:testing on 2026-10-03.
# Upstream serves a single linux/amd64 OCI manifest, not a manifest index.
DAKOTA_IMAGE_PIN='ghcr.io/projectbluefin/dakota@sha256:401cd9ddd142a7198585edb9629695aa6a359bc0e83862529f51940ee1242e9c'
IMAGE="${CHAIRLIFT_DAKOTA_IMAGE:-$DAKOTA_IMAGE_PIN}"
# Local experiments may use another image; CI must exercise the reviewed pin.
if [[ "${CI:-}" == "true" || "${GITHUB_ACTIONS:-}" == "true" ]]; then
    if [[ "$IMAGE" != "$DAKOTA_IMAGE_PIN" ]]; then
        echo "CHAIRLIFT_DAKOTA_IMAGE overrides are not allowed in CI; update dakota-image.sh" >&2
        return 1
    fi
fi
