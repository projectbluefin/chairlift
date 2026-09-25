# 0017 — A pin switches to a dated build named by a validated day word

- **Status:** Accepted
- **Date:** 2026-09-25

## Context

Issue #137 asks for a rollback surface that lets a user choose *which* build to
return to, and says it "should cover pinning too". ADR-0013 landed the read
half: `internal/registrytags` lists the registry's dated builds at runtime, and
Recovery's **Published versions** row (`internal/views/versions.go`) displays
them, read-only. It also recorded what is deliberately still missing —
"pinning is not in this package, and is not unblocked by it" — because
`chairlift-ublue-helper` accepts no image reference (ADR-0001). A pin must
therefore be a new privileged operation whose target the helper derives itself
from a validated grammar, the way `channel-switch` already derives its own.

Five facts constrain what that grammar can be. All were observed on
2026-09-25, by reading each repository's complete tag list from GHCR and by
manifest request.

**The helper's argv is the only value an authenticated caller controls.** An
authenticated caller is not a trusted caller: PolicyKit validates nothing past
`exec.argv1` (ADR-0001). Any word the helper accepts has to be validated against
a closed grammar, and the concrete reference must be derived on the privileged
side, from state the caller cannot write.

**A dated build's tag spelling is a property of the image and the stream, and
no single separator is universal.** Counts below are tags of the form
`<stream>.<YYYYMMDD>` and `<stream>-<YYYYMMDD>` in each repository's complete
tag list:

| repository | stream | `.` form | `-` form | newest dated tag |
| --- | --- | --- | --- | --- |
| `ghcr.io/ublue-os/bluefin` | `latest` | 0 | 19 | 20260908 |
| | `stable` | 0 | 15 | 20260922 |
| | `stable-daily` | 0 | 29 | 20260922 |
| | `gts` | 0 | 15 | 20260922 |
| | `lts` | 4 | 4 | 20260714 |
| | `lts-hwe` | 4 | 4 | 20260714 |
| | `lts-testing` | 27 | 27 | 20260720 |
| | `lts-hwe-testing` | 26 | 26 | 20260720 |
| `ghcr.io/projectbluefin/bluefin-lts` | `testing` | 51 | 22 | 20260701 |
| | `lts` | 1 | 1 | 20260606 |
| | `stable` | 0 | 0 | — |
| `ghcr.io/projectbluefin/dakota` | `latest` | 28 | 0 | 20260212 |
| | `stable`, `testing` | 0 | 0 | — |
| `ghcr.io/projectbluefin/dakota-gaming` | `stable`, `testing` | 0 | 0 | — |

Two readings of that table are load-bearing. The separator is not a per-image
constant: `ghcr.io/projectbluefin/dakota` publishes `latest.20260212` and no
hyphenated form at all, while `ghcr.io/ublue-os/bluefin` publishes
`stable-20260922` and no dotted form. And for every stream that publishes both
spellings on a given image, the two cover the same days — verified by diffing
the two day sets for `lts-testing`, `lts`, `lts-hwe`, and `lts-hwe-testing`,
which are equal, and confirmed by manifest request for
`ghcr.io/projectbluefin/bluefin-lts` (`testing-20260606` and
`testing.20260606` both resolve to `sha256:438bf18d…`; `testing-20260701` and
`testing.20260701` both to `sha256:8d5daa18…`).

**The dated surface is sparse, and stale on most images.** `stable`,
`stable-daily`, and `gts` on `ghcr.io/ublue-os/bluefin` moved on 2026-09-22, the
newest date the repository published anything under — three days before this
survey. Its LTS streams stopped at 2026-07-20, `bluefin-lts:testing` at
2026-07-01, and `dakota:latest` at 2026-02-12. `dakota-gaming` publishes no
dated tag at all. A day a user picks therefore frequently has no build.

**A day with no build resolves to 404.** Verified against
`ghcr.io/ublue-os/bluefin`: `stable-20260925`, `stable-20260923`, and
`stable-20260910` all answer 404 with a `MANIFEST_UNKNOWN` body, while
`stable-20260922` answers 200. The registry is the authority on which days
exist, and it answers that question per reference.

