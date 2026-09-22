package registrytags

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRegistry serves the shapes GHCR actually serves, so the tests exercise
// the client against the registry that was verified on 2026-09-22 rather
// than against an idealized one.
//
// Three of its behaviours are load-bearing and each cost a real request to
// establish:
//
//   - A tag list page carries a Link header naming the *next* page with
//     `n=0`, the registry's "server decides" page size. The next page is
//     reached through `?last=<final tag of the previous page>`.
//   - A resolved tag answers with the manifest media type in the
//     Content-Type header, and the body carries no top-level mediaType at
//     all — GHCR served ghcr.io/ublue-os/bluefin:stable-20260623 exactly
//     that way.
//   - An unknown tag answers 404 with a MANIFEST_UNKNOWN error document
//     rather than an empty body.
type fakeRegistry struct {
	// tags is the complete listing, served in pages of pageSize for every
	// repository the fake is asked about.
	tags     []string
	pageSize int
	// paths are the repository paths this fake answers for. The default is
	// a single one; a test about eviction names more.
	paths []string
	// manifests maps a tag to what resolving it returns.
	manifests map[string]fakeManifest
	// listStatus, when non-zero, makes every tag-list request answer with
	// this status instead of a listing.
	listStatus int

	// mu guards requests, which the handler appends to from its own
	// goroutine so a test can count the requests a cached call did *not*
	// make.
	mu       sync.Mutex
	requests []string
}

// repositories returns the repository paths this fake answers for.
func (f *fakeRegistry) repositories() []string {
	if len(f.paths) == 0 {
		return []string{"org/image"}
	}
	return f.paths
}

// record notes one request path.
func (f *fakeRegistry) record(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, path)
}

// count returns how many requests reached path.
func (f *fakeRegistry) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	total := 0
	for _, requested := range f.requests {
		if requested == path {
			total++
		}
	}
	return total
}

// seen returns every path requested so far, for a failure message.
func (f *fakeRegistry) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// fakeManifest is one tag's answer.
type fakeManifest struct {
	digest      string
	contentType string
	annotation  string
}

func (f *fakeRegistry) handler(t *testing.T) http.Handler {
	t.Helper()

	writeJSON := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Errorf("encoding fake response: %v", err)
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r.URL.Path)

		if r.URL.Path == "/token" {
			writeJSON(w, map[string]string{"token": "test-token"})
			return
		}

		rest, found := strings.CutPrefix(r.URL.Path, "/v2/")
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if strings.HasSuffix(rest, "/tags/list") {
			if f.listStatus != 0 {
				w.WriteHeader(f.listStatus)
				return
			}
			f.writePage(w, r, strings.TrimSuffix(rest, "/tags/list"), writeJSON)
			return
		}

		tag, addressed := "", false
		for _, path := range f.repositories() {
			if remainder, matched := strings.CutPrefix(rest, path+"/manifests/"); matched {
				tag, addressed = remainder, true
				break
			}
		}
		manifest, published := f.manifests[tag]
		if !addressed || !published {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]any{
				"errors": []map[string]string{{"code": "MANIFEST_UNKNOWN"}},
			})
			return
		}

		w.Header().Set("Content-Type", manifest.contentType)
		w.Header().Set("Docker-Content-Digest", manifest.digest)
		document := map[string]any{"schemaVersion": 2}
		if manifest.annotation != "" {
			document["annotations"] = map[string]string{createdAnnotation: manifest.annotation}
		}
		writeJSON(w, document)
	})
}

// writePage serves one page of the listing and the Link header pointing at
// the next, in the form GHCR uses.
func (f *fakeRegistry) writePage(w http.ResponseWriter, r *http.Request, repositoryPath string, writeJSON func(http.ResponseWriter, any)) {
	pageSize := f.pageSize
	if pageSize <= 0 {
		pageSize = 2
	}

	start := 0
	if last := r.URL.Query().Get("last"); last != "" {
		for index, tag := range f.tags {
			if tag == last {
				start = index + 1
				break
			}
		}
	}

	end := start + pageSize
	if end > len(f.tags) {
		end = len(f.tags)
	}
	page := f.tags[start:end]

	if end < len(f.tags) && len(page) > 0 {
		w.Header().Set("Link", "</v2/"+repositoryPath+"/tags/list?last="+page[len(page)-1]+`&n=0>; rel="next"`)
	}
	writeJSON(w, map[string]any{"name": repositoryPath, "tags": page})
}

