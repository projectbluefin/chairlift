package registrytags

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is the TTL's time source, so a test can age an answer without
// sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// catalogFixture is a fake registry with the usual tag list plus a catalog
// reading it through the fake clock.
func catalogFixture(t *testing.T) (*Catalog, *fakeRegistry, *fakeClock, string) {
	t.Helper()

	fake := &fakeRegistry{
		tags:     []string{"latest", "stable-20260623", "stable-20260622", "lts-amd64"},
		pageSize: 2,
		manifests: map[string]fakeManifest{
			"stable-20260623": {
				digest:      "sha256:9f0201d2",
				contentType: "application/vnd.oci.image.manifest.v1+json",
				annotation:  "2026-06-23T01:57:08Z",
			},
		},
	}
	client, repository := newFake(t, fake)
	clock := newFakeClock()

	return &Catalog{Client: client, Now: clock.Now}, fake, clock, repository
}

func TestCatalogServesBuildsFromItsCacheWithinTheLifetime(t *testing.T) {
	catalog, fake, _, repository := catalogFixture(t)
	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	first, err := catalog.Builds(context.Background(), repository, since)
	if err != nil {
		t.Fatalf("Builds: %v", err)
	}
	listings := fake.count("/v2/org/image/tags/list")

	// A window re-renders the calendar, and moving between pages that show
	// it re-reads the same catalog. Neither may re-list the repository.
	for range 3 {
		again, err := catalog.Builds(context.Background(), repository, since)
		if err != nil {
			t.Fatalf("Builds: %v", err)
		}
		if len(again) != len(first) {
			t.Fatalf("cached Builds returned %d builds, first call returned %d", len(again), len(first))
		}
	}

	if got := fake.count("/v2/org/image/tags/list"); got != listings {
		t.Errorf("four Builds calls made %d tag-list requests, want %d — the catalog re-listed inside its TTL",
			got, listings)
	}
	if listings == 0 {
		t.Fatal("the first Builds call made no request at all")
	}
	if len(first) != 2 {
		t.Errorf("Builds returned %+v, want the two dated tags", first)
	}
}

func TestCatalogRelistsOnceTheAnswerExpires(t *testing.T) {
	catalog, fake, clock, repository := catalogFixture(t)
	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	if _, err := catalog.Builds(context.Background(), repository, since); err != nil {
		t.Fatalf("Builds: %v", err)
	}
	listings := fake.count("/v2/org/image/tags/list")

	// Just inside the TTL the cached answer still stands...
	clock.advance(DefaultTTL - time.Second)
	if _, err := catalog.Builds(context.Background(), repository, since); err != nil {
		t.Fatalf("Builds: %v", err)
	}
	if got := fake.count("/v2/org/image/tags/list"); got != listings {
		t.Errorf("Builds re-listed %s before its TTL elapsed", DefaultTTL-time.Second)
	}

	// ...and past it the repository is read again. The point of reading the
	// registry rather than a baked table is that the answer tracks a
	// repository that publishes daily, so a stale answer must not be served
	// forever.
	clock.advance(2 * time.Second)
	if _, err := catalog.Builds(context.Background(), repository, since); err != nil {
		t.Fatalf("Builds: %v", err)
	}
	if got := fake.count("/v2/org/image/tags/list"); got <= listings {
		t.Errorf("Builds served an answer %s past its TTL (%d listings, want more than %d)",
			DefaultTTL+time.Second, got, listings)
	}
}

func TestCatalogHonoursAConfiguredLifetime(t *testing.T) {
	catalog, fake, clock, repository := catalogFixture(t)
	catalog.TTL = time.Minute

	if _, err := catalog.Builds(context.Background(), repository, time.Time{}); err != nil {
		t.Fatalf("Builds: %v", err)
	}
	listings := fake.count("/v2/org/image/tags/list")

	clock.advance(2 * time.Minute)
	if _, err := catalog.Builds(context.Background(), repository, time.Time{}); err != nil {
		t.Fatalf("Builds: %v", err)
	}
	if got := fake.count("/v2/org/image/tags/list"); got <= listings {
		t.Errorf("a one-minute TTL did not expire after two minutes (%d listings, want more than %d)", got, listings)
	}
}

