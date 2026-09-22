// Package developerfeeds owns the curated developer feed catalog: the
// community-vetted OPML asset that Developer Mode stages for the Pulp feed
// reader, and the offline validator that reports what is wrong with it.
//
// The catalog itself (developer-feeds.opml) is embedded in the binary, so it
// ships with the application and is read from no user-writable path and no
// network. Everything this package does is offline by construction
// — Parse decodes bytes the caller supplies, Load decodes the embedded asset,
// and Validate walks the decoded document. That is deliberate: the epic that
// asked for this catalog (projectbluefin/chairlift#235) requires CI to check
// OPML well-formedness, tag balance, and URL uniqueness without an outbound
// request, and a package that cannot open a socket cannot break that rule.
// Verifying that the endpoints are still alive is a manual, documented recipe
// (see docs/specs/developer-feeds.md, which is that contract and its
// re-verification procedure); it is never a unit test.
//
// The package is pure Go: no puregotk, no CGO, no GTK libraries at
// initialization, so its tests run under the ordinary `go test ./internal/...`
// gate on a headless runner.
package developerfeeds

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"
)

//go:embed developer-feeds.opml
var catalog []byte

// OPMLVersion is the OPML specification version the catalog declares. Version
// 2.0 is what every modern feed reader — including Pulp — parses; the
// validator rejects any other value rather than guessing.
const OPMLVersion = "2.0"

// The category vocabulary. Each leaf outline in the catalog carries exactly
// one of these as its `category` attribute; grouping and nesting are expressed
// by the OPML hierarchy, so a tag never has to encode a path.
const (
	// CategoryChangelog marks a project's release or changelog stream.
	CategoryChangelog = "changelog"
	// CategoryBlog marks a long-form publication.
	CategoryBlog = "blog"
	// CategoryNewsletter marks a periodic email-style publication.
	CategoryNewsletter = "newsletter"
	// CategoryPodcast marks an audio or video show.
	CategoryPodcast = "podcast"
)

// categories is the frozen vocabulary Validate accepts, in the order the
// catalog's own documentation lists it. Adding a category is a catalog
// contract change, not a data change: a reader that groups by tag has to learn
// the new one first.
var categories = []string{CategoryChangelog, CategoryBlog, CategoryNewsletter, CategoryPodcast}

// requiredGroups are the top-level outlines the catalog must offer, by their
// user-visible text. They are the three sections projectbluefin/chairlift#236
// names in its acceptance criteria — Changelogs, Blogs/Newsletters, and
// Podcasts — so a catalog that quietly drops one is incomplete rather than
// merely shorter.
var requiredGroups = []string{"Changelogs", "Blogs & Newsletters", "Podcasts"}

// trackingParameters are query-parameter names that identify a campaign rather
// than a resource. A feed URL carrying one is not the canonical endpoint the
// publisher maintains: the campaign expires, the parameter is copied from a
// newsletter link, and the subscription eventually points somewhere the
// publisher never intended to support. Validate rejects them anywhere in a
// feed or site URL.
var trackingParameters = map[string]bool{
	"fbclid":  true,
	"gclid":   true,
	"igshid":  true,
	"mc_cid":  true,
	"mc_eid":  true,
	"ref_src": true,
}

