package avatar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
)

// swapFetch points the network seam at fn for one test and restores the real
// one afterwards, so no test leaves production behavior replaced.
func swapFetch(t *testing.T, fn FetchFunc) {
	t.Helper()
	original := Fetch
	Fetch = fn
	t.Cleanup(func() { Fetch = original })
}

func TestCatalogParsesEveryManifestLine(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(dinoManifest), "\n")
	entries := Catalog()

	if len(entries) == 0 {
		t.Fatal("the embedded catalog parsed to zero entries")
	}
	if len(entries) != len(lines) {
		t.Errorf("catalog holds %d entries for %d manifest lines; a malformed line is being dropped silently",
			len(entries), len(lines))
	}

	for i, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			t.Errorf("line %d has %d tab-separated columns, want 4: %q", i+1, len(fields), line)
			continue
		}
		for j, field := range fields {
			if strings.TrimSpace(field) == "" {
				t.Errorf("line %d column %d is empty: %q", i+1, j, line)
			}
			if field != strings.TrimSpace(field) {
				t.Errorf("line %d column %d carries surrounding whitespace: %q", i+1, j, field)
			}
		}
	}
}

func TestCatalogIdentifiersAndPathsAreUnique(t *testing.T) {
	seenID := make(map[string]int)
	seenPath := make(map[string]int)
	for i, entry := range Catalog() {
		if previous, ok := seenID[entry.ID]; ok {
			t.Errorf("id %q appears at entries %d and %d", entry.ID, previous, i)
		}
		seenID[entry.ID] = i

		if previous, ok := seenPath[entry.Path]; ok {
			t.Errorf("path %q appears at entries %d and %d", entry.Path, previous, i)
		}
		seenPath[entry.Path] = i
	}
}

func TestCatalogPathsAreLiteralRepositoryPaths(t *testing.T) {
	for _, entry := range Catalog() {
		switch {
		case strings.HasPrefix(entry.Path, "/"):
			t.Errorf("%s: path %q is absolute, want repository-relative", entry.ID, entry.Path)
		case strings.HasPrefix(entry.Path, "./"):
			t.Errorf("%s: path %q carries a ./ prefix, want the literal repository path", entry.ID, entry.Path)
		case path.Clean(entry.Path) != entry.Path:
			t.Errorf("%s: path %q is not in clean form (%q)", entry.ID, entry.Path, path.Clean(entry.Path))
		case strings.Contains(entry.Path, "\\"):
			t.Errorf("%s: path %q uses a backslash", entry.ID, entry.Path)
		case !strings.HasSuffix(entry.Path, ".webp"):
			t.Errorf("%s: path %q is not a WebP illustration", entry.ID, entry.Path)
		}
	}
}

func TestLookupResolvesEveryCatalogIdentifier(t *testing.T) {
	for _, want := range Catalog() {
		got, ok := Lookup(want.ID)
		if !ok {
			t.Errorf("Lookup(%q) missed an id the catalog lists", want.ID)
			continue
		}
		if got != want {
			t.Errorf("Lookup(%q) = %+v, want %+v", want.ID, got, want)
		}
	}
}

func TestLookupMissesIdentifiersTheCatalogDoesNotList(t *testing.T) {
	for _, id := range []string{"", " ", "Bluefin", "bluefin ", "torosaurus-latus", "katharina"} {
		if got, ok := Lookup(id); ok {
			t.Errorf("Lookup(%q) resolved to %+v; ids are resolved literally", id, got)
		}
	}
}

func TestLookupPathResolvesTheLiteralColumn(t *testing.T) {
	for _, want := range Catalog() {
		got, ok := LookupPath(want.Path)
		if !ok {
			t.Errorf("LookupPath(%q) missed a path the catalog lists", want.Path)
			continue
		}
		if got != want {
			t.Errorf("LookupPath(%q) = %+v, want %+v", want.Path, got, want)
		}
	}
}

func TestLookupPathRejectsPathsNoColumnCarries(t *testing.T) {
	entries := Catalog()

	// A path derived from an identifier is the tempting shortcut this lookup
	// exists to refuse: dakotaraptor's artwork is dakota.webp, so the derived
	// form is a real file in the repository but not this entry's path.
	cases := []string{
		"",
		"public/characters/karl.webp ",
		" public/characters/karl.webp",
		"public/characters/KARL.webp",
		"./public/characters/karl.webp",
		"public/characters/dakotaraptor.webp",
		"public/characters/kentrosaurus.webp",
		"bluefin.webp",
		"characters/bluefin.webp",
	}
	for _, p := range cases {
		if got, ok := LookupPath(p); ok {
			t.Errorf("LookupPath(%q) resolved to %+v; resolution is literal and strict", p, got)
		}
	}

	// Guard the shortcut itself: the derived path must belong to no entry, or
	// the case above would pass for the wrong reason.
	derived := make(map[string]string, len(entries))
	for _, entry := range entries {
		derived["public/characters/"+entry.ID+".webp"] = entry.Path
	}
	for candidate, owner := range derived {
		if candidate == owner {
			continue
		}
		if _, ok := LookupPath(candidate); ok {
			t.Errorf("LookupPath(%q) resolved; expected it to be absent", candidate)
		}
	}
}

