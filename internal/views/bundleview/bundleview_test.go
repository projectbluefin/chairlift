package bundleview

import (
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/homebrew"
)

func TestPresentEnumeratesLoadOutcomes(t *testing.T) {
	empty := Present(0, "")
	// The discovery warning names the directories that were searched, so it
	// stays in the log and never reaches a row.
	failed := Present(0, "read bundle directory \"/usr/share/ublue-os/homebrew\": permission denied")
	one := Present(1, "")
	partial := Present(2, "read bundle directory \"/opt/extra\": permission denied")

	for name, got := range map[string]Presentation{"empty": empty, "failed": failed, "one": one} {
		if got.Description != GroupDescription {
			t.Errorf("%s: description = %q, want the group description", name, got.Description)
		}
	}
	if empty.PlaceholderTitle == "" || empty.PlaceholderSubtitle == "" {
		t.Errorf("empty: want a placeholder row, got %+v", empty)
	}
	if failed.PlaceholderTitle == "" || failed.PlaceholderSubtitle == "" {
		t.Errorf("failed: want a placeholder row, got %+v", failed)
	}
	if failed.PlaceholderTitle == empty.PlaceholderTitle {
		t.Errorf("a failed search must not read like a computer with no collections: %q", failed.PlaceholderTitle)
	}
	if one.PlaceholderTitle != "" || one.PlaceholderSubtitle != "" || partial.PlaceholderTitle != "" {
		t.Errorf("rows exist, so no placeholder: one=%+v partial=%+v", one, partial)
	}
	if !strings.HasPrefix(partial.Description, GroupDescription) || partial.Description == GroupDescription {
		t.Errorf("partial: description = %q, want the group description plus a note", partial.Description)
	}
	for _, got := range []Presentation{empty, failed, one, partial} {
		for _, text := range []string{got.Description, got.PlaceholderTitle, got.PlaceholderSubtitle} {
			for _, jargon := range []string{"/", "Homebrew", "system", "Could not"} {
				if strings.Contains(text, jargon) {
					t.Errorf("collection text %q contains %q", text, jargon)
				}
			}
		}
	}
}

func TestDescribeNamesEveryCollectionForAPerson(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		comment      string
		itemCount    int
		wantTitle    string
		wantSubtitle string
	}{
		{
			name:         "curated collection Bluefin CLI",
			id:           "cli",
			itemCount:    16,
			wantTitle:    "Bluefin CLI",
			wantSubtitle: "A modern terminal environment with everyday utilities, Starship prompt, and shell bling. Includes 16 apps and tools.",
		},
		{
			name:         "curated collection Extra Wallpapers",
			id:           "artwork",
			itemCount:    4,
			wantTitle:    "Extra Wallpapers",
			wantSubtitle: "More dinosaur artwork, and wallpaper tools. Includes 4 apps and tools.",
		},
		{
			name:         "curated collection Fonts",
			id:           "fonts",
			itemCount:    21,
			wantTitle:    "Fonts",
			wantSubtitle: "A broad selection of everyday, design, office, and accessibility fonts. Includes 21 apps and tools.",
		},
		{
			name:         "curated collection Full Desktop Suite",
			id:           "full-desktop",
			comment:      "full-desktop.Brewfile",
			itemCount:    58,
			wantTitle:    "Full Desktop Suite",
			wantSubtitle: "A complete set of desktop apps for everyday productivity. Includes 58 apps and tools.",
		},
		{
			name:         "unknown collection with a human comment",
			id:           "site-extras",
			comment:      "Everything the design team needs on day one",
			itemCount:    4,
			wantTitle:    "Site extras",
			wantSubtitle: "Everything the design team needs on day one. Includes 4 apps and tools.",
		},
		{
			name:         "unknown collection whose comment points at a location",
			id:           "vendor-pack",
			comment:      "Generated from /usr/share/vendor/list",
			itemCount:    2,
			wantTitle:    "Vendor pack",
			wantSubtitle: "A set of apps and tools put together for this computer. Includes 2 apps and tools.",
		},
		{
			name:         "unknown collection keeps interior capitalization",
			id:           "k9s_addons",
			wantTitle:    "K9s addons",
			wantSubtitle: "A set of apps and tools put together for this computer.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Describe(tt.id, tt.comment, tt.itemCount)
			if got.Title != tt.wantTitle {
				t.Errorf("Describe(%q, %q, %d) title = %q, want %q", tt.id, tt.comment, tt.itemCount, got.Title, tt.wantTitle)
			}
			if got.Subtitle != tt.wantSubtitle {
				t.Errorf("Describe(%q, %q, %d) subtitle = %q, want %q", tt.id, tt.comment, tt.itemCount, got.Subtitle, tt.wantSubtitle)
			}
		})
	}
}

