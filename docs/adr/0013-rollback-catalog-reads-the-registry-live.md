# 0013 — The dated-build catalog reads the registry live

- **Status:** Accepted
- **Date:** 2026-09-22

## Context

Issue #137 asks for a rollback surface that lets a user choose *which* build to
return to, and says it "should always show the state of the registry" and
"should cover pinning too". ChairLift today can return to the deployment the
system is already keeping — `bootc rollback` through
`chairlift-ublue-helper`, and `internal/sysupdate.RollbackVersion`'s
older-only candidate on native A/B hosts — but neither offers a choice, because
neither knows what else exists.

What else exists is published only in the registry. `internal/imageinfo`'s
tables (ADR-0011) are the authority on rebase targets, and they cannot answer
this: they are hand-verified, keyed on registry path and stream, and deliberately
small, because a wrong entry there is a failed `bootc switch` on a user's
machine. The set a rollback calendar needs is a different kind of set — the
complete dated-tag history of one repository.

Its size is the whole problem. Read from `ghcr.io/ublue-os/bluefin` on
2026-09-22: 1907 tags, 1325 of them containing an eight-digit run, and 13
distinct dated spellings of the single day 2026-06-23 alone (`44.20260623`,
`stable-20260623`, `stable-44.20260623`, `stable-daily-20260623`,
`stable-daily-44.20260623`, `gts-20260623`, `gts-44.20260623`, and their
hyphen-separated twins). A repository that publishes most days cannot be
tracked in a table verified by hand.

The parity plan recorded the open question and deliberately deferred it: "It
needs a live `/v2/<repo>/tags/list` call to populate the dated tags, which puts
a network round-trip behind a GUI control and cannot be covered by the gated
tests without a fixture server. Worth doing, but as its own phase with a
recorded decision about where the network call lives."

## Decision

**The dated-build catalog is read from the registry at runtime.** It is not a
table in Go, and it is not a generated asset committed to the repository.

**The network call lives in a new pure-Go leaf package,
`internal/registrytags`.** Every request goes through the package's
`Client.HTTP` transport, the same seam `internal/sbom` uses, so gated tests
drive a loopback registry and no gate in `make ci` reaches the network — the
existing rule for the changelog fetch, applied here.

**The package is read-only and owns no privileged surface.** It returns a
catalog for display and comparison. Nothing it returns is handed to
`pkexec`, and it takes no argument that reaches one.

**The day comes from the tag; the digest and the build's precise time come
from one manifest read, on demand.** `Client.Tags` lists; `ParseBuild`
classifies; `Client.Tag` resolves. A calendar of ninety days costs the same
paginated requests as a calendar of nine, because the date is in the tag
name.

**Reads are cached in process, bounded and time-limited, and a failure is
never cached.** `Catalog` holds a tag listing for a short TTL and resolves a
chosen tag once. An error is returned to the caller to render rather than
being served as a stale catalog, because a catalog that is not the registry's
is the thing this decision exists to avoid.

**Pinning is not in this package, and is not unblocked by it.** The ublue
helper takes no image reference (ADR-0001), so a pin has to be a new
privileged operation whose target the helper derives itself from a validated
grammar, exactly as `channel-switch` derives its own. That is a separate
decision with its own blast radius.

## Consequences

A rollback calendar shows the registry's current state by construction rather
than by refresh discipline. The failure mode a baked table would have — a
feature that quietly works from week-old data — does not exist here.

The surface therefore requires the network, and must render its own
unavailable and error states. There is no offline catalog, deliberately: an
offline catalog would be the stale artifact this decision rejects, and a
user choosing a rollback target on a machine with no network is better served
by being told so than by being shown a list that may have moved.

The catalog is a second, independent source of truth about image references.
It does not re-verify `internal/imageinfo`'s tables and must not be used to
resolve a `bootc switch` target; the two answer different questions and only
one of them is allowed to reach the privileged path.

One build is reachable through many tags, so callers that want one row per
build must group — by digest, or by day. This package returns every alias
rather than collapsing them, because choosing which spelling to show depends
on the stream the machine is booted into, which is `internal/imageinfo`'s
question.

## Alternatives considered

- **Bake the catalog into the repository from a scheduled workflow.** The
  issue names a GitHub Action doing a `skopeo inspect`; running it on a
  schedule and committing the result would remove the runtime network call
  entirely. Rejected: it is stale by construction, which contradicts the
  issue's own "should always show the state of the registry", and it commits
  a generated artifact, which this repository does not carry.
- **Extend `internal/imageinfo`'s tables with dated tags.** Rejected: those
  tables exist so that a `bootc switch` target is never guessed, and each
  entry is verified by hand and recorded by date. A set that changes daily
  cannot be maintained that way, and an entry that goes stale there is a
  privileged action aimed at a reference that no longer exists.
- **Put the listing in `internal/sbom`.** Rejected: `sbom` owns the
  changelog's referrer discovery, a different question with a different
  fallback path. The seam is what is shared, not the package; coupling two
  independently-reasoned fetches would make either one harder to change.
- **Resolve `org.opencontainers.image.created` for every dated tag** so the
  calendar shows build times rather than days. Rejected: one request per tag,
  over 1300 on the verified listing, to render a calendar whose day is
  already in the tag name.
- **Read the tag list through a `gh` CLI or an external tool.** Rejected: it
  adds a runtime dependency to a GUI that currently needs none for this, and
  puts the parse in a subprocess where it cannot be table-tested.

## References

- Shapes: [design/package-managers.md](../design/package-managers.md),
  [plans/2026-08-17-bluefin-suite-parity-plan.md](../plans/2026-08-17-bluefin-suite-parity-plan.md)
- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md) (the
  privilege boundary a pin target would have to cross),
  [ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md) (the
  leaf-package layout), [ADR-0011](0011-chairlift-owns-bluefin-family-rebasing.md)
  (the tables that stay the authority on rebase targets)
