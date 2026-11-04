# Developer feed catalog curation

The embedded [OPML catalog](../../internal/developerfeeds/developer-feeds.opml)
is curated, not generated. [`developerfeeds.go`](../../internal/developerfeeds/developerfeeds.go)
owns parsing and offline validation; use it for the exact accepted format rather
than maintaining a parallel schema here.

## Staging and import

Developer Mode's `stage_feeds` opt-in stages
`~/.local/share/chairlift/developer-feeds.opml` after a confirmed live promotion.
It does not import subscriptions. The separate `install_pulp` opt-in provisions
the reader; neither option is required for developer access. Both default off.
[`devsetup`](../../internal/devsetup/setup.go) selects follow-on work, and
[`pulp.go`](../../internal/developerfeeds/pulp.go) owns staging and provisioning.
Dry-run writes nothing. Disabling Developer Mode removes neither subscriptions
nor the staged catalog. Resolve the file path through the Developer Mode
disclosure and import it manually in the reader.

## Curation

Name the proposing issue, review thread or community source in the pull request.
Community review is separate from syntactic validation: passing the offline
gate proves neither publisher liveness nor endorsement.

Preserve the OPML groups and category vocabulary accepted by the validator.
Use canonical HTTPS feed endpoints, unique feed URLs and distinct sibling
labels. Site links are optional; omit one rather than inventing it. Do not copy
tracking parameters into feed or site URLs. Adding or removing entries also
requires updating the reviewed inventory in
[`developerfeeds_test.go`](../../internal/developerfeeds/developerfeeds_test.go).

## Manual re-verification

Check changed endpoints before proposing a catalog edit, and recheck when a
user reports a dead feed. This requires network access and is not part of CI.
From the repository root:

```sh
grep -o 'xmlUrl="[^"]*"' internal/developerfeeds/developer-feeds.opml | cut -d'"' -f2 |
while read -r url; do
  printf '%s %s\n' "$(curl -sS -L -o /dev/null --max-time 20 \
    -w '%{http_code}|%{content_type}' "$url")" "$url"
done
```

Expect HTTP 200 and an XML content type (`application/rss+xml`,
`application/atom+xml`, `application/xml` or `text/xml`). `000` means a connection
or resolution failure; 404/410 suggests a moved or retired feed. HTTP 200 with
HTML is not a valid feed. Check `htmlUrl` links separately when they change;
those should resolve to the publisher's page rather than XML.

Reachability is not activity. Inspect the latest publication date against the
publisher's schedule:

```sh
curl -sS -L --max-time 20 "$url" | grep -o -m1 -E '<(pubDate|updated|published)>[^<]*'
```

If a feed fails, look for a publisher-announced replacement before removing it.
Update the canonical endpoint and any affected inventory counts. Run the normal
`make ci` gate and record the manual check's date and result in the pull request,
not in an accumulating verification log here. Keep CI validation offline: no
hostname resolution, outbound request or command is needed to validate OPML.
