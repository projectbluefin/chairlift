package pageview

import (
	"reflect"
	"testing"
	"time"
)

func TestApplicationRowsCoverEveryPresentation(t *testing.T) {
	tests := []struct {
		name string
		got  Row
		want Row
	}{
		{
			name: "unpinned Homebrew package",
			got:  HomebrewPackage("ripgrep", "14.1.1", false),
			want: Row{Title: "ripgrep", Subtitle: "14.1.1"},
		},
		{
			name: "pinned Homebrew package",
			got:  HomebrewPackage("ripgrep", "14.1.1", true),
			want: Row{Title: "ripgrep", Subtitle: "14.1.1 • Pinned"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("presentation = %#v, want %#v", tt.got, tt.want)
			}
		})
	}
}

func TestUpdateRowsCoverEveryPresentation(t *testing.T) {
	tapTests := []struct {
		name     string
		formulae []string
		casks    []string
		want     Row
	}{
		{
			name:     "several programs",
			formulae: []string{"vendor/tools/demo", "plain"},
			casks:    []string{"vendor/apps/gui"},
			want: Row{
				Title:    "vendor/tap",
				Subtitle: "Updates are paused for 3 programs you installed from this source",
			},
		},
		{
			name:     "one program",
			formulae: []string{"plain"},
			want: Row{
				Title:    "vendor/tap",
				Subtitle: "Updates are paused for 1 program you installed from this source",
			},
		},
		{
			name: "no programs",
			want: Row{Title: "vendor/tap", Subtitle: "Updates are paused for software from this source"},
		},
	}
	for _, tt := range tapTests {
		t.Run("untrusted tap/"+tt.name, func(t *testing.T) {
			got := UntrustedTap("vendor/tap", tt.formulae, tt.casks)
			if got != tt.want {
				t.Fatalf("UntrustedTap() = %#v, want %#v", got, tt.want)
			}
		})
	}

}