// Outline is one OPML <outline> element. A group outline carries children and
// no feed URL; a leaf outline carries a feed URL and no children. Validate
// holds both halves of that rule.
type Outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	Category string    `xml:"category,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Outlines []Outline `xml:"outline"`
}

// Head is the OPML <head> element.
type Head struct {
	Title     string `xml:"title,omitempty"`
	OwnerName string `xml:"ownerName,omitempty"`
	Created   string `xml:"dateCreated,omitempty"`
}

// Body is the OPML <body> element.
type Body struct {
	Outlines []Outline `xml:"outline"`
}

// Document is a parsed OPML document.
type Document struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    Head     `xml:"head"`
	Body    Body     `xml:"body"`
}

// Feed is one subscribable entry of the catalog, flattened out of the OPML
// hierarchy with the group names that led to it.
type Feed struct {
	// Title is the feed's display name, the outline's `title` attribute.
	Title string
	// Category is the leaf's clean category tag, one of the Category*
	// constants.
	Category string
	// Groups is the chain of enclosing group texts, outermost first.
	Groups []string
	// XMLURL is the feed endpoint a reader subscribes to.
	XMLURL string
	// HTMLURL is the human-readable site for the feed, empty when the
	// publisher offers no stable page for it.
	HTMLURL string
}

// Problem is one catalog defect: where it is, which field carries it, and what
// is wrong. Validate returns every problem it finds rather than the first, so
// a contributor fixing the catalog sees the whole list in one run.
type Problem struct {
	// Path is the outline's group chain joined with "/", empty for a
	// document-level problem.
	Path string
	// Field names the offending attribute or document part, e.g. "xmlUrl".
	Field string
	// Detail explains what a reader would get wrong.
	Detail string
}

// String renders the problem as a single line, path first.
func (p Problem) String() string {
	if p.Path == "" {
		return p.Field + ": " + p.Detail
	}
	return p.Path + ": " + p.Field + ": " + p.Detail
}

// Asset returns a copy of the embedded OPML catalog. Callers that stage the
// catalog on disk get their own bytes; mutating the result cannot corrupt the
// copy the next caller receives.
func Asset() []byte {
	return bytes.Clone(catalog)
}

// Parse decodes an OPML document. It is strict on purpose: the decoder rejects
// unbalanced or mismatched tags, and Parse additionally rejects a root element
// that is not <opml> and any content after the closing tag, so a truncated or
// concatenated asset fails here instead of silently parsing as a shorter
// catalog.
func Parse(data []byte) (*Document, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true

	var doc Document
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("developer feeds catalog: parsing OPML: %w", err)
	}
	if doc.XMLName.Local != "opml" {
		return nil, fmt.Errorf("developer feeds catalog: root element is <%s>, want <opml>", doc.XMLName.Local)
	}
	if err := requireEndOfDocument(decoder); err != nil {
		return nil, err
	}
	return &doc, nil
}

// requireEndOfDocument consumes the remainder of the stream and fails if it
// holds anything but whitespace and comments.
func requireEndOfDocument(decoder *xml.Decoder) error {
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("developer feeds catalog: reading past the root element: %w", err)
		}
		switch typed := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(typed)) != "" {
				return fmt.Errorf("developer feeds catalog: unexpected text after the closing </opml>")
			}
		case xml.StartElement:
			return fmt.Errorf("developer feeds catalog: unexpected element <%s> after the closing </opml>", typed.Name.Local)
		}
	}
}

// Load parses the embedded catalog asset.
func Load() (*Document, error) {
	return Parse(Asset())
}

// Validate reports every defect that would make the catalog unusable or
// misleading for a reader: a malformed document skeleton, an outline that is
// neither a proper group nor a proper feed, a missing or unknown category tag,
// a feed URL that is not the publisher's canonical HTTPS endpoint, and any
// duplicate feed URL or sibling title. It returns nil when the document is
// sound; the result is always offline — no URL is resolved.
func Validate(doc *Document) []Problem {
	if doc == nil {
		return []Problem{{Field: "document", Detail: "catalog is nil"}}
	}

	var problems []Problem
	if doc.Version != OPMLVersion {
		problems = append(problems, Problem{
			Field:  "version",
			Detail: fmt.Sprintf("document declares OPML version %q, want %q", doc.Version, OPMLVersion),
		})
	}
	if strings.TrimSpace(doc.Head.Title) == "" {
		problems = append(problems, Problem{
			Field:  "head/title",
			Detail: "document has no title, so a reader cannot label the imported list",
		})
	}
	if len(doc.Body.Outlines) == 0 {
		problems = append(problems, Problem{
			Field:  "body",
			Detail: "document contains no outlines",
		})
	}

	seenURLs := make(map[string]string)
	present := make(map[string]bool)
	for _, group := range doc.Body.Outlines {
		name := outlineName(group)
		present[name] = true
		if group.XMLURL != "" {
			problems = append(problems, Problem{
				Path:   name,
				Field:  "xmlUrl",
				Detail: "a top-level outline is a category group and must not carry a feed URL",
			})
		}
		if len(group.Outlines) == 0 {
			problems = append(problems, Problem{
				Path:   name,
				Field:  "outline",
				Detail: "category group contains no feeds",
			})
		}
		problems = append(problems, walkOutlines(group.Outlines, name, seenURLs)...)
	}

	for _, required := range requiredGroups {
		if !present[required] {
			problems = append(problems, Problem{
				Field:  "body",
				Detail: fmt.Sprintf("catalog has no %q category group", required),
			})
		}
	}
	return problems
}

// walkOutlines validates every outline under a group, prefixing each problem
// with the path that leads to it. seenURLs accumulates feed URLs across the
// whole document so a URL repeated in two different categories is caught.
func walkOutlines(outlines []Outline, path string, seenURLs map[string]string) []Problem {
	var problems []Problem
	seenTitles := make(map[string]bool)

	for _, outline := range outlines {
		name := outlineName(outline)
		childPath := path
		if name != "" {
			childPath = path + "/" + name
		}

		if name == "" {
			problems = append(problems, Problem{
				Path:   path,
				Field:  "text",
				Detail: "outline has no text, so a reader shows it as a blank entry",
			})
		}
		if seenTitles[name] && name != "" {
			problems = append(problems, Problem{
				Path:   childPath,
				Field:  "title",
				Detail: "two outlines in the same group share this title",
			})
		}
		seenTitles[name] = true

		if outline.XMLURL == "" {
			if len(outline.Outlines) == 0 {
				problems = append(problems, Problem{
					Path:   childPath,
					Field:  "outline",
					Detail: "outline is neither a feed (no xmlUrl) nor a group (no children)",
				})
			}
			problems = append(problems, walkOutlines(outline.Outlines, childPath, seenURLs)...)
			continue
		}

		problems = append(problems, validateFeed(outline, childPath)...)
		if len(outline.Outlines) > 0 {
			problems = append(problems, Problem{
				Path:   childPath,
				Field:  "outline",
				Detail: "a feed outline must not contain child outlines",
			})
		}
		if previous, duplicate := seenURLs[outline.XMLURL]; duplicate {
			problems = append(problems, Problem{
				Path:   childPath,
				Field:  "xmlUrl",
				Detail: "feed URL is already listed under " + previous,
			})
		} else {
			seenURLs[outline.XMLURL] = path
		}
	}
	return problems
}

// validateFeed checks the attributes a leaf outline must carry.
func validateFeed(outline Outline, path string) []Problem {
	var problems []Problem

	if strings.TrimSpace(outline.Title) == "" {
		problems = append(problems, Problem{
			Path:   path,
			Field:  "title",
			Detail: "feed has no title, so a reader shows its URL instead of a name",
		})
	}
	if outline.Type != "rss" {
		problems = append(problems, Problem{
			Path:   path,
			Field:  "type",
			Detail: fmt.Sprintf("feed type is %q, want %q", outline.Type, "rss"),
		})
	}
	if !isKnownCategory(outline.Category) {
		problems = append(problems, Problem{
			Path:   path,
			Field:  "category",
			Detail: fmt.Sprintf("category %q is not one of %s", outline.Category, strings.Join(categories, ", ")),
		})
	}
	if detail := canonicalURLDetail(outline.XMLURL); detail != "" {
		problems = append(problems, Problem{Path: path, Field: "xmlUrl", Detail: detail})
	}
	if outline.HTMLURL != "" {
		if detail := canonicalURLDetail(outline.HTMLURL); detail != "" {
			problems = append(problems, Problem{Path: path, Field: "htmlUrl", Detail: detail})
		}
	}
	return problems
}

// canonicalURLDetail reports why raw is not a clean canonical endpoint, or the
// empty string when it is one. Only URLs that would reach a real host are
// considered: the check is entirely lexical, so it never resolves a name.
func canonicalURLDetail(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "URL is empty"
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Sprintf("URL %q does not parse: %v", raw, err)
	}
	if parsed.Scheme != "https" {
		return fmt.Sprintf("URL %q must use https, not %q", raw, parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Sprintf("URL %q has no host", raw)
	}
	if parsed.User != nil {
		return fmt.Sprintf("URL %q carries credentials", raw)
	}
	if parsed.Fragment != "" {
		return fmt.Sprintf("URL %q carries a fragment", raw)
	}
	for parameter := range parsed.Query() {
		if strings.HasPrefix(strings.ToLower(parameter), "utm_") || trackingParameters[strings.ToLower(parameter)] {
			return fmt.Sprintf("URL %q carries the tracking parameter %q; use the publisher's canonical endpoint", raw, parameter)
		}
	}
	return ""
}

// isKnownCategory reports whether tag is in the frozen vocabulary.
func isKnownCategory(tag string) bool {
	for _, category := range categories {
		if tag == category {
			return true
		}
	}
	return false
}

// outlineName is the label a reader displays for an outline: OPML gives the
// required `text` attribute and an optional `title` that overrides it.
func outlineName(outline Outline) string {
	if strings.TrimSpace(outline.Title) != "" {
		return outline.Title
	}
	return outline.Text
}

// Feeds flattens the catalog into subscribable entries in document order,
// each carrying the group chain that led to it. It reads the document as
// written; run Validate first when the caller needs the structure to be sound.
func Feeds(doc *Document) []Feed {
	if doc == nil {
		return nil
	}
	var feeds []Feed
	for _, group := range doc.Body.Outlines {
		feeds = append(feeds, collectFeeds(group.Outlines, []string{outlineName(group)})...)
	}
	return feeds
}

// collectFeeds walks a group's outlines, appending leaves and descending into
// nested groups.
func collectFeeds(outlines []Outline, groups []string) []Feed {
	var feeds []Feed
	for _, outline := range outlines {
		if outline.XMLURL == "" {
			feeds = append(feeds, collectFeeds(outline.Outlines, append(groups, outlineName(outline)))...)
			continue
		}
		feeds = append(feeds, Feed{
			Title:    outlineName(outline),
			Category: outline.Category,
			Groups:   append([]string(nil), groups...),
			XMLURL:   outline.XMLURL,
			HTMLURL:  outline.HTMLURL,
		})
	}
	return feeds
}
