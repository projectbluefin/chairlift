# Spec: Developer feeds OPML catalog

This contract governs `internal/developerfeeds/developer-feeds.opml`, the
curated developer feed catalog ChairLift compiles into the binary. A confirmed
live Developer Mode enable may stage it for manual import when
`features_page.dx_group.stage_feeds` is opted in; that setting and the separate
`install_pulp` setting both default to false. The validator and catalog curator
consume this format contract; staging never imports subscriptions into Pulp.

## Interface

An OPML 2.0 document. Structure, with the attribute constraints the validator
enforces:

| Element | Attribute | Required | Constraints |
| --- | --- | --- | --- |
| `opml` | `version` | yes | exactly `2.0` |
| `head` | `title` | yes | non-empty; labels the imported list in a reader |
| `body > outline` | display label (`title`, else `text`) | required categories | all three required category labels must be present; additional group names are not validated |
| `outline` (group) | `outline` children | yes | at least one; a group MUST NOT carry `xmlUrl` |
| `outline` (feed) | `title` | yes | non-empty; used as the display label; `text` is retained by curation convention |
| `outline` (feed) | `type` | yes | exactly `rss` |
| `outline` (feed) | `category` | yes | one of `changelog`, `blog`, `newsletter`, `podcast` |
| `outline` (feed) | `xmlUrl` | yes | absolute `https` URL; no credentials, fragment, or tracking parameter |
| `outline` (feed) | `htmlUrl` | no | same clean absolute `https` constraints as `xmlUrl` when present |
| `outline` (feed) | `outline` children | no | a feed MUST NOT contain child outlines |

Shape excerpt (the other two required groups must also contain feeds):

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

The three required top-level labels are `Changelogs`, `Blogs & Newsletters`,
and `Podcasts`. Curation groups podcasts one level further by show family and
keeps their `podcast` category tag. The validator walks nested groups recursively;
it does not enforce a maximum depth or category-to-group mapping.

## Rules

1. The document MUST be well-formed XML with balanced tags and a single
   `<opml>` root element. A following root or non-whitespace text is invalid;
   trailing whitespace and comments are permitted. `Parse` also ignores trailing
   processing-instruction/directive tokens rather than enforcing a token allowlist.
2. Every feed `xmlUrl` MUST be unique across the whole document.
3. Sibling outlines below a top-level group MUST NOT share a display label
   (`title` when non-empty, otherwise `text`). Required top-level labels are
   checked for presence, not uniqueness.
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
8. Catalog parsing and validation MUST be offline: no validation rule resolves
   a hostname, opens a socket, or runs a command. `TestPackageStaysOffline`
   rejects direct imports of the listed network/command/GTK packages in
   production files. It is not a transitive dependency audit: Pulp provisioning
   separately delegates user-scope execution to `internal/flatpak`.
9. The number of feeds per category is pinned in
   `internal/developerfeeds/developerfeeds_test.go`. Adding or removing a feed
   MUST update that count in the same change, which is what makes a catalog
   edit a visible, reviewable event.

Rule 9 makes a count-changing catalog edit visible to review; it does not prove
publisher liveness or community vetting, and a same-count replacement needs the
same review. The epic that requested this catalog
([projectbluefin/chairlift#235](https://github.com/projectbluefin/chairlift/issues/235))
requires community vetting; preserve that human curation step separately from
the offline gate.

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

Historical evidence from 2026-09-22: all 32 feed URLs returned HTTP 200 with an
XML content type, and all 26 site URLs (`htmlUrl`) returned HTTP 200. This is not
current liveness evidence; repeat verification when curating a change. Command:

```sh
curl -sS -L -o /dev/null --max-time 20 \
  -w '%{http_code}|%{content_type}|%{url_effective}' "$url"
```

### Known exclusions

Historical curation rationale from 2026-09-22 for entries absent from the catalog:

- **Last Week in Cloud Native** (`lwcn.dev`) — the domain served a parking page
  and the only known URL carried a `utm_source` parameter. Recheck for a
  canonical publisher feed before adding it.
- **Site links for six shows** — `OpenObservability Talks`, `Geeking Out`,
  `Agentic DevOps`, `PurePerformance`, `The Kubelist Podcast`, and `Argo
  Unpacked` had no verified publisher page (`openobservability.fm` and
  `pureperformance.show` did not resolve; the Heavybit Kubelist landing page
  returned 404). `htmlUrl` is optional; their feed URLs passed that dated
  verification. Recheck a publisher page before adding its site link.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| `~/.local/share/chairlift/developer-feeds.opml` | Embedded asset staged only after a confirmed live enable with `stage_feeds` opted in; imported by the user, never by ChairLift |

## References

- Rationale: [ADR-0007](../adr/0007-pure-leaf-packages-route-around-untestable-gtk.md)
  (the validator is a puregotk-free leaf package precisely so it can be tested
  headlessly), and epic
  [#235](https://github.com/projectbluefin/chairlift/issues/235), which decides
  the catalog's contents and its vetting requirement
- Context: [design/overview.md](../design/overview.md)
- Source: [`developerfeeds.go`](../../internal/developerfeeds/developerfeeds.go)
  (offline parser/validator), [`pulp.go`](../../internal/developerfeeds/pulp.go)
  (user-scope provisioning/staging), and
  [`features_page.go`](../../internal/views/features_page.go)
  (`startDeveloperFeedSetup` opt-in admission and worker).