func TestBootcUpdateSubtitlesCoverEveryState(t *testing.T) {
	tests := []struct {
		name    string
		staged  bool
		version string
		want    string
	}{
		{
			name: "not staged",
			want: "Check whether a newer version of the operating system is available",
		},
		{
			name:   "staged without version",
			staged: true,
			want:   "A new version is ready and installs when you restart",
		},
		{
			name:    "staged with version",
			staged:  true,
			version: "42.1",
			want:    "Version 42.1 is ready and installs when you restart",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BootcUpdateSubtitle(tt.staged, tt.version); got != tt.want {
				t.Fatalf("BootcUpdateSubtitle(%v, %q) = %q, want %q", tt.staged, tt.version, got, tt.want)
			}
		})
	}

	resultTests := []struct {
		name    string
		staged  bool
		version string
		want    string
	}{
		{
			name:   "staged with version",
			staged: true, version: "42.1",
			want: "Version 42.1 is ready and installs when you restart",
		},
		{
			name:   "staged without version",
			staged: true,
			want:   "A new version is ready and installs when you restart",
		},
		{
			name: "nothing staged",
			want: "Your system is up to date",
		},
	}
	for _, tt := range resultTests {
		t.Run("stage result/"+tt.name, func(t *testing.T) {
			got := BootcStageResultSubtitle(tt.staged, tt.version)
			if got != tt.want {
				t.Fatalf(
					"BootcStageResultSubtitle(%v, %q) = %q, want %q",
					tt.staged,
					tt.version,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestFeatureRowsAndDescriptions(t *testing.T) {
	if got, want := Feature("gaming", "Gaming support"), (Row{Title: "Gaming support", Subtitle: "gaming"}); got != want {
		t.Fatalf("Feature() = %#v, want %#v", got, want)
	}
	for _, tt := range []struct {
		count int
		want  string
	}{
		{count: 0, want: "0 features available"},
		{count: 1, want: "1 features available"},
		{count: 3, want: "3 features available"},
	} {
		if got := FeatureGroupDescription(tt.count); got != tt.want {
			t.Errorf("FeatureGroupDescription(%d) = %q, want %q", tt.count, got, tt.want)
		}
	}
}

func TestHelpResourcesPreserveConfiguredOrder(t *testing.T) {
	got := HelpResources("https://example.test", "", "https://chat.example.test")
	want := []HelpResource{
		{Title: "Visit project website", URL: "https://example.test"},
		{Title: "Browse documentation", URL: "https://chat.example.test"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HelpResources() = %#v, want %#v", got, want)
	}

	all := HelpResources("website", "issues", "chat")
	if len(all) != 3 || all[1] != (HelpResource{Title: "Report a problem", URL: "issues"}) {
		t.Fatalf("HelpResources(all configured) = %#v, want all three resources in display order", all)
	}
	if none := HelpResources("", "", ""); len(none) != 0 {
		t.Fatalf("HelpResources(empty) = %#v, want no resources", none)
	}
}

func TestMaintenanceCommandsPreservePrivilegeBoundary(t *testing.T) {
	tests := []struct {
		name string
		sudo bool
		want Command
	}{
		{
			name: "unprivileged",
			want: Command{Name: "/usr/bin/cleanup"},
		},
		{
			name: "privileged",
			sudo: true,
			want: Command{Name: "pkexec", Args: []string{"/usr/bin/cleanup"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaintenanceCommand("/usr/bin/cleanup", tt.sudo)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("MaintenanceCommand() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// The primary row is the one a person reads at a glance, so it carries a
// version and a date and nothing else — never the digest or the registry
// path, which live behind Details.
func TestSystemVersionRowStaysReadable(t *testing.T) {
	const released = "2026-08-10T20:08:01-06:00"
	parsed, err := time.Parse(time.RFC3339, released)
	if err != nil {
		t.Fatal(err)
	}
	date := parsed.Local().Format("2 January 2006")

	tests := []struct {
		name                             string
		version, released, stagedVersion string
		staged                           bool
		want                             string
	}{
		{
			name:    "version and date",
			version: "42.20260810", released: released,
			want: "You are running version 42.20260810, released " + date,
		},
		{
			name:    "version only",
			version: "42.20260810",
			want:    "You are running version 42.20260810",
		},
		{
			name:     "unparseable date is dropped",
			version:  "42.20260810",
			released: "not-a-time",
			want:     "You are running version 42.20260810",
		},
		{
			name:     "date only",
			released: released,
			want:     "You are running the version released " + date,
		},
		{
			name: "nothing readable",
			want: "This system's version could not be read",
		},
		{
			name:    "an update is waiting",
			version: "42.20260810", staged: true, stagedVersion: "42.20260901",
			want: "You are running version 42.20260810. Version 42.20260901 is ready and installs when you restart",
		},
		// A composefs host can say an update is staged without being able
		// to read its version unprivileged; the row must still say so.
		{
			name:    "an update of unknown version is waiting",
			version: "20260921", staged: true,
			want: "You are running version 20260921. A new version is ready and installs when you restart",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := SystemVersionRow(tt.version, tt.released, tt.staged, tt.stagedVersion)
			if row.Title != "System version" {
				t.Fatalf("SystemVersionRow().Title = %q, want %q", row.Title, "System version")
			}
			if row.Subtitle != tt.want {
				t.Fatalf("SystemVersionRow(%q, %q, %v, %q).Subtitle = %q, want %q",
					tt.version, tt.released, tt.staged, tt.stagedVersion, row.Subtitle, tt.want)
			}
		})
	}
}

// Details is where the identifiers live, and an unknown value must produce
// no row at all rather than a labelled blank.
func TestSystemVersionDetailsOmitUnknownFields(t *testing.T) {
	full := SystemVersionDetails(
		"42.20260810",
		"2026-08-10T20:08:01-06:00",
		"ghcr.io/ublue-os/bluefin:latest",
		"sha256:0123456789abcdef0123456789abcdef",
	)
	titles := make([]string, 0, len(full))
	for _, row := range full {
		if row.Subtitle == "" {
			t.Fatalf("SystemVersionDetails() produced an empty %q row", row.Title)
		}
		titles = append(titles, row.Title)
	}
	want := []string{"Version", "Released", "Source", "Build ID"}
	if !reflect.DeepEqual(titles, want) {
		t.Fatalf("SystemVersionDetails() titles = %#v, want %#v", titles, want)
	}
	if got := full[3].Subtitle; got != ShortDigest("sha256:0123456789abcdef0123456789abcdef") {
		t.Fatalf("Build ID = %q, want the shortened digest", got)
	}

	if rows := SystemVersionDetails("", "not-a-time", "", ""); len(rows) != 0 {
		t.Fatalf("SystemVersionDetails() with nothing known = %#v, want no rows", rows)
	}
}

func TestSystemDigestShortening(t *testing.T) {
	tests := []struct {
		name   string
		digest string
		want   string
	}{
		{name: "empty"},
		{name: "short", digest: "sha256:1234", want: "sha256:1234"},
		{name: "exact boundary", digest: "1234567890123456789", want: "1234567890123456789"},
		{name: "truncated", digest: "12345678901234567890", want: "1234567890123456789..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShortDigest(tt.digest); got != tt.want {
				t.Fatalf("ShortDigest(%q) = %q, want %q", tt.digest, got, tt.want)
			}
		})
	}
}

func TestStagingLogSubtitleNamesTheCapWhenOneApplied(t *testing.T) {
	tests := []struct {
		name  string
		shown int
		total int
		want  string
	}{
		{
			name: "before any output",
			want: "View output",
		},
		{
			name:  "single line",
			shown: 1, total: 1,
			want: "View output (1 line)",
		},
		{
			name:  "every line retained",
			shown: 37, total: 37,
			want: "View output (37 lines)",
		},
		{
			name:  "window over a verbose run",
			shown: 200, total: 4321,
			want: "Showing the last 200 of 4321 lines",
		},
		{
			name:  "first dropped line",
			shown: 200, total: 201,
			want: "Showing the last 200 of 201 lines",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StagingLogSubtitle(tt.shown, tt.total); got != tt.want {
				t.Fatalf("StagingLogSubtitle(%d, %d) = %q, want %q", tt.shown, tt.total, got, tt.want)
			}
		})
	}
}

// TestFeaturesEmptyStateOnlyWhenNothingIsOffered holds issue #352: a host
// with no Developer, Gaming, or Printers group and no optional features must
// explain the empty page, and any one offering — including a features group
// still checking, or a Printers group whose switches are all locked — must
// suppress the explanation.
func TestFeaturesEmptyStateOnlyWhenNothingIsOffered(t *testing.T) {
	for _, tc := range []struct {
		bluefin, printers, optional, want bool
	}{
		{want: true},
		{bluefin: true},
		{printers: true},
		{optional: true},
		{bluefin: true, printers: true, optional: true},
	} {
		row, empty := FeaturesEmptyState(tc.bluefin, tc.printers, tc.optional)
		if empty != tc.want {
			t.Errorf("FeaturesEmptyState(%v, %v, %v) empty = %v, want %v", tc.bluefin, tc.printers, tc.optional, empty, tc.want)
		}
		if empty && (row.Title == "" || row.Subtitle == "") {
			t.Errorf("FeaturesEmptyState(%v, %v, %v) shows an empty state without saying why: %+v", tc.bluefin, tc.printers, tc.optional, row)
		}
	}
}
