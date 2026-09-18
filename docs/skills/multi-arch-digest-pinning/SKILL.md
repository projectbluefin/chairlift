---
name: multi-arch-digest-pinning
description: Use when pinning or rolling a container image reference to a content digest.
version: 1.0.0
last_updated: 2026-09-18
tags:
  - supply-chain
  - containers
metadata:
  type: reference
---

# A digest pin must name the manifest index; pinning a child manifest silently narrows the image to one architecture

**When it applies:** Replacing a floating tag with an `@sha256:` reference, or
rolling an existing digest, anywhere a container image is named — today that
is `stacks` in `internal/aistack/aistack.go`, whose four
`quay.io/ramalama/*` entries the local AI stack writes into the user's
quadlet.

**What to do:** Resolve the *index* (manifest list) digest, never an
architecture's child entry. A registry answers a bare
`curl -sI https://quay.io/v2/ramalama/cuda/manifests/latest` with a
single-architecture digest — for that image today, a
`application/vnd.docker.distribution.manifest.v1+json` digest entirely
different from the index digest — so the obvious one-liner produces exactly
the wrong value. Request the list media types explicitly instead:

```
curl -sS -D - -o /tmp/m.json \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  https://quay.io/v2/ramalama/<name>/manifests/latest
```

Confirm the response body's `mediaType` really is an index or manifest list
before taking the `Docker-Content-Digest` header; only then paste it. That
ChairLift targets more than one architecture is not incidental —
`.github/workflows/test.yml` ships a `goarch: [amd64, arm64]` build matrix —
and the two pins fail in importantly different ways on an arm64 host. A child
digest is a specific, addressable manifest, so the registry serves it happily
and the mismatch surfaces later, as an image that will not run; the reference
looks valid everywhere you inspect it. An index digest makes the registry
itself answer, and the answer names the problem: podman reports "no matching
manifest for linux/arm64". Prefer the error that arrives at pull time and
says which architecture is missing.

Inspect `.manifests[].platform.architecture` in the same response while you
have it, and record what you found. Pinning the index stays correct even for
a single-architecture image: `quay.io/ramalama/rocm` and
`quay.io/ramalama/intel-gpu` publish an amd64 entry only (verified
2026-09-18), so on arm64 those two produce exactly that "no matching
manifest" error — an honest report of an image upstream has not published,
rather than something ChairLift can fix by choosing a different digest.

Two follow-through rules. First, the provenance date in the comment must be
the date you actually performed the resolution — a pin's entire value is that
a later reader can re-derive it, and a fabricated date destroys that. Second,
nothing updates these digests automatically, so the roll procedure belongs
beside the pinned values, as it does in the comment above `stacks`; a roll
procedure that omits the Accept header reintroduces the bug at the next roll.

Do not mistake the invariant test for coverage of any of this.
`TestEveryStackIsPinnedByAnImmutableDigest` asserts only that every entry's
image contains `@sha256:` and none contains `:latest`. That protects the
*shape* of the pin against a future "refresh" back to a moving tag; it cannot
see staleness and it cannot see architecture coverage. The index check
happens at roll time, by a human, or not at all.

**Learned from:** PR #127 replaced the four `quay.io/ramalama/*:latest`
references with digests — the right direction, since a floating tag is not a
pin — but every one of the four was the amd64 child entry of that image's
manifest list, and the comment carried a provenance date that was not when
the digests were taken. The pins on `main` are now the index digests, each
re-resolved from quay.io on 2026-09-18 with the Accept header above.
