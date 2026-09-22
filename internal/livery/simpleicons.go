package livery

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// simpleIconsManifest lists every brand simpleicons.org publishes, as
// `slug<TAB>title`.
//
// The slugs come from the project's own generated `slugs.md`, not from
// reimplementing its title-to-slug rules. Those rules have edge cases that
// look obvious and are not: "Write.as" is `writedotas`, ".NET" is `dotnet`,
// "Node.js" is `nodedotjs`. A hand-written transform scored 24 out of 25 on a
// random sample, which across 3,461 brands is a hundred-odd brands that would
// simply 404 for the user who picked them.
//
// Embedding the list rather than querying is what makes the picker searchable
// offline and instant; only the chosen mark costs a request.
//
//go:embed assets/simple-icons.txt
var simpleIconsManifest string

// SimpleIconsURL is the site the page links to.
const SimpleIconsURL = "https://simpleicons.org/"

// simpleIconsCDN serves one SVG per brand slug. This is the whole Simple
// Icons integration and deliberately the lightest one available: no vendored
// catalog, no scraper, no metadata sync — one GET for the one mark the user
// asked for.
const simpleIconsCDN = "https://cdn.simpleicons.org/"

// fetchTimeout bounds the round trip. The mark is a few kilobytes; a slow
// network should surface a toast, not a frozen switch.
const fetchTimeout = 10 * time.Second

// ErrIconNotFound reports a slug the service does not publish. The CDN
// answers 404 with a zero-length body for an unknown brand, which is the
// common case of a typo and deserves its own message rather than a generic
// HTTP error.
var ErrIconNotFound = errors.New("livery: no Simple Icons mark with that name")

// FetchFunc is the network seam.
//
// The round trip lives behind a variable for the same reason internal/sbom's
// does: no gated test may make an outbound request, so the package's tests
// point this at a loopback httptest server. Production never replaces it.
type FetchFunc func(ctx context.Context, url string) ([]byte, error)

// Fetch is the function FetchSimpleIcon calls. Tests replace it.
var Fetch FetchFunc = httpFetch

func httpFetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("livery: building request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("livery: reaching simpleicons.org: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrIconNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("livery: simpleicons.org answered %s", resp.Status)
	}
	// The marks are single-path SVGs of a few kilobytes. The cap keeps a
	// misrouted response — a captive-portal login page, say — from being
	// read into memory in full. Reading maxMarkBytes+1 ensures truncated
	// payloads are caught and rejected rather than silently accepted.
	const maxMarkBytes = 256 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMarkBytes+1))
	if err != nil {
		return nil, fmt.Errorf("livery: reading mark: %w", err)
	}
	if len(data) > maxMarkBytes {
		return nil, fmt.Errorf("livery: mark exceeds %d byte limit", maxMarkBytes)
	}
	return data, nil
}

// slugPattern is the character set the service uses for brand slugs.
// Validating before the request turns a bad entry into an immediate message
// instead of a round trip, and keeps user text out of the URL path.
var slugPattern = regexp.MustCompile(`^[a-z0-9.-]{1,64}$`)

