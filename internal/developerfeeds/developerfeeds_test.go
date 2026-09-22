package developerfeeds

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// vettedInventory pins the catalog this repository has actually vetted, by
// category. Every entry was verified live on the date recorded in
// docs/specs/developer-feeds.md; adding or removing a feed is a curation event
// that must be re-verified and reflected here, so a silently edited asset
// fails instead of shipping an unvetted subscription to users.
var vettedInventory = map[string]int{
	CategoryChangelog:  5,
	CategoryBlog:       7,
	CategoryNewsletter: 1,
	CategoryPodcast:    19,
}

// TestParseRejectsMalformedXML proves the well-formedness half of the offline
// validation: a truncated asset, an unbalanced tag, or a stray second root
// element is an error rather than a shorter catalog that parses clean.
func TestParseRejectsMalformedXML(t *testing.T) {
	const valid = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0"><head><title>t</title></head><body></body></opml>`

	cases := []struct {
		name    string
		fixture string
	}{
		{"unclosed outline", `<opml version="2.0"><body><outline text="a"></body></opml>`},
		{"mismatched close tag", `<opml version="2.0"><body><outline text="a"></outlines></body></opml>`},
		{"stray close tag", `<opml version="2.0"><body></outline></body></opml>`},
		{"unterminated attribute", `<opml version="2.0"><body><outline text="a></body></opml>`},
		{"truncated document", `<opml version="2.0"><body><outline text="a"/>`},
		{"root element is not opml", `<rss version="2.0"><channel/></rss>`},
		{"second root element", valid + "\n<opml version=\"2.0\"/>"},
		{"text after the root element", valid + "\ntrailing"},
		{"empty document", ``},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := Parse([]byte(testCase.fixture)); err == nil {
				t.Fatalf("Parse accepted malformed OPML: %s", testCase.fixture)
			}
		})
	}

	t.Run("the well-formed control parses", func(t *testing.T) {
		doc, err := Parse([]byte(valid))
		if err != nil {
			t.Fatalf("Parse rejected well-formed OPML: %v", err)
		}
		if doc.Version != "2.0" {
			t.Errorf("parsed version = %q, want %q", doc.Version, "2.0")
		}
	})
}

