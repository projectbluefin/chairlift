---
name: multi-arch-digest-pinning
description: Use when pinning or rolling a container image reference to a content digest.
version: 1.2.0
last_updated: 2026-10-04
tags:
  - supply-chain
  - containers
metadata:
  type: reference
---

# A digest pin must name the manifest index; pinning a child manifest silently narrows the image to one architecture

**When it applies:** Replacing a floating tag with an `@sha256:` reference, or
rolling an existing digest, anywhere a container image is named. ChairLift's
`internal/printerapp` deliberately runs moving `:stable` tags with Podman
auto-update (ADR-0020), so this applies there only to a site pinning a family
through `ApplyOverrides`; any pin must also verify the exact keyless workflow
identity, provenance and signed SBOM. Agent Mode delegates engine fetching to
llmman and has no ChairLift-owned container pin.

**What to do:** Resolve the *index* (manifest list) digest when the tag publishes
one, never an architecture's child entry. A bare
`curl -sI https://<registry>/v2/<repo>/manifests/latest` can negotiate a
single-architecture manifest instead — for the images this was learned on, a
`application/vnd.docker.distribution.manifest.v1+json` digest entirely
different from the index digest. Request the list media types explicitly:

```
curl -sS -D - -o /tmp/m.json \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  https://<registry>/v2/<repo>/manifests/latest
```

Confirm the response's Content-Type is an index or manifest list before taking
the `Docker-Content-Digest` header; a missing body `mediaType` is not contrary
evidence. Inspect body metadata when present. ChairLift targets both architectures:
`.github/workflows/test.yml` ships a `goarch: [amd64, arm64]` build matrix.
The two pins fail in importantly different ways on an arm64 host. A child
digest is a specific, addressable manifest, so the registry serves it happily
and the mismatch surfaces later, as an image that will not run; the reference
looks valid everywhere you inspect it. An index digest makes the registry
itself answer, and the answer names the problem: podman reports "no matching
manifest for linux/arm64". Prefer the error that arrives at pull time and
says which architecture is missing.

Inspect `.manifests[].platform.architecture` in the same response while you
have it, and record what you found. Pinning the index stays correct even for
a single-architecture image: two of the four images this was learned on
published an amd64 entry only (verified 2026-09-18), so on arm64 they
produced exactly that "no matching manifest" error — an honest report of an image upstream has not published,
rather than something ChairLift can fix by choosing a different digest.

Two follow-through rules. First, the provenance date in the comment must be
the date you actually performed the resolution — a pin's entire value is that
a later reader can re-derive it, and a fabricated date destroys that. Second,
nothing updates these digests automatically, so the roll procedure belongs
beside the pinned values; a roll procedure that omits the Accept header
reintroduces the bug at the next roll.

Do not mistake a shape test for coverage of any of this. A test asserting
only that every image contains `@sha256:` and none contains `:latest`
protects the
*shape* of the pin against a future "refresh" back to a moving tag; it cannot
see staleness and it cannot see architecture coverage. The index check
happens at roll time, by a human, or not at all.

**Learned from:** PR #127 replaced the local-AI stack's four `:latest`
references with digests — the right direction, since a floating tag is not a
pin — but every one of the four was the amd64 child entry of that image's
manifest list, and the comment carried a provenance date that was not when
the digests were taken. They were re-resolved as index digests on 2026-09-18
with the Accept header above, and later removed with the stack itself.

## Dakota E2E and release gates

`test/e2e/dakota-image.sh` owns the reviewed Dakota pin. Every Dakota
container harness sources it before invoking podman; today the tag's pin is a
single linux/amd64 manifest, not an invented multi-arch index. `--pull=missing` is
safe for that content-addressed reference: a cached image cannot silently
change when `testing` moves. `CHAIRLIFT_DAKOTA_IMAGE` is a local experiment
override only; CI (`CI=true` or `GITHUB_ACTIONS=true`) rejects a different
value. Local override results are not evidence for the committed gate.

To roll the pin:

1. Obtain an anonymous GHCR pull token from
   `https://ghcr.io/token?service=ghcr.io&scope=repository:projectbluefin/dakota:pull`.
   Request `/v2/projectbluefin/dakota/manifests/testing` with that Bearer token
   and the index/list Accept types above. Verify the response type and digest.
2. If GHCR reports that the tag is an OCI manifest rather than an index,
   repeat with `application/vnd.oci.image.manifest.v1+json` also accepted.
   Verify the response's Content-Type and the config blob's `os` and
   `architecture`. Do not select a child from an available index. On
   2026-10-03, Dakota served a single linux/amd64 OCI manifest directly;
   there was no index to pin. Its body omitted `mediaType`, so use the
   response Content-Type as well. This does not provide arm64 E2E coverage.
3. Verify SHA-256 of the raw response bytes equals `Docker-Content-Digest`,
   and fetch the manifest by that digest to confirm it remains addressable.
   Update the pin and resolution date together; record platform changes.
4. Run `make ci` and `make e2e-atspi` using the committed default (no image
   override), and exercise any other Dakota-backed E2E/screenshot targets.
   Review the resulting logs/screenshots before accepting the update.

`TestDakotaImageSelection` covers CI rejection and local overrides;
`TestDakotaHarnessesUsePinnedImage` prevents a replacement harness from
reintroducing its own floating default. These checks cannot verify image
contents or availability. Digest pinning gives reproducibility, not publisher
identity verification, and an intentional update still requires review.