// Dakota collections are curated, a curated super-bundle member, or explicitly retired.
func TestDakotaCollectionsAreCuratedOrRetired(t *testing.T) {
	for _, id := range []string{
		"ai-tools", "artwork", "cli", "cncf", "dakota-dev-flatpaks", "dakota-fonts",
		"experimental-ide", "fonts", "fonts-dev", "full-desktop", "ide", "k8s-tools",
		"nsl", "swift", "system-dx-flatpaks", "system-flatpaks", "video-wallpaper",
		"wallpaper-slideshow",
	} {
		_, inCatalog := catalog[id]
		_, inRetired := retired[id]
		isMember := false
		for parent := range catalog {
			for _, m := range Members(parent) {
				if m == id {
					isMember = true
				}
			}
		}
		if !inCatalog && !inRetired && !isMember {
			t.Errorf("collection %q is neither in catalog, a member, nor retired", id)
		}
		if isMember && (inCatalog || inRetired) {
			t.Errorf("member collection %q must not also be in catalog or retired", id)
		}
		if inCatalog && inRetired {
			t.Errorf("collection %q is in both catalog and retired", id)
		}
	}
}

func TestCuratedCollectionsAreVisibleAndOrdered(t *testing.T) {
	curatedIDs := []string{"artwork", "cli", "fonts", "fonts-dev", "ai-tools", "full-desktop"}
	for _, id := range curatedIDs {
		if !Curated(id) {
			t.Errorf("Curated(%q) = false, want true", id)
		}
		if !Visible(id, nil) {
			t.Errorf("Visible(%q) = false, want true", id)
		}
	}
	for i, id := range curatedIDs {
		if got := Order(id); got != i+1 {
			t.Errorf("Order(%q) = %d, want %d", id, got, i+1)
		}
	}
	if Order("custom-admin-pack") != 100 {
		t.Errorf("Order(custom-admin-pack) = %d, want 100", Order("custom-admin-pack"))
	}
}

func TestRetiredCollectionsAreNotVisible(t *testing.T) {
	for id := range retired {
		if Visible(id, nil) {
			t.Errorf("Visible(%q) = true, want false for retired collection", id)
		}
		if Curated(id) {
			t.Errorf("Curated(%q) = true, want false for retired collection", id)
		}
	}
	// Unknown user-added collections remain visible.
	if !Visible("team-tools", nil) {
		t.Errorf("Visible(team-tools) = false, want true for custom user collection")
	}
}

// TestNoCollectionRowLeaksToolingIdentity holds the rule the group exists to
// satisfy: a person reading a row never sees a file name, a location, or a
// packaging term.
func TestNoCollectionRowLeaksToolingIdentity(t *testing.T) {
	banned := []string{
		"brewfile", "homebrew", "flatpak", "flathub", "ujust", "bootc", "quadlet",
		"cask", "formula", "tap ", "/", "\\", "bundles_paths",
	}

	rows := make([]string, 0, len(catalog)*2)
	for id := range catalog {
		// The identifier and a jargon comment are the two inputs that
		// could carry tooling text into a row.
		for _, comment := range []string{"", id + ".Brewfile", "Installed by ujust " + id} {
			collection := Describe(id, comment, 7)
			rows = append(rows, collection.Title, collection.Subtitle)
		}
	}

	presentation := Present(0, "read bundle directory \"/usr/share/ublue-os/homebrew\": permission denied")
	rows = append(rows, GroupDescription, presentation.PlaceholderTitle, presentation.PlaceholderSubtitle)

	for _, text := range rows {
		lowered := strings.ToLower(text)
		for _, marker := range banned {
			if strings.Contains(lowered, marker) {
				t.Errorf("collection text %q contains %q", text, marker)
			}
		}
	}
}

// Super-bundle siblings come from the catalog, never include the row
// itself, and resolve only against collections actually discovered.
func TestMemberBundlesResolvesDiscoveredSiblings(t *testing.T) {
	discovered := map[string]homebrew.Bundle{
		"artwork":             {Name: "artwork", Path: "/b/artwork.Brewfile", ItemCount: 2},
		"wallpaper-slideshow": {Name: "wallpaper-slideshow", Path: "/b/wallpaper-slideshow.Brewfile", ItemCount: 1},
		"fonts":               {Name: "fonts", Path: "/b/fonts.Brewfile", ItemCount: 5},
	}

	got := MemberBundles("artwork", discovered)
	if len(got) != 1 || got[0].Path != "/b/wallpaper-slideshow.Brewfile" {
		t.Errorf("MemberBundles(artwork) = %v, want only wallpaper-slideshow", got)
	}
	if got := MemberBundles("fonts", discovered); len(got) != 0 {
		t.Errorf("MemberBundles(fonts) = %v, want none when dakota-fonts was not discovered", got)
	}
	if got := MemberBundles("cli", discovered); len(got) != 0 {
		t.Errorf("MemberBundles(cli) = %v, want none", got)
	}
	if got := MemberBundles("team-tools", discovered); len(got) != 0 {
		t.Errorf("MemberBundles(team-tools) = %v, want none for a custom collection", got)
	}
}