// newFake starts a fake registry and returns a client aimed at it along with
// the repository reference to pass to the client.
func newFake(t *testing.T, fake *fakeRegistry) (*Client, string) {
	t.Helper()

	server := httptest.NewTLSServer(fake.handler(t))
	t.Cleanup(server.Close)

	return &Client{HTTP: server.Client()}, strings.TrimPrefix(server.URL, "https://") + "/org/image"
}

func TestTagsFollowsEveryPageTheRegistryOffers(t *testing.T) {
	fake := &fakeRegistry{
		tags:     []string{"latest", "stable", "stable-20260623", "lts-testing.20260621", "lts-amd64"},
		pageSize: 2,
	}
	client, repository := newFake(t, fake)

	tags, err := client.Tags(context.Background(), repository)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}

	want := []string{"latest", "stable", "stable-20260623", "lts-testing.20260621", "lts-amd64"}
	if len(tags) != len(want) {
		t.Fatalf("Tags returned %d tags, want %d: %v", len(tags), len(want), tags)
	}
	for index := range want {
		if tags[index] != want[index] {
			t.Errorf("Tags[%d] = %q, want %q", index, tags[index], want[index])
		}
	}

	// Three pages of two, two and one, plus one token request. A client that
	// ignored the Link header would stop after the first page and a client
	// that rebuilt `last` from what it had seen would re-read page one.
	if listings := fake.count("/v2/org/image/tags/list"); listings != 3 {
		t.Errorf("client made %d tag-list requests, want 3 (one per page): %v", listings, fake.seen())
	}
}

func TestTagsStopsAtTheLastPage(t *testing.T) {
	fake := &fakeRegistry{tags: []string{"latest", "stable"}, pageSize: 10}
	client, repository := newFake(t, fake)

	tags, err := client.Tags(context.Background(), repository)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("Tags returned %d tags, want 2", len(tags))
	}
}

func TestTagsReportsAnEmptyRepository(t *testing.T) {
	fake := &fakeRegistry{}
	client, repository := newFake(t, fake)

	tags, err := client.Tags(context.Background(), repository)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("Tags returned %v, want no tags", tags)
	}
}

func TestTagsReportsARegistryFailure(t *testing.T) {
	fake := &fakeRegistry{listStatus: http.StatusInternalServerError}
	client, repository := newFake(t, fake)

	if _, err := client.Tags(context.Background(), repository); err == nil {
		t.Fatal("Tags accepted a 500 from the registry")
	}
}

func TestTagReadsTheCreatedAnnotationFromASingleArchManifest(t *testing.T) {
	fake := &fakeRegistry{
		manifests: map[string]fakeManifest{
			"stable-20260623": {
				digest:      "sha256:9f0201d2",
				contentType: "application/vnd.oci.image.manifest.v1+json",
				annotation:  "2026-06-23T01:57:08Z",
			},
		},
	}
	client, repository := newFake(t, fake)

	tag, err := client.Tag(context.Background(), repository, "stable-20260623")
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}

	if tag.Name != "stable-20260623" {
		t.Errorf("Name = %q, want stable-20260623", tag.Name)
	}
	if tag.Digest != "sha256:9f0201d2" {
		t.Errorf("Digest = %q, want the response header's value", tag.Digest)
	}
	want := time.Date(2026, 6, 23, 1, 57, 8, 0, time.UTC)
	if !tag.Created.Equal(want) {
		t.Errorf("Created = %s, want %s", tag.Created, want)
	}
}