**Where several spellings exist for one day they are one build.** Verified:
`stable-20260922`, `stable-daily-20260922`, `gts-20260922`, and `44.20260922`
all resolve to `sha256:76aa5d6f…`, whose
`org.opencontainers.image.created` is `2026-09-22T01:37:01Z` — agreeing with the
day the tag names, as ADR-0013 found for every build it sampled.

**A pinned host cannot switch back today.** Reading `internal/imageinfo`: a
dated tag such as `stable-20260922` appears in neither `stableTags` nor
`testingTags` of its image's entry, so `Info.Channel()` returns
`ChannelUnknown`; and `TargetTag`'s two maps are keyed on *stream* tags, so
`TargetTag(ref, "stable-20260922", ChannelStable)` finds no entry and returns
ok=false. `SwitchTarget` therefore fails, and `channel-switch` refuses on
exactly the host that needs it most. This is not a bug in that path — it is
that "the channel to move to" and "the stream this pinned build came from" are
different questions, and only the second is answerable from a dated tag.

## Decision

**A pin is a `bootc switch` to a dated build of the stream the machine is
already running, and the only value that crosses the pkexec boundary is the
day.**

**The argv is `pin <YYYYMMDD> [--dry-run]`.** The day word is the sole
argument, and it is validated as a closed grammar rather than sanitized: exactly
eight bytes, every one an ASCII digit, parsed with `time.Parse("20060102", …)`.
That parse is strict — it rejects `20260230` (`day out of range`), `20261301`
(`month out of range`), a seven- or nine-digit run, and any trailing byte — so
the accepted set is 8-digit strings that name a real calendar day. A day later
than today in UTC is rejected too: a build cannot exist for a day that has not
happened. Everything else — a tag, a reference, a digest, a stream word, a
second positional argument, a misplaced flag — is rejected by
`ubluehelper.ParseInvocation`, as it is for every other subcommand.

**The helper derives the target; no string the registry supplies becomes its
text.** The stream is recovered from the *booted* tag, so a pinned host can
re-pin and can return: `registrytags.ParseBuild` reads `stable-20260922` as
stream `stable`, and a tag that is not a dated build is its own stream
(`lts-testing`). That stream must then appear in the running image's entry in
`internal/imageinfo`'s channel table, among `stableTags` or `testingTags`,
which is the gate ADR-0011 established for every switch target. The reference
is built from the descriptor's own registry path (`Info.CleanRef()`) plus one of
a **closed, ordered pair of candidate spellings**: `<stream>-<YYYYMMDD>` first,
then `<stream>.<YYYYMMDD>`. Hyphen is first because it is the only form
`ghcr.io/ublue-os/bluefin` publishes for `latest`, `stable`, `stable-daily`,
and `gts`, and where both forms exist it covers the same days — except on
`ghcr.io/projectbluefin/bluefin-lts`'s `testing`, where the 22 hyphenated days
are a subset of the 51 dotted ones, so the order still selects a valid build
for those days and falls through to the dotted form for the rest. The dotted
candidate is not redundancy — it is the only form
`ghcr.io/projectbluefin/dakota` publishes for `latest`. A constructed candidate
proves its own provenance: its text carries the stream and the day, so a
candidate that resolves *is* a build of the requested stream on the requested
day, and no lookup or heuristic decides the answer.

**The target is verified to resolve before anything is staged, and every
failure is a refusal.** The helper resolves the candidates in order and
switches to the first that answers; if neither answers, it exits non-zero
having staged nothing. A registry error or a timeout is a refusal, never a
fallthrough to switching unverified — which is the whole point of the check,
because these tables are hand-verified and go stale (ADR-0011 names that
maintenance obligation itself) and a tag can be pruned between the GUI's read
and the switch. The check consults only the single-manifest resolution
(`registrytags.Client.Tag`), never a tag listing: the privileged path must not
be able to become a catalog crawl. Under `--dry-run` the helper derives and
prints the target and stops without contacting the registry, because a dry run
must not depend on the network and `test/e2e/helper_commands_test.go` runs
every command with `--dry-run` on a host whose gated tests make no outbound
request. The asymmetry that buys is recorded below.

