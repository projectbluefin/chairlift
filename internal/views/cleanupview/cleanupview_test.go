package cleanupview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/updateproviders"
)

func step(id updateproviders.StepID, outcome updateproviders.StepOutcome) updateproviders.StepResult {
	return updateproviders.StepResult{ID: id, Outcome: outcome}
}

func TestSummarizeReportsOnlyWhatHappened(t *testing.T) {
	const gigabyte = 2_000_000_000
	measured := Space{Before: 1_000_000_000, After: 1_000_000_000 + gigabyte, Measured: true}
	unmeasured := Space{Measured: false}
	unchanged := Space{Before: 5_000, After: 5_000, Measured: true}

	tests := []struct {
		name    string
		dryRun  bool
		results []updateproviders.StepResult
		space   Space
		want    string
		isError bool
		noRow   bool
	}{
		{
			name:    "both cleaned with a measured gain names the number",
			results: []updateproviders.StepResult{step(updateproviders.StepOldDownloads, updateproviders.OutcomeCleaned), step(updateproviders.StepUnusedSupport, updateproviders.OutcomeCleaned)},
			space:   measured,
			want:    "Freed 2.0 GB.",
		},
		{
			name:    "cleaned without a usable measurement claims no number",
			results: []updateproviders.StepResult{step(updateproviders.StepOldDownloads, updateproviders.OutcomeCleaned), step(updateproviders.StepUnusedSupport, updateproviders.OutcomeSkipped)},
			space:   unmeasured,
			want:    "Cleanup finished.",
		},
		{
			name:    "a measurement that did not move claims no number",
			results: []updateproviders.StepResult{step(updateproviders.StepOldDownloads, updateproviders.OutcomeCleaned)},
			space:   unchanged,
			want:    "Cleanup finished.",
		},
		{
			name:    "every provider absent is not a cleanup",
			results: []updateproviders.StepResult{step(updateproviders.StepOldDownloads, updateproviders.OutcomeSkipped), step(updateproviders.StepUnusedSupport, updateproviders.OutcomeSkipped)},
			space:   measured,
			want:    "There was nothing to clean up.",
		},
		{
			name:    "a failure is named per provider, not swallowed",
			results: []updateproviders.StepResult{step(updateproviders.StepOldDownloads, updateproviders.OutcomeFailed), step(updateproviders.StepUnusedSupport, updateproviders.OutcomeCleaned)},
			space:   unmeasured,
			want:    "Could not remove old downloads. Removed unused supporting software.",
			isError: true,
		},
		{
			name:    "a cancelled authentication is reported as cancelled",
			results: []updateproviders.StepResult{step(updateproviders.StepOldDownloads, updateproviders.OutcomeCleaned), step(updateproviders.StepUnusedSupport, updateproviders.OutcomeCancelled)},
			space:   unmeasured,
			want:    "Removed old downloads. Removing unused supporting software was cancelled.",
			isError: true,
		},
		{
			name:    "a partial run still names a measured gain",
			results: []updateproviders.StepResult{step(updateproviders.StepOldDownloads, updateproviders.OutcomeCleaned), step(updateproviders.StepUnusedSupport, updateproviders.OutcomeFailed)},
			space:   measured,
			want:    "Removed old downloads. Could not remove unused supporting software. Freed 2.0 GB.",
			isError: true,
		},
		{
			name:    "an entirely failed run never mentions space",
			results: []updateproviders.StepResult{step(updateproviders.StepOldDownloads, updateproviders.OutcomeFailed), step(updateproviders.StepUnusedSupport, updateproviders.OutcomeFailed)},
			space:   measured,
			want:    "Could not remove old downloads. Could not remove unused supporting software.",
			isError: true,
		},
		{
			name:    "dry run claims nothing and leaves the row alone",
			dryRun:  true,
			results: []updateproviders.StepResult{step(updateproviders.StepOldDownloads, updateproviders.OutcomeCleaned), step(updateproviders.StepUnusedSupport, updateproviders.OutcomeCleaned)},
			space:   measured,
			want:    "[DRY-RUN] Preview: nothing was removed — no changes made",
			noRow:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Summarize(tt.dryRun, tt.results, tt.space)
			if got.Toast != tt.want {
				t.Errorf("Toast = %q, want %q", got.Toast, tt.want)
			}
			wantHeadline := tt.want
			if tt.noRow {
				wantHeadline = ""
			}
			if got.Headline != wantHeadline {
				t.Errorf("Headline = %q, want %q", got.Headline, wantHeadline)
			}
			if got.IsError != tt.isError {
				t.Errorf("IsError = %v, want %v", got.IsError, tt.isError)
			}
		})
	}
}