func TestTagAcceptsATagWithNoCreatedAnnotation(t *testing.T) {
	// Signature and architecture-suffixed tags resolve but carry no date.
	// Verified 2026-09-22: ghcr.io/ublue-os/bluefin:lts-amd64 and its
	// sha256-<hex>.sig tags both answer 200 with no created annotation.
	fake := &fakeRegistry{
		manifests: map[string]fakeManifest{
			"lts-amd64": {digest: "sha256:aaaa", contentType: "application/vnd.oci.image.manifest.v1+json"},
		},
	}
	client, repository := newFake(t, fake)

	tag, err := client.Tag(context.Background(), repository, "lts-amd64")
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	if !tag.Created.IsZero() {
		t.Errorf("Created = %s, want the zero time for a tag with no annotation", tag.Created)
	}
	if tag.Digest != "sha256:aaaa" {
		t.Errorf("Digest = %q, want sha256:aaaa", tag.Digest)
	}
}

func TestTagReportsAnUnpublishedTagAsUnknown(t *testing.T) {
	fake := &fakeRegistry{manifests: map[string]fakeManifest{}}
	client, repository := newFake(t, fake)

	_, err := client.Tag(context.Background(), repository, "stable-19990101")
	if !errors.Is(err, ErrUnknownTag) {
		t.Fatalf("Tag error = %v, want ErrUnknownTag", err)
	}
}

func TestTagRejectsAnUnusableTagName(t *testing.T) {
	fake := &fakeRegistry{}
	client, repository := newFake(t, fake)

	// A tag carrying a slash would change the request's shape rather than
	// its final path segment.
	for _, tag := range []string{"", "a/b", "..", "with space", strings.Repeat("t", 129)} {
		if _, err := client.Tag(context.Background(), repository, tag); err == nil {
			t.Errorf("Tag accepted %q", tag)
		}
	}
}

func TestRepositoryRejectsReferencesThatWouldChangeTheRequest(t *testing.T) {
	tests := []struct {
		ref   string
		valid bool
	}{
		{ref: "ghcr.io/ublue-os/bluefin", valid: true},
		{ref: "ghcr.io/projectbluefin/bluefin-lts", valid: true},
		{ref: "registry.example.internal:5000/team/image", valid: true},
		{ref: "127.0.0.1:44321/org/image", valid: true},
		// A tag or digest is a call-site bug: stripping it would list a
		// repository the caller did not name.
		{ref: "ghcr.io/ublue-os/bluefin:stable"},
		{ref: "ghcr.io/ublue-os/bluefin@sha256:abc"},
		// A scheme would be interpolated into a URL.
		{ref: "https://ghcr.io/ublue-os/bluefin"},
		// No repository path at all.
		{ref: "ghcr.io"},
		{ref: ""},
		// Traversal and empty segments.
		{ref: "ghcr.io/../etc"},
		{ref: "ghcr.io/ublue-os/"},
		// A userinfo-shaped host.
		{ref: "user@ghcr.io/ublue-os/bluefin"},
		// Repository paths are lowercase in the distribution spec.
		{ref: "ghcr.io/UBlue-OS/Bluefin"},
	}

	for _, tt := range tests {
		_, _, err := repository(tt.ref)
		if tt.valid && err != nil {
			t.Errorf("repository(%q) rejected a valid reference: %v", tt.ref, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("repository(%q) accepted an unusable reference", tt.ref)
		}
	}
}

func TestNextPageReadsOnlyTheNextRelation(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{header: "", want: ""},
		{header: `</v2/org/image/tags/list?last=x&n=0>; rel="next"`, want: "/v2/org/image/tags/list?last=x&n=0"},
		{header: `</v2/org/image/tags/list?last=x>; rel="prev"`, want: ""},
		{
			header: `<https://example.test/a>; rel="prev", </v2/org/image/tags/list?last=y>; rel="next"`,
			want:   "/v2/org/image/tags/list?last=y",
		},
		// A bare URL with no relation is not a next page.
		{header: `</v2/org/image/tags/list?last=z>`, want: ""},
	}

	for _, tt := range tests {
		if got := nextPage(tt.header); got != tt.want {
			t.Errorf("nextPage(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}