**Returning to the stream is a second command, `unpin`, and it takes no
argument at all.** A pin's own tag names the stream it belongs to, so the
return trip is fully derivable: `unpin` requires the booted tag to be a dated
build, recovers its stream, requires that stream to be a table tag of the
running image — every stream observed above is itself published as a tag, and
ADR-0011's table already records those tags — verifies it resolves, and
switches to `<cleanRef>:<stream>`. Reusing `channel-switch` was rejected: it
accepts only the two *channel* words `stable` and `testing`, while the streams
that actually carry dated builds are `lts`, `lts-hwe`, `lts-testing`,
`lts-hwe-testing`, `gts`, `stable-daily`, and `latest`; and it refuses outright
on a pinned host, as the Context above records. `unpin` is the `factory-reset`
shape — the target is spelled on the privileged side and the GUI sends only the
command word.

**Two PolicyKit actions, at `channel-switch`'s authentication class.**
`io.projectbluefin.chairlift.ublue.pin` (`exec.argv1` `pin`) and
`io.projectbluefin.chairlift.ublue.unpin` (`exec.argv1` `unpin`), both
annotated with the fixed `/usr/bin/chairlift-ublue-helper`, both defaulting to
`auth_admin` / `auth_admin` / `auth_admin_keep`. A pin is the same operation
family as `channel-switch` — a staged `bootc switch` transaction — and the same
reversibility, so it takes the same class rather than a novel one. It also
carries the same `--enforce-container-sigpolicy` as every other switch ChairLift
stages: a pin to an older build is precisely the case where dropping signature
enforcement would be tempting, and it does not.

**Nothing about the GUI-side selection crosses the boundary.** The Recovery
list already displays the registry's own spelling of each day
(`pageview.PublishedVersions` renders "Published as stable-20260922"), and the
Pin action sends only the day parsed out of the row it was pressed on. The
displayed tag and the helper's derived tag agree because the candidate order
above was chosen to cover every stream the table names; the derivation itself
lives in the gated `internal/ubluehelper` package behind a resolver seam, so
`cmd/chairlift-ublue-helper` supplies the registry lookup and decides nothing.
`Invocation.UsesChannelTable()` must gain both commands, because both resolve
their stream through the channel table and a broken `channels.yml` override has
to stop them rather than let the helper resolve a different target.

## Consequences

A pin cannot name an image, a tag, a digest, or a stream. The only thing a
caller chooses is a day, and the day can only reach a reference the helper
built from root-owned state — the descriptor, `/run/ublue-os/booted-image`, and
`imageinfo`'s verified table — after the registry confirmed that exact
reference exists. ADR-0001's boundary is therefore unchanged in kind, and
ADR-0013's rule is unchanged in substance and now stated precisely: what must
never happen is a registry-supplied string reaching the switch argv, and the
registry is consulted here only to answer yes or no about a reference the
privileged side already computed. `registrytags`' listing surface stays out of
the privileged path entirely.

The privileged helper acquires an outbound HTTPS read where it previously had
none. The request is bounded: the host and repository path come from the
descriptor, the tag from the closed grammar, and only `Client.Tag` is called,
so the URL is a function of root-owned state plus eight digits. A verification
timeout is a refusal. This is a real addition to what a root binary does, and it
is recorded here rather than left implicit.

Pin is usable only where an image publishes a current dated history, and the
survey above says that is essentially `ghcr.io/ublue-os/bluefin` alone. On
`bluefin-lts`, `dakota`, and `dakota-gaming` the dated surface is stale or
absent, so the existence check refuses and the feature is inert — correctly, and
visibly, rather than as a failed pull. Whoever wants pin there has to fix the
publishing side, not this grammar.

A host booted on a spelling outside the table is refused. `44.20260922` is a
real, published dated tag of `ghcr.io/ublue-os/bluefin`, but `44` is not one of
that image's stream tags, so a host pinned to it can neither re-pin nor unpin.
That host is already refused by `channel-switch` for the same reason (its tag
classifies as `ChannelUnknown`), so this decision does not narrow anything that
worked; it is named here because it is the shape of gap a future reader will
find.

