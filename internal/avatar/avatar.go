// Package avatar owns the dinosaur avatar catalog and the illustrations
// bundled for it.
//
// The catalog is the set of dinosaur characters Project Bluefin publishes as
// artwork, transcribed from `projectbluefin/website`'s own
// `src/data/wolves-dinosaur-species.ts`; the illustrations are committed under
// assets/, with their source and revision in assets/NOTICE. It is a pure leaf
// package: no puregotk, no CGO, no GTK, so it is unit-testable on a GTK-less
// host (docs/adr/0007-pure-leaf-packages-route-around-untestable-gtk.md).
//
// Only the catalog and the artwork live here. Decoding the WebP and encoding
// the PNG is internal/avatar's transcoding step, and handing the result to
// AccountsService is the dispatch step; both are separate work, so this
// package deliberately stops at "the bytes the catalog names".
package avatar

import (
	"embed"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
)

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
// The catalog and the artwork it names are both embedded (embeddedArtworkFS),
// so a pick reads from the binary and no network is involved anywhere in this
// package.
//
//go:embed assets/dino-avatars.txt
var dinoManifest string

// embeddedArtworkFS holds every catalog illustration. The tree is flat while
// the repository's is not — katharina.webp lives under characters/header/
// upstream — so embeddedArtwork keys each file by its base name.
//
//go:embed assets/*.webp
var embeddedArtworkFS embed.FS

// ErrAvatarNotFound reports an ID the catalog does not list.
var ErrAvatarNotFound = errors.New("avatar: no catalog entry with that id")

// ErrArtworkNotFound reports a catalog Path the binary carries no bundled
// artwork for. A manifest row added without its illustration is a packaging
// error the caller can name rather than a silent failure to render it.
var ErrArtworkNotFound = errors.New("avatar: no artwork bundled for that path")

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
	// Path is the artwork's location within the source repository, stored
	// literally. It is the key the bundled copy is looked up under.
	Path string
}

// ArtworkFunc reads one catalog illustration from local storage, keyed by the
// entry's literal Path column. It reports false when the binary carries no
// copy, which is FetchAvatar's signal to report ErrArtworkNotFound. It is a
// variable so a test can reach that path.
type ArtworkFunc func(artworkPath string) ([]byte, bool)

// LocalArtwork is the function FetchAvatar reads the artwork from.
var LocalArtwork ArtworkFunc = embeddedArtwork

// embeddedArtwork returns the illustration embedded for one catalog Path.
func embeddedArtwork(artworkPath string) ([]byte, bool) {
	data, err := embeddedArtworkFS.ReadFile("assets/" + path.Base(artworkPath))
	if err != nil {
		return nil, false
	}
	return data, true
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

// FetchAvatar returns one catalog illustration by ID, read from the artwork
// bundled in the binary. The bytes come from the Path column, never a
// derivation from the ID.
func FetchAvatar(id string) ([]byte, error) {
	entry, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAvatarNotFound, id)
	}

	data, ok := LocalArtwork(entry.Path)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrArtworkNotFound, entry.Path)
	}
	return data, nil
}

// placeholderSeed is the text the empty avatar widget derives its colour
// from. Empty means the toolkit picks a colour at random each launch.
var placeholderSeed string

// PlaceholderSeed returns the text that colours the placeholder avatar. It is
// empty in every released binary.
func PlaceholderSeed() string { return placeholderSeed }

// SetPlaceholderSeed fixes the placeholder avatar's colour, so two screenshot
// captures match. It is called exclusively from ChairLift's chairlift_e2e-tagged
// build; no released binary contains a call site.
func SetPlaceholderSeed(seed string) { placeholderSeed = seed }