// NormalizeSlug lowercases and trims a user's entry into a candidate slug.
//
// Simple Icons slugs are lowercase and strip spaces and most punctuation, so
// accepting "Home Assistant" and asking for "homeassistant" is what a user
// means. It is a convenience, not a lookup table: anything the service does
// not publish still comes back as ErrIconNotFound.
func NormalizeSlug(entry string) string {
	s := strings.ToLower(strings.TrimSpace(entry))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

// ValidSlug reports whether a normalized slug is well-formed.
func ValidSlug(slug string) bool {
	return slugPattern.MatchString(slug)
}

// fillPattern matches the fill attribute the CDN emits on the root element.
var fillPattern = regexp.MustCompile(`fill="[^"]*"`)

// FetchSimpleIcon retrieves a brand mark and prepares it for installation.
//
// The mark is recolored to currentColor, which is what a symbolic surface
// needs: the app-grid button is drawn by the shell at the theme's foreground
// color, so a brand-colored fill would be overridden anyway on some themes
// and clash on others. No caller wants a fixed color, so none can ask for
// one; a surface that draws in color can add the parameter back then.
func FetchSimpleIcon(ctx context.Context, slug string) ([]byte, error) {
	slug = NormalizeSlug(slug)
	if !ValidSlug(slug) {
		return nil, fmt.Errorf("livery: %q is not a valid Simple Icons name", slug)
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	data, err := Fetch(ctx, simpleIconsCDN+slug)
	if err != nil {
		return nil, err
	}
	return prepareSimpleIcon(data)
}

// prepareSimpleIcon validates and recolors a fetched mark.
//
// It is separate from the fetch so the parsing half is exercised without a
// server, and so a body that is not an SVG — the CDN answering with an error
// page, a proxy interposing — is reported as such rather than installed as
// an icon that renders blank.
func prepareSimpleIcon(data []byte) ([]byte, error) {
	if !looksLikeSVG(data) {
		return nil, errors.New("livery: simpleicons.org did not return an SVG")
	}
	const fill = "currentColor"
	svg := string(data)
	if fillPattern.MatchString(svg) {
		svg = fillPattern.ReplaceAllString(svg, `fill="`+fill+`"`)
	} else {
		// A mark served without a fill attribute defaults to black, which is
		// invisible on a dark panel. Give the root one.
		svg = strings.Replace(svg, "<svg ", `<svg fill="`+fill+`" `, 1)
	}
	return []byte(svg), nil
}

// SimpleIcon is one selectable brand mark.
type SimpleIcon struct {
	// Slug is the simpleicons.org identifier, persisted and used in the URL.
	Slug string
	// Title is the brand name as the project writes it.
	Title string
}

var (
	simpleOnce   sync.Once
	simpleIcons  []SimpleIcon
	simpleBySlug map[string]SimpleIcon
)

func loadSimpleIcons() {
	lines := strings.Split(strings.TrimSpace(simpleIconsManifest), "\n")
	simpleIcons = make([]SimpleIcon, 0, len(lines))
	simpleBySlug = make(map[string]SimpleIcon, len(lines))
	for _, line := range lines {
		slug, title, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || slug == "" || title == "" {
			continue
		}
		icon := SimpleIcon{Slug: slug, Title: title}
		simpleIcons = append(simpleIcons, icon)
		simpleBySlug[slug] = icon
	}
	sort.Slice(simpleIcons, func(i, j int) bool {
		return strings.ToLower(simpleIcons[i].Title) < strings.ToLower(simpleIcons[j].Title)
	})
}

// SimpleIcons returns the full brand catalog, in title order.
func SimpleIcons() []SimpleIcon {
	simpleOnce.Do(loadSimpleIcons)
	out := make([]SimpleIcon, len(simpleIcons))
	copy(out, simpleIcons)
	return out
}

// LookupSimpleIcon returns the brand with the given slug.
func LookupSimpleIcon(slug string) (SimpleIcon, bool) {
	simpleOnce.Do(loadSimpleIcons)
	icon, ok := simpleBySlug[slug]
	return icon, ok
}

// SearchSimpleIcons returns brands matching query, best matches first, capped
// at limit.
//
// Ranking mirrors the project picker: prefix matches before substring ones,
// over both the title and the slug, so typing "git" puts Git and GitHub above
// brands that merely contain those letters. An empty query returns the head
// of the catalog so the list is never blank.
func SearchSimpleIcons(query string, limit int) []SimpleIcon {
	simpleOnce.Do(loadSimpleIcons)
	q := strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 {
		limit = len(simpleIcons)
	}

	if q == "" {
		if len(simpleIcons) < limit {
			limit = len(simpleIcons)
		}
		out := make([]SimpleIcon, limit)
		copy(out, simpleIcons[:limit])
		return out
	}

	var prefix, contains []SimpleIcon
	for _, icon := range simpleIcons {
		title := strings.ToLower(icon.Title)
		switch {
		case strings.HasPrefix(title, q) || strings.HasPrefix(icon.Slug, q):
			prefix = append(prefix, icon)
		case strings.Contains(title, q) || strings.Contains(icon.Slug, q):
			contains = append(contains, icon)
		}
	}

	out := append(prefix, contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
