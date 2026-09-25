// Package registrytags reads the tags an OCI registry publishes for one image
// repository, and the creation date each dated build carries.
//
// ChairLift resolves every rebase target from tables in internal/imageinfo
// whose entries are verified against the registry by hand and recorded by
// date (ADR-0011). That is the right shape for a small, stable set of stream
// names, and the wrong shape for the question a rollback or pin surface asks:
// which dated builds exist right now. Bluefin publishes a dated tag most
// days, so a table of them would be stale within a day and the answer has to
// come from the registry itself.
//
// This package is the read-only half of that. It performs no privileged
// operation and takes no argument that reaches one — the tags it returns are
// for display and for comparison against what is booted, not for handing to
// `bootc switch`, which resolves its target from internal/imageinfo's tables
// behind the pkexec boundary. ADR-0013 records where the network call lives
// and why pinning to a dated tag is not part of this package.
//
// Every request goes through Client.HTTP, the seam internal/sbom already
// uses, so no gated test reaches the network.
package registrytags

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// maxDocumentBytes caps every JSON document this package decodes. The tag
// list for ghcr.io/ublue-os/bluefin is 1907 tags across 20 pages and one
// manifest is a few kilobytes, so the cap is generous — but it is present
// because these are unauthenticated responses read into a GUI process.
const maxDocumentBytes = 8 << 20

// maxPages bounds pagination. It exists so a registry that answers every
// page with a fresh Link header cannot spin this loop forever; the real
// listing needs 20 pages at GHCR's page size.
const maxPages = 200

// manifestAccept lists every manifest media type a registry may answer a tag
// with.
//
// Sending the index types is load-bearing rather than decorative. Verified
// 2026-09-22 against ghcr.io/ublue-os/bluefin: with this list, `lts` and
// `10` answer with their application/vnd.oci.image.index.v1+json index,
// while a request that negotiates on `*/*` is answered with a single
// architecture's child manifest — which is how the wrong digest for an AI
// stack image was obtained on 2026-09-18 (AGENTS.md).
const manifestAccept = "application/vnd.oci.image.index.v1+json," +
	"application/vnd.oci.image.manifest.v1+json," +
	"application/vnd.docker.distribution.manifest.list.v2+json," +
	"application/vnd.docker.distribution.manifest.v2+json"

// createdAnnotation is the OCI annotation the Bluefin family stamps its
// builds with. It is present on both shapes a tag can resolve to: verified
// 2026-09-22, `ghcr.io/ublue-os/bluefin:stable-20260623` is a single-arch
// image manifest carrying `2026-06-23T01:57:08Z`, and
// `ghcr.io/ublue-os/bluefin:lts-testing.20260621` is an index carrying
// `2026-06-21T21:50:41Z`. A client that reads only indexes, or only
// manifests, therefore reports "no date" for half the family.
const createdAnnotation = "org.opencontainers.image.created"

// ErrUnknownTag reports that the registry does not publish the requested tag.
// It is a normal outcome rather than a transport failure: a dated tag is
// retained for a long time but not forever, and a user can select a day whose
// tag has since been pruned.
var ErrUnknownTag = errors.New("registry does not publish this tag")

// Client reads one registry over HTTP.
type Client struct {
	// HTTP is the transport used for every request. Leaving it nil uses a
	// client with a timeout rather than http.DefaultClient, which has none.
	HTTP *http.Client
}

// httpClient returns the client to use.
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

// Tag is one tag the registry publishes for a repository.
type Tag struct {
	// Name is the tag as the registry spells it, e.g. "stable-20260623".
	Name string
	// Digest is the digest the registry resolved the tag to, from the
	// response's Docker-Content-Digest header. It is the value the registry
	// itself will accept back, which is why it is read from the header
	// rather than recomputed from the body.
	Digest string
	// Created is the org.opencontainers.image.created annotation, or the
	// zero time when the tag carries none. Signature tags
	// (sha256-<hex>.sig) and architecture-suffixed stream tags (lts-amd64)
	// carry no date — verified 2026-09-22 — and are not errors.
	Created time.Time
}

