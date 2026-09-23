package livery

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"
)

// cncfManifest lists every CNCF project that publishes a color icon, as
// `id<TAB>name<TAB>path`.
//
// Both the name and the path are stored rather than derived, because deriving
// either is wrong often enough to matter. Thirty of the 214 icon paths do not
// follow `<id>-icon-color.svg` — cilium ships `cilium_icon-color.svg`,
// kubeflow-notebooks a bare `icon-color.svg`, kmesh uppercase — so a derived
// filename 404s on exactly those. And title-casing the directory name gives
// "Cloudcustodian", "Opentelemetry", "Etcd": the artwork repository's own
// example pages carry the real names, and those are what is recorded here.
//
// The manifest is a few kilobytes of text, so it is embedded; the artwork
// itself is fetched on demand, because 214 color SVGs is megabytes nobody
// needs until they pick one.
//
//go:embed assets/cncf-projects.txt
var cncfManifest string

// cncfArtworkRaw serves the icon files.
const cncfArtworkRaw = "https://raw.githubusercontent.com/cncf/artwork/main/"

// CNCFArtworkURL is the repository the dock marks come from, linked from the
// page so the provenance of the artwork is visible.
const CNCFArtworkURL = "https://github.com/cncf/artwork"

// CNCFProject is one selectable project mark.
type CNCFProject struct {
	// ID is the cncf/artwork directory name, persisted in GSettings.
	ID string
	// Name is the user-visible label the search matches against.
	Name string
	// path is the icon's location within the artwork repository.
	path string
}

var (
	cncfOnce     sync.Once
	cncfProjects []CNCFProject
	cncfByID     map[string]CNCFProject
)

func loadCNCF() {
	lines := strings.Split(strings.TrimSpace(cncfManifest), "\n")
	cncfProjects = make([]CNCFProject, 0, len(lines))
	cncfByID = make(map[string]CNCFProject, len(lines))
	for _, line := range lines {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) != 3 {
			continue
		}
		id, name, path := fields[0], fields[1], fields[2]
		if id == "" || path == "" {
			continue
		}
		if name == "" {
			name = cncfDisplayName(id)
		}
		p := CNCFProject{ID: id, Name: name, path: path}
		cncfProjects = append(cncfProjects, p)
		cncfByID[id] = p
	}
}

// cncfDisplayName is the fallback label for a project the artwork repository's
// example pages do not name — two of the 214 at the time of writing.
//
// It is a presentation convenience, not a brand database: "bank-vaults"
// becomes "Bank Vaults". Anything it gets wrong is still findable, because
// search matches the id as well as the label.
func cncfDisplayName(id string) string {
	parts := strings.Split(id, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

// LookupCNCF returns the project with the given id.
func LookupCNCF(id string) (CNCFProject, bool) {
	cncfOnce.Do(loadCNCF)
	p, ok := cncfByID[id]
	return p, ok
}

// IndexOfCNCF returns a project's position in the catalog, or -1.
func IndexOfCNCF(id string) int {
	cncfOnce.Do(loadCNCF)
	for i, p := range cncfProjects {
		if p.ID == id {
			return i
		}
	}
	return -1
}

// DefaultCNCFID is the project a fresh install starts from.
const DefaultCNCFID = "kubernetes"

// NextCNCFID returns the project after id, wrapping at the end.
//
// Like NextID it is total: an unknown id advances to the first project rather
// than erroring, so rotation can never leave the selection unset.
func NextCNCFID(id string) string {
	cncfOnce.Do(loadCNCF)
	if len(cncfProjects) == 0 {
		return ""
	}
	i := IndexOfCNCF(id)
	if i < 0 || i == len(cncfProjects)-1 {
		return cncfProjects[0].ID
	}
	return cncfProjects[i+1].ID
}

// FetchCNCFIcon retrieves a project's color mark.
//
// The artwork is full color and is installed under a non-symbolic icon name,
// so unlike the panel and app-grid marks it is not recolored by the theme —
// which is the whole point of the dock section.
func FetchCNCFIcon(ctx context.Context, id string) ([]byte, error) {
	project, ok := LookupCNCF(id)
	if !ok {
		return nil, fmt.Errorf("livery: %q is not a CNCF project with published artwork", id)
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	data, err := Fetch(ctx, cncfArtworkRaw+project.path)
	if err != nil {
		return nil, err
	}
	if !looksLikeSVG(data) {
		return nil, fmt.Errorf("livery: cncf/artwork did not return an SVG for %s", id)
	}
	return data, nil
}

// SearchCNCF returns projects matching query, best matches first, capped at
// limit.
//
// Ranking is prefix-first then substring, over both the id and the display
// name: "k8s" finds nothing but "kub" finds kubernetes, kubeflow, kubewarden
// in that order, and searching "etcd" puts etcd above any project that merely
// contains those letters. An empty query returns the head of the catalog, so
// the list is never blank before typing.
func SearchCNCF(query string, limit int) []CNCFProject {
	cncfOnce.Do(loadCNCF)
	q := strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 {
		limit = len(cncfProjects)
	}

	if q == "" {
		if len(cncfProjects) < limit {
			limit = len(cncfProjects)
		}
		out := make([]CNCFProject, limit)
		copy(out, cncfProjects[:limit])
		return out
	}

	var prefix, contains []CNCFProject
	for _, p := range cncfProjects {
		id := strings.ToLower(p.ID)
		name := strings.ToLower(p.Name)
		switch {
		case strings.HasPrefix(id, q) || strings.HasPrefix(name, q):
			prefix = append(prefix, p)
		case strings.Contains(id, q) || strings.Contains(name, q):
			contains = append(contains, p)
		}
	}

	out := append(prefix, contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