// TestValidateReportsEachDocumentDefect drives the validator with a document
// that passes, then mutates exactly one thing per case and requires the
// problem to name that field at that path. The expectations are literals, so a
// validator that stopped checking one of these rules fails here instead of
// passing vacuously.
func TestValidateReportsEachDocumentDefect(t *testing.T) {
	type want struct {
		path  string
		field string
		// detail is a substring the explanation must carry, empty when the
		// field and path are enough to identify the defect.
		detail string
	}

	cases := []struct {
		name   string
		mutate func(*Document)
		want   want
	}{
		{
			name:   "document version is not 2.0",
			mutate: func(d *Document) { d.Version = "1.0" },
			want:   want{field: "version"},
		},
		{
			name:   "head has no title",
			mutate: func(d *Document) { d.Head.Title = "  " },
			want:   want{field: "head/title"},
		},
		{
			name:   "body has no outlines",
			mutate: func(d *Document) { d.Body.Outlines = nil },
			want:   want{field: "body"},
		},
		{
			name: "a required category group is missing",
			mutate: func(d *Document) {
				d.Body.Outlines = slices.DeleteFunc(d.Body.Outlines, func(o Outline) bool {
					return o.Text == "Podcasts"
				})
			},
			want: want{field: "body", detail: `"Podcasts"`},
		},
		{
			name: "a top-level group carries a feed URL",
			mutate: func(d *Document) {
				d.Body.Outlines[0].XMLURL = "https://example.com/feed.xml"
			},
			want: want{path: "Changelogs", field: "xmlUrl"},
		},
		{
			name: "a category group has no feeds",
			mutate: func(d *Document) {
				d.Body.Outlines[2].Outlines = nil
			},
			want: want{path: "Podcasts", field: "outline"},
		},
		{
			name: "an outline has no text",
			mutate: func(d *Document) {
				d.Body.Outlines[0].Outlines = append(d.Body.Outlines[0].Outlines, Outline{
					Text: "", Title: "", Type: "rss", Category: CategoryChangelog,
					XMLURL: "https://example.com/press.atom",
				})
			},
			want: want{path: "Changelogs", field: "text"},
		},
		{
			name: "a feed has no title attribute",
			mutate: func(d *Document) {
				d.Body.Outlines[0].Outlines[0].Title = ""
			},
			want: want{path: "Changelogs/Bluefin OS Releases", field: "title"},
		},
		{
			name: "a feed declares a non-rss type",
			mutate: func(d *Document) {
				d.Body.Outlines[0].Outlines[0].Type = "atom"
			},
			want: want{path: "Changelogs/Bluefin OS Releases", field: "type"},
		},
		{
			name: "a feed has no category tag",
			mutate: func(d *Document) {
				d.Body.Outlines[0].Outlines[0].Category = ""
			},
			want: want{path: "Changelogs/Bluefin OS Releases", field: "category"},
		},
		{
			name: "a feed carries an unknown category tag",
			mutate: func(d *Document) {
				d.Body.Outlines[0].Outlines[0].Category = "kubernetes"
			},
			want: want{path: "Changelogs/Bluefin OS Releases", field: "category"},
		},
		{
			name: "a feed URL is not https",
			mutate: func(d *Document) {
				d.Body.Outlines[0].Outlines[0].XMLURL = "http://example.com/feed.xml"
			},
			want: want{path: "Changelogs/Bluefin OS Releases", field: "xmlUrl"},
		},
		{
			name: "a feed URL carries a tracking parameter",
			mutate: func(d *Document) {
				d.Body.Outlines[0].Outlines[0].XMLURL = "https://example.com/feed.xml?utm_source=bluefin"
			},
			want: want{path: "Changelogs/Bluefin OS Releases", field: "xmlUrl", detail: "utm_source"},
		},
		{
			name: "a feed URL repeats another entry",
			mutate: func(d *Document) {
				d.Body.Outlines[1].Outlines[0].XMLURL = d.Body.Outlines[0].Outlines[0].XMLURL
			},
			want: want{path: "Blogs & Newsletters/CNCF Blog", field: "xmlUrl"},
		},
		{
			name: "two sibling feeds share a title",
			mutate: func(d *Document) {
				d.Body.Outlines[1].Outlines[1].Title = d.Body.Outlines[1].Outlines[0].Title
			},
			want: want{path: "Blogs & Newsletters/CNCF Blog", field: "title"},
		},
		{
			name: "a feed outline carries children",
			mutate: func(d *Document) {
				d.Body.Outlines[0].Outlines[0].Outlines = []Outline{{Text: "nested"}}
			},
			want: want{path: "Changelogs/Bluefin OS Releases", field: "outline"},
		},
		{
			name: "an outline is neither a feed nor a group",
			mutate: func(d *Document) {
				d.Body.Outlines[1].Outlines = append(d.Body.Outlines[1].Outlines, Outline{Text: "Empty folder"})
			},
			want: want{path: "Blogs & Newsletters/Empty folder", field: "outline"},
		},
		{
			name: "a site URL is not https",
			mutate: func(d *Document) {
				d.Body.Outlines[0].Outlines[0].HTMLURL = "http://example.com/releases"
			},
			want: want{path: "Changelogs/Bluefin OS Releases", field: "htmlUrl"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doc := soundCatalog()
			if problems := Validate(doc); len(problems) != 0 {
				t.Fatalf("the unmutated control is not sound: %v", problems)
			}

			testCase.mutate(doc)

			problems := Validate(doc)
			if len(problems) == 0 {
				t.Fatal("Validate reported no problem for a mutated document")
			}
			if !slices.ContainsFunc(problems, func(p Problem) bool {
				return p.Path == testCase.want.path && p.Field == testCase.want.field &&
					strings.Contains(p.Detail, testCase.want.detail)
			}) {
				t.Fatalf("Validate reported %v, want a problem at path %q field %q containing %q",
					problems, testCase.want.path, testCase.want.field, testCase.want.detail)
			}
		})
	}
}

// TestValidateRejectsMalformedFeedURLs covers the URL shapes that are not
// https, not absolute, or not an endpoint at all.
func TestValidateRejectsMalformedFeedURLs(t *testing.T) {
	for _, raw := range []string{
		"/feed.xml",
		"example.com/feed.xml",
		"ftp://example.com/feed.xml",
		"https:///feed.xml",
		"https://user:secret@example.com/feed.xml",
		"https://example.com/feed.xml#latest",
		"https://example.com/feed.xml?fbclid=abc123",
	} {
		t.Run(strconv.Quote(raw), func(t *testing.T) {
			doc := soundCatalog()
			doc.Body.Outlines[0].Outlines[0].XMLURL = raw
			problems := Validate(doc)
			if !slices.ContainsFunc(problems, func(p Problem) bool {
				return p.Field == "xmlUrl" && p.Path == "Changelogs/Bluefin OS Releases"
			}) {
				t.Fatalf("Validate accepted feed URL %q: %v", raw, problems)
			}
		})
	}
}

