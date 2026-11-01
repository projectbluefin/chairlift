package dockerbrew

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// bottleRoot is the only place a bottle may be downloaded from.
const bottleRoot = "https://ghcr.io/v2/homebrew/core/"

// Response size caps. The signed index is about 35 MB today.
const (
	maxIndexBytes   = 512 << 20
	maxFormulaBytes = 8 << 20
)

var (
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+~-]*$`)
)

func bottleURL(name, sha string) string {
	return bottleRoot + name + "/blobs/sha256:" + sha
}

type formulaJSON struct {
	Name     string `json:"name"`
	Versions struct {
		Stable string `json:"stable"`
	} `json:"versions"`
	Revision     int      `json:"revision"`
	Dependencies []string `json:"dependencies"`
	Variations   map[string]*struct {
		Dependencies *[]string `json:"dependencies"`
	} `json:"variations"`
	Bottle struct {
		Stable *struct {
			Files map[string]struct {
				URL    string `json:"url"`
				SHA256 string `json:"sha256"`
			} `json:"files"`
		} `json:"stable"`
	} `json:"bottle"`
}

// dependencies are the formula's dependencies on arch: a per-arch variation
// that names dependencies replaces the base list.
func (f formulaJSON) dependencies(arch string) []string {
	if v := f.Variations[arch]; v != nil && v.Dependencies != nil {
		return *v.Dependencies
	}
	return f.Dependencies
}

// SelectBottles picks the fixed daemon-side set out of a verified index
// payload. The dependency closure of docker-engine on arch must be exactly
// Formulae() plus the system-provided nftables, so a new upstream dependency
// fails closed instead of producing an engine missing a library. Every
// bottle's URL must be GHCR's blob URL for its own digest.
func SelectBottles(payload []byte, arch string) ([]Bottle, error) {
	if Loader(arch) == "" {
		return nil, fmt.Errorf("dockerbrew: unsupported bottle architecture %q", arch)
	}
	var all []formulaJSON
	if err := json.Unmarshal(payload, &all); err != nil {
		return nil, fmt.Errorf("dockerbrew: parse formula index payload: %w", err)
	}
	want := Formulae()
	byName := make(map[string]formulaJSON, len(want))
	for _, f := range all {
		if !slices.Contains(want, f.Name) {
			continue
		}
		if _, dup := byName[f.Name]; dup {
			return nil, fmt.Errorf("dockerbrew: formula %q appears twice in the index", f.Name)
		}
		byName[f.Name] = f
	}

	closure := map[string]bool{}
	queue := []string{want[0]}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if closure[name] {
			continue
		}
		closure[name] = true
		if name == systemDependency {
			continue
		}
		f, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("dockerbrew: docker-engine depends on %q, which is not in the fixed formula set", name)
		}
		queue = append(queue, f.dependencies(arch)...)
	}
	expected := append(slices.Clone(want), systemDependency)
	got := make([]string, 0, len(closure))
	for name := range closure {
		got = append(got, name)
	}
	sort.Strings(expected)
	sort.Strings(got)
	if !slices.Equal(got, expected) {
		return nil, fmt.Errorf("dockerbrew: docker-engine dependency closure on %s is %v, want %v", arch, got, expected)
	}

	out := make([]Bottle, 0, len(want))
	for _, name := range want {
		f := byName[name]
		if !versionPattern.MatchString(f.Versions.Stable) || strings.Contains(f.Versions.Stable, "..") {
			return nil, fmt.Errorf("dockerbrew: formula %q has unusable version %q", name, f.Versions.Stable)
		}
		if f.Revision < 0 {
			return nil, fmt.Errorf("dockerbrew: formula %q has negative revision %d", name, f.Revision)
		}
		if f.Bottle.Stable == nil {
			return nil, fmt.Errorf("dockerbrew: formula %q has no stable bottle", name)
		}
		file, ok := f.Bottle.Stable.Files[arch]
		if !ok {
			return nil, fmt.Errorf("dockerbrew: formula %q has no %s bottle", name, arch)
		}
		if !shaPattern.MatchString(file.SHA256) {
			return nil, fmt.Errorf("dockerbrew: formula %q has malformed bottle digest %q", name, file.SHA256)
		}
		if file.URL != bottleURL(name, file.SHA256) {
			return nil, fmt.Errorf("dockerbrew: formula %q bottle URL %q is not GHCR's blob URL for its digest", name, file.URL)
		}
		out = append(out, Bottle{Name: name, Version: f.Versions.Stable, Revision: f.Revision, SHA256: file.SHA256, URL: file.URL})
	}
	return out, nil
}

// Client fetches the index and bottles. The zero value is production.
type Client struct {
	HTTP     *http.Client
	IndexURL string
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c Client) indexURL() string {
	if c.IndexURL != "" {
		return c.IndexURL
	}
	return IndexURL
}

func (c Client) get(ctx context.Context, url string, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("dockerbrew: fetch %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("dockerbrew: fetch %s: %s", url, resp.Status)
	}
	return resp, nil
}

func (c Client) fetchAll(ctx context.Context, url string, limit int64) ([]byte, error) {
	resp, err := c.get(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("dockerbrew: read %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("dockerbrew: %s is larger than %d bytes", url, limit)
	}
	return data, nil
}

// Index fetches the signed formula index and returns its verified payload.
func (c Client) Index(ctx context.Context) ([]byte, error) {
	data, err := c.fetchAll(ctx, c.indexURL(), maxIndexBytes)
	if err != nil {
		return nil, err
	}
	return VerifyIndex(data)
}

// maxBottleBytes caps one bottle download; a variable so tests can lower it.
var maxBottleBytes int64 = 1 << 30

// Bottle downloads b to w and verifies its sha256. On error w may hold a
// partial or unverified body, which the caller must discard.
func (c Client) Bottle(ctx context.Context, b Bottle, w io.Writer) error {
	if !shaPattern.MatchString(b.SHA256) || b.URL != bottleURL(b.Name, b.SHA256) {
		return fmt.Errorf("dockerbrew: refusing bottle URL %q for %s", b.URL, b.Name)
	}
	// GHCR serves public blobs to an anonymous bearer token; this is what
	// brew itself sends.
	resp, err := c.get(ctx, b.URL, http.Header{"Authorization": {"Bearer QQ=="}})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, maxBottleBytes+1))
	if err != nil {
		return fmt.Errorf("dockerbrew: download %s: %w", b.Name, err)
	}
	if n > maxBottleBytes {
		return fmt.Errorf("dockerbrew: %s bottle exceeds %d bytes", b.Name, maxBottleBytes)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != b.SHA256 {
		return fmt.Errorf("dockerbrew: %s bottle digest %s, want %s", b.Name, got, b.SHA256)
	}
	return nil
}

// Latest reads the UNSIGNED per-formula API for an unprivileged "update
// available" check only. Nothing it returns may be installed; the privileged
// helper re-derives the set from the signed index.
func (c Client) Latest(ctx context.Context) ([]Bottle, error) {
	arch, err := Arch()
	if err != nil {
		return nil, err
	}
	base, ok := strings.CutSuffix(c.indexURL(), ".jws.json")
	if !ok {
		return nil, fmt.Errorf("dockerbrew: index URL %q does not end in .jws.json", c.indexURL())
	}
	docs := make([]json.RawMessage, 0, len(Formulae()))
	for _, name := range Formulae() {
		data, err := c.fetchAll(ctx, base+"/"+name+".json", maxFormulaBytes)
		if err != nil {
			return nil, err
		}
		if !json.Valid(data) {
			return nil, fmt.Errorf("dockerbrew: formula %s: invalid JSON", name)
		}
		docs = append(docs, data)
	}
	payload, err := json.Marshal(docs)
	if err != nil {
		return nil, errors.Join(errors.New("dockerbrew: assemble formulae"), err)
	}
	return SelectBottles(payload, arch)
}
