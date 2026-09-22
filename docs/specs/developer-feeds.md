# Spec: Developer feeds OPML catalog

This contract governs `internal/developerfeeds/developer-feeds.opml`, the
curated developer feed catalog ChairLift compiles into the binary and stages
for the Pulp feed reader when Developer Mode is enabled. It is consumed by the
catalog's validator (`internal/developerfeeds/developerfeeds.go`), by the
Developer Mode staging path, and by whoever curates the list.

## Interface

An OPML 2.0 document. Structure, with the attribute constraints the validator
enforces:

| Element | Attribute | Required | Constraints |
| --- | --- | --- | --- |
| `opml` | `version` | yes | exactly `2.0` |
| `head` | `title` | yes | non-empty; labels the imported list in a reader |
| `body > outline` | `text` | yes | one of the three required category groups |
| `outline` (group) | `outline` children | yes | at least one; a group MUST NOT carry `xmlUrl` |
| `outline` (feed) | `text`, `title` | yes | non-empty; `title` is what a reader displays |
| `outline` (feed) | `type` | yes | exactly `rss` |
| `outline` (feed) | `category` | yes | one of `changelog`, `blog`, `newsletter`, `podcast` |
| `outline` (feed) | `xmlUrl` | yes | absolute `https` URL; no credentials, fragment, or tracking parameter |
| `outline` (feed) | `htmlUrl` | no | absolute `https` URL when present |
| `outline` (feed) | `outline` children | no | a feed MUST NOT contain child outlines |

Minimal valid shape:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Control Center developer feeds</title></head>
  <body>
    <outline text="Changelogs">
      <outline text="Bluefin OS Releases" title="Bluefin OS Releases" type="rss"
               category="changelog"
               xmlUrl="https://github.com/projectbluefin/bluefin/releases.atom"
               htmlUrl="https://github.com/projectbluefin/bluefin/releases"/>
    </outline>
    <!-- Blogs & Newsletters and Podcasts follow the same shape -->
  </body>
</opml>
```

The three required top-level groups are `Changelogs`, `Blogs & Newsletters`,
and `Podcasts`. `Podcasts` MAY nest one further level by show family; a nested
group carries no feed URL and its feeds keep the `podcast` category tag.

## Rules

1. The document MUST be well-formed XML with balanced tags and a single
   `<opml>` root element. Content after the closing tag is invalid.
2. Every feed `xmlUrl` MUST be unique across the whole document.
3. Two outlines in the same group MUST NOT share a title.
4. Every `xmlUrl` and `htmlUrl` MUST be an absolute `https` URL, and MUST NOT
   carry userinfo, a fragment, or a campaign/tracking query parameter (`utm_*`,
   `fbclid`, `gclid`, `igshid`, `mc_cid`, `mc_eid`, `ref_src`). A feed's URL is
   the endpoint the publisher maintains, not the link a newsletter happened to
   carry.
5. Every outline MUST be either a group (children, no `xmlUrl`) or a feed
   (`xmlUrl`, no children). An outline that is neither is a defect.
6. The document MUST declare OPML `2.0` and carry a non-empty `head` title.
7. Each of the three required groups MUST exist and MUST contain at least one
   feed.
8. Validation MUST be offline: no rule in this spec resolves a hostname, opens
   a socket, or runs a command. `TestPackageStaysOffline` holds the package to
   that by rejecting imports of an HTTP client, a dialer, or a command runner.
9. The number of feeds per category is pinned in
   `internal/developerfeeds/developerfeeds_test.go`. Adding or removing a feed
   MUST update that count in the same change, which is what makes a catalog
   edit a visible, reviewable event.

Rule 9 is deliberate friction. A feed list that can grow without review is how
an unvetted or dead subscription reaches users; the epic that asked for this
catalog ([projectbluefin/chairlift#235](https://github.com/projectbluefin/chairlift/issues/235))
requires community vetting, and the test is where that requirement is enforced
rather than merely stated.

## Curation

The list is curated, not generated. A change MUST name its source — the issue,
the review thread, or the community member who proposed the feed — in the pull
request that makes it. Issue
[#236](https://github.com/projectbluefin/chairlift/issues/236) requests the
initial review from `@mrbobbytables` and the surrounding community.

## Manual re-verification

Offline validation cannot tell whether a feed is still alive, so liveness is
checked manually. Run this before proposing a catalog change, and on whatever
cadence the project agrees for the standing catalog — at minimum when a user
reports a dead feed. It needs network access and is never part of `make ci`.

From the repository root:

```sh
grep -o 'xmlUrl="[^"]*"' internal/developerfeeds/developer-feeds.opml | cut -d'"' -f2 |
while read -r url; do
  printf '%s %s\n' "$(curl -sS -L -o /dev/null --max-time 20 \
    -w '%{http_code}|%{content_type}' "$url")" "$url"
done
```

Every line MUST begin `200|` and the content type MUST be an XML type
(`application/rss+xml`, `application/atom+xml`, `application/xml`, `text/xml`).
`000|` means the host did not resolve or the connection failed; `404` or `410`
means the publisher moved or retired the feed; `200` with `text/html` means the
URL now serves a page rather than a feed. Repeat the same loop over `htmlUrl`
values when site links change.

Reachability is not activity. For each feed, confirm the most recent
`<pubDate>`, `<updated>`, or `<published>` value is recent enough for that
publisher's schedule — a `200` on a feed whose newest item is three years old
is a dead subscription:

```sh
curl -sS -L --max-time 20 "$url" | grep -o -m1 -E '<(pubDate|updated|published)>[^<]*'
```

When a check fails:

1. Look for the publisher's replacement feed before removing anything; most
   moves leave a redirect or an announcement post.
2. Update `developer-feeds.opml` with the canonical endpoint, and the pinned
   counts in `developerfeeds_test.go` if the number of feeds changed.
3. Run `make ci` and record the re-verification date and result in the pull
   request. Do not add a network test for this: the catalog's CI path is
   offline by design (rule 8).

### Last verification

2026-09-22: all 32 feed URLs returned HTTP 200 with an XML content type, and
all 26 site URLs (`htmlUrl`) returned HTTP 200, checked with:

```sh
curl -sS -L -o /dev/null --max-time 20 \
  -w '%{http_code}|%{content_type}|%{url_effective}' "$url"
```

### Known exclusions

Proposed entries deliberately absent from the catalog:

- **Last Week in Cloud Native** (`lwcn.dev`) — the domain serves a parking page
  pending validation and the only known URL carried a `utm_source` parameter.
  Excluded until the publisher restores a canonical feed.
- **Site links for six shows** — `OpenObservability Talks`, `Geeking Out`,
  `Agentic DevOps`, `PurePerformance`, `The Kubelist Podcast`, and `Argo
  Unpacked` have no verified publisher page (`openobservability.fm` and
  `pureperformance.show` do not resolve; the Heavybit Kubelist landing page
  returned 404). `htmlUrl` is optional, and their feed URLs are live and
  verified. Add the site link when a publisher page resolves.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| `~/.local/share/chairlift/developer-feeds.opml` | The embedded asset, staged for user import by the Developer Mode flow (epic [#235](https://github.com/projectbluefin/chairlift/issues/235)) |

## References

- Rationale: [ADR-0007](../adr/0007-pure-leaf-packages-route-around-untestable-gtk.md)
  (the validator is a puregotk-free leaf package precisely so it can be tested
  headlessly), and epic
  [#235](https://github.com/projectbluefin/chairlift/issues/235), which decides
  the catalog's contents and its vetting requirement
- Context: [design/overview.md](../design/overview.md)