func TestReclaimedRefusesUntrustworthyReadings(t *testing.T) {
	tests := []struct {
		name  string
		space Space
		want  int64
		ok    bool
	}{
		{"unmeasured", Space{Before: 1, After: 1_000_000_000}, 0, false},
		{"below the noise floor", Space{Before: 0, After: MinReportableBytes - 1, Measured: true}, 0, false},
		{"at the floor", Space{Before: 0, After: MinReportableBytes, Measured: true}, MinReportableBytes, true},
		{"free space fell during the run", Space{Before: 5_000_000_000, After: 4_000_000_000, Measured: true}, 0, false},
		{"genuine gain", Space{Before: 1_000_000_000, After: 3_500_000_000, Measured: true}, 2_500_000_000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.space.Reclaimed()
			if ok != tt.ok || got != tt.want {
				t.Errorf("Reclaimed() = %d, %v, want %d, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestFormatBytesUsesDecimalUnits(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 bytes"},
		{999, "999 bytes"},
		{1_000, "1.0 kB"},
		{1_500_000, "1.5 MB"},
		{12_000_000, "12 MB"},
		{2_000_000_000, "2.0 GB"},
		{3_400_000_000_000, "3.4 TB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.in); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// An unknown step has no name, so it must drop out of the sentence rather
// than appear as an empty phrase or a raw identifier.
func TestUnknownStepsAreLeftOutOfTheSentence(t *testing.T) {
	results := []updateproviders.StepResult{
		step(updateproviders.StepOldDownloads, updateproviders.OutcomeFailed),
		step(updateproviders.StepID("future-provider"), updateproviders.OutcomeCleaned),
	}
	got := Summarize(false, results, Space{})
	if strings.Contains(got.Toast, "future-provider") || strings.Contains(got.Toast, "Removed .") {
		t.Errorf("Toast = %q, want the unnamed step omitted", got.Toast)
	}
}

// The page's row text is a hard gate: no filenames, no paths, no tool names.
func TestRowTextNamesNoToolsOrPaths(t *testing.T) {
	texts := []string{
		GroupTitle, GroupDescription, RowTitle, RowSubtitle, ButtonLabel, BusyLabel,
		ScriptsGroupTitle, ScriptsGroupDescription, ScriptsAdminSubtitle,
		ScriptsButtonLabel, ScriptsBusyLabel,
		Label(updateproviders.StepOldDownloads), Label(updateproviders.StepUnusedSupport),
	}
	banned := []string{"Flatpak", "Homebrew", "brew", "runtime", "pkexec", "sudo", "/", ".Brewfile"}
	for _, text := range texts {
		for _, word := range banned {
			if strings.Contains(text, word) {
				t.Errorf("%q contains %q, which no user should have to read", text, word)
			}
		}
	}
}

// A partial total would understate the "before" reading and so overstate
// what the run reclaimed, which is why an unreadable path fails the whole
// measurement rather than being skipped.
func TestFreeBytesReadsRealFilesystemsAndRefusesMissingOnes(t *testing.T) {
	if _, ok := FreeBytes([]string{t.TempDir()}); !ok {
		t.Error("FreeBytes(tempdir) reported no reading, want a reading")
	}
	if _, ok := FreeBytes([]string{t.TempDir(), "/nonexistent-path-for-cleanupview"}); ok {
		t.Error("FreeBytes reported a total despite an unreadable path, want no reading")
	}
	if _, ok := FreeBytes(nil); ok {
		t.Error("FreeBytes(nil) reported a total, want no reading")
	}
}
