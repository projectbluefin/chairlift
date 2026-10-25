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

// Dakota collections are curated, a curated super-bundle member, or explicitly
// retired. Livery owns artwork; wallpaper-slideshow keeps a standalone Apps row
// until its own slideshow controls exist.
func TestDakotaCollectionsAreCuratedOrRetired(t *testing.T) {
	for _, id := range []string{
		"ai-tools", "cli", "cncf", "dakota-dev-flatpaks", "dakota-fonts",
		"experimental-ide", "fonts", "fonts-dev", "full-desktop", "ide", "k8s-tools",
		"nsl", "swift", "system-dx-flatpaks", "system-flatpaks", "video-wallpaper",
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

func TestCuratedCollectionsOrderAndDefaults(t *testing.T) {
	curatedIDs := []string{"cli", "fonts", "fonts-dev", "ai-tools", "full-desktop"}
	for _, id := range curatedIDs {
		if !Curated(id) {
			t.Errorf("Curated(%q) = false, want true", id)
		}
		if !Visible(id, nil) {
			t.Errorf("Visible(%q) = false, want true", id)
		}
	}
	if DefaultSelected("full-desktop") {
		t.Errorf("DefaultSelected(full-desktop) = true, want false")
	}
	for _, id := range []string{"cli", "fonts", "fonts-dev", "ai-tools"} {
		if !DefaultSelected(id) {
			t.Errorf("DefaultSelected(%q) = false, want true", id)
		}
	}
	if !DefaultSelected("custom-admin-pack") {
		t.Errorf("DefaultSelected(custom-admin-pack) = false, want true")
	}
}

func TestWallpaperCollectionsLeaveTheAppsCatalog(t *testing.T) {
	if !WallpaperCollection("artwork") {
		t.Fatal("WallpaperCollection(artwork) = false, want Livery to own it")
	}
	if _, ok := catalog["artwork"]; ok {
		t.Error("artwork is in the Apps catalog, want it owned by Livery only")
	}
	if !Visible("wallpaper-slideshow", nil) {
		t.Error("Visible(wallpaper-slideshow) = false, want a standalone Apps row")
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

func TestObservedInstalledMapsEveryBundleStatus(t *testing.T) {
	for _, tc := range []struct {
		status        homebrew.BundleStatus
		wantInstalled bool
		wantKnown     bool
	}{
		{homebrew.BundleInstalled, true, true},
		{homebrew.BundleUpdateAvailable, true, true},
		{homebrew.BundleNotInstalled, false, true},
		{homebrew.BundleIndeterminate, false, false},
		{"", false, false},
	} {
		installed, known := ObservedInstalled(tc.status)
		if installed != tc.wantInstalled || known != tc.wantKnown {
			t.Errorf("ObservedInstalled(%q) = (%v, %v), want (%v, %v)", tc.status, installed, known, tc.wantInstalled, tc.wantKnown)
		}
	}
}

// Super-bundle siblings come from the catalog, never include the row
// itself, and resolve only against collections actually discovered.
func TestMemberBundlesResolvesDiscoveredSiblings(t *testing.T) {
	discovered := map[string]homebrew.Bundle{
		"fonts":        {Name: "fonts", Path: "/b/fonts.Brewfile", ItemCount: 5},
		"dakota-fonts": {Name: "dakota-fonts", Path: "/b/dakota-fonts.Brewfile", ItemCount: 1},
	}

	got := MemberBundles("fonts", discovered)
	if len(got) != 1 || got[0].Path != "/b/dakota-fonts.Brewfile" {
		t.Errorf("MemberBundles(fonts) = %v, want only dakota-fonts", got)
	}
	delete(discovered, "dakota-fonts")
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

// The row is installed only when every member is observed installed; one
// member observed missing makes it not installed; an undecided member with
// no observed-missing sibling changes nothing.
func TestAggregateInstalled(t *testing.T) {
	const (
		in  = homebrew.BundleInstalled
		up  = homebrew.BundleUpdateAvailable
		out = homebrew.BundleNotInstalled
		ind = homebrew.BundleIndeterminate
	)
	for _, tc := range []struct {
		name          string
		statuses      []homebrew.BundleStatus
		wantInstalled bool
		wantKnown     bool
	}{
		{"no members", nil, false, false},
		{"single installed", []homebrew.BundleStatus{in}, true, true},
		{"all installed or upgradable", []homebrew.BundleStatus{in, up}, true, true},
		{"one missing", []homebrew.BundleStatus{in, out}, false, true},
		{"installed then undecided", []homebrew.BundleStatus{in, ind}, false, false},
		{"undecided then installed", []homebrew.BundleStatus{ind, in}, false, false},
		{"undecided then missing", []homebrew.BundleStatus{ind, out}, false, true},
		{"all undecided", []homebrew.BundleStatus{ind, ind}, false, false},
	} {
		installed, known := AggregateInstalled(tc.statuses)
		if installed != tc.wantInstalled || known != tc.wantKnown {
			t.Errorf("%s: AggregateInstalled(%v) = (%v, %v), want (%v, %v)", tc.name, tc.statuses, installed, known, tc.wantInstalled, tc.wantKnown)
		}
	}
}

func TestPresentContentsBuildsRowsWithKindLabels(t *testing.T) {
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
		{Name: "jq", KindLabel: "Command-line tool", AccessibleName: "jq"},
		{Name: "ripgrep", KindLabel: "Command-line tool", AccessibleName: "ripgrep"},
		{Name: "visual-studio-code", KindLabel: "App", AccessibleName: "visual-studio-code"},
		{Name: "org.gnome.TextEditor", KindLabel: "Flatpak", AccessibleName: "org.gnome.TextEditor"},
		{Name: "Xcode", KindLabel: "App Store", AccessibleName: "Xcode"},
		{Name: "golang.go", KindLabel: "VS Code extension", AccessibleName: "golang.go"},
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