// TestEmbeddedCatalogIsSound runs the whole validator over the asset the
// binary embeds, so the catalog CI ships is the one these rules were checked
// against.
func TestEmbeddedCatalogIsSound(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if problems := Validate(doc); len(problems) != 0 {
		t.Fatalf("the embedded catalog is not sound:\n  %s", joinProblems(problems))
	}
}

// TestEmbeddedCatalogMatchesTheVettedInventory pins the curated counts and the
// three category groups the epic requires. It reads the parsed document rather
// than trusting Validate, so a validator change cannot hide a catalog change.
func TestEmbeddedCatalogMatchesTheVettedInventory(t *testing.T) {
	feeds := embeddedFeeds(t)

	counts := make(map[string]int)
	for _, feed := range feeds {
		counts[feed.Category]++
	}
	if len(counts) != len(vettedInventory) {
		t.Errorf("catalog uses %d categories, want %d: %v", len(counts), len(vettedInventory), counts)
	}
	for category, want := range vettedInventory {
		if got := counts[category]; got != want {
			t.Errorf("catalog has %d %s feeds, want %d", got, category, want)
		}
	}

	total := 0
	for _, count := range vettedInventory {
		total += count
	}
	if len(feeds) != total {
		t.Errorf("catalog has %d feeds, want %d", len(feeds), total)
	}

	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var groups []string
	for _, outline := range doc.Body.Outlines {
		groups = append(groups, outline.Text)
	}
	wantGroups := []string{"Changelogs", "Blogs & Newsletters", "Podcasts"}
	if !slices.Equal(groups, wantGroups) {
		t.Errorf("catalog groups = %v, want %v", groups, wantGroups)
	}
}

// TestEmbeddedCatalogCarriesTheProjectChangelogs asserts the entries issue
// #236 names by URL, so a reordering or a well-meaning "cleanup" cannot drop
// the Bluefin and Homebrew changelogs that motivated the catalog.
func TestEmbeddedCatalogCarriesTheProjectChangelogs(t *testing.T) {
	want := map[string]string{
		"Bluefin OS Releases":  "https://github.com/projectbluefin/bluefin/releases.atom",
		"Bluefin LTS Releases": "https://github.com/projectbluefin/bluefin-lts/releases.atom",
		"Homebrew Releases":    "https://github.com/Homebrew/brew/releases.atom",
	}

	got := make(map[string]string)
	for _, feed := range embeddedFeeds(t) {
		got[feed.Title] = feed.XMLURL
	}
	for title, wantURL := range want {
		if got[title] != wantURL {
			t.Errorf("catalog feed %q has URL %q, want %q", title, got[title], wantURL)
		}
	}
}

// TestEmbeddedCatalogFeedURLsAreUnique checks uniqueness directly against the
// parsed asset, independently of the validator that also reports duplicates.
func TestEmbeddedCatalogFeedURLsAreUnique(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	seen := make(map[string]string)
	var outlineCount int
	var walk func(outlines []Outline, path string)
	walk = func(outlines []Outline, path string) {
		for _, outline := range outlines {
			if outline.XMLURL == "" {
				walk(outline.Outlines, path+"/"+outline.Text)
				continue
			}
			outlineCount++
			if first, duplicate := seen[outline.XMLURL]; duplicate {
				t.Errorf("feed URL %s appears under both %s and %s", outline.XMLURL, first, path)
			}
			seen[outline.XMLURL] = path
		}
	}
	walk(doc.Body.Outlines, "")

	if outlineCount != len(seen) {
		t.Errorf("counted %d feed outlines but %d unique URLs", outlineCount, len(seen))
	}
	if len(seen) == 0 {
		t.Fatal("the catalog contains no feed URLs at all")
	}
}

// TestEmbeddedCatalogFeedsCarryTheirGroupChain spot-checks the flattened view
// the Developer Mode staging path consumes, including a nested podcast group.
func TestEmbeddedCatalogFeedsCarryTheirGroupChain(t *testing.T) {
	feeds := embeddedFeeds(t)

	byTitle := make(map[string]Feed, len(feeds))
	for _, feed := range feeds {
		byTitle[feed.Title] = feed
	}

	kubeFM, ok := byTitle["KubeFM"]
	if !ok {
		t.Fatal("catalog has no KubeFM feed")
	}
	wantGroups := []string{"Podcasts", "Cloud Architecture, Infrastructure & Observability"}
	if !slices.Equal(kubeFM.Groups, wantGroups) {
		t.Errorf("KubeFM groups = %v, want %v", kubeFM.Groups, wantGroups)
	}
	if kubeFM.Category != "podcast" {
		t.Errorf("KubeFM category = %q, want %q", kubeFM.Category, "podcast")
	}
	if kubeFM.HTMLURL != "https://kube.fm/" {
		t.Errorf("KubeFM site = %q, want %q", kubeFM.HTMLURL, "https://kube.fm/")
	}

	changelog, ok := byTitle["Bluefin OS Releases"]
	if !ok {
		t.Fatal("catalog has no Bluefin OS Releases feed")
	}
	if !slices.Equal(changelog.Groups, []string{"Changelogs"}) {
		t.Errorf("Bluefin OS Releases groups = %v, want [Changelogs]", changelog.Groups)
	}

	for _, feed := range feeds {
		if len(feed.Groups) == 0 {
			t.Errorf("feed %q carries no group chain", feed.Title)
		}
		if strings.TrimSpace(feed.HTMLURL) == "" {
			continue
		}
		if !strings.HasPrefix(feed.HTMLURL, "https://") {
			t.Errorf("feed %q site %q is not https", feed.Title, feed.HTMLURL)
		}
	}
}

