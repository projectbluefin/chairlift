package livery

import (
	"strings"
	"testing"
)

// TestSimpleIconsCatalogLoads asserts the embedded brand list parses.
func TestSimpleIconsCatalogLoads(t *testing.T) {
	simpleOnce.Do(loadSimpleIcons)
	icons := simpleIcons
	if len(icons) < 3000 {
		t.Fatalf("catalog has %d brands, want simpleicons.org's full set", len(icons))
	}
	for _, icon := range icons {
		if icon.Slug == "" || icon.Title == "" {
			t.Fatalf("incomplete entry %+v", icon)
		}
	}
}

// TestSlugsComeFromTheProjectsOwnTable is the regression test for the reason
// the manifest exists.
//
// simpleicons.org derives slugs from titles by rules with edge cases that a
// reimplementation gets wrong — a hand-written transform scored 24 of 25 on a
// random sample, which across 3,461 brands is a hundred that would 404 for
// whoever picked them. These four are the shapes that break naive rules.
func TestSlugsComeFromTheProjectsOwnTable(t *testing.T) {
	want := map[string]string{
		"Write.as": "writedotas",
		".NET":     "dotnet",
		"Node.js":  "nodedotjs",
		"C++":      "cplusplus",
	}
	byTitle := map[string]string{}
	simpleOnce.Do(loadSimpleIcons)
	for _, icon := range simpleIcons {
		byTitle[icon.Title] = icon.Slug
	}
	for title, slug := range want {
		if got := byTitle[title]; got != slug {
			t.Errorf("%q has slug %q, want %q", title, got, slug)
		}
	}
}

// TestBrandSearchRanksPrefixMatchesFirst asserts typing a brand's own name
// surfaces it rather than burying it behind incidental substring hits.
func TestBrandSearchRanksPrefixMatchesFirst(t *testing.T) {
	results := SearchSimpleIcons("mastodon", 10)
	if len(results) == 0 {
		t.Fatal("no results for mastodon")
	}
	if !strings.EqualFold(results[0].Title, "Mastodon") {
		t.Errorf("first result is %q, want Mastodon", results[0].Title)
	}
}

// TestBrandSearchFindsByTitleOrSlug covers both haystacks, since a user may
// type either what they see or what the URL uses.
func TestBrandSearchFindsByTitleOrSlug(t *testing.T) {
	for _, query := range []string{"Node.js", "nodedotjs", "node"} {
		var found bool
		for _, icon := range SearchSimpleIcons(query, 50) {
			if icon.Slug == "nodedotjs" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("searching %q did not find Node.js", query)
		}
	}
}

// TestBrandSearchWithNoMatchesReturnsEmpty asserts nonsense yields nothing
// rather than the whole catalog.
func TestBrandSearchWithNoMatchesReturnsEmpty(t *testing.T) {
	if results := SearchSimpleIcons("zzzznotabrand", 10); len(results) != 0 {
		t.Errorf("got %d results for nonsense, want 0", len(results))
	}
}

// TestCatalogNamesAreLiteralText documents why the picker rows disable Pango
// markup.
//
// AdwPreferencesRow parses titles as markup by default, so a brand whose name
// contains an ampersand — "AT&T", "1&1", "Dungeons & Dragons" — fails to
// parse and renders with no title. The catalog is a fixed list, so this
// asserts the hazard still exists rather than hoping it went away: if these
// names ever vanish upstream the test fails and someone re-checks whether the
// escaping is still needed.
func TestCatalogNamesAreLiteralText(t *testing.T) {
	var withMarkupChars int
	simpleOnce.Do(loadSimpleIcons)
	for _, icon := range simpleIcons {
		if strings.ContainsAny(icon.Title, "&<>") {
			withMarkupChars++
		}
	}
	if withMarkupChars == 0 {
		t.Fatal("no brand names contain markup characters; the rows' SetUseMarkup(false) may no longer be needed")
	}
	if _, ok := LookupSimpleIcon("atandt"); !ok {
		t.Error("AT&T missing: it is one of the names that exposed this")
	}
}