A dry run can print a target that a live run would refuse, because it derives
without verifying. The alternative — verifying under `--dry-run` — would make
the E2E accepted-command gate depend on the network, which this repository
forbids; the printed line is a preview of the argv, not a promise that the
switch will succeed.

Two obligations land with the implementation. No argument the GUI passes may
carry a `/`, and `internal/ublue`'s existing assertion of that must be extended
to the pin day word and to `unpin`'s empty argument list. And the candidate
order must be pinned by an offline table test built from the observations
above, so that the tag the helper derives for a given image, stream, and day is
asserted to be the tag the catalog displays.

## Alternatives considered

- **Accept the tag, or the reference, and validate it in the helper.** Rejected:
  a validator that accepts `ghcr.io/ublue-os/bluefin:stable-20260922` is one
  edit away from accepting any reference, and PolicyKit cannot help — it does
  not inspect argv past `argv1` (ADR-0001). The boundary is that the caller
  names a day, not an image.
- **Carry the stream word as a second argument.** Rejected: it would let a
  caller pin to another stream's dated build, which is a different image than
  the one they are running, so it is an image selection by another name. The
  booted tag already answers which stream — including on a host that is itself
  pinned.
- **Record a dated-tag spelling per image (or per image and stream) in
  `imageinfo`'s table.** Rejected: the table above shows the separator is not
  a per-image constant (`dakota` is dotted, `bluefin` is hyphenated), so this
  becomes a per-stream table that must be re-verified by hand as published
  streams come and go. Trying the two spellings and requiring one to resolve
  reaches the same answer from evidence the registry supplies per request, and
  fails closed when neither does.
- **Verify existence in the GUI only, and let `bootc switch` fail on a dead
  tag.** Rejected: a tag can be pruned between the list read and the switch,
  and the newest dated build lags the current day by days on every image
  surveyed — so this is the common case, not the rare one. A refusal that names
  the reason is worth one request.
- **Require the resolved tag's `org.opencontainers.image.created` day to equal
  the requested day.** Rejected: the constructed candidate's own text already
  carries the stream and the day, so the annotation adds no authority the
  grammar lacks, and it would refuse a correctly published build whose
  annotation falls on the previous UTC day — a boundary ADR-0013's sample is
  too small to rule out.
- **Make `pin` stricter than `channel-switch`, at `auth_admin` with no
  `auth_admin_keep` on the active session.** Rejected: a pin neither escalates
  privilege nor is irreversible, and it is the same staged-switch operation
  `channel-switch`, `driver-switch`, and `factory-reset` already perform at the
  shared class; a lone exception would read as a policy statement this ADR is
  not making. Revisiting it is one attribute in the policy file plus the
  expectation in `internal/installcheck`.
- **A separate `unpin` PolicyKit action is redundant — reuse
  `channel-switch`.** Rejected on the evidence in Context: on a pinned host
  `channel-switch` refuses today, and its accepted words cannot express `lts`,
  `gts`, or `stable-daily`. Returning to the stream is its own operation with
  its own target derivation.

## References

- Shapes: [design/package-managers.md](../design/package-managers.md) (the
  registry tag catalog and the view-layer leaf packages),
  [design/destination-matrix.md](../design/destination-matrix.md) (Recovery's
  group and action ownership)
- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md) (the
  boundary this target crosses), [ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md)
  (the leaf-package layout the derivation follows),
  [ADR-0011](0011-chairlift-owns-bluefin-family-rebasing.md) (the tables that
  stay the authority on switch targets),
  [ADR-0013](0013-rollback-catalog-reads-the-registry-live.md) (the read-only
  catalog, and the pin decision it deferred to this one)
- Enforced by: `internal/ubluehelper`'s per-command parse tests,
  `internal/ublue`'s no-argument-carries-a-slash assertion,
  `internal/installcheck/polkit_test.go`
  (`TestPolkitPoliciesMatchPrivilegedHelpers`,
  `TestEveryUblueCommandHasOnePolicyAction`), and
  `test/e2e/helper_commands_test.go`
  (`TestEveryHelperCommandHasAnAcceptedCase`)
- Implements: projectbluefin/chairlift#359 (the helper subcommands) and #360
  (the Recovery page's pin action, return to stream, and user documentation)