// TestCatalogVocabularyIsFrozen records the category tags and required group
// names as literals, so widening either set is a visible, reviewable change
// rather than a side effect of editing the catalog.
func TestCatalogVocabularyIsFrozen(t *testing.T) {
	if want := []string{"changelog", "blog", "newsletter", "podcast"}; !slices.Equal(categories, want) {
		t.Errorf("categories = %v, want %v", categories, want)
	}
	if want := []string{"Changelogs", "Blogs & Newsletters", "Podcasts"}; !slices.Equal(requiredGroups, want) {
		t.Errorf("requiredGroups = %v, want %v", requiredGroups, want)
	}
}

// TestAssetReturnsACopy keeps the embedded catalog immutable: a caller that
// stages the asset and rewrites bytes in place must not affect the next load.
func TestAssetReturnsACopy(t *testing.T) {
	first := Asset()
	if len(first) == 0 {
		t.Fatal("Asset returned no bytes")
	}
	original := first[0]
	first[0] = '!'

	if second := Asset(); second[0] != original {
		t.Fatalf("mutating the returned asset changed the embedded catalog: %q", second[0])
	}
}

// TestPackageStaysOffline is the structural half of the CI-safety requirement:
// the catalog is validated by tests that make no outbound request, and this
// package cannot make one — it imports no HTTP client, no dialer, and no
// command runner. A future edit that adds one fails here as well as in review.
func TestPackageStaysOffline(t *testing.T) {
	forbidden := []string{
		"net",
		"net/http",
		"net/smtp",
		"os/exec",
		"codeberg.org/puregotk/puregotk",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquote import %s: %v", name, spec.Path.Value, err)
			}
			if slices.Contains(forbidden, path) {
				t.Errorf("%s imports %q; the catalog validator must stay offline", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no production Go files were checked")
	}
}

// soundCatalog is the minimal document every defect case mutates: one feed per
// required category, each with the attributes Validate demands. Its values are
// literals so the expectations in the defect table do not move with the
// production constants.
func soundCatalog() *Document {
	return &Document{
		Version: "2.0",
		Head:    Head{Title: "Test catalog"},
		Body: Body{Outlines: []Outline{
			{
				Text: "Changelogs",
				Outlines: []Outline{{
					Text: "Bluefin OS Releases", Title: "Bluefin OS Releases", Type: "rss",
					Category: "changelog",
					XMLURL:   "https://example.com/bluefin/releases.atom",
					HTMLURL:  "https://example.com/bluefin/releases",
				}},
			},
			{
				Text: "Blogs & Newsletters",
				Outlines: []Outline{
					{
						Text: "CNCF Blog", Title: "CNCF Blog", Type: "rss", Category: "blog",
						XMLURL: "https://example.com/cncf/feed", HTMLURL: "https://example.com/cncf",
					},
					{
						Text: "DevOps'ish", Title: "DevOps'ish", Type: "rss", Category: "newsletter",
						XMLURL: "https://example.com/devopsish/feed", HTMLURL: "https://example.com/devopsish",
					},
				},
			},
			{
				Text: "Podcasts",
				Outlines: []Outline{{
					Text: "KubeFM", Title: "KubeFM", Type: "rss", Category: "podcast",
					XMLURL: "https://example.com/kubefm/feed", HTMLURL: "https://example.com/kubefm",
				}},
			},
		}},
	}
}

// embeddedFeeds loads the asset and returns its flattened feeds, failing the
// test rather than returning a partial catalog.
func embeddedFeeds(t *testing.T) []Feed {
	t.Helper()

	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return Feeds(doc)
}

// joinProblems renders problems one per line for failure messages.
func joinProblems(problems []Problem) string {
	rendered := make([]string, 0, len(problems))
	for _, problem := range problems {
		rendered = append(rendered, problem.String())
	}
	return strings.Join(rendered, "\n  ")
}
