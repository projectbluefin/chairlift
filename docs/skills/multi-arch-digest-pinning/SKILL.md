---
name: multi-arch-digest-pinning
description: Use when pinning or rolling a container image reference.
---

# Container digest rollout

Pin a manifest index when the tag publishes one, not an architecture's child
manifest. Request these Accept types explicitly:

```text
application/vnd.oci.image.index.v1+json
application/vnd.docker.distribution.manifest.list.v2+json
```

Inspect the response Content-Type (the body's `mediaType` may be absent),
`Docker-Content-Digest`, and the index's platform entries. Confirm required
platforms; a digest's shape does not prove architecture coverage or availability.
For a genuinely single-platform tag, also accept its manifest type and verify
the config blob's OS/architecture. Never claim an index exists when it does not.
Verify SHA-256 of raw manifest bytes against the digest and fetch by digest to
confirm it is addressable. Record the actual resolution date and platform limits.
Pinning provides reproducibility, not publisher authentication.

## Dakota gates

`test/e2e/dakota-image.sh` owns the shared pin. All Dakota harnesses source it;
never add a private floating default. CI rejects `CHAIRLIFT_DAKOTA_IMAGE`
overrides. A local override is an experiment, not evidence for the committed pin.

1. Get an anonymous GHCR pull token from
   `https://ghcr.io/token?service=ghcr.io&scope=repository:projectbluefin/dakota:pull`.
2. Request `/v2/projectbluefin/dakota/manifests/testing` with that Bearer token
   and the index/list Accept types. If it serves a single manifest, repeat with
   the OCI manifest type accepted and inspect its config. Do not select a child
   from an available index.
3. Verify digest, addressability and platforms; update the shared pin and its
   observation comment together.
4. Run `make ci`, `make e2e-atspi`, and affected E2E/screenshot targets with the
   committed default. Inspect logs and frames before accepting the roll.

## Printer images

`internal/printerapp` resolves a family's stable tag through cosign and installs
only the digest from the verified payload. Preserve the exact issuer and
family-specific publishing identity; do not re-resolve the tag after verification.
Missing, invalid or conflicting verification payloads fail closed. Diagnostics
probe persisted `Image=`, not the moving tag. A signature is not SBOM/provenance
verification. See [ADR-0023](../../adr/0023-verify-printer-images-before-installing.md).