func TestArtworkURLIsDerivedFromPathAtThePinnedCommit(t *testing.T) {
	const wantPrefix = "https://raw.githubusercontent.com/projectbluefin/website/" + PinnedCommit + "/"

	if !strings.Contains(SourceRepository, "projectbluefin/website") {
		t.Errorf("SourceRepository = %q, want the artwork's own repository", SourceRepository)
	}
	for _, entry := range Catalog() {
		got := ArtworkURL(entry)
		if got != wantPrefix+entry.Path {
			t.Errorf("ArtworkURL(%s) = %q, want %q", entry.ID, got, wantPrefix+entry.Path)
		}
	}
}

func TestFetchAvatarRequestsTheLiteralPathForEveryID(t *testing.T) {
	var requested []string
	swapFetch(t, func(_ context.Context, url string) ([]byte, error) {
		requested = append(requested, url)
		return []byte("webp"), nil
	})

	for _, entry := range Catalog() {
		if _, err := FetchAvatar(context.Background(), entry.ID); err != nil {
			t.Fatalf("FetchAvatar(%q): %v", entry.ID, err)
		}
	}

	if len(requested) != len(Catalog()) {
		t.Fatalf("made %d requests for %d catalog entries", len(requested), len(Catalog()))
	}
	for i, entry := range Catalog() {
		want := dinoArtworkRaw + entry.Path
		if requested[i] != want {
			t.Errorf("request %d was %q, want %q (literal Path column, not a derived filename)",
				i, requested[i], want)
		}
	}
}

func TestFetchAvatarRejectsAnUnknownIDBeforeAnyRequest(t *testing.T) {
	called := false
	swapFetch(t, func(_ context.Context, _ string) ([]byte, error) {
		called = true
		return nil, nil
	})

	if _, err := FetchAvatar(context.Background(), "stegosaurus"); !errors.Is(err, ErrAvatarNotFound) {
		t.Errorf("FetchAvatar(unknown) error = %v, want ErrAvatarNotFound", err)
	}
	if called {
		t.Error("an unknown id reached the network; the catalog must answer first")
	}
}

func TestFetchAvatarPropagatesTheSeamError(t *testing.T) {
	want := errors.New("boom")
	swapFetch(t, func(_ context.Context, _ string) ([]byte, error) { return nil, want })

	if _, err := FetchAvatar(context.Background(), "karl"); !errors.Is(err, want) {
		t.Errorf("FetchAvatar error = %v, want %v", err, want)
	}
}

func TestHTTPFetchReportsMissingArtwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	if _, err := httpFetch(context.Background(), server.URL+"/public/characters/gone.webp"); !errors.Is(err, ErrArtworkNotFound) {
		t.Errorf("httpFetch(404) error = %v, want ErrArtworkNotFound", err)
	}
}

func TestHTTPFetchReportsOtherFailures(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError, http.StatusBadGateway} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))

		_, err := httpFetch(context.Background(), server.URL)
		server.Close()

		if err == nil {
			t.Errorf("httpFetch(%d) returned no error", status)
			continue
		}
		if errors.Is(err, ErrArtworkNotFound) {
			t.Errorf("httpFetch(%d) reported a missing file: %v", status, err)
		}
	}
}

func TestHTTPFetchReturnsTheBodyAndReadsNothingPastTheLimit(t *testing.T) {
	const want = "RIFF....WEBP"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(want))
	}))
	defer server.Close()

	got, err := httpFetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("httpFetch: %v", err)
	}
	if string(got) != want {
		t.Errorf("httpFetch body = %q, want %q", got, want)
	}
}

func TestHTTPFetchRejectsArtworkPastTheLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxArtworkBytes+1))
	}))
	defer server.Close()

	if _, err := httpFetch(context.Background(), server.URL); err == nil {
		t.Fatal("httpFetch accepted a body past the size limit")
	}
}

func TestHTTPFetchHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("late"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := httpFetch(ctx, server.URL); err == nil {
		t.Error("httpFetch returned no error for a cancelled context")
	}
}