// repository splits a repository reference into the host to reach and the
// path to ask for. Both parts are validated before either reaches a URL.
//
// The reference is the "registry/repository" spelling internal/imageinfo's
// tables already use as their keys (imageinfo.KnownImages returns exactly
// this shape), so a caller never has to reassemble one from an image
// reference. A scheme, a tag, or a digest is a call-site bug and is
// rejected rather than silently stripped, because stripping is how
// "ghcr.io/ublue-os/bluefin:stable" would become a listing of a repository
// the caller did not name.
func repository(ref string) (host, path string, err error) {
	slash := strings.Index(ref, "/")
	if slash < 0 {
		return "", "", fmt.Errorf("repository %q is not registry/path", ref)
	}
	host, path = ref[:slash], ref[slash+1:]
	if !hostPattern.MatchString(host) {
		return "", "", fmt.Errorf("repository %q has an unusable registry host", ref)
	}
	if !validPath(path) {
		return "", "", fmt.Errorf("repository %q has an unusable repository path", ref)
	}
	return host, path, nil
}

// hostPattern accepts a registry host with an optional port. It is
// deliberately narrow: this value is interpolated into a URL, so a value
// carrying a path separator, userinfo, or a scheme must not reach it.
var hostPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?(:[0-9]{1,5})?$`)

// pathSegmentPattern is the OCI distribution specification's repository path
// component: lowercase alphanumerics separated by periods, one or two
// underscores, or one or more dashes.
var pathSegmentPattern = regexp.MustCompile(`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*$`)

// validPath reports whether every segment of a repository path is one the
// distribution specification allows.
func validPath(path string) bool {
	if path == "" {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if !pathSegmentPattern.MatchString(segment) {
			return false
		}
	}
	return true
}

// tagPattern rejects a tag that would change the request's shape rather than
// its last path segment.
var tagPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)

// Tags returns every tag the registry publishes for repository.
//
// Pagination follows the response's Link header verbatim rather than
// composing the next URL. Verified 2026-09-22: GHCR answers a 100-tag page
// with `Link: </v2/ublue-os/bluefin/tags/list?last=…&n=0>; rel="next"`, so
// the page size is the registry's to choose and `n=0` means "server
// decides". A client that appends its own `?n=` or rebuilds `last` from the
// tags it has seen either re-reads a page or stops early; following the
// header is the only form that stays correct if the page size changes.
func (c *Client) Tags(ctx context.Context, ref string) ([]string, error) {
	host, path, err := repository(ref)
	if err != nil {
		return nil, err
	}

	token, err := c.token(ctx, host, path)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("https://%s/v2/%s/tags/list", host, path)
	var tags []string
	for page := 0; ; page++ {
		if page == maxPages {
			return nil, fmt.Errorf("listing %s: registry paginated past %d pages", ref, maxPages)
		}

		body, header, err := c.get(ctx, endpoint, token)
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", ref, err)
		}

		var document struct {
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(body, &document); err != nil {
			return nil, fmt.Errorf("listing %s: registry sent an unreadable tag list: %w", ref, err)
		}
		tags = append(tags, document.Tags...)

		next := nextPage(header.Get("Link"))
		if next == "" {
			return tags, nil
		}
		if !sameHostPath(next) {
			return nil, fmt.Errorf("listing %s: registry sent a pagination link %q that leaves the registry", ref, next)
		}
		endpoint = "https://" + host + next
	}
}

// sameHostPath reports whether a pagination link is a host-relative path, so
// appending it to "https://<host>" cannot change which host the next request
// reaches. The Link header is registry-controlled data: an absolute URL, a
// protocol-relative //host form, or a value like "@evil.example/…" (which
// would turn the original host into URL userinfo) must all be rejected before
// the bearer token is sent to whatever host the concatenation now names.
func sameHostPath(next string) bool {
	parsed, err := url.Parse(next)
	if err != nil {
		return false
	}
	return !parsed.IsAbs() && parsed.Host == "" && parsed.User == nil &&
		strings.HasPrefix(parsed.Path, "/")
}

// nextPage extracts the URL of the next page from a Link header, or returns
// "" when the header names no next page. Only the rel="next" relation is
// followed; a registry that sends any other relation has reached its last
// page.
func nextPage(header string) string {
	for _, link := range strings.Split(header, ",") {
		target, parameters, found := strings.Cut(link, ";")
		if !found || !strings.Contains(parameters, `rel="next"`) {
			continue
		}
		target = strings.TrimSpace(target)
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		return strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
	}
	return ""
}

// Tag resolves one tag to its digest and creation date.
//
// It returns ErrUnknownTag when the registry does not publish the tag. A tag
// that exists but carries no created annotation is returned with a zero
// Created and no error, because "this tag is not a build" is an answer the
// caller asked for, not a failure to answer it.
func (c *Client) Tag(ctx context.Context, ref, tag string) (Tag, error) {
	host, path, err := repository(ref)
	if err != nil {
		return Tag{}, err
	}
	if !tagPattern.MatchString(tag) {
		return Tag{}, fmt.Errorf("tag %q is not a usable tag name", tag)
	}

	token, err := c.token(ctx, host, path)
	if err != nil {
		return Tag{}, err
	}

	endpoint := fmt.Sprintf("https://%s/v2/%s/manifests/%s", host, path, tag)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Tag{}, err
	}
	request.Header.Set("Accept", manifestAccept)
	setToken(request, token)

	response, err := c.httpClient().Do(request)
	if err != nil {
		return Tag{}, fmt.Errorf("resolving %s: %w", tag, err)
	}
	defer func() { _ = response.Body.Close() }()

	// A registry answers an unknown tag with 404 and a MANIFEST_UNKNOWN
	// body. Verified 2026-09-22 against ghcr.io/ublue-os/bluefin.
	if response.StatusCode == http.StatusNotFound {
		return Tag{}, fmt.Errorf("%s: %w", tag, ErrUnknownTag)
	}
	if response.StatusCode != http.StatusOK {
		return Tag{}, fmt.Errorf("resolving %s: registry returned %s", tag, response.Status)
	}

	// The response's Content-Type header is the authority on which shape
	// arrived, not the document's own mediaType field: verified 2026-09-22,
	// ghcr.io serves ghcr.io/ublue-os/bluefin:stable-20260623 with
	// Content-Type application/vnd.oci.image.manifest.v1+json and a body
	// carrying no top-level mediaType at all. Annotations are read from
	// whichever shape arrives, so the caller never has to know which one a
	// tag is — index and single-arch tags both carry the date.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDocumentBytes))
	if err != nil {
		return Tag{}, fmt.Errorf("resolving %s: reading the registry's answer: %w", tag, err)
	}

	var document struct {
		Annotations map[string]string `json:"annotations"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return Tag{}, fmt.Errorf("resolving %s: registry sent an unreadable manifest: %w", tag, err)
	}

	resolved := Tag{Name: tag, Digest: response.Header.Get("Docker-Content-Digest")}
	if raw := document.Annotations[createdAnnotation]; raw != "" {
		created, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return Tag{}, fmt.Errorf("resolving %s: registry sent an unreadable %s annotation %q", tag, createdAnnotation, raw)
		}
		resolved.Created = created.UTC()
	}
	return resolved, nil
}

// token obtains an anonymous pull token. A registry that needs no token
// returns an empty string rather than an error, so the caller's requests go
// out unauthenticated.
func (c *Client) token(ctx context.Context, host, path string) (string, error) {
	endpoint := fmt.Sprintf("https://%s/token?service=%s&scope=%s",
		host, url.QueryEscape(host), url.QueryEscape("repository:"+path+":pull"))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("requesting a pull token from %s: %w", host, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", nil
	}

	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil {
		return "", nil
	}
	return cmp.Or(body.Token, body.AccessToken), nil
}

// get performs one authenticated GET and returns the body and headers.
func (c *Client) get(ctx context.Context, endpoint, token string) ([]byte, http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	setToken(request, token)

	response, err := c.httpClient().Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("registry returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDocumentBytes))
	if err != nil {
		return nil, nil, err
	}
	return body, response.Header, nil
}

// setToken attaches a bearer token when one was issued.
func setToken(request *http.Request, token string) {
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}
