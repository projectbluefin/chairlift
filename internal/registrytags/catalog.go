package registrytags

import (
	"context"
	"sync"
	"time"
)

// Defaults for a Catalog that does not set its own.
const (
	// DefaultTTL is how long a cached registry answer stays usable. It is
	// short on purpose: the point of reading the registry rather than a
	// baked table is that the answer tracks a repository that publishes
	// daily, so a window that has been open all afternoon must eventually
	// ask again. Fifteen minutes is long enough that re-rendering a
	// calendar, or moving between the pages that show it, costs nothing.
	DefaultTTL = 15 * time.Minute
	// DefaultMaxEntries bounds how many answers a Catalog holds, so a
	// long-running window that visits many repositories cannot grow without
	// bound. A machine manages one image and one image has a few hundred
	// dated tags, so this is far above ordinary use.
	DefaultMaxEntries = 256
)

// Catalog answers the rollback and pin questions from a cached view of one
// registry, so a surface that re-renders does not re-list a repository on
// every pass.
//
// A Catalog is safe for concurrent use: the pages that read it run their
// registry work off the main thread.
//
// A failed read is never cached. The whole reason this package exists is
// that the catalog must show the registry's current state, so an error is
// returned to the caller to render rather than papered over with the last
// answer that worked.
type Catalog struct {
	// Client is the registry reads go through. A nil Client uses a Client
	// with no transport override.
	Client *Client
	// TTL is how long an answer stays usable. Zero uses DefaultTTL.
	TTL time.Duration
	// MaxEntries bounds the catalog's memory. Zero uses DefaultMaxEntries.
	MaxEntries int
	// Now is the clock the TTL is measured against. A nil Now uses
	// time.Now.
	Now func() time.Time

	mu          sync.Mutex
	tags        map[string]cached[[]string]
	resolutions map[string]cached[Tag]
}

// cached is one stored answer and when it was stored.
type cached[T any] struct {
	value   T
	fetched time.Time
}

// client returns the client to use.
func (c *Catalog) client() *Client {
	if c.Client != nil {
		return c.Client
	}
	return &Client{}
}

// ttl returns the configured lifetime.
func (c *Catalog) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return DefaultTTL
}

// maxEntries returns the configured bound.
func (c *Catalog) maxEntries() int {
	if c.MaxEntries > 0 {
		return c.MaxEntries
	}
	return DefaultMaxEntries
}

// now returns the configured clock reading.
func (c *Catalog) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Builds returns the dated builds of repository published on or after since,
// newest first.
//
// The tag list is fetched at most once per TTL however many times this is
// called. No per-tag request is made: the day a build belongs to is in the
// tag, so a calendar of the last ninety days costs the same handful of
// paginated requests as a calendar of the last nine. Resolving a chosen
// build to its digest and precise creation time costs one request, and Tag
// is the call that makes it.
func (c *Catalog) Builds(ctx context.Context, repository string, since time.Time) ([]Build, error) {
	tags, err := c.tagList(ctx, repository)
	if err != nil {
		return nil, err
	}
	return Builds(tags, since), nil
}

// Tag resolves one tag to its digest and creation date, through the cache.
func (c *Catalog) Tag(ctx context.Context, repository, tag string) (Tag, error) {
	key := repository + "\x00" + tag

	c.mu.Lock()
	if entry, ok := c.resolutions[key]; ok && c.now().Sub(entry.fetched) < c.ttl() {
		c.mu.Unlock()
		return entry.value, nil
	}
	c.mu.Unlock()

	resolved, err := c.client().Tag(ctx, repository, tag)
	if err != nil {
		return Tag{}, err
	}

	c.mu.Lock()
	if c.resolutions == nil {
		c.resolutions = make(map[string]cached[Tag])
	}
	evict(c.resolutions, c.maxEntries())
	c.resolutions[key] = cached[Tag]{value: resolved, fetched: c.now()}
	c.mu.Unlock()

	return resolved, nil
}

// tagList returns repository's tags, from the cache when the cached answer is
// still inside its TTL.
func (c *Catalog) tagList(ctx context.Context, repository string) ([]string, error) {
	c.mu.Lock()
	if entry, ok := c.tags[repository]; ok && c.now().Sub(entry.fetched) < c.ttl() {
		c.mu.Unlock()
		return entry.value, nil
	}
	c.mu.Unlock()

	tags, err := c.client().Tags(ctx, repository)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.tags == nil {
		c.tags = make(map[string]cached[[]string])
	}
	evict(c.tags, c.maxEntries())
	c.tags[repository] = cached[[]string]{value: tags, fetched: c.now()}
	c.mu.Unlock()

	return tags, nil
}

// evict drops the oldest entry of entries when it is at capacity, making room
// for the entry the caller is about to add.
//
// It is called with the catalog's lock held. Dropping the oldest answer
// rather than the least recently read one is deliberate: entries are only
// ever read or replaced, never re-ordered, so the oldest is also the one
// furthest past its TTL and the one whose replacement is cheapest.
func evict[T any](entries map[string]cached[T], capacity int) {
	if len(entries) < capacity {
		return
	}

	oldestKey, found := "", false
	var oldestTime time.Time
	for key, entry := range entries {
		if !found || entry.fetched.Before(oldestTime) {
			oldestKey, oldestTime, found = key, entry.fetched, true
		}
	}
	if found {
		delete(entries, oldestKey)
	}
}