// A super-bundle sibling is folded into its parent's row when the parent was
// discovered, and keeps its own row when it was not, so it stays installable.
func TestMembersFoldIntoDiscoveredParentOnly(t *testing.T) {
	for id := range catalog {
		for _, member := range Members(id) {
			if member == id {
				t.Errorf("Members(%q) includes itself", id)
			}
			withParent := map[string]homebrew.Bundle{
				id:     {Name: id},
				member: {Name: member},
			}
			if Visible(member, withParent) {
				t.Errorf("Visible(%q) = true with parent %q discovered, want folded into parent row", member, id)
			}
			alone := map[string]homebrew.Bundle{member: {Name: member}}
			if !Visible(member, alone) {
				t.Errorf("Visible(%q) = false without parent %q, want its own row", member, id)
			}
		}
	}
}

func TestPresentContentsBuildsRowsWithSourceLabels(t *testing.T) {
	items := []homebrew.BundleItem{
		{Name: "jq", Kind: homebrew.BundleItemBrew},
		{Name: "ripgrep", Kind: homebrew.BundleItemBrew},
		{Name: "visual-studio-code", Kind: homebrew.BundleItemCask},
		{Name: "org.gnome.TextEditor", Kind: homebrew.BundleItemFlatpak},
		{Name: "Xcode", Kind: homebrew.BundleItemMas},
		{Name: "golang.go", Kind: homebrew.BundleItemVSCode},
	}

	got := PresentContents(items, len(items))

	if !got.HasRows() {
		t.Fatalf("PresentContents() = %+v, want rows", got)
	}
	if got.EmptyTitle != "" || got.EmptySubtitle != "" {
		t.Errorf("presentContents empty placeholders should be empty when rows exist: %+v", got)
	}
	if got.OverflowNotice != "" {
		t.Errorf("OverflowNotice = %q, want empty when everything fits", got.OverflowNotice)
	}
	wantRows := []ContentsRow{
		{Item: items[0], Name: "jq", Label: "from Homebrew", AccessibleName: "jq", CheckAccessibleName: "Select jq", Selectable: true},
		{Item: items[1], Name: "ripgrep", Label: "from Homebrew", AccessibleName: "ripgrep", CheckAccessibleName: "Select ripgrep", Selectable: true},
		{Item: items[2], Name: "visual-studio-code", Label: "from Homebrew", AccessibleName: "visual-studio-code", CheckAccessibleName: "Select visual-studio-code", Selectable: true},
		{Item: items[3], Name: "org.gnome.TextEditor", Label: "from Flathub", AccessibleName: "org.gnome.TextEditor", CheckAccessibleName: "Select org.gnome.TextEditor", Selectable: true},
		{Item: items[4], Name: "Xcode", Label: "from the App Store", AccessibleName: "Xcode", CheckAccessibleName: "Select Xcode", Selectable: false},
		{Item: items[5], Name: "golang.go", Label: "from the VS Code Marketplace", AccessibleName: "golang.go", CheckAccessibleName: "Select golang.go", Selectable: false},
	}
	if !reflect.DeepEqual(got.Rows, wantRows) {
		t.Fatalf("PresentContents().Rows = %#v, want %#v", got.Rows, wantRows)
	}
}

func TestPresentContentsReportsOverflowWhenCapped(t *testing.T) {
	items := []homebrew.BundleItem{
		{Name: "alpha", Kind: homebrew.BundleItemBrew},
		{Name: "beta", Kind: homebrew.BundleItemBrew},
	}

	got := PresentContents(items, 5)

	if !got.HasRows() {
		t.Fatalf("PresentContents() = %+v, want rows", got)
	}
	if got.OverflowNotice != "Showing 2 of 5 entries." {
		t.Errorf("OverflowNotice = %q, want %q", got.OverflowNotice, "Showing 2 of 5 entries.")
	}
}

func TestPresentContentsReportsEmptyForUnrecognisedBrewfile(t *testing.T) {
	got := PresentContents(nil, 0)
	if got.HasRows() {
		t.Fatalf("PresentContents() = %+v, want no rows", got)
	}
	if got.EmptyTitle != ContentsHiddenTitle {
		t.Errorf("EmptyTitle = %q, want %q", got.EmptyTitle, ContentsHiddenTitle)
	}
	if got.EmptySubtitle == "" {
		t.Errorf("EmptySubtitle = empty, want an explanation")
	}
}

func TestPresentContentsReportsOverflowWhenItemsAreCropped(t *testing.T) {
	// A Brewfile that the parser reached but yielded nothing of is not the
	// same as a Brewfile that was bigger than the cap: the caller passes a
	// total that exceeds the items it can show, so the row renders the
	// overflow placeholder rather than the unrecognised-entries one.
	got := PresentContents(nil, 12)
	if got.HasRows() {
		t.Fatalf("PresentContents() = %+v, want no rows", got)
	}
	if got.EmptySubtitle != ContentsHiddenSubtitle {
		t.Errorf("EmptySubtitle = %q, want %q", got.EmptySubtitle, ContentsHiddenSubtitle)
	}
}
