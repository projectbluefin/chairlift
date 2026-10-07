package bundleview

import (
	"strings"
	"sync"
	"testing"
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

// Dakota ships these collections in /usr/share/ublue-os/homebrew (observed on
// dakota:testing, 2026-10-07). An unnamed one falls back to its title-cased
// file name and a generic summary, which is how "Nsl" reached the Apps page.
func TestDakotaCollectionsAreAllNamed(t *testing.T) {
	for _, id := range []string{
		"ai-tools", "artwork", "cli", "cncf", "dakota-dev-flatpaks", "dakota-fonts",
		"experimental-ide", "fonts", "fonts-dev", "full-desktop", "ide", "k8s-tools",
		"nsl", "swift", "system-dx-flatpaks", "system-flatpaks", "video-wallpaper",
		"wallpaper-slideshow",
	} {
		if _, ok := catalog[id]; !ok {
			t.Errorf("collection %q has no catalog entry", id)
		}
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

// TestGateInstallPhaseFollowsTheLifecycle covers what a late-connected
// button reads to join at the phase every other button for the collection
// shows: a button built after a run started must read "Installing…" and be
// insensitive, one built after a live success "Installed" and insensitive,
// and one built after a failed or dry run "Install" and sensitive — never a
// sensitive "Install" over a completed gate, which does nothing when clicked.
func TestGateInstallPhaseFollowsTheLifecycle(t *testing.T) {
	var gate InstallGate
	steps := []struct {
		name          string
		transition    func()
		wantLabel     string
		wantSensitive bool
	}{
		{"ready", func() {}, InstallLabelReady, true},
		{"running", func() { gate.TryStart() }, InstallLabelRunning, false},
		{"reset after a failure or dry run", func() { gate.Reset() }, InstallLabelReady, true},
		{"running again", func() { gate.TryStart() }, InstallLabelRunning, false},
		{"completed", func() { gate.Complete() }, InstallLabelCompleted, false},
		{"reset cannot reopen a completion", func() { gate.Reset() }, InstallLabelCompleted, false},
	}
	for _, step := range steps {
		step.transition()
		label, sensitive := gate.InstallPhase()
		if label != step.wantLabel || sensitive != step.wantSensitive {
			t.Fatalf("%s: InstallPhase = (%q, %v), want (%q, %v)", step.name, label, sensitive, step.wantLabel, step.wantSensitive)
		}
	}
}
