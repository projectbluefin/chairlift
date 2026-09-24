package bundleview

import (
	"strings"
	"sync"
	"testing"
)

const wantGroupDescription = "Install a set of apps and tools together in one step. " +
	"Collections come from Homebrew, a third-party source, and can be a large download."

func TestPresentEnumeratesLoadOutcomes(t *testing.T) {
	tests := []struct {
		name               string
		count              int
		warning            string
		wantDescription    string
		wantPlaceholder    string
		wantPlaceholderSub string
	}{
		{
			name:               "empty",
			wantDescription:    wantGroupDescription,
			wantPlaceholder:    "No collections available",
			wantPlaceholderSub: "This system does not offer any app collections.",
		},
		{
			// The discovery warning names the directories that were
			// searched, so it stays in the log and never reaches a row.
			name:               "empty with errors",
			warning:            "read bundle directory \"/usr/share/ublue-os/homebrew\": permission denied",
			wantDescription:    wantGroupDescription,
			wantPlaceholder:    "Collections could not be loaded",
			wantPlaceholderSub: "Something went wrong while looking for app collections.",
		},
		{
			name:            "one",
			count:           1,
			wantDescription: wantGroupDescription,
		},
		{
			name:            "partial",
			count:           2,
			warning:         "read bundle directory \"/opt/extra\": permission denied",
			wantDescription: wantGroupDescription + " Some collections could not be read.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Present(tt.count, tt.warning)
			if got.Description != tt.wantDescription {
				t.Errorf("Present() description = %q, want %q", got.Description, tt.wantDescription)
			}
			if got.PlaceholderTitle != tt.wantPlaceholder {
				t.Errorf("Present() placeholder title = %q, want %q", got.PlaceholderTitle, tt.wantPlaceholder)
			}
			if got.PlaceholderSubtitle != tt.wantPlaceholderSub {
				t.Errorf("Present() placeholder subtitle = %q, want %q", got.PlaceholderSubtitle, tt.wantPlaceholderSub)
			}
		})
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
			name:         "known collection with no comment",
			id:           "cli",
			itemCount:    16,
			wantTitle:    "Command line tools",
			wantSubtitle: "A modern set of everyday terminal utilities. Includes 16 apps and tools.",
		},
		{
			name:         "known collection with a jargon comment",
			id:           "cncf",
			comment:      "CNCF Projects Brewfile",
			itemCount:    80,
			wantTitle:    "Cloud native tools",
			wantSubtitle: "Tools for building and running cloud native software. Includes 80 apps and tools.",
		},
		{
			name:         "known collection whose comment is its own file name",
			id:           "full-desktop",
			comment:      "full-desktop.Brewfile",
			itemCount:    58,
			wantTitle:    "Full desktop",
			wantSubtitle: "A complete set of desktop apps for everyday use. Includes 58 apps and tools.",
		},
		{
			name:         "known collection whose comment names packaging",
			id:           "system-flatpaks",
			comment:      "Default system-wide flatpaks for Bluefin",
			itemCount:    39,
			wantTitle:    "Everyday apps",
			wantSubtitle: "The desktop apps recommended for everyday use. Includes 39 apps and tools.",
		},
		{
			name:         "known collection whose comment names a tool the person never runs",
			id:           "dakota-fonts",
			comment:      "Dakota's optional font selection, installed by ujust install-dev-fonts.",
			itemCount:    15,
			wantTitle:    "Extra fonts",
			wantSubtitle: "An additional font selection for design and development. Includes 15 apps and tools.",
		},
		{
			name:         "known collection whose comment is a heading",
			id:           "swift",
			comment:      "Swift Development Environment",
			itemCount:    3,
			wantTitle:    "Swift development",
			wantSubtitle: "Everything needed to build Swift projects. Includes 3 apps and tools.",
		},
		{
			name:         "known collection with a single entry",
			id:           "ide",
			itemCount:    1,
			wantTitle:    "Code editors",
			wantSubtitle: "Popular code editors and development environments. Includes 1 app or tool.",
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
			wantSubtitle: "A set of apps and tools put together for this system. Includes 2 apps and tools.",
		},
		{
			name:         "unknown collection keeps interior capitalization",
			id:           "k9s_addons",
			wantTitle:    "K9s addons",
			wantSubtitle: "A set of apps and tools put together for this system.",
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

// TestNoCollectionRowLeaksToolingIdentity holds the rule the group exists to
// satisfy: a person reading a row never sees a file name, a location, or a
// packaging term. Homebrew itself is exempt — the group description names it
// deliberately, because the software comes from a third party.
func TestNoCollectionRowLeaksToolingIdentity(t *testing.T) {
	banned := []string{
		"brewfile", "flatpak", "flathub", "ujust", "bootc", "quadlet",
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
	rows = append(rows, presentation.PlaceholderTitle, presentation.PlaceholderSubtitle)

	for _, text := range rows {
		lowered := strings.ToLower(text)
		for _, marker := range banned {
			if strings.Contains(lowered, marker) {
				t.Errorf("collection text %q contains %q", text, marker)
			}
		}
	}
}

func TestGateAllowsOnlyOneConcurrentAction(t *testing.T) {
	var gate InstallGate
	const callers = 64

	start := make(chan struct{})
	results := make(chan bool, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- gate.TryStart()
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	acquired := 0
	for result := range results {
		if result {
			acquired++
		}
	}
	if acquired != 1 {
		t.Fatalf("concurrent TryStart acquisitions = %d, want exactly 1", acquired)
	}
}

func TestGateResetAndCompletion(t *testing.T) {
	var gate InstallGate
	if !gate.TryStart() {
		t.Fatal("zero-value gate did not start")
	}
	gate.Reset()
	if !gate.TryStart() {
		t.Fatal("reset gate did not restart")
	}
	gate.Complete()
	if gate.TryStart() {
		t.Fatal("completed gate restarted")
	}
	gate.Reset()
	if gate.TryStart() {
		t.Fatal("reset reopened a completed gate")
	}
}