func TestCatalogResolvesOneTagOnce(t *testing.T) {
	catalog, fake, _, repository := catalogFixture(t)

	first, err := catalog.Tag(context.Background(), repository, "stable-20260623")
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	second, err := catalog.Tag(context.Background(), repository, "stable-20260623")
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}

	if !first.Created.Equal(second.Created) || first.Digest != second.Digest {
		t.Errorf("cached Tag = %+v, want %+v", second, first)
	}
	if got := fake.count("/v2/org/image/manifests/stable-20260623"); got != 1 {
		t.Errorf("resolving one tag twice made %d manifest requests, want 1", got)
	}
}

func TestCatalogDoesNotCacheAFailedRead(t *testing.T) {
	catalog, fake, _, repository := catalogFixture(t)
	fake.listStatus = 500

	if _, err := catalog.Builds(context.Background(), repository, time.Time{}); err == nil {
		t.Fatal("Builds accepted a failing registry")
	}

	// The catalog exists to show the registry's current state. Caching the
	// failure would keep showing an error after the registry recovered;
	// falling back to a stale listing would be worse, because the surface
	// would show a catalog that is not the registry's.
	fake.listStatus = 0
	builds, err := catalog.Builds(context.Background(), repository, time.Time{})
	if err != nil {
		t.Fatalf("Builds after the registry recovered: %v", err)
	}
	if len(builds) != 2 {
		t.Errorf("Builds returned %+v, want the two dated tags", builds)
	}
}

func TestCatalogEvictsItsOldestAnswerAtTheBound(t *testing.T) {
	catalog, fake, _, fakeRepository := catalogFixture(t)
	catalog.MaxEntries = 1
	fake.paths = []string{"org/image", "org/other"}

	// The two repositories must be reachable, so take the fake's own host
	// from the reference newFake already built rather than inventing one.
	host, found := strings.CutSuffix(fakeRepository, "/org/image")
	if !found {
		t.Fatalf("newFake returned %q, which no longer ends in /org/image", fakeRepository)
	}
	first := host + "/org/image"
	second := host + "/org/other"

	if _, err := catalog.Builds(context.Background(), first, time.Time{}); err != nil {
		t.Fatalf("Builds(%s): %v", first, err)
	}
	if _, err := catalog.Builds(context.Background(), second, time.Time{}); err != nil {
		t.Fatalf("Builds(%s): %v", second, err)
	}

	// The bound is one, so adding the second repository dropped the first.
	// Re-reading it is therefore a fresh listing rather than a cache hit.
	before := fake.count("/v2/org/image/tags/list")
	if _, err := catalog.Builds(context.Background(), first, time.Time{}); err != nil {
		t.Fatalf("Builds(%s): %v", first, err)
	}
	if got := fake.count("/v2/org/image/tags/list"); got <= before {
		t.Errorf("the oldest answer survived a bound of one (%d listings, want more than %d)", got, before)
	}
}

func TestCatalogIsSafeForConcurrentReaders(t *testing.T) {
	catalog, _, _, repository := catalogFixture(t)

	var group sync.WaitGroup
	for worker := range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 4 {
				if _, err := catalog.Builds(context.Background(), repository, time.Time{}); err != nil {
					t.Errorf("worker %d: Builds: %v", worker, err)
					return
				}
				if _, err := catalog.Tag(context.Background(), repository, "stable-20260623"); err != nil {
					t.Errorf("worker %d: Tag: %v", worker, err)
					return
				}
			}
		}()
	}
	group.Wait()

	// The pages that read the catalog run their registry work off the GTK
	// main thread, so concurrent readers are the ordinary case rather than
	// an edge one.
}
