// Package avatar owns the dinosaur avatar catalog and the seam that fetches
// one illustration from it.
//
// The catalog is the set of dinosaur characters Project Bluefin publishes as
// artwork, transcribed from `projectbluefin/website`'s own
// `src/data/wolves-dinosaur-species.ts` at the pinned commit below. It is a
// pure leaf package: no puregotk, no CGO, no GTK, so it is unit-testable on a
// GTK-less host (docs/adr/0007-pure-leaf-packages-route-around-untestable-gtk.md).
//
// Only the catalog and the fetch live here. Decoding the WebP and encoding the
// PNG is internal/avatar's transcoding step, and handing the result to
// AccountsService is the dispatch step; both are separate work, so this
// package deliberately stops at "the bytes the catalog names".
package avatar

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PinnedCommit is the `projectbluefin/website` revision every catalog Path is
// resolved against.
//
// The artwork is pinned to a commit, not a branch: an avatar the user picked
// must keep resolving to the same illustration after the website repository
// moves on, and `main` there is a live site.
const PinnedCommit = "92b81281649c0795f16f266f712c9a2798af54c4"

// SourceRepository is the repository the illustrations come from. It is
// exported so the picker can link to the provenance of the artwork.
const SourceRepository = "https://github.com/projectbluefin/website"

// dinoArtworkRaw serves one file of the repository at the pinned commit.
//
// The Path column is appended verbatim. Nothing here rewrites, normalizes, or
// derives a path from an ID — see LookupPath for why that matters.
const dinoArtworkRaw = "https://raw.githubusercontent.com/projectbluefin/website/" + PinnedCommit + "/"

// dinoManifest is the embedded catalog, as `id<TAB>common name<TAB>species<TAB>path`.
//
// The path is stored literally rather than derived from the ID for the same
// reason internal/livery's CNCF manifest stores it: derivation is wrong often
// enough to matter. `dakotaraptor` lives at `characters/dakota.webp` and
// `kentrosaurus` at `characters/header/katharina.webp`, so a derived
// `<id>.webp` would 404 on both. The common name is stored for the same
// reason: title-casing an ID yields "Bob Torosaurus" where the project writes
// "Bob".
//
// Embedding the catalog is what makes the picker complete and instant with no
// network: ten lines of text against megabytes of artwork nobody needs until
// they pick one.
//
//go:embed assets/dino-avatars.txt
var dinoManifest string

// fetchTimeout bounds the round trip. A single illustration is a couple of
// hundred kilobytes; a slow network should surface a failure, not a frozen
// dialog.
const fetchTimeout = 10 * time.Second

// maxArtworkBytes caps what one response may contribute to memory.
//
// The largest illustration in the catalog is 209 KiB, so this is roughly
// tenfold headroom for the file growing. The cap exists for the response that
// is not the file at all — a captive portal's login page, a proxy's error
// document — and reading the limit plus one byte is what catches a truncated
// payload instead of silently accepting it.
const maxArtworkBytes = 2 * 1024 * 1024

// ErrAvatarNotFound reports an ID the catalog does not list.
var ErrAvatarNotFound = errors.New("avatar: no catalog entry with that id")

// ErrArtworkNotFound reports a Path the pinned commit does not serve. The raw
// endpoint answers 404 for a moved or misspelled path, which is the failure a
// catalog edit causes and deserves its own message rather than a generic HTTP
// error.
var ErrArtworkNotFound = errors.New("avatar: the pinned commit serves no artwork at that path")

// Avatar is one selectable dinosaur character.
type Avatar struct {
	// ID is the catalog identifier, persisted in settings and used for
	// lookup. It is stable: it is not the display name and does not change
	// when the displayed name does.
	ID string
	// CommonName is the character's name as the project publishes it, or the
	// genus where the project redacts it.
	CommonName string
	// Species is the scientific name.
	Species string
	// Path is the artwork's location within the source repository at
	// PinnedCommit, stored literally.
	Path string
}

// FetchFunc is the network seam.
//
// The round trip lives behind a variable for the same reason internal/sbom's
// and internal/livery's do: no test may make an outbound request, so the
// package's tests point this at a loopback httptest server. Production never
// replaces it.
type FetchFunc func(ctx context.Context, url string) ([]byte, error)

// Fetch is the function FetchAvatar calls. Tests replace it.
var Fetch FetchFunc = httpFetch

func httpFetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("avatar: building request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("avatar: reaching the artwork repository: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrArtworkNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("avatar: the artwork repository answered %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtworkBytes+1))
	if err != nil {
		return nil, fmt.Errorf("avatar: reading artwork: %w", err)
	}
	if len(data) > maxArtworkBytes {
		return nil, fmt.Errorf("avatar: artwork exceeds the %d byte limit", maxArtworkBytes)
	}
	return data, nil
}

var (
	loadOnce      sync.Once
	catalog       []Avatar
	catalogByID   map[string]Avatar
	catalogByPath map[string]Avatar
)

func load() {
	lines := strings.Split(strings.TrimSpace(dinoManifest), "\n")
	catalog = make([]Avatar, 0, len(lines))
	catalogByID = make(map[string]Avatar, len(lines))
	catalogByPath = make(map[string]Avatar, len(lines))
	for _, line := range lines {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) != 4 {
			continue
		}
		entry := Avatar{
			ID:         fields[0],
			CommonName: fields[1],
			Species:    fields[2],
			Path:       fields[3],
		}
		if entry.ID == "" || entry.CommonName == "" || entry.Species == "" || entry.Path == "" {
			continue
		}
		catalog = append(catalog, entry)
		catalogByID[entry.ID] = entry
		catalogByPath[entry.Path] = entry
	}
}

// Catalog returns every dinosaur avatar, in manifest order.
func Catalog() []Avatar {
	loadOnce.Do(load)
	out := make([]Avatar, len(catalog))
	copy(out, catalog)
	return out
}

// Lookup returns the avatar with the given ID.
func Lookup(id string) (Avatar, bool) {
	loadOnce.Do(load)
	entry, ok := catalogByID[id]
	return entry, ok
}

// LookupPath returns the avatar whose Path column is exactly path.
//
// Resolution is literal and strict: no trimming, no case folding, no
// `./`-prefix or `<id>.webp` fallback. A caller holding an identifier from
// somewhere other than this catalog — a settings file written by a future
// version, a hand-edited path — gets a miss it can report, rather than a
// plausible-looking guess that silently substitutes someone else's avatar.
func LookupPath(path string) (Avatar, bool) {
	loadOnce.Do(load)
	entry, ok := catalogByPath[path]
	return entry, ok
}

// ArtworkURL returns the pinned URL the entry's artwork is served from.
func ArtworkURL(entry Avatar) string {
	return dinoArtworkRaw + entry.Path
}

// FetchAvatar retrieves one catalog illustration by ID.
//
// The URL is composed from the entry's literal Path column, never from the ID,
// so a catalog whose filenames disagree with its identifiers — which three of
// the ten do — still fetches the right file.
func FetchAvatar(ctx context.Context, id string) ([]byte, error) {
	entry, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAvatarNotFound, id)
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	return Fetch(ctx, ArtworkURL(entry))
}
